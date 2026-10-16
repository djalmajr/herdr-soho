package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// waitingGitOnlyDir is the DJA-318 CLI tests' restricted PATH: the system
// git resolves (so a .gitignore append would be detectable) and nothing
// else — no herdr, no agent CLIs.
func waitingGitOnlyDir(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required for the waiting-state CLI test")
	}
	name := "git"
	if runtime.GOOS == "windows" {
		name = "git.exe"
	}
	dir := t.TempDir()
	target := filepath.Join(dir, name)
	if err := os.Symlink(path, target); err != nil {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("reading the system git: %v", readErr)
		}
		if writeErr := os.WriteFile(target, data, 0o755); writeErr != nil {
			t.Fatalf("copying the system git: %v", writeErr)
		}
	}
	return dir
}

// TestHookLeavesTheWaitingFormerStateUntouched pins the DJA-318 hook
// contract: a Claude hook must exit 0 (a nonzero exit blocks the prompt)
// and must not write under the .soho dir while the .herdr-soho dir waits
// for the setup migration.
func TestHookLeavesTheWaitingFormerStateUntouched(t *testing.T) {
	env, repo := commandFixture(t)
	delete(env, "SOHO_DIR") // the state root must be the default <repo>/.soho
	env["HERDR_ENV"] = "1"
	env["PATH"] = waitingGitOnlyDir(t)
	if err := os.MkdirAll(filepath.Join(repo, ".herdr-soho"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("# pre-existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := runIn(t, []string{"hook", "reminder"}, env, repo)
	if code != 0 || out != hookReminder+"\n" || stderr != "" {
		t.Fatalf("reminder: %d %q %q", code, out, stderr)
	}
	code, out, stderr = runIn(t, []string{"hook", "doctor"}, env, repo)
	if code != 0 || stderr != "" {
		t.Fatalf("doctor: %d %q %q", code, out, stderr)
	}
	for _, name := range []string{".soho", "wait"} {
		if _, err := os.Stat(filepath.Join(repo, name)); !os.IsNotExist(err) {
			t.Fatalf("the hook left %s in the project: %v", name, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil || string(data) != "# pre-existing\n" {
		t.Fatalf(".gitignore changed by the hook: %q %v", data, err)
	}
}

// TestSetupKeepsTheFormerStateWithWorkersWithoutRefusal pins the DJA-318
// setup contract: soho setup is the command that resolves the wait, so it
// never receives the refusal — even when the migration keeps the
// .herdr-soho dir because a worker is still listed in its roster.
func TestSetupKeepsTheFormerStateWithWorkersWithoutRefusal(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved // git reports the resolved spelling (macOS /tmp)
	}
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".herdr-soho", "ws"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A live worker in the old roster: the migration must keep .herdr-soho.
	if err := os.WriteFile(filepath.Join(root, ".herdr-soho", "ws", "agents.tsv"), []byte("build\tw0test:p0a\tpi\timplementer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	env := platform.Env{
		"HOME": home, "USERPROFILE": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"SOHO_SKILL_DIR": testSkillDir(t), "HERDR_WORKSPACE_ID": "ws", "PATH": waitingGitOnlyDir(t),
	}
	code, out, stderr := runIn(t, []string{"setup", "--no-hooks", "--target", "AGENTS.md"}, env, root)
	if code != 0 {
		t.Fatalf("setup code=%d out=%q err=%q", code, out, stderr)
	}
	wantKept := "migrate: kept " + filepath.Join(root, ".herdr-soho") + ": workers are still in its roster; release them with 'soho release <name> --close' and run 'soho setup' again"
	if !strings.Contains(out, wantKept) {
		t.Fatalf("setup must report the kept former dir:\n%s", out)
	}
	for _, stream := range []string{out, stderr} {
		if strings.Contains(stream, "is waiting for 'soho setup'") {
			t.Fatalf("setup received the refusal: %q", stream)
		}
	}
	// The setup completed (block written) and the migration left the former
	// dir in place, still waiting.
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatalf("setup did not write the block: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".herdr-soho", "ws", "agents.tsv")); err != nil {
		t.Fatalf("the kept former state dir lost its roster: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".soho")); !os.IsNotExist(err) {
		t.Fatalf("setup created the soho dir while the former state waits: %v", err)
	}
}

// TestStateOpeningCommandsRefuseWhileFormerStateWaits pins the DJA-318 CLI
// refusal: while the former state dir waits for the setup migration, the
// commands that open the state dir (init, spawn, dispatch, status, wait)
// exit 2 with the setup instruction and leave the project untouched — no
// .soho, no .gitignore append.
func TestStateOpeningCommandsRefuseWhileFormerStateWaits(t *testing.T) {
	env, repo := commandFixture(t)
	delete(env, "SOHO_DIR") // the state root must be the default <repo>/.soho
	env["HERDR_ENV"] = "1"
	env["HERDR_WORKSPACE_ID"] = "w13"
	// The restricted PATH: only the system git (a .gitignore append stays
	// detectable) and a fake herdr (the refusal fires before any herdr call,
	// so the fake only needs to exist).
	binDir := t.TempDir()
	if _, err := fakecli.Install(t, binDir, "herdr", []fakecli.Rule{{AnyArgs: true}}); err != nil {
		t.Fatal(err)
	}
	env["PATH"] = waitingGitOnlyDir(t) + string(os.PathListSeparator) + binDir
	if err := os.MkdirAll(filepath.Join(repo, ".herdr-soho"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("# pre-existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantRefusal := "the former state dir '" + filepath.Join(repo, ".herdr-soho") + "' is waiting for 'soho setup'"
	for _, argv := range [][]string{
		{"init"},
		{"spawn", "implementer", "build"},
		{"dispatch"},
		{"status"},
		{"wait"},
	} {
		code, out, stderr := runIn(t, argv, env, repo)
		if code != 2 || !strings.Contains(stderr, wantRefusal) {
			t.Fatalf("%v: code=%d out=%q err=%q", argv, code, out, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".soho")); !os.IsNotExist(err) {
		t.Fatalf("the commands created the soho dir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil || string(data) != "# pre-existing\n" {
		t.Fatalf(".gitignore changed by the commands: %q %v", data, err)
	}
}

// TestStateDirCreatesStateAfterSetupMovesTheFormerState pins the DJA-318
// after-state: once the setup migration has moved the .herdr-soho dir to
// .soho, the state dir opens again as usual.
func TestStateDirCreatesStateAfterSetupMovesTheFormerState(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	// An empty roster: the migration moves .herdr-soho to .soho.
	if err := os.MkdirAll(filepath.Join(root, ".herdr-soho", "ws"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".herdr-soho", "ws", "agents.tsv"), []byte("# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	env := platform.Env{
		"HOME": home, "USERPROFILE": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"SOHO_SKILL_DIR": testSkillDir(t), "HERDR_WORKSPACE_ID": "ws", "PATH": waitingGitOnlyDir(t),
	}
	code, out, stderr := runIn(t, []string{"setup", "--no-hooks", "--target", "AGENTS.md"}, env, root)
	if code != 0 || !strings.Contains(out, "migrate: moved "+filepath.Join(root, ".herdr-soho")+" to "+filepath.Join(root, ".soho")) {
		t.Fatalf("setup code=%d out=%q err=%q", code, out, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".herdr-soho")); !os.IsNotExist(err) {
		t.Fatalf("the former dir survived the setup move: %v", err)
	}
	// After the move, the state dir opens again as usual.
	ctx := core.LoadConfig(env, root)
	dir := core.StateDir(&ctx, env, root)
	if dir != filepath.Join(root, ".soho", "ws") {
		t.Fatalf("dir=%q", dir)
	}
	for _, child := range []string{"briefs", "reports", "wait"} {
		if _, err := os.Stat(filepath.Join(dir, child)); err != nil {
			t.Fatalf("%s missing after the setup move: %v", child, err)
		}
	}
}
