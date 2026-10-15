package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

// TestApplyLegacyEnvKeepsAPresentNewName: SOHO_NOWRITE set to the empty
// string still decides, so writes stay allowed over a former
// HERDR_SOHO_NOWRITE=1; other names keep the bridge's rule, an empty or
// absent new value takes the former one.
func TestApplyLegacyEnvKeepsAPresentNewName(t *testing.T) {
	env := platform.Env{"SOHO_NOWRITE": "", "HERDR_SOHO_NOWRITE": "1", "SOHO_DIR": "", "HERDR_SOHO_DIR": "/tmp/legacy-state"}
	copied := ApplyLegacyEnv(env)
	if got := env["SOHO_NOWRITE"]; got != "" {
		t.Fatalf("SOHO_NOWRITE = %q, want the present empty value kept", got)
	}
	if got := env["SOHO_DIR"]; got != "/tmp/legacy-state" {
		t.Fatalf("SOHO_DIR = %q, want the former value copied over the empty new one", got)
	}
	if len(copied) != 1 || copied[0] != "HERDR_SOHO_DIR" {
		t.Fatalf("copied = %v, want only HERDR_SOHO_DIR", copied)
	}
	if Nowrite(env) {
		t.Fatal("Nowrite = true, want the present SOHO_NOWRITE to decide")
	}
}

// TestRoleDirsUsesOneRolesOverride: SOHO_ROLES, when present, is the only
// roles override; the former HERDR_SOHO_ROLES is used only while it is
// absent.
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
	if !contains(legacy, oldDir) {
		t.Fatalf("dirs with only the former name = %v, want %s", legacy, oldDir)
	}
	if file := RoleFile("reviewer", platform.Env{"SOHO_ROLES": newDir, "HERDR_SOHO_ROLES": oldDir, "SOHO_SKILL_DIR": skill}, cwd); file != filepath.Join(newDir, "reviewer.md") {
		t.Fatalf("RoleFile = %s, want the new override's reviewer", file)
	}
}
