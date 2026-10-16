package setup

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
)

// ignoreRepoEnv is the hermetic migrateEnv with the real git in front of
// the (empty) PATH and a fresh git repository initialized at cwd, so the
// ignore-rule step and the git checks the assertions run use the same
// real repository.
func ignoreRepoEnv(t *testing.T) (platform.Env, string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required for the ignore-rule migration tests")
	}
	env, cwd := migrateEnv(t)
	cmd := exec.Command(git, "init", "-q", cwd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	env["PATH"] = filepath.Dir(git) + string(os.PathListSeparator) + env.Get("PATH")
	return env, cwd
}

// gitRun runs a git command inside root with the test process environment
// and fails the test when it exits non-zero.
func gitRun(t *testing.T, git, root string, args ...string) {
	t.Helper()
	cmd := exec.Command(git, append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// gitCheckIgnoreExit runs 'git check-ignore -q <rel>' exactly the way the
// acceptance check does and returns its exit code (0 means ignored).
func gitCheckIgnoreExit(t *testing.T, git, root, rel string) int {
	t.Helper()
	cmd := exec.Command(git, "-C", root, "check-ignore", "-q", rel)
	err := cmd.Run()
	if err == nil {
		return 0
	}
	if x, ok := err.(*exec.ExitError); ok {
		return x.ExitCode()
	}
	t.Fatalf("git check-ignore -q %s: %v", rel, err)
	return -1
}

// TestMigrateIgnoreRule: when the project .gitignore or the git exclude
// ignored a former project config, the migration extends that rule with
// the soho.conf name in the same file, so the migrated file is not left
// untracked.
func TestMigrateIgnoreRule(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required for the ignore-rule migration tests")
	}

	t.Run("gitignore with the slash rule: the new line lands right after the old one", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		root := platform.ProjectRoot(env, cwd)
		gitignore := filepath.Join(root, ".gitignore")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "# project\n/.agents/herdr-soho.conf\nnode_modules\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"), "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		old := filepath.Join(root, ".agents", "herdr-soho.conf")
		sohoConf := filepath.Join(root, ".agents", "soho.conf")
		want := []string{
			"migrate: moved " + old + " to " + sohoConf,
			"migrate: added /.agents/soho.conf to " + gitignore,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustNotExist(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"))
		if got := mustReadFile(t, sohoConf); got != "panes=2\n" {
			t.Fatalf("soho.conf = %q", got)
		}
		if got := mustReadFile(t, gitignore); got != "# project\n/.agents/herdr-soho.conf\n/.agents/soho.conf\nnode_modules\n" {
			t.Fatalf(".gitignore = %q", got)
		}
		if code := gitCheckIgnoreExit(t, git, root, ".agents/soho.conf"); code != 0 {
			t.Fatalf("git check-ignore -q .agents/soho.conf exit %d, want 0", code)
		}
	})

	t.Run("gitignore without the slash: the new line copies the no-slash style", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		root := platform.ProjectRoot(env, cwd)
		gitignore := filepath.Join(root, ".gitignore")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "\n.agents/herdr-agents.conf\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-agents.conf"), "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		old := filepath.Join(root, ".agents", "herdr-agents.conf")
		sohoConf := filepath.Join(root, ".agents", "soho.conf")
		want := []string{
			"migrate: moved " + old + " to " + sohoConf,
			"migrate: added .agents/soho.conf to " + gitignore,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if got := mustReadFile(t, gitignore); got != "\n.agents/herdr-agents.conf\n.agents/soho.conf\n" {
			t.Fatalf(".gitignore = %q", got)
		}
		if code := gitCheckIgnoreExit(t, git, root, ".agents/soho.conf"); code != 0 {
			t.Fatalf("git check-ignore -q .agents/soho.conf exit %d, want 0", code)
		}
	})

	t.Run("rule only in info/exclude: the exclude is extended and the gitignore is untouched", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		root := platform.ProjectRoot(env, cwd)
		gitignore := filepath.Join(root, ".gitignore")
		exclude := filepath.Join(root, ".git", "info", "exclude")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "# project\n")
		mustWriteFile(t, exclude, "# git exclude\n/.agents/herdr-soho.conf\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"), "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		old := filepath.Join(root, ".agents", "herdr-soho.conf")
		sohoConf := filepath.Join(root, ".agents", "soho.conf")
		want := []string{
			"migrate: moved " + old + " to " + sohoConf,
			"migrate: added /.agents/soho.conf to " + exclude,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if got := mustReadFile(t, exclude); got != "# git exclude\n/.agents/herdr-soho.conf\n/.agents/soho.conf\n" {
			t.Fatalf("exclude = %q", got)
		}
		if got := mustReadFile(t, gitignore); got != "# project\n" {
			t.Fatalf(".gitignore changed to %q", got)
		}
		if code := gitCheckIgnoreExit(t, git, root, ".agents/soho.conf"); code != 0 {
			t.Fatalf("git check-ignore -q .agents/soho.conf exit %d, want 0", code)
		}
	})

	t.Run("dry: would add is reported and the whole tree is byte-identical", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		root := platform.ProjectRoot(env, cwd)
		gitignore := filepath.Join(root, ".gitignore")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "# project\n/.agents/herdr-soho.conf\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"), "panes=2\n")
		before := treeListing(t, cwd)
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, true)
		old := filepath.Join(root, ".agents", "herdr-soho.conf")
		sohoConf := filepath.Join(root, ".agents", "soho.conf")
		want := []string{
			"migrate: would move " + old + " to " + sohoConf,
			"migrate: would add /.agents/soho.conf to " + gitignore,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if after := treeListing(t, cwd); after != before {
			t.Fatalf("dry run changed the tree:\nbefore:\n%s\nafter:\n%s", before, after)
		}
		mustExist(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"))
	})

	t.Run("guard: an already ignored .agents dir: no line and no message", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), ".agents/\n/.agents/herdr-soho.conf\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "soho.conf"), "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		if len(lines) != 0 {
			t.Fatalf("lines = %v, want none", lines)
		}
		if got := mustReadFile(t, filepath.Join(cwd, ".gitignore")); got != ".agents/\n/.agents/herdr-soho.conf\n" {
			t.Fatalf(".gitignore = %q", got)
		}
	})

	t.Run("guard: a tracked former config without an ignore rule: no line", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		root := platform.ProjectRoot(env, cwd)
		gitignore := filepath.Join(root, ".gitignore")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "# project\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"), "panes=2\n")
		gitRun(t, git, root, "config", "user.name", "test")
		gitRun(t, git, root, "config", "user.email", "test@example.com")
		gitRun(t, git, root, "add", ".agents/herdr-soho.conf")
		gitRun(t, git, root, "commit", "-qm", "initial")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		old := filepath.Join(root, ".agents", "herdr-soho.conf")
		sohoConf := filepath.Join(root, ".agents", "soho.conf")
		want := []string{
			"migrate: moved " + old + " to " + sohoConf,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if got := mustReadFile(t, gitignore); got != "# project\n" {
			t.Fatalf(".gitignore changed to %q", got)
		}
		if code := gitCheckIgnoreExit(t, git, root, ".agents/soho.conf"); code != 1 {
			t.Fatalf("git check-ignore -q .agents/soho.conf exit %d, want 1 (not ignored)", code)
		}
	})

	t.Run("repeated setup: the line lands once and the later runs add nothing", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		root := platform.ProjectRoot(env, cwd)
		gitignore := filepath.Join(root, ".gitignore")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "# project\n/.agents/herdr-soho.conf\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "soho.conf"), "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: added /.agents/soho.conf to " + gitignore,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("first run lines = %v, want %v", lines, want)
		}
		first := mustReadFile(t, gitignore)
		if first != "# project\n/.agents/herdr-soho.conf\n/.agents/soho.conf\n" {
			t.Fatalf(".gitignore = %q", first)
		}
		for i, run := 2, 3; i <= run; i++ {
			lines = migrateNames(&ctx, env, cwd, false)
			if len(lines) != 0 {
				t.Fatalf("run %d lines = %v, want none", i, lines)
			}
			if got := mustReadFile(t, gitignore); got != first {
				t.Fatalf("run %d changed .gitignore to %q", i, got)
			}
		}
	})

	t.Run("crlf file: the new line uses crlf", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		root := platform.ProjectRoot(env, cwd)
		gitignore := filepath.Join(root, ".gitignore")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "win\r\n/.agents/herdr-soho.conf\r\nmore\r\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"), "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		old := filepath.Join(root, ".agents", "herdr-soho.conf")
		sohoConf := filepath.Join(root, ".agents", "soho.conf")
		want := []string{
			"migrate: moved " + old + " to " + sohoConf,
			"migrate: added /.agents/soho.conf to " + gitignore,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if got := mustReadFile(t, gitignore); got != "win\r\n/.agents/herdr-soho.conf\r\n/.agents/soho.conf\r\nmore\r\n" {
			t.Fatalf(".gitignore = %q", got)
		}
	})

	t.Run("outside a git repository: nothing changes and no message from the step", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		env["PATH"] = filepath.Dir(git) + string(os.PathListSeparator) + env.Get("PATH")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "# project\n/.agents/herdr-soho.conf\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"), "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		old := filepath.Join(cwd, ".agents", "herdr-soho.conf")
		sohoConf := filepath.Join(cwd, ".agents", "soho.conf")
		want := []string{
			"migrate: moved " + old + " to " + sohoConf,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if got := mustReadFile(t, filepath.Join(cwd, ".gitignore")); got != "# project\n/.agents/herdr-soho.conf\n" {
			t.Fatalf(".gitignore = %q", got)
		}
		if got := mustReadFile(t, sohoConf); got != "panes=2\n" {
			t.Fatalf("soho.conf = %q", got)
		}
	})

	t.Run("former rule in both files: only the gitignore (the first one) is extended", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		root := platform.ProjectRoot(env, cwd)
		gitignore := filepath.Join(root, ".gitignore")
		exclude := filepath.Join(root, ".git", "info", "exclude")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "# p\n/.agents/herdr-soho.conf\n")
		mustWriteFile(t, exclude, "# e\n/.agents/herdr-agents.conf\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"), "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		old := filepath.Join(root, ".agents", "herdr-soho.conf")
		sohoConf := filepath.Join(root, ".agents", "soho.conf")
		want := []string{
			"migrate: moved " + old + " to " + sohoConf,
			"migrate: added /.agents/soho.conf to " + gitignore,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if got := mustReadFile(t, gitignore); got != "# p\n/.agents/herdr-soho.conf\n/.agents/soho.conf\n" {
			t.Fatalf(".gitignore = %q", got)
		}
		if got := mustReadFile(t, exclude); got != "# e\n/.agents/herdr-agents.conf\n" {
			t.Fatalf("exclude changed to %q", got)
		}
	})

	t.Run("former rule is the last line without a final newline: the new rule gets its own line", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		root := platform.ProjectRoot(env, cwd)
		gitignore := filepath.Join(root, ".gitignore")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "# project\n/.agents/herdr-soho.conf")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"), "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		old := filepath.Join(root, ".agents", "herdr-soho.conf")
		sohoConf := filepath.Join(root, ".agents", "soho.conf")
		want := []string{
			"migrate: moved " + old + " to " + sohoConf,
			"migrate: added /.agents/soho.conf to " + gitignore,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if got := mustReadFile(t, gitignore); got != "# project\n/.agents/herdr-soho.conf\n/.agents/soho.conf" {
			t.Fatalf(".gitignore = %q", got)
		}
		if code := gitCheckIgnoreExit(t, git, root, ".agents/soho.conf"); code != 0 {
			t.Fatalf("git check-ignore -q .agents/soho.conf exit %d, want 0", code)
		}
	})

	t.Run("crlf file whose last line has no ending: the new rule lands after crlf", func(t *testing.T) {
		env, cwd := ignoreRepoEnv(t)
		root := platform.ProjectRoot(env, cwd)
		gitignore := filepath.Join(root, ".gitignore")
		mustWriteFile(t, filepath.Join(cwd, ".gitignore"), "win\r\n/.agents/herdr-soho.conf")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"), "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		old := filepath.Join(root, ".agents", "herdr-soho.conf")
		sohoConf := filepath.Join(root, ".agents", "soho.conf")
		want := []string{
			"migrate: moved " + old + " to " + sohoConf,
			"migrate: added /.agents/soho.conf to " + gitignore,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if got := mustReadFile(t, gitignore); got != "win\r\n/.agents/herdr-soho.conf\r\n/.agents/soho.conf" {
			t.Fatalf(".gitignore = %q", got)
		}
		if code := gitCheckIgnoreExit(t, git, root, ".agents/soho.conf"); code != 0 {
			t.Fatalf("git check-ignore -q .agents/soho.conf exit %d, want 0", code)
		}
	})
}
