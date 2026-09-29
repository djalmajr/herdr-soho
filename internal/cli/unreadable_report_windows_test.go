//go:build windows

package cli

import (
	"os"
	"syscall"
	"testing"
)

func makeUnreadableReport(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("report contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.CloseHandle(handle); err != nil {
			t.Errorf("close unreadable report handle: %v", err)
		}
	})
}
