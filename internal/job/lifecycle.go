package job

import (
	"strconv"
)

// lifecycle.go is the write side of the job state after the job is
// recorded: the run facts (Record) and the failure path (Fail). Both run
// under the job lock and reuse the existing readers, writers and event
// append; nothing here touches the lifecycle edges beyond the ones the
// transitions table already allows.

// failNextStep is the walk Fail takes along existing lifecycle edges to
// failed. The contract's lifecycle has no preparing->failed edge, so a
// preparation failure walks through running and finishing inside one
// locked operation; blocked walks through finishing.
var failNextStep = map[string]string{
	StatusAccepted:  StatusPreparing,
	StatusPreparing: StatusRunning,
	StatusRunning:   StatusFinishing,
	StatusBlocked:   StatusFinishing,
	StatusFinishing: StatusFailed,
}

// Record reads the job state under the job lock, applies update, and saves
// it atomically. update may not change ID, Status, BriefSHA256 or
// DecisionsAckedSeq: the stored values are restored after update runs.
func (s *Store) Record(id string, update func(*State)) (State, error) {
	var st State
	err := s.withExisting(id, func(dir string) error {
		var err error
		st, err = readState(dir)
		if err != nil {
			return err
		}
		saved := st
		update(&st)
		st.ID = saved.ID
		st.Status = saved.Status
		st.BriefSHA256 = saved.BriefSHA256
		st.DecisionsAckedSeq = saved.DecisionsAckedSeq
		return saveState(dir, st)
	})
	return st, err
}

// Fail moves a job to failed under one job lock: a failure event (resumo
// and refs.motivo are the motivo), the stored motivo, the walk along
// existing edges to failed with a save after each step, a terminal event
// (refs.motivo, refs.exit), and the report. A job already failed only
// republishes the report and appends no event; any other terminal or later
// status is refused with exit 2.
func (s *Store) Fail(id, motivo string, exit int) (State, error) {
	var st State
	err := s.withExisting(id, func(dir string) error {
		var err error
		st, err = readState(dir)
		if err != nil {
			return err
		}
		if st.Status == StatusFailed {
			return refreshReport(dir, st)
		}
		if isTerminalOutcome(st.Status) || st.Status == StatusCollected || st.Status == StatusClosed {
			return errUsage("job: transition " + st.Status + " -> failed is not allowed")
		}
		if _, err = appendEventFn(dir, s.clock().Now(), EventIn{
			Tipo:   "failure",
			Resumo: motivo,
			Refs:   map[string]string{"motivo": motivo},
		}); err != nil {
			return err
		}
		pointer := motivo
		st.Motivo = &pointer
		for st.Status != StatusFailed {
			next, ok := failNextStep[st.Status]
			if !ok {
				return errUsage("job: transition " + st.Status + " -> failed is not allowed")
			}
			st.Status = next
			if err := saveState(dir, st); err != nil {
				return err
			}
		}
		// The terminal event carries the outcome in refs; it has no resumo
		// of its own, so no user-facing text is invented here.
		if _, err = appendEventFn(dir, s.clock().Now(), EventIn{
			Tipo: "terminal",
			Refs: map[string]string{"motivo": motivo, "exit": strconv.Itoa(exit)},
		}); err != nil {
			return err
		}
		return refreshReport(dir, st)
	})
	return st, err
}
