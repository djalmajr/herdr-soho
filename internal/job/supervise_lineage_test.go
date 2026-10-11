package job

import (
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// TestUseLaunchSelectionLineage: a bound supervisor hands the selection to
// its team commands only; the git sync and the Herdr adapter keep the
// environment as it was (no private name, the original generic selectors).
func TestUseLaunchSelectionLineage(t *testing.T) {
	root := t.TempDir()
	store := Open(root)
	if _, err := store.Start("job-1", []byte(supTestBrief)); err != nil {
		t.Fatal(err)
	}
	dir, err := Dir(root, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir()
	editState(t, dir, func(s *State) {
		s.Status, s.Dir, s.Branch, s.Base, s.Modo = StatusRunning, worktree, "job/job-1", "main", "worktree"
	})
	env := platform.Env{"XDG_CONFIG_HOME": "/server/xdg", "HOME": "/server/home", "USERPROFILE": "/server/home", "APPDATA": "/server/appdata", "PATH": "/usr/bin"}
	sup, err := NewSupervisor(store, "job-1", env, "/bin/herdr-soho", nil)
	if err != nil {
		t.Fatal(err)
	}
	sel := core.UserSelection{Base: "/starter/config", Home: "/starter/home"}
	sup.UseLaunchSelection(sel)
	if !sup.PrivateLaunch {
		t.Fatal("PrivateLaunch is not set")
	}
	ops, ok := sup.Ops.(selfCLI)
	if !ok {
		t.Fatalf("Ops is %T", sup.Ops)
	}
	if ops.Env[launchConfigBaseVar] != sel.Base || ops.Env[launchHomeVar] != sel.Home {
		t.Fatalf("team command env misses the selection: %v", ops.Env)
	}
	check := func(what string, got platform.Env) {
		t.Helper()
		for _, name := range []string{launchConfigBaseVar, launchHomeVar} {
			if _, ok := got[name]; ok {
				t.Fatalf("%s env carries %s", what, name)
			}
		}
		for name, value := range env {
			if got[name] != value {
				t.Fatalf("%s env %s=%q, want the original %q", what, name, got[name], value)
			}
		}
	}
	for name, value := range env {
		if ops.Env[name] != value {
			t.Fatalf("team command env %s=%q, want the original %q", name, ops.Env[name], value)
		}
	}
	if sup.Git == nil {
		t.Fatal("no git sync for a branch job")
	}
	check("git sync", sup.Git.Env)
	herdr, ok := sup.Herdr.(*herdrCLI)
	if !ok {
		t.Fatalf("Herdr is %T", sup.Herdr)
	}
	check("herdr", herdr.Env)
	check("input", env)
}
