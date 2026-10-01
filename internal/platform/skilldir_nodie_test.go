package platform

import (
	"path/filepath"
	"testing"
)

func TestSkillDirSourceOrEmptyDoesNotDie(t *testing.T) {
	t.Run("no source holds a skill: empty result, no Die", func(t *testing.T) {
		home := t.TempDir()
		dir, origin := SkillDirSourceOrEmpty(homeSkillFixture(t, home))
		if dir != "" || origin != "" {
			t.Fatalf("SkillDirSourceOrEmpty() = %q %q; want empty", dir, origin)
		}
	})
	t.Run("the configured value wins without a Die", func(t *testing.T) {
		configured := filepath.Join(t.TempDir(), "skill")
		dir, origin := SkillDirSourceOrEmpty(Env{"HERDR_SOHO_SKILL_DIR": configured})
		if dir != configured || origin != "HERDR_SOHO_SKILL_DIR" {
			t.Fatalf("SkillDirSourceOrEmpty() = %q %q; want %q HERDR_SOHO_SKILL_DIR", dir, origin, configured)
		}
	})
	t.Run("the home skill is found without a Die", func(t *testing.T) {
		home := t.TempDir()
		agents := filepath.Join(home, ".agents", "skills", "herdr-soho")
		writeSkillTree(t, agents)
		dir, origin := SkillDirSourceOrEmpty(homeSkillFixture(t, home))
		if dir != agents || origin != "home" {
			t.Fatalf("SkillDirSourceOrEmpty() = %q %q; want %q home", dir, origin, agents)
		}
	})
	t.Run("SkillDirSource still dies with the same message", func(t *testing.T) {
		home := t.TempDir()
		got := capturePanic(func() { SkillDirSource(homeSkillFixture(t, home)) })
		exit, ok := got.(*ExitError)
		if !ok || exit.Code != 2 || exit.Msg != "cannot find skill directory; set HERDR_SOHO_SKILL_DIR" {
			t.Fatalf("SkillDirSource panic=%#v; want ExitError code 2 with the current message", got)
		}
	})
}
