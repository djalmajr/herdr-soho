package job

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// lifecycleWalkTo moves a fresh job to the given status along allowed
// lifecycle edges.
var lifecycleWalkTo = map[string][]string{
	StatusAccepted:  {},
	StatusPreparing: {StatusPreparing},
	StatusRunning:   {StatusPreparing, StatusRunning},
	StatusBlocked:   {StatusPreparing, StatusRunning, StatusBlocked},
	StatusFinishing: {StatusPreparing, StatusRunning, StatusFinishing},
	StatusDone:      {StatusPreparing, StatusRunning, StatusFinishing, StatusDone},
}

// lifecycleState creates the job and moves it to the given status.
func lifecycleState(t *testing.T, s *Store, id, status string) {
	t.Helper()
	steps, ok := lifecycleWalkTo[status]
	if !ok {
		t.Fatalf("no walk to %s", status)
	}
	if _, err := s.Start(id, []byte(briefA)); err != nil {
		t.Fatal(err)
	}
	for _, to := range steps {
		if _, err := s.Transition(id, to); err != nil {
			t.Fatalf("transition to %s: %v", to, err)
		}
	}
}

// lifecycleRawState rewrites state.json directly (the same seam the CLI
// tests use) for statuses the lifecycle edges do not reach.
func lifecycleRawState(t *testing.T, root, id, status string) {
	t.Helper()
	body := fmt.Sprintf(`{"schema":1,"id":%q,"status":%q,"brief_sha256":"x","decisions_acked_seq":0,"motivo":null}`, id, status)
	if err := os.WriteFile(filepath.Join(root, "jobs", id, "state.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// failEvents returns the types and refs of the two events Fail appends:
// the failure and the terminal.
func failEvents(t *testing.T, root, id string) (failure, terminal Event) {
	t.Helper()
	events, err := readEvents(filepath.Join(root, "jobs", id))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 {
		t.Fatalf("events = %v, want at least the failure and the terminal", events)
	}
	return events[len(events)-2], events[len(events)-1]
}

func assertFailedReport(t *testing.T, root, id, motivo string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "jobs", id, "report.json"))
	if err != nil {
		t.Fatalf("report.json: %v", err)
	}
	var rep Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatalf("report.json: %v", err)
	}
	if rep.Status != StatusFailed || rep.Motivo == nil || *rep.Motivo != motivo {
		t.Fatalf("report = status %q motivo %v, want failed / %q", rep.Status, rep.Motivo, motivo)
	}
}

func TestLifecycleFailFromEachState(t *testing.T) {
	for _, status := range []string{StatusAccepted, StatusPreparing, StatusRunning, StatusBlocked, StatusFinishing} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			s := Open(root)
			lifecycleState(t, s, "job-1", status)
			st, err := s.Fail("job-1", "a gate failed", 19)
			if err != nil {
				t.Fatalf("Fail from %s: %v", status, err)
			}
			if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "a gate failed" {
				t.Fatalf("state = %q motivo %v", st.Status, st.Motivo)
			}
			failure, terminal := failEvents(t, root, "job-1")
			if failure.Tipo != "failure" || failure.Resumo != "a gate failed" ||
				failure.Refs["motivo"] != "a gate failed" {
				t.Fatalf("failure event = %+v", failure)
			}
			if terminal.Tipo != "terminal" || terminal.Refs["motivo"] != "a gate failed" ||
				terminal.Refs["exit"] != "19" {
				t.Fatalf("terminal event = %+v", terminal)
			}
			assertFailedReport(t, root, "job-1", "a gate failed")
		})
	}
}

func TestLifecycleFailIdempotentAndRefusals(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if _, err := s.Start("job-1", []byte(briefA)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Fail("job-1", "a gate failed", 19); err != nil {
		t.Fatal(err)
	}
	_, first := failEvents(t, root, "job-1")
	assertFailedReport(t, root, "job-1", "a gate failed")
	if _, err := os.Stat(filepath.Join(root, "jobs", "job-1", "report.json")); err != nil {
		t.Fatal(err)
	}

	// A retried Fail appends no second event and republishes the report.
	st, err := s.Fail("job-1", "again", 19)
	if err != nil {
		t.Fatalf("retried Fail: %v", err)
	}
	if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "a gate failed" {
		t.Fatalf("retry changed the state: %q motivo %v", st.Status, st.Motivo)
	}
	events, err := readEvents(filepath.Join(root, "jobs", "job-1"))
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Seq != first.Seq || last.Tipo != "terminal" {
		t.Fatalf("retry appended an event: %+v (want none after %+v)", last, first)
	}
	assertFailedReport(t, root, "job-1", "a gate failed")

	// Any other terminal or later status is refused with exit 2.
	if _, err := s.Start("job-2", []byte(briefEscaped)); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{StatusDone, StatusTimeout, StatusCanceled, StatusCollected, StatusClosed} {
		lifecycleRawState(t, root, "job-2", status)
		_, err := s.Fail("job-2", "x", 19)
		wantExit(t, err, ExitUsage, "job: transition "+status+" -> failed is not allowed")
	}
	_, err = s.Fail("nope", "x", 19)
	wantExit(t, err, ExitNotFound, "job: not found")
}

func TestLifecycleRecord(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	created, err := s.Start("job-1", []byte(briefA))
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Record("job-1", func(st *State) {
		st.Checkout = "/x/y"
		st.Dir = "/x/y/.worktrees/job-job-1"
		st.Branch = "job/job-1"
		st.Base = "main"
		st.BaseSHA = "abc123"
		st.Modo = "worktree"
		st.WorkspaceID = "w9"
		st.RootPane = "w9:p1"
		st.TimeoutMin = 120
		st.StartedAt = "2026-01-02T03:04:05-03:00"
		// A hostile update tries to touch the protected fields.
		st.ID = "other"
		st.Status = StatusDone
		st.BriefSHA256 = "deadbeef"
		st.DecisionsAckedSeq = 7
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.ID != "job-1" || st.Status != StatusAccepted || st.BriefSHA256 != created.BriefSHA256 || st.DecisionsAckedSeq != 0 {
		t.Fatalf("protected fields changed: %+v", st)
	}
	if st.Checkout != "/x/y" || st.Dir != "/x/y/.worktrees/job-job-1" || st.Branch != "job/job-1" ||
		st.Base != "main" || st.BaseSHA != "abc123" || st.Modo != "worktree" ||
		st.WorkspaceID != "w9" || st.RootPane != "w9:p1" || st.TimeoutMin != 120 ||
		st.StartedAt != "2026-01-02T03:04:05-03:00" {
		t.Fatalf("run facts not applied: %+v", st)
	}
	raw, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored State
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusAccepted || stored.Checkout != "/x/y" || stored.TimeoutMin != 120 || stored.Motivo != nil {
		t.Fatalf("state.json = %+v", stored)
	}
	_, err = s.Record("nope", func(*State) {})
	wantExit(t, err, ExitNotFound, "job: not found")
}

// TestLifecycleStateBytesUnchangedWithoutRunFacts guards the Phase 1
// state.json bytes: a job without run facts serializes exactly as before
// the optional fields landed.
func TestLifecycleStateBytesUnchangedWithoutRunFacts(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	st, err := s.Start("job-1", []byte(briefA))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{"schema":1,"id":"job-1","status":"accepted","brief_sha256":"%s","decisions_acked_seq":0,"motivo":null}
`, st.BriefSHA256)
	if string(raw) != want {
		t.Fatalf("state.json bytes = %s, want %s", raw, want)
	}
}
