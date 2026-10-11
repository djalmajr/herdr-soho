package job

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// launch_files_test.go pins the private-file defenses of launch.go: a
// launch.json or launch.lock that is not a private regular file at its own
// path is refused at once, never followed, never written through, and
// never makes a step wait.

// requireSymlink creates link -> target. The symlink guard is portable, so
// its coverage is too: a platform or run that cannot create the symlink
// fails the test instead of skipping it.
func requireSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create the symlink fixture: %v", err)
	}
}

func TestLaunchRecordMustBeAPrivateRegularFile(t *testing.T) {
	env := launchEnv(launchTestWorkspace, launchTestPane)
	t.Run("symlink is not followed or written through", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		rec, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
		if err != nil {
			t.Fatal(err)
		}
		holdStartLock(t, dir)
		// The real record moves outside; the path becomes a symlink to it.
		outside := filepath.Join(t.TempDir(), "elsewhere.json")
		if err := os.Rename(filepath.Join(dir, launchFileName), outside); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(outside)
		requireSymlink(t, outside, filepath.Join(dir, launchFileName))
		if _, _, err := readLaunch(dir); !errors.Is(err, errLaunchFile) {
			t.Fatalf("readLaunch err = %v, want errLaunchFile", err)
		}
		if confirmed, reason := ConfirmLaunch(store, "job-1", env); confirmed || reason != "the launch record is unreadable" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if got := checkLaunch(dir, clock, clock.Monotonic()+int64(time.Second), rec); got != launchLost {
			t.Fatalf("checkLaunch = %v, want launchLost", got)
		}
		if _, err := revokeLaunch(store, clock, rec); err == nil {
			t.Fatal("revokeLaunch through a symlink succeeded")
		}
		if fenceLaunch(store, "job-1") != launchUncertain {
			t.Fatal("fenceLaunch through a symlink is not uncertain")
		}
		after, _ := os.ReadFile(outside)
		if !bytes.Equal(before, after) {
			t.Fatalf("the symlink target changed:\n%s\n%s", before, after)
		}
	})
	t.Run("oversized record", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		holdStartLock(t, dir)
		path := filepath.Join(dir, launchFileName)
		data, _ := os.ReadFile(path)
		padded := append(bytes.TrimRight(data, "\n"), []byte(strings.Repeat(" ", maxLaunchBytes))...)
		if err := os.WriteFile(path, padded, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readLaunch(dir); !errors.Is(err, errLaunchFile) {
			t.Fatalf("readLaunch err = %v, want errLaunchFile", err)
		}
		if confirmed, _ := ConfirmLaunch(store, "job-1", env); confirmed {
			t.Fatal("an oversized record was confirmed")
		}
	})
	t.Run("directory at the record path", func(t *testing.T) {
		store, _, dir := launchFixture(t)
		if err := os.Mkdir(filepath.Join(dir, launchFileName), 0o700); err != nil {
			t.Fatal(err)
		}
		holdStartLock(t, dir)
		if confirmed, reason := ConfirmLaunch(store, "job-1", env); confirmed || reason != "the launch record is unreadable" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if fenceLaunch(store, "job-1") != launchUncertain {
			t.Fatal("fenceLaunch on a directory is not uncertain")
		}
	})
}

func TestLaunchLockMustBeAPrivateRegularFile(t *testing.T) {
	cases := map[string]func(t *testing.T, path string){
		"symlink": func(t *testing.T, path string) {
			requireSymlink(t, filepath.Join(t.TempDir(), "target.lock"), path)
		},
		"directory": func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"oversized": func(t *testing.T, path string) {
			if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxLaunchBytes+1), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, plant := range cases {
		t.Run(name, func(t *testing.T) {
			store, clock, dir := launchFixture(t)
			lockPath := filepath.Join(dir, launchLockName)
			plant(t, lockPath)
			start := time.Now()
			ran := false
			err := withLaunchLock(dir, clock, clock.Monotonic()+int64(time.Hour), func() error { ran = true; return nil })
			if err == nil || errors.Is(err, errLaunchLockBusy) || ran {
				t.Fatalf("withLaunchLock err=%v ran=%v, want an immediate refusal", err, ran)
			}
			if clock.sleeps != 0 || time.Since(start) > 2*time.Second {
				t.Fatalf("the refusal waited: sleeps=%d wall=%v", clock.sleeps, time.Since(start))
			}
			if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err == nil {
				t.Fatal("issueLaunch succeeded with a planted lock")
			}
			if _, err := os.Lstat(filepath.Join(dir, launchFileName)); !os.IsNotExist(err) {
				t.Fatalf("a record was written: %v", err)
			}
			if name == "symlink" {
				target, _ := os.Readlink(lockPath)
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Fatalf("the symlink target was created: %v", err)
				}
			}
		})
	}
}

// TestConfirmLaunchStartLockKinds: an absent start.lock is a stale launch
// (no lock file is created), a non-regular one is never tried.
func TestConfirmLaunchStartLockKinds(t *testing.T) {
	env := launchEnv(launchTestWorkspace, launchTestPane)
	t.Run("absent", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(filepath.Join(dir, "start.lock"))
		if confirmed, reason := ConfirmLaunch(store, "job-1", env); confirmed || !strings.Contains(reason, "stale") {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if _, err := os.Lstat(filepath.Join(dir, "start.lock")); !os.IsNotExist(err) {
			t.Fatalf("start.lock was created: %v", err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(filepath.Join(dir, "start.lock"))
		if err := os.Mkdir(filepath.Join(dir, "start.lock"), 0o700); err != nil {
			t.Fatal(err)
		}
		if confirmed, reason := ConfirmLaunch(store, "job-1", env); confirmed || reason != "the start lock is unreadable" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if rec := recordState(t, dir); rec.State != launchPending {
			t.Fatalf("record state = %q", rec.State)
		}
	})
}

// TestAwaitLaunchAcceptsAConfirmationThatWinsTheRevoke: a confirmation
// written after the nominal wait but before the revoke's critical section
// is accepted (the linearization point is the revoke, not the deadline);
// one written after the revoke is not.
func TestAwaitLaunchAcceptsAConfirmationThatWinsTheRevoke(t *testing.T) {
	store, clock, dir := launchFixture(t)
	rec, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
	if err != nil {
		t.Fatal(err)
	}
	holdStartLock(t, dir)
	wait, poll := time.Second, 100*time.Millisecond
	start := clock.Monotonic()
	clock.onSleep = func(_ int, _ time.Duration) {
		// Confirm only once the nominal wait has passed: the last poll's
		// sleep crosses the deadline, then the revoke takes the lock.
		if clock.Monotonic()-start >= int64(wait) {
			_, _ = ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
		}
	}
	accepted, err := awaitLaunch(store, clock, rec, wait, poll)
	if err != nil || !accepted {
		t.Fatalf("accepted=%v err=%v, want the confirmation that won the revoke", accepted, err)
	}
	if got := recordState(t, dir); got.State != launchConfirmed {
		t.Fatalf("record state = %q", got.State)
	}
}
