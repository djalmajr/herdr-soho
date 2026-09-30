package platform

import (
	"os"
	"path/filepath"
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
	t.Run("a home directory without the skill mark is not accepted", func(t *testing.T) {
		home := t.TempDir()
		// The directory exists but carries no SKILL.md + roles mark.
		if err := os.MkdirAll(filepath.Join(home, ".agents", "skills", "herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".agents", "skills", "herdr-soho", "README.md"), []byte("no skill here\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := capturePanic(func() { SkillDir(homeSkillFixture(t, home)) })
		exit, ok := got.(*ExitError)
		if !ok || exit.Code != 2 || exit.Msg != "cannot find skill directory; set HERDR_SOHO_SKILL_DIR" {
			t.Fatalf("SkillDir panic=%#v; want ExitError code 2 with the current message", got)
		}
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
	})
}
