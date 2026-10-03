package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testCopyRow(path string) CopyRow {
	return CopyRow{Path: path, Owner: "-", Pane: "-", Created: "2026-10-03T00:00:00Z", Source: "-", Origin: "mutation-copy"}
}

func writeCopiesLock(t *testing.T, sd string, age time.Duration) string {
	t.Helper()
	lock := filepath.Join(sd, "copies.tsv.lock")
	if err := os.WriteFile(lock, []byte(fmt.Sprintf("%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))), 0o600); err != nil {
		t.Fatal(err)
	}
	if age > 0 {
		old := time.Now().Add(-age)
		if err := os.Chtimes(lock, old, old); err != nil {
			t.Fatal(err)
		}
	}
	return lock
}

// The review case: 32 concurrent registrations must leave 32 lines, no lost
// update.
func TestUpsertCopiesLocksTheRegistry(t *testing.T) {
	sd := t.TempDir()
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- UpsertCopies(sd, testCopyRow(fmt.Sprintf("/copy/%d", i)))
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	rows, err := ReadCopies(sd)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 32 {
		t.Fatalf("retained rows=%d, want 32 (no lost update under the lock)", len(rows))
	}
}

func TestLockTakesAStaleLockAndTimesOutOnALiveOne(t *testing.T) {
	t.Run("a lock older than 60 s is taken as abandoned", func(t *testing.T) {
		sd := t.TempDir()
		writeCopiesLock(t, sd, 2*time.Hour)
		if err := UpsertCopies(sd, testCopyRow("/copy/stale")); err != nil {
			t.Fatalf("the stale lock blocked the registration: %v", err)
		}
		if _, err := os.Stat(filepath.Join(sd, "copies.tsv.lock")); !os.IsNotExist(err) {
			t.Fatalf("the lock file remains after the unlock (stat err=%v)", err)
		}
	})
	t.Run("a live lock past the timeout comes back as the locked error and writes nothing", func(t *testing.T) {
		sd := t.TempDir()
		lock := writeCopiesLock(t, sd, 0)
		old := CopiesLockTimeout
		CopiesLockTimeout = 300 * time.Millisecond
		t.Cleanup(func() { CopiesLockTimeout = old })
		err := UpsertCopies(sd, testCopyRow("/copy/locked"))
		var locked *CopiesLockedError
		if !errors.As(err, &locked) || locked.Path != lock {
			t.Fatalf("err=%v, want the locked error for %s", err, lock)
		}
		want := fmt.Sprintf("copies: registry is locked by another herdr-soho (%s)", lock)
		if err.Error() != want {
			t.Fatalf("message=%q, want %q", err.Error(), want)
		}
		rows, err := ReadCopies(sd)
		if err != nil || len(rows) != 0 {
			t.Fatalf("rows=%#v err=%v, want nothing written", rows, err)
		}
		if _, err := os.Stat(lock); err != nil {
			t.Fatalf("the held lock was touched: %v", err)
		}
	})
}

// DropCopiesLines re-reads the registry under the lock: it drops exactly the
// given paths and keeps the lines that were added meanwhile.
func TestDropCopiesLinesKeepsTheLinesAddedMeanwhile(t *testing.T) {
	sd := t.TempDir()
	if err := UpsertCopies(sd, testCopyRow("/copy/one")); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCopies(sd, testCopyRow("/copy/two")); err != nil {
		t.Fatal(err)
	}
	// A line added between the caller's snapshot and the re-write: it must
	// survive.
	if err := UpsertCopies(sd, testCopyRow("/copy/three")); err != nil {
		t.Fatal(err)
	}
	if err := DropCopiesLines(sd, map[string]bool{"/copy/one": true}); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadCopies(sd)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, row := range rows {
		got[row.Path] = true
	}
	if len(rows) != 2 || !got["/copy/two"] || !got["/copy/three"] {
		t.Fatalf("rows=%#v, want /copy/two and /copy/three only", rows)
	}
}
