package job

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/soho/internal/platform"
)

// managed_test.go: ManagedSelection with real processes. The parent is this
// test binary started as `<binary> job supervise --id <ID>`
// (managed_helper_test.go), bound to a pending launch through BindLaunch
// while the test holds start.lock like a live starter; the child is a
// managed command that classifies itself.

// managedFixture is one job store with a pending launch for job-1 in w1/p1,
// a running state recording cwd as the job directory, and a held start
// lock.
type managedFixture struct {
	root, dir, cwd, base string
}

func newManagedFixture(t *testing.T) managedFixture {
	return newManagedFixtureStartLock(t, true)
}

// newManagedFixtureStartLock is newManagedFixture with the start lock held
// by the test process for the whole run when lockHeld is true, as a live
// starter does.
//
// The jobdir-symlink and jobs-symlink scenarios of
// TestManagedSelectionRefused set lockHeld false: an open handle inside
// the job directory stops Windows from renaming that directory or the
// jobs directory ("Access is denied", errno 5), so the test process
// cannot hold the lock through the post-bind swap. There the helper takes
// the start lock for the bind and releases it before the swap
// (managedHelperLockVar, managedHelperParent). The property under test is
// the post-bind refusal of the symlinked directory, not the swap with
// the lock held.
func newManagedFixtureStartLock(t *testing.T, lockHeld bool) managedFixture {
	t.Helper()
	root := t.TempDir()
	store := Open(root)
	if _, err := store.Start("job-1", []byte(supTestBrief)); err != nil {
		t.Fatal(err)
	}
	dir, err := Dir(root, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	if _, err := store.Record("job-1", func(s *State) {
		s.Status, s.Dir, s.WorkspaceID, s.RootPane = StatusRunning, cwd, launchTestWorkspace, launchTestPane
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := issueLaunch(store, store.clock(), "job-1", launchTestWorkspace, launchTestPane); err != nil {
		t.Fatal(err)
	}
	if lockHeld {
		holdStartLock(t, dir)
	}
	base := filepath.Join(t.TempDir(), "config-base")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	return managedFixture{root: filepath.Clean(root), dir: dir, cwd: cwd, base: filepath.Clean(base)}
}

// managedRun is one parent run.
type managedRun struct {
	parentArgs  []string // after the binary; default job supervise --id job-1
	bind        bool
	mutate      string
	childArgs   string // default "status"
	childEnv    string // k=v;k=<unset>
	childDir    string // default the fixture's cwd
	parentExe   string // default this test binary
	noStartLock bool   // the test process does not hold the start lock (the helper does)
}

// run starts the parent and returns its output lines.
func (f managedFixture) run(t *testing.T, r managedRun) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := r.parentArgs
	if args == nil {
		args = []string{"job", "supervise", "--id", "job-1"}
	}
	childArgs := r.childArgs
	if childArgs == "" {
		childArgs = "status"
	}
	childDir := r.childDir
	if childDir == "" {
		childDir = f.cwd
	}
	env := WithoutLaunchLocators(managedHelperEnv())
	env["HERDR_ENV"] = "1"
	env["HERDR_WORKSPACE_ID"] = launchTestWorkspace
	env["HERDR_PANE_ID"] = launchTestPane
	env["SOHO_DIR"] = f.root
	env[managedHelperVar] = "parent"
	env[managedRootVar] = f.root
	env[managedArgsVar] = childArgs
	env[managedChildEnvVar] = r.childEnv
	env[managedChildDirVar] = childDir
	env[managedMutateVar] = r.mutate
	if r.bind {
		env[managedBindVar] = "1"
	}
	if r.noStartLock {
		env[managedHelperLockVar] = "1"
	}
	env[launchConfigBaseVar] = f.base
	exe := self
	if r.parentExe != "" {
		exe = r.parentExe
		env[managedChildExeVar] = self
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = env.List()
	done := make(chan struct{})
	var out []byte
	var runErr error
	go func() {
		out, runErr = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the managed parent did not finish within 60s")
	}
	if runErr != nil {
		t.Fatalf("parent: %v\n%s", runErr, out)
	}
	return string(out)
}

func wantOutcome(t *testing.T, out string, outcome ManagedOutcome, base string) {
	t.Helper()
	want := fmt.Sprintf("outcome=%d base=%s\n", outcome, base)
	if !strings.HasSuffix(out, want) {
		t.Fatalf("output %q, want it to end with %q", out, want)
	}
}

func requireProcExec(t *testing.T) {
	t.Helper()
	if _, _, err := platform.ProcExec(os.Getpid()); err != nil {
		t.Fatalf("this platform cannot read a process's executable and arguments (%v): the managed binding cannot be exercised here", err)
	}
}

// TestManagedSelectionBound: a team command run by the bound supervisor in
// the job directory gets the selection, and the record holds the
// supervisor's binding.
func TestManagedSelectionBound(t *testing.T) {
	requireProcExec(t)
	f := newManagedFixture(t)
	out := f.run(t, managedRun{bind: true})
	if !strings.HasPrefix(out, "bind=true reason=\n") {
		t.Fatalf("bind: %q", out)
	}
	wantOutcome(t, out, ManagedBound, f.base)
	rec := recordState(t, f.dir)
	if rec.State != launchConfirmed || rec.ConfigBase != f.base || rec.Started == "" || rec.Exe == "" || rec.PID <= 0 {
		t.Fatalf("record %+v", rec)
	}
	if !SameExecutable(rec.Exe) {
		t.Fatalf("recorded exe %q is not this test binary", rec.Exe)
	}
}

// TestManagedSelectionIgnored: the private values are ignored, exactly as
// without them, for a command that is not managed and for a parent proven
// not to be a job supervisor — even when the values are unusable.
func TestManagedSelectionIgnored(t *testing.T) {
	requireProcExec(t)
	f := newManagedFixture(t)
	t.Run("not a managed command under a bound supervisor", func(t *testing.T) {
		out := f.run(t, managedRun{bind: true, childArgs: "doctor"})
		wantOutcome(t, out, ManagedIgnored, "")
	})
	t.Run("config with arguments is not managed", func(t *testing.T) {
		out := f.run(t, managedRun{bind: true, childArgs: "config get x"})
		wantOutcome(t, out, ManagedIgnored, "")
	})
	t.Run("the parent is not job supervise", func(t *testing.T) {
		out := f.run(t, managedRun{parentArgs: []string{"job", "status", "--id", "job-1"}})
		wantOutcome(t, out, ManagedIgnored, "")
	})
	t.Run("the parent is not job supervise and the values are unusable", func(t *testing.T) {
		out := f.run(t, managedRun{parentArgs: []string{"spawn"}, childEnv: launchConfigBaseVar + "=relative/base"})
		wantOutcome(t, out, ManagedIgnored, "")
	})
}

// TestManagedSelectionRefused: under a parent identified as a job
// supervisor (or one that cannot be told apart), every broken part of the
// binding refuses: the command never falls back to the environment's
// configuration.
func TestManagedSelectionRefused(t *testing.T) {
	requireProcExec(t)
	cases := []managedRun{
		{bind: false},
		{bind: true, mutate: "revoke"},
		{bind: true, mutate: "nonce"},
		{bind: true, mutate: "pid"},
		{bind: true, mutate: "started"},
		{bind: true, mutate: "exe"},
		{bind: true, mutate: "base"},
		{bind: true, mutate: "workspace"},
		{bind: true, mutate: "store"},
		{bind: true, mutate: "launch-missing"},
		{bind: true, mutate: "launch-symlink"},
		{bind: true, mutate: "state-dir"},
		{bind: true, mutate: "state-pane"},
		{bind: true, mutate: "state-symlink"},
		{bind: true, mutate: "jobdir-symlink", noStartLock: true},
		{bind: true, mutate: "jobs-symlink", noStartLock: true},
		{bind: true, childEnv: launchConfigBaseVar + "=relative/base"},
		{bind: true, childEnv: launchHomeVar + "=/home-without-a-base;" + launchConfigBaseVar + "=<unset>"},
		{bind: true, childEnv: "HERDR_WORKSPACE_ID=w-other"},
		{bind: true, childEnv: "HERDR_PANE_ID=p-other"},
		{bind: true, childEnv: "HERDR_ENV=<unset>"},
		{bind: true, childEnv: "SOHO_DIR=" + filepath.Join(os.TempDir(), "another-store")},
		{parentArgs: []string{"job", "supervise", "--id", "../job-1"}},
		{parentArgs: []string{"job", "supervise", "--id", "job-1", "--extra"}},
		{parentArgs: []string{"job", "supervise"}},
	}
	if testHasFIFO {
		cases = append(cases, managedRun{bind: true, mutate: "launch-fifo"})
	}
	for _, c := range cases {
		name := fmt.Sprintf("args=%v bind=%v mutate=%s env=%s", c.parentArgs, c.bind, c.mutate, c.childEnv)
		t.Run(name, func(t *testing.T) {
			f := newManagedFixtureStartLock(t, !c.noStartLock)
			out := f.run(t, c)
			if c.bind && !strings.HasPrefix(out, "bind=true reason=\n") {
				t.Fatalf("bind: %q", out)
			}
			wantOutcome(t, out, ManagedRefused, "")
		})
	}
	t.Run("a supervise parent running another executable file", func(t *testing.T) {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(t.TempDir(), filepath.Base(self))
		if err := os.WriteFile(other, data, 0o755); err != nil {
			t.Fatal(err)
		}
		// The copy binds the launch with its own executable, so only the
		// child's file identity check tells it from this binary.
		f := newManagedFixture(t)
		out := f.run(t, managedRun{parentExe: other, bind: true})
		if !strings.HasPrefix(out, "bind=true reason=\n") {
			t.Fatalf("bind: %q", out)
		}
		wantOutcome(t, out, ManagedRefused, "")
	})
	t.Run("another working directory", func(t *testing.T) {
		f := newManagedFixture(t)
		out := f.run(t, managedRun{bind: true, childDir: t.TempDir()})
		wantOutcome(t, out, ManagedRefused, "")
	})
}

// TestManagedParentSuperviseShape: the direct-parent parser accepts
// exactly the two forms job start runs — `job supervise --id <ID>` and
// `job supervise --id <ID> --repo <ORG/REPO>`, in that order, with a valid
// id and repo — and keeps the existing classification for every other
// variation: parentNotSupervisor (0) for arguments without the supervise
// shape, parentUnknown (2) for the supervise shape with an invalid id or
// repo. The child prints the parent's kind (managedHelperChild) before its
// outcome line; the bound run proves the accepted repo form end to end.
func TestManagedParentSuperviseShape(t *testing.T) {
	requireProcExec(t)
	const (
		kindNotSupervisor = 0
		kindSupervisor    = 1
		kindUnknown       = 2
	)
	cases := []struct {
		name string
		args []string
		kind int
	}{
		{"id only", []string{"job", "supervise", "--id", "job-1"}, kindSupervisor},
		{"id and repo", []string{"job", "supervise", "--id", "job-1", "--repo", "example-org/example-repo"}, kindSupervisor},
		{"repo before id", []string{"job", "supervise", "--repo", "example-org/example-repo", "--id", "job-1"}, kindUnknown},
		{"repeated repo", []string{"job", "supervise", "--id", "job-1", "--repo", "example-org/example-repo", "--repo", "example-org/example-repo"}, kindUnknown},
		{"invalid repo", []string{"job", "supervise", "--id", "job-1", "--repo", "inválido"}, kindUnknown},
		{"empty repo", []string{"job", "supervise", "--id", "job-1", "--repo", ""}, kindUnknown},
		{"extra argument", []string{"job", "supervise", "--id", "job-1", "--repo", "example-org/example-repo", "extra"}, kindUnknown},
		{"repo without value", []string{"job", "supervise", "--id", "job-1", "--repo"}, kindUnknown},
		{"invalid id", []string{"job", "supervise", "--id", "../job-1"}, kindUnknown},
		{"not the supervise shape", []string{"job", "status", "--id", "job-1"}, kindNotSupervisor},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newManagedFixture(t)
			out := f.run(t, managedRun{parentArgs: c.args})
			if !strings.Contains(out, fmt.Sprintf("parentkind=%d\n", c.kind)) {
				t.Fatalf("output %q, want parentkind=%d", out, c.kind)
			}
		})
	}
	// The accepted repo form binds the launch and the child gets the
	// selection, exactly like the id-only form.
	t.Run("id and repo binds the launch", func(t *testing.T) {
		f := newManagedFixture(t)
		out := f.run(t, managedRun{bind: true, parentArgs: []string{"job", "supervise", "--id", "job-1", "--repo", "example-org/example-repo"}})
		if !strings.HasPrefix(out, "bind=true reason=\n") {
			t.Fatalf("bind: %q", out)
		}
		wantOutcome(t, out, ManagedBound, f.base)
	})
}
