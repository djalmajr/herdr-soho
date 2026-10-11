package job

import (
	"path/filepath"
	"strconv"
	"time"
)

// supervise_control.go is the supervisor's side of the dispatcher's control
// requests and of the job's time budgets: it delivers amend and send to the
// job orchestrator, asks for an immediate push on checkpoint, starts the
// stop path on cancel, warns at 80 % of the active budget and starts the
// stop path at 100 % of it (the timeout outcome), warns at 80 % of the
// blocked budget (the blocked_warning) and starts the stop path at 100 %
// of it (the blocked ceiling, which ends the job through the existing
// timeout outcome with its own motivo). A blocked job pauses the active
// budget: only the seconds in status blocked count against the blocked
// budget. Delivery is at-least-once: a request retires only after its
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
		// The amendment unblocks the job: the crossing settles the blocked
		// interval into the counters and re-anchors in the same write.
		if err := s.settleAndTransition(snap.State, StatusRunning, true); err != nil {
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
		s.askCollaborations()
	}
	s.stopping = kind
	s.stopDeadline = s.Clock.Monotonic() + int64(grace.Nanoseconds())
}

// checkBudget applies the job's time budgets and decides the stop path's
// end while it runs. Both budgets are measured with Clock.Monotonic from a
// virtual base captured once per process for the current interval
// (ensureIntervalBase, from the persisted anchor): the active budget is
// TimeoutMin minutes (120 when 0) and counts the settled active counter
// plus the in-flight interval only while the current interval is active —
// accepted, preparing, running and finishing count as active, so a blocked
// job pauses the ACCRUAL of active time, never its enforcement: a blocked
// job whose settled active total is already at or past 80 % (or 100 %) still
// warns (and stops), deduplicated, while blocked — while the blocked budget
// is BlockedTimeoutMin minutes (the machine default, 1440, when 0 on a
// state without it) and counts only the seconds in status blocked. Each
// interval is settled into its persisted counter at every blocked-boundary
// crossing (settleAndTransition), so a supervisor restart
// neither resets nor double-counts; the wall clock is compared only at the
// base capture, so a backwards Now never shortens or extends a budget. At
// 80 % of the active budget the timeout_warning lands once (active time
// only); at 100 % the stop path ends the job timeout (9). At 80 % of the
// blocked budget the blocked_warning lands once; at 100 % the stop path
// ends the job through the blocked_ceiling kind, the existing timeout
// outcome (9) with its own motivo — the active timeout takes precedence
// when both are reached. orchState is this tick's already-read orchestrator
// state (a stop that starts this tick reuses it for the done check, no
// second read); it is "" on a tick where a stop is already running at the
// tick start (the stop path reads the orchestrator itself).
func (s *Supervisor) checkBudget(orchState string) (int, bool, error) {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return 0, false, err
	}
	st := snap.State
	if err := s.ensureIntervalBase(st); err != nil {
		return 0, false, err
	}
	if s.stopping != "" {
		state := orchState
		if state == "" {
			if name := st.Orchestrator; name != "" {
				if read, _, err := s.Ops.Status(name); err == nil {
					state = read
				}
			}
		}
		if state == "done" {
			return s.endStop(st)
		}
		if s.Clock.Monotonic() >= s.stopDeadline {
			return s.endStop(st)
		}
		return 0, false, nil
	}
	// monoToMs converts the monotonic difference to the whole milliseconds
	// the counters persist: Monotonic reads are nanoseconds, and a raw
	// difference against a millisecond budget would expire every job on
	// the first tick.
	inFlightMs := monoToMs(s.Clock.Monotonic() - s.intervalBase)
	if inFlightMs < 0 {
		inFlightMs = 0
	}
	// Active = the settled active counter always, plus the in-flight
	// interval only when the current interval is active (not blocked):
	// blocking pauses the ACCRUAL of active time, never its enforcement —
	// a blocked job whose settled active total is already at or past 80 %
	// (or 100 %) still warns (and stops), deduplicated, while blocked, and
	// blocked wall time never adds to the active total. The blocked total is
	// cumulative: the settled counter plus the in-flight interval while the
	// job is blocked — so a job past the ceiling stays past it (the ceiling
	// ends it, a stop's grace unblock does not reset the consumed blocked
	// time), and a restarted supervisor re-derives it from the persisted
	// counter. Everything is compared in milliseconds, the unit of the
	// persisted counters.
	var activeTotalMs, blockedTotalMs int64
	activeTotalMs = st.BudgetActiveMs
	if st.Status != StatusBlocked {
		activeTotalMs += inFlightMs
	}
	blockedTotalMs = st.BudgetBlockedMs
	if st.Status == StatusBlocked {
		blockedTotalMs += inFlightMs
	}
	budgetMs := int64((time.Duration(st.TimeoutMin) * time.Minute).Milliseconds())
	if st.TimeoutMin <= 0 {
		budgetMs = int64((120 * time.Minute).Milliseconds())
	}
	if activeTotalMs >= budgetMs*4/5 {
		if !s.warnedActive {
			// warnedActive guards against a second warning in this process;
			// the event log guards against one across a restart.
			s.warnedActive = true
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
					Resumo: "80% of the active job budget used",
				}); err != nil {
					return 0, false, err
				}
			}
		}
		if activeTotalMs >= budgetMs {
			s.beginStop("timeout", defaultTimeoutGrace)
		}
	}
	blockedCapMs := int64((time.Duration(st.BlockedTimeoutMin) * time.Minute).Milliseconds())
	if st.BlockedTimeoutMin <= 0 {
		blockedCapMs = int64((1440 * time.Minute).Milliseconds())
	}
	if blockedTotalMs >= blockedCapMs*4/5 {
		if !s.warnedBlocked {
			// warnedBlocked guards against a second warning in this process;
			// the event log guards against one across a restart.
			s.warnedBlocked = true
			dir, err := s.Store.readDir(s.ID)
			if err != nil {
				return 0, false, err
			}
			events, err := readEvents(dir)
			if err != nil {
				return 0, false, err
			}
			if !hasTipo(events, "blocked_warning") {
				if _, err := s.Store.Append(s.ID, EventIn{
					Tipo:   "blocked_warning",
					Resumo: "80% of the blocked ceiling used",
				}); err != nil {
					return 0, false, err
				}
			}
		}
		if blockedTotalMs >= blockedCapMs {
			// The blocked ceiling runs the same stop path as the timeout: the
			// stop amendment, the bounded grace, the final push and the
			// release are unchanged; endStop ends the job through the
			// existing timeout outcome with the ceiling's own motivo. The
			// active timeout takes precedence: it starts the stop when the
			// active budget is exhausted too, so the ceiling starts it only
			// when the active check did not.
			if s.stopping == "" {
				s.beginStop("blocked_ceiling", defaultTimeoutGrace)
			}
		}
	}
	if s.stopping != "" {
		// The budget check started the stop this tick: the stop path
		// decides, reusing this tick's read (orchState) for the done check
		// (no second read).
		if orchState == "done" {
			return s.endStop(st)
		}
		if s.Clock.Monotonic() >= s.stopDeadline {
			return s.endStop(st)
		}
		return 0, false, nil
	}
	return 0, false, nil
}

// endStop ends the stop path: the job moves to finishing when it is not
// there yet, and the outcome follows the kind (canceled 21, timeout 9, and
// the blocked ceiling — which ends through the existing timeout outcome 9
// with its own motivo, because the lifecycle has no terminal blocked and a
// live blocked state must not be closed). The terminal path (the git slice)
// commits leftovers, pushes and releases. A pending cancel retires once the
// terminal outcome is recorded (finishOutcome and fail retire it on every
// terminal path), so nothing stays pending on a terminal job; a crash in
// the window between the terminal record and the retirement is retired by
// the supervisor that starts on the terminal job.
func (s *Supervisor) endStop(st State) (int, bool, error) {
	status, motivo, exit := StatusCanceled, "cancelado", ExitCanceled
	if s.stopping == StatusTimeout {
		status, motivo, exit = StatusTimeout, "tempo esgotado", ExitTimeout
	} else if s.stopping == "blocked_ceiling" {
		// The ceiling's distinct motivo names it in the state and the
		// terminal refs so a reader tells it from the active-budget
		// timeout; the outcome stays the existing timeout.
		status, motivo, exit = StatusTimeout, "tempo de bloqueio esgotado", ExitTimeout
	}
	// The stop's motivo replaces whatever the state carries (a block's
	// question, an earlier stop) for every kind, before the job moves on:
	// the terminal state never keeps a stale block motivo. The motivo and
	// the terminal intent land in the same write that enters finishing, so
	// a crash never leaves the job finishing without its stop outcome.
	if err := s.enterFinishing(&TerminalIntent{Status: status, Motivo: motivo, Exit: exit}, &motivo); err != nil {
		return 0, true, err
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
