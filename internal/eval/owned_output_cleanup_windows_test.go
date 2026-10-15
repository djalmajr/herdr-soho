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
	// The persistent lock runs the removal to the bounded window
	// (winCaptureRemovalRetryWindow) and returns the error, never an
	// unbounded wait: the bound is the designed window plus margin.
	if elapsed := time.Since(start); elapsed > winCaptureRemovalRetryWindow+2*time.Second {
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

// The source fix for the post-cancel capture race: the owned output capture
// must be opened with FILE_SHARE_DELETE so the path-based removal passes —
// as a pending delete — while the handle is still held, exactly the window
// in which a dying child's inherited handle used to block the removal after
// the ActiveProcesses==0 drain proof. A capture opened with the old stdlib
// sharing (FILE_SHARE_READ|FILE_SHARE_WRITE) fails the removal below with
// the native sharing violation, so this test is the deterministic guard of
// the sharing flag, not of the rare kernel-timing race.
func TestOwnedCaptureOpensWithDeleteSharing(t *testing.T) {
	scratch := t.TempDir()
	file, err := openOwnedCapture(scratch, winStdoutFileName)
	if err != nil {
		t.Fatalf("open the owned capture: %v", err)
	}
	// The capture is a writable stream: write through it before the
	// removal.
	if _, err := file.Write([]byte("captured output")); err != nil {
		t.Fatalf("write through the owned capture: %v", err)
	}
	// Path-based removal while the handle is still held: FILE_SHARE_DELETE
	// makes this a pending delete that passes; without delete sharing it
	// fails here with the native sharing violation.
	if err := os.Remove(file.Name()); err != nil {
		t.Fatalf("removal with the owned handle still held: %v (the capture sharing lacks FILE_SHARE_DELETE)", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the owned capture: %v", err)
	}
	// After the last handle closes the pending-delete entry is gone.
	if _, err := os.Stat(file.Name()); !os.IsNotExist(err) {
		t.Fatalf("owned capture still present after the last handle closed: %v", err)
	}
}

// TestExtendedCapturePathMirrorsTheStdlibLongPathRule pins the path the
// capture's CreateFile receives: os.OpenFile used to extend a long path
// itself (fixLongPath), and the direct CreateFile call has to keep doing so
// on a machine without long-path support.
func TestExtendedCapturePathMirrorsTheStdlibLongPathRule(t *testing.T) {
	long := strings.Repeat("d", winShortPathLimit)
	cases := []struct{ name, in, want string }{
		{"short absolute", `C:\scratch\.herdr-eval-stdout`, `C:\scratch\.herdr-eval-stdout`},
		{"long absolute", `C:\` + long + `\.herdr-eval-stdout`, `\\?\C:\` + long + `\.herdr-eval-stdout`},
		{"long UNC", `\\server\share\` + long + `\.herdr-eval-stdout`, `\\?\UNC\server\share\` + long + `\.herdr-eval-stdout`},
		{"already extended", `\\?\C:\` + long, `\\?\C:\` + long},
		{"NT namespace", `\??\C:\` + long, `\??\C:\` + long},
		{"device", `\\.\pipe\` + long, `\\.\pipe\` + long},
	}
	for _, c := range cases {
		if got := extendedCapturePath(c.in); got != c.want {
			t.Errorf("%s: extendedCapturePath = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestOwnedCaptureOpensALongPath opens a capture whose full path is past
// MAX_PATH, as a long scratch directory (Options.TempRoot or the process
// temporary directory) produces: the open, the write and the removal with
// the handle held all work, and the file is gone after the close.
func TestOwnedCaptureOpensALongPath(t *testing.T) {
	dir := t.TempDir()
	for len(dir) < 300 {
		dir = filepath.Join(dir, strings.Repeat("s", 60))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the long scratch directory: %v", err)
	}
	file, err := openOwnedCapture(dir, winStdoutFileName)
	if err != nil {
		t.Fatalf("open the owned capture at a %d-character path: %v", len(filepath.Join(dir, winStdoutFileName)), err)
	}
	if file.Name() != filepath.Join(dir, winStdoutFileName) {
		t.Fatalf("capture name = %q, want the plain path", file.Name())
	}
	if _, err := file.Write([]byte("captured output")); err != nil {
		t.Fatalf("write through the long-path capture: %v", err)
	}
	if err := os.Remove(file.Name()); err != nil {
		t.Fatalf("removal of the long-path capture with the handle held: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the long-path capture: %v", err)
	}
	if _, err := os.Stat(file.Name()); !os.IsNotExist(err) {
		t.Fatalf("long-path capture still present after the last handle closed: %v", err)
	}
}
