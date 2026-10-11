//go:build unix

package job

// The FIFO at an allowed report path: hashEvidence refuses it with the
// outside-roots error without blocking — the pre-open regular check
// refuses it before the open, and the non-blocking open flags keep a FIFO
// swapped in after the check from blocking. The read runs in a goroutine
// against a bounded wait: a blocking open would fail the test.

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSuperviseCollabHashEvidenceFifo(t *testing.T) {
	// Catches the mutation "open before the regular check" (the decision-2
	// revert: r.Open without the pre-open stat and the non-blocking flags):
	// the open of the FIFO would block the read forever and the bounded
	// wait would fail.
	root := t.TempDir()
	allowed := filepath.Join(root, "reports")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(allowed, "pipe.md")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo %s: %v", fifo, err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := hashEvidence([]string{allowed}, fifo)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errEvidenceOutside) {
			t.Fatalf("FIFO %q accepted: err=%v, want the outside-roots refusal", fifo, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("hashEvidence blocked on the FIFO %q", fifo)
	}
}
