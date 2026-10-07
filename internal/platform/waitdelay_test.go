package platform

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// The child traps SIGTERM and exits through its own handler 300 ms after
// the signal. With the 1 s waitDelay the handler wins and the result
// carries the handler's exit code (42) and no signal; with a waitDelay
// shorter than the handler (the old 100 ms) the child is SIGKILLed
// instead — the result would carry Signal "SIGKILL" and no status. The
// child is the test binary itself, re-executed in the handler role: no
// shell, no external sleep.

const (
	waitDelayRoleEnv  = "HERDR_WAITDELAY_ROLE"
	waitDelayRoleName = "sigterm-handler"
	waitDelayHelper   = "TestWaitDelaySigtermHelper"
)

// TestWaitDelaySigtermHelper is the re-executed helper entry: when the
// role env is set the handler role runs and the process exits; in the main
// test run the env is unset and the function returns without running
// anything. The role traps SIGTERM exactly like the old `trap 'sleep 0.3;
// exit 42' TERM` did: the handler runs 300 ms after the signal and exits
// 42 through it, and the process stays alive until the signal arrives.
func TestWaitDelaySigtermHelper(t *testing.T) {
	if os.Getenv(waitDelayRoleEnv) != waitDelayRoleName {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	go func() {
		<-ch
		time.Sleep(300 * time.Millisecond)
		os.Exit(42)
	}()
	// Block inside a real blocking syscall (a read on a pipe whose write
	// end this process keeps open): the default SIGTERM still reaches the
	// handler while the read is blocked.
	r, w, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture: pipe: %v\n", err)
		os.Exit(1)
	}
	_ = w // held open for the life of the process: the read never returns.
	buf := make([]byte, 1)
	_, _ = r.Read(buf)
	os.Exit(1) // unreachable: the read blocks until the handler exits
}

func TestRunCliWaitDelayCoversSigtermHandler(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX SIGTERM handler")
	}
	bin := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	program := filepath.Join(bin, "sigterm-handler")
	if err := os.Link(exe, program); err != nil {
		data, readErr := os.ReadFile(exe)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if writeErr := os.WriteFile(program, data, 0o700); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	start := time.Now()
	// The deadline fires mid-loop and SIGTERMs the child; the handler then
	// needs 300 ms more, inside the 1 s waitDelay. The result (exit 42
	// through the handler, ETIMEDOUT) does not depend on the deadline's
	// value, only on the child having started and installed its trap before
	// the deadline fires (D17: a loaded host can take well over a second to
	// start a process), so the deadline is generous.
	r := RunCli("sigterm-handler", []string{"-test.run=^" + waitDelayHelper + "$"}, RunOptions{Env: Env{"PATH": bin, waitDelayRoleEnv: waitDelayRoleName}, TimeoutMs: 5000})
	elapsed := time.Since(start)
	if r.Status == nil || *r.Status != 42 {
		t.Fatalf("child did not exit through its SIGTERM handler: status=%v signal=%q error=%q", derefStatus(r.Status), r.Signal, r.Error)
	}
	if r.Signal != "" {
		t.Fatalf("child was killed by signal %s instead of exiting 42 through its handler", r.Signal)
	}
	if r.Error != "ETIMEDOUT" {
		t.Fatalf("the deadline did not fire (no ETIMEDOUT), so the handler was never exercised: result=%#v", r)
	}
	if elapsed < 4900*time.Millisecond || elapsed > 6500*time.Millisecond {
		t.Fatalf("elapsed=%v outside the expected window (SIGTERM at ~5 s, handler 300 ms, waitDelay 1 s)", elapsed)
	}
}

func derefStatus(v *int) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprint(*v)
}
