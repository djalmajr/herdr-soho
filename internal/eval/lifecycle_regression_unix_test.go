//go:build !windows

package eval

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// F-3: a green owned run that leaves a detached native child behind (no
// inherited stdio, re-parented out of the go test tree) must return only
// after the child is gone and the scratch is removable. Before this
// regression the child survived the measurement: it was the executed
// review probe S3 (aliveAfterReturn=true).
func TestLifecycleUnixDetachedChildDiesBeforeReturn(t *testing.T) {
	scratch := lifecycleScratch(t)
	pidFile := filepath.Join(scratch, "child.pid")
	marker := filepath.Join(scratch, "marker")
	writeLifecycleFixture(t, scratch)
	env := lifecycleEnv(lifecycleScratchEnv(scratch), pidFile, marker, false, false)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := runOwnedGoProcess(context.Background(), lifecycleGoExecutable(t), scratch, lifecycleTestArgs(), env, stdout, stderr)
	if err != nil {
		t.Fatalf("the fixture is a green run; runOwnedGoProcess = %v (stderr %q)", err, stderr.String())
	}
	pid := readPIDFile(pidFile)
	if pid <= 0 {
		t.Fatalf("detached child pid file missing: the fixture never reached the child")
	}
	// The child must be gone before the measurement returns: only then can
	// the caller remove the scratch without a live owner.
	// Signal 0 also succeeds for an unreaped zombie. Require the existing
	// three-way native reader to prove death; an unreadable PID is not proof.
	if _, live := platform.ReadProc(pid, platform.EnvFromOS()); live != platform.ProcGone {
		t.Cleanup(func() { _ = os.RemoveAll(scratch) })
		t.Fatalf("detached child %d still alive after runOwnedGoProcess returned (F-3)", pid)
	}
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the green return: %v", err)
	}
	if _, err := os.Lstat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch still present after RemoveAll: %v", err)
	}
}

// Cancellation control for the detached class: the existing
// TestRegressionCancelKillsOwnedProcessTree covers the inherited-stdio tree;
// this one covers a no-stdio detached child — the context cancellation must
// terminate it within the bounded cancellation and leave the scratch
// removable.
func TestLifecycleUnixCancelKillsDetachedChild(t *testing.T) {
	scratch := lifecycleScratch(t)
	pidFile := filepath.Join(scratch, "child.pid")
	marker := filepath.Join(scratch, "marker")
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
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := runOwnedGoProcess(ctx, lifecycleGoExecutable(t), scratch, lifecycleTestArgs(), env, stdout, stderr)
	if err == nil {
		t.Fatal("want the cancellation to surface as an owned-process error")
	}
	pid := readPIDFile(pidFile)
	if pid <= 0 {
		t.Fatalf("detached child pid file missing: the cancel never fired mid-test")
	}
	if !waitWhile(5*time.Second, func() bool { return processAlive(pid) }) {
		t.Fatalf("detached child %d still alive after the bounded cancellation (F-3 cancel path)", pid)
	}
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the cancellation: %v", err)
	}
}

// Identity control: the post-run group kill may only signal the group this
// run created (Setpgid, our own child as leader). A green run that leaves no
// children hits the empty-group path — the signal is a no-op (ESRCH, no
// target, no foreign or reused identity reachable) and must not disturb the
// run.
func TestLifecycleUnixGroupKillNoopWithoutChildren(t *testing.T) {
	scratch := lifecycleScratch(t)
	writeLifecycleFixture(t, scratch)
	env := lifecycleEnv(lifecycleScratchEnv(scratch), filepath.Join(scratch, "child.pid"), filepath.Join(scratch, "marker"), false, true)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	start := time.Now()
	err := runOwnedGoProcess(context.Background(), lifecycleGoExecutable(t), scratch, lifecycleTestArgs(), env, stdout, stderr)
	if err != nil {
		t.Fatalf("a childless green run must succeed: %v (stderr %q)", err, stderr.String())
	}
	if elapsed := time.Since(start); elapsed > 90*time.Second {
		t.Fatalf("childless run took %s: the empty-group kill must not block", elapsed)
	}
	if readPIDFile(filepath.Join(scratch, "child.pid")) != 0 {
		t.Fatal("the NOCHILD fixture must not have spawned a child")
	}
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the childless run: %v", err)
	}
}
