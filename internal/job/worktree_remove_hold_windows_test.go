//go:build windows

package job

import (
	"os/exec"
	"testing"
	"time"
)

// holdWorktreeDir starts a process whose current directory is dir, the
// way the job's root pane shell holds the job worktree: Windows then
// deletes every file inside dir but not dir itself. release stops it.
func holdWorktreeDir(t *testing.T, dir string) (release func()) {
	t.Helper()
	// One process, no shell wrapper: a child of a wrapper would keep
	// the directory after the wrapper is killed.
	cmd := exec.Command("ping", "-n", "120", "127.0.0.1")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	released := false
	release = func() {
		if !released {
			released = true
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			// The handle on the directory goes with the process; give
			// the system a moment to drop it.
			time.Sleep(200 * time.Millisecond)
		}
	}
	t.Cleanup(release)
	return release
}
