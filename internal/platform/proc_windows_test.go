//go:build windows

package platform

import (
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
	if err := StopProcessTree(parent, Env{}); err != nil {
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
