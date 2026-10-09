package job

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// prepareFixture is one hermetic preparation scenario: a local bare remote
// with a main branch, a repos root under a canonical temp root, and an env
// whose PATH resolves a fake gh (dir first) and only the system git after
// it. Fixture git commands and Preparer.Env never touch the real HOME or
// git configuration.
type prepareFixture struct {
	t         *testing.T
	root      string
	bare      string
	reposRoot string
	fakeDir   string
	env       platform.Env
	machine   Machine
}

const (
	prepOrg  = "example-org"
	prepRepo = "example-repo"
	prepID   = "abc123"
)

func newPrepareFixture(t *testing.T, ghRules []fakecli.Rule) *prepareFixture {
	t.Helper()
	f := newPrepareBase(t)
	f.installGh(t, ghRules)
	return f
}

// newPrepareBase stages the bare remote, the env, and the machine without
// installing the fake gh yet, so a test can build rules from the fixture
// paths (f.checkout) before installing.
func newPrepareBase(t *testing.T) *prepareFixture {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("canonicalize the fixture root: %v", err)
	}
	f := &prepareFixture{
		t:         t,
		root:      root,
		bare:      filepath.Join(root, "bare.git"),
		reposRoot: filepath.Join(root, "repos"),
		machine:   Machine{Orgs: []string{prepOrg}, ReposRoot: filepath.Join(root, "repos")},
	}
	f.env = f.envFor("")
	if out, err := f.git(f.root, "init", "-q", "--bare", f.bare); err != nil {
		t.Fatalf("git init --bare: %s: %v", out, err)
	}
	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
		{"branch", "-M", "main"},
		{"remote", "add", "origin", f.bare},
		{"push", "-q", "origin", "main"},
	} {
		if out, err := f.git(seed, args...); err != nil {
			t.Fatalf("git %v: %s: %v", strings.Join(args, " "), out, err)
		}
	}
	// A fresh bare's HEAD points at the init default branch; pin it to
	// main so the remote default branch is deterministic on every git.
	if out, err := f.git(f.bare, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		t.Fatalf("git symbolic-ref: %s: %v", out, err)
	}
	return f
}

func (f *prepareFixture) installGh(t *testing.T, ghRules []fakecli.Rule) {
	t.Helper()
	f.fakeDir = t.TempDir()
	if _, err := fakecli.Install(t, f.fakeDir, "gh", ghRules); err != nil {
		t.Fatalf("install the fake gh: %v", err)
	}
	// The env's PATH puts the fake gh dir first; rebuild it now that the
	// fake dir exists so Prepare sees the fake, not the setup env.
	f.env = f.envFor(f.fakeDir)
}

// envFor builds the hermetic env: the fake gh dir on PATH when given (it
// must come first), the system git behind it, its own HOME and
// XDG_CONFIG_HOME, an empty GIT_CONFIG_GLOBAL, GIT_CONFIG_NOSYSTEM=1, and
// the commit identity.
func (f *prepareFixture) envFor(fakeDir string) platform.Env {
	t := f.t
	root := t.TempDir()
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "conf")
	global := filepath.Join(root, "gitconfig")
	for _, dir := range []string{home, conf} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(global, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	gitDir := prepareGitOnlyDir(t)
	var list []string
	if fakeDir != "" {
		list = fakecli.Env(nil, fakeDir, fakecli.EnvOptions{SystemPath: gitDir})
	} else {
		list = []string{"PATH=" + gitDir}
	}
	env := platform.Env{}
	for _, item := range list {
		key, value, ok := strings.Cut(item, "=")
		if ok && key != "" {
			env[key] = value
		}
	}
	env["HOME"] = home
	env["XDG_CONFIG_HOME"] = conf
	env["GIT_CONFIG_GLOBAL"] = global
	env["GIT_CONFIG_NOSYSTEM"] = "1"
	env["GIT_AUTHOR_NAME"] = "fixture"
	env["GIT_AUTHOR_EMAIL"] = "fixture@example.org"
	env["GIT_COMMITTER_NAME"] = "fixture"
	env["GIT_COMMITTER_EMAIL"] = "fixture@example.org"
	return env
}

// prepareGitOnlyDir returns a directory holding only a link to the system
// git: the restricted PATH tail the fixture envs carry (git is a known
// native binary; nothing else is resolvable on the PATH).
func prepareGitOnlyDir(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("locate the system git: %v", err)
	}
	name := "git"
	if runtime.GOOS == "windows" {
		name = "git.exe"
	}
	dir := t.TempDir()
	target := filepath.Join(dir, name)
	if err := os.Symlink(path, target); err != nil {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("reading the system git: %v", readErr)
		}
		if writeErr := os.WriteFile(target, data, 0o755); writeErr != nil {
			t.Fatalf("copying the system git: %v", writeErr)
		}
	}
	return dir
}

// git runs a real git with the fixture env and returns the combined output,
// bounded by a 60 s deadline.
func (f *prepareFixture) git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.WaitDelay = time.Second
	cmd.Dir = dir
	cmd.Env = f.env.List()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f *prepareFixture) checkout() string {
	return filepath.Join(f.reposRoot, prepOrg, prepRepo)
}

// cloneCheckout stages an existing checkout by cloning the bare remote.
func (f *prepareFixture) cloneCheckout() string {
	dest := f.checkout()
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if out, err := f.git(f.root, "clone", "-q", f.bare, dest); err != nil {
		f.t.Fatalf("git clone: %s: %v", out, err)
	}
	return dest
}

// gitClone is the Clone test seam: it runs `git clone <bare> <dest>`.
func (f *prepareFixture) gitClone() func(org, repo, dest string) error {
	return func(org, repo, dest string) error {
		if _, err := f.git(filepath.Dir(dest), "clone", "-q", f.bare, dest); err != nil {
			return err
		}
		return nil
	}
}

func (f *prepareFixture) preparer() Preparer {
	return Preparer{Machine: f.machine, Env: f.env}
}

func (f *prepareFixture) req(base, mode string) PrepareRequest {
	req := PrepareRequest{ID: prepID, Org: prepOrg, Repo: prepRepo, Base: base, Mode: "worktree"}
	if mode != "" {
		req.Mode = mode
	}
	return req
}

// ghCalls reads the fake gh call log; a missing log means no gh ran.
func (f *prepareFixture) ghCalls() []fakecli.Call {
	calls, err := fakecli.ReadCalls(filepath.Join(f.fakeDir, "gh.calls.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		f.t.Fatalf("read the gh call log: %v", err)
	}
	return calls
}

func wantExit(t *testing.T, err error, code int, msg string) {
	t.Helper()
	exit, ok := err.(*ExitError)
	if !ok || exit == nil {
		t.Fatalf("error = %v (%T), want *ExitError code %d", err, err, code)
	}
	if exit.Code != code {
		t.Fatalf("exit code = %d, want %d (%v)", exit.Code, code, err)
	}
	if msg != "" && exit.Msg != msg {
		t.Fatalf("message = %q, want %q", exit.Msg, msg)
	}
}

// wantPlatformExit asserts Machine.Admit's reused error shape: a
// *platform.ExitError with the given code and message.
func wantPlatformExit(t *testing.T, err error, code int, msg string) {
	t.Helper()
	var exit *platform.ExitError
	if !errors.As(err, &exit) || exit == nil {
		t.Fatalf("error = %v (%T), want *platform.ExitError code %d", err, err, code)
	}
	if exit.Code != code {
		t.Fatalf("exit code = %d, want %d (%v)", exit.Code, code, err)
	}
	if msg != "" && exit.Msg != msg {
		t.Fatalf("message = %q, want %q", exit.Msg, msg)
	}
}

func sameArgv(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// assertNoJobRefs fails if the dry-run push ever landed a job ref on the
// bare remote.
func assertNoJobRefs(t *testing.T, f *prepareFixture) {
	t.Helper()
	out, err := f.git(f.bare, "for-each-ref", "refs/heads/job/")
	if err != nil {
		t.Fatalf("for-each-ref: %s: %v", out, err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("the bare remote gained job refs: %q", out)
	}
}

func TestPrepareAdmission(t *testing.T) {
	t.Run("organization out of scope refuses with exit 2 before any subprocess", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		_, err := f.preparer().Prepare(PrepareRequest{ID: prepID, Org: "other-org", Repo: prepRepo, Base: "main"})
		wantPlatformExit(t, err, ExitUsage, "job: organization out of scope")
		if calls := f.ghCalls(); len(calls) != 0 {
			t.Fatalf("gh calls after a refused admission = %d, want 0", len(calls))
		}
		if _, statErr := os.Stat(f.checkout()); !os.IsNotExist(statErr) {
			t.Fatalf("checkout path appeared after a refused admission: %v", statErr)
		}
	})
	t.Run("invalid id refuses with exit 2 before any subprocess", func(t *testing.T) {
		for _, id := range []string{"", "a b", ".", "..", strings.Repeat("a", 65)} {
			t.Run("id="+strings.ReplaceAll(id, " ", "_"), func(t *testing.T) {
				f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
				req := f.req("main", "")
				req.ID = id
				_, err := f.preparer().Prepare(req)
				wantExit(t, err, ExitUsage, "job: invalid id")
				if calls := f.ghCalls(); len(calls) != 0 {
					t.Fatalf("gh calls after an invalid id = %d, want 0", len(calls))
				}
			})
		}
	})
}

func TestPrepareCloneAndWorktree(t *testing.T) {
	t.Run("a missing checkout is cloned and the job worktree is created", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		prep := f.preparer()
		prep.Clone = f.gitClone()
		out, err := prep.Prepare(f.req("main", ""))
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		checkout := f.checkout()
		worktree := filepath.Join(checkout, ".worktrees", "job-"+prepID)
		if !out.Cloned || out.Checkout != checkout || out.Dir != worktree || out.Branch != "job/"+prepID || out.Base != "main" {
			t.Fatalf("Prepared = %+v, want cloned worktree %+v", out, Prepared{Checkout: checkout, Dir: worktree, Branch: "job/" + prepID, Base: "main"})
		}
		branch, _ := f.git(worktree, "rev-parse", "--abbrev-ref", "HEAD")
		if strings.TrimSpace(branch) != "job/"+prepID {
			t.Fatalf("worktree branch = %q, want job/%s", strings.TrimSpace(branch), prepID)
		}
		headSHA, _ := f.git(worktree, "rev-parse", "HEAD")
		baseSHA, _ := f.git(checkout, "rev-parse", "refs/remotes/origin/main")
		if strings.TrimSpace(headSHA) == "" || strings.TrimSpace(headSHA) != strings.TrimSpace(baseSHA) {
			t.Fatalf("worktree HEAD %q != origin/main %q", strings.TrimSpace(headSHA), strings.TrimSpace(baseSHA))
		}
		if out.BaseSHA != strings.TrimSpace(baseSHA) {
			t.Fatalf("BaseSHA = %q, want %q", out.BaseSHA, strings.TrimSpace(baseSHA))
		}
		// --no-track leaves no upstream configured for the job branch.
		if output, err := f.git(checkout, "config", "branch.job/"+prepID+".remote"); err == nil {
			t.Fatalf("branch.job/%s.remote = %q, want unset", prepID, output)
		}
		assertNoJobRefs(t, f)
	})
}

func TestPrepareProductionClone(t *testing.T) {
	f := newPrepareBase(t)
	f.installGh(t, []fakecli.Rule{
		{Argv: []string{"auth", "status"}},
		{Argv: []string{"repo", "clone", prepOrg + "/" + prepRepo, f.checkout()}},
	})
	out, err := f.preparer().Prepare(f.req("main", ""))
	wantExit(t, err, ExitPrepare, "job: preparation failed: clone")
	if out.Cloned {
		t.Fatal("Prepared.Cloned = true after a failed production clone")
	}
	calls := f.ghCalls()
	if len(calls) != 2 {
		t.Fatalf("gh calls = %d (argv %v), want exactly 2", len(calls), calls)
	}
	if !sameArgv(calls[0].Argv, []string{"auth", "status"}) {
		t.Fatalf("first gh call argv = %v, want [auth status]", calls[0].Argv)
	}
	if !sameArgv(calls[1].Argv, []string{"repo", "clone", prepOrg + "/" + prepRepo, f.checkout()}) {
		t.Fatalf("second gh call argv = %v, want [repo clone example-org/example-repo <checkout>]", calls[1].Argv)
	}
	if _, statErr := os.Stat(f.checkout()); !os.IsNotExist(statErr) {
		t.Fatalf("checkout exists after a failed clone: %v", statErr)
	}
}

func TestPrepareGhAuthentication(t *testing.T) {
	t.Run("a missing checkout refuses the failed gh auth before any clone", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}, Code: 1}})
		_, err := f.preparer().Prepare(f.req("main", ""))
		wantExit(t, err, ExitPrepare, "job: preparation failed: gh authentication")
		calls := f.ghCalls()
		if len(calls) != 1 || !sameArgv(calls[0].Argv, []string{"auth", "status"}) {
			t.Fatalf("gh calls = %v, want exactly [auth status]", calls)
		}
		if _, statErr := os.Stat(f.checkout()); !os.IsNotExist(statErr) {
			t.Fatalf("checkout appeared after a failed gh auth: %v", statErr)
		}
		if _, statErr := os.Stat(filepath.Join(f.reposRoot, prepOrg)); !os.IsNotExist(statErr) {
			t.Fatalf("org root appeared after a failed gh auth: %v", statErr)
		}
	})
	t.Run("an existing checkout refuses before any git fetch", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}, Code: 1}})
		checkout := f.cloneCheckout()
		// A branch only a fetch would import into the checkout.
		if out, err := f.git(f.bare, "branch", "fetch-sentinel", "main"); err != nil {
			t.Fatalf("git branch on the bare: %s: %v", out, err)
		}
		_, err := f.preparer().Prepare(f.req("main", ""))
		wantExit(t, err, ExitPrepare, "job: preparation failed: gh authentication")
		calls := f.ghCalls()
		if len(calls) != 1 {
			t.Fatalf("gh calls = %d, want exactly 1", len(calls))
		}
		if _, statErr := os.Stat(filepath.Join(checkout, ".worktrees", "job-"+prepID)); !os.IsNotExist(statErr) {
			t.Fatal("the worktree was created before gh authentication")
		}
		if _, err := f.git(checkout, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/fetch-sentinel"); err == nil {
			t.Fatal("git fetch ran before gh authentication")
		}
	})
}

func TestPrepareBaseResolution(t *testing.T) {
	t.Run("an unknown base refuses without worktree or branch", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		checkout := f.cloneCheckout()
		_, err := f.preparer().Prepare(f.req("nope", ""))
		wantExit(t, err, ExitPrepare, "job: preparation failed: unknown base")
		if _, statErr := os.Stat(filepath.Join(checkout, ".worktrees", "job-"+prepID)); !os.IsNotExist(statErr) {
			t.Fatalf("the worktree was created for an unknown base: %v", statErr)
		}
		if _, err := f.git(checkout, "rev-parse", "--verify", "--quiet", "refs/heads/job/"+prepID); err == nil {
			t.Fatal("the job branch was created for an unknown base")
		}
	})
	t.Run("an empty base resolves the remote HEAD", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		checkout := f.cloneCheckout()
		out, err := f.preparer().Prepare(f.req("", ""))
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		remoteBase, _ := f.git(checkout, "rev-parse", "refs/remotes/origin/main")
		if out.Base != "main" || out.BaseSHA != strings.TrimSpace(remoteBase) {
			t.Fatalf("Base/BaseSHA = %q/%q, want main/%s", out.Base, out.BaseSHA, strings.TrimSpace(remoteBase))
		}
		assertNoJobRefs(t, f)
	})
	t.Run("a remote HEAD pointing at a missing branch is an unknown base", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		if out, err := f.git(f.bare, "symbolic-ref", "HEAD", "refs/heads/gone"); err != nil {
			t.Fatalf("git symbolic-ref: %s: %v", out, err)
		}
		f.cloneCheckout()
		_, err := f.preparer().Prepare(f.req("", ""))
		wantExit(t, err, ExitPrepare, "job: preparation failed: unknown base")
	})
}

func TestPrepareRetry(t *testing.T) {
	t.Run("a second prepare reuses the worktree and reports no clone", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		prep := f.preparer()
		prep.Clone = f.gitClone()
		first, err := prep.Prepare(f.req("main", ""))
		if err != nil {
			t.Fatalf("first Prepare: %v", err)
		}
		second, err := prep.Prepare(f.req("main", ""))
		if err != nil {
			t.Fatalf("second Prepare: %v", err)
		}
		if second.Cloned {
			t.Fatal("the second run reported a clone")
		}
		if second.Dir != first.Dir || second.Branch != first.Branch || second.Base != first.Base || second.BaseSHA != first.BaseSHA {
			t.Fatalf("second Prepared = %+v, want %+v", second, first)
		}
		headSHA, _ := f.git(first.Dir, "rev-parse", "HEAD")
		if strings.TrimSpace(headSHA) != first.BaseSHA {
			t.Fatalf("worktree HEAD %q != first BaseSHA %q", strings.TrimSpace(headSHA), first.BaseSHA)
		}
		assertNoJobRefs(t, f)
	})
	t.Run("an existing local job branch without a worktree is attached", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		checkout := f.cloneCheckout()
		// A local job branch one commit ahead of origin/main, no worktree.
		for _, args := range [][]string{
			{"checkout", "-q", "-B", "job/" + prepID, "refs/remotes/origin/main"},
			{"commit", "-q", "--allow-empty", "-m", "local-only"},
			// Back to main so the job branch is not checked out in the
			// main worktree when Prepare attaches it.
			{"checkout", "-q", "main"},
		} {
			if out, err := f.git(checkout, args...); err != nil {
				t.Fatalf("git %v: %s: %v", strings.Join(args, " "), out, err)
			}
		}
		localSHA, _ := f.git(checkout, "rev-parse", "refs/heads/job/"+prepID)
		out, err := f.preparer().Prepare(f.req("main", ""))
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		headSHA, _ := f.git(out.Dir, "rev-parse", "HEAD")
		if strings.TrimSpace(headSHA) != strings.TrimSpace(localSHA) {
			t.Fatalf("worktree HEAD %q != the pre-existing branch tip %q (recreated, not attached)", strings.TrimSpace(headSHA), strings.TrimSpace(localSHA))
		}
		assertNoJobRefs(t, f)
	})
	t.Run("a plain directory at the worktree path is refused and untouched", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		checkout := f.cloneCheckout()
		dir := filepath.Join(checkout, ".worktrees", "job-"+prepID)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(dir, "marker")
		if err := os.WriteFile(marker, []byte("keep me"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := f.preparer().Prepare(f.req("main", ""))
		wantExit(t, err, ExitPrepare, "job: preparation failed: worktree")
		data, readErr := os.ReadFile(marker)
		if readErr != nil || string(data) != "keep me" {
			t.Fatalf("the existing directory was touched: %v %q", readErr, data)
		}
		if _, err := f.git(checkout, "rev-parse", "--verify", "--quiet", "refs/heads/job/"+prepID); err == nil {
			t.Fatal("the job branch was created for a foreign worktree path")
		}
	})
	t.Run("a plain directory is refused while the checkout itself is on the job branch", func(t *testing.T) {
		// git -C walks up from a plain directory to the checkout, so the
		// branch and common-dir checks alone would accept it.
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		checkout := f.cloneCheckout()
		if out, err := f.git(checkout, "checkout", "-b", "job/"+prepID); err != nil {
			t.Fatalf("checkout -b: %v %s", err, out)
		}
		dir := filepath.Join(checkout, ".worktrees", "job-"+prepID)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(dir, "marker")
		if err := os.WriteFile(marker, []byte("keep me"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := f.preparer().Prepare(f.req("main", ""))
		wantExit(t, err, ExitPrepare, "job: preparation failed: worktree")
		data, readErr := os.ReadFile(marker)
		if readErr != nil || string(data) != "keep me" {
			t.Fatalf("the existing directory was touched: %v %q", readErr, data)
		}
		if _, statErr := os.Lstat(filepath.Join(dir, ".git")); !os.IsNotExist(statErr) {
			t.Fatalf("a .git entry appeared in the plain directory: %v", statErr)
		}
	})
}

func TestPrepareUnknownMode(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	req := f.req("main", "")
	req.Mode = "elsewhere"
	_, err := f.preparer().Prepare(req)
	wantExit(t, err, ExitUsage, "job: invalid mode")
	if calls := f.ghCalls(); len(calls) != 0 {
		t.Fatalf("a subprocess ran for an invalid mode: %v", calls)
	}
}

func TestPrepareWorkspaceMode(t *testing.T) {
	t.Run("a clean checkout is used as is, without branch or worktree", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		checkout := f.cloneCheckout()
		out, err := f.preparer().Prepare(f.req("main", "workspace"))
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if out.Dir != checkout || out.Checkout != checkout || out.Branch != "main" || out.Cloned {
			t.Fatalf("Prepared = %+v, want the checkout on main", out)
		}
		if _, err := f.git(checkout, "rev-parse", "--verify", "--quiet", "refs/heads/job/"+prepID); err == nil {
			t.Fatal("workspace mode created the job branch")
		}
		if _, statErr := os.Stat(filepath.Join(checkout, ".worktrees")); !os.IsNotExist(statErr) {
			t.Fatalf("workspace mode created .worktrees: %v", statErr)
		}
		assertNoJobRefs(t, f)
	})
	t.Run("job state and worktrees in the checkout are excluded, not dirty", func(t *testing.T) {
		// The job state root (<checkout>/.herdr-soho) and the job worktrees
		// (<checkout>/.worktrees) live inside the checkout; a later
		// workspace-mode job must not see them as untracked changes.
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		checkout := f.cloneCheckout()
		for _, dir := range []string{".herdr-soho/jobs/other", ".worktrees/job-other"} {
			if err := os.MkdirAll(filepath.Join(checkout, dir), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(checkout, dir, "f"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.preparer().Prepare(f.req("main", "workspace")); err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		exclude, err := os.ReadFile(filepath.Join(checkout, ".git", "info", "exclude"))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range []string{"/.herdr-soho/", "/.worktrees/"} {
			if strings.Count(string(exclude), line+"\n") != 1 {
				t.Fatalf("info/exclude = %q, want %s once", exclude, line)
			}
		}
		// A retry adds nothing.
		if _, err := f.preparer().Prepare(f.req("main", "workspace")); err != nil {
			t.Fatalf("Prepare retry: %v", err)
		}
		again, _ := os.ReadFile(filepath.Join(checkout, ".git", "info", "exclude"))
		if string(again) != string(exclude) {
			t.Fatalf("retry changed info/exclude: %q -> %q", exclude, again)
		}
	})
	t.Run("an untracked file is a dirty checkout", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		checkout := f.cloneCheckout()
		if err := os.WriteFile(filepath.Join(checkout, "stray.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := f.preparer().Prepare(f.req("main", "workspace"))
		wantExit(t, err, ExitPrepare, "job: preparation failed: checkout dirty")
	})
	t.Run("an index lock is a locked checkout", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		checkout := f.cloneCheckout()
		if err := os.WriteFile(filepath.Join(checkout, ".git", "index.lock"), []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := f.preparer().Prepare(f.req("main", "workspace"))
		wantExit(t, err, ExitPrepare, "job: preparation failed: checkout locked")
	})
}

func TestPreparePushAccess(t *testing.T) {
	t.Run("a push url that points nowhere refuses and never lands a ref", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		checkout := f.cloneCheckout()
		// Fetch keeps working; only the push path is broken.
		if out, err := f.git(checkout, "config", "remote.origin.pushurl", filepath.Join(f.root, "nonexistent.git")); err != nil {
			t.Fatalf("git config pushurl: %s: %v", out, err)
		}
		_, err := f.preparer().Prepare(f.req("main", ""))
		wantExit(t, err, ExitPrepare, "job: preparation failed: push access")
		assertNoJobRefs(t, f)
	})
}

func TestPrepareGhDeadline(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}, Delay: 5000}})
	prep := f.preparer()
	prep.GhTimeout = 200 * time.Millisecond
	start := time.Now()
	_, err := prep.Prepare(f.req("main", ""))
	elapsed := time.Since(start)
	wantExit(t, err, ExitPrepare, "job: preparation failed: gh authentication")
	if elapsed > 10*time.Second {
		t.Fatalf("Prepare took %s, want under 10s", elapsed)
	}
}
