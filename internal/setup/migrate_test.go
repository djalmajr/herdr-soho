package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// migrateEnv keeps the migration tests hermetic: home and the config
// locations are fixture-owned (HOME, XDG_CONFIG_HOME and APPDATA all
// point into t.TempDir()), the PATH is empty so the git root calls that
// ProjectRoot and StateProjectRoot make resolve to the plain cwd, and
// nothing legacy exists yet. SOHO_SKILL_DIR points at the repository's
// skill so the LoadConfig calls never materialize the bundled skill into
// the fixture home.
func migrateEnv(t *testing.T) (platform.Env, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(root, "project")
	for _, dir := range []string{home, cwd} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return platform.Env{
		"HOME": home, "USERPROFILE": home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"APPDATA":         filepath.Join(home, "AppData", "Roaming"),
		"PATH":            "",
		"SOHO_SKILL_DIR":  filepath.Join("..", "..", "skills", "soho"),
	}, cwd
}

// formerUserDirs returns the former herdr-soho user config dir and the
// soho user config dir the migration moves it to, for this env.
func formerUserDirs(t *testing.T, env platform.Env) (old, novo string) {
	t.Helper()
	sohoConf := platform.UserConfigPath(platform.Current(), env)
	novo = filepath.Dir(sohoConf)
	old = filepath.Join(filepath.Dir(novo), "herdr-soho")
	return old, novo
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s should not exist (err=%v)", path, err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("%s should exist: %v", path, err)
	}
}

// treeListing fingerprints every file (path + sha256) and directory under
// root so a dry run must leave the whole tree byte-identical.
func treeListing(t *testing.T, root string) string {
	t.Helper()
	entries := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if info.Mode().IsRegular() {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			sum := sha256.Sum256(data)
			entries = append(entries, rel+" "+hex.EncodeToString(sum[:]))
		} else {
			entries = append(entries, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}

// TestMigrateNamesUserConfig: only the herdr-soho user config dir exists
// (a config file plus teams/default.conf): both entries go to the soho
// dir, the old dir is removed, and the reloaded config brings its keys.
func TestMigrateNamesUserConfig(t *testing.T) {
	t.Run("moves both entries, removes the empty old dir, reloads the config", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old, novo := formerUserDirs(t, env)
		mustWriteFile(t, filepath.Join(old, "config"), "max_workers=7\n")
		mustWriteFile(t, filepath.Join(old, "teams", "default.conf"), "lanes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: moved " + filepath.Join(old, "config") + " to " + filepath.Join(novo, "config"),
			"migrate: moved " + filepath.Join(old, "teams") + " to " + filepath.Join(novo, "teams"),
			"migrate: removed the empty " + old,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustNotExist(t, old)
		if got := mustReadFile(t, filepath.Join(novo, "config")); got != "max_workers=7\n" {
			t.Fatalf("soho config = %q", got)
		}
		if got := mustReadFile(t, filepath.Join(novo, "teams", "default.conf")); got != "lanes=2\n" {
			t.Fatalf("soho teams/default.conf = %q", got)
		}
		if got := core.Cfg(&ctx, "max_workers", "3", env); got != "7" {
			t.Fatalf("reloaded max_workers = %q, want 7", got)
		}
		if got := core.CfgSource(&ctx, "max_workers", env); got != "user" {
			t.Fatalf("reloaded max_workers source = %q, want user", got)
		}
	})

	t.Run("moves both former user dirs, in order", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		soho := platform.UserConfigPath(platform.Current(), env)
		novo := filepath.Dir(soho)
		oldSo := filepath.Join(filepath.Dir(novo), "herdr-soho")
		oldAg := filepath.Join(filepath.Dir(novo), "herdr-agents")
		mustWriteFile(t, filepath.Join(oldSo, "config"), "a=1\n")
		mustWriteFile(t, filepath.Join(oldAg, "teams", "default.conf"), "b=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: moved " + filepath.Join(oldSo, "config") + " to " + filepath.Join(novo, "config"),
			"migrate: removed the empty " + oldSo,
			"migrate: moved " + filepath.Join(oldAg, "teams") + " to " + filepath.Join(novo, "teams"),
			"migrate: removed the empty " + oldAg,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustNotExist(t, oldSo)
		mustNotExist(t, oldAg)
		mustExist(t, filepath.Join(novo, "config"))
		mustExist(t, filepath.Join(novo, "teams", "default.conf"))
	})

	t.Run("conflict: the old entry is kept and neither file changes", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old, novo := formerUserDirs(t, env)
		mustWriteFile(t, filepath.Join(old, "config"), "old=1\n")
		mustWriteFile(t, filepath.Join(novo, "config"), "new=1\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: kept " + filepath.Join(old, "config") + ": " + filepath.Join(novo, "config") + " already exists; merge by hand",
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if got := mustReadFile(t, filepath.Join(old, "config")); got != "old=1\n" {
			t.Fatalf("old config changed to %q", got)
		}
		if got := mustReadFile(t, filepath.Join(novo, "config")); got != "new=1\n" {
			t.Fatalf("soho config changed to %q", got)
		}
		mustExist(t, old)
	})
}

// TestMigrateNamesProjectConfig: the former project configs move to
// .agents/soho.conf in order; a conflict is kept with its line.
func TestMigrateNamesProjectConfig(t *testing.T) {
	t.Run("only herdr-agents.conf: it becomes soho.conf", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old := filepath.Join(cwd, ".agents", "herdr-agents.conf")
		mustWriteFile(t, old, "panes=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: moved " + old + " to " + filepath.Join(cwd, ".agents", "soho.conf"),
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustNotExist(t, old)
		if got := mustReadFile(t, filepath.Join(cwd, ".agents", "soho.conf")); got != "panes=2\n" {
			t.Fatalf("soho.conf = %q", got)
		}
	})

	t.Run("both former project configs: the first moves, the second is kept", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		oldSo := filepath.Join(cwd, ".agents", "herdr-soho.conf")
		oldAg := filepath.Join(cwd, ".agents", "herdr-agents.conf")
		mustWriteFile(t, oldSo, "a=1\n")
		mustWriteFile(t, oldAg, "b=2\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		sohoConf := filepath.Join(cwd, ".agents", "soho.conf")
		want := []string{
			"migrate: moved " + oldSo + " to " + sohoConf,
			"migrate: kept " + oldAg + ": " + sohoConf + " already exists; merge by hand",
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		if got := mustReadFile(t, sohoConf); got != "a=1\n" {
			t.Fatalf("soho.conf = %q, want the first former file", got)
		}
		mustExist(t, oldAg)
	})
}

// TestMigrateNamesState: the former state dir moves to the soho one only
// when the state root is the default, the soho dir is absent and no
// worker is still listed.
func TestMigrateNamesState(t *testing.T) {
	t.Run("empty roster (header only): the dir becomes .soho", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		roster := filepath.Join(cwd, ".herdr-soho", "ws1", "agents.tsv")
		mustWriteFile(t, roster, "# roster\n\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: moved " + filepath.Join(cwd, ".herdr-soho") + " to " + filepath.Join(cwd, ".soho"),
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustNotExist(t, filepath.Join(cwd, ".herdr-soho"))
		if got := mustReadFile(t, filepath.Join(cwd, ".soho", "ws1", "agents.tsv")); got != "# roster\n\n" {
			t.Fatalf("moved roster = %q", got)
		}
	})

	t.Run("a worker in the roster: kept with the release instruction", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old := filepath.Join(cwd, ".herdr-soho")
		mustWriteFile(t, filepath.Join(old, "ws1", "agents.tsv"), "# roster\nbuild\tidle\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: kept " + old + ": workers are still in its roster; release them with 'soho release <name> --close' and run 'soho setup' again",
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustExist(t, old)
		mustNotExist(t, filepath.Join(cwd, ".soho"))
	})

	t.Run("the state dir is pinned in the config: kept", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old := filepath.Join(cwd, ".herdr-soho")
		mustWriteFile(t, filepath.Join(old, "ws1", "agents.tsv"), "# roster\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "soho.conf"), "state_dir=custom-state\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: kept " + old + ": the state dir is set to " + filepath.Join(cwd, "custom-state"),
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustExist(t, old)
		mustNotExist(t, filepath.Join(cwd, ".soho"))
	})

	t.Run("the soho state dir already exists: kept", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old := filepath.Join(cwd, ".herdr-soho")
		mustWriteFile(t, filepath.Join(old, "ws1", "agents.tsv"), "# roster\n")
		mustWriteFile(t, filepath.Join(cwd, ".soho", "ws2", "agents.tsv"), "# roster\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: kept " + old + ": " + filepath.Join(cwd, ".soho") + " already exists; remove " + old + " once its reports are no longer needed",
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustExist(t, old)
	})

	t.Run("an empty .soho counts as absent: the dir moves over it", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old := filepath.Join(cwd, ".herdr-soho")
		novo := filepath.Join(cwd, ".soho")
		mustWriteFile(t, filepath.Join(old, "ws1", "agents.tsv"), "# roster\n\n")
		if err := os.MkdirAll(novo, 0o700); err != nil {
			t.Fatal(err)
		}
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: moved " + old + " to " + novo,
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustNotExist(t, old)
		if got := mustReadFile(t, filepath.Join(novo, "ws1", "agents.tsv")); got != "# roster\n\n" {
			t.Fatalf("moved roster = %q", got)
		}
	})

	t.Run("a .soho with a file still blocks: kept line, nothing changes", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old := filepath.Join(cwd, ".herdr-soho")
		novo := filepath.Join(cwd, ".soho")
		mustWriteFile(t, filepath.Join(old, "ws1", "agents.tsv"), "# roster\n\n")
		mustWriteFile(t, filepath.Join(novo, "note.txt"), "keep\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: kept " + old + ": " + novo + " already exists; remove " + old + " once its reports are no longer needed",
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustExist(t, old)
		if got := mustReadFile(t, filepath.Join(novo, "note.txt")); got != "keep\n" {
			t.Fatalf(".soho note changed to %q", got)
		}
	})

	t.Run("a symlink .soho to an empty dir still blocks", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old := filepath.Join(cwd, ".herdr-soho")
		novo := filepath.Join(cwd, ".soho")
		target := filepath.Join(cwd, "soho-target")
		mustWriteFile(t, filepath.Join(old, "ws1", "agents.tsv"), "# roster\n\n")
		if err := os.MkdirAll(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, novo); err != nil {
			t.Skip("cannot create a symlink on this platform")
		}
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		want := []string{
			"migrate: kept " + old + ": " + novo + " already exists; remove " + old + " once its reports are no longer needed",
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
		mustExist(t, old)
		mustExist(t, novo)
	})
}

// TestMigrateNamesDryRun: with dry=true the same fixtures report the
// would lines, the config is not reloaded, and the tree is left
// byte-identical (the listing before and after the call is equal).
func TestMigrateNamesDryRun(t *testing.T) {
	type dryFixture struct {
		env   platform.Env
		cwd   string
		old   string
		novo  string
		lines []string
	}
	// runDry stages the fixture, runs the dry migration and checks the
	// tree listing is unchanged and the config was not reloaded.
	runDry := func(t *testing.T, stage func(t *testing.T, env platform.Env, cwd string, old, novo string)) dryFixture {
		t.Helper()
		env, cwd := migrateEnv(t)
		old, novo := formerUserDirs(t, env)
		stage(t, env, cwd, old, novo)
		before := treeListing(t, filepath.Dir(env.Get("HOME")))
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, true)
		after := treeListing(t, filepath.Dir(env.Get("HOME")))
		if before != after {
			t.Fatalf("dry run changed the tree:\nbefore:\n%s\nafter:\n%s", before, after)
		}
		if got := core.Cfg(&ctx, "max_workers", "3", env); got != "3" {
			t.Fatalf("dry run reloaded the config: max_workers = %q, want the default 3", got)
		}
		return dryFixture{env: env, cwd: cwd, old: old, novo: novo, lines: lines}
	}

	t.Run("user config: would move both entries and would remove the dir", func(t *testing.T) {
		fx := runDry(t, func(t *testing.T, env platform.Env, cwd string, old, novo string) {
			mustWriteFile(t, filepath.Join(old, "config"), "max_workers=7\n")
			mustWriteFile(t, filepath.Join(old, "teams", "default.conf"), "lanes=2\n")
		})
		want := []string{
			"migrate: would move " + filepath.Join(fx.old, "config") + " to " + filepath.Join(fx.novo, "config"),
			"migrate: would move " + filepath.Join(fx.old, "teams") + " to " + filepath.Join(fx.novo, "teams"),
			"migrate: would remove the empty " + fx.old,
		}
		if !reflect.DeepEqual(fx.lines, want) {
			t.Fatalf("lines = %v, want %v", fx.lines, want)
		}
		mustExist(t, filepath.Join(fx.old, "config"))
		mustNotExist(t, filepath.Join(fx.novo, "config"))
	})

	t.Run("conflict: the kept line, nothing touched", func(t *testing.T) {
		fx := runDry(t, func(t *testing.T, env platform.Env, cwd string, old, novo string) {
			mustWriteFile(t, filepath.Join(old, "config"), "old=1\n")
			mustWriteFile(t, filepath.Join(novo, "config"), "new=1\n")
		})
		want := []string{
			"migrate: kept " + filepath.Join(fx.old, "config") + ": " + filepath.Join(fx.novo, "config") + " already exists; merge by hand",
		}
		if !reflect.DeepEqual(fx.lines, want) {
			t.Fatalf("lines = %v, want %v", fx.lines, want)
		}
		if got := mustReadFile(t, filepath.Join(fx.old, "config")); got != "old=1\n" {
			t.Fatalf("old config changed to %q", got)
		}
	})

	t.Run("project config: would move herdr-agents.conf", func(t *testing.T) {
		fx := runDry(t, func(t *testing.T, env platform.Env, cwd string, old, novo string) {
			mustWriteFile(t, filepath.Join(cwd, ".agents", "herdr-agents.conf"), "panes=2\n")
		})
		want := []string{
			"migrate: would move " + filepath.Join(fx.cwd, ".agents", "herdr-agents.conf") + " to " + filepath.Join(fx.cwd, ".agents", "soho.conf"),
		}
		if !reflect.DeepEqual(fx.lines, want) {
			t.Fatalf("lines = %v, want %v", fx.lines, want)
		}
		mustExist(t, filepath.Join(fx.cwd, ".agents", "herdr-agents.conf"))
		mustNotExist(t, filepath.Join(fx.cwd, ".agents", "soho.conf"))
	})

	t.Run("state with an empty roster: would move the dir", func(t *testing.T) {
		fx := runDry(t, func(t *testing.T, env platform.Env, cwd string, old, novo string) {
			mustWriteFile(t, filepath.Join(cwd, ".herdr-soho", "ws1", "agents.tsv"), "# roster\n\n")
		})
		want := []string{
			"migrate: would move " + filepath.Join(fx.cwd, ".herdr-soho") + " to " + filepath.Join(fx.cwd, ".soho"),
		}
		if !reflect.DeepEqual(fx.lines, want) {
			t.Fatalf("lines = %v, want %v", fx.lines, want)
		}
		mustExist(t, filepath.Join(fx.cwd, ".herdr-soho"))
		mustNotExist(t, filepath.Join(fx.cwd, ".soho"))
	})

	t.Run("state with a worker in the roster: kept line, nothing touched", func(t *testing.T) {
		fx := runDry(t, func(t *testing.T, env platform.Env, cwd string, old, novo string) {
			mustWriteFile(t, filepath.Join(cwd, ".herdr-soho", "ws1", "agents.tsv"), "# roster\nbuild\tidle\n")
		})
		want := []string{
			"migrate: kept " + filepath.Join(fx.cwd, ".herdr-soho") + ": workers are still in its roster; release them with 'soho release <name> --close' and run 'soho setup' again",
		}
		if !reflect.DeepEqual(fx.lines, want) {
			t.Fatalf("lines = %v, want %v", fx.lines, want)
		}
		mustExist(t, filepath.Join(fx.cwd, ".herdr-soho"))
	})

	t.Run("state pinned in the config: kept line, nothing touched", func(t *testing.T) {
		fx := runDry(t, func(t *testing.T, env platform.Env, cwd string, old, novo string) {
			mustWriteFile(t, filepath.Join(cwd, ".herdr-soho", "ws1", "agents.tsv"), "# roster\n")
			mustWriteFile(t, filepath.Join(cwd, ".agents", "soho.conf"), "state_dir=custom-state\n")
		})
		want := []string{
			"migrate: kept " + filepath.Join(fx.cwd, ".herdr-soho") + ": the state dir is set to " + filepath.Join(fx.cwd, "custom-state"),
		}
		if !reflect.DeepEqual(fx.lines, want) {
			t.Fatalf("lines = %v, want %v", fx.lines, want)
		}
		mustExist(t, filepath.Join(fx.cwd, ".herdr-soho"))
	})

	t.Run("state with an empty .soho: would move, nothing removed", func(t *testing.T) {
		fx := runDry(t, func(t *testing.T, env platform.Env, cwd string, old, novo string) {
			mustWriteFile(t, filepath.Join(cwd, ".herdr-soho", "ws1", "agents.tsv"), "# roster\n\n")
			if err := os.MkdirAll(filepath.Join(cwd, ".soho"), 0o700); err != nil {
				t.Fatal(err)
			}
		})
		want := []string{
			"migrate: would move " + filepath.Join(fx.cwd, ".herdr-soho") + " to " + filepath.Join(fx.cwd, ".soho"),
		}
		if !reflect.DeepEqual(fx.lines, want) {
			t.Fatalf("lines = %v, want %v", fx.lines, want)
		}
		mustExist(t, filepath.Join(fx.cwd, ".herdr-soho"))
		mustExist(t, filepath.Join(fx.cwd, ".soho"))
	})
}

// TestMigrateNamesNoFormerNames: a project with nothing from the former
// names yields no line at all.
func TestMigrateNamesNoFormerNames(t *testing.T) {
	t.Run("no former names: nothing", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		if len(lines) != 0 {
			t.Fatalf("lines = %v, want none", lines)
		}
	})

	t.Run("only the soho names: nothing", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		_, novo := formerUserDirs(t, env)
		mustWriteFile(t, filepath.Join(novo, "config"), "max_workers=7\n")
		mustWriteFile(t, filepath.Join(cwd, ".agents", "soho.conf"), "panes=2\n")
		mustWriteFile(t, filepath.Join(cwd, ".soho", "ws1", "agents.tsv"), "# roster\n")
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		if len(lines) != 0 {
			t.Fatalf("lines = %v, want none", lines)
		}
	})
}

// TestMigrateNamesUnreadableRosterKeepsState: a roster that cannot be
// inspected keeps the former state dir: an unreadable agents.tsv and a
// state dir whose entries cannot be listed both end in the kept line with
// the short error; a missing agents.tsv still counts as empty.
func TestMigrateNamesUnreadableRosterKeepsState(t *testing.T) {
	t.Run("an unreadable agents.tsv keeps the state", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old := filepath.Join(cwd, ".herdr-soho")
		roster := filepath.Join(old, "ws", "agents.tsv")
		mustWriteFile(t, roster, "# roster\nworker\tidle\n")
		if err := os.Chmod(roster, 0); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(roster, 0o600)
		if _, err := os.ReadFile(roster); err == nil {
			t.Skip("permission denial unavailable on this platform")
		}
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		assertUnreadableRosterKept(t, old, lines)
	})

	t.Run("a state dir whose entries cannot be listed keeps the state", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old := filepath.Join(cwd, ".herdr-soho")
		mustWriteFile(t, filepath.Join(old, "ws", "agents.tsv"), "# roster\nworker\tidle\n")
		if err := os.Chmod(old, 0o200); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(old, 0o700)
		if _, err := os.ReadDir(old); err == nil {
			t.Skip("a write-only dir still lists entries on this platform")
		}
		ctx := core.LoadConfig(env, cwd)
		lines := migrateNames(&ctx, env, cwd, false)
		assertUnreadableRosterKept(t, old, lines)
	})
}

// assertUnreadableRosterKept checks the kept line's fixed shape (the short
// error sits between the fixed phrases) and that the state dir stayed.
func assertUnreadableRosterKept(t *testing.T, old string, lines []string) {
	t.Helper()
	if len(lines) != 1 {
		t.Fatalf("lines = %v, want the single kept line", lines)
	}
	const head = "migrate: kept "
	const mid = ": could not read its roster ("
	const tail = "); release the workers and run 'soho setup' again"
	line := lines[0]
	if !strings.HasPrefix(line, head+old+mid) || !strings.HasSuffix(line, tail) {
		t.Fatalf("line = %q, want the kept line with a short error", line)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("former state dir moved despite the uninspectable roster: %v", err)
	}
	mustNotExist(t, filepath.Join(filepath.Dir(old), ".soho"))
}

// TestMigrateNamesReloadAfterStateMoveSeesSession: after the state dir
// moves, the config reloads so the rest of the command sees the moved
// session layer.
func TestMigrateNamesReloadAfterStateMoveSeesSession(t *testing.T) {
	env, cwd := migrateEnv(t)
	env["HERDR_WORKSPACE_ID"] = "ws"
	old := filepath.Join(cwd, ".herdr-soho")
	mustWriteFile(t, filepath.Join(old, "ws", "agents.tsv"), "# roster\n")
	mustWriteFile(t, filepath.Join(old, "ws", "session.conf"), "setup_target=local\n")
	ctx := core.LoadConfig(env, cwd)
	lines := migrateNames(&ctx, env, cwd, false)
	want := []string{"migrate: moved " + old + " to " + filepath.Join(cwd, ".soho")}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	if got := core.Cfg(&ctx, "setup_target", "canonical", env); got != "local" {
		t.Fatalf("setup_target = %q after the state move, want local from the moved session.conf", got)
	}
	if got := core.CfgSource(&ctx, "setup_target", env); got != "session" {
		t.Fatalf("setup_target source = %q, want session", got)
	}
}

// TestMigrateNamesExplicitStatePinKeepsOld: an explicitly pinned state dir
// keeps the former state even when the pin resolves to the default
// <root>/.soho: SOHO_DIR, SOHO_STATE_DIR and the state_dir entry.
func TestMigrateNamesExplicitStatePinKeepsOld(t *testing.T) {
	for _, mode := range []string{"SOHO_DIR", "SOHO_STATE_DIR", "state_dir config"} {
		t.Run(mode, func(t *testing.T) {
			env, cwd := migrateEnv(t)
			old := filepath.Join(cwd, ".herdr-soho")
			mustWriteFile(t, filepath.Join(old, "ws", "agents.tsv"), "# roster\n")
			switch mode {
			case "SOHO_DIR":
				env["SOHO_DIR"] = filepath.Join(cwd, ".soho")
			case "SOHO_STATE_DIR":
				env["SOHO_STATE_DIR"] = ".soho"
			default:
				mustWriteFile(t, filepath.Join(cwd, ".agents", "soho.conf"), "state_dir=.soho\n")
			}
			ctx := core.LoadConfig(env, cwd)
			lines := migrateNames(&ctx, env, cwd, false)
			want := []string{"migrate: kept " + old + ": the state dir is set to " + filepath.Join(cwd, ".soho")}
			if !reflect.DeepEqual(lines, want) {
				t.Fatalf("lines = %v, want %v", lines, want)
			}
			if _, err := os.Stat(old); err != nil {
				t.Fatalf("former state dir moved despite the explicit pin: %v", err)
			}
		})
	}
}

// TestMigrateNamesDryProjectedDestinations: a dry run projects the
// destinations it would write, so the second former entry targeting one of
// them shows the same kept line the real run produces.
func TestMigrateNamesDryProjectedDestinations(t *testing.T) {
	t.Run("two former project configs: the second is kept in dry and in the real run", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		old1 := filepath.Join(cwd, ".agents", "herdr-soho.conf")
		old2 := filepath.Join(cwd, ".agents", "herdr-agents.conf")
		soho := filepath.Join(cwd, ".agents", "soho.conf")
		mustWriteFile(t, old1, "layout=tab\n")
		mustWriteFile(t, old2, "layout=split\n")
		before := treeListing(t, cwd)
		ctx := core.LoadConfig(env, cwd)
		dry := migrateNames(&ctx, env, cwd, true)
		if got := treeListing(t, cwd); got != before {
			t.Fatalf("dry run changed the tree")
		}
		keptLine := "migrate: kept " + old2 + ": " + soho + " already exists; merge by hand"
		wantDry := []string{"migrate: would move " + old1 + " to " + soho, keptLine}
		if !reflect.DeepEqual(dry, wantDry) {
			t.Fatalf("dry lines = %v, want %v", dry, wantDry)
		}
		actual := migrateNames(&ctx, env, cwd, false)
		wantActual := []string{"migrate: moved " + old1 + " to " + soho, keptLine}
		if !reflect.DeepEqual(actual, wantActual) {
			t.Fatalf("actual lines = %v, want %v", actual, wantActual)
		}
		if got := mustReadFile(t, soho); got != "layout=tab\n" {
			t.Fatalf("soho.conf = %q, want the first former file", got)
		}
		mustExist(t, old2)
	})

	t.Run("user dirs with a projected conflict: the second is kept in dry", func(t *testing.T) {
		env, cwd := migrateEnv(t)
		oldSo, novo := formerUserDirs(t, env)
		oldAg := filepath.Join(filepath.Dir(novo), "herdr-agents")
		mustWriteFile(t, filepath.Join(oldSo, "config"), "a=1\n")
		mustWriteFile(t, filepath.Join(oldAg, "config"), "b=2\n")
		root := filepath.Dir(env.Get("HOME"))
		before := treeListing(t, root)
		ctx := core.LoadConfig(env, cwd)
		dry := migrateNames(&ctx, env, cwd, true)
		if got := treeListing(t, root); got != before {
			t.Fatalf("dry run changed the tree")
		}
		want := []string{
			"migrate: would move " + filepath.Join(oldSo, "config") + " to " + filepath.Join(novo, "config"),
			"migrate: would remove the empty " + oldSo,
			"migrate: kept " + filepath.Join(oldAg, "config") + ": " + filepath.Join(novo, "config") + " already exists; merge by hand",
		}
		if !reflect.DeepEqual(dry, want) {
			t.Fatalf("dry lines = %v, want %v", dry, want)
		}
		mustExist(t, filepath.Join(oldSo, "config"))
		mustExist(t, filepath.Join(oldAg, "config"))
	})
}

// TestCmdPlanInsideSkillSkipsMigrationPreviews: with the project root
// inside the skill, --plan runs no migration preview and makes no herdr
// call; the plan itself still prints.
func TestCmdPlanInsideSkillSkipsMigrationPreviews(t *testing.T) {
	env, cwd := migrateEnv(t)
	env["SOHO_SKILL_DIR"] = cwd
	bin := t.TempDir()
	env["PATH"] = bin
	env["SOHO_FAKECLI_CONFIG"] = bin
	if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{{AnyArgs: true}}); err != nil {
		t.Fatal(err)
	}
	ctx := core.LoadConfig(env, cwd)
	code, out, msg := runSetupWrite([]string{"--plan", "--no-hooks"}, &ctx, env, cwd)
	calls, err := fakecli.ReadCalls(filepath.Join(bin, "herdr.calls.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading the call log: %v", err)
	}
	t.Logf("code=%d calls=%v message=%q", code, calls, msg)
	if len(calls) != 0 {
		t.Fatalf("plan invoked herdr inside the skill: %v", calls)
	}
	if strings.Contains(out, "migrate: ") {
		t.Fatalf("plan printed migration lines inside the skill: %q", out)
	}
	if code != 0 {
		t.Fatalf("code = %d, message = %q; the plan itself is not refused inside the skill", code, msg)
	}
	if !strings.HasPrefix(out, "plan (nothing is written):") {
		t.Fatalf("plan output = %q, want the plan header", out)
	}
}

// TestMigrateNamesRenameFailureKeepsSource: a refused rename leaves the
// source byte-identical, creates no destination, and reports the kept line
// with the error.
func TestMigrateNamesRenameFailureKeepsSource(t *testing.T) {
	env, cwd := migrateEnv(t)
	old := filepath.Join(cwd, ".agents", "herdr-soho.conf")
	mustWriteFile(t, old, "# keep\nlayout=tab\n")
	parent := filepath.Dir(old)
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o700)
	ctx := core.LoadConfig(env, cwd)
	lines := migrateNames(&ctx, env, cwd, false)
	if _, err := os.Stat(filepath.Join(parent, "soho.conf")); err == nil {
		t.Skip("a read-only dir does not refuse the rename on this platform")
	}
	if got := mustReadFile(t, old); got != "# keep\nlayout=tab\n" {
		t.Fatalf("source changed after the refused rename: %q", got)
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "migrate: kept "+old+": ") {
		t.Fatalf("lines = %v, want the single kept line with the error", lines)
	}
}
