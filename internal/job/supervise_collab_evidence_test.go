package job

// The bounded, root-bound evidence read (hashEvidence, F4): the hostile
// path classes a participant marker or a recorded event can carry — empty
// or relative, a sibling-prefix escape, an absolute file outside every
// root, a missing file, a symlink inside a root pointing outside, a
// directory, and an overlong file — are all refused with the same
// outside-roots error, and a legitimate report inside the allowed
// directory stands with its accepted path and fresh sha256.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeEvidenceTarget writes body at path (creating the parent
// directories) and returns the path.
func writeEvidenceTarget(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o666); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSuperviseCollabHashEvidenceHostilePaths(t *testing.T) {
	// Catches the mutation "open through os.Open(path) instead of the
	// root": the sibling-prefix, the outside-root, the missing-file and
	// the symlink-escape cases would then read and hash the target.
	root := t.TempDir()
	allowed := filepath.Join(root, "reports")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	// The second allowed root does not exist: the read skips it.
	roots := []string{allowed, filepath.Join(root, "missing")}
	outside := filepath.Join(root, "outside")
	writeEvidenceTarget(t, filepath.Join(outside, "outside.md"), "outside target")

	t.Run("empty or relative path", func(t *testing.T) {
		for _, path := range []string{"", "reports/relative.md"} {
			if _, _, err := hashEvidence(roots, path); err == nil {
				t.Fatalf("path %q accepted: want the absolute-path refusal", path)
			}
		}
	})

	t.Run("sibling-prefix escape", func(t *testing.T) {
		// A sibling of the allowed root whose name starts with it:
		// <root>-evil holds the file, <root> does not.
		sibling := writeEvidenceTarget(t, root+"-evil/x.md", "sibling target")
		if _, _, err := hashEvidence(roots, sibling); !errors.Is(err, errEvidenceOutside) {
			t.Fatalf("sibling-prefix target %q accepted: err=%v, want the outside-roots refusal", sibling, err)
		}
	})

	t.Run("absolute file outside every root", func(t *testing.T) {
		path := filepath.Join(outside, "outside.md")
		if _, _, err := hashEvidence(roots, path); !errors.Is(err, errEvidenceOutside) {
			t.Fatalf("outside target %q accepted: err=%v, want the outside-roots refusal", path, err)
		}
	})

	t.Run("missing file inside a root", func(t *testing.T) {
		path := filepath.Join(allowed, "missing.md")
		if _, _, err := hashEvidence(roots, path); !errors.Is(err, errEvidenceOutside) {
			t.Fatalf("missing target %q accepted: err=%v, want the outside-roots refusal", path, err)
		}
	})

	t.Run("symlink inside a root pointing outside", func(t *testing.T) {
		target := filepath.Join(outside, "outside.md")
		link := filepath.Join(allowed, "evil-link.md")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot create symlinks on %s: %v", runtime.GOOS, err)
		}
		if _, _, err := hashEvidence(roots, link); !errors.Is(err, errEvidenceOutside) {
			t.Fatalf("symlink escape %q accepted: err=%v, want the outside-roots refusal", link, err)
		}
	})

	t.Run("directory inside a root", func(t *testing.T) {
		if _, _, err := hashEvidence(roots, allowed); !errors.Is(err, errEvidenceOutside) {
			t.Fatalf("directory %q accepted: err=%v, want the outside-roots refusal", allowed, err)
		}
	})

	t.Run("file of maxEvidenceBytes+1 bytes", func(t *testing.T) {
		path := filepath.Join(allowed, "big.md")
		if err := os.WriteFile(path, make([]byte, maxEvidenceBytes+1), 0o666); err != nil {
			t.Fatal(err)
		}
		if _, _, err := hashEvidence(roots, path); !errors.Is(err, errEvidenceOutside) {
			t.Fatalf("overlong target %q accepted: err=%v, want the outside-roots refusal", path, err)
		}
	})

	t.Run("legitimate report under the state dir reports", func(t *testing.T) {
		body := "the legitimate report\n"
		path := writeEvidenceTarget(t, filepath.Join(allowed, "legit.md"), body)
		accepted, sum, err := hashEvidence(roots, path)
		if err != nil {
			t.Fatalf("legitimate target %q refused: %v", path, err)
		}
		if accepted != path {
			t.Fatalf("accepted = %q, want %q", accepted, path)
		}
		want := sha256.Sum256([]byte(body))
		if sum != hex.EncodeToString(want[:]) {
			t.Fatalf("sha256 = %s, want %s", sum, hex.EncodeToString(want[:]))
		}
	})
}

// The allowed-root refusal of a symlinked appended component: the job
// workspace's reports directory that is a symlink to an outside directory
// never becomes an allowed root (only the trusted base may resolve
// through symlinks), while the same file through a real reports directory
// and through a symlinked trusted base (the job worktree) stands with its
// fresh sha256.
func TestSuperviseCollabHashEvidenceSymlinkedRoot(t *testing.T) {
	// Catches the mutation "a symlinked allowed root is trusted" (the
	// decision-1 revert in collabEvidenceRoots): the symlinked reports
	// directory would then become the allowed root and the outside file
	// would be read and hashed.
	outside := t.TempDir()
	body := "the outside report\n"
	writeEvidenceTarget(t, filepath.Join(outside, "report.md"), body)

	// The refused root: the job workspace's reports directory is a
	// symlink to the outside directory holding the file.
	ws := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws, "reports")); err != nil {
		t.Skipf("cannot create symlinks on %s: %v", runtime.GOOS, err)
	}
	path := filepath.Join(ws, "reports", "report.md")
	roots := collabEvidenceRoots(State{WorkspaceID: "w1"}, ws)
	if _, sum, err := hashEvidence(roots, path); !errors.Is(err, errEvidenceOutside) || sum != "" {
		t.Fatalf("symlinked root %q accepted: sum=%s err=%v, want the outside-roots refusal and no hash", path, sum, err)
	}

	// Control (a): the same file through a real reports directory stands
	// with its fresh sha256.
	ws2 := t.TempDir()
	real := writeEvidenceTarget(t, filepath.Join(ws2, "reports", "report.md"), body)
	accepted, sum, err := hashEvidence(collabEvidenceRoots(State{WorkspaceID: "w1"}, ws2), real)
	if err != nil || accepted != real {
		t.Fatalf("real reports dir refused: %v (accepted %q)", err, accepted)
	}
	want := sha256.Sum256([]byte(body))
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("sha256 = %s, want %s", sum, hex.EncodeToString(want[:]))
	}

	// Control (b): the trusted base reached through a symlink (the job
	// worktree) still stands: the base may resolve through symlinks.
	base := t.TempDir()
	worktree := filepath.Join(base, "wt-link")
	if err := os.Symlink(base, worktree); err != nil {
		t.Skipf("cannot create symlinks on %s: %v", runtime.GOOS, err)
	}
	file := writeEvidenceTarget(t, filepath.Join(worktree, "report.md"), body)
	accepted, sum, err = hashEvidence(collabEvidenceRoots(State{WorkspaceID: "w1", Dir: worktree}, ws2), file)
	if err != nil || accepted != file {
		t.Fatalf("symlinked trusted base refused: %v (accepted %q)", err, accepted)
	}
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("sha256 = %s, want %s", sum, hex.EncodeToString(want[:]))
	}
}
