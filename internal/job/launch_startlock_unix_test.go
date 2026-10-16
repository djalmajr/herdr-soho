//go:build unix

package job

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/djalmajr/soho/internal/platform"
)

// launch_startlock_unix_test.go pins the unix-specific start.lock probe:
// a FIFO at the path never blocks a step (the no-follow, non-blocking
// open and the post-open kind check refuse it), and an unreadable mode is
// an I/O error, not a kind refusal.

// confirmLaunchWithin runs ConfirmLaunch in a goroutine and fails the test
// when want has passed without a result: the probe must not block.
func confirmLaunchWithin(t *testing.T, store *Store, id string, env platform.Env, want time.Duration) (confirmed bool, reason string) {
	t.Helper()
	type outcome struct {
		confirmed bool
		reason    string
	}
	ch := make(chan outcome, 1)
	go func() {
		c, r := ConfirmLaunch(store, id, env)
		ch <- outcome{c, r}
	}()
	select {
	case res := <-ch:
		return res.confirmed, res.reason
	case <-time.After(want):
		t.Fatalf("ConfirmLaunch blocked for %v", want)
	}
	return false, ""
}

func TestConfirmLaunchStartLockFIFO(t *testing.T) {
	env := launchEnv(launchTestWorkspace, launchTestPane)
	t.Run("a FIFO at start.lock is refused within 5s", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "start.lock")
		_ = os.Remove(path)
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatalf("create the FIFO fixture: %v", err)
		}
		confirmed, reason := confirmLaunchWithin(t, store, "job-1", env, 5*time.Second)
		if confirmed || reason != "the start lock is not a private regular file" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if rec := recordState(t, dir); rec.State != launchPending {
			t.Fatalf("record state = %q", rec.State)
		}
	})
	t.Run("probeStartLock on a FIFO is refused within 5s", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "fifo.lock")
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatalf("create the FIFO fixture: %v", err)
		}
		pre, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		type outcome struct {
			held bool
			err  error
		}
		ch := make(chan outcome, 1)
		go func() {
			held, err := probeStartLock(path, pre)
			ch <- outcome{held, err}
		}()
		select {
		case res := <-ch:
			if res.held || !errors.Is(res.err, errLaunchFile) {
				t.Fatalf("held=%v err=%v, want errLaunchFile", res.held, res.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("probeStartLock blocked on a FIFO for 5s")
		}
	})
	t.Run("an unreadable mode is an I/O error, not a kind refusal", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Fatal("the mode 000 refusal cannot be proven as root: the open would succeed, so the I/O-error reason cannot be exercised")
		}
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "start.lock")
		_ = os.Remove(path)
		if err := os.WriteFile(path, nil, 0o000); err != nil {
			t.Fatalf("plant the unreadable lock: %v", err)
		}
		confirmed, reason := ConfirmLaunch(store, "job-1", env)
		if confirmed || reason != "the start lock is unreadable" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if rec := recordState(t, dir); rec.State != launchPending {
			t.Fatalf("record state = %q", rec.State)
		}
	})
}
