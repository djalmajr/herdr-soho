package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// homeSkillFixture points the executable probe at a bin dir without a skill
// tree, so only the home layouts (or the configured env) can provide one.
func homeSkillFixture(t *testing.T, home string) Env {
	t.Helper()
	bin := t.TempDir()
	previous := currentExecutable
	currentExecutable = func() (string, error) { return filepath.Join(bin, "herdr-soho"), nil }
	t.Cleanup(func() { currentExecutable = previous })
	return Env{"HOME": home, "USERPROFILE": home}
}

func writeSkillTree(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "roles"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# skill\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSkillDirFindsTheInstalledSkillInHome(t *testing.T) {
	t.Run("the .agents skill wins when both home layouts are valid", func(t *testing.T) {
		home := t.TempDir()
		agents := filepath.Join(home, ".agents", "skills", "herdr-soho")
		claude := filepath.Join(home, ".claude", "skills", "herdr-soho")
		writeSkillTree(t, agents)
		writeSkillTree(t, claude)
		dir, origin := SkillDirSource(homeSkillFixture(t, home))
		if dir != agents || origin != "home" {
			t.Fatalf("SkillDirSource() = %q %q; want %q home", dir, origin, agents)
		}
	})
	t.Run("the .claude skill is found when .agents is absent", func(t *testing.T) {
		home := t.TempDir()
		claude := filepath.Join(home, ".claude", "skills", "herdr-soho")
		writeSkillTree(t, claude)
		dir, origin := SkillDirSource(homeSkillFixture(t, home))
		if dir != claude || origin != "home" {
			t.Fatalf("SkillDirSource() = %q %q; want %q home", dir, origin, claude)
		}
	})
	t.Run("a home directory without the skill mark falls through to the bundled skill", func(t *testing.T) {
		home := t.TempDir()
		// The directory exists but carries no SKILL.md + roles mark.
		if err := os.MkdirAll(filepath.Join(home, ".agents", "skills", "herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".agents", "skills", "herdr-soho", "README.md"), []byte("no skill here\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		dir, origin := SkillDirSource(homeSkillFixture(t, home))
		if origin != "bundled" {
			t.Fatalf("SkillDirSource() = %q %q; want the bundled fallback for an unmarked home", dir, origin)
		}
		if !strings.HasPrefix(dir, testBundleCacheBase(home)) {
			t.Fatalf("bundled skill materialized outside the derived cache: %q", dir)
		}
		probeBundledSkill(t, dir)
	})
	t.Run("the installed home skill wins over the bundled skill without materializing", func(t *testing.T) {
		home := t.TempDir()
		agents := filepath.Join(home, ".agents", "skills", "herdr-soho")
		writeSkillTree(t, agents)
		dir, origin := SkillDirSource(homeSkillFixture(t, home))
		if dir != agents || origin != "home" {
			t.Fatalf("SkillDirSource() = %q %q; want %q home", dir, origin, agents)
		}
		if _, err := os.Stat(testBundleCacheBase(home)); !os.IsNotExist(err) {
			t.Fatalf("the installed home skill must not materialize the bundled cache: %v", err)
		}
	})
	t.Run("the configured native cache base wins over the home fallback", func(t *testing.T) {
		home := t.TempDir()
		xdg := t.TempDir()
		env := homeSkillFixture(t, home)
		env[testBundleCacheVariable()] = xdg
		dir, origin := SkillDirSource(env)
		if origin != "bundled" {
			t.Fatalf("SkillDirSource() = %q %q; want the bundled fallback", dir, origin)
		}
		want := filepath.Join(xdg, "herdr-soho", "bundled")
		if !strings.HasPrefix(dir, want+string(filepath.Separator)) {
			t.Fatalf("bundled skill materialized outside XDG_CACHE_HOME: %q", dir)
		}
		if _, err := os.Stat(testBundleCacheBase(home)); !os.IsNotExist(err) {
			t.Fatalf("HOME/.cache must stay untouched while XDG_CACHE_HOME is set: %v", err)
		}
		probeBundledSkill(t, dir)
	})
	t.Run("HERDR_SOHO_SKILL_DIR wins over both home layouts", func(t *testing.T) {
		home := t.TempDir()
		writeSkillTree(t, filepath.Join(home, ".agents", "skills", "herdr-soho"))
		writeSkillTree(t, filepath.Join(home, ".claude", "skills", "herdr-soho"))
		configured := t.TempDir()
		env := homeSkillFixture(t, home)
		env["HERDR_SOHO_SKILL_DIR"] = configured
		dir, origin := SkillDirSource(env)
		if dir != configured || origin != "HERDR_SOHO_SKILL_DIR" {
			t.Fatalf("SkillDirSource() = %q %q; want %q HERDR_SOHO_SKILL_DIR", dir, origin, configured)
		}
		if _, err := os.Stat(testBundleCacheBase(home)); !os.IsNotExist(err) {
			t.Fatalf("the configured skill dir must not materialize the bundled cache: %v", err)
		}
	})
}

// probeBundledSkill asserts the materialized bundle holds the real embedded
// resources as physical files: a regular non-empty SKILL.md, a roles
// directory and the config defaults file the shared commands read.
func probeBundledSkill(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"SKILL.md", "config.defaults"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("bundle resource %s: %v %v; want a regular non-empty physical file", name, info, err)
		}
	}
	info, err := os.Stat(filepath.Join(dir, "roles"))
	if err != nil || !info.IsDir() {
		t.Fatalf("bundle roles: %v %v; want a physical directory the role resolution reads", info, err)
	}
}

// These expectations deliberately do not call the production resolver.
func testBundleCacheBase(home string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(home, ".cache")
}
func testBundleCacheVariable() string {
	if runtime.GOOS == "windows" {
		return "LOCALAPPDATA"
	}
	return "XDG_CACHE_HOME"
}
