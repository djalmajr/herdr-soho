package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

// formerNamesEnv keeps the negative-test env hermetic: the home and config
// locations are fixture-owned, the skill is the repository's, and the
// restricted PATH resolves only the system git.
func formerNamesEnv(t *testing.T) (platform.Env, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "conf")
	cwd := filepath.Join(root, "project")
	for _, dir := range []string{home, conf, cwd, filepath.Join(root, "bin")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return platform.Env{
		"HOME": home, "USERPROFILE": home, "XDG_CONFIG_HOME": conf,
		"SOHO_SKILL_DIR": testSkillDir(t), "PATH": gitOnlyDir(t),
	}, cwd
}

func emptyCtx() *Config {
	return &Config{Entries: make(map[string]ConfigEntry), Order: make([]string, 0), Sources: make([]string, 0)}
}

// TestFormerNamesFormerEnvNamesHaveNoEffect: the HERDR_SOHO_ environment names
// are no longer read: each one, defined alone, must have no effect on
// the setting it used to set.
func TestFormerNamesFormerEnvNamesHaveNoEffect(t *testing.T) {
	env, cwd := formerNamesEnv(t)

	// HERDR_SOHO_STATE_DIR alone: the state directory stays the default.
	state := platform.Env{"HERDR_SOHO_STATE_DIR": filepath.Join(env.Get("HOME"), "legacy-state")}
	state["SOHO_SKILL_DIR"] = env.Get("SOHO_SKILL_DIR")
	state["PATH"] = env.Get("PATH")
	if got := StateDirSetting(emptyCtx(), state, cwd); got != ".soho" {
		t.Fatalf("HERDR_SOHO_STATE_DIR alone: StateDirSetting = %q, want the default .soho", got)
	}

	// HERDR_SOHO_DIR alone: the state root stays the default <root>/.soho.
	dir := platform.Env{"HERDR_SOHO_DIR": filepath.Join(env.Get("HOME"), "legacy-root")}
	dir["SOHO_SKILL_DIR"] = env.Get("SOHO_SKILL_DIR")
	dir["PATH"] = env.Get("PATH")
	if got := StateRootPath(emptyCtx(), dir, cwd); got != filepath.Join(cwd, ".soho") {
		t.Fatalf("HERDR_SOHO_DIR alone: StateRootPath = %q, want %q", got, filepath.Join(cwd, ".soho"))
	}

	// HERDR_SOHO_ROLES alone: the override directory is not added.
	roles := t.TempDir()
	roleEnv := platform.Env{"HERDR_SOHO_ROLES": roles, "SOHO_SKILL_DIR": env.Get("SOHO_SKILL_DIR"), "PATH": env.Get("PATH")}
	for _, d := range RoleDirs(roleEnv, cwd) {
		if d == roles {
			t.Fatalf("HERDR_SOHO_ROLES alone is still a roles override: dirs %v", RoleDirs(roleEnv, cwd))
		}
	}

	// HERDR_SOHO_NOWRITE=1 alone: the read-only gate is off.
	off := platform.Env{"HERDR_SOHO_NOWRITE": "1"}
	if Nowrite(off) {
		t.Fatal("HERDR_SOHO_NOWRITE=1 alone is still the read-only gate")
	}
}

// TestFormerNamesFormerUserConfigNotRead: with only the former user config
// present, the user layer brings none of its keys.
func TestFormerNamesFormerUserConfigNotRead(t *testing.T) {
	env, cwd := formerNamesEnv(t)
	legacy := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho", "config")
	write(t, legacy, "max_workers=7\n")
	ctx := LoadConfig(env, cwd)
	if got := Cfg(&ctx, "max_workers", "3", env); got != "3" {
		t.Fatalf("max_workers = %q, want the default 3 (the former user config is not read)", got)
	}
	if got := CfgSource(&ctx, "max_workers", env); got == "user" {
		t.Fatalf("max_workers source = %q, want not the user layer", got)
	}
	if file := ProjectConfigFileUsed(env, cwd); file != "" {
		t.Fatalf("project config = %q, want none", file)
	}
}

// TestFormerNamesFormerProjectConfigNotRead: with only the former project
// config present, the project layer brings none of its keys.
func TestFormerNamesFormerProjectConfigNotRead(t *testing.T) {
	env, cwd := formerNamesEnv(t)
	write(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"), "max_workers=7\n")
	ctx := LoadConfig(env, cwd)
	if got := Cfg(&ctx, "max_workers", "3", env); got != "3" {
		t.Fatalf("max_workers = %q, want the default 3 (the former project config is not read)", got)
	}
	if got := CfgSource(&ctx, "max_workers", env); got == "project" {
		t.Fatalf("max_workers source = %q, want not the project layer", got)
	}
	if file := ProjectConfigFileUsed(env, cwd); file != "" {
		t.Fatalf("ProjectConfigFileUsed = %q, want none", file)
	}
	if file := ConfigFileFor("project", env, cwd); file != filepath.Join(cwd, ".agents", "soho.conf") {
		t.Fatalf("ConfigFileFor = %q, want the soho project path", file)
	}
}

// TestFormerNamesFormerStateDirNotUsed: with only the former state directory
// present, the state directory is <root>/.soho.
func TestFormerNamesFormerStateDirNotUsed(t *testing.T) {
	env, cwd := formerNamesEnv(t)
	if err := os.MkdirAll(filepath.Join(cwd, ".herdr-soho"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := StateDirSetting(emptyCtx(), env, cwd); got != ".soho" {
		t.Fatalf("StateDirSetting = %q, want .soho", got)
	}
	if got := StateRootPath(emptyCtx(), env, cwd); got != filepath.Join(cwd, ".soho") {
		t.Fatalf("StateRootPath = %q, want %q", got, filepath.Join(cwd, ".soho"))
	}
}
