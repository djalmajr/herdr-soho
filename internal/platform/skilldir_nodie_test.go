package platform

import (
	"os"
	"path/filepath"
	"strings"
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
	t.Run("the bundled lookup finds a completed bundle without creating the cache", func(t *testing.T) {
		home := t.TempDir()
		env := homeSkillFixture(t, home)
		cache := testBundleCacheBase(home)
		// Before anything materializes: the read-only lookup finds nothing
		// and must not have created the cache.
		if dir, origin := SkillDirSourceOrEmpty(env); dir != "" || origin != "" {
			t.Fatalf("SkillDirSourceOrEmpty() = %q %q; want empty before materialization", dir, origin)
		}
		if _, err := os.Stat(cache); !os.IsNotExist(err) {
			t.Fatalf("the read-only lookup created the cache: %v", err)
		}
		// SkillDirSource materializes the embedded bundle into the derived cache.
		dir, origin := SkillDirSource(env)
		if origin != "bundled" {
			t.Fatalf("SkillDirSource() = %q %q; want %q bundled", dir, origin, "bundled")
		}
		if !strings.HasPrefix(dir, cache) {
			t.Fatalf("the bundled skill escaped the derived cache: %q", dir)
		}
		probeBundledSkill(t, dir)
		// The read-only lookup now finds the completed bundle.
		again, originAgain := SkillDirSourceOrEmpty(env)
		if again != dir || originAgain != "bundled" {
			t.Fatalf("SkillDirSourceOrEmpty() after materialization = %q %q; want %q bundled", again, originAgain, dir)
		}
		// Reuse: a second materialization returns the same verified bundle and
		// the cache holds exactly one bundle.
		reuse, _ := SkillDirSource(env)
		if reuse != dir {
			t.Fatalf("second materialization = %q; want the same bundle %q", reuse, dir)
		}
		entries, err := os.ReadDir(filepath.Dir(dir))
		if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(dir) {
			t.Fatalf("cache holds %v; want exactly the one verified bundle (%v)", entries, err)
		}
	})
	t.Run("a relative configured cache is refused without any fallback write", func(t *testing.T) {
		home := t.TempDir()
		cwd := t.TempDir()
		t.Chdir(cwd)
		env := homeSkillFixture(t, home)
		env[testBundleCacheVariable()] = "relative-cache"
		if dir, origin := SkillDirSourceOrEmpty(env); dir != "" || origin != "" {
			t.Fatalf("readonly lookup = %q %q", dir, origin)
		}
		value := capturePanic(func() { SkillDirSource(env) })
		if exit, ok := value.(*ExitError); !ok || exit.Code != 2 {
			t.Fatalf("invalid cache panic = %#v", value)
		}
		for _, base := range []string{home, cwd} {
			entries, err := os.ReadDir(base)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid cache mutated %s: %v %v", base, entries, err)
			}
		}
	})
	t.Run("native and legacy NOWRITE only reuse an already published bundle", func(t *testing.T) {
		for _, key := range []string{"HERDR_SOHO_NOWRITE", "HERDR_AGENTS_NOWRITE"} {
			t.Run(key, func(t *testing.T) {
				home := t.TempDir()
				env := homeSkillFixture(t, home)
				env[key] = "1"
				value := capturePanic(func() { SkillDirSource(env) })
				if exit, ok := value.(*ExitError); !ok || exit.Code != 2 {
					t.Fatalf("readonly missing cache panic = %#v", value)
				}
				entries, err := os.ReadDir(home)
				if err != nil || len(entries) != 0 {
					t.Fatalf("readonly created cache: %v %v", entries, err)
				}
				delete(env, key)
				dir, source := SkillDirSource(env)
				env[key] = "1"
				again, origin := SkillDirSource(env)
				if source != "bundled" || again != dir || origin != source {
					t.Fatalf("readonly reuse = %q %q; want %q %q", again, origin, dir, source)
				}
			})
		}
	})
	t.Run("a corrupt cache is refused without overwrite and dies with the old message", func(t *testing.T) {
		home := t.TempDir()
		env := homeSkillFixture(t, home)
		dir, origin := SkillDirSource(env)
		if origin != "bundled" {
			t.Fatalf("SkillDirSource() = %q %q; want bundled", dir, origin)
		}
		file := filepath.Join(dir, "config.defaults")
		if err := os.WriteFile(file, []byte("foreign content"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := capturePanic(func() { SkillDirSource(env) })
		exit, ok := got.(*ExitError)
		if !ok || exit.Code != 2 || exit.Msg != "cannot find skill directory; set HERDR_SOHO_SKILL_DIR" {
			t.Fatalf("SkillDirSource on a corrupt cache panicked %#v; want ExitError code 2 with the old message", got)
		}
		data, err := os.ReadFile(file)
		if err != nil || string(data) != "foreign content" {
			t.Fatalf("corrupt cache overwritten: %q %v", data, err)
		}
	})
	t.Run("no usable env at all retains the old missing-skill error", func(t *testing.T) {
		previous := currentExecutable
		currentExecutable = func() (string, error) { return filepath.Join(t.TempDir(), "herdr-soho"), nil }
		t.Cleanup(func() { currentExecutable = previous })
		got := capturePanic(func() { SkillDirSource(Env{}) })
		exit, ok := got.(*ExitError)
		if !ok || exit.Code != 2 || exit.Msg != "cannot find skill directory; set HERDR_SOHO_SKILL_DIR" {
			t.Fatalf("SkillDirSource without any usable env panicked %#v; want the old missing-skill error", got)
		}
	})
	t.Run("SkillDirSource still dies with the same message when no source holds a skill", func(t *testing.T) {
		// No home skill mark and no usable cache env (no HOME/USERPROFILE):
		// the pre-bundle contract survives for envs that cannot derive a cache.
		previous := currentExecutable
		currentExecutable = func() (string, error) { return filepath.Join(t.TempDir(), "herdr-soho"), nil }
		t.Cleanup(func() { currentExecutable = previous })
		got := capturePanic(func() { SkillDirSource(Env{"HOME": "", "USERPROFILE": ""}) })
		exit, ok := got.(*ExitError)
		if !ok || exit.Code != 2 || exit.Msg != "cannot find skill directory; set HERDR_SOHO_SKILL_DIR" {
			t.Fatalf("SkillDirSource panic=%#v; want ExitError code 2 with the current message", got)
		}
	})
}
