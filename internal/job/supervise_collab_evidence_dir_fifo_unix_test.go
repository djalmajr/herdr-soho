//go:build unix

package job

// The evidence roots never block on a FIFO: a FIFO (named pipe) at the
// trusted base, at an appended component, behind a symlinked component, or
// swapped into a component's place while the root is being opened is
// refused at once, the same as any other path outside the allowed
// directories; a real in-root report still hashes.

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type evidenceResult struct {
	accepted, sum string
	err           error
}

// hashEvidenceWithin runs hashEvidence on a goroutine and fails the test
// when it has not returned after the bound: an open that waits for a
// FIFO's writer would hold it forever.
func hashEvidenceWithin(t *testing.T, roots []collabEvidenceRoot, path string) evidenceResult {
	t.Helper()
	done := make(chan evidenceResult, 1)
	go func() {
		accepted, sum, err := hashEvidence(roots, path)
		done <- evidenceResult{accepted, sum, err}
	}()
	select {
	case r := <-done:
		return r
	case <-time.After(10 * time.Second):
		t.Fatalf("hashEvidence(%s) blocked on a FIFO", path)
		return evidenceResult{}
	}
}

func mkfifo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
}

func requireEvidenceRefused(t *testing.T, r evidenceResult, what string) {
	t.Helper()
	if !errors.Is(r.err, errEvidenceOutside) || r.sum != "" || r.accepted != "" {
		t.Fatalf("%s: accepted=%q sum=%s err=%v, want the outside-roots refusal", what, r.accepted, r.sum, r.err)
	}
}

// A FIFO at the trusted base, or a base path that is a symlink to a FIFO,
// is refused without waiting for a writer.
func TestSuperviseCollabEvidenceFifoBaseRefused(t *testing.T) {
	// Catches the mutation "the base is opened with os.OpenRoot" (no
	// non-blocking open first): the open would wait for a writer.
	for _, tc := range []struct {
		name    string
		suffix  string
		viaLink bool
	}{
		{"base FIFO", "", false},
		{"base FIFO with a suffix", "reports", false},
		{"base symlink to a FIFO", "", true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, "base")
			mkfifo(t, base)
			if tc.viaLink {
				link := filepath.Join(dir, "link")
				if err := os.Symlink(base, link); err != nil {
					t.Skipf("symlink: %v", err)
				}
				base = link
			}
			roots := []collabEvidenceRoot{{base: base, suffix: tc.suffix}}
			path := filepath.Join(base, tc.suffix, "report.md")
			requireEvidenceRefused(t, hashEvidenceWithin(t, roots, path), tc.name)
		})
	}
}

// A FIFO in the place of an appended component, at every depth of the
// suffix, is refused without waiting for a writer.
func TestSuperviseCollabEvidenceFifoComponentRefused(t *testing.T) {
	// Catches the mutation "a component is opened with Root.OpenRoot" (no
	// non-blocking open first): the open would wait for a writer.
	suffix := filepath.Join("herdr-soho", "w1", "reports")
	for _, depth := range []string{"herdr-soho", filepath.Join("herdr-soho", "w1"), suffix} {
		depth := depth
		t.Run(depth, func(t *testing.T) {
			base := t.TempDir()
			mkfifo(t, filepath.Join(base, depth))
			roots := []collabEvidenceRoot{{base: base, suffix: suffix}}
			path := filepath.Join(base, suffix, "report.md")
			requireEvidenceRefused(t, hashEvidenceWithin(t, roots, path), "FIFO at "+depth)
		})
	}
	t.Run("reports", func(t *testing.T) {
		base := t.TempDir()
		mkfifo(t, filepath.Join(base, "reports"))
		roots := []collabEvidenceRoot{{base: base, suffix: "reports"}}
		requireEvidenceRefused(t, hashEvidenceWithin(t, roots, filepath.Join(base, "reports", "report.md")), "FIFO at reports")
	})
}

// A component that is a symlink to a FIFO, inside or outside the base, is
// refused without waiting for a writer.
func TestSuperviseCollabEvidenceSymlinkToFifoRefused(t *testing.T) {
	// Catches the mutation "a component is opened with Root.OpenRoot": the
	// in-base relative symlink resolves through the parent's handle to the
	// FIFO and the open would wait for a writer (an absolute target is
	// refused by the handle as an escape before any open).
	for _, tc := range []struct {
		name   string
		inBase bool
		depth  string
		target string
	}{
		{"reports to an in-base FIFO", true, "reports", "pipe"},
		{"reports to an outside FIFO", false, "reports", ""},
		{"herdr-soho to an in-base FIFO", true, "herdr-soho", "pipe"},
		{"w1 to a FIFO in the base, outside its parent", true, filepath.Join("herdr-soho", "w1"), filepath.Join("..", "pipe")},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			suffix := filepath.Join("herdr-soho", "w1", "reports")
			if tc.depth == "reports" {
				suffix = "reports"
			}
			fifo := filepath.Join(t.TempDir(), "pipe")
			if tc.inBase {
				fifo = filepath.Join(base, "pipe")
			}
			mkfifo(t, fifo)
			link := filepath.Join(base, tc.depth)
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			target := fifo
			if tc.inBase {
				target = tc.target
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink: %v", err)
			}
			roots := []collabEvidenceRoot{{base: base, suffix: suffix}}
			path := filepath.Join(base, suffix, "report.md")
			requireEvidenceRefused(t, hashEvidenceWithin(t, roots, path), tc.name)
		})
	}
}

// A component that is a real directory when the root's open starts and is
// replaced by a FIFO before its own open (right after its parent's handle
// is opened) is refused without waiting for a writer.
func TestSuperviseCollabEvidenceDirectoryRacedToFifoRefused(t *testing.T) {
	// Catches the mutation "a component is opened with Root.OpenRoot": the
	// FIFO swapped in after the parent's open would hold the next open.
	for _, tc := range []struct{ after, swap string }{
		{"herdr-soho", "w1"},
		{"w1", "reports"},
	} {
		tc := tc
		t.Run(tc.swap, func(t *testing.T) {
			base := t.TempDir()
			suffix := filepath.Join("herdr-soho", "w1", "reports")
			path := writeEvidenceTarget(t, filepath.Join(base, suffix, "report.md"), "inside\n")
			parent := filepath.Join(base, "herdr-soho")
			if tc.after == "w1" {
				parent = filepath.Join(parent, "w1")
			}
			swapped := false
			evidenceComponentOpenedHook = func(name string) {
				if swapped || name != tc.after {
					return
				}
				swapped = true
				if err := os.Rename(filepath.Join(parent, tc.swap), filepath.Join(parent, tc.swap+"-moved")); err != nil {
					t.Error(err)
				}
				if err := syscall.Mkfifo(filepath.Join(parent, tc.swap), 0o600); err != nil {
					t.Error(err)
				}
			}
			t.Cleanup(func() { evidenceComponentOpenedHook = nil })
			roots := []collabEvidenceRoot{{base: base, suffix: suffix}}
			requireEvidenceRefused(t, hashEvidenceWithin(t, roots, path), "directory raced to a FIFO at "+tc.swap)
			if !swapped {
				t.Fatal("the swap never ran")
			}
		})
	}
}

// Controls: a real in-root report hashes, through a base reached by a
// symlink too (the base's own symlinks resolve) and through an in-root
// file symlink; escapes by "..", by a file symlink leaving the root and by
// a component symlink leaving the base stay refused.
func TestSuperviseCollabEvidenceFifoSafeOpenKeepsTheContract(t *testing.T) {
	// Catches the mutation "the non-blocking open refuses every root" (a
	// real directory not promoted to a root): the genuine report would be
	// refused.
	base := t.TempDir()
	suffix := filepath.Join("herdr-soho", "w1", "reports")
	path := writeEvidenceTarget(t, filepath.Join(base, suffix, "report.md"), "real\n")
	roots := []collabEvidenceRoot{{base: base, suffix: suffix}}
	if r := hashEvidenceWithin(t, roots, path); r.err != nil || r.accepted != path || r.sum != evidenceSum("real\n") {
		t.Fatalf("genuine report: accepted=%q sum=%s err=%v", r.accepted, r.sum, r.err)
	}
	if r := hashEvidenceWithin(t, []collabEvidenceRoot{{base: filepath.Join(base, suffix)}}, path); r.err != nil || r.sum != evidenceSum("real\n") {
		t.Fatalf("genuine report, no suffix: accepted=%q sum=%s err=%v", r.accepted, r.sum, r.err)
	}
	linkBase := filepath.Join(t.TempDir(), "base-link")
	if err := os.Symlink(base, linkBase); err != nil {
		t.Skipf("symlink: %v", err)
	}
	linked := filepath.Join(linkBase, suffix, "report.md")
	if r := hashEvidenceWithin(t, []collabEvidenceRoot{{base: linkBase, suffix: suffix}}, linked); r.err != nil || r.accepted != linked || r.sum != evidenceSum("real\n") {
		t.Fatalf("report under a symlinked base: accepted=%q sum=%s err=%v", r.accepted, r.sum, r.err)
	}
	inRoot := filepath.Join(base, suffix, "alias.md")
	if err := os.Symlink("report.md", inRoot); err != nil {
		t.Fatal(err)
	}
	if r := hashEvidenceWithin(t, roots, inRoot); r.err != nil || r.sum != evidenceSum("real\n") {
		t.Fatalf("in-root file symlink: accepted=%q sum=%s err=%v", r.accepted, r.sum, r.err)
	}

	outside := writeEvidenceTarget(t, filepath.Join(t.TempDir(), "secret.md"), "outside\n")
	requireEvidenceRefused(t, hashEvidenceWithin(t, roots, filepath.Join(base, suffix, "..", "..", "..", "report.md")), "dot-dot escape")
	escape := filepath.Join(base, suffix, "escape.md")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	requireEvidenceRefused(t, hashEvidenceWithin(t, roots, escape), "file symlink leaving the root")
	other := t.TempDir()
	writeEvidenceTarget(t, filepath.Join(other, "w1", "reports", "report.md"), "outside\n")
	escBase := t.TempDir()
	if err := os.Symlink(other, filepath.Join(escBase, "herdr-soho")); err != nil {
		t.Fatal(err)
	}
	requireEvidenceRefused(t, hashEvidenceWithin(t, []collabEvidenceRoot{{base: escBase, suffix: suffix}}, filepath.Join(escBase, suffix, "report.md")), "component symlink leaving the base")
}
