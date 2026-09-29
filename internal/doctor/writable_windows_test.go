//go:build windows

package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsWritableFollowsLibuvAccess(t *testing.T) {
	// Mutation captured: checking the 0200 bit for directories too reports a
	// read-only-attribute directory as not writable, where Node's accessSync
	// W_OK (libuv fs__access) grants it.
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Skipf("the read-only attribute did not stick on this host (%v, %v)", info, err)
	}
	if !isWritable(dir) {
		t.Fatal("a directory with FILE_ATTRIBUTE_READONLY is writable for Node's accessSync W_OK")
	}
	file := filepath.Join(t.TempDir(), "state-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(file, 0o600) })
	if isWritable(file) {
		t.Fatal("a read-only file is not writable")
	}
}
