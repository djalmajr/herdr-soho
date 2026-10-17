package dispatch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
)

// DJA-379: PathWithin is the containment check the dispatch report route
// uses (the state dir must be inside the worker's cwd). The table covers
// the normalization contract: equal paths, children, the common-prefix
// sibling trap, `..` folds, and the Windows rules under goos == "win32"
// (case-insensitive, backslash and slash equivalent, the drive letter
// folded), mirroring how SamePath normalizes its inputs.
func TestPathWithinTable(t *testing.T) {
	cases := []struct {
		child, parent, goos string
		want                bool
	}{
		// Equal paths (both platforms).
		{"/a/b", "/a/b", "linux", true},
		{"/a/b", "/a/b", "darwin", true},
		{"C:\\work\\repo", "c:/work/repo", "win32", true},
		// Children.
		{"/a/b/c", "/a/b", "linux", true},
		{"/a/b/c/d", "/a/b", "darwin", true},
		{`C:\work\repo\sub`, `C:\work\repo`, "win32", true},
		{`C:\work\repo\sub`, `c:/work`, "win32", true},
		// Common-prefix siblings are not inside each other.
		{"/a/bc", "/a/b", "linux", false},
		{"/a/b", "/a/bc", "darwin", false},
		{`C:\work\repx`, `C:\work\repo`, "win32", false},
		// `..` folds before the comparison.
		{"/a/b/../b", "/a/b", "linux", true},
		{"/a/b/c", "/a/b/c/..", "linux", true},
		{"/a/b/..", "/a/b", "linux", false},
		{`C:\a\b\..\c`, `C:/a/c`, "win32", true},
		// Windows case and separator folding, the drive letter included.
		{`c:/WORK/REPO/sub`, `C:\work\repo`, "win32", true},
		{`C:\Work\Repo\Sub`, `c:/work/repo`, "win32", true},
		{`D:\work\repo`, `C:\work\repo`, "win32", false},
		// POSIX stays case-sensitive.
		{"/A/b/c", "/a/b", "linux", false},
		{"/a/b/c", "/A/b", "darwin", false},
		// A trailing slash normalizes away.
		{"/a/b/c", "/a/b/", "linux", true},
	}
	for _, tc := range cases {
		if got := PathWithin(tc.child, tc.parent, tc.goos); got != tc.want {
			t.Fatalf("PathWithin(%q, %q, %q) = %v; want %v", tc.child, tc.parent, tc.goos, got, tc.want)
		}
	}
}

// DJA-379: the readers already scan both the state dir and the soho
// scratch dir, and the report route must not need reader changes. This
// pins the family lookup side: an accepted dispatch recorded only under
// $TMPDIR/soho/<ws>/reports still resolves the family (the state-dir
// side is covered by the existing ForSpecFamily cases).
func TestPathWithinRelatedReadersScanBothDirs(t *testing.T) {
	t.Run("the family lookup finds an accepted dispatch recorded only in the TMPDIR soho dir", func(t *testing.T) {
		sd := t.TempDir()
		tmp := t.TempDir()
		reports := filepath.Join(tmp, "soho", "ws", "reports")
		if err := os.MkdirAll(reports, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(reports, "build-20261001T000000.dispatch.json"), []byte(`{"version":1,"kind":"claude","model":"sonnet","submission":"accepted"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := ForSpecFamily("build", sd, platform.Env{"HERDR_WORKSPACE_ID": "ws", "TMPDIR": tmp}, "/work", &core.Config{Entries: map[string]core.ConfigEntry{}})
		if err != nil || got != "anthropic" {
			t.Fatalf("ForSpecFamily from the TMPDIR sidecar: got %q, %v; want anthropic", got, err)
		}
	})
}
