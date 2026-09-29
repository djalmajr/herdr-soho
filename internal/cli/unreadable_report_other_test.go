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
}
