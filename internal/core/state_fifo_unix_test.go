//go:build unix

package core

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// Catches the mutation "LastReport reads the marker with ReadTextFile"
// (os.ReadFile): a FIFO planted as the marker would block the reader.
func TestLastReportFifoMarkerReadsAsNoMarker(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(LastReportPath(dir, "impl-1"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan string, 1)
	go func() { done <- LastReport(dir, "impl-1") }()
	select {
	case got := <-done:
		if got != "" {
			t.Fatalf("LastReport(fifo) = %q, want no marker", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LastReport blocked on the FIFO marker")
	}
	// Control: a regular marker reads its target.
	if err := os.WriteFile(LastReportPath(dir, "rev-1"), []byte("/tmp/r.md\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LastReport(dir, "rev-1"); got != "/tmp/r.md" {
		t.Fatalf("LastReport(file) = %q", got)
	}
}
