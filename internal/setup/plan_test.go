package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
)

func TestSetupPlanJavaScriptCases(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		// JS: "unifiedDiff: equal contents produce no diff"
		{"unifiedDiff: equal contents produce no diff", func(t *testing.T) {
			if got := UnifiedDiff("a\nb\n", "a\nb\n", "f"); got != "" {
				t.Fatalf("diff=%q", got)
			}
		}},
		// JS: "unifiedDiff: a new file is one insertion hunk (old side 0,0)"
		{"unifiedDiff: a new file is one insertion hunk (old side 0,0)", func(t *testing.T) {
			got := UnifiedDiff("", "x\n", "f")
			if !strings.Contains(got, "@@ -0,0 +1 @@\n+x\n") {
				t.Fatalf("diff=%q", got)
			}
		}},
		// JS: "unifiedDiff: an emptied file is one deletion hunk (new side 0,0)"
		{"unifiedDiff: an emptied file is one deletion hunk (new side 0,0)", func(t *testing.T) {
			got := UnifiedDiff("x\n", "", "f")
			if !strings.Contains(got, "@@ -1 +0,0 @@\n-x\n") {
				t.Fatalf("diff=%q", got)
			}
		}},
		// JS: "unifiedDiff: an append at the end keeps the context and adds the lines"
		{"unifiedDiff: an append at the end keeps the context and adds the lines", func(t *testing.T) {
			got := UnifiedDiff("a\nb\n", "a\nb\nc\n", "f")
			if !strings.Contains(got, " a\n b\n+c\n") {
				t.Fatalf("diff=%q", got)
			}
		}},
		// JS: "unifiedDiff: a change in the middle, deletion before insertion within the block"
		{"unifiedDiff: a change in the middle, deletion before insertion within the block", func(t *testing.T) {
			got := UnifiedDiff("a\nb\nc\n", "a\nB\nc\n", "f")
			if !strings.Contains(got, "-b\n+B\n") {
				t.Fatalf("diff=%q", got)
			}
		}},
		// JS: "unifiedDiff: no final newline — markers on the affected ends, once per line"
		{"unifiedDiff: no final newline — markers on the affected ends, once per line", func(t *testing.T) {
			got := UnifiedDiff("a", "b", "f")
			if strings.Count(got, `\ No newline at end of file`) != 2 {
				t.Fatalf("diff=%q", got)
			}
		}},
		// JS: "unifiedDiff: several hunks — close edits merge, far edits split"
		{"unifiedDiff: several hunks — close edits merge, far edits split", func(t *testing.T) {
			before := "0\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n"
			after := "zero\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\nnineteen\n"
			got := UnifiedDiff(before, after, "f")
			if strings.Count(got, "@@ ") != 2 {
				t.Fatalf("diff=%q", got)
			}
		}},
		// JS: "confKeys: distinct keys in file order, comments and odd lines skipped"
		{"confKeys: distinct keys in file order, comments and odd lines skipped", func(t *testing.T) {
			got := confKeys("# ignored\nodd\na=1 # comment\nb=2\na=3\n =empty\n")
			if strings.Join(got, ",") != "a,b" {
				t.Fatalf("keys=%q", got)
			}
		}},
		// JS: "planDiffFile: new, changed, removed and unchanged keys, with the before file absent"
		{"planDiffFile: new, changed, removed and unchanged keys, with the before file absent", func(t *testing.T) {
			got := planConfigDiff("old=1\nchange=before\nsame=x\n", "new=2\nchange=after\nsame=x\n")
			for _, want := range []string{"old", "new", "change", "(unset)", "(removed)"} {
				if !strings.Contains(got, want) {
					t.Fatalf("diff lacks %q: %s", want, got)
				}
			}
			if strings.Contains(got, "same") {
				t.Fatalf("unchanged key included: %s", got)
			}
		}},
		// JS: "planDiffFile: comments do not create keys; the last assignment wins; keys keep their dotted form"
		{"planDiffFile: comments do not create keys; the last assignment wins; keys keep their dotted form", func(t *testing.T) {
			got := planConfigDiff("# max_workers=2\nodd line\nlane.build.kind=pi\nmax_workers=2 # old\nmax_workers=3\n", "lane.build.kind=pi\nmax_workers=4\n")
			if strings.Contains(got, "odd") || strings.Contains(got, "#") || !strings.Contains(got, "max_workers") || !strings.Contains(got, "3 → 4") {
				t.Fatalf("diff=%s", got)
			}
		}},
		// JS: "planFileDiff: the path, \"(no change)\" or the labeled diff, and the blank line"
		{"planFileDiff: the path, (no change) or the labeled diff, and the blank line", func(t *testing.T) {
			got := planFileDiff("guide.md", "same\n", "same\n")
			if got != "guide.md\n  (no change)\n\n" {
				t.Fatalf("diff=%q", got)
			}
		}},
		{"e2e: setup --plan --panes prints the plan and writes nothing", func(t *testing.T) {
			root, env, ctx := localPlanFixture(t)
			before := setupFilesystemSnapshot(t, root)
			code, out, _, msg := runPlanStreams([]string{"--panes", "3", "--no-hooks"}, ctx, env, root)
			if code != 0 || msg != "" || !strings.Contains(out, "plan (nothing is written)") {
				t.Fatalf("code=%d msg=%q out=%q", code, msg, out)
			}
			if after := setupFilesystemSnapshot(t, root); len(after) != len(before) {
				t.Fatalf("plan changed filesystem: before=%v after=%v", before, after)
			}
		}},
		{"e2e: --panes 2 plans the 2-pane preset (nothing written)", func(t *testing.T) {
			root, env, ctx := localPlanFixture(t)
			code, out, _, msg := runPlanStreams([]string{"--panes", "2", "--no-hooks"}, ctx, env, root)
			if code != 0 || msg != "" || !strings.Contains(out, "panes") || !strings.Contains(out, "→ 2") {
				t.Fatalf("code=%d msg=%q out=%q", code, msg, out)
			}
			if _, err := os.Stat(filepath.Join(root, ".agents", "herdr-soho.conf")); !os.IsNotExist(err) {
				t.Fatalf("plan wrote config: %v", err)
			}
		}},
		{"e2e: --set, --user-set and --session-set together — the three sections, nothing written", func(t *testing.T) {
			root, env, ctx := localPlanFixture(t)
			env["HERDR_WORKSPACE_ID"] = "fixture-workspace"
			code, out, _, msg := runPlanStreams([]string{"--set", "max_workers", "5", "--user-set", "model.pi.worker", "fixture/model", "--session-set", "lane.build.kind", "pi", "--no-hooks"}, ctx, env, root)
			if code != 0 || msg != "" || !strings.Contains(out, "max_workers") || !strings.Contains(out, "model.pi.worker") || !strings.Contains(out, "lane.build.kind") {
				t.Fatalf("code=%d msg=%q out=%q", code, msg, out)
			}
			if _, err := os.Stat(core.ConfigFileFor("project", env, root)); !os.IsNotExist(err) {
				t.Fatalf("plan wrote project config: %v", err)
			}
		}},
		{"e2e: invalid keys/values and a missing flag value die 2 before anything is shown or written", func(t *testing.T) {
			for _, args := range [][]string{{"--set", "bogus", "x"}, {"--panes"}} {
				root, env, ctx := localPlanFixture(t)
				code, out, _, msg := runPlanStreams(args, ctx, env, root)
				if code != 2 || out != "" || msg == "" {
					t.Fatalf("args=%v code=%d out=%q msg=%q", args, code, out, msg)
				}
			}
		}},
		{"e2e: a write the real setup would refuse is refused by the plan (rc 4), file untouched", func(t *testing.T) {
			root, env, ctx := localPlanFixture(t)
			file := filepath.Join(root, ".claude", "settings.json")
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("{invalid json"), 0o600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(file)
			code, out, _, msg := runPlanStreams(nil, ctx, env, root)
			after, _ := os.ReadFile(file)
			if code != 4 || msg == "" || string(after) != string(before) {
				t.Fatalf("code=%d out=%q msg=%q after=%q", code, out, msg, after)
			}
		}},
		{"e2e: --target on a file with the block plans the in-place replacement; --no-hooks skips the hooks", func(t *testing.T) {
			root, env, ctx := localPlanFixture(t)
			file := filepath.Join(root, "custom.md")
			if err := os.WriteFile(file, []byte("custom\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			code, out, _, msg := runPlanStreams([]string{"--target", "custom.md", "--no-hooks"}, ctx, env, root)
			if code != 0 || msg != "" || !strings.Contains(out, file) || strings.Contains(out, "settings.json") {
				t.Fatalf("code=%d msg=%q out=%q", code, msg, out)
			}
			got, _ := os.ReadFile(file)
			if string(got) != "custom\n" {
				t.Fatalf("plan wrote target: %q", got)
			}
		}},
		{"e2e: the .gitignore entry is planned (not written) when the state dir would not be ignored", func(t *testing.T) {
			root, env, ctx := localPlanFixture(t)
			env["HERDR_SOHO_DIR"] = "state-cache"
			code, out, _, msg := runPlanStreams([]string{"--no-hooks"}, ctx, env, root)
			if code != 0 || msg != "" || !strings.Contains(out, ".gitignore") || !strings.Contains(out, "state-cache") {
				t.Fatalf("code=%d msg=%q out=%q", code, msg, out)
			}
			if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
				t.Fatalf("plan wrote .gitignore: %v", err)
			}
		}},
		{"e2e: --session-set without a resolvable workspace dies 2", func(t *testing.T) {
			root, env, ctx := localPlanFixture(t)
			delete(env, "HERDR_WORKSPACE_ID")
			code, out, _, msg := runPlanStreams([]string{"--session-set", "lane.build.kind", "pi"}, ctx, env, root)
			if code != 2 || out != "" || !strings.Contains(msg, "workspace") {
				t.Fatalf("code=%d out=%q msg=%q", code, out, msg)
			}
		}},
		{"cmdSetupPlan: in-process — DieError 2 for a bad lane spec, stdout untouched", func(t *testing.T) {
			root, env, ctx := localPlanFixture(t)
			code, out, _, msg := runPlanStreams([]string{"--lane", "build=unknown"}, ctx, env, root)
			if code != 2 || out != "" || msg == "" {
				t.Fatalf("code=%d out=%q msg=%q", code, out, msg)
			}
		}},
	}
	for _, tc := range tests {
		t.Run("// JS: \""+tc.name+"\"", func(t *testing.T) { // Mutation captured: changing a planned before/after value or writing during plan changes observable preview semantics.
			tc.run(t)
		})
	}
}
