package dispatch

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// TestRolesNoDeleteRule guards the shipped roles' no-delete rule (DJA-258 +
// DJA-297): the role body the dispatch composes the prompt from must keep the
// rule that only the owner removes copies, worktrees, logs, reports and
// scratch folders, and must not keep the replaced copy-cleanup wording.
func TestRolesNoDeleteRule(t *testing.T) {
	const present = "Never delete, empty, overwrite or recreate a copy, worktree, log, report or scratch folder, including one you created"
	absent := []string{
		"Delete each copy",
		"the copy is yours to delete",
		"Before you delete a throwaway copy",
		"a directory you did not create in this slice",
	}
	skillDir := filepath.Join("..", "..", "skills", "soho")
	env := platform.Env{"HERDR_SOHO_SKILL_DIR": skillDir}
	goos := "posix"
	if runtime.GOOS == "windows" {
		goos = "win32"
	}
	// A cwd outside any project: core.RoleFile must resolve the shipped role
	// file, exactly what the dispatch composes the prompt from.
	cwd := t.TempDir()
	for _, role := range []string{"implementer", "tasker", "reviewer"} {
		t.Run(role, func(t *testing.T) {
			file := core.RoleFile(role, env, cwd)
			want := filepath.Join(skillDir, "roles", role+".md")
			if !SamePath(file, want, goos) {
				t.Fatalf("role %s resolved to %s, want the shipped %s", role, file, want)
			}
			body := core.RoleBody(file)
			if !strings.Contains(body, present) {
				t.Errorf("%s body misses the no-delete rule", role)
			}
			for _, phrase := range absent {
				if strings.Contains(body, phrase) {
					t.Errorf("%s body still says %q", role, phrase)
				}
			}
		})
	}
}
