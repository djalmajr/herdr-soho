//go:build unix

package job

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestLaunchFilesRefuseAFIFO: a FIFO planted at launch.json or launch.lock
// never blocks a launch step: each returns at once with a refusal, and
// nothing reads from or writes to the FIFO.
func TestLaunchFilesRefuseAFIFO(t *testing.T) {
	env := launchEnv(launchTestWorkspace, launchTestPane)
	t.Run("record", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		rec, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
		if err != nil {
			t.Fatal(err)
		}
		holdStartLock(t, dir)
		path := filepath.Join(dir, launchFileName)
		_ = os.Remove(path)
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatalf("create the FIFO fixture: %v", err)
		}
		start := time.Now()
		if confirmed, reason := ConfirmLaunch(store, "job-1", env); confirmed || reason != "the launch record is unreadable" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if got := checkLaunch(dir, clock, clock.Monotonic()+int64(time.Second), rec); got != launchLost {
			t.Fatalf("checkLaunch = %v", got)
		}
		if fenceLaunch(store, "job-1") != launchUncertain {
			t.Fatal("fenceLaunch on a FIFO is not uncertain")
		}
		if wall := time.Since(start); wall > 2*time.Second {
			t.Fatalf("the FIFO held the steps for %v", wall)
		}
	})
	t.Run("lock", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if err := syscall.Mkfifo(filepath.Join(dir, launchLockName), 0o600); err != nil {
			t.Fatalf("create the FIFO fixture: %v", err)
		}
		start := time.Now()
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err == nil {
			t.Fatal("issueLaunch succeeded with a FIFO lock")
		}
		if confirmed, _ := ConfirmLaunch(store, "job-1", env); confirmed {
			t.Fatal("confirmed with a FIFO lock")
		}
		if clock.sleeps != 0 {
			t.Fatalf("a FIFO lock was retried: sleeps=%d", clock.sleeps)
		}
		if wall := time.Since(start); wall > 2*time.Second {
			t.Fatalf("the FIFO held the steps for %v", wall)
		}
	})
}
