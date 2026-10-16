package core

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

// treeSnapshot hashes every file under root (directories as their type),
// byte for byte, so a refusal can be proved to have left the project
// untouched: .gitignore included.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			snap[path] = fmt.Sprintf("file %x", sha256.Sum256(data))
		} else {
			snap[path] = "type " + d.Type().String()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snap
}

// waitingFixture builds a git checkout whose state root is the default
// <root>/.soho: the git tree makes a .gitignore append detectable, and no
// SOHO_DIR/SOHO_STATE_DIR pins the state dir.
func waitingFixture(t *testing.T) (root, home string) {
	t.Helper()
	root = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved // git reports the resolved spelling (macOS /tmp)
	}
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	home = t.TempDir()
	return root, home
}

func TestFormerStateWaiting(t *testing.T) {
	ctx := &Config{Entries: map[string]ConfigEntry{}}
	home := t.TempDir()
	baseEnv := platform.Env{"HOME": home, "USERPROFILE": home}

	waiting := func(t *testing.T, env platform.Env, cwd string) (string, string, bool) {
		t.Helper()
		return FormerStateWaiting(ctx, env, cwd)
	}

	t.Run("true with the former dir and no soho dir", func(t *testing.T) {
		root, home := waitingFixture(t)
		if err := os.MkdirAll(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HOME": home, "USERPROFILE": home}
		former, target, got := waiting(t, env, root)
		if !got || former != filepath.Join(root, ".herdr-soho") || target != filepath.Join(root, ".soho") {
			t.Fatalf("waiting=%v former=%q target=%q", got, former, target)
		}
	})
	t.Run("true with an empty soho dir", func(t *testing.T) {
		root, home := waitingFixture(t)
		for _, dir := range []string{".herdr-soho", ".soho"} {
			if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		env := platform.Env{"HOME": home, "USERPROFILE": home}
		if _, _, got := waiting(t, env, root); !got {
			t.Fatal("an empty .soho dir still waits for the setup move")
		}
	})
	t.Run("false with a soho dir holding an entry", func(t *testing.T) {
		root, home := waitingFixture(t)
		for _, dir := range []string{".herdr-soho", ".soho/ws"} {
			if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		env := platform.Env{"HOME": home, "USERPROFILE": home}
		if _, _, got := waiting(t, env, root); got {
			t.Fatal("a .soho dir with an entry is in use: not waiting")
		}
	})
	t.Run("false with the soho dir as a symlink to an empty dir", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory symlinks need elevated privileges on Windows")
		}
		root, home := waitingFixture(t)
		if err := os.MkdirAll(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		real := t.TempDir()
		if err := os.MkdirAll(filepath.Join(real, "empty"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(real, "empty"), filepath.Join(root, ".soho")); err != nil {
			t.Skipf("cannot create the symlink: %v", err)
		}
		env := platform.Env{"HOME": home, "USERPROFILE": home}
		if _, _, got := waiting(t, env, root); got {
			t.Fatal("a symlinked .soho is not the empty target the migration moves into")
		}
	})
	t.Run("false with SOHO_DIR even when the pin resolves to the default", func(t *testing.T) {
		root, home := waitingFixture(t)
		if err := os.MkdirAll(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HOME": home, "USERPROFILE": home, "SOHO_DIR": filepath.Join(root, ".soho")}
		if _, _, got := waiting(t, env, root); got {
			t.Fatal("an explicit SOHO_DIR pin keeps the former dir, even at the default path")
		}
	})
	t.Run("false with SOHO_STATE_DIR", func(t *testing.T) {
		root, home := waitingFixture(t)
		if err := os.MkdirAll(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HOME": home, "USERPROFILE": home, "SOHO_STATE_DIR": filepath.Join(root, ".soho")}
		if _, _, got := waiting(t, env, root); got {
			t.Fatal("an explicit SOHO_STATE_DIR pin keeps the former dir")
		}
	})
	t.Run("false with state_dir in the project file", func(t *testing.T) {
		root, home := waitingFixture(t)
		if err := os.MkdirAll(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		agents := filepath.Join(root, ".agents")
		if err := os.MkdirAll(agents, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(agents, "soho.conf"), []byte("state_dir=.other\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// A loaded config so the project layer is the real source of state_dir.
		env := platform.Env{"HOME": home, "USERPROFILE": home, "SOHO_SKILL_DIR": t.TempDir(), "HERDR_WORKSPACE_ID": "ws", "PATH": baseEnv["PATH"]}
		loaded := LoadConfig(env, root)
		former, target, got := FormerStateWaiting(&loaded, env, root)
		if got || former != filepath.Join(root, ".herdr-soho") || target != filepath.Join(root, ".soho") {
			t.Fatalf("waiting=%v former=%q target=%q", got, former, target)
		}
	})
	t.Run("false without the former dir", func(t *testing.T) {
		root, home := waitingFixture(t)
		env := platform.Env{"HOME": home, "USERPROFILE": home}
		if _, _, got := waiting(t, env, root); got {
			t.Fatal("no former dir: nothing waits")
		}
	})
	t.Run("false with the former dir as a file", func(t *testing.T) {
		root, home := waitingFixture(t)
		if err := os.WriteFile(filepath.Join(root, ".herdr-soho"), []byte("not a dir\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HOME": home, "USERPROFILE": home}
		if _, _, got := waiting(t, env, root); got {
			t.Fatal("a regular file at the former path is not the state dir")
		}
	})
}

func TestStateDirRefusesTheWaitingFormerState(t *testing.T) {
	ctx := &Config{Entries: map[string]ConfigEntry{}}

	t.Run("the refusal exits 2 with the exact message and leaves the tree byte for byte", func(t *testing.T) {
		root, home := waitingFixture(t)
		if err := os.MkdirAll(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("# pre-existing\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		bin, log := herdrLogger(t, t.TempDir())
		env := platform.Env{"HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home}
		for key, value := range herdrLoggerEnv(t, bin) {
			env[key] = value
		}
		before := treeSnapshot(t, root)
		code, msg := stateDirExit(t, ctx, env, root)
		want := "the former state dir '" + filepath.Join(root, ".herdr-soho") + "' is waiting for 'soho setup': run 'soho setup' first; it moves it to '" + filepath.Join(root, ".soho") + "' (nothing was written)"
		if code != 2 || msg != want {
			t.Fatalf("exit=%d msg=%q want %q", code, msg, want)
		}
		if after := treeSnapshot(t, root); !reflect.DeepEqual(before, after) {
			t.Fatalf("project tree changed by the refusal: before=%v after=%v", before, after)
		}
		assertNoHerdrCalls(t, log)
	})
	t.Run("nowrite mode keeps today's read path", func(t *testing.T) {
		root, home := waitingFixture(t)
		if err := os.MkdirAll(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home, "SOHO_NOWRITE": "1"}
		dir := StateDir(ctx, env, root)
		if dir != filepath.Join(root, ".soho", "ws") {
			t.Fatalf("dir=%q", dir)
		}
		if _, err := os.Stat(filepath.Join(root, ".soho")); !os.IsNotExist(err) {
			t.Fatalf("nowrite created state: %v", err)
		}
	})
	t.Run("control: without the former dir the same StateDir creates the state", func(t *testing.T) {
		root, home := waitingFixture(t)
		env := platform.Env{"HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home}
		dir := StateDir(ctx, env, root)
		if dir != filepath.Join(root, ".soho", "ws") {
			t.Fatalf("dir=%q", dir)
		}
		for _, child := range []string{"briefs", "reports", "wait"} {
			if _, err := os.Stat(filepath.Join(dir, child)); err != nil {
				t.Fatalf("%s missing: %v", child, err)
			}
		}
	})
	t.Run("control: a filled soho dir is in use, the refusal stays off", func(t *testing.T) {
		root, home := waitingFixture(t)
		if err := os.MkdirAll(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, ".soho", "ws"), 0o700); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home}
		dir := StateDir(ctx, env, root)
		if dir != filepath.Join(root, ".soho", "ws") {
			t.Fatalf("dir=%q", dir)
		}
	})
}
