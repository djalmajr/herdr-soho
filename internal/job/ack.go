package job

import (
	"fmt"
	"strconv"
)

// Ack moves decisions_acked_seq up to seq when seq is greater. The event log
// is the source of truth: each decision_acked event carries refs.seq_ref,
// the acknowledged upto, so a crash between the append and the state save
// converges on retry. A lower or equal value changes nothing and adds no
// event. upto below 1 is exit 2.
func (s *Store) Ack(id string, upto int) (State, error) {
	if upto < 1 {
		return State{}, errUsage("job: ack --upto must be >= 1")
	}
	var st State
	wrote := false
	err := s.withExisting(id, func(dir string) error {
		var err error
		st, err = readState(dir)
		if err != nil {
			return err
		}
		events, err := readEvents(dir)
		if err != nil {
			return err
		}
		watermark := effectiveWatermark(st.DecisionsAckedSeq, events)
		if watermark > st.DecisionsAckedSeq {
			// The log is ahead of the state: a crash happened after the
			// append. Repair the state from the log before anything else.
			st.DecisionsAckedSeq = watermark
			wrote = true
			if err = saveState(dir, st); err != nil {
				return err
			}
		}
		if upto <= watermark {
			// A no-op in a report-carrying state still refreshes the stored
			// report: a failed report rename after a successful write leaves
			// the log and the state converged but the report missing or
			// stale, and the retry is the repair.
			return s.refreshIfWrote(dir, st, reportSetState(st.Status) || wrote)
		}
		// Publish the acknowledgement event first, then the watermark.
		// There is no rollback of a written event; the retry repairs from
		// the log.
		if _, err = appendEventFn(dir, s.clock().Now(), EventIn{
			Tipo:   "decision_acked",
			Resumo: fmt.Sprintf("ack %d", upto),
			Refs:   map[string]string{"seq_ref": strconv.Itoa(upto)},
		}); err != nil {
			return err
		}
		st.DecisionsAckedSeq = upto
		wrote = true
		if err = saveState(dir, st); err != nil {
			return err
		}
		return s.refreshIfWrote(dir, st, true)
	})
	return st, err
}

// Close moves a terminal or collected job to closed. Pending decisions are
// exit 24 unless force is set; a forced close with pending decisions records
// a cleanup with refs.motivo=force_sem_ack. A job that is already closed
// repairs its stored report without a second cleanup event.
func (s *Store) Close(id string, force bool) (State, error) {
	var st State
	err := s.withExisting(id, func(dir string) error {
		var err error
		st, err = readState(dir)
		if err != nil {
			return err
		}
		if st.Status == StatusClosed {
			// Retry after a completed close: repair the stored report
			// (missing or stale) without touching the log or the state.
			return refreshReport(dir, st)
		}
		if !closable(st.Status) {
			return errUsage("job: close is not allowed from " + st.Status)
		}
		closed, err := s.closeLocked(dir, st, force)
		st = closed
		return err
	})
	return st, err
}

// closeLocked is the single close path shared by Close and
// Transition(id, StatusClosed): the pending-decision check, the cleanup
// audit events, the state save and the report refresh. The caller holds the
// job lock and passes the current state.
func (s *Store) closeLocked(dir string, st State, force bool) (State, error) {
	events, err := readEvents(dir)
	if err != nil {
		return State{}, err
	}
	watermark := effectiveWatermark(st.DecisionsAckedSeq, events)
	pending := pendingDecisions(events, watermark)
	if len(pending) > 0 {
		if !force {
			return State{}, &ExitError{Code: ExitUnacked, Msg: "job close: unacknowledged decisions", Seqs: pending}
		}
		// An earlier ordinary cleanup does not count: only a cleanup with
		// exactly this motivo is proof of a prior forced close.
		if !hasCleanupMotivo(events, "force_sem_ack") {
			if _, err = appendEventFn(dir, s.clock().Now(), EventIn{
				Tipo:   "cleanup",
				Resumo: "forced close with unacknowledged decisions",
				Refs:   map[string]string{"motivo": "force_sem_ack"},
			}); err != nil {
				return State{}, err
			}
		}
	} else if !lastIsCleanup(events) {
		if _, err = appendEventFn(dir, s.clock().Now(), EventIn{
			Tipo:   "cleanup",
			Resumo: "cleanup",
		}); err != nil {
			return State{}, err
		}
	}
	// The state still carries the terminal outcome: republish the report
	// while it does, so a missing report is built with the outcome as its
	// status instead of closed.
	if isTerminalOutcome(st.Status) {
		if err := refreshReport(dir, st); err != nil {
			return State{}, err
		}
	}
	st.Status = StatusClosed
	if err := saveState(dir, st); err != nil {
		return State{}, err
	}
	return st, refreshReport(dir, st)
}

func closable(status string) bool {
	switch status {
	case StatusDone, StatusFailed, StatusTimeout, StatusCanceled, StatusCollected, StatusClosed:
		return true
	default:
		return false
	}
}

// effectiveWatermark is the acknowledgement watermark the log and the state
// agree on: the state value and every decision_acked refs.seq_ref.
func effectiveWatermark(acked int, events []Event) int {
	watermark := acked
	for _, event := range events {
		if event.Tipo != "decision_acked" {
			continue
		}
		ref, ok := event.Refs["seq_ref"]
		if !ok {
			continue
		}
		if seq, err := strconv.Atoi(ref); err == nil && seq > watermark {
			watermark = seq
		}
	}
	return watermark
}

// pendingDecisions lists the decision seqs above the watermark, in seq
// order. The result is empty, not nil, when nothing is pending.
func pendingDecisions(events []Event, acked int) []int {
	pending := []int{}
	for _, event := range events {
		if event.Tipo == "decision" && event.Seq > acked {
			pending = append(pending, event.Seq)
		}
	}
	return pending
}

// hasCleanupMotivo reports whether the log already holds a cleanup event
// with exactly this refs.motivo.
func hasCleanupMotivo(events []Event, motivo string) bool {
	for _, event := range events {
		if event.Tipo == "cleanup" && event.Refs["motivo"] == motivo {
			return true
		}
	}
	return false
}

func lastIsCleanup(events []Event) bool {
	return len(events) > 0 && events[len(events)-1].Tipo == "cleanup"
}

// OlderThanCleanSkips reports whether clean --older-than must leave this job
// directory in place: true while a decision seq is above the effective
// acknowledgement watermark. The read is lock-free like the other read
// APIs; the CLI slice adds the clean regression.
func (s *Store) OlderThanCleanSkips(id string) (bool, error) {
	dir, err := s.readDir(id)
	if err != nil {
		return false, err
	}
	st, err := readState(dir)
	if err != nil {
		return false, err
	}
	events, err := readEvents(dir)
	if err != nil {
		return false, err
	}
	return len(pendingDecisions(events, effectiveWatermark(st.DecisionsAckedSeq, events))) > 0, nil
}
