package platform

import (
	"path/filepath"
	"testing"
)

func TestSkillDirSohoName(t *testing.T) {
	t.Run("the soho home skill wins over the herdr-soho legacy home skill", func(t *testing.T) {
		home := t.TempDir()
		soho := filepath.Join(home, ".agents", "skills", "soho")
		legacy := filepath.Join(home, ".agents", "skills", "herdr-soho")
		writeSkillTree(t, soho)
		writeSkillTree(t, legacy)
		dir, origin := SkillDirSource(homeSkillFixture(t, home))
		if dir != soho || origin != "home" {
			t.Fatalf("SkillDirSource() = %q %q; want %q home (the soho tree wins)", dir, origin, soho)
		}
	})
	t.Run("the herdr-soho legacy home skill is still found when only it exists", func(t *testing.T) {
		home := t.TempDir()
		legacy := filepath.Join(home, ".claude", "skills", "herdr-soho")
		writeSkillTree(t, legacy)
		dir, origin := SkillDirSource(homeSkillFixture(t, home))
		if dir != legacy || origin != "home" {
			t.Fatalf("SkillDirSource() = %q %q; want %q home", dir, origin, legacy)
		}
	})
	t.Run("the soho .claude layout is found when the soho .agents layout is absent", func(t *testing.T) {
		home := t.TempDir()
		claude := filepath.Join(home, ".claude", "skills", "soho")
		legacyAgents := filepath.Join(home, ".agents", "skills", "herdr-soho")
		writeSkillTree(t, claude)
		writeSkillTree(t, legacyAgents)
		dir, origin := SkillDirSource(homeSkillFixture(t, home))
		if dir != claude || origin != "home" {
			t.Fatalf("SkillDirSource() = %q %q; want %q home (the soho .claude tree wins over the legacy .agents tree)", dir, origin, claude)
		}
	})
}
