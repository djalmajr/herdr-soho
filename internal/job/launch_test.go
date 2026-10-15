package job

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/soho/internal/platform"
)

// The launch tests below pin the private startup confirmation in launch.go
// (the DJA-261 T2 expected-result items 1-9). They run against
// launchTestClock, which accumulates the requested sleeps as the monotonic
// reading: every wait ends deterministically and no test takes a real pause.

// launchTestClock accumulates the requested sleeps as the monotonic reading
// (nanoseconds) and runs the optional hook on each Sleep, so a confirmation
// can be landed between two reads of the record.
type launchTestClock struct {
	now     time.Time
	mono    int64
	sleeps  int
	onSleep func(n int, d time.Duration)
}

func (c *launchTestClock) Now() time.Time   { return c.now }
func (c *launchTestClock) Monotonic() int64 { return c.mono }
func (c *launchTestClock) Sleep(d time.Duration) {
	if d > 0 {
		c.mono += int64(d)
		c.now = c.now.Add(d)
	}
	c.sleeps++
	if c.onSleep != nil {
		c.onSleep(c.sleeps, d)
	}
}

const (
	launchTestWorkspace = "w1"
	launchTestPane      = "p1"
	// launchHangGuard is the wall-clock hang guard of
	// TestLaunchLockBusyBounded: it fails only when a launch step blocks
	// far beyond the bounded launch-lock retries, whatever the node load.
	// It is not the performance bound: that is the exact test-clock window
	// the test asserts.
	launchHangGuard = 30 * time.Second
)

// launchFixture creates a store holding job-1 (supTestBrief) with the test
// clock the launch steps must use.
func launchFixture(t *testing.T) (*Store, *launchTestClock, string) {
	t.Helper()
	root := t.TempDir()
	clock := &launchTestClock{now: time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC)}
	store := Open(root)
	store.Clock = clock
	if _, err := store.Start("job-1", []byte(supTestBrief)); err != nil {
		t.Fatal(err)
	}
	jobDir, err := Dir(root, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	return store, clock, jobDir
}

// launchEnv builds the supervisor's environment; an empty value leaves the
// variable unset.
func launchEnv(workspace, pane string) platform.Env {
	env := platform.Env{}
	if workspace != "" {
		env["HERDR_WORKSPACE_ID"] = workspace
	}
	if pane != "" {
		env["HERDR_PANE_ID"] = pane
	}
	return env
}

// holdStartLock holds the job's start.lock for the rest of the test, as the
// live starter does.
func holdStartLock(t *testing.T, dir string) {
	t.Helper()
	lock, err := tryStartLock(dir)
	if err != nil {
		t.Fatalf("hold the start lock: %v", err)
	}
	t.Cleanup(func() { closeStartLock(lock) })
}

// launchFile reads the raw record bytes.
func launchFile(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, launchFileName))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// recordState reads and parses the record.
func recordState(t *testing.T, dir string) launchRecord {
	t.Helper()
	rec, ok, err := readLaunch(dir)
	if !ok || err != nil {
		t.Fatalf("readLaunch: ok=%v err=%v", ok, err)
	}
	return rec
}

// setLaunchState rewrites the record with the same fields and a new state.
func setLaunchState(t *testing.T, dir, state string) {
	t.Helper()
	rec := recordState(t, dir)
	rec.State = state
	if state == launchConfirmed {
		rec.PID = os.Getpid()
	}
	if err := writeLaunch(dir, rec); err != nil {
		t.Fatal(err)
	}
}

// copyLaunchFile copies the raw record into another job dir or store.
func copyLaunchFile(t *testing.T, fromDir, toDir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fromDir, launchFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toDir, launchFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertLaunchBytesUnchanged(t *testing.T, dir string, before []byte) {
	t.Helper()
	if after := launchFile(t, dir); string(after) != string(before) {
		t.Fatalf("record bytes changed:\nbefore %s\nafter  %s", before, after)
	}
}

// TestLaunchHappyPath is the expected-result item 1: issue, the starter
// holds start.lock, the supervisor confirms from its own workspace and
// pane, and the starter's await reads the confirmation without sleeping.
func TestLaunchHappyPath(t *testing.T) {
	store, clock, jobDir := launchFixture(t)
	rec, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != launchPending || rec.Nonce == "" {
		t.Fatalf("issued record: %+v", rec)
	}
	if got := recordState(t, jobDir); got.State != launchPending || got.Nonce != rec.Nonce {
		t.Fatalf("file record: %+v", got)
	}
	holdStartLock(t, jobDir)
	confirmed, reason := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
	if !confirmed || reason != "" {
		t.Fatalf("ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
	}
	got := recordState(t, jobDir)
	if got.State != launchConfirmed || got.Nonce != rec.Nonce || got.PID != os.Getpid() || got.Confirmed == "" {
		t.Fatalf("confirmed record: %+v", got)
	}
	mono := clock.Monotonic()
	accepted, err := awaitLaunch(store, clock, rec, defaultLaunchWait, defaultLaunchPoll)
	if !accepted || err != nil {
		t.Fatalf("awaitLaunch: accepted=%v err=%v", accepted, err)
	}
	if clock.Monotonic() != mono || clock.sleeps != 0 {
		t.Fatalf("awaitLaunch slept: mono %d->%d sleeps=%d", mono, clock.Monotonic(), clock.sleeps)
	}
}

// TestConfirmLaunchNoRecordAndConfirmedNoOp is the expected-result item 2:
// with no record the confirm changes nothing and creates no file; on a
// confirmed record it refuses with an empty reason and leaves the bytes
// (duplicate and restart confirmations preserve the first one).
func TestConfirmLaunchNoRecordAndConfirmedNoOp(t *testing.T) {
	t.Run("no record", func(t *testing.T) {
		store, _, jobDir := launchFixture(t)
		confirmed, reason := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
		if confirmed || reason != "" {
			t.Fatalf("ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
		}
		if _, err := os.Lstat(filepath.Join(jobDir, launchFileName)); !os.IsNotExist(err) {
			t.Fatalf("the launch file was created: %v", err)
		}
	})
	t.Run("confirmed record is a no-op", func(t *testing.T) {
		store, clock, jobDir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		holdStartLock(t, jobDir)
		if confirmed, reason := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane)); !confirmed || reason != "" {
			t.Fatalf("first ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
		}
		before := launchFile(t, jobDir)
		confirmed, reason := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
		if confirmed || reason != "" {
			t.Fatalf("second ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
		}
		assertLaunchBytesUnchanged(t, jobDir, before)
	})
}

// TestConfirmLaunchMismatchLeavesRecordUnchanged is the expected-result
// item 3: every mismatch refuses the confirm with a non-empty reason and
// leaves the record byte for byte.
func TestConfirmLaunchMismatchLeavesRecordUnchanged(t *testing.T) {
	cases := []struct {
		name   string
		env    platform.Env
		reason string
	}{
		{"another workspace", launchEnv("w2", launchTestPane), "the launch belongs to another workspace or pane"},
		{"another pane", launchEnv(launchTestWorkspace, "p2"), "the launch belongs to another workspace or pane"},
		{"empty pane id", launchEnv(launchTestWorkspace, ""), "the launch belongs to another workspace or pane"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, clock, jobDir := launchFixture(t)
			if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
				t.Fatal(err)
			}
			holdStartLock(t, jobDir)
			before := launchFile(t, jobDir)
			confirmed, reason := ConfirmLaunch(store, "job-1", tc.env)
			if confirmed || reason == "" {
				t.Fatalf("ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
			}
			if reason != tc.reason {
				t.Fatalf("reason %q, want %q", reason, tc.reason)
			}
			assertLaunchBytesUnchanged(t, jobDir, before)
		})
	}

	t.Run("another job id", func(t *testing.T) {
		store, clock, jobDir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Start("job-2", []byte(strings.Replace(supTestBrief, "job-1", "job-2", 1))); err != nil {
			t.Fatal(err)
		}
		otherDir, err := Dir(store.Root, "job-2")
		if err != nil {
			t.Fatal(err)
		}
		copyLaunchFile(t, jobDir, otherDir)
		before := launchFile(t, otherDir)
		confirmed, reason := ConfirmLaunch(store, "job-2", launchEnv(launchTestWorkspace, launchTestPane))
		if confirmed || reason == "" {
			t.Fatalf("ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
		}
		if reason != "the launch belongs to another job or store" {
			t.Fatalf("reason %q, want the job or store refusal", reason)
		}
		assertLaunchBytesUnchanged(t, otherDir, before)
	})

	t.Run("another store root", func(t *testing.T) {
		store, clock, jobDir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		root2 := t.TempDir()
		store2 := Open(root2)
		if _, err := store2.Start("job-1", []byte(supTestBrief)); err != nil {
			t.Fatal(err)
		}
		otherDir, err := Dir(root2, "job-1")
		if err != nil {
			t.Fatal(err)
		}
		copyLaunchFile(t, jobDir, otherDir)
		before := launchFile(t, otherDir)
		confirmed, reason := ConfirmLaunch(store2, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
		if confirmed || reason == "" {
			t.Fatalf("ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
		}
		if reason != "the launch belongs to another job or store" {
			t.Fatalf("reason %q, want the job or store refusal", reason)
		}
		assertLaunchBytesUnchanged(t, otherDir, before)
	})

	t.Run("corrupt json", func(t *testing.T) {
		store, clock, jobDir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		holdStartLock(t, jobDir)
		if err := os.WriteFile(filepath.Join(jobDir, launchFileName), []byte("{not json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := launchFile(t, jobDir)
		confirmed, reason := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
		if confirmed || reason == "" {
			t.Fatalf("ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
		}
		if reason != "the launch record is unreadable" {
			t.Fatalf("reason %q, want the unreadable refusal", reason)
		}
		assertLaunchBytesUnchanged(t, jobDir, before)
	})

	t.Run("stale starter", func(t *testing.T) {
		store, clock, jobDir := launchFixture(t)
		if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
			t.Fatal(err)
		}
		// start.lock is not held: the starter is gone.
		before := launchFile(t, jobDir)
		confirmed, reason := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
		if confirmed || reason == "" {
			t.Fatalf("ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
		}
		if reason != "the launch is stale: its starter is gone" {
			t.Fatalf("reason %q, want the stale refusal", reason)
		}
		assertLaunchBytesUnchanged(t, jobDir, before)
	})
}

// TestAwaitLaunchExpiresAndRevokes is the expected-result item 4: without a
// confirmation the await ends at the monotonic deadline (the clock advanced
// at least wait and at most wait+poll), revokes the record, and a later
// confirm cannot confirm it.
func TestAwaitLaunchExpiresAndRevokes(t *testing.T) {
	store, clock, jobDir := launchFixture(t)
	rec, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
	if err != nil {
		t.Fatal(err)
	}
	holdStartLock(t, jobDir)
	wait, poll := defaultLaunchWait, defaultLaunchPoll
	accepted, err := awaitLaunch(store, clock, rec, wait, poll)
	if accepted || err != nil {
		t.Fatalf("awaitLaunch: accepted=%v err=%v", accepted, err)
	}
	mono := clock.Monotonic()
	if mono < int64(wait) || mono > int64(wait)+int64(poll) {
		t.Fatalf("the wait ended at %v, want at least %v and at most %v", time.Duration(mono), wait, wait+poll)
	}
	got := recordState(t, jobDir)
	if got.State != launchRevoked || got.Nonce != rec.Nonce {
		t.Fatalf("revoked record: %+v", got)
	}
	confirmed, _ := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
	if confirmed {
		t.Fatal("a revoked record was confirmed")
	}
	if got := recordState(t, jobDir); got.State != launchRevoked {
		t.Fatalf("record state after the late confirm: %+v", got)
	}
}

// TestAwaitLaunchRaceLinearization is the expected-result item 5: a
// confirmation that lands during the wait is accepted on the next read
// before the deadline, and a confirmation attempted after the revoke
// cannot confirm.
func TestAwaitLaunchRaceLinearization(t *testing.T) {
	t.Run("confirmation during the wait", func(t *testing.T) {
		store, clock, jobDir := launchFixture(t)
		rec, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
		if err != nil {
			t.Fatal(err)
		}
		holdStartLock(t, jobDir)
		var hookCalls int
		var hookConfirmed bool
		var hookReason string
		// The supervisor confirms on the 3rd sleep, in the pause between
		// two reads of the record.
		clock.onSleep = func(n int, d time.Duration) {
			if n != 3 {
				return
			}
			hookCalls++
			hookConfirmed, hookReason = ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
		}
		wait, poll := time.Second, 100*time.Millisecond
		accepted, err := awaitLaunch(store, clock, rec, wait, poll)
		if !accepted || err != nil {
			t.Fatalf("awaitLaunch: accepted=%v err=%v", accepted, err)
		}
		if hookCalls != 1 || !hookConfirmed || hookReason != "" {
			t.Fatalf("hook: calls=%d confirmed=%v reason=%q", hookCalls, hookConfirmed, hookReason)
		}
		if clock.Monotonic() >= int64(wait) {
			t.Fatalf("the await did not notice the confirmation before the deadline: mono=%v", time.Duration(clock.Monotonic()))
		}
		got := recordState(t, jobDir)
		if got.State != launchConfirmed || got.Nonce != rec.Nonce {
			t.Fatalf("confirmed record: %+v", got)
		}
	})
	t.Run("confirmation after the revoke", func(t *testing.T) {
		store, clock, jobDir := launchFixture(t)
		rec, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
		if err != nil {
			t.Fatal(err)
		}
		holdStartLock(t, jobDir)
		wait, poll := 300*time.Millisecond, 100*time.Millisecond
		accepted, err := awaitLaunch(store, clock, rec, wait, poll)
		if accepted || err != nil {
			t.Fatalf("awaitLaunch: accepted=%v err=%v", accepted, err)
		}
		confirmed, _ := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
		if confirmed {
			t.Fatal("a revoked record was confirmed")
		}
		if got := recordState(t, jobDir); got.State != launchRevoked {
			t.Fatalf("record state after the late confirm: %+v", got)
		}
	})
}

// TestIssueLaunchReplacesStaleConfirmation is the expected-result item 6:
// a fresh issue writes a pending record with a different nonce, so the new
// await never accepts the old confirmation, and the old starter's record
// can neither classify nor revoke the new one.
func TestIssueLaunchReplacesStaleConfirmation(t *testing.T) {
	store, clock, jobDir := launchFixture(t)
	old, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
	if err != nil {
		t.Fatal(err)
	}
	holdStartLock(t, jobDir)
	if confirmed, reason := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane)); !confirmed || reason != "" {
		t.Fatalf("first ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
	}
	fresh, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.State != launchPending || fresh.Nonce == old.Nonce {
		t.Fatalf("fresh record: %+v (old %+v)", fresh, old)
	}
	got := recordState(t, jobDir)
	if got.State != launchPending || got.Nonce != fresh.Nonce {
		t.Fatalf("the old confirmation was not replaced: %+v", got)
	}
	wait, poll := 300*time.Millisecond, 100*time.Millisecond
	accepted, err := awaitLaunch(store, clock, fresh, wait, poll)
	if accepted || err != nil {
		t.Fatalf("the new await accepted the old confirmation: accepted=%v err=%v", accepted, err)
	}
	got = recordState(t, jobDir)
	if got.State != launchRevoked || got.Nonce != fresh.Nonce {
		t.Fatalf("the new record after the await: %+v", got)
	}
	// The old starter's record can no longer fence the new one.
	if out := checkLaunch(jobDir, clock, clock.Monotonic()+int64(time.Second), old); out != launchLost {
		t.Fatalf("checkLaunch(old) = %v, want launchLost", out)
	}
	if accepted, err := revokeLaunch(store, clock, old); accepted || err == nil {
		t.Fatalf("revokeLaunch(old): accepted=%v err=%v, want the replaced-record refusal", accepted, err)
	}
	if got := recordState(t, jobDir); got.State != launchRevoked || got.Nonce != fresh.Nonce {
		t.Fatalf("the new record after the old revoke: %+v", got)
	}
}

// TestLaunchLockBusyBounded is the expected-result item 7: while
// launch.lock is held by a separate descriptor, every launch step is
// bounded on the monotonic clock and does not block the real wall clock.
func TestLaunchLockBusyBounded(t *testing.T) {
	store, clock, jobDir := launchFixture(t)
	rec, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
	if err != nil {
		t.Fatal(err)
	}
	// Hold launch.lock through a separate descriptor: flock is per open
	// file, so it conflicts with the withLaunchLock tries in this process.
	// The record must be issued before the contention; the hold covers the
	// three bounded reads below.
	lockFile, err := os.OpenFile(filepath.Join(jobDir, launchLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := tryFlock(lockFile); err != nil {
		t.Fatalf("hold the launch lock: %v", err)
	}
	t.Cleanup(func() { _ = lockFile.Close() })

	wait, poll := defaultLaunchWait, defaultLaunchPoll
	mono0 := clock.Monotonic()
	started := time.Now()
	accepted, err := awaitLaunch(store, clock, rec, wait, poll)
	if wall := time.Since(started); wall >= launchHangGuard {
		t.Fatalf("awaitLaunch blocked %v", wall)
	}
	if accepted || err == nil {
		t.Fatalf("awaitLaunch: accepted=%v err=%v, want the unfenced refusal", accepted, err)
	}
	elapsed := clock.Monotonic() - mono0
	lower := int64(wait) + int64(launchRevokeWait)
	upper := lower + 2*int64(launchLockRetry)
	if elapsed < lower || elapsed >= upper {
		t.Fatalf("the await ended at %v on the test clock, want within [%v, %v)", time.Duration(elapsed), time.Duration(lower), time.Duration(upper))
	}
	started = time.Now()
	confirmed, reason := ConfirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane))
	if wall := time.Since(started); wall >= launchHangGuard {
		t.Fatalf("ConfirmLaunch blocked %v", wall)
	}
	if confirmed || !strings.Contains(reason, "busy") {
		t.Fatalf("ConfirmLaunch: confirmed=%v reason=%q", confirmed, reason)
	}
	started = time.Now()
	fence := fenceLaunch(store, "job-1")
	if wall := time.Since(started); wall >= launchHangGuard {
		t.Fatalf("fenceLaunch blocked %v", wall)
	}
	if fence != launchUncertain {
		t.Fatalf("fenceLaunch = %v, want launchUncertain", fence)
	}
	got := recordState(t, jobDir)
	if got.State != launchPending || got.Nonce != rec.Nonce {
		t.Fatalf("the contention changed the record: %+v", got)
	}
}

// TestFenceLaunch is the expected-result item 8: crash recovery's fence per
// record state.
func TestFenceLaunch(t *testing.T) {
	cases := []struct {
		name           string
		setup          func(t *testing.T, store *Store, clock *launchTestClock, dir string)
		want           launchFence
		wantFile       bool
		wantState      string
		bytesUnchanged bool
	}{
		{
			name:     "no record",
			setup:    func(t *testing.T, store *Store, clock *launchTestClock, dir string) {},
			want:     launchNone,
			wantFile: false,
		},
		{
			name: "pending becomes revoked",
			setup: func(t *testing.T, store *Store, clock *launchTestClock, dir string) {
				if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
					t.Fatal(err)
				}
			},
			want:      launchFenced,
			wantFile:  true,
			wantState: launchRevoked,
		},
		{
			name: "revoked stays fenced",
			setup: func(t *testing.T, store *Store, clock *launchTestClock, dir string) {
				if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
					t.Fatal(err)
				}
				setLaunchState(t, dir, launchRevoked)
			},
			want:           launchFenced,
			wantFile:       true,
			bytesUnchanged: true,
		},
		{
			name: "confirmed is uncertain",
			setup: func(t *testing.T, store *Store, clock *launchTestClock, dir string) {
				if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
					t.Fatal(err)
				}
				setLaunchState(t, dir, launchConfirmed)
			},
			want:           launchUncertain,
			wantFile:       true,
			bytesUnchanged: true,
		},
		{
			name: "corrupt is uncertain",
			setup: func(t *testing.T, store *Store, clock *launchTestClock, dir string) {
				if err := os.WriteFile(filepath.Join(dir, launchFileName), []byte("{not json\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want:           launchUncertain,
			wantFile:       true,
			bytesUnchanged: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, clock, jobDir := launchFixture(t)
			tc.setup(t, store, clock, jobDir)
			var before []byte
			if tc.wantFile {
				before = launchFile(t, jobDir)
			}
			if got := fenceLaunch(store, "job-1"); got != tc.want {
				t.Fatalf("fenceLaunch = %v, want %v", got, tc.want)
			}
			if !tc.wantFile {
				if _, err := os.Lstat(filepath.Join(jobDir, launchFileName)); !os.IsNotExist(err) {
					t.Fatalf("the fence created a file: %v", err)
				}
				return
			}
			if tc.bytesUnchanged {
				assertLaunchBytesUnchanged(t, jobDir, before)
			}
			if tc.wantState != "" {
				if got := recordState(t, jobDir); got.State != tc.wantState {
					t.Fatalf("record state after the fence: %+v, want %s", got, tc.wantState)
				}
			}
		})
	}
}
