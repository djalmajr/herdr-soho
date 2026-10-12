package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// TestSessionStateRootPath pins the state root a command looks its session
// file up under before the session is read (SessionStateRootPath): while no
// session sets state_dir it equals the full-load root (StateRootPath over
// LoadConfig); when the session file itself sets state_dir it stays at the
// pre-session root, so the circular bootstrap reads the session that sits
// under the root it resolved without the session; the user and project
// state_dir layers are honored, the defaults layer's state_dir is not, and
// HERDR_SOHO_DIR wins over every layer.
func TestSessionStateRootPath(t *testing.T) {
	// layers builds one hermetic scenario: a non-git project cwd (so the
	// project root is the cwd), an empty home with its own XDG config dir,
	// an empty global gitconfig, a restricted PATH that resolves only the
	// system git the root resolution runs, and a skill dir whose defaults
	// layer names a state_dir that StateDirSetting must ignore (the
	// defaults source is skipped).
	layers := func(t *testing.T) (env platform.Env, proj, root string) {
		t.Helper()
		root = t.TempDir()
		home := filepath.Join(root, "home")
		conf := filepath.Join(root, "conf")
		skill := filepath.Join(root, "skill")
		global := filepath.Join(root, "gitconfig")
		for _, dir := range []string{home, conf, skill} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(skill, "config.defaults"), []byte("state_dir="+filepath.Join(root, "defaults-state")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(global, []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}
		proj = filepath.Join(root, "proj")
		if err := os.MkdirAll(proj, 0o700); err != nil {
			t.Fatal(err)
		}
		env = platform.Env{
			"PATH":                 gitOnlyDir(t),
			"HOME":                 home,
			"XDG_CONFIG_HOME":      conf,
			"GIT_CONFIG_GLOBAL":    global,
			"GIT_CONFIG_NOSYSTEM":  "1",
			"HERDR_SOHO_SKILL_DIR": skill,
			"HERDR_WORKSPACE_ID":   "w1",
		}
		return env, proj, root
	}
	// writeUser sets the user layer at the environment's XDG config path.
	writeUser := func(t *testing.T, env platform.Env, body string) {
		t.Helper()
		file := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho", "config")
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// writeProject sets the project layer at the new config path.
	writeProject := func(t *testing.T, proj, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(proj, ".agents"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proj, ".agents", "herdr-soho.conf"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// writeSession sets the session file at <root>/<ws>/session.conf.
	writeSession := func(t *testing.T, root, ws, body string) {
		t.Helper()
		dir := filepath.Join(root, ws)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "session.conf"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// fullRoot is the root the complete load resolves: the value the
	// session's state_dir moves it to, or the pre-session root when no
	// session sets it.
	fullRoot := func(t *testing.T, env platform.Env, cwd string) string {
		t.Helper()
		cfg := LoadConfig(env, cwd)
		return StateRootPath(&cfg, env, cwd)
	}

	t.Run("equals the full-load root while no session sets state_dir", func(t *testing.T) {
		env, proj, _ := layers(t)
		// The defaults layer names a state_dir; StateDirSetting skips the
		// defaults source, so neither root may move to it. A pre-session
		// reader that honors the defaults source, or that drops a layer,
		// fails here.
		want := filepath.Join(proj, ".herdr-soho")
		if got := SessionStateRootPath(env, proj); got != want {
			t.Fatalf("pre-session root = %q, want %q", got, want)
		}
		if got := fullRoot(t, env, proj); got != want {
			t.Fatalf("full-load root = %q, want %q", got, want)
		}
	})

	t.Run("user config state_dir is honored by both roots", func(t *testing.T) {
		env, proj, root := layers(t)
		writeUser(t, env, "state_dir="+filepath.Join(root, "u-state")+"\n")
		want := filepath.Join(root, "u-state")
		// A pre-session reader that drops the user layer would return the
		// default root.
		if got := SessionStateRootPath(env, proj); got != want {
			t.Fatalf("pre-session root = %q, want %q", got, want)
		}
		if got := fullRoot(t, env, proj); got != want {
			t.Fatalf("full-load root = %q, want %q", got, want)
		}
	})

	t.Run("project .agents/herdr-soho.conf state_dir is honored by both roots", func(t *testing.T) {
		env, proj, _ := layers(t)
		writeProject(t, proj, "state_dir=proj-state\n")
		// A relative state_dir joins the project root; a pre-session
		// reader that drops the project layer would return the default.
		want := filepath.Join(proj, "proj-state")
		if got := SessionStateRootPath(env, proj); got != want {
			t.Fatalf("pre-session root = %q, want %q", got, want)
		}
		if got := fullRoot(t, env, proj); got != want {
			t.Fatalf("full-load root = %q, want %q", got, want)
		}
	})

	t.Run("session state_dir does not move the pre-session root", func(t *testing.T) {
		env, proj, root := layers(t)
		// The session file sits at the pre-session (default) root and
		// names another state_dir. If the pre-session reader also loaded
		// the session layer, it would bootstrap the very file it looks up:
		// the root would move to the session's state_dir. The full load
		// still moves: the session wins over the base layers there.
		writeSession(t, filepath.Join(proj, ".herdr-soho"), "w1",
			"state_dir="+filepath.Join(root, "session-state")+"\n"+
				"lane.build.roles=from-session\n")
		if got := fullRoot(t, env, proj); got != filepath.Join(root, "session-state") {
			t.Fatalf("full-load root = %q, want the session's state_dir %q", got, filepath.Join(root, "session-state"))
		}
		if got := SessionStateRootPath(env, proj); got != filepath.Join(proj, ".herdr-soho") {
			t.Fatalf("pre-session root = %q, want the default root (a reader that loads the session layer would return the session's state_dir)", got)
		}
		// The full load read the session from the pre-session root: its
		// key must carry the session source. If the lookup had run under
		// the post-session root, the file would not be there and the key
		// would be absent.
		cfg := LoadConfig(env, proj)
		e, ok := cfg.Entries["lane_build_roles"]
		if !ok || e.Source != "session" || e.Value != "from-session" {
			t.Fatalf("session entry lane_build_roles = %+v (ok %v), want {from-session session}", e, ok)
		}
	})

	t.Run("session at the user state_dir root does not move the pre-session root", func(t *testing.T) {
		env, proj, root := layers(t)
		// The pre-session root is the user layer's root; the session file
		// under it names yet another state_dir. The pre-session root must
		// stay at the user root, and the session must still be loaded from
		// there.
		userRoot := filepath.Join(root, "u-state")
		writeUser(t, env, "state_dir="+userRoot+"\n")
		writeSession(t, userRoot, "w1",
			"state_dir="+filepath.Join(root, "session-state")+"\n"+
				"lane.build.roles=from-session\n")
		if got := SessionStateRootPath(env, proj); got != userRoot {
			t.Fatalf("pre-session root = %q, want the user root %q", got, userRoot)
		}
		if got := fullRoot(t, env, proj); got != filepath.Join(root, "session-state") {
			t.Fatalf("full-load root = %q, want the session's state_dir", got)
		}
		cfg := LoadConfig(env, proj)
		e, ok := cfg.Entries["lane_build_roles"]
		if !ok || e.Source != "session" || e.Value != "from-session" {
			t.Fatalf("session entry lane_build_roles = %+v (ok %v), want {from-session session}", e, ok)
		}
	})

	t.Run("HERDR_SOHO_DIR wins over user, project and session", func(t *testing.T) {
		env, proj, root := layers(t)
		writeUser(t, env, "state_dir="+filepath.Join(root, "u-state")+"\n")
		writeProject(t, proj, "state_dir=proj-state\n")
		// With the override set, the session lookup itself runs under the
		// override; the session file there sets yet another root. Without
		// the override the roots would be the project's (the project layer
		// beats the user's) and the session's.
		override := filepath.Join(root, "env-dir")
		env["HERDR_SOHO_DIR"] = override
		writeSession(t, override, "w1", "state_dir="+filepath.Join(root, "session-state")+"\n")
		for name, got := range map[string]string{
			"pre-session": SessionStateRootPath(env, proj),
			"full-load":   fullRoot(t, env, proj),
		} {
			if got != override {
				t.Fatalf("%s root = %q, want the HERDR_SOHO_DIR override %q", name, got, override)
			}
		}
	})
}
