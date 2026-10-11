package job

import (
	"errors"
	"testing"
	"time"
)

// Catches the mutation "entering finishing from blocked does not settle the
// blocked interval" (enterFinishing back to a plain TransitionSettle): a
// blocked job whose orchestrator then finishes with a pending decision would
// charge the whole blocked interval to the active budget and time out.
func TestSuperviseBlockedPendingDecisionSettlesTheBlockedClock(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) { st.TimeoutMin = 1 }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Note("job-1", EventIn{Tipo: "decision", Escopo: "global", Resumo: "keep the queue"}); err != nil {
		t.Fatal(err)
	}
	report := writeOrchestratorReport(t, "# Report — job\n\n- [partial] Item 1 — the gate was not run\n")
	team.statusScript = fillScript(nil, 2, "working")
	team.statusScript = fillScript(team.statusScript, 6, "question")
	team.statusScript = append(team.statusScript, fakeStatus{State: "done", Report: report})
	team.statusScript = fillScript(team.statusScript, 20, "done")
	clock.stopAfter = 12
	clock.stop = errors.New("stop the watch loop")
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("the watch loop ended before the stop: the job did not stay blocked")
				return
			}
			if !errors.Is(r.(error), clock.stop) {
				panic(r)
			}
		}()
		if _, err := sup.Run(); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()
	st := supState(t, store)
	if st.Status != StatusBlocked || st.Motivo == nil || *st.Motivo != decisionsPendingMotivo {
		t.Fatalf("state = %s motivo=%v, want blocked on the pending decision", st.Status, st.Motivo)
	}
	// Ticks every 10 s from t0: working t0, t10; question t20 blocks
	// (20 s active settled); done t80 leaves blocked into finishing (60 s
	// blocked settled) and re-blocks at once (no active time between).
	if st.BudgetActiveMs != 20000 || st.BudgetBlockedMs != 60000 {
		t.Fatalf("settled = active %d / blocked %d ms, want 20000 / 60000", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	events := mustEvents(t, jobDir)
	if countTipe(events, "terminal") != 0 || countTipe(events, "timeout_warning") != 0 {
		t.Fatalf("event types = %v, want no terminal and no active-budget warning", eventTipes(events))
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}
