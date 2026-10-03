//go:build windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// windowsPing starts a loopback ping the test itself owns (killed in the
// cleanup, also when an assertion failed): -n 30 keeps it alive for the
// test's window without touching the network.
func windowsPing(t *testing.T, count int) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("ping", "-n", strconv.Itoa(count), "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Skipf("starting ping: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// TestProcInfoWindows: the process started by the test has a non-empty
// name that ends in the right executable and a creation time — the raw
// FILETIME ticks — stable between two reads.
func TestProcInfoWindows(t *testing.T) {
	cmd := windowsPing(t, 30)
	pid := cmd.Process.Pid
	var started, name string
	var ok bool
	deadline := time.Now().Add(5 * time.Second)
	for {
		started, name, ok = ProcInfo(pid, Env{})
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ProcInfo(%d) never became ok (last: %q %q)", pid, started, name)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if name == "" || !strings.HasSuffix(strings.ToLower(name), "ping.exe") {
		t.Fatalf("name = %q; want a non-empty name ending in ping.exe", name)
	}
	if _, err := strconv.ParseInt(started, 10, 64); err != nil {
		t.Fatalf("started = %q; want the raw FILETIME ticks as decimal", started)
	}
	again, againName, okAgain := ProcInfo(pid, Env{})
	if !okAgain || again != started || againName != name {
		t.Fatalf("second read = (%q, %q, %v); the creation time must be stable for the life of the process", again, againName, okAgain)
	}
	t.Run("the parent of the started process is the test process", func(t *testing.T) {
		parentOf, err := windowsParentSnapshot()
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		parent, seen := parentOf[pid]
		if !seen || parent != os.Getpid() {
			t.Fatalf("parent of %d = %d (seen=%v); want %d (the test process)", pid, parent, seen, os.Getpid())
		}
	})
	t.Run("a dead process gives ok=false", func(t *testing.T) {
		short := windowsPing(t, 1)
		shortPid := short.Process.Pid
		if _, err := short.Process.Wait(); err != nil {
			t.Fatalf("waiting for the short ping: %v", err)
		}
		if started, name, ok := ProcInfo(shortPid, Env{}); ok || started != "" || name != "" {
			t.Fatalf("ProcInfo(dead %d) = (%q, %q, %v); want the zero result", shortPid, started, name, ok)
		}
	})
}

// TestStopProcessTreeWindows: the stop terminates a parent and a child
// started by the test (cmd /c ping: the cmd is the parent, the ping the
// child).
func TestStopProcessTreeWindows(t *testing.T) {
	if _, err := exec.LookPath("cmd"); err != nil {
		t.Skip("cmd is not available on PATH")
	}
	shell := exec.Command("cmd", "/c", "ping", "-n", "30", "127.0.0.1")
	if err := shell.Start(); err != nil {
		t.Skipf("starting cmd: %v", err)
	}
	t.Cleanup(func() {
		_ = shell.Process.Kill()
		_ = shell.Wait()
	})
	parent := shell.Process.Pid
	var child int
	deadline := time.Now().Add(5 * time.Second)
	for {
		parentOf, err := windowsParentSnapshot()
		if err == nil {
			for pid, p := range parentOf {
				if p == parent {
					if child == 0 {
						child = pid
					}
					break
				}
			}
		}
		if child > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cmd %d never spawned a child", parent)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Cleanup(func() {
		if p, err := os.FindProcess(child); err == nil {
			_ = p.Kill()
		}
	})
	started := time.Now()
	readStarted := ""
	readDeadline := time.Now().Add(5 * time.Second)
	for {
		s, _, ok := ProcInfo(parent, Env{})
		if ok {
			readStarted = s
			break
		}
		if time.Now().After(readDeadline) {
			t.Fatalf("ProcInfo(%d) never became ok", parent)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := StopProcessTree(parent, readStarted, Env{}); err != nil {
		t.Fatalf("StopProcessTree(%d): %v", parent, err)
	}
	for _, pid := range []int{parent, child} {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !procExists(pid) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if procExists(pid) {
			t.Fatalf("pid %d survived the stop", pid)
		}
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("the TERM round took %s; a waiting process must not need the KILL round", elapsed)
	}
}

// TestWindowsAdmissionRevalidatesTheCurrentParent: unit test of the
// collection admission with the fake Toolhelp32 tables and the fake
// creation ticks injected: after the creation-time capture a second
// snapshot is taken, and a descendant is admitted only when its current
// parent is the first snapshot's parent and its creation ticks are not
// older than its parent's captured ticks. The TERM is recorded, never
// sent (the candidate pids are not real processes of this test).
func TestWindowsAdmissionRevalidatesTheCurrentParent(t *testing.T) {
	const (
		root   = 200000 // the registered root (ticks 100)
		inTree = 200001 // admitted: the current parent is the root, newer than the root
		moved  = 200002 // rejected: the second snapshot has another parent
		older  = 200003 // rejected: older than its parent (a reused pid)
		grand  = 200004 // admitted: under the admitted child
	)
	ticks := map[int]string{root: "100", inTree: "250", moved: "150", older: "50", grand: "300"}
	first := map[int]int{root: 1, inTree: root, moved: root, older: root, grand: inTree}
	second := map[int]int{root: 1, inTree: root, moved: 999999, older: root, grand: inTree}
	reads := 0
	snapshots := 0
	oldSnap, oldRead, oldTerm := procParentSnapshotFunc, procReadIdentity, procSendTerm
	procParentSnapshotFunc = func() (map[int]int, error) {
		snapshots++
		if snapshots == 1 {
			return first, nil
		}
		return second, nil
	}
	procReadIdentity = func(pid int, env Env) (procIdentity, ProcLiveness) {
		reads++
		// The collection (5 reads) and the TERM re-reads (3) see the
		// ticks; anything after that (a KILL round, if ever reached) sees
		// unreadable pids and must not terminate anything.
		if reads > 8 {
			return procIdentity{}, ProcUnknown
		}
		s, seen := ticks[pid]
		if !seen {
			return procIdentity{}, ProcGone
		}
		return procIdentity{started: s}, ProcRunning
	}
	var term []int
	procSendTerm = func(pid int, env Env) error {
		term = append(term, pid)
		return nil
	}
	t.Cleanup(func() {
		procParentSnapshotFunc, procReadIdentity, procSendTerm = oldSnap, oldRead, oldTerm
	})
	err := StopProcessTree(root, "100", Env{})
	t.Logf("StopProcessTree error=%v reads=%d", err, reads)
	if snapshots != 2 {
		t.Fatalf("snapshots = %d; want the collection and the revalidation snapshot", snapshots)
	}
	// The admitted set is the root, inTree and grand (deepest first, the
	// root last): moved lost its parent in the second snapshot, older is
	// older than its parent.
	if len(term) != 3 || term[0] != grand || term[1] != inTree || term[2] != root {
		t.Fatalf("TERM order = %v; want [%d %d %d]: only the revalidated candidates, deepest first", term, grand, inTree, root)
	}
}

// TestWindowsAdmissionRootStartChanged: the unit twin of the unix root
// check: the root's captured creation time diverging from the registered
// one stops the call with the changed-after-the-check error and no
// signal.
func TestWindowsAdmissionRootStartChanged(t *testing.T) {
	const (
		root = 300000
		kid  = 300001
	)
	reads := 0
	oldSnap, oldRead, oldTerm := procParentSnapshotFunc, procReadIdentity, procSendTerm
	procParentSnapshotFunc = func() (map[int]int, error) {
		return map[int]int{root: 1, kid: root}, nil
	}
	procReadIdentity = func(pid int, env Env) (procIdentity, ProcLiveness) {
		reads++
		if pid == root {
			// The registered start is 100; the captured ticks are 200:
			// the pid changed after the check.
			return procIdentity{started: "200"}, ProcRunning
		}
		return procIdentity{started: "250"}, ProcRunning
	}
	var term []int
	procSendTerm = func(pid int, env Env) error {
		term = append(term, pid)
		return nil
	}
	t.Cleanup(func() {
		procParentSnapshotFunc, procReadIdentity, procSendTerm = oldSnap, oldRead, oldTerm
	})
	err := StopProcessTree(root, "100", Env{})
	if err == nil || err.Error() != fmt.Sprintf("process %d changed after the check", root) {
		t.Fatalf("StopProcessTree = %v; want the changed-after-the-check error", err)
	}
	if len(term) != 0 {
		t.Fatalf("TERM order = %v; want no signal on a divergent root", term)
	}
}

// TestWindowsAdmissionUnreadableRoot: an unreadable root identity read
// stops the call with an error and no signal (a read failure is not an
// absence).
func TestWindowsAdmissionUnreadableRoot(t *testing.T) {
	const root = 400000
	oldSnap, oldRead, oldTerm := procParentSnapshotFunc, procReadIdentity, procSendTerm
	procParentSnapshotFunc = func() (map[int]int, error) {
		return map[int]int{root: 1}, nil
	}
	procReadIdentity = func(pid int, env Env) (procIdentity, ProcLiveness) {
		return procIdentity{}, ProcUnknown
	}
	var term []int
	procSendTerm = func(pid int, env Env) error {
		term = append(term, pid)
		return nil
	}
	t.Cleanup(func() {
		procParentSnapshotFunc, procReadIdentity, procSendTerm = oldSnap, oldRead, oldTerm
	})
	err := StopProcessTree(root, "100", Env{})
	if err == nil || err.Error() != fmt.Sprintf("process %d is unreadable; not stopped", root) {
		t.Fatalf("StopProcessTree = %v; want the unreadable-root error", err)
	}
	if len(term) != 0 {
		t.Fatalf("TERM order = %v; want no signal on an unreadable root", term)
	}
}

// A failed GetExitCodeProcess (BOOL 0) is an unknown liveness, never a
// proven absence: the registry line stays and a wait keeps polling
// (r92c). Only STILL_ACTIVE is running and another code is gone.
func TestWindowsExitStateKeepsAFailedQueryUnknown(t *testing.T) {
	old := windowsExitCode
	t.Cleanup(func() { windowsExitCode = old })
	for _, tc := range []struct {
		code uint32
		ok   bool
		want ProcLiveness
	}{
		{0, false, ProcUnknown},
		{windowsStillActiveCode, true, ProcRunning},
		{0, true, ProcGone},
		{1, true, ProcGone},
	} {
		windowsExitCode = func(uintptr) (uint32, bool) { return tc.code, tc.ok }
		if got := windowsExitState(0); got != tc.want {
			t.Fatalf("code=%d ok=%v: windowsExitState=%v, want %v", tc.code, tc.ok, got, tc.want)
		}
	}
	// procExists on a live process (this test) whose exit query fails: not
	// proven gone, so it still reads as existing.
	windowsExitCode = func(uintptr) (uint32, bool) { return 0, false }
	if !procExists(os.Getpid()) {
		t.Fatal("procExists read a failed exit-code query as gone")
	}
}
