//go:build windows

package eval

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func lockOwnedCleanupFile(t *testing.T) (*os.File, func()) {
	t.Helper()
	file, err := os.Create(filepath.Join(t.TempDir(), ".herdr-eval-stdout"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := syscall.UTF16PtrFromString(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(path, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	closeLock := func() {
		once.Do(func() {
			if err := syscall.CloseHandle(handle); err != nil {
				t.Errorf("close owned test lock: %v", err)
			}
		})
	}
	t.Cleanup(closeLock)
	return file, closeLock
}

func TestOwnedOutputCleanupRetriesAReleasedNativeSharingLock(t *testing.T) {
	file, release := lockOwnedCleanupFile(t)
	done := make(chan struct{})
	go func() { time.Sleep(35 * time.Millisecond); release(); close(done) }()
	err := removeOwnedOutput(file)
	<-done
	if err != nil {
		t.Fatalf("released owned lock must allow deletion: %v", err)
	}
	if _, err := os.Stat(file.Name()); !os.IsNotExist(err) {
		t.Fatalf("owned output remains: %v", err)
	}
}

func TestOwnedOutputCleanupReportsAPersistentNativeSharingLock(t *testing.T) {
	file, release := lockOwnedCleanupFile(t)
	start := time.Now()
	err := removeOwnedOutput(file)
	if err == nil || !strings.Contains(err.Error(), "remove owned output file .herdr-eval-stdout") {
		t.Fatalf("missing cleanup failure: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("sharing retry exceeded the bounded return: %v", elapsed)
	}
	if _, err := os.Stat(file.Name()); err != nil {
		t.Fatalf("locked owned output unexpectedly absent: %v", err)
	}
	release()
	if err := removeOwnedOutput(file); err != nil {
		t.Fatalf("retry after release: %v", err)
	}
}

func TestOwnedOutputCleanupTreatsAlreadyMissingOwnedFilesAsClean(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), ".herdr-eval-stderr"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file.Name()); err != nil {
		t.Fatal(err)
	}
	if err := removeOwnedOutput(nil, file); err != nil {
		t.Fatalf("already removed owned output: %v", err)
	}
}
