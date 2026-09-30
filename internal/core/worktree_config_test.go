package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// linkedWorktreeRepo builds a git repository with an initial commit and a
// linked worktree beside it, returning the environment, the main checkout
// root, the worktree root, and the main checkout's new-style project file.
func linkedWorktreeRepo(t *testing.T) (platform.Env, string, string, string) {
	t.Helper()
	env, repo := fixture(t)
	root, err := filepath.EvalSymlinks(filepath.Dir(repo))
	if err != nil {
		t.Fatal(err)
	}
	canonicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalRepo, "tracked"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, canonicalRepo, "config", "user.email", "test@example.invalid")
	gitRun(t, canonicalRepo, "config", "user.name", "Test")
	gitRun(t, canonicalRepo, "add", "tracked")
	gitRun(t, canonicalRepo, "commit", "-qm", "initial")
	wt := filepath.Join(root, "wt")
	gitRun(t, canonicalRepo, "worktree", "add", "-q", "-b", "wt-branch", wt)
	canonicalWt, err := filepath.EvalSymlinks(wt)
	if err != nil {
		t.Fatal(err)
	}
	mainConf := filepath.Join(canonicalRepo, ".agents", "herdr-soho.conf")
	return env, canonicalRepo, canonicalWt, mainConf
}

func TestA13LinkedWorktreeProjectConfig(t *testing.T) {
	t.Run("file only in the main checkout: the linked worktree reads and writes it", func(t *testing.T) {
		env, repo, wt, mainConf := linkedWorktreeRepo(t)
		write(t, mainConf, "max_workers=7\nlane.review.kind=claude\n")
		used := ProjectConfigFileUsed(env, wt)
		if used != mainConf {
			t.Fatalf("project file used from the worktree = %q, want %q", used, mainConf)
		}
		ctx := LoadConfig(env, wt)
		if got := Cfg(&ctx, "max_workers", "", env); got != "7" || CfgSource(&ctx, "max_workers", env) != "project" {
			t.Fatalf("max_workers from the worktree = %q (%s), want 7 (project)", got, CfgSource(&ctx, "max_workers", env))
		}
		if got := Cfg(&ctx, "lane_review_kind", "", env); got != "claude" || CfgSource(&ctx, "lane_review_kind", env) != "project" {
			t.Fatalf("lane.review.kind from the worktree = %q (%s), want claude (project)", got, CfgSource(&ctx, "lane_review_kind", env))
		}
		// config set --project writes to the same target the read used.
		out, _ := capture(t, func() {
			CmdConfigSet([]string{"--project", "max_workers", "9"}, &ctx, env, wt)
		})
		if !strings.Contains(out, "set max_workers=9 in "+mainConf) {
			t.Fatalf("config set out=%q", out)
		}
		data, err := os.ReadFile(mainConf)
		if err != nil || !strings.Contains(string(data), "max_workers=9") {
			t.Fatalf("main checkout file = %q err=%v", data, err)
		}
		if _, err := os.Stat(filepath.Join(wt, ".agents", "herdr-soho.conf")); !os.IsNotExist(err) {
			t.Fatalf("config set created a file in the worktree: %v", err)
		}
		refreshed := LoadConfig(env, wt)
		if got := Cfg(&refreshed, "max_workers", "", env); got != "9" {
			t.Fatalf("after config set, max_workers = %q", got)
		}
		// The main checkout still reads the same file.
		mainCtx := LoadConfig(env, repo)
		if got := Cfg(&mainCtx, "max_workers", "", env); got != "9" || CfgSource(&mainCtx, "max_workers", env) != "project" {
			t.Fatalf("main checkout max_workers = %q (%s)", got, CfgSource(&mainCtx, "max_workers", env))
		}
	})
	t.Run("file in both: the worktree's file wins in the worktree, the main file in the main checkout", func(t *testing.T) {
		env, repo, wt, mainConf := linkedWorktreeRepo(t)
		wtConf := filepath.Join(wt, ".agents", "herdr-soho.conf")
		write(t, mainConf, "max_workers=7\n")
		write(t, wtConf, "max_workers=5\n")
		if got := ProjectConfigFileUsed(env, wt); got != wtConf {
			t.Fatalf("project file used from the worktree = %q, want %q", got, wtConf)
		}
		if got := ConfigFileFor("project", env, wt); got != wtConf {
			t.Fatalf("config set target from the worktree = %q, want %q", got, wtConf)
		}
		wtCtx := LoadConfig(env, wt)
		if got := Cfg(&wtCtx, "max_workers", "", env); got != "5" {
			t.Fatalf("worktree max_workers = %q, want 5", got)
		}
		repoCtx := LoadConfig(env, repo)
		if got := Cfg(&repoCtx, "max_workers", "", env); got != "7" {
			t.Fatalf("main checkout max_workers = %q, want 7", got)
		}
	})
	t.Run("linked worktree without any config: the write target is the main checkout's file", func(t *testing.T) {
		env, repo, wt, mainConf := linkedWorktreeRepo(t)
		if got := ProjectConfigFileUsed(env, wt); got != "" {
			t.Fatalf("project file used = %q, want none", got)
		}
		if got := ConfigFileFor("project", env, wt); got != mainConf {
			t.Fatalf("config set target = %q, want %q", got, mainConf)
		}
		wtCtx := LoadConfig(env, wt)
		if got := Cfg(&wtCtx, "max_workers", "", env); got != "3" {
			t.Fatalf("max_workers = %q, want the default 3", got)
		}
		if got := CfgSource(&wtCtx, "max_workers", env); got == "project" {
			t.Fatalf("max_workers source = %q, want no project layer", got)
		}
		// An ordinary checkout keeps its own new-style path as the target.
		if got := ConfigFileFor("project", env, repo); got != mainConf {
			t.Fatalf("main checkout config set target = %q, want %q", got, mainConf)
		}
	})
	t.Run("linked worktree falls back to the main checkout's legacy file", func(t *testing.T) {
		env, _, wt, mainConf := linkedWorktreeRepo(t)
		mainLegacy := strings.TrimSuffix(mainConf, "herdr-soho.conf") + "herdr-agents.conf"
		write(t, mainLegacy, "max_workers=11\n")
		if got := ProjectConfigFileUsed(env, wt); got != mainLegacy {
			t.Fatalf("project file used from the worktree = %q, want %q", got, mainLegacy)
		}
		ctx := LoadConfig(env, wt)
		if got := Cfg(&ctx, "max_workers", "", env); got != "11" || CfgSource(&ctx, "max_workers", env) != "project" {
			t.Fatalf("max_workers = %q (%s), want 11 (project)", got, CfgSource(&ctx, "max_workers", env))
		}
		if got := ConfigFileFor("project", env, wt); got != mainLegacy {
			t.Fatalf("config set target = %q, want %q", got, mainLegacy)
		}
	})
}
