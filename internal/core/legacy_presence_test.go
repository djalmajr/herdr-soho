package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

// TestRoleDirsUsesOneRolesOverride: SOHO_ROLES, when present, is the only
// roles override; the former SOHO_ROLES is no longer read.
func TestRoleDirsUsesOneRolesOverride(t *testing.T) {
	newDir, oldDir := t.TempDir(), t.TempDir()
	for _, dir := range []string{newDir, oldDir} {
		if err := os.WriteFile(filepath.Join(dir, "reviewer.md"), []byte("---\nkind: codex\n---\nbody\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	skill := filepath.Join("..", "..", "skills", "soho")
	contains := func(dirs []string, want string) bool {
		for _, dir := range dirs {
			if dir == want {
				return true
			}
		}
		return false
	}
	cwd := t.TempDir()
	both := RoleDirs(platform.Env{"SOHO_ROLES": newDir, "HERDR_SOHO_ROLES": oldDir, "SOHO_SKILL_DIR": skill}, cwd)
	if !contains(both, newDir) || contains(both, oldDir) {
		t.Fatalf("dirs with both names = %v, want %s and not %s", both, newDir, oldDir)
	}
	legacy := RoleDirs(platform.Env{"HERDR_SOHO_ROLES": oldDir, "SOHO_SKILL_DIR": skill}, cwd)
	if contains(legacy, oldDir) {
		t.Fatalf("dirs with only the former name still include %s: %v", oldDir, legacy)
	}
	if file := RoleFile("reviewer", platform.Env{"SOHO_ROLES": newDir, "HERDR_SOHO_ROLES": oldDir, "SOHO_SKILL_DIR": skill}, cwd); file != filepath.Join(newDir, "reviewer.md") {
		t.Fatalf("RoleFile = %s, want the new override's reviewer", file)
	}
}
