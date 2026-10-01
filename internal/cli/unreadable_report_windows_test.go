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
	// A same-process share handle does not always block the collector's read
	// (the round-48 Windows run read the report through it); probe the very
	// read the command performs and skip where the fixture is ineffective.
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("host can read the unreadable report; unreadable-report fixture is ineffective")
	}
}
