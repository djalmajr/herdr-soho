//go:build !windows

package eval

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// Real-process tests of the bounded owned-group verification through the
// production owned-run contract (runOwnedGoProcess), on the native lifecycle
// fixture (a green go test that re-execs its own binary; the middle
// generation spawns a no-stdio detached child that blocks forever). Every
// process is fixture-owned and reaped: the child is killed by the owned
// group kill and reaped by the system reaper once its parent exits. No
// foreign inspection beyond the read-only three-way liveness reader and the
// PID/PGID/state snapshot the verification itself performs.

// Bounded return with the at-return proof: the cancellation must surface as
// the owned-process error with the cancellation text preserved, and —
// stronger than the waitWhile grace of the legacy cancel test — the
// detached child must read as a proven gone (the three-way reader, no
// waiting) immediately after runOwnedGoProcess returns, because the
// verification completes the group's exit before the return.
func TestOwnedGroupDrainCancelledOrphanGoneAtReturn(t *testing.T) {
	scratch := lifecycleScratch(t)
	pidFile := filepath.Join(scratch, "child.pid")
	marker := filepath.Join(scratch, "drain-cancel-marker")
	writeLifecycleFixture(t, scratch)
	env := lifecycleEnv(lifecycleScratchEnv(scratch), pidFile, marker, true, false)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			if readPIDFile(pidFile) > 0 {
				time.Sleep(300 * time.Millisecond)
				cancel()
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		cancel()
	}()
	start := time.Now()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := runOwnedGoProcess(ctx, lifecycleGoExecutable(t), scratch, lifecycleTestArgs(), env, stdout, stderr)
	returnAfter := time.Since(start)
	if err == nil {
		t.Fatal("want the cancellation to surface as an owned-process error")
	}
	// The cancellation error is preserved in the returned error.
	if !strings.Contains(err.Error(), "signal: killed") {
		t.Fatalf("returned error %q does not preserve the cancellation error (stderr %q)", err, diagnosticTail(stderr.Bytes(), nil))
	}
	if returnAfter > 30*time.Second {
		t.Fatalf("the bounded cancellation took %s after the run start: the kill, the bounded verification and the pipe release must all be bounded", returnAfter)
	}
	pid := readPIDFile(pidFile)
	if pid <= 0 {
		t.Fatalf("detached child pid file missing: the cancel never fired mid-test")
	}
	// The at-return proof: no wait, no grace — the group was verified
	// before the return, so the child already reads as a proven gone.
	if _, live := platform.ReadProc(pid, platform.EnvFromOS()); live != platform.ProcGone {
		t.Cleanup(func() { _ = os.RemoveAll(scratch) })
		t.Fatalf("detached child %d does not read as a proven gone immediately after the return (liveness %v)", pid, live)
	}
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the verified cancellation: %v", err)
	}
	if _, err := os.Lstat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch still present after RemoveAll: %v", err)
	}
}

// A group the verification cannot prove exited — every kernel probe and
// every snapshot read stays pending (the seams are forced to fail, the way
// an unreadable group reads) — must fail with the explicit, non-claiming
// error at the bounded ceiling, while the fixture's real child (killed by
// the production group kill) is in fact dead behind the verification.
func TestOwnedGroupDrainUnverifiedGroupExplicitError(t *testing.T) {
	withOwnedGroupVerifyHooks(t,
		func(int) bool { return false },
		func(int, time.Duration) bool { return false })
	scratch := lifecycleScratch(t)
	pidFile := filepath.Join(scratch, "child.pid")
	marker := filepath.Join(scratch, "drain-unverified-marker")
	writeLifecycleFixture(t, scratch)
	env := lifecycleEnv(lifecycleScratchEnv(scratch), pidFile, marker, false, false)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	start := time.Now()
	err := runOwnedGoProcess(context.Background(), lifecycleGoExecutable(t), scratch, lifecycleTestArgs(), env, stdout, stderr)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want the explicit verification failure for the unprovable group")
	}
	for _, want := range []string{"not proven exited", "no cleanup is claimed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("returned error %q does not carry %q: the ceiling failure must be explicit and must not claim cleanup", err, want)
		}
	}
	if elapsed < ownedGroupVerifyCeiling {
		t.Fatalf("the verification failed after %s, before its %s ceiling: the budget was not honored (stderr %q)", elapsed, ownedGroupVerifyCeiling, diagnosticTail(stderr.Bytes(), nil))
	}
	// The explicit error does not hide a green run: the fixture itself ran
	// green, so the failure is the verification's, joined to no run error.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Fatalf("the fixture run was green; the returned error %v must not carry the run's own exit error", err)
	}
	// The fixture owns every process and reaps it: the real child (killed
	// by the production group kill) is dead, proven by the legacy two-way
	// read over a bounded wait — the verification's failure claims nothing
	// about the real group state.
	pid := readPIDFile(pidFile)
	if pid <= 0 {
		t.Cleanup(func() { _ = os.RemoveAll(scratch) })
		t.Fatalf("detached child pid file missing: the fixture never reached the child")
	}
	if !waitWhile(5*time.Second, func() bool { return processAlive(pid) }) {
		t.Fatalf("detached child %d still alive after the bounded run: the fixture must own and reap every process", pid)
	}
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the failed verification: %v", err)
	}
}

// Prompt normal empty-group completion on a real run: the childless green
// run hits the empty-group fast path (the kernel's ESRCH, no ps read, no
// sleep), so the verification must add no perceptible delay to the return.
func TestOwnedGroupDrainGreenNoChildPrompt(t *testing.T) {
	scratch := lifecycleScratch(t)
	writeLifecycleFixture(t, scratch)
	env := lifecycleEnv(lifecycleScratchEnv(scratch), filepath.Join(scratch, "child.pid"), filepath.Join(scratch, "drain-nop-marker"), false, true)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	start := time.Now()
	err := runOwnedGoProcess(context.Background(), lifecycleGoExecutable(t), scratch, lifecycleTestArgs(), env, stdout, stderr)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("a childless green run must succeed: %v (stderr %q)", err, diagnosticTail(stderr.Bytes(), nil))
	}
	if elapsed > 30*time.Second {
		t.Fatalf("childless run took %s: the empty-group verification must return promptly (no read, no sleep)", elapsed)
	}
	if readPIDFile(filepath.Join(scratch, "child.pid")) != 0 {
		t.Fatal("the NOCHILD fixture must not have spawned a child")
	}
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the childless run: %v", err)
	}
}
