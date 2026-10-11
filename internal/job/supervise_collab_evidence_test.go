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
