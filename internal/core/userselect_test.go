package core_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// TestLoadConfigForSelection: a selection replaces only the user layer —
// the selected base's current file, else its legacy file — and the config
// command names that file; the zero selection reads the environment's.
func TestLoadConfigForSelection(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	server := filepath.Join(root, "server-xdg")
	starter := filepath.Join(root, "starter base")
	for _, dir := range []string{cwd, filepath.Join(server, "herdr-soho"), filepath.Join(starter, "herdr-agents")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(server, "herdr-soho", "config"), []byte("max_workers=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(starter, "herdr-agents", "config")
	if err := os.WriteFile(legacy, []byte("max_workers=7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{"XDG_CONFIG_HOME": server, "HOME": filepath.Join(root, "home"), "USERPROFILE": filepath.Join(root, "home"), "APPDATA": server}
	show := func(ctx core.Config) string {
		var out bytes.Buffer
		old := platform.Stdout
		platform.Stdout = &out
		defer func() { platform.Stdout = old }()
		core.CmdConfig(&ctx, env, cwd)
		return out.String()
	}

	base := core.LoadConfigFor(env, cwd, core.UserSelection{})
	if got := base.Entries["max_workers"].Value; got != "2" {
		t.Fatalf("zero selection max_workers=%q", got)
	}
	if text := show(base); !strings.Contains(text, "user file:    "+filepath.Join(server, "herdr-soho", "config")+"\n") {
		t.Fatalf("zero selection config output:\n%s", text)
	}

	sel := core.UserSelection{Base: starter}
	chosen := core.LoadConfigFor(env, cwd, sel)
	if got := chosen.Entries["max_workers"].Value; got != "7" {
		t.Fatalf("selection max_workers=%q", got)
	}
	if text := show(chosen); !strings.Contains(text, "user file:    "+legacy+" (legacy)\n") {
		t.Fatalf("selection config output:\n%s", text)
	}
	if env["XDG_CONFIG_HOME"] != server {
		t.Fatal("the selection changed the environment")
	}

	current := filepath.Join(starter, "herdr-soho", "config")
	if err := os.MkdirAll(filepath.Dir(current), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("max_workers=5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chosen = core.LoadConfigFor(env, cwd, sel)
	if got := chosen.Entries["max_workers"].Value; got != "5" {
		t.Fatalf("selection with both files max_workers=%q", got)
	}
	if text := show(chosen); !strings.Contains(text, "user file:    "+current+"\n") {
		t.Fatalf("selection with both files config output:\n%s", text)
	}
}
