//go:build !windows

package job

import (
	"os"
	"path/filepath"
	"testing"
)

// holdWorktreeDir makes dir undeletable while its contents stay
// deletable, the state a Windows directory is in while a process has it
// as its current directory: here the parent turns read-only, so git can
// remove every file inside dir but not dir itself. release undoes it.
func holdWorktreeDir(t *testing.T, dir string) (release func()) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only parent that holds the directory")
	}
	parent := filepath.Dir(dir)
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	released := false
	release = func() {
		if !released {
			released = true
			_ = os.Chmod(parent, 0o755)
		}
	}
	t.Cleanup(release)
	return release
}
