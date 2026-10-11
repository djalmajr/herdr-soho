//go:build unix

package job

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runWithin runs the supervisor on a goroutine and fails the test when it
// has not returned after the bound: a blocking read would hold it forever.
func runWithin(t *testing.T, sup *Supervisor) (int, error) {
	t.Helper()
	type result struct {
		exit int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		exit, err := sup.Run()
		done <- result{exit, err}
	}()
	select {
	case r := <-done:
		return r.exit, r.err
	case <-time.After(20 * time.Second):
		t.Fatal("the supervisor blocked on a FIFO")
		return 0, nil
	}
}

// Catches the mutation "the participant's last-report marker is read with
// os.ReadFile" (core.LastReport back to ReadTextFile): the terminal
// snapshot would block on the FIFO marker and the job would never end.
func TestSuperviseTerminalFifoMarkerDoesNotBlock(t *testing.T) {
	var friction []string
	store, jobDir, _, team, sup := nsSup(t, 10*time.Second, &friction)
	herdr := collabSup(t, store, sup)
	ws := collabWorkspace(t, jobDir)
	_, id := makeCollab(t, ws, collabJobDir(t, store), "w1", "impl-1", "rev-1", "w1:p2", "w1:p3")
	team.rosterRows = collabRoster("impl-1", "rev-1")
	if err := syscall.Mkfifo(filepath.Join(ws, "last-report-impl-1"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	report := writeOrchestratorReport(t, "# report\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}
	exit, err := runWithin(t, sup)
	if err != nil || exit != 0 {
		t.Fatalf("Run = %d, %v, want 0", exit, err)
	}
	out := findColab(t, readColabs(t, jobDir), id)
	for _, p := range out.ParticipantReports {
		if p.Name == "impl-1" {
			t.Fatalf("participant reports = %+v, want no entry for the FIFO marker", out.ParticipantReports)
		}
	}
	requireWorkspaceClosed(t, herdr, team)
}

// Catches the mutation "the orchestrator's report is copied with
// os.ReadFile": a FIFO at the report path would block the supervisor
// instead of counting like a missing report.
func TestSuperviseFifoOrchestratorReportCountsAsMissing(t *testing.T) {
	var friction []string
	store, _, _, team, sup := nsSup(t, 10*time.Second, &friction)
	fifo := filepath.Join(t.TempDir(), "report.md")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	team.statusScript = []fakeStatus{{State: "done", Report: fifo}, {State: "done", Report: fifo}, {State: "done", Report: fifo}}
	exit, err := runWithin(t, sup)
	if err != nil || exit != ExitFailed {
		t.Fatalf("Run = %d, %v, want %d", exit, err, ExitFailed)
	}
	st := supState(t, store)
	if st.Status != StatusFailed || st.Motivo == nil || !strings.Contains(*st.Motivo, "without a report") {
		t.Fatalf("state = %s motivo=%v, want failed without a report", st.Status, st.Motivo)
	}
	if _, err := os.Stat(fifo); err != nil {
		t.Fatal(err)
	}
}
