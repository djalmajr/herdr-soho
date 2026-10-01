//go:build !windows

package cli

import (
	"os"
	"testing"
)

func makeUnreadableReport(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	// A host where the collector can still read the fixture (for example a
	// host that models directory entries differently) cannot exercise the
	// refusal the subtest proves.
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("host can read the unreadable report; unreadable-report fixture is ineffective")
	}
}
