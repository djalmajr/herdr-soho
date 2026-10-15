package job

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// TestStartTransitionFailureLeavesWorkspaceOpen: a confirmed launch
// survives a failed step 13 transition: the start returns the original
// error, the job ends failed with the fixed storage message, the report
// records the workspace open with one cleanup event, and the Herdr
// workspace is never closed under the live supervisor.
func TestStartTransitionFailureLeavesWorkspaceOpen(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	stateRoot := t.TempDir()
	fake := startFake()
	var friction []string
	s := startStarter(t, f, fake, stateRoot, &friction)

	// The rename seam fails only the first save that would publish running
	// (the step 13 transition); the failure walk's own running save — the
	// second such save — passes, so the job still reaches failed.
	previous := renameAtomic
	var refusedRunningSave bool
	renameAtomic = func(from, to string) error {
		if filepath.Base(to) == "state.json" && !refusedRunningSave {
			if data, err := os.ReadFile(from); err == nil && bytes.Contains(data, []byte(`"status":"running"`)) {
				refusedRunningSave = true
				return errors.New("job: simulated rename failure")
			}
		}
		return previous(from, to)
	}
	t.Cleanup(func() { renameAtomic = previous })

	st, store, err := s.Start(startRequest("main", briefA))
	if err == nil {
		t.Fatalf("Start succeeded: %+v", st)
	}
	if store == nil {
		t.Fatal("no store returned")
	}

	// The job ends failed with the fixed storage message.
	snap, err := store.Snapshot("job-1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.State.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", snap.State.Status)
	}
	if snap.State.Motivo == nil || *snap.State.Motivo != "job: cannot record the job state" {
		t.Fatalf("motivo = %v, want the fixed storage message", snap.State.Motivo)
	}

	// The report records the workspace open and the log has exactly one
	// cleanup event (and one failure event: a single store.Fail).
	if got := readReportWorkspace(t, stateRoot); got != "open" {
		t.Fatalf("limpeza.workspace = %q, want open", got)
	}
	jobDir := filepath.Join(stateRoot, "jobs", "job-1")
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	cleanups, failures := 0, 0
	for _, event := range events {
		switch event.Tipo {
		case "cleanup":
			cleanups++
		case "failure":
			failures++
		}
	}
	if cleanups != 1 {
		t.Fatalf("cleanup events = %d, want exactly one", cleanups)
	}
	if failures != 1 {
		t.Fatalf("failure events = %d, want exactly one", failures)
	}

	// The fake Herdr recorded no workspace close: the confirmed
	// supervisor owns the workspace and closes it itself.
	if len(fake.CloseIDs) != 0 {
		t.Fatalf("CloseIDs = %v, want none", fake.CloseIDs)
	}
}
