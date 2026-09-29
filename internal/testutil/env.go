package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CleanEnv returns the current environment without inherited Herdr session
// values, points Herdr at a socket path that cannot resolve a live session,
// and gives the test its own TMPDIR: caches live there (the model lists), and
// a shared one let a test in one package read what another package wrote.
func CleanEnv(t testing.TB) []string {
	t.Helper()

	inherited := os.Environ()
	env := make([]string, 0, len(inherited)+1)
	for _, item := range inherited {
		key, _, ok := strings.Cut(item, "=")
		if !ok || strings.HasPrefix(strings.ToUpper(key), "HERDR_") || strings.EqualFold(key, "TMPDIR") {
			continue
		}
		env = append(env, item)
	}
	return append(env, "HERDR_SOCKET_PATH="+filepath.Join(t.TempDir(), "herdr.sock"), "TMPDIR="+t.TempDir())
}
