//go:build unix

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// within runs fn on a goroutine and fails the test when it has not
// returned after the bound: a blocking open would hold it forever.
func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s blocked on the FIFO", what)
	}
}

// Catches the mutation "the regular-file read opens with plain O_RDONLY"
// (or os.ReadFile): the open of the FIFO would block.
func TestReadRegularFileRefusesAFifoWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	within(t, "ReadRegularFile", func() {
		if _, err := ReadRegularFile(fifo); !errors.Is(err, ErrNotRegular) {
			t.Errorf("ReadRegularFile(fifo) err = %v, want ErrNotRegular", err)
		}
	})
	within(t, "ReadRegularTextFile", func() {
		if _, err := ReadRegularTextFile(fifo); !errors.Is(err, ErrNotRegular) {
			t.Errorf("ReadRegularTextFile(fifo) err = %v, want ErrNotRegular", err)
		}
	})
	if _, err := ReadRegularFile(dir); !errors.Is(err, ErrNotRegular) {
		t.Errorf("ReadRegularFile(dir) err = %v, want ErrNotRegular", err)
	}
	// Control: a regular file reads unchanged, CRLF normalized as text.
	file := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(file, []byte("a\r\nb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, err := ReadRegularFile(file); err != nil || string(raw) != "a\r\nb\n" {
		t.Fatalf("ReadRegularFile(file) = %q, %v", raw, err)
	}
	if text, err := ReadRegularTextFile(file); err != nil || text != "a\nb\n" {
		t.Fatalf("ReadRegularTextFile(file) = %q, %v", text, err)
	}
}
