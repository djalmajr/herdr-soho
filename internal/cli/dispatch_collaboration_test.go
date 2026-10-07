package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDispatchKeepsActiveAssignmentContractAndRoles(t *testing.T) {
	for _, change := range []string{"role", "brief", "amend", "phase"} {
		t.Run(change, func(t *testing.T) {
			f, brief := collaborationFixture(t, false)
			a := startCollaboration(t, f, brief)
			args := []string{"dispatch", "author", brief, "--no-wait"}
			sd := f.stateDir(t)
			if change == "role" {
				args = append(args, "--role", "reviewer")
			}
			if change == "brief" {
				if err := os.WriteFile(brief, []byte("different contract"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if change == "amend" {
				if err := os.WriteFile(filepath.Join(sd, "last-report-author"), []byte("old report"), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--amend")
			}
			if change == "phase" {
				a = cliEvent(t, f, a, "ws:p1", "ready", "Ready")
			}
			code, _, stderr := f.run(t, f.env, args...)
			if code != 10 || !strings.Contains(stderr, a.ID) {
				t.Fatalf("active contract was replaced: %d %s", code, stderr)
			}
		})
	}
}
