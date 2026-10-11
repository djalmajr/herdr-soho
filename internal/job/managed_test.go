package job

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
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
	holdStartLock(t, dir)
	base := filepath.Join(t.TempDir(), "config-base")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	return managedFixture{root: filepath.Clean(root), dir: dir, cwd: cwd, base: filepath.Clean(base)}
}

// managedRun is one parent run.
type managedRun struct {
	parentArgs []string // after the binary; default job supervise --id job-1
	bind       bool
	mutate     string
	childArgs  string // default "status"
	childEnv   string // k=v;k=<unset>
	childDir   string // default the fixture's cwd
	parentExe  string // default this test binary
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
	env["HERDR_SOHO_DIR"] = f.root
	env[managedHelperVar] = "parent"
	env[managedRootVar] = f.root
	env[managedArgsVar] = childArgs
	env[managedChildEnvVar] = r.childEnv
	env[managedChildDirVar] = childDir
	env[managedMutateVar] = r.mutate
	if r.bind {
		env[managedBindVar] = "1"
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
		{bind: true, mutate: "jobdir-symlink"},
		{bind: true, mutate: "jobs-symlink"},
		{bind: true, childEnv: launchConfigBaseVar + "=relative/base"},
		{bind: true, childEnv: launchHomeVar + "=/home-without-a-base;" + launchConfigBaseVar + "=<unset>"},
		{bind: true, childEnv: "HERDR_WORKSPACE_ID=w-other"},
		{bind: true, childEnv: "HERDR_PANE_ID=p-other"},
		{bind: true, childEnv: "HERDR_ENV=<unset>"},
		{bind: true, childEnv: "HERDR_SOHO_DIR=" + filepath.Join(os.TempDir(), "another-store")},
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
			f := newManagedFixture(t)
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
