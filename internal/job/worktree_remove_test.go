package job

// worktreeRemoveFixture is one hermetic removal scenario, in the pattern
// of the prepare tests: a local bare remote with a main branch, a cloned
// checkout, and the job worktree <checkout>/.worktrees/job-<id> on the
// branch job/<id> pushed to the remote. The fixture's git commands run
// with a restricted env (their own HOME and configuration); the
// subprocesses that removeOwnedWorktree starts read the process env
// (platform.EnvFromOS), which the fixture isolates with t.Setenv so the
// real HOME and system git configuration never reach them.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

const (
	worktreeRemovalID     = "dja231"
	worktreeRemovalBranch = "job/" + worktreeRemovalID
)

type worktreeRemoveFixture struct {
	t        *testing.T
	root     string
	bare     string
	checkout string
	dir      string
	env      platform.Env
}

func newWorktreeRemoveFixture(t *testing.T) *worktreeRemoveFixture {
	return newWorktreeRemoveFixtureBranch(t, worktreeRemovalBranch)
}

// newWorktreeRemoveFixtureBranch stages the fixture with the worktree on
// the given branch, so a test can stage a worktree on the wrong branch.
func newWorktreeRemoveFixtureBranch(t *testing.T, branch string) *worktreeRemoveFixture {
	t.Helper()
	root := t.TempDir()
	var err error
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatalf("canonicalize the fixture root: %v", err)
	}
	global := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(global, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	// Isolate the env that removeOwnedWorktree's subprocesses inherit:
	// no system git configuration, a fixture HOME.
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("HOME", filepath.Join(root, "home"))
	f := &worktreeRemoveFixture{
		t:        t,
		root:     root,
		bare:     filepath.Join(root, "bare.git"),
		checkout: filepath.Join(root, "repos", "example-org", "example-repo"),
		dir:      filepath.Join(root, "repos", "example-org", "example-repo", ".worktrees", "job-"+worktreeRemovalID),
	}
	f.env = f.envFor(root)
	if out, err := f.git(f.root, "init", "-q", "--bare", f.bare); err != nil {
		t.Fatalf("git init --bare: %s: %v", out, err)
	}
	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "app.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "app.txt"},
		{"commit", "-q", "-m", "seed"},
		{"branch", "-M", "main"},
		{"remote", "add", "origin", f.bare},
		{"push", "-q", "origin", "main"},
	} {
		if out, err := f.git(seed, args...); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	// A fresh bare's HEAD points at the init default branch; pin it to
	// main so the remote default branch is deterministic on every git.
	if out, err := f.git(f.bare, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		t.Fatalf("git symbolic-ref: %s: %v", out, err)
	}
	if err := os.MkdirAll(filepath.Dir(f.checkout), 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := f.git(f.root, "clone", "-q", f.bare, f.checkout); err != nil {
		t.Fatalf("git clone: %s: %v", out, err)
	}
	if out, err := f.git(f.checkout, "worktree", "add", "--no-track", "-b", branch, f.dir, "refs/remotes/origin/main"); err != nil {
		t.Fatalf("git worktree add: %s: %v", out, err)
	}
	if out, err := f.git(f.dir, "push", "-q", "origin", branch); err != nil {
		t.Fatalf("git push the job branch: %s: %v", out, err)
	}
	return f
}

// env builds the hermetic env for the fixture's own git commands: a
// PATH resolving only the system git, its own HOME and XDG_CONFIG_HOME,
// an empty GIT_CONFIG_GLOBAL, GIT_CONFIG_NOSYSTEM=1, and the commit
// identity.
func (f *worktreeRemoveFixture) envFor(root string) platform.Env {
	gitDir := prepareGitOnlyDir(f.t)
	env := platform.Env{"PATH": gitDir}
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "conf")
	if err := os.MkdirAll(home, 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.MkdirAll(conf, 0o700); err != nil {
		f.t.Fatal(err)
	}
	env["HOME"] = home
	env["XDG_CONFIG_HOME"] = conf
	env["GIT_CONFIG_GLOBAL"] = filepath.Join(root, "gitconfig")
	env["GIT_CONFIG_NOSYSTEM"] = "1"
	env["GIT_AUTHOR_NAME"] = "fixture"
	env["GIT_AUTHOR_EMAIL"] = "fixture@example.org"
	env["GIT_COMMITTER_NAME"] = "fixture"
	env["GIT_COMMITTER_EMAIL"] = "fixture@example.org"
	return env
}

// git runs a real git with the fixture env and returns the combined
// output, bounded by a 60 s deadline.
func (f *worktreeRemoveFixture) git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.WaitDelay = time.Second
	cmd.Dir = dir
	cmd.Env = f.env.List()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// facts reads the report facts the supervisor would pass in: Clean as
// given, Head the worktree's head, HeadRemoto the remote job branch head.
func (f *worktreeRemoveFixture) facts(clean bool) ReportFacts {
	head, _ := f.git(f.dir, "rev-parse", "HEAD")
	remote, _ := f.git(f.checkout, "ls-remote", "origin", "refs/heads/"+worktreeRemovalBranch)
	remoteFields := strings.Fields(remote)
	headRemoto := ""
	if len(remoteFields) > 0 {
		headRemoto = remoteFields[0]
	}
	return ReportFacts{Clean: clean, Head: strings.TrimSpace(head), HeadRemoto: headRemoto}
}

// registrations counts the worktree registrations under the checkout's
// .git/worktrees, a plain filesystem read (no git, so nothing prunes).
func (f *worktreeRemoveFixture) registrations() int {
	entries, err := os.ReadDir(filepath.Join(f.checkout, ".git", "worktrees"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		f.t.Fatalf("read the worktree registrations: %v", err)
	}
	return len(entries)
}

// branchExists reports whether the job branch still exists at the
// checkout.
func (f *worktreeRemoveFixture) branchExists() bool {
	_, err := f.git(f.checkout, "rev-parse", "--verify", "--quiet", "refs/heads/"+worktreeRemovalBranch)
	return err == nil
}

func TestWorktreeRemoveCleanAndPushed(t *testing.T) {
	f := newWorktreeRemoveFixture(t)
	removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(true))
	if !removed || motivo != "" {
		t.Fatalf("removeOwnedWorktree = %v %q, want removed with the empty motivo", removed, motivo)
	}
	if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
		t.Fatalf("the worktree dir %s still exists (stat err %v)", f.dir, err)
	}
	if !f.branchExists() {
		t.Fatalf("the branch job/%s was deleted (it must stay)", worktreeRemovalID)
	}
	if f.registrations() != 0 {
		t.Fatalf("worktree registrations = %d, want 0 (the registration is cleaned by git)", f.registrations())
	}
	status, _ := f.git(f.checkout, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("the checkout shows a dirty status: %q", status)
	}
}

func TestWorktreeRemoveDirty(t *testing.T) {
	t.Run("a modified tracked file is dirty", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		path := filepath.Join(f.dir, "app.txt")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, []byte("dirty\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(false))
		if removed || motivo != WorktreeRemoveDirty {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveDirty)
		}
		if _, err := os.Stat(f.dir); err != nil {
			t.Fatalf("the worktree dir was touched (stat %v)", err)
		}
	})
	t.Run("an untracked file is dirty", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		if err := os.WriteFile(filepath.Join(f.dir, "stray.txt"), []byte("stray\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(false))
		if removed || motivo != WorktreeRemoveDirty {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveDirty)
		}
		if _, err := os.Stat(f.dir); err != nil {
			t.Fatalf("the worktree dir was touched (stat %v)", err)
		}
	})
}

func TestWorktreeRemoveNotPushed(t *testing.T) {
	f := newWorktreeRemoveFixture(t)
	if err := os.WriteFile(filepath.Join(f.dir, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := f.git(f.dir, "add", "new.txt"); err != nil {
		t.Fatalf("git add: %s: %v", out, err)
	}
	if out, err := f.git(f.dir, "commit", "-q", "-m", "new"); err != nil {
		t.Fatalf("git commit: %s: %v", out, err)
	}
	// The local head moved past the remote job branch head.
	facts := f.facts(true)
	if facts.Head == facts.HeadRemoto {
		t.Fatalf("fixture: head %q equals the remote head (the setup must leave an unpushed commit)", facts.Head)
	}
	removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, facts)
	if removed || motivo != WorktreeRemoveNotPushed {
		t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveNotPushed)
	}
	if _, err := os.Stat(f.dir); err != nil {
		t.Fatalf("the worktree dir was touched (stat %v)", err)
	}
}

func TestWorktreeRemoveCheckFailed(t *testing.T) {
	f := newWorktreeRemoveFixture(t)
	t.Run("facts without the head fact are check_failed", func(t *testing.T) {
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, ReportFacts{})
		if removed || motivo != WorktreeRemoveCheckFailed {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveCheckFailed)
		}
		if _, err := os.Stat(f.dir); err != nil {
			t.Fatalf("the worktree dir was touched (stat %v)", err)
		}
	})
	t.Run("a missing head wins over a dirty tree", func(t *testing.T) {
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, ReportFacts{Clean: false})
		if removed || motivo != WorktreeRemoveCheckFailed {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveCheckFailed)
		}
	})
}

func TestWorktreeRemoveNotOwned(t *testing.T) {
	t.Run("a dir that is not the job worktree path is not_owned", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		for name, dir := range map[string]string{
			"another job dir":      filepath.Join(f.checkout, ".worktrees", "job-other"),
			"the checkout itself":  f.checkout,
			"outside the checkout": filepath.Join(f.root, "elsewhere"),
		} {
			removed, motivo := removeOwnedWorktree(f.checkout, dir, worktreeRemovalID, f.facts(true))
			if removed || motivo != WorktreeRemoveNotOwned {
				t.Fatalf("%s: removeOwnedWorktree = %v %q, want false %q", name, removed, motivo, WorktreeRemoveNotOwned)
			}
		}
		// The id that does not match the dir's own name.
		if removed, motivo := removeOwnedWorktree(f.checkout, f.dir, "other9", f.facts(true)); removed || motivo != WorktreeRemoveNotOwned {
			t.Fatalf("mismatched id: removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveNotOwned)
		}
		if _, err := os.Stat(f.dir); err != nil {
			t.Fatalf("the worktree dir was touched (stat %v)", err)
		}
	})
	t.Run("a relative dir is not_owned", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		removed, motivo := removeOwnedWorktree(f.checkout, "not/absolute", worktreeRemovalID, f.facts(true))
		if removed || motivo != WorktreeRemoveNotOwned {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveNotOwned)
		}
	})
	t.Run("a worktree on the wrong branch is not_owned", func(t *testing.T) {
		f := newWorktreeRemoveFixtureBranch(t, "job/other")
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(true))
		if removed || motivo != WorktreeRemoveNotOwned {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveNotOwned)
		}
		if _, err := os.Stat(f.dir); err != nil {
			t.Fatalf("the worktree dir was touched (stat %v)", err)
		}
	})
	t.Run("a file at the job worktree path is not_owned", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		if err := os.RemoveAll(f.dir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.dir, []byte("a file, not a worktree\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(true))
		if removed || motivo != WorktreeRemoveNotOwned {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveNotOwned)
		}
	})
	t.Run("a plain directory at the job worktree path is not_owned", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		if err := os.RemoveAll(f.dir); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(f.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(true))
		if removed || motivo != WorktreeRemoveNotOwned {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveNotOwned)
		}
	})
	t.Run("a broken symlink at the job worktree path is not_owned", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		if err := os.RemoveAll(f.dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(f.root, "gone"), f.dir); err != nil {
			t.Fatal(err)
		}
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(true))
		if removed || motivo != WorktreeRemoveNotOwned {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveNotOwned)
		}
	})
}

func TestWorktreeRemoveConverges(t *testing.T) {
	t.Run("a second call after the removal converges", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		if removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(true)); !removed || motivo != "" {
			t.Fatalf("first removal = %v %q, want removed", removed, motivo)
		}
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(true))
		if !removed || motivo != "" {
			t.Fatalf("second removal = %v %q, want removed with the empty motivo", removed, motivo)
		}
		if !f.branchExists() {
			t.Fatalf("the branch job/%s was deleted (it must stay)", worktreeRemovalID)
		}
	})
	t.Run("an already deleted dir with the registration still there converges", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		if f.registrations() != 1 {
			t.Fatalf("fixture: registrations = %d, want 1", f.registrations())
		}
		if err := os.RemoveAll(f.dir); err != nil {
			t.Fatal(err)
		}
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, ReportFacts{})
		if !removed || motivo != "" {
			t.Fatalf("removeOwnedWorktree = %v %q, want removed with the empty motivo", removed, motivo)
		}
		// Nothing outside the dir is touched: the leftover registration
		// stays for git's own prune, and the branch stays.
		if f.registrations() != 1 {
			t.Fatalf("registrations = %d, want 1 (the function must not prune)", f.registrations())
		}
		if !f.branchExists() {
			t.Fatalf("the branch job/%s was deleted (it must stay)", worktreeRemovalID)
		}
		// The convergence is stable on a further call.
		removed, motivo = removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(true))
		if !removed || motivo != "" {
			t.Fatalf("further removal = %v %q, want removed with the empty motivo", removed, motivo)
		}
	})
}

// TestWorktreeRemoveHeldDirectory: git removes the job's work and its
// registration but cannot delete a directory still held (on Windows, the
// job's root pane has it as its current directory). The unregistered
// empty directory is the removal done, and a later call deletes it.
func TestWorktreeRemoveHeldDirectory(t *testing.T) {
	t.Run("an unregistered empty directory left by git counts as removed", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		facts := f.facts(true)
		release := holdWorktreeDir(t, f.dir)
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, facts)
		if !removed || motivo != "" {
			t.Fatalf("removeOwnedWorktree = %v %q, want removed with the empty motivo", removed, motivo)
		}
		if f.registrations() != 0 {
			t.Fatalf("worktree registrations = %d, want 0 (git removed it)", f.registrations())
		}
		if entries, err := os.ReadDir(f.dir); err == nil && len(entries) != 0 {
			t.Fatalf("the held directory keeps %d entries, want none", len(entries))
		}
		if !f.branchExists() {
			t.Fatalf("the branch job/%s was deleted (it must stay)", worktreeRemovalID)
		}
		release()
		removed, motivo = removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, ReportFacts{})
		if !removed || motivo != "" {
			t.Fatalf("second call = %v %q, want removed with the empty motivo", removed, motivo)
		}
		if _, err := os.Lstat(f.dir); !os.IsNotExist(err) {
			t.Fatalf("the leftover directory still exists after the hold ended (lstat err %v)", err)
		}
	})
	t.Run("content that appears before the leftover's deletion refuses the convergence", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		facts := f.facts(true)
		if err := os.RemoveAll(f.dir); err != nil {
			t.Fatal(err)
		}
		if out, err := f.git(f.checkout, "worktree", "prune"); err != nil {
			t.Fatalf("git worktree prune: %s: %v", out, err)
		}
		if err := os.MkdirAll(f.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		old := removeLeftoverDir
		removeLeftoverDir = func(dir string) error {
			// A process still in the directory writes a file between the
			// emptiness check and the deletion.
			if err := os.WriteFile(filepath.Join(dir, "late.txt"), []byte("late\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return old(dir)
		}
		t.Cleanup(func() { removeLeftoverDir = old })
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, facts)
		if removed || motivo != WorktreeRemoveNotOwned {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveNotOwned)
		}
		if _, err := os.Stat(filepath.Join(f.dir, "late.txt")); err != nil {
			t.Fatalf("the late file was touched: %v", err)
		}
	})
	t.Run("an unregistered directory that is not empty stays not_owned", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		if err := os.RemoveAll(f.dir); err != nil {
			t.Fatal(err)
		}
		if out, err := f.git(f.checkout, "worktree", "prune"); err != nil {
			t.Fatalf("git worktree prune: %s: %v", out, err)
		}
		if err := os.MkdirAll(f.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.dir, "keep.txt"), []byte("mine\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, f.facts(true))
		if removed || motivo != WorktreeRemoveNotOwned {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveNotOwned)
		}
		if _, err := os.Stat(filepath.Join(f.dir, "keep.txt")); err != nil {
			t.Fatalf("the file in the directory was touched: %v", err)
		}
	})
}

// TestHerdrPanes pins the herdrCLI.Panes contract against the package's
// fake herdr: the exact argv `pane list --workspace <id>`, the rows read
// from result.panes by pane_id and revision alone, the List-like error
// handling (exit 4 on failure or unreadable rows), and the Close-like id
// refusal (exit 2 before anything runs).
func TestHerdrPanes(t *testing.T) {
	wantArgv := []string{"pane", "list", "--workspace", "w9"}
	t.Run("the rows are read from result.panes by pane_id and revision", func(t *testing.T) {
		stdout := `{"id":"cli:pane:list","result":{"panes":[` +
			`{"agent":"claude","cwd":"/w","focused":false,"pane_id":"w9:p1","revision":9,"tab_id":"w9:t1","terminal_title":"a","workspace_id":"w9"},` +
			`{"agent":"pi","pane_id":"w9:p2","revision":45}]}}`
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: wantArgv, Stdout: stdout}})
		rows, err := f.cli().Panes("w9")
		if err != nil {
			t.Fatalf("Panes: %v", err)
		}
		want := []herdrPane{{PaneID: "w9:p1", Revision: 9}, {PaneID: "w9:p2", Revision: 45}}
		if len(rows) != len(want) {
			t.Fatalf("Panes = %+v, want %+v", rows, want)
		}
		for i := range want {
			if rows[i] != want[i] {
				t.Fatalf("Panes[%d] = %+v, want %+v", i, rows[i], want[i])
			}
		}
		calls := f.calls()
		if len(calls) != 1 || !sameArgv(calls[0].Argv, wantArgv) {
			t.Fatalf("herdr calls = %v, want exactly %v", calls, wantArgv)
		}
	})
	t.Run("an empty list is no rows", func(t *testing.T) {
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: wantArgv, Stdout: `{"result":{"panes":[]}}`}})
		rows, err := f.cli().Panes("w9")
		if err != nil || len(rows) != 0 {
			t.Fatalf("Panes = %+v, %v; want no rows and no error", rows, err)
		}
	})
	t.Run("an unreadable list is exit 4", func(t *testing.T) {
		for name, stdout := range map[string]string{
			"not json":             "not json",
			"no result":            `{"id":"cli:x"}`,
			"result array":         `{"result":[]}`,
			"no panes":             `{"result":{}}`,
			"panes not array":      `{"result":{"panes":{}}}`,
			"row without pane id":  `{"result":{"panes":[{"revision":1}]}}`,
			"row with empty id":    `{"result":{"panes":[{"pane_id":"","revision":1}]}}`,
			"row without revision": `{"result":{"panes":[{"pane_id":"w9:p1"}]}}`,
			"string revision":      `{"result":{"panes":[{"pane_id":"w9:p1","revision":"1"}]}}`,
			"negative revision":    `{"result":{"panes":[{"pane_id":"w9:p1","revision":-1}]}}`,
			"fractional revision":  `{"result":{"panes":[{"pane_id":"w9:p1","revision":1.5}]}}`,
		} {
			t.Run(name, func(t *testing.T) {
				f := newHerdrFixture(t, []fakecli.Rule{{Argv: wantArgv, Stdout: stdout}})
				rows, err := f.cli().Panes("w9")
				wantExit(t, err, ExitHerdr, "job: herdr pane list returned an unknown result")
				if rows != nil {
					t.Fatalf("Panes rows = %+v, want nil", rows)
				}
			})
		}
	})
	t.Run("a non-zero exit is exit 4", func(t *testing.T) {
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: wantArgv, Code: 1, Stdout: `{"result":{"panes":[]}}`}})
		_, err := f.cli().Panes("w9")
		wantExit(t, err, ExitHerdr, "job: herdr pane list failed")
	})
	t.Run("an invalid workspace id refuses with code 2 without running anything", func(t *testing.T) {
		ids := []string{"", "../escape", ".", " w2", "w2."}
		for _, id := range ids {
			t.Run(strconv.Quote(id), func(t *testing.T) {
				f := newHerdrFixture(t, nil)
				_, err := f.cli().Panes(id)
				wantExit(t, err, ExitUsage, "job: invalid workspace id")
				if calls := f.calls(); len(calls) != 0 {
					t.Fatalf("herdr calls = %v, want none", calls)
				}
			})
		}
	})
}

// TestWorktreeRemoveRechecksTheLiveTree: the facts are read before the
// job's agents are released, so the removal reads the live tree again —
// a commit made after the facts keeps the worktree as not_pushed, and a
// file left after them keeps it as dirty.
func TestWorktreeRemoveRechecksTheLiveTree(t *testing.T) {
	t.Run("a commit after the facts is not_pushed", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		facts := f.facts(true)
		if out, err := f.git(f.dir, "commit", "-q", "--allow-empty", "-m", "late"); err != nil {
			t.Fatalf("late commit: %s: %v", out, err)
		}
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, facts)
		if removed || motivo != WorktreeRemoveNotPushed {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveNotPushed)
		}
		if _, err := os.Stat(f.dir); err != nil {
			t.Fatalf("the worktree was touched: %v", err)
		}
	})
	t.Run("a file left after the facts is dirty", func(t *testing.T) {
		f := newWorktreeRemoveFixture(t)
		facts := f.facts(true)
		if err := os.WriteFile(filepath.Join(f.dir, "late.txt"), []byte("late\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		removed, motivo := removeOwnedWorktree(f.checkout, f.dir, worktreeRemovalID, facts)
		if removed || motivo != WorktreeRemoveDirty {
			t.Fatalf("removeOwnedWorktree = %v %q, want false %q", removed, motivo, WorktreeRemoveDirty)
		}
		if _, err := os.Stat(filepath.Join(f.dir, "late.txt")); err != nil {
			t.Fatalf("the late file was touched: %v", err)
		}
	})
}
