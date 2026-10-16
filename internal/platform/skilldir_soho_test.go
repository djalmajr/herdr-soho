package platform

import (
	"path/filepath"
	"testing"
)

func TestSkillDirSohoName(t *testing.T) {
	t.Run("the soho home skill wins over a tree at the former name (negative control)", func(t *testing.T) {
		home := t.TempDir()
		soho := filepath.Join(home, ".agents", "skills", "soho")
		former := filepath.Join(home, ".agents", "skills", "herdr-soho")
		writeSkillTree(t, soho)
		writeSkillTree(t, former)
		dir, origin := SkillDirSource(homeSkillFixture(t, home))
		if dir != soho || origin != "home" {
			t.Fatalf("SkillDirSource() = %q %q; want %q home (the soho tree wins)", dir, origin, soho)
		}
	})
	// DJA-318: a tree at the former name is no longer a skill source.
	t.Run("a tree at the former name only is not found (negative)", func(t *testing.T) {
		home := t.TempDir()
		former := filepath.Join(home, ".claude", "skills", "herdr-soho")
		writeSkillTree(t, former)
		dir, origin := SkillDirSourceOrEmpty(homeSkillFixture(t, home))
		if dir == former || origin == "home" {
			t.Fatalf("SkillDirSourceOrEmpty() = %q %q; the former name must not be a home source", dir, origin)
		}
	})
	t.Run("the soho .claude layout is found when the soho .agents layout is absent", func(t *testing.T) {
		home := t.TempDir()
		claude := filepath.Join(home, ".claude", "skills", "soho")
		formerAgents := filepath.Join(home, ".agents", "skills", "herdr-soho")
		writeSkillTree(t, claude)
		writeSkillTree(t, formerAgents) // interference: the former tree must not hide the soho .claude tree
		dir, origin := SkillDirSource(homeSkillFixture(t, home))
		if dir != claude || origin != "home" {
			t.Fatalf("SkillDirSource() = %q %q; want %q home (the soho .claude tree wins)", dir, origin, claude)
		}
	})
}
