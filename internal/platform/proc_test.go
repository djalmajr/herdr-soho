package platform

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// procTestEnv is the test process's own environment as an Env (the gate
// runs without the real herdr on PATH; ps/sleep/sh come from the system
// directories).
func procTestEnv(t *testing.T) Env {
	t.Helper()
	env := Env{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

// spawnProcess starts a real long-lived process the test itself owns and
// kills it in the cleanup (also when an assertion failed), so the test
// never leaves a process behind and never touches one it did not start.
func spawnProcess(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not available on PATH", name)
	}
	cmd := exec.Command(path, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// procGone waits (with a ceiling) until the pid is not running anymore
// (a zombie is not running).
func procGone(t *testing.T, env Env, pid int, ceiling time.Duration) {
	t.Helper()
	deadline := time.Now().Add(ceiling)
	for time.Now().Before(deadline) {
		if !procAliveStates([]int{pid}, env)[pid] {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pid %d is still running after %s", pid, ceiling)
}

func TestProcInfoCases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ps-based process info is unix; the windows runtime round exercises the kernel32 path")
	}
	env := procTestEnv(t)
	cmd := spawnProcess(t, "sleep", "300")
	pid := cmd.Process.Pid
	t.Run("a running process gives its start time and command base name", func(t *testing.T) {
		started, name, ok := ProcInfo(pid, env)
		if !ok || started == "" || name != "sleep" {
			t.Fatalf("ProcInfo(%d) = (%q, %q, %v); want (non-empty lstart, sleep, true)", pid, started, name, ok)
		}
		again, againName, okAgain := ProcInfo(pid, env)
		if !okAgain || again != started || againName != name {
			t.Fatalf("second read = (%q, %q, %v); the start time must be stable for the life of the process", again, againName, okAgain)
		}
	})
	t.Run("a dead pid gives no snapshot", func(t *testing.T) {
		if err := cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = cmd.Wait() // reap: the entry leaves the table; the kill error is expected
		if started, name, ok := ProcInfo(pid, env); ok || started != "" || name != "" {
			t.Fatalf("ProcInfo(dead %d) = (%q, %q, %v); want the zero result", pid, started, name, ok)
		}
	})
	t.Run("a zombie (killed, not reaped) is not running", func(t *testing.T) {
		zombie := spawnProcess(t, "sleep", "300")
		zpid := zombie.Process.Pid
		if err := zombie.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		// The entry stays a zombie until the cleanup reaps it: the state
		// must read as not running while it exists.
		procGone(t, env, zpid, 5*time.Second)
	})
}

func TestStopProcessTreeCases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix TERM/KILL round; the windows round exercises taskkill /T")
	}
	env := procTestEnv(t)
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not available on PATH")
	}
	t.Run("stops the pid and the descendants of it", func(t *testing.T) {
		// A backgrounded job forces sh to fork and keeps sh alive (wait):
		// the tree is sh + sleep, exactly one descendant.
		shell := exec.Command("sh", "-c", "sleep 300 & wait")
		if err := shell.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = shell.Process.Kill()
			_ = shell.Wait()
		})
		parent := shell.Process.Pid
		// The descendant is the sleep under the sh: read it from the same
		// pid/ppid snapshot the stop helper reads, polling until the sh
		// has forked.
		var kids []int
		deadline := time.Now().Add(5 * time.Second)
		for {
			var err error
			kids, err = procDescendants(parent, env)
			if err == nil && len(kids) == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("descendants of %d never became one (last: %v, %v); want the one backgrounded child", parent, kids, err)
			}
			time.Sleep(50 * time.Millisecond)
		}
		started := time.Now()
		if err := StopProcessTree(parent, env); err != nil {
			t.Fatalf("StopProcessTree(%d): %v", parent, err)
		}
		procGone(t, env, parent, 5*time.Second)
		for _, child := range kids {
			procGone(t, env, child, 5*time.Second)
		}
		if elapsed := time.Since(started); elapsed > 4*time.Second {
			t.Fatalf("the TERM round took %s; a sleeping process must not need the KILL round", elapsed)
		}
	})
	t.Run("a dead pid is a no-op", func(t *testing.T) {
		cmd := spawnProcess(t, "sleep", "300")
		pid := cmd.Process.Pid
		if err := cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = cmd.Wait() // reap: the entry leaves the table; the kill error is expected
		if err := StopProcessTree(pid, env); err != nil {
			t.Fatalf("StopProcessTree(dead %d): %v", pid, err)
		}
	})
	t.Run("a stop never reaches a pid the tree does not contain", func(t *testing.T) {
		other := spawnProcess(t, "sleep", "300")
		shell := exec.Command("sh", "-c", "sleep 300 & wait")
		if err := shell.Start(); err != nil {
			t.Fatal(err)
		}
		shellCleanup := func() {
			_ = shell.Process.Kill()
			_ = shell.Wait()
		}
		t.Cleanup(shellCleanup)
		if err := StopProcessTree(shell.Process.Pid, env); err != nil {
			t.Fatal(err)
		}
		if !procAliveStates([]int{other.Process.Pid}, env)[other.Process.Pid] {
			t.Fatalf("the stop signalled a pid outside the tree (%d)", other.Process.Pid)
		}
	})
}
