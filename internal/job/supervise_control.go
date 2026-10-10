package job

import (
	"path/filepath"
	"strconv"
	"time"
)

// supervise_control.go is the supervisor's side of the dispatcher's control
// requests and of the job's time budget: it delivers amend and send to the
// job orchestrator, asks for an immediate push on checkpoint, starts the
// stop path on cancel, warns at 80 % of the budget and starts the stop path
// at 100 %. Delivery is at-least-once: a request retires only after its
// delivery and its event, so a crash between the two redelivers the request
// on the next supervisor run — a redelivered amend is a duplicate
// amendment the orchestrator applies idempotently, and a redelivered
// checkpoint only asks for another (no-op) push. A cancel is the stop path's
// delivery: it stays pending through the grace and retires only after the
// stop's terminal outcome is recorded, so a supervisor that restarts inside
// the grace finds it and starts the stop once more.

// stopAmendBody is the exact body of the stop amendment the stop path
// writes and dispatches (contract "Cancel and timeout").
const stopAmendBody = "Stop now and report what you have: commit what is integrated, release your workers and write your report.\n"

// defaultTimeoutGrace is the grace the timeout stop path gives the job
// orchestrator; the cancel grace comes from the request.
const defaultTimeoutGrace = 120 * time.Second

// deliverControl reads the pending control requests and handles them in the
// queue's order: cancel starts the stop path on its first sight in this
// process and is a no-op while a stop is running (no second stop
// amendment, no grace reset, no retirement — it retires only after the
// stop's terminal outcome is recorded), checkpoint asks for an immediate
// push, and amend and send are delivered to the job orchestrator — a
// delivery failure keeps the request for the next tick and writes one
// friction line naming the request. A request retires only after its
// delivery and its event.
func (s *Supervisor) deliverControl() (int, bool, error) {
	items, err := s.Store.PendingControl(s.ID)
	if err != nil {
		return 0, false, err
	}
	for _, item := range items {
		switch item.Kind {
		case "cancel":
			// The pending cancel stays in control/ until the stop path's
			// terminal outcome retires it (endStop): a supervisor that
			// restarts inside the grace finds it and starts the stop once
			// more. While a stop is already running the request is a no-op
			// for this tick: no new stop amendment, no grace reset, no
			// retirement.
			if s.stopping == "" {
				s.beginStop("canceled", time.Duration(item.Grace)*time.Second)
			}
		case "checkpoint":
			s.pushNow = true
			if _, err := s.Store.Append(s.ID, EventIn{Tipo: "checkpoint", Resumo: "push requested"}); err != nil {
				return 0, false, err
			}
			if err := s.Store.RetireControl(s.ID, item); err != nil {
				return 0, false, err
			}
		case "amend", "send":
			if err := s.deliverControlBody(item, item.Kind == "amend"); err != nil {
				return 0, false, err
			}
		}
	}
	return 0, false, nil
}

// deliverControlBody delivers one amend or send request to the job
// orchestrator: on success the amend_received event lands (refs.seq_ref the
// request's counter), an amend unblocks a blocked job, and the request
// retires. A delivery failure keeps the request for the next tick and
// writes one friction line naming the request.
func (s *Supervisor) deliverControlBody(item ControlItem, amend bool) error {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return err
	}
	var deliverErr error
	if amend {
		deliverErr = s.Ops.Dispatch(snap.State.Orchestrator, item.Path, true)
	} else {
		deliverErr = s.Ops.Send(snap.State.Orchestrator, item.Path)
	}
	if deliverErr != nil {
		if s.Friction != nil {
			s.Friction("job: cannot deliver the control request " + item.Name)
		}
		return nil
	}
	resumo := "note delivered"
	if amend {
		resumo = "amendment delivered"
	}
	if _, err := s.Store.Append(s.ID, EventIn{
		Tipo:   "amend_received",
		Resumo: resumo,
		Refs:   map[string]string{"seq_ref": strconv.Itoa(item.Seq)},
	}); err != nil {
		return err
	}
	if amend && snap.State.Status == StatusBlocked {
		if _, err := s.Store.Transition(s.ID, StatusRunning); err != nil {
			return err
		}
		if _, err := s.Store.Record(s.ID, func(st *State) { st.Motivo = nil }); err != nil {
			return err
		}
		if _, err := s.Store.Append(s.ID, EventIn{Tipo: "unblocked"}); err != nil {
			return err
		}
	}
	return s.Store.RetireControl(s.ID, item)
}

// beginStop starts the stop path shared by cancel and timeout: it writes
// the stop amendment (atomic, 0600, the exact contract body), dispatches it
// to the job orchestrator as an amendment (a failure goes to friction and
// the stop continues), and records the kind and its monotonic deadline.
// checkBudget decides the stop's end from here on. stopping and the
// deadline live only in this process: a supervisor that restarts and finds
// the still-pending cancel starts the stop once more — one stop amendment
// per supervisor process, the request's full grace measured on that
// process's own monotonic clock. That is the accepted bound: the grace
// restarts on a supervisor restart, never on a tick.
func (s *Supervisor) beginStop(kind string, grace time.Duration) {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		if s.Friction != nil {
			s.Friction("job: the supervisor cannot read the state to stop the job")
		}
	}
	name := ""
	if err == nil {
		name = snap.State.Orchestrator
	}
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		if s.Friction != nil {
			s.Friction("job: the supervisor cannot find the job directory to stop the job")
		}
	} else {
		path := filepath.Join(jobDir, "stop-amend.md")
		if err := writeAtomic0600(path, []byte(stopAmendBody)); err != nil && s.Friction != nil {
			s.Friction("job: the supervisor cannot write the stop amendment")
		}
		// The stop continues even when the dispatch fails.
		if err := s.Ops.Dispatch(name, path, true); err != nil && s.Friction != nil {
			s.Friction("job: the supervisor cannot dispatch the stop amendment")
		}
	}
	s.stopping = kind
	s.stopDeadline = s.Clock.Monotonic() + int64(grace.Nanoseconds())
}

// checkBudget applies the job's time budget and decides the stop path's end
// while it runs. The budget is TimeoutMin minutes (120 when 0) measured
// with Clock.Monotonic from a virtual base captured once: this process's
// first reading minus the elapsed time already recorded (StartedAt, wall
// time, read once and clamped at zero, so a started_at in the future starts
// the budget at zero). The wall clock is never compared after that, so a
// backwards Now never shortens or extends the budget. A blocked job still
// counts toward the budget.
func (s *Supervisor) checkBudget() (int, bool, error) {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return 0, false, err
	}
	st := snap.State
	if !s.budgetBaseSet {
		base := s.Clock.Monotonic()
		if st.StartedAt != "" {
			if start, perr := time.Parse(time.RFC3339, st.StartedAt); perr == nil {
				if elapsed := s.Clock.Now().Sub(start); elapsed > 0 {
					base -= int64(elapsed)
				}
			}
		}
		s.budgetBase = base
		s.budgetBaseSet = true
	}
	if s.stopping != "" {
		if name := st.Orchestrator; name != "" {
			if state, _, err := s.Ops.Status(name); err == nil && state == "done" {
				return s.endStop(st)
			}
		}
		if s.Clock.Monotonic() >= s.stopDeadline {
			return s.endStop(st)
		}
		return 0, false, nil
	}
	budget := time.Duration(st.TimeoutMin) * time.Minute
	if st.TimeoutMin <= 0 {
		budget = 120 * time.Minute
	}
	elapsed := time.Duration(s.Clock.Monotonic() - s.budgetBase)
	if elapsed < budget*4/5 {
		return 0, false, nil
	}
	if !s.warned {
		// warned guards against a second warning in this process; the
		// event log guards against one across a restart.
		s.warned = true
		dir, err := s.Store.readDir(s.ID)
		if err != nil {
			return 0, false, err
		}
		events, err := readEvents(dir)
		if err != nil {
			return 0, false, err
		}
		if !hasTipo(events, "timeout_warning") {
			if _, err := s.Store.Append(s.ID, EventIn{
				Tipo:   "timeout_warning",
				Resumo: "80% of the job budget used",
			}); err != nil {
				return 0, false, err
			}
		}
	}
	if elapsed >= budget {
		s.beginStop("timeout", defaultTimeoutGrace)
	}
	return 0, false, nil
}

// endStop ends the stop path: the job moves to finishing when it is not
// there yet, and the outcome follows the kind (canceled 21, timeout 9).
// The terminal path (the git slice) commits leftovers, pushes and releases.
// A pending cancel retires once the terminal outcome is recorded
// (finishOutcome and fail retire it on every terminal path), so nothing
// stays pending on a terminal job; a crash in the window between the
// terminal record and the retirement is retired by the supervisor that
// starts on the terminal job.
func (s *Supervisor) endStop(st State) (int, bool, error) {
	status, motivo, exit := StatusCanceled, "cancelado", ExitCanceled
	if s.stopping == StatusTimeout {
		status, motivo, exit = StatusTimeout, "tempo esgotado", ExitTimeout
	}
	if st.Status != StatusFinishing {
		if _, err := s.Store.Transition(s.ID, StatusFinishing); err != nil {
			return 0, true, err
		}
	}
	return s.finishOutcome(status, motivo, exit)
}

// retirePendingCancel retires the pending cancel request, if any: the
// request stayed pending through the stop so a restarted supervisor finds
// it, and once the job is terminal there is nothing left to deliver. It
// sends nothing; a missing source (already retired) is the no-op that
// makes the retirement idempotent.
func (s *Supervisor) retirePendingCancel() {
	items, err := s.Store.PendingControl(s.ID)
	if err != nil {
		if s.Friction != nil {
			s.Friction("job: the supervisor cannot read the control requests to retire the cancel")
		}
		return
	}
	for _, item := range items {
		if item.Kind != "cancel" {
			continue
		}
		if err := s.Store.RetireControl(s.ID, item); err != nil && s.Friction != nil {
			s.Friction("job: the supervisor cannot retire the cancel request " + item.Name)
		}
	}
}
