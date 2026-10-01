package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func a13SkillDir(t *testing.T) string {
	t.Helper()
	// go test runs in the package directory; runtime.Caller would give the
	// path the binary was built from, which is not this host's on Windows runs.
	pkg, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	skillDir, err := filepath.Abs(filepath.Join(pkg, "..", "..", "skills", "herdr-soho"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err == nil {
		return skillDir
	}
	t.Fatalf("skill directory %q missing", skillDir)
	return ""
}

func a13Git(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), output, err)
	}
	return strings.TrimSpace(string(output))
}

// a13GitOnlyPath puts only the system git on PATH, so doctor's herdr probes
// see no real or fake herdr binary.
func a13GitOnlyPath(t *testing.T) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required for the linked worktree doctor fixture")
	}
	bin := t.TempDir()
	// The product locates git the way JS does on win32: name plus a PATHEXT
	// extension, so the link must carry the real binary's extension
	// (git.exe); an extensionless name is only ever found on POSIX.
	link := filepath.Join(bin, "git"+filepath.Ext(gitPath))
	if err := os.Symlink(gitPath, link); err != nil {
		// Windows without the symlink privilege: git's own directory (the
		// doctor lines under test do not depend on what else is there).
		return filepath.Dir(gitPath)
	}
	return bin
}

// a13LinkedWorktree builds a repository with an initial commit and a linked
// worktree, returning the main checkout and the worktree (canonical paths).
func a13LinkedWorktree(t *testing.T) (string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	a13Git(t, repo, "init", "-q")
	a13Git(t, repo, "config", "user.email", "test@example.invalid")
	a13Git(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "tracked"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a13Git(t, repo, "add", "tracked")
	a13Git(t, repo, "commit", "-qm", "initial")
	a13Git(t, repo, "worktree", "add", "-q", "-b", "wt-branch", wt)
	return repo, wt
}

func TestA13DoctorProjectConfigAndStateDirLines(t *testing.T) {
	repo, wt := a13LinkedWorktree(t)
	state := filepath.Join(filepath.Dir(repo), "state")
	mainConf := filepath.Join(repo, ".agents", "herdr-soho.conf")
	wtConf := filepath.Join(wt, ".agents", "herdr-soho.conf")
	remove := func(paths ...string) {
		t.Helper()
		for _, p := range paths {
			_ = os.Remove(p)
		}
	}
	t.Cleanup(func() { remove(mainConf, wtConf) })

	runDoctor := func() string {
		t.Helper()
		home := t.TempDir()
		env := platform.Env{
			"HOME": home, "USERPROFILE": home,
			"PATH":                 a13GitOnlyPath(t),
			"HERDR_SOHO_DIR":       state,
			"HERDR_SOHO_SKILL_DIR": a13SkillDir(t),
		}
		ctx := core.Config{Entries: map[string]core.ConfigEntry{}}
		var out strings.Builder
		DoctorCheck(&ctx, env, wt, &out)
		return out.String()
	}

	t.Run("without any project config the line says none and the state dir is named", func(t *testing.T) {
		got := runDoctor()
		if !strings.Contains(got, "ok     project config: none\n") {
			t.Fatalf("doctor output lacks 'project config: none':\n%s", got)
		}
		if !strings.Contains(got, "state dir writable: "+state+"\n") {
			t.Fatalf("doctor output lacks the state dir line:\n%s", got)
		}
	})
	t.Run("the project config line names the main checkout's file in the linked worktree", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Dir(mainConf), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mainConf, []byte("max_workers=7\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := runDoctor()
		if !strings.Contains(got, "ok     project config: "+mainConf+"\n") {
			t.Fatalf("doctor output lacks the project config line:\n%s", got)
		}
		if !strings.Contains(got, "state dir writable: "+state+"\n") {
			t.Fatalf("doctor output lacks the state dir line:\n%s", got)
		}
		if strings.Contains(got, "project config: none") {
			t.Fatalf("doctor reported none although the main checkout holds the config:\n%s", got)
		}
	})
	t.Run("a worktree-local file wins over the main checkout's file", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Dir(wtConf), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(wtConf, []byte("max_workers=5\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := runDoctor()
		if !strings.Contains(got, "ok     project config: "+wtConf+"\n") {
			t.Fatalf("doctor output lacks the worktree project config line:\n%s", got)
		}
		if strings.Contains(got, "project config: none") {
			t.Fatalf("doctor reported none although the worktree holds the config:\n%s", got)
		}
	})
}
