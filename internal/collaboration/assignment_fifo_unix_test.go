//go:build unix

package collaboration

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Catches the mutation "Read and ReadClaim open the record with os.ReadFile
// / os.Open": a FIFO planted as assignment.json would block the reader (and
// the supervisor's terminal enumeration through ListActive).
func TestAssignmentFifoRecordIsUnreadableWithoutBlocking(t *testing.T) {
	ws := t.TempDir()
	id := "c-00000000000000f1"
	dir := filepath.Join(ws, "collaboration", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "assignment.json"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	store := Store{StateDir: ws}
	done := make(chan struct{})
	var readErr, claimErr, listErr error
	var unreadable []UnreadableAssignment
	go func() {
		defer close(done)
		_, readErr = store.Read(id)
		_, _, claimErr = store.ReadClaim(id)
		_, unreadable, listErr = store.ListActive()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO assignment.json blocked")
	}
	if readErr == nil || claimErr == nil {
		t.Fatalf("Read err = %v, ReadClaim err = %v, want both refused", readErr, claimErr)
	}
	if listErr != nil || len(unreadable) != 1 || unreadable[0].ID != id {
		t.Fatalf("ListActive unreadable = %+v err = %v, want the FIFO record unreadable", unreadable, listErr)
	}
}
