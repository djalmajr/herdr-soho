package collaboration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeClaimRecord lays one raw assignment.json under the store without
// the store's write path: the record is exactly the hostile bytes.
func writeClaimRecord(t *testing.T, s Store, id, body string) {
	t.Helper()
	dir := filepath.Join(s.StateDir, "collaboration", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assignment.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadClaim(t *testing.T) {
	s := Store{StateDir: t.TempDir()}
	root := t.TempDir()
	created, err := s.Create(Assignment{
		Author:    Participant{Name: "author", Pane: "ws:a", Role: "implementer", Family: "openai", Started: "one", Session: "s-a", Cwd: root},
		Reviewer:  Participant{Name: "reviewer", Pane: "ws:b", Role: "reviewer", Family: "anthropic", Started: "two", Session: "s-b", Cwd: root},
		Workspace: "w1", Root: root, BriefHash: "brief", Paths: []string{"file.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ws, r, err := s.ReadClaim(created.ID)
	if err != nil || ws != "w1" || r != root {
		t.Fatalf("ReadClaim = (%q, %q, %v), want the record's workspace and root", ws, r, err)
	}

	t.Run("an invalid field keeps the claim readable", func(t *testing.T) {
		// The full read refuses the record (version 2) but the raw claim
		// still proves the ownership.
		writeClaimRecord(t, s, "c-00000000000001ee", `{"version":2,"id":"c-00000000000001ee","workspace":"w1","root":"`+root+`"}`)
		if _, err := s.Read("c-00000000000001ee"); err == nil {
			t.Fatal("the full read accepted an invalid record")
		}
		ws, r, err := s.ReadClaim("c-00000000000001ee")
		if err != nil || ws != "w1" || r != root {
			t.Fatalf("ReadClaim = (%q, %q, %v), want the record's workspace and root", ws, r, err)
		}
	})

	t.Run("a non-JSON record has no claim", func(t *testing.T) {
		writeClaimRecord(t, s, "c-00000000000000ff", "{")
		if _, _, err := s.ReadClaim("c-00000000000000ff"); err == nil {
			t.Fatal("ReadClaim accepted a non-JSON record")
		}
	})

	t.Run("an overlong record is refused", func(t *testing.T) {
		// More than the 1 MiB limit: the bounded read stops at the limit
		// and the body is not a JSON object.
		writeClaimRecord(t, s, "c-00000000000002ff", strings.Repeat("a", 1<<20+1))
		if _, _, err := s.ReadClaim("c-00000000000002ff"); err == nil {
			t.Fatal("ReadClaim accepted an overlong record")
		}
	})

	t.Run("a padded record is refused even with a valid claim prefix", func(t *testing.T) {
		body := `{"version":1,"workspace":"w1","root":"` + root + `"}` + strings.Repeat("a", 1<<20)
		writeClaimRecord(t, s, "c-00000000000004ff", body)
		if _, _, err := s.ReadClaim("c-00000000000004ff"); err == nil {
			t.Fatal("ReadClaim accepted a padded record")
		}
	})

	t.Run("an invalid id is refused before any read", func(t *testing.T) {
		if _, _, err := s.ReadClaim("not-an-id"); err == nil {
			t.Fatal("ReadClaim accepted an invalid id")
		}
	})

	t.Run("a missing record is an error", func(t *testing.T) {
		if _, _, err := s.ReadClaim("c-00000000000003ff"); err == nil {
			t.Fatal("ReadClaim accepted a missing record")
		}
	})
}
