package fakecli

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestWarmPreparedPaysTheColdStartOnce proves the warm helper's contract
// without running a second TestMain: on Windows warmPrepared runs the
// prepared image once, reports nil, leaves no warm directory behind, and
// leaves the prepared image intact. It cannot discriminate a no-op warm
// mutation (a no-op also satisfies these observables); the cold-start
// behavior is proven package-level, where the first fake call under its
// production deadline passes only if the image was warmed during prepare.
func TestWarmPreparedPaysTheColdStartOnce(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the warm-up only applies to the Windows prepared image")
	}
	if err := prepareBinary(); err != nil {
		t.Fatalf("prepareBinary: %v", err)
	}
	before := warmDirs(t)
	if err := warmPrepared(preparedBinary); err != nil {
		t.Fatalf("warmPrepared: %v", err)
	}
	if preparedBinary == "" {
		t.Fatal("preparedBinary is not set after prepareBinary")
	}
	if _, err := os.Stat(preparedBinary); err != nil {
		t.Fatalf("prepared image missing: %v", err)
	}
	for _, dir := range warmDirs(t) {
		if !inList(dir, before) {
			t.Fatalf("warm directory left behind: %s", dir)
		}
	}
}

// warmDirs lists the directories of our MkdirTemp pattern in os.TempDir.
func warmDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "herdr-fakecli-warm-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func inList(name string, list []string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}
