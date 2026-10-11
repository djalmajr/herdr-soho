package job

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// launch_startlock_test.go pins the supervisor's start.lock probe in
// ConfirmLaunch (probeStartLock): the probe never creates, follows or
// blocks on a path, and the friction reasons tell a busy lock, an unsafe
// path and an I/O error apart.

// TestConfirmLaunchStartLockProbe: a pending record is confirmed only when
// start.lock is a regular file whose lock is held; an absent or free lock
// is stale, and a non-regular lock is refused without confirming or
// creating anything.
func TestConfirmLaunchStartLockProbe(t *testing.T) {
	env := launchEnv(launchTestWorkspace, launchTestPane)
	t.Run("absent lock is stale and not created", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(filepath.Join(dir, "start.lock"))
		confirmed, reason := ConfirmLaunch(store, "job-1", env)
		if confirmed || !strings.Contains(reason, "stale") {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if _, err := os.Lstat(filepath.Join(dir, "start.lock")); !os.IsNotExist(err) {
			t.Fatalf("start.lock was created: %v", err)
		}
	})
	t.Run("a free regular lock is stale", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "start.lock")
		_ = os.Remove(path)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		confirmed, reason := ConfirmLaunch(store, "job-1", env)
		if confirmed || reason != "the launch is stale: its starter is gone" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if rec := recordState(t, dir); rec.State != launchPending {
			t.Fatalf("record state = %q", rec.State)
		}
	})
	t.Run("a held regular lock confirms", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		holdStartLock(t, dir)
		confirmed, reason := ConfirmLaunch(store, "job-1", env)
		if !confirmed || reason != "" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if rec := recordState(t, dir); rec.State != launchConfirmed {
			t.Fatalf("record state = %q", rec.State)
		}
	})
	t.Run("a directory is refused and the record is pending", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "start.lock")
		_ = os.Remove(path)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		confirmed, reason := ConfirmLaunch(store, "job-1", env)
		if confirmed || reason != "the start lock is not a private regular file" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if rec := recordState(t, dir); rec.State != launchPending {
			t.Fatalf("record state = %q", rec.State)
		}
	})
	t.Run("a symlink to a held regular file is refused", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "held.lock")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		targetFile, err := os.OpenFile(target, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := tryFlock(targetFile); err != nil {
			t.Fatalf("hold the target lock: %v", err)
		}
		t.Cleanup(func() { _ = targetFile.Close() })
		requireSymlink(t, target, filepath.Join(dir, "start.lock"))
		confirmed, reason := ConfirmLaunch(store, "job-1", env)
		if confirmed || reason != "the start lock is not a private regular file" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if rec := recordState(t, dir); rec.State != launchPending {
			t.Fatalf("record state = %q", rec.State)
		}
	})
	t.Run("a dangling symlink is refused and its target not created", func(t *testing.T) {
		store, clock, dir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "absent.lock")
		requireSymlink(t, target, filepath.Join(dir, "start.lock"))
		confirmed, reason := ConfirmLaunch(store, "job-1", env)
		if confirmed || reason != "the start lock is not a private regular file" {
			t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			t.Fatalf("the symlink target was created: %v", err)
		}
		if rec := recordState(t, dir); rec.State != launchPending {
			t.Fatalf("record state = %q", rec.State)
		}
	})
}

// TestProbeStartLockRefusals: the probe's identity and missing-path
// refusals, straight from probeStartLock.
func TestProbeStartLockRefusals(t *testing.T) {
	t.Run("a pre FileInfo of another file is refused", func(t *testing.T) {
		dir := t.TempDir()
		probe := filepath.Join(dir, "probe.lock")
		other := filepath.Join(dir, "other.lock")
		if err := os.WriteFile(probe, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(other, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		pre, err := os.Lstat(other)
		if err != nil {
			t.Fatal(err)
		}
		held, err := probeStartLock(probe, pre)
		if held || !errors.Is(err, errLaunchFile) {
			t.Fatalf("held=%v err=%v, want errLaunchFile", held, err)
		}
	})
	t.Run("a missing path is refused and not created", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "absent.lock")
		held, err := probeStartLock(path, nil)
		if held || err == nil {
			t.Fatalf("held=%v err=%v, want an error", held, err)
		}
		if !os.IsNotExist(err) {
			t.Fatalf("err=%v, want a not-exist error", err)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("the path was created: %v", err)
		}
	})
}

// TestConfirmLaunchLaunchLockDirectory: with launch.lock a directory the
// confirm fails at once with the launch-lock refusal, not the busy one.
func TestConfirmLaunchLaunchLockDirectory(t *testing.T) {
	store, clock, dir := launchFixture(t)
	if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, launchLockName)
	_ = os.Remove(lockPath)
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	confirmed, reason := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
	if confirmed || reason != "the launch lock is not a private regular file" {
		t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
	}
	if rec := recordState(t, dir); rec.State != launchPending {
		t.Fatalf("record state = %q", rec.State)
	}
}
