package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
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

// oneShellChildTree starts sh -c "sleep 300 & wait" and polls (with a
// ceiling) until it has exactly one descendant: the owned tree for the
// stop tests.
func oneShellChildTree(t *testing.T, env Env) (parent int, child int) {
	t.Helper()
	shell := exec.Command("sh", "-c", "sleep 300 & wait")
	if err := shell.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = shell.Process.Kill()
		_ = shell.Wait()
	})
	parent = shell.Process.Pid
	var kids []int
	deadline := time.Now().Add(5 * time.Second)
	for {
		var err error
		kids, err = procDescendants(parent, env)
		if err == nil && len(kids) == 1 {
			return parent, kids[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendants of %d never became one (last: %v, %v)", parent, kids, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestStopRejectsAChildReparentedAfterTheSnapshot is the review probe as
// a durable test with the real process table: the hook TERMs the parent
// between the snapshot and the identity read, so the child reparents
// while the snapshot still lists it under the parent. The child must not
// be signalled and must stay alive.
func TestStopRejectsAChildReparentedAfterTheSnapshot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix TERM/KILL round; the windows round exercises the same contract")
	}
	env := procTestEnv(t)
	parent, child := oneShellChildTree(t, env)
	procStopAfterSnapshot = func(root int, env Env) {
		// Reparent the child before the identity pass: TERM the parent
		// and let the reparenting settle (os.Process.Signal compiles on
		// every OS; the test itself skips off-unix).
		if p, err := os.FindProcess(root); err == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Cleanup(func() { procStopAfterSnapshot = nil })
	if err := StopProcessTree(parent, env); err != nil {
		t.Fatalf("StopProcessTree(%d): %v; nothing verified is a clean no-op", parent, err)
	}
	if !procAliveStates([]int{child}, env)[child] {
		t.Fatal("the child was signalled after it reparented; only verified pids are signalled")
	}
	// The child outlived the stop: it is owned by this test, stop it by
	// pid.
	if p, err := os.FindProcess(child); err == nil {
		_ = p.Kill()
	}
}

// TestStopSkipsAKillWhenTheStartTimeChanged: a process that ignores TERM
// reaches the KILL round; the injected identity read then reports a
// different start time for it, and the KILL must not go.
func TestStopSkipsAKillWhenTheStartTimeChanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix TERM/KILL round; the windows round exercises the same contract")
	}
	env := procTestEnv(t)
	cmd := exec.Command("/usr/bin/python3", "-c", "import signal,time\nsignal.signal(signal.SIGTERM, signal.SIG_IGN)\nwhile True: time.sleep(0.05)\n")
	if _, err := exec.LookPath("/usr/bin/python3"); err != nil {
		t.Skip("/usr/bin/python3 is not available")
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("starting python3: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() // SIGKILL: the process ignores TERM
		_ = cmd.Wait()
	})
	pid := cmd.Process.Pid
	real := procReadIdentity
	reads := map[int]int{}
	procReadIdentity = func(pid int, env Env) (procIdentity, bool) {
		id, ok := real(pid, env)
		if ok {
			reads[pid]++
			// The collection read and the pre-TERM re-read pass through;
			// from the pre-KILL re-read on the start time is adulterated.
			if reads[pid] >= 3 {
				id.started = "adulterated start time"
			}
		}
		return id, ok
	}
	t.Cleanup(func() { procReadIdentity = real })
	if err := StopProcessTree(pid, env); err == nil {
		t.Fatal("StopProcessTree succeeded although the process survived; the skipped KILL is a failure to report")
	}
	if !procAliveStates([]int{pid}, env)[pid] {
		t.Fatal("the process was KILLed despite the changed start time")
	}
}

// TestStopTermsTheDeepestDescendantFirst: the TERM round goes from the
// deepest descendant to the root, the root last. The parent ignores TERM
// (and stays alive after its child dies) so both TERM sends are
// observable; an sh -c "... & wait" parent would exit the moment its
// child stops and the second send would be skipped on a zombie.
func TestStopTermsTheDeepestDescendantFirst(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix TERM/KILL round; the windows round exercises the same contract")
	}
	env := procTestEnv(t)
	if _, err := exec.LookPath("/usr/bin/python3"); err != nil {
		t.Skip("/usr/bin/python3 is not available")
	}
	parent := exec.Command("/usr/bin/python3", "-c", "import signal,subprocess,time\nsignal.signal(signal.SIGTERM, signal.SIG_IGN)\nsubprocess.Popen(['sleep', '300'])\ntime.sleep(300)\n")
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = parent.Process.Kill()
		_ = parent.Wait()
	})
	var child int
	deadline := time.Now().Add(5 * time.Second)
	for {
		kids, err := procDescendants(parent.Process.Pid, env)
		if err == nil && len(kids) == 1 {
			child = kids[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendants of %d never became one (last: %v, %v)", parent.Process.Pid, kids, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	real := procSendTerm
	var order []int
	procSendTerm = func(pid int, env Env) error {
		order = append(order, pid)
		return real(pid, env)
	}
	t.Cleanup(func() { procSendTerm = real })
	if err := StopProcessTree(parent.Process.Pid, env); err != nil {
		t.Fatalf("StopProcessTree(%d): %v", parent.Process.Pid, err)
	}
	if len(order) != 2 || order[0] != child || order[1] != parent.Process.Pid {
		t.Fatalf("TERM order = %v; want the child before the parent", order)
	}
}

// TestReview92RejectReparentedSnapshot (review probe, now durable): a
// fake ps whose -A table still lists the child under the parent while the
// child reparented (the parent was TERMed inside the snapshot) must not
// lead the stop to signal the child: the identity pass drops it and it
// stays alive.
func TestReview92RejectReparentedSnapshot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix TERM/KILL round; the windows round exercises the same contract")
	}
	if _, err := exec.LookPath("/usr/bin/python3"); err != nil {
		t.Skip("/usr/bin/python3 is not available")
	}
	env := procTestEnv(t)
	dir := t.TempDir()
	kidFile := filepath.Join(dir, "kid")
	beat := filepath.Join(dir, "beat")
	py := `import os,time,sys
kid=os.fork()
if kid:
 open(sys.argv[1],"w").write(str(kid))
 time.sleep(300)
else:
 while True:
  open(sys.argv[2],"w").write(str(os.getppid())+" "+str(time.time_ns()))
  time.sleep(.05)
`
	root := exec.Command("/usr/bin/python3", "-c", py, kidFile, beat)
	if err := root.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = root.Wait(); close(done) }()
	t.Cleanup(func() { _ = root.Process.Kill(); <-done })
	var kid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(kidFile)
		kid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		if kid > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if kid <= 0 {
		t.Fatal("child not started")
	}
	t.Cleanup(func() { p, _ := os.FindProcess(kid); _ = p.Kill() })
	time.Sleep(100 * time.Millisecond)
	b, _ := os.ReadFile(beat)
	fields := strings.Fields(string(b))
	if len(fields) != 2 || fields[0] != strconv.Itoa(root.Process.Pid) {
		t.Fatalf("initial parent invalid: %q", b)
	}
	t.Logf("owned parent=%d child=%d initial-ppid=%s", root.Process.Pid, kid, fields[0])
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-A" ]; then
 kill -TERM %d
 /bin/sleep 0.3
 cat '%s' > '%s'
 printf '%%s %%s\n' '%d' '%d' '%d' '%d'
 exit 0
fi
# Only the two owned fixture PIDs are present in the simulated table.
for p in %d %d; do
 if kill -0 "$p" 2>/dev/null; then printf '%%s S\n' "$p"; fi
done
`, root.Process.Pid, beat, filepath.Join(dir, "reparented"), root.Process.Pid, os.Getpid(), kid, root.Process.Pid, root.Process.Pid, kid)
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	env["PATH"] = dir + ":" + env.Get("PATH")
	err := StopProcessTree(root.Process.Pid, env)
	t.Logf("StopProcessTree error=%v", err)
	b, e := os.ReadFile(filepath.Join(dir, "reparented"))
	if e != nil {
		t.Fatal(e)
	}
	fields = strings.Fields(string(b))
	if len(fields) != 2 {
		t.Fatalf("snapshot transition unavailable: %q", b)
	}
	ppid, _ := strconv.Atoi(fields[0])
	t.Logf("after snapshot: child=%d ppid=%d former-parent=%d", kid, ppid, root.Process.Pid)
	if ppid == root.Process.Pid {
		t.Fatal("child did not reparent: fixture invalid")
	}
	before, _ := os.ReadFile(beat)
	time.Sleep(200 * time.Millisecond)
	after, _ := os.ReadFile(beat)
	if string(before) == string(after) {
		t.Fatal("stop signalled the child after it changed parent: heartbeat stopped")
	}
}

// TestReview92StopsOwnedChild (review probe control, now durable): the
// same fixture with the parent remaining in the tree — the child stays
// verified and the stop reaches it.
func TestReview92StopsOwnedChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix TERM/KILL round; the windows round exercises the same contract")
	}
	if _, err := exec.LookPath("/usr/bin/python3"); err != nil {
		t.Skip("/usr/bin/python3 is not available")
	}
	env := procTestEnv(t)
	dir := t.TempDir()
	kidFile := filepath.Join(dir, "kid")
	beat := filepath.Join(dir, "beat")
	py := `import os,time,sys
kid=os.fork()
if kid:
 open(sys.argv[1],"w").write(str(kid))
 time.sleep(300)
else:
 while True:
  open(sys.argv[2],"w").write(str(os.getppid())+" "+str(time.time_ns()))
  time.sleep(.05)
`
	root := exec.Command("/usr/bin/python3", "-c", py, kidFile, beat)
	if err := root.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = root.Wait(); close(done) }()
	t.Cleanup(func() { _ = root.Process.Kill(); <-done })
	var kid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(kidFile)
		kid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		if kid > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if kid <= 0 {
		t.Fatal("child not started")
	}
	t.Cleanup(func() { p, _ := os.FindProcess(kid); _ = p.Kill() })
	time.Sleep(100 * time.Millisecond)
	b, _ := os.ReadFile(beat)
	fields := strings.Fields(string(b))
	if len(fields) != 2 || fields[0] != strconv.Itoa(root.Process.Pid) {
		t.Fatalf("initial parent invalid: %q", b)
	}
	t.Logf("owned parent=%d child=%d initial-ppid=%s", root.Process.Pid, kid, fields[0])
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-A" ]; then
 : # parent %d remains in tree
 /bin/sleep 0.3
 cat '%s' > '%s'
 printf '%%s %%s\n' '%d' '%d' '%d' '%d'
 exit 0
fi
# Only the two owned fixture PIDs are present in the simulated table.
for p in %d %d; do
 if kill -0 "$p" 2>/dev/null; then printf '%%s S\n' "$p"; fi
done
`, root.Process.Pid, beat, filepath.Join(dir, "reparented"), root.Process.Pid, os.Getpid(), kid, root.Process.Pid, root.Process.Pid, kid)
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	env["PATH"] = dir + ":" + env.Get("PATH")
	err := StopProcessTree(root.Process.Pid, env)
	t.Logf("StopProcessTree error=%v", err)
	b, e := os.ReadFile(filepath.Join(dir, "reparented"))
	if e != nil {
		t.Fatal(e)
	}
	fields = strings.Fields(string(b))
	if len(fields) != 2 {
		t.Fatalf("snapshot transition unavailable: %q", b)
	}
	ppid, _ := strconv.Atoi(fields[0])
	t.Logf("after snapshot: child=%d ppid=%d former-parent=%d", kid, ppid, root.Process.Pid)
	if ppid != root.Process.Pid {
		t.Fatal("fixture child was not in tree")
	}
	before, _ := os.ReadFile(beat)
	time.Sleep(200 * time.Millisecond)
	after, _ := os.ReadFile(beat)
	if string(before) != string(after) {
		t.Fatal("owned tree child survived stop: heartbeat still advances")
	}
}
