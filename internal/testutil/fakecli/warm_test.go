package fakecli

import (
	"os"
	"runtime"
	"testing"
)

// TestWarmPreparedPaysTheColdStartOnce proves the warm helper's contract
// without running a second TestMain: on Windows warmPrepared runs the
// prepared image once, reports nil, leaves no warm directory behind, and
// leaves the prepared image intact. The temporary directory is the test's
// own (TMP, TEMP and TMPDIR point at an empty directory), so warm
// directories that other package test processes create in the shared
// temporary directory at the same time cannot be mistaken for this one, and
// the check is exact: the directory is empty afterwards. It cannot
// discriminate a no-op warm mutation (a no-op also satisfies these
// observables); the cold-start behavior is proven package-level, where the
// first fake call under its production deadline passes only if the image
// was warmed during prepare.
func TestWarmPreparedPaysTheColdStartOnce(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the warm-up only applies to the Windows prepared image")
	}
	if err := prepareBinary(); err != nil {
		t.Fatalf("prepareBinary: %v", err)
	}
	private := t.TempDir()
	for _, key := range []string{"TMP", "TEMP", "TMPDIR"} {
		t.Setenv(key, private)
	}
	if got := os.TempDir(); got != private {
		t.Fatalf("os.TempDir() = %q, want the test's own %q", got, private)
	}
	if err := warmPrepared(preparedBinary); err != nil {
		t.Fatalf("warmPrepared: %v", err)
	}
	if preparedBinary == "" {
		t.Fatal("preparedBinary is not set after prepareBinary")
	}
	if _, err := os.Stat(preparedBinary); err != nil {
		t.Fatalf("prepared image missing: %v", err)
	}
	left, err := os.ReadDir(private)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range left {
		t.Errorf("warm directory left behind: %s", e.Name())
	}
}
