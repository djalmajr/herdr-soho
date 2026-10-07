package collaboration

import (
	"os"
	"path/filepath"
	"testing"
)

func submitFixtureEvent(t *testing.T, s Store, a Assignment, actor Participant, event string) PreparedEvent {
	t.Helper()
	revision, err := Fingerprint(a.Author.Cwd, a.Paths)
	if err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(t.TempDir(), "report.md")
	body := "report body\n"
	if event == "approved" {
		body = "findings: 0 (P0 0, P1 0, P2 0, P3 0) | verdict: pass\n"
	}
	if err := os.WriteFile(report, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareEvent(s, EventRequest{AssignmentID: a.ID, Actor: actor, Type: event, Report: report, Revision: revision.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestReviewCycleRequiresFreshVersionAndFinalization(t *testing.T) {
	s, a := assignmentFixture(t)
	ready := submitFixtureEvent(t, s, a, a.Author, "ready")
	a, err := CompleteDelivery(s, ready, DeliveryResult{Status: "queued"})
	if err != nil || a.Phase != Reviewing {
		t.Fatalf("ready=%+v %v", a, err)
	}
	findings := submitFixtureEvent(t, s, a, a.Reviewer, "findings")
	a, err = CompleteDelivery(s, findings, DeliveryResult{Status: "received"})
	if err != nil || a.Phase != Fixing {
		t.Fatalf("findings=%+v %v", a, err)
	}
	if err := os.WriteFile(filepath.Join(a.Author.Cwd, "file.go"), []byte("corrected"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrected := submitFixtureEvent(t, s, a, a.Author, "corrected")
	a, err = CompleteDelivery(s, corrected, DeliveryResult{Status: "received"})
	if err != nil || a.Round != 1 || a.Phase != Reviewing {
		t.Fatalf("corrected=%+v %v", a, err)
	}
	approved := submitFixtureEvent(t, s, a, a.Reviewer, "approved")
	a, err = CompleteDelivery(s, approved, DeliveryResult{Status: "submitted"})
	if err != nil || a.Phase != AwaitingOrchestrator || !a.Active() {
		t.Fatalf("approved=%+v %v", a, err)
	}
	if err := os.WriteFile(filepath.Join(a.Author.Cwd, "file.go"), []byte("later edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finalize(a.ID, "accept"); err == nil {
		t.Fatal("stale approval accepted")
	}
	if _, err := s.Finalize(a.ID, "reject"); err != nil {
		t.Fatal(err)
	}
	final, err := s.Read(a.ID)
	if err != nil || final.Active() || final.Verdict != "reject" {
		t.Fatal("finalization did not close assignment")
	}
}

func TestReviewEventsRejectWrongActorAndOldRevision(t *testing.T) {
	s, a := assignmentFixture(t)
	ready := submitFixtureEvent(t, s, a, a.Author, "ready")
	a, err := CompleteDelivery(s, ready, DeliveryResult{Status: "received"})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []EventRequest{
		{AssignmentID: a.ID, Actor: a.Author, Type: "approved", Report: ready.Event.Report, Revision: a.Revision},
		{AssignmentID: a.ID, Actor: a.Reviewer, Type: "approved", Report: ready.Event.Report, Revision: "old"},
	} {
		if _, err := PrepareEvent(s, req); err == nil {
			t.Fatal("invalid review event accepted")
		}
	}
}

func TestFinalizationRequiresUnchangedRoundEvidence(t *testing.T) {
	for _, evidence := range []string{"ready", "approved"} {
		t.Run(evidence, func(t *testing.T) {
			s, a := assignmentFixture(t)
			ready := submitFixtureEvent(t, s, a, a.Author, "ready")
			a, err := CompleteDelivery(s, ready, DeliveryResult{Status: "submitted"})
			if err != nil {
				t.Fatal(err)
			}
			approved := submitFixtureEvent(t, s, a, a.Reviewer, "approved")
			a, err = CompleteDelivery(s, approved, DeliveryResult{Status: "submitted"})
			if err != nil {
				t.Fatal(err)
			}
			report := ready.Event.Report
			if evidence == "approved" {
				report = approved.Event.Report
			}
			if err := os.WriteFile(report, []byte("different evidence"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Finalize(a.ID, "accept"); err == nil {
				t.Fatal("changed evidence accepted")
			}
			if _, err := s.Finalize(a.ID, "reject"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFailedEventPublicationAllowsExplicitRetry(t *testing.T) {
	s, a := assignmentFixture(t)
	report := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(report, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Fingerprint(a.Author.Cwd, a.Paths)
	if err != nil {
		t.Fatal(err)
	}
	request := EventRequest{AssignmentID: a.ID, Actor: a.Author, Type: "ready", Report: report, Revision: r.Fingerprint}
	lock := filepath.Join(s.directory(), "state.lock")
	if err := os.WriteFile(lock, []byte("held"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareEvent(s, request); err == nil {
		t.Fatal("published through a held lock")
	}
	entries, err := os.ReadDir(filepath.Join(s.directory(), a.ID))
	if err != nil || len(entries) != 1 || entries[0].Name() != "assignment.json" {
		t.Fatalf("unpublished snapshot survived: %v %v", entries, err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareEvent(s, request)
	if err != nil || !prepared.Fresh || prepared.Assignment.Phase != Reviewing {
		t.Fatalf("retry failed: %+v %v", prepared, err)
	}
}

func TestOldEventReplayInNewRoundReturnsExplicitError(t *testing.T) {
	s, a := assignmentFixture(t)
	ready := submitFixtureEvent(t, s, a, a.Author, "ready")
	a, _ = CompleteDelivery(s, ready, DeliveryResult{Status: "received"})
	findings := submitFixtureEvent(t, s, a, a.Reviewer, "findings")
	a, _ = CompleteDelivery(s, findings, DeliveryResult{Status: "received"})
	corrected := submitFixtureEvent(t, s, a, a.Author, "corrected")
	a, _ = CompleteDelivery(s, corrected, DeliveryResult{Status: "received"})
	if _, err := PrepareEvent(s, EventRequest{AssignmentID: a.ID, Actor: a.Reviewer, Type: "findings", Report: findings.Event.Report, Revision: findings.Event.Revision}); err == nil {
		t.Fatal("old event silently suppressed a new round")
	}
}
