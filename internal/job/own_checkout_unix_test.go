//go:build unix

package job

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestOwnCheckoutStateFIFO pins the unix refusal of a state.json that is
// not a regular file: a FIFO named state.json is refused by
// readPrivateFile's Lstat kind check before it is opened, so the anchor
// returns nil instead of blocking on the open.
func TestOwnCheckoutStateFIFO(t *testing.T) {
	f := newOwnWorktreeFixture(t, "j1")
	statePath := filepath.Join(f.storeRoot(), "jobs", f.id, "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(statePath, 0o600); err != nil {
		t.Fatalf("make the fifo: %v", err)
	}
	f.expectNil(t, f.env, f.top)
}
