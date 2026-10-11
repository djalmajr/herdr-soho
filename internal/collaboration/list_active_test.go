package collaboration

import (
	"os"
	"path/filepath"
	"testing"
)

// listActiveFixture returns a store with two assignments (different
// participants, so both can stand) and ends the second one.
func listActiveFixture(t *testing.T) (Store, Assignment, Assignment) {
	t.Helper()
	s := Store{StateDir: t.TempDir()}
	root := t.TempDir()
	mk := func(author, reviewer, familyA, familyB string) Assignment {
		t.Helper()
		a := Assignment{
			Author:    Participant{Name: author, Pane: "ws:" + author, Role: "implementer", Family: familyA, Started: "one", Session: "s-" + author, Cwd: root},
			Reviewer:  Participant{Name: reviewer, Pane: "ws:" + reviewer, Role: "reviewer", Family: familyB, Started: "two", Session: "s-" + reviewer, Cwd: root},
			Workspace: "ws", Root: root, BriefHash: "brief", Paths: []string{"file.go"},
		}
		created, err := s.Create(a)
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	first := mk("author", "reviewer", "openai", "anthropic")
	second := mk("author2", "reviewer2", "google", "mistral")
	if _, err := s.Stop(second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finalize(second.ID, "reject"); err != nil {
		t.Fatal(err)
	}
	return s, first, second
}

func TestListActiveAbsentDirectory(t *testing.T) {
	s := Store{StateDir: filepath.Join(t.TempDir(), "missing")}
	active, unreadable, err := s.ListActive()
	if err != nil || active != nil || unreadable != nil {
		t.Fatalf("absent directory: active=%+v unreadable=%+v err=%v, want nil/nil/nil", active, unreadable, err)
	}
	// The read never creates the directory.
	if _, err := os.Stat(s.directory()); !os.IsNotExist(err) {
		t.Fatalf("ListActive created the directory: %v", err)
	}
}

func TestListActiveListsOnlyActiveAssignments(t *testing.T) {
	s, first, second := listActiveFixture(t)
	active, unreadable, err := s.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != first.ID || !active[0].Active() {
		t.Fatalf("active = %+v, want only %s", active, first.ID)
	}
	if len(unreadable) != 0 {
		t.Fatalf("unreadable = %+v, want none", unreadable)
	}
	// The ended assignment stays on disk but is not active.
	ended, err := s.Read(second.ID)
	if err != nil || ended.Active() || ended.Phase != Finished || ended.Verdict != "reject" {
		t.Fatalf("ended record = %+v err=%v, want finished/reject on disk", ended, err)
	}
}

func TestListActiveUnreadableRecordIsNotAnError(t *testing.T) {
	s, first, _ := listActiveFixture(t)
	// A second active assignment so the unreadable one is not the only one.
	other := Assignment{
		Author:    Participant{Name: "author3", Pane: "ws:author3", Role: "implementer", Family: "google", Started: "one", Session: "s-a3", Cwd: t.TempDir()},
		Reviewer:  Participant{Name: "reviewer3", Pane: "ws:reviewer3", Role: "reviewer", Family: "mistral", Started: "two", Session: "s-r3", Cwd: t.TempDir()},
		Workspace: "ws", Root: t.TempDir(), BriefHash: "brief", Paths: []string{"file.go"},
	}
	otherCreated, err := s.Create(other)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the record of the other assignment.
	file, err := s.Path(otherCreated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	active, unreadable, err := s.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v, want the per-record failure in unreadable", err)
	}
	if len(active) != 1 || active[0].ID != first.ID {
		t.Fatalf("active = %+v, want only the readable active assignment", active)
	}
	if len(unreadable) != 1 || unreadable[0].ID != otherCreated.ID || unreadable[0].Cause == "" {
		t.Fatalf("unreadable = %+v, want the corrupted id with a cause", unreadable)
	}
}

func TestListActiveIgnoresForeignEntries(t *testing.T) {
	s, first, _ := listActiveFixture(t)
	// A plain file and a directory that is not a collaboration id are
	// ignored, not errors.
	if err := os.WriteFile(filepath.Join(s.directory(), "stray.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.directory(), "not-an-id"), 0o700); err != nil {
		t.Fatal(err)
	}
	active, unreadable, err := s.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != first.ID || len(unreadable) != 0 {
		t.Fatalf("active=%+v unreadable=%+v, want the fixture assignment only", active, unreadable)
	}
}

// A record whose id does not match its own id field is unreadable, as in
// Read: the list names it, it does not fail the enumeration.
func TestListActiveMismatchedIDIsUnreadable(t *testing.T) {
	s, _, _ := listActiveFixture(t)
	created, err := s.Create(Assignment{
		Author:    Participant{Name: "author4", Pane: "ws:author4", Role: "implementer", Family: "google", Started: "one", Session: "s-a4", Cwd: t.TempDir()},
		Reviewer:  Participant{Name: "reviewer4", Pane: "ws:reviewer4", Role: "reviewer", Family: "mistral", Started: "two", Session: "s-r4", Cwd: t.TempDir()},
		Workspace: "ws", Root: t.TempDir(), BriefHash: "brief", Paths: []string{"file.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite the record with a mismatched id (bypassing the store's own
	// write, which would never publish that).
	file, _ := s.Path(created.ID)
	if err := os.WriteFile(file, []byte(`{"version":1,"id":"c-deadbeefdeadbeef"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, unreadable, err := s.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	found := false
	for _, u := range unreadable {
		if u.ID == created.ID && u.Cause != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unreadable = %+v, want the mismatched record named with a cause", unreadable)
	}
}
