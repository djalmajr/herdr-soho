//go:build !windows

package eval

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// probeWaitDelay bounds how long the runner waits for the go test process to
// release inherited pipes after the owned process tree has terminated; it is
// the runner-side counterpart of the go toolchain's own WaitDelay.
const probeWaitDelay = 5 * time.Second

// runOwnedGoProcess runs the go test command as an owned process group: the
// whole tree (go, the compiled test binary and any worker children) is in a
// new process group, cancellation or the bounded deadline kills that entire
// group with SIGKILL, and WaitDelay bounds the pipe release wait. The
// temporary work directories (TMPDIR/TEMP/TMP) are expected to point inside
// the runner-owned temporary copy so cancelled go-build work is removed with
// it.
func runOwnedGoProcess(ctx context.Context, goExecutable string, dir string, args []string, env []string, stdout, stderr interface{ Write([]byte) (int, error) }) error {
	cmd := exec.CommandContext(ctx, goExecutable, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Kill the whole process group, not only the direct go process: the
	// compiled test binary and its (potentially untrusted worker) children
	// must not outlive the measurement.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = probeWaitDelay
	err := cmd.Run()
	// The group kill must happen on every return, not only on
	// cancellation: a green run reaps the group leader (the go process)
	// while a worker grandchild can remain detached — re-parented out of
	// the go test tree, holding no inherited stdio — and would otherwise
	// outlive the measurement and the scratch cleanup. Identity and
	// ownership: this run created the group (Setpgid) with its own child
	// as the leader, so -pid can only refer to the group this run owns.
	// POSIX exposes no generation token for this captured PGID. Once its
	// last member exits, reuse before the final signal can target a new
	// group; this best-effort cleanup does not eliminate that race. A member that
	// calls setsid leaves the group by design: there is no sandbox, and
	// that escape is declared, not a leak this layer must chase.
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		// cmd.Wait reaps only the direct go process: a grandchild (the go
		// toolchain's compile, a worker child) can still be inside the
		// kernel's exit path when the run returns — on macOS it reads a `?`
		// (unknown Mach) ps state while it finishes. Bounded verification of
		// the exact owned group completes the exit before the return; a
		// stuck group fails with an explicit, non-claiming error joined to
		// the run's own error (the cancellation text is preserved), never a
		// cleanup claim.
		if verifyErr := verifyOwnedGroupGone(cmd.Process.Pid); verifyErr != nil {
			if err != nil {
				return errors.Join(err, verifyErr)
			}
			return verifyErr
		}
	}
	return err
}

// processAlive reports whether a process with the given PID is running; the
// signal-0 probe is the standard Unix liveness check.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means the process exists but belongs to another user.
	return err == syscall.EPERM
}
