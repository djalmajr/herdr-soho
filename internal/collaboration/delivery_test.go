package collaboration

import "testing"

func TestDuplicateEventNeverRequestsSecondDelivery(t *testing.T) {
	s, a := assignmentFixture(t)
	prepared := submitFixtureEvent(t, s, a, a.Author, "ready")
	if !prepared.Fresh {
		t.Fatal("first event not fresh")
	}
	repeated, err := PrepareEvent(s, EventRequest{AssignmentID: a.ID, Actor: a.Author, Type: "ready", Report: prepared.Event.Report, Revision: prepared.Event.Revision})
	if err != nil || repeated.Fresh || repeated.Event.Delivery != "pending" {
		t.Fatalf("retry=%+v %v", repeated, err)
	}
	a, err = CompleteDelivery(s, prepared, DeliveryResult{Status: "uncertain", Cause: "observer ended after submission"})
	if err != nil || a.Phase != Escalated || a.Events[0].Delivery != "uncertain" {
		t.Fatalf("uncertain=%+v %v", a, err)
	}
	repeated, err = PrepareEvent(s, EventRequest{AssignmentID: a.ID, Actor: a.Author, Type: "ready", Report: prepared.Event.Report, Revision: prepared.Event.Revision})
	if err != nil || repeated.Fresh || repeated.Event.Delivery != "uncertain" {
		t.Fatal("uncertain result enabled automatic resend")
	}
	if _, err := s.Finalize(a.ID, "accept"); err == nil {
		t.Fatal("uncertain delivery accepted")
	}
}

func TestLateDeliveryKeepsRejectedCollaborationFinished(t *testing.T) {
	s, a := assignmentFixture(t)
	ready := submitFixtureEvent(t, s, a, a.Author, "ready")
	a, _ = CompleteDelivery(s, ready, DeliveryResult{Status: "received"})
	approved := submitFixtureEvent(t, s, a, a.Reviewer, "approved")
	if _, err := s.Finalize(a.ID, "reject"); err != nil {
		t.Fatal(err)
	}
	a, err := CompleteDelivery(s, approved, DeliveryResult{Status: "uncertain", Cause: "late result"})
	if err != nil || a.Active() || a.Verdict != "reject" || a.Events[len(a.Events)-1].Delivery != "uncertain" {
		t.Fatalf("late result reopened final state: %+v %v", a, err)
	}
	if active, err := s.ActiveFor(a.Author.Pane); err != nil || active != nil {
		t.Fatal("finished collaboration still blocks participant")
	}
	if _, err := s.Create(a); err != nil {
		t.Fatal("cannot assign the released participants", err)
	}
}
