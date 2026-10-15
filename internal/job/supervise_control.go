package job

import (
	"context"
	"os"
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
// the grace finds it and starts the stop once more. A delivery cut by the
// cancel is uncertain, not a retry: outside the stop the delivery runs
// under a context a background observer cancels the moment the pending
// cancel appears, and during the stop it is bounded to the remaining grace
// — a delivery that fails because of the cut records one
// delivery_uncertain event (refs.seq_ref the request's counter) and is
// never re-sent, across restarts; the request stays pending in control/.
// A common failure without a cut keeps the retry: friction and a new
// attempt on the next tick.

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
			// A request whose delivery was already recorded uncertain is
			// never re-sent: the mark lives in the log, across restarts.
			if s.deliveryUncertain(item.Seq) {
				continue
			}
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
// writes one friction line naming the request — unless the failure is the
// cut of the cancel, which records the delivery_uncertain mark instead (no
// friction, no retry). Outside the stop the delivery runs under the
// context the observer cancels when a pending cancel appears in flight;
// during the stop it is bounded to the remaining grace, and with no time
// left it is not attempted at all: the request stays in control/ and the
// stop ends the job.
func (s *Supervisor) deliverControlBody(item ControlItem, amend bool) error {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return err
	}
	ops := s.Ops
	var deliveryCtx context.Context
	stopWatching := func() {}
	if s.stopping != "" {
		// During the stop the delivery is limited to the remaining grace
		// (the bounded adapter); with no time left there is no attempt —
		// the request stays in control/ and the stop ends the job.
		remaining := time.Duration(s.stopDeadline - s.Clock.Monotonic())
		if remaining <= 0 {
			return nil
		}
		ops = s.boundedOps(remaining)
	} else {
		// Outside the stop the delivery runs under the context the
		// observer cancels when a pending cancel arrives in flight: the
		// in-flight subprocess the adapter opened is the one that ends.
		deliveryCtx, stopWatching = s.watchForCancel()
		ops = s.interruptibleOps(deliveryCtx)
	}
	var deliverErr error
	if amend {
		deliverErr = ops.Dispatch(snap.State.Orchestrator, item.Path, true)
	} else {
		deliverErr = ops.Send(snap.State.Orchestrator, item.Path)
	}
	// Read the cut before stopping the watch: stopWatching cancels the
	// context, which would look like a cancel of the delivery.
	cut := deliveryCtx != nil && deliveryCtx.Err() != nil
	stopWatching()
	if deliverErr != nil {
		// The cut is the cancel ending the in-flight delivery: the context
		// was cancelled (outside the stop) or the grace deadline already
		// passed (during it). A common failure without a cut keeps the
		// retry: friction and a new attempt on the next tick.
		if !cut && s.stopping != "" && s.Clock.Monotonic() >= s.stopDeadline {
			cut = true
		}
		if cut {
			return s.markDeliveryUncertain(item)
		}
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

// boundedOps is s.Ops with its command bound lowered to at most d when the
// adapter can bound it (the production adapter and the fakes); the other
// adapters run as is. Callers only bound with a positive remainder.
func (s *Supervisor) boundedOps(d time.Duration) teamOps {
	if b, ok := s.Ops.(interface{ bounded(time.Duration) teamOps }); ok {
		return b.bounded(d)
	}
	return s.Ops
}

// interruptibleOps is s.Ops with its commands running under the delivery
// context when the adapter can take one (the production adapter and the
// fakes); the other adapters run as is.
func (s *Supervisor) interruptibleOps(ctx context.Context) teamOps {
	if i, ok := s.Ops.(interface{ interruptible(context.Context) teamOps }); ok {
		return i.interruptible(ctx)
	}
	return s.Ops
}

// watchForCancel returns a context a background observer cancels the moment
// a pending cancel (control/cancel.json) appears: the observer stats the
// file every poll (250 ms when the poll is zero) on the real clock, touches
// nothing else, and stops when cancel is called — the delivery ended. A
// cancel of the context ends only the subprocesses the delivery adapter
// opened (the platform context), never another process tree.
func (s *Supervisor) watchForCancel() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	dir, err := s.Store.readDir(s.ID)
	if err != nil {
		// The job directory is unreadable: nothing can arrive, so the
		// context stays live and the delivery runs to its own bound.
		return ctx, cancel
	}
	poll := s.Poll
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	path := filepath.Join(dir, "control", "cancel.json")
	go func() {
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(path); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

// deliveryUncertain reports whether the request's counter already carries a
// delivery_uncertain event in the log: the mark that keeps the request from
// being re-sent, across restarts. An unreadable log reports false and the
// delivery is attempted: a redelivered note or amendment is applied
// idempotently, while skipping the delivery would lose the request.
func (s *Supervisor) deliveryUncertain(seq int) bool {
	dir, err := s.Store.readDir(s.ID)
	if err != nil {
		return false
	}
	events, err := readEvents(dir)
	if err != nil {
		return false
	}
	for _, e := range events {
		if e.Tipo == "delivery_uncertain" && e.Refs["seq_ref"] == strconv.Itoa(seq) {
			return true
		}
	}
	return false
}

// markDeliveryUncertain records the delivery_uncertain event of a cut
// delivery, at most one per request: the log deduplicates by tipo and
// seq_ref, the timeout_warning pattern. It records no amend_received,
// unblocks nothing and retires nothing: the request stays pending.
func (s *Supervisor) markDeliveryUncertain(item ControlItem) error {
	dir, err := s.Store.readDir(s.ID)
	if err != nil {
		return err
	}
	events, err := readEvents(dir)
	if err != nil {
		return err
	}
	for _, e := range events {
		if e.Tipo == "delivery_uncertain" && e.Refs["seq_ref"] == strconv.Itoa(item.Seq) {
			return nil
		}
	}
	_, err = s.Store.Append(s.ID, EventIn{
		Tipo:   "delivery_uncertain",
		Resumo: "delivery interrupted by the cancel",
		Refs:   map[string]string{"seq_ref": strconv.Itoa(item.Seq)},
	})
	return err
}

// beginStop starts the stop path shared by cancel and timeout: it writes
// the stop amendment (atomic, 0600, the exact contract body), dispatches it
// to the job orchestrator as an amendment (a failure goes to friction and
// the stop continues), and records the kind and its monotonic deadline.
// checkBudget decides the stop's end from here on. The deadline of a cancel
// is the acceptance's persisted CancelDeadline converted once to this
// process's monotonic clock (the same idiom as ensureIntervalBase and
// ensureFinishingBase): a supervisor that restarts and finds the
// still-pending cancel starts the stop once more — one stop amendment per
// process — and ends the grace on the same wall instant; a restart never
// restarts the grace. A state without the fields (a job accepted by a
// binary before them) or with an unreadable deadline keeps the bound the
// persisted deadline replaces: the request's full grace measured on this
// process's own monotonic clock, with one friction line. The timeout and
// the blocked ceiling have no persisted anchor and always measure their
// grace on the process's own monotonic clock. The deadline is computed
// before the stop amendment dispatches: with time remaining that dispatch
// runs limited to it (the bounded adapter), and a cut dispatch only goes
// to friction; with no time remaining (a persisted deadline already spent,
// for example a restart after the grace) the stop amendment is not
// dispatched at all and writes no friction for it either — dispatching
// under the adapter's own bound would hold the job up to that bound before
// the first check ends the stop, and the first check is what ends it. The
// stop amendment is not a queue request, so it records no
// delivery_uncertain mark.
func (s *Supervisor) beginStop(kind string, grace time.Duration) {
	snap, snapErr := s.Store.Snapshot(s.ID)
	if snapErr != nil {
		if s.Friction != nil {
			s.Friction("job: the supervisor cannot read the state to stop the job")
		}
	}
	name := ""
	if snapErr == nil {
		name = snap.State.Orchestrator
	}
	monoNow := s.Clock.Monotonic()
	deadline := monoNow + int64(grace.Nanoseconds())
	if kind == "canceled" && snapErr == nil {
		if anchor := snap.State.CancelDeadline; anchor != "" {
			if dl, parseErr := time.Parse(time.RFC3339Nano, anchor); parseErr == nil {
				// The persisted acceptance deadline converted to this
				// process's monotonic clock: the wall clock is compared only
				// here, so from then on the stop runs on the monotonic clock
				// only, and a backwards Now never extends the grace.
				deadline = monoNow + int64(dl.Sub(s.Clock.Now()))
			} else if s.Friction != nil {
				s.Friction("job: the persisted cancel deadline is absent or unreadable: the stop measures the full grace from this process")
			}
		} else if s.Friction != nil {
			s.Friction("job: the persisted cancel deadline is absent or unreadable: the stop measures the full grace from this process")
		}
	}
	// Never below monoNow: a wall clock that stepped past the deadline ends
	// the stop on the first check instead of already being past it.
	if deadline < monoNow {
		deadline = monoNow
	}
	s.stopping = kind
	s.stopDeadline = deadline
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		if s.Friction != nil {
			s.Friction("job: the supervisor cannot find the job directory to stop the job")
		}
		return
	}
	path := filepath.Join(jobDir, "stop-amend.md")
	if err := writeAtomic0600(path, []byte(stopAmendBody)); err != nil && s.Friction != nil {
		s.Friction("job: the supervisor cannot write the stop amendment")
	}
	// The stop continues even when the dispatch fails. The dispatch is
	// limited to the grace remaining from the deadline computed above, and
	// with no time remaining it is not attempted at all: no friction for
	// the skip, the first check ends the stop.
	remaining := time.Duration(deadline - s.Clock.Monotonic())
	if remaining > 0 {
		if err := s.boundedOps(remaining).Dispatch(name, path, true); err != nil && s.Friction != nil {
			s.Friction("job: the supervisor cannot dispatch the stop amendment")
		}
	}
	s.askCollaborations()
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
