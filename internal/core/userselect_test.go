package core_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
)

// TestLoadConfigForSelection: a selection replaces only the user layer —
// the selected base's soho file — and the config command names that file;
// the zero selection reads the environment's. A former herdr-soho file in
// the selected base is not read.
func TestLoadConfigForSelection(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	server := filepath.Join(root, "server-xdg")
	starter := filepath.Join(root, "starter base")
	for _, dir := range []string{cwd, filepath.Join(server, "soho"), filepath.Join(starter, "soho"), filepath.Join(starter, "herdr-soho")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(server, "soho", "config"), []byte("max_workers=2\n"), 0o600); err != nil {
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
	if text := show(base); !strings.Contains(text, "user file:    "+filepath.Join(server, "soho", "config")+"\n") {
		t.Fatalf("zero selection config output:\n%s", text)
	}

	// A former herdr-soho file in the selected base is not read.
	former := filepath.Join(starter, "herdr-soho", "config")
	if err := os.WriteFile(former, []byte("max_workers=7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	formerOnly := core.LoadConfigFor(env, cwd, core.UserSelection{Base: starter})
	if got := formerOnly.Entries["max_workers"]; got.Value == "7" || got.Source == "user" {
		t.Fatalf("former user config still read: max_workers=%#v", got)
	}
	if text := show(formerOnly); !strings.Contains(text, "user file:    "+filepath.Join(starter, "soho", "config")+"\n") || strings.Contains(text, " (legacy)") {
		t.Fatalf("former-only selection config output:\n%s", text)
	}
	if env["XDG_CONFIG_HOME"] != server {
		t.Fatal("the selection changed the environment")
	}

	current := filepath.Join(starter, "soho", "config")
	if err := os.WriteFile(current, []byte("max_workers=5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chosen := core.LoadConfigFor(env, cwd, core.UserSelection{Base: starter})
	if got := chosen.Entries["max_workers"].Value; got != "5" {
		t.Fatalf("selection with the soho file present max_workers=%q", got)
	}
	if text := show(chosen); !strings.Contains(text, "user file:    "+current+"\n") {
		t.Fatalf("selection config output:\n%s", text)
	}
}
