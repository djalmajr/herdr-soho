package job

import (
	"testing"
)

// TestSuperviseFinalizeInvalidWorkspaceID pins the supervisor's close guard:
// a terminal finalize whose recorded workspace id is not one safe path
// segment calls no Herdr.Close, writes the fixed friction line, leaves the
// report workspace open, and records no WorkspaceClosed.
func TestSuperviseFinalizeInvalidWorkspaceID(t *testing.T) {
	f := newSupGitFixture(t, supGitGhRules)
	if _, err := f.store.Record(supGitID, func(st *State) { st.WorkspaceID = "../escape" }); err != nil {
		t.Fatal(err)
	}
	f.job.commitIn(t, "feat: first")
	if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
		t.Fatal(err)
	}
	if len(f.herdr.CloseIDs) != 0 {
		t.Fatalf("herdr close = %v, want none for the invalid id", f.herdr.CloseIDs)
	}
	rep := f.report(t)
	limpeza, ok := rep["limpeza"].(map[string]any)
	if !ok || limpeza["workspace"] != "open" {
		t.Fatalf("report limpeza = %v, want the open workspace", rep["limpeza"])
	}
	found := false
	for _, line := range *f.friction {
		if line == "job: the recorded workspace id is invalid; the workspace was not closed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("friction = %v, want the fixed invalid-id line", *f.friction)
	}
	st := f.state(t)
	if st.WorkspaceClosed {
		t.Fatal("WorkspaceClosed was recorded for the invalid id")
	}
	assertSyncPostconditions(t, f.job)
}
