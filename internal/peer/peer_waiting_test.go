package peer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// peerGitOnlyDir is the peer test's git-only PATH dir: the system git
// (symlinked, or copied where symlinks need privileges) and nothing else —
// the real herdr must stay off PATH so only the fake answers.
func peerGitOnlyDir(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required to prove the .gitignore stays untouched")
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

// waitingSendProject builds a git checkout as the send's cwd: with waiting
// the .herdr-soho dir waits for the setup migration; the fixture's SOHO_DIR
// is dropped so the state root is the default <project>/.soho. The target
// is the agent_not_found case the fake herdr harness already simulates:
// the send dies 4 after logging no-agent, before any prompt.
func waitingSendProject(t *testing.T, waiting bool) (dir string, f *fixture) {
	t.Helper()
	dir = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved // git reports the resolved spelling (macOS /tmp)
	}
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	if waiting {
		if err := os.MkdirAll(filepath.Join(dir, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f = newFixtureAt(t, t.TempDir(), []fakecli.Rule{
		{Argv: []string{"agent", "get", "w12:p1"}, Stderr: noAgentNotRef, Code: 1},
		{Argv: []string{"api", "snapshot"}, Stdout: `{"result":{"snapshot":{"workspaces":[{"workspace_id":"w12","label":"w12"}],"tabs":[],"agents":[],"panes":[]}}}`},
	})
	delete(f.env, "SOHO_DIR")      // the state root must be the default <dir>/.soho
	delete(f.env, "HERDR_PANE_ID") // the sender has no registered pane
	// The fixture PATH has no git: give the production code a git-only dir,
	// so a .gitignore append by the refusal-free path is detectable. The
	// fake herdr stays the only herdr on PATH.
	f.env["PATH"] = peerGitOnlyDir(t) + string(os.PathListSeparator) + f.env["PATH"]
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })
	return dir, f
}

func TestSendWritesNothingWhileFormerStateWaitsForSetup(t *testing.T) {
	t.Run("waiting: the no-agent 4 keeps its code and stderr, and nothing lands under .soho", func(t *testing.T) {
		dir, f := waitingSendProject(t, true)
		code, out, stderr := f.run([]string{"send", "w12:p1", "hello", "--timeout", "500"})
		if code != 4 || out != "" {
			t.Fatalf("the send keeps today's exit 4 and sends nothing: code=%d out=%q stderr=%q", code, out, stderr)
		}
		// The short-timeout warning still reaches stderr, only without the
		// friction.log under the state dir.
		want := "soho: warning: send: --timeout is in milliseconds; 500 is under a second (for 500 seconds pass 500000)\n" +
			"soho: send: no agent in local/w12:p1\n"
		if stderr != want {
			t.Fatalf("stderr=%q want %q", stderr, want)
		}
		for _, name := range []string{".soho", ".gitignore", "wait", "peer-messages.tsv"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatalf("the waiting project gained %s: %v", name, err)
			}
		}
	})
	t.Run("control: without the former dir the same send writes the peer log and the gitignore", func(t *testing.T) {
		dir, f := waitingSendProject(t, false)
		code, out, stderr := f.run([]string{"send", "w12:p1", "hello", "--timeout", "500"})
		if code != 4 || out != "" {
			t.Fatalf("the send keeps today's exit 4: code=%d out=%q stderr=%q", code, out, stderr)
		}
		if _, err := os.Stat(filepath.Join(dir, ".soho", "ws-test", "peer-messages.tsv")); err != nil {
			t.Fatalf("the control send must keep writing the peer log: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if err != nil || !strings.Contains(string(data), ".soho") {
			t.Fatalf("the control send must keep appending the gitignore: %q %v", data, err)
		}
	})
}
