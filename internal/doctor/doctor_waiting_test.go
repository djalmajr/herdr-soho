package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
)

// doctorGitOnlyDir is the doctor test's git-only PATH: the production code
// under test resolves git (so a .gitignore append would be detectable) and
// nothing else — no herdr, no agent CLIs.
func doctorGitOnlyDir(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("locate the system git: %v", err)
	}
	name := "git"
	if runtime.GOOS == "windows" {
		name = "git.exe"
	}
	dir := t.TempDir()
	target := filepath.Join(dir, name)
	if err := os.Symlink(path, target); err != nil {
		t.Skipf("copying the system git: %v", err)
	}
	return dir
}

// TestDoctorKeepsTheWaitingFormerStateUntouched pins the DJA-318 doctor
// contract: while the .herdr-soho dir waits for the setup migration, the
// state dir check prints the decision line instead of probing — no
// StateRoot, no MkdirAll, so neither the .soho dir nor the .gitignore is
// touched. The LegacyLeftovers leftover warning keeps printing as before.
func TestDoctorKeepsTheWaitingFormerStateUntouched(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved // git reports the resolved spelling (macOS /tmp)
	}
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitignore := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(gitignore, []byte("# pre-existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	env := platform.Env{
		"HERDR_ENV": "1", "HOME": home, "USERPROFILE": home,
		"SOHO_SKILL_DIR": filepath.Join(t.TempDir(), "no-skill"), "PATH": doctorGitOnlyDir(t),
	}
	ctx := core.Config{Entries: map[string]core.ConfigEntry{}}
	var out strings.Builder
	DoctorCheck(&ctx, env, root, &out)
	got := out.String()
	wantLine := "ok     state dir not created: 'soho setup' moves " + filepath.Join(root, ".herdr-soho") + " to " + filepath.Join(root, ".soho")
	if !strings.Contains(got, wantLine) {
		t.Fatalf("expected %q in:\n%s", wantLine, got)
	}
	if strings.Contains(got, "state dir writable") {
		t.Fatalf("the doctor probed the state dir instead of saying it is not created:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".soho")); !os.IsNotExist(err) {
		t.Fatalf("doctor created the soho dir: %v", err)
	}
	data, err := os.ReadFile(gitignore)
	if err != nil || string(data) != "# pre-existing\n" {
		t.Fatalf(".gitignore changed by the doctor: %q err=%v", data, err)
	}
	// The LegacyLeftovers leftover warning for the former state dir keeps
	// printing as before.
	if !strings.Contains(got, "former state dir "+filepath.Join(root, ".herdr-soho")+" is no longer used: run 'soho setup' to move it to "+filepath.Join(root, ".soho")+" once no worker is live") {
		t.Fatalf("legacy leftover warning missing:\n%s", got)
	}
}
