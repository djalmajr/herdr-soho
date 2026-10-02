package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/dispatch"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestDispatchHintsR3Paths(t *testing.T) {
	t.Run("dirglob", func(t *testing.T) {
		t.Logf("src/*/ vs src/components/: forward=%v reverse=%v", pathsCross("src/*/", "src/components/"), pathsCross("src/components/", "src/*/"))
		if !pathsCross("src/*/", "src/components/") || !pathsCross("src/components/", "src/*/") {
			t.Error("glob directory lost its common subtree src/components/a.ts")
		}
	})
	// JS: "dispatch: a character class crosses the star glob that shares a file with it"
	t.Run("pathsCross: a character class and a star glob cross on their common file", func(t *testing.T) {
		if !pathsCross("src/[ab].ts", "src/*.ts") || !pathsCross("src/*.ts", "src/[ab].ts") {
			t.Fatal("the character class stopped crossing the star glob that shares src/a.ts with it")
		}
	})
	t.Run("pathsCross: a recursive glob crosses an owned directory in either order", func(t *testing.T) {
		if !pathsCross("**/*.ts", "src/components/") || !pathsCross("src/components/", "**/*.ts") {
			t.Fatal("the recursive glob stopped crossing the owned directory that holds src/components/a.ts")
		}
	})
	t.Run("pathsCross: a star glob crosses the directory it can reach into", func(t *testing.T) {
		if !pathsCross("*.md", "docs/") || !pathsCross("docs/", "*.md") {
			t.Fatal("the star glob stopped crossing the docs/ directory")
		}
	})
	t.Run("pathsCross: the round-1 negatives stay negative", func(t *testing.T) {
		if pathsCross("*.workers.test.ts", "src/a.ts") || pathsCross("src/a.ts", "*.workers.test.ts") {
			t.Fatal("the leading-star glob crossed a file it does not match")
		}
		if pathsCross("*.mjs", "*.js") || pathsCross("*.js", "*.mjs") {
			t.Fatal("globs with incompatible extensions crossed")
		}
	})
}

func TestDispatchHintsR3Consumer(t *testing.T) {
	// JS: "dispatch: the owned-files overlap warn follows the corrected crossing"
	overlap := func(t *testing.T, mine, theirs string) string {
		t.Helper()
		h := newTM3bHarness(t, strings.Replace(tm3bFullBrief(), "internal/a.go", mine, 1))
		for _, dir := range []string{filepath.Join(h.ws, "briefs"), filepath.Join(h.ws, "reports")} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		pending := filepath.Join(h.ws, "briefs", "peer.md")
		if err := os.WriteFile(pending, []byte("# Brief\n# Owned files\n"+theirs+"\n# Report contract\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		report := filepath.Join(h.ws, "reports", "peer.md")
		if err := os.WriteFile(filepath.Join(h.ws, "last-report-peer"), []byte(report+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		roster := "peer\tw0test:p0b\tcodex\timplementer\topenai\t0\t" + h.root + "\tnow\tgpt-5\ttask\timplementer\n"
		if err := os.WriteFile(filepath.Join(h.ws, "agents.tsv"), []byte(roster), 0o600); err != nil {
			t.Fatal(err)
		}
		oldErr := platform.Stderr
		var stderr bytes.Buffer
		platform.Stderr = &stderr
		t.Cleanup(func() { platform.Stderr = oldErr })
		warnOwnedOverlap(h.brief, "implementer", "build", h.ws, h.ctx, h.env, h.root, h.root)
		pendingWarning := stderr.String()
		if err := os.WriteFile(report, []byte("complete\n"), 0600); err != nil {
			t.Fatal(err)
		}
		stderr.Reset()
		warnOwnedOverlap(h.brief, "implementer", "build", h.ws, h.ctx, h.env, h.root, h.root)
		t.Logf("pending warning=%q; completed warning=%q", pendingWarning, stderr.String())
		if stderr.Len() != 0 {
			t.Errorf("completed report still warns: %q", stderr.String())
		}
		return pendingWarning
	}
	t.Run("a character class owned by both briefs warns", func(t *testing.T) {
		got := overlap(t, "- src/[ab].ts", "- src/*.ts\n")
		if !strings.Contains(got, "owns files that 'peer' is still editing: src/[ab].ts") {
			t.Fatalf("character-class overlap warning missing: %q", got)
		}
	})
	t.Run("a recursive glob against an owned directory warns", func(t *testing.T) {
		got := overlap(t, "- **/*.ts", "- src/components/\n")
		if !strings.Contains(got, "owns files that 'peer' is still editing: **/*.ts") {
			t.Fatalf("owned-directory overlap warning missing: %q", got)
		}
	})
	t.Run("a star glob against an owned directory warns", func(t *testing.T) {
		got := overlap(t, "- *.md", "- docs/\n")
		if !strings.Contains(got, "owns files that 'peer' is still editing: *.md") {
			t.Fatalf("owned-directory overlap warning missing: %q", got)
		}
	})
	t.Run("the D14 case stays silent", func(t *testing.T) {
		if got := overlap(t, "- *.workers.test.ts", "- src/a.ts\n"); strings.Contains(got, "owns files") {
			t.Fatalf("false overlap warning: %q", got)
		}
	})
	t.Run("incompatible-extension globs stay silent", func(t *testing.T) {
		if got := overlap(t, "- *.mjs", "- *.js\n"); strings.Contains(got, "owns files") {
			t.Fatalf("false overlap warning: %q", got)
		}
	})
	t.Run("dirglob", func(t *testing.T) {
		got := overlap(t, "- src/*/", "- src/components/\n")
		if !strings.Contains(got, "owns files") {
			t.Errorf("missing glob-directory overlap warning: %q", got)
		}
	})

}

func TestDispatchHintsR3DirectoryLint(t *testing.T) {
	h := newTM3bHarness(t, strings.Replace(tm3bFullBrief(), "internal/a.go", "- docs/", 1))
	for _, d := range []string{filepath.Join(h.root, "docs"), filepath.Join(h.root, "worker")} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	h.env["HERDR_SOHO_BRIEF_LINT"] = "warn"
	options := dispatch.BriefLintOptions{WorkerCwd: filepath.Join(h.root, "worker"), OrchestratorCwd: h.root}
	a := dispatch.BriefLintFindings(h.brief, h.ctx, h.env, options)
	body, err := os.ReadFile(h.brief)
	if err != nil {
		t.Fatal(err)
	}
	owned := ownedPaths(string(body), nil)
	if len(owned) != 1 || owned[0] != "docs/" {
		t.Errorf("directory ownership lost: %v", owned)
	}
	t.Logf("owned=%v; missing=%q; unseen=%q", owned, a.MissingMessage, a.Warnings)
	if a.MissingMessage != "" || len(a.Warnings) != 1 || !strings.Contains(a.Warnings[0], "cites 'docs/'") {
		t.Errorf("directory lint lost expected result: %+v", a)
	}
	if err := os.MkdirAll(filepath.Join(options.WorkerCwd, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	b := dispatch.BriefLintFindings(h.brief, h.ctx, h.env, options)
	t.Logf("after directory becomes visible: missing=%q warnings=%q", b.MissingMessage, b.Warnings)
	if b.MissingMessage != "" || len(b.Warnings) != 0 {
		t.Errorf("lint did not observe visibility transition: %+v", b)
	}
}
