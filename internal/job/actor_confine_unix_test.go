//go:build unix

package job

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestActorConfineFIFOs: a FIFO at the session file or at the exclude
// file is refused without blocking (both opens are non-blocking and the
// handle must be a regular file), and nothing is written.
func TestActorConfineFIFOs(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var friction []string
	sup := &Supervisor{Store: Open(filepath.Join(root, "store")), Friction: func(m string) { friction = append(friction, m) }}
	mustMkdir(t, filepath.Join(root, "state", acfWorkspace))
	fifo := filepath.Join(root, "state", acfWorkspace, "session.conf")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if sup.writeSessionFile(filepath.Join(root, "state"), acfWorkspace, "a=1\n", false) {
		t.Fatal("written over a FIFO")
	}
	if info, err := os.Lstat(fifo); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("the FIFO was replaced: %v %v", info, err)
	}
	co := filepath.Join(root, "co")
	mustMkdir(t, filepath.Join(co, ".git", "info"))
	if err := syscall.Mkfifo(filepath.Join(co, ".git", "info", "exclude"), 0o600); err != nil {
		t.Fatal(err)
	}
	if appendExclude(co, "/x/w7/") {
		t.Fatal("appended to a FIFO")
	}
}
