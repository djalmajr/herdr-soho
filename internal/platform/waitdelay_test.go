package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The child traps SIGTERM and exits through its own handler 300 ms after the
// signal. With the 1 s waitDelay the handler wins and the result carries the
// handler's exit code (42) and no signal; with a waitDelay shorter than the
// handler (the old 100 ms) the child is SIGKILLed instead — the result would
// carry Signal "SIGKILL" and no status.
func TestRunCliWaitDelayCoversSigtermHandler(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX SIGTERM handler")
	}
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatalf("locate sleep for the fixture: %v", err)
	}
	bin := t.TempDir()
	program := filepath.Join(bin, "sigterm-handler")
	script := fmt.Sprintf("#!/bin/sh\ntrap '%s 0.3; exit 42' TERM\nwhile :; do\n\t%s 1 &\n\twait $!\ndone\n", sleepPath, sleepPath)
	if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	// The deadline fires mid-loop and SIGTERMs the child; the handler then
	// needs 300 ms more, inside the 1 s waitDelay. The result (exit 42
	// through the handler, ETIMEDOUT) does not depend on the deadline's
	// value, only on the child having started and installed its trap before
	// the deadline fires (D17: a loaded host can take well over a second to
	// exec a fresh shell), so the deadline is generous.
	r := RunCli("sigterm-handler", nil, RunOptions{Env: Env{"PATH": bin}, TimeoutMs: 5000})
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
