//go:build unix

package job

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLaunchFileModeIs0600: the record carries the POSIX permission bits
// 0600. This checks POSIX mode bits only; it is not a proof of Windows ACLs.
func TestLaunchFileModeIs0600(t *testing.T) {
	store, clock, jobDir := launchFixture(t)
	if _, err := issueLaunch(store, clock, "job-1", launchTestWorkspace, launchTestPane); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(jobDir, launchFileName))
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Fatalf("launch.json mode %o, want 0600", mode)
	}
}
