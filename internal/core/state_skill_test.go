package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// herdrLogger installs a fake herdr that logs every call and exits 3: any
// herdr call made by the code under test lands in the log.
func herdrLogger(t *testing.T, dir string) (bin, log string) {
	t.Helper()
	bin = filepath.Join(dir, "bin")
	log = filepath.Join(dir, "herdr-calls.log")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "herdr"), []byte("#!/bin/sh\necho \"$@\" >> "+log+"\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func assertNoHerdrCalls(t *testing.T, log string) {
	t.Helper()
	data, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return // no herdr call at all
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("herdr was called: %q", data)
	}
}

// skillFixture builds a skill dir (SKILL.md + roles) and an empty home in a
// temp root that is not a git checkout.
func skillFixture(t *testing.T) (root, skill, home string) {
	t.Helper()
	root = t.TempDir()
	skill = filepath.Join(root, "skill")
	home = filepath.Join(root, "home")
	for _, d := range []string{skill, home} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# herdr-soho\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skill, "roles"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root, skill, home
}

func stateDirExit(t *testing.T, ctx *Config, env platform.Env, cwd string) (code int, msg string) {
	t.Helper()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		StateDir(ctx, env, cwd)
	}()
	exitErr, ok := recovered.(*platform.ExitError)
	if !ok {
		t.Fatalf("StateDir did not die (recovered=%#v)", recovered)
	}
	return exitErr.Code, exitErr.Msg
}

func TestStateDirRefusesTheSkillDir(t *testing.T) {
	ctx := &Config{Entries: map[string]ConfigEntry{}}

	t.Run("cwd = the skill: StateDir exits 2 with the phrase and creates nothing inside the skill", func(t *testing.T) {
		_, skill, home := skillFixture(t)
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home}
		code, msg := stateDirExit(t, ctx, env, skill)
		wantDir := filepath.Join(skill, ".herdr-soho")
		want := "the state dir '" + wantDir + "' would be inside the herdr-soho skill ('" + skill + "'); run herdr-soho from the project's directory (nothing was written)"
		if code != 2 || msg != want {
			t.Fatalf("exit=%d msg=%q want %q", code, msg, want)
		}
		if _, err := os.Stat(filepath.Join(skill, ".herdr-soho")); !os.IsNotExist(err) {
			t.Fatalf("state tree created inside the skill: %v", err)
		}
		entries, err := os.ReadDir(skill)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 || entries[0].Name() != "SKILL.md" || entries[1].Name() != "roles" {
			t.Fatalf("skill dir changed: %v", entries)
		}
	})
	t.Run("cwd in a subdirectory of the skill: the same refusal", func(t *testing.T) {
		_, skill, home := skillFixture(t)
		sub := filepath.Join(skill, "sub")
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home}
		code, msg := stateDirExit(t, ctx, env, sub)
		if code != 2 {
			t.Fatalf("exit=%d msg=%q", code, msg)
		}
		if want := "the state dir '" + filepath.Join(sub, ".herdr-soho") + "' would be inside the herdr-soho skill ('" + skill + "'); run herdr-soho from the project's directory (nothing was written)"; msg != want {
			t.Fatalf("msg=%q want %q", msg, want)
		}
		if _, err := os.Stat(filepath.Join(skill, ".herdr-soho")); !os.IsNotExist(err) {
			t.Fatalf("state created at the skill root: %v", err)
		}
		if _, err := os.Stat(filepath.Join(sub, ".herdr-soho")); !os.IsNotExist(err) {
			t.Fatalf("state created inside the skill subdir: %v", err)
		}
	})
	t.Run("the skill accessed through a symlink: the same refusal, named by the symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory symlinks need elevated privileges on Windows")
		}
		root, real, home := skillFixture(t)
		link := filepath.Join(root, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Skipf("cannot create the symlink: %v", err)
		}
		// The skill is known by the symlink, but the cwd is the resolved
		// target (a different spelling): only EvalSymlinks makes the state
		// root compare as inside the skill.
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": link, "HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home}
		if got := StateInSkill(ctx, env, real); got != link {
			t.Fatalf("StateInSkill()=%q want %q", got, link)
		}
		if got := StateInSkill(ctx, env, filepath.Join(real, "deep")); got != link {
			t.Fatalf("StateInSkill(sub)()=%q want %q", got, link)
		}
		code, msg := stateDirExit(t, ctx, env, real)
		if code != 2 {
			t.Fatalf("exit=%d msg=%q", code, msg)
		}
		if want := "the state dir '" + filepath.Join(real, ".herdr-soho") + "' would be inside the herdr-soho skill ('" + link + "'); run herdr-soho from the project's directory (nothing was written)"; msg != want {
			t.Fatalf("msg=%q want %q", msg, want)
		}
		// Neither the symlink path nor its target gained a state tree.
		if _, err := os.Stat(filepath.Join(link, ".herdr-soho")); !os.IsNotExist(err) {
			t.Fatalf("state created through the symlink: %v", err)
		}
		if _, err := os.Stat(filepath.Join(real, ".herdr-soho")); !os.IsNotExist(err) {
			t.Fatalf("state created at the symlink target: %v", err)
		}
	})
	t.Run("the skill as a git checkout: the refusal leaves the .gitignore untouched and calls no herdr", func(t *testing.T) {
		root, skill, home := skillFixture(t)
		if resolved, err := filepath.EvalSymlinks(skill); err == nil {
			skill = resolved // git reports the resolved spelling (macOS /tmp)
		}
		if out, gitErr := exec.Command("git", "init", "-q", skill).CombinedOutput(); gitErr != nil {
			t.Fatalf("git init: %s %v", out, gitErr)
		}
		if err := os.WriteFile(filepath.Join(skill, ".gitignore"), []byte("# pre-existing\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		bin, log := herdrLogger(t, root)
		env := platform.Env{
			"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home,
			"PATH": bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		}
		code, msg := stateDirExit(t, ctx, env, skill)
		want := "the state dir '" + filepath.Join(skill, ".herdr-soho") + "' would be inside the herdr-soho skill ('" + skill + "'); run herdr-soho from the project's directory (nothing was written)"
		if code != 2 || msg != want {
			t.Fatalf("exit=%d msg=%q want %q", code, msg, want)
		}
		got, err := os.ReadFile(filepath.Join(skill, ".gitignore"))
		if err != nil || string(got) != "# pre-existing\n" {
			t.Fatalf(".gitignore changed by the refusal: %q err=%v", got, err)
		}
		assertNoHerdrCalls(t, log)
	})
	t.Run("no HERDR_WORKSPACE_ID: the refusal exits 2 without calling pane current", func(t *testing.T) {
		root, skill, home := skillFixture(t)
		bin, log := herdrLogger(t, root)
		env := platform.Env{
			"HERDR_SOHO_SKILL_DIR": skill, "HOME": home, "USERPROFILE": home,
			"PATH": bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		}
		code, msg := stateDirExit(t, ctx, env, skill)
		want := "the state dir '" + filepath.Join(skill, ".herdr-soho") + "' would be inside the herdr-soho skill ('" + skill + "'); run herdr-soho from the project's directory (nothing was written)"
		if code != 2 || msg != want {
			t.Fatalf("exit=%d msg=%q want %q", code, msg, want)
		}
		assertNoHerdrCalls(t, log)
		if _, err := os.Stat(filepath.Join(skill, ".herdr-soho")); !os.IsNotExist(err) {
			t.Fatalf("state created inside the skill: %v", err)
		}
	})
	t.Run("cwd outside the skill: nothing changes", func(t *testing.T) {
		root, skill, home := skillFixture(t)
		proj := filepath.Join(root, "project")
		if err := os.MkdirAll(proj, 0o700); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HERDR_SOHO_DIR": filepath.Join(root, "state"), "HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home}
		if got := StateInSkill(ctx, env, proj); got != "" {
			t.Fatalf("StateInSkill()=%q want empty", got)
		}
		dir := StateDir(ctx, env, proj)
		if dir != filepath.Join(root, "state", "ws") {
			t.Fatalf("dir=%q", dir)
		}
		for _, child := range []string{"briefs", "reports", "wait"} {
			if _, err := os.Stat(filepath.Join(dir, child)); err != nil {
				t.Fatalf("%s missing: %v", child, err)
			}
		}
		if _, err := os.Stat(filepath.Join(skill, ".herdr-soho")); !os.IsNotExist(err) {
			t.Fatalf("state created inside the skill: %v", err)
		}
	})
	t.Run("an absolute HERDR_SOHO_DIR outside the skill wins from a cwd inside it", func(t *testing.T) {
		root, skill, home := skillFixture(t)
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HERDR_SOHO_DIR": filepath.Join(root, "state"), "HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home}
		if got := StateInSkill(ctx, env, skill); got != "" {
			t.Fatalf("StateInSkill()=%q want empty", got)
		}
		dir := StateDir(ctx, env, skill)
		if dir != filepath.Join(root, "state", "ws") {
			t.Fatalf("dir=%q", dir)
		}
		if _, err := os.Stat(filepath.Join(dir, "wait")); err != nil {
			t.Fatalf("state not created outside the skill: %v", err)
		}
		if _, err := os.Stat(filepath.Join(skill, ".herdr-soho")); !os.IsNotExist(err) {
			t.Fatalf("state also created inside the skill: %v", err)
		}
	})
	t.Run("no skill found: nothing changes and the lookup does not die", func(t *testing.T) {
		root, _, home := skillFixture(t)
		plain := filepath.Join(root, "plain")
		if err := os.MkdirAll(plain, 0o700); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home}
		if got := StateInSkill(ctx, env, plain); got != "" {
			t.Fatalf("StateInSkill()=%q want empty (no skill, no Die)", got)
		}
		dir := StateDir(ctx, env, plain)
		if dir != filepath.Join(plain, ".herdr-soho", "ws") {
			t.Fatalf("dir=%q", dir)
		}
		if _, err := os.Stat(filepath.Join(dir, "wait")); err != nil {
			t.Fatalf("state not created: %v", err)
		}
	})
	t.Run("nowrite mode keeps the read path untouched inside the skill", func(t *testing.T) {
		_, skill, home := skillFixture(t)
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HERDR_SOHO_NOWRITE": "1", "HOME": home, "USERPROFILE": home}
		dir := StateDir(ctx, env, skill)
		if dir != filepath.Join(skill, ".herdr-soho", "ws") {
			t.Fatalf("dir=%q", dir)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("nowrite created state: %v", err)
		}
	})
}

// TestStateRootAndStateDirPathKeepTheSkillGitignoreUntouched is the guard for
// the read-only side of the skill refusal: StateRoot/StateDirPath do not
// refuse (they only resolve the path), but they must not append the state dir
// to the .gitignore of a skill that is a git checkout.
func TestStateRootAndStateDirPathKeepTheSkillGitignoreUntouched(t *testing.T) {
	ctx := &Config{Entries: map[string]ConfigEntry{}}

	gitSkill := func(t *testing.T) (root, skill, home string) {
		t.Helper()
		root, skill, home = skillFixture(t)
		if resolved, err := filepath.EvalSymlinks(skill); err == nil {
			skill = resolved // git reports the resolved spelling (macOS /tmp)
		}
		if out, gitErr := exec.Command("git", "init", "-q", skill).CombinedOutput(); gitErr != nil {
			t.Fatalf("git init: %s %v", out, gitErr)
		}
		return root, skill, home
	}

	t.Run("StateRoot from a cwd inside a skill that is a git checkout: no .gitignore is created", func(t *testing.T) {
		_, skill, home := gitSkill(t)
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home, "PATH": os.Getenv("PATH")}
		dir := StateRoot(ctx, env, skill)
		if dir != filepath.Join(skill, ".herdr-soho") {
			t.Fatalf("dir=%q want %q", dir, filepath.Join(skill, ".herdr-soho"))
		}
		if _, err := os.Stat(filepath.Join(skill, ".gitignore")); !os.IsNotExist(err) {
			t.Fatalf("StateRoot created the skill .gitignore: %v", err)
		}
	})
	t.Run("StateRoot with a pre-existing skill .gitignore: the bytes are untouched", func(t *testing.T) {
		_, skill, home := gitSkill(t)
		if err := os.WriteFile(filepath.Join(skill, ".gitignore"), []byte("# pre-existing\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home, "PATH": os.Getenv("PATH")}
		StateRoot(ctx, env, skill)
		got, err := os.ReadFile(filepath.Join(skill, ".gitignore"))
		if err != nil || string(got) != "# pre-existing\n" {
			t.Fatalf(".gitignore changed by StateRoot: %q err=%v", got, err)
		}
	})
	t.Run("StateDirPath from a cwd inside the skill: no .gitignore either", func(t *testing.T) {
		_, skill, home := gitSkill(t)
		if err := os.WriteFile(filepath.Join(skill, ".gitignore"), []byte("# pre-existing\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home, "PATH": os.Getenv("PATH")}
		dir := StateDirPath(ctx, env, skill)
		if dir != filepath.Join(skill, ".herdr-soho", "ws") {
			t.Fatalf("dir=%q want %q", dir, filepath.Join(skill, ".herdr-soho", "ws"))
		}
		got, err := os.ReadFile(filepath.Join(skill, ".gitignore"))
		if err != nil || string(got) != "# pre-existing\n" {
			t.Fatalf(".gitignore changed by StateDirPath: %q err=%v", got, err)
		}
	})
	t.Run("a cwd outside the skill in a git repo: the .gitignore entry is still appended", func(t *testing.T) {
		root, skill, home := skillFixture(t)
		proj := filepath.Join(root, "project")
		if err := os.MkdirAll(proj, 0o700); err != nil {
			t.Fatal(err)
		}
		if resolved, err := filepath.EvalSymlinks(proj); err == nil {
			proj = resolved
		}
		if out, gitErr := exec.Command("git", "init", "-q", proj).CombinedOutput(); gitErr != nil {
			t.Fatalf("git init: %s %v", out, gitErr)
		}
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HOME": home, "USERPROFILE": home, "PATH": os.Getenv("PATH")}
		dir := StateRoot(ctx, env, proj)
		if dir != filepath.Join(proj, ".herdr-soho") {
			t.Fatalf("dir=%q want %q", dir, filepath.Join(proj, ".herdr-soho"))
		}
		got, err := os.ReadFile(filepath.Join(proj, ".gitignore"))
		if err != nil || string(got) != ".herdr-soho/\n" {
			t.Fatalf(".gitignore not appended as today: %q err=%v", got, err)
		}
	})
}
