package testutil

import (
	"bytes"
	"os"
	"runtime"
	"sync"
	"testing"
)

// DenyFileReads blocks reads of an owned fixture file until the returned
// release function runs. Cleanup also releases it, including failed tests.
// The native implementation preserves the file rather than replacing it.
func DenyFileReads(t testing.TB, path string) func() {
	t.Helper()
	if runtime.GOOS != "windows" && os.Getuid() == 0 {
		t.Skip("root bypasses Unix file read permissions")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("read-denial fixture must be a regular owned file: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read-denial fixture must begin readable: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("read-denial fixture must contain bytes to read")
	}
	restore, err := denyFileReads(path)
	if err != nil {
		t.Fatalf("block fixture reads: %v", err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			if err := restore(); err != nil {
				t.Errorf("release fixture read denial: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("released read-denial fixture changed or is unreadable: %v", err)
			}
		})
	}
	t.Cleanup(release)
	if _, err := os.ReadFile(path); err == nil {
		release()
		t.Fatal("read-denial fixture is ineffective: file remains readable")
	}
	return release
}
