package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestResolvedRoleKindConfigThenFrontmatter(t *testing.T) {
	root := t.TempDir()
	roles := filepath.Join(root, "roles")
	if err := os.MkdirAll(roles, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roles, "implementer.md"), []byte("---\nkind: claude\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{"HERDR_SOHO_ROLES": roles, "HERDR_SOHO_SKILL_DIR": filepath.Join(root, "skill")}
	ctx := &Config{Entries: map[string]ConfigEntry{}, Order: []string{}}
	t.Run("frontmatter is the fallback", func(t *testing.T) {
		if got := ResolvedRoleKind("implementer", ctx, env, root); got != "claude" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("config override wins and lane is not read", func(t *testing.T) {
		ctx.Entries["role_implementer_kind"] = ConfigEntry{Value: "codex", Source: "project"}
		ctx.Entries["lane_build_kind"] = ConfigEntry{Value: "grok", Source: "project"}
		if got := ResolvedRoleKind("implementer", ctx, env, root); got != "codex" {
			t.Fatalf("got %q", got)
		}
	})
}
