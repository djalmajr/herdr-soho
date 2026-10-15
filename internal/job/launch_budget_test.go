package job

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
)

// launch_budget_test.go: the launch record's bounded read is sized for
// every field at its cap, so a confirmation with two valid locators at
// maxLocatorBytes is written and read back; a field over its cap is never
// written.

// fullRecord is a record with every string field at its cap, made of
// bytes json.Marshal escapes to the longest form it writes.
func fullRecord() launchRecord {
	path := func(n int, fill string) string { return "/" + strings.Repeat(fill, n-1) }
	token := strings.Repeat("<", launchTokenMax)
	return launchRecord{
		V: launchVersion, ID: token, Store: path(launchSystemPathMax, "\x01"), Workspace: token, Pane: token,
		Nonce: token, Issued: token, State: token, PID: math.MaxInt, Confirmed: token,
		Started: token, Exe: path(launchSystemPathMax, "<"),
		ConfigBase: path(maxLocatorBytes, "<"), Home: path(maxLocatorBytes, "&"),
	}
}

func TestLaunchRecordBudget(t *testing.T) {
	t.Run("the fixed overhead is within launchRecordOverhead", func(t *testing.T) {
		data, err := json.Marshal(launchRecord{V: math.MaxInt, PID: math.MaxInt})
		if err != nil {
			t.Fatal(err)
		}
		// Empty omitempty fields are dropped by Marshal; add back each
		// dropped key with its quotes, colon and comma.
		dropped := 0
		for _, key := range []string{"confirmed", "started", "exe", "config_base", "home"} {
			dropped += len(`,"` + key + `":""`)
		}
		if got := len(data) + 1 + dropped; got > launchRecordOverhead {
			t.Fatalf("overhead %d > launchRecordOverhead %d", got, launchRecordOverhead)
		}
	})
	t.Run("every field at its cap is written and read back", func(t *testing.T) {
		dir := t.TempDir()
		rec := fullRecord()
		if err := writeLaunch(dir, rec); err != nil {
			t.Fatalf("writeLaunch: %v", err)
		}
		info, err := os.Stat(filepath.Join(dir, launchFileName))
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > maxLaunchRecordBytes || info.Size() <= 4096 {
			t.Fatalf("record size %d, want within (4096, %d]", info.Size(), maxLaunchRecordBytes)
		}
		got, ok, err := readLaunch(dir)
		if err != nil || !ok || got != rec {
			t.Fatalf("readLaunch ok=%v err=%v equal=%v", ok, err, got == rec)
		}
	})
	t.Run("a field over its cap is never written", func(t *testing.T) {
		over := []func(*launchRecord){
			func(r *launchRecord) { r.ConfigBase += "x" },
			func(r *launchRecord) { r.Home += "x" },
			func(r *launchRecord) { r.Store += "x" },
			func(r *launchRecord) { r.Exe += "x" },
			func(r *launchRecord) { r.ID += "x" },
			func(r *launchRecord) { r.Workspace += "x" },
			func(r *launchRecord) { r.Pane += "x" },
			func(r *launchRecord) { r.Nonce += "x" },
			func(r *launchRecord) { r.Issued += "x" },
			func(r *launchRecord) { r.State += "x" },
			func(r *launchRecord) { r.Confirmed += "x" },
			func(r *launchRecord) { r.Started += "x" },
		}
		for i, edit := range over {
			dir := t.TempDir()
			rec := fullRecord()
			edit(&rec)
			if err := writeLaunch(dir, rec); err != errLaunchRecordTooLarge {
				t.Fatalf("field %d: writeLaunch err = %v, want errLaunchRecordTooLarge", i, err)
			}
			if _, err := os.Lstat(filepath.Join(dir, launchFileName)); !os.IsNotExist(err) {
				t.Fatalf("field %d: a record over its cap was written: %v", i, err)
			}
		}
	})
	t.Run("a file over the record cap is refused", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, launchFileName), []byte(strings.Repeat(" ", maxLaunchRecordBytes+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readLaunch(dir); err != errLaunchFile {
			t.Fatalf("readLaunch err = %v, want errLaunchFile", err)
		}
	})
}

// TestConfirmLaunchLongLocators: a confirmation that records two valid
// locators of maxLocatorBytes each (with characters json.Marshal escapes)
// is written, read back and accepted by the starter.
func TestConfirmLaunchLongLocators(t *testing.T) {
	store, clock, dir := launchFixture(t)
	want, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane)
	if err != nil {
		t.Fatal(err)
	}
	holdStartLock(t, dir)
	root := nativeRoot(t)
	sel := core.UserSelection{Base: root + strings.Repeat("<", maxLocatorBytes-len(root)), Home: root + strings.Repeat("&", maxLocatorBytes-len(root))}
	if !usableLocator(sel.Base) || !usableLocator(sel.Home) {
		t.Fatal("the fixture locators are not usable")
	}
	bind := launchBinding{Started: "darwin:1.1", Exe: "/" + strings.Repeat("\"", 4095), Sel: sel}
	if confirmed, reason := confirmLaunch(store, "job-1", launchEnv(launchTestWorkspace, launchTestPane), bind); !confirmed || reason != "" {
		t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
	}
	rec := recordState(t, dir)
	if rec.ConfigBase != sel.Base || rec.Home != sel.Home || rec.Exe != bind.Exe {
		t.Fatal("the long fields did not round-trip")
	}
	if outcome := checkLaunch(dir, clock, clock.Monotonic()+int64(launchRevokeWait), want); outcome != launchAccepted {
		t.Fatalf("checkLaunch = %v, want accepted", outcome)
	}
}
