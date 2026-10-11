package job

// The allowed evidence roots bound to the opened handles: a replacement of
// an appended component before, during or after the root is opened can
// never make an outside directory the root the read uses.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func evidenceSum(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// A replacement after the root is opened: the read goes through the
// handle of the real reports directory, so the hash is the original
// file's, never the outside file now reachable at the same path.
func TestSuperviseCollabEvidenceRootSwapAfterOpenStaysBound(t *testing.T) {
	// Catches the mutation "the file is opened by path after the root
	// check" (a fresh os.OpenRoot of the joined path in hashEvidence): the
	// read would follow the swapped-in symlink and hash the outside file.
	ws := t.TempDir()
	outside := t.TempDir()
	path := writeEvidenceTarget(t, filepath.Join(ws, "reports", "report.md"), "inside\n")
	writeEvidenceTarget(t, filepath.Join(outside, "report.md"), "outside\n")
	if err := os.Symlink(outside, filepath.Join(t.TempDir(), "probe")); err != nil {
		t.Skipf("cannot create symlinks on %s: %v", runtime.GOOS, err)
	}
	swapped := false
	evidenceRootOpenedHook = func() {
		if swapped {
			return
		}
		swapped = true
		if err := os.Rename(filepath.Join(ws, "reports"), filepath.Join(ws, "reports-moved")); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(outside, filepath.Join(ws, "reports")); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { evidenceRootOpenedHook = nil })
	accepted, sum, err := hashEvidence(collabEvidenceRoots(State{WorkspaceID: "w1"}, ws), path)
	if !swapped {
		t.Fatal("the swap never ran")
	}
	if err != nil || accepted != path || sum != evidenceSum("inside\n") {
		t.Fatalf("after the swap: accepted=%q sum=%s err=%v, want the original file's hash %s (outside %s)", accepted, sum, err, evidenceSum("inside\n"), evidenceSum("outside\n"))
	}
}

// A replacement of an appended component between its open and its check:
// the component is no longer the real directory the handle names, and the
// root is refused.
func TestSuperviseCollabEvidenceComponentSwapRefused(t *testing.T) {
	// Catches the mutation "the opened component is not checked against
	// its directory entry" (the Lstat/SameFile check in openEvidenceRoot
	// removed): the swapped component would be accepted.
	base := t.TempDir()
	suffix := filepath.Join("herdr-soho", "w1", "reports")
	path := writeEvidenceTarget(t, filepath.Join(base, suffix, "report.md"), "inside\n")
	if err := os.Symlink(base, filepath.Join(t.TempDir(), "probe")); err != nil {
		t.Skipf("cannot create symlinks on %s: %v", runtime.GOOS, err)
	}
	swapped := false
	evidenceComponentOpenedHook = func(name string) {
		if swapped || name != "herdr-soho" {
			return
		}
		swapped = true
		if err := os.Rename(filepath.Join(base, "herdr-soho"), filepath.Join(base, "moved")); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(filepath.Join(base, "moved"), filepath.Join(base, "herdr-soho")); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { evidenceComponentOpenedHook = nil })
	roots := []collabEvidenceRoot{{base: base, suffix: suffix}}
	if _, sum, err := hashEvidence(roots, path); !errors.Is(err, errEvidenceOutside) || sum != "" {
		t.Fatalf("swapped component accepted: sum=%s err=%v, want the outside-roots refusal", sum, err)
	}
	if !swapped {
		t.Fatal("the swap never ran")
	}
}

// An appended component that is a symlink before the read, inside or
// outside the trusted base, at any depth of the suffix, is refused; the
// same layout with real directories stands.
func TestSuperviseCollabEvidenceSymlinkedComponentRefused(t *testing.T) {
	// Catches the mutation "the suffix is opened in one step from the
	// joined path" (os.OpenRoot of the joined path): a symlinked component
	// would resolve and be accepted.
	for _, tc := range []struct {
		name   string
		inBase bool
		depth  string
	}{
		{"reports to outside", false, "reports"},
		{"reports to inside the base", true, "reports"},
		{"herdr-soho to outside", false, "herdr-soho"},
		{"herdr-soho to inside the base", true, "herdr-soho"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			suffix := filepath.Join("herdr-soho", "w1", "reports")
			if tc.depth == "reports" {
				suffix = "reports"
			}
			target := t.TempDir()
			if tc.inBase {
				target = filepath.Join(base, "elsewhere")
			}
			// The real tree the symlink points at holds the rest of the
			// suffix and the file.
			rest := suffix[len(tc.depth):]
			writeEvidenceTarget(t, filepath.Join(target, rest, "report.md"), "linked\n")
			if err := os.Symlink(target, filepath.Join(base, tc.depth)); err != nil {
				t.Skipf("cannot create symlinks on %s: %v", runtime.GOOS, err)
			}
			path := filepath.Join(base, suffix, "report.md")
			roots := []collabEvidenceRoot{{base: base, suffix: suffix}}
			if _, sum, err := hashEvidence(roots, path); !errors.Is(err, errEvidenceOutside) || sum != "" {
				t.Fatalf("symlinked component %s accepted: sum=%s err=%v", tc.depth, sum, err)
			}
			// Control: the same layout with real directories stands.
			real := t.TempDir()
			file := writeEvidenceTarget(t, filepath.Join(real, suffix, "report.md"), "real\n")
			accepted, sum, err := hashEvidence([]collabEvidenceRoot{{base: real, suffix: suffix}}, file)
			if err != nil || accepted != file || sum != evidenceSum("real\n") {
				t.Fatalf("real layout: accepted=%q sum=%s err=%v", accepted, sum, err)
			}
		})
	}
}
