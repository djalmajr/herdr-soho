package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

func TestTM8LegacyCore(t *testing.T) {
	t.Run(`legacy config paths: the herdr-soho directory and file names next to the soho ones`, func(t *testing.T) { // JS: "legacy config paths: the old directory and file names next to the new ones"
		env := platform.Env{"HOME": "/home/user", "XDG_CONFIG_HOME": "/config", "USERPROFILE": `C:\Users\user`, "APPDATA": `C:\Users\user\AppData\Roaming`}
		if got := LegacyUserConfigPath("darwin", env); got != filepath.Join("/config", "herdr-soho", "config") {
			t.Fatalf("darwin legacy user config=%q", got)
		}
		if got := LegacyUserConfigPath("linux", platform.Env{"HOME": "/home/user"}); got != filepath.Join("/home/user", ".config", "herdr-soho", "config") {
			t.Fatalf("linux legacy user config=%q", got)
		}
		winEnv := platform.Env{"USERPROFILE": `C:\Users\user`, "APPDATA": `C:\Users\user\AppData\Roaming`}
		if got := LegacyUserConfigPath("win32", winEnv); got != filepath.Join(`C:\Users\user\AppData\Roaming`, "herdr-soho", "config") {
			t.Fatalf("windows legacy user config=%q", got)
		}
		if got := LegacyProjectConfigPath("/repo"); got != filepath.Join("/repo", ".agents", "herdr-soho.conf") {
			t.Fatalf("legacy project config=%q", got)
		}
	})
	t.Run(`legacyStatePath uses Windows separators`, func(t *testing.T) { // JS: "legacyStatePath uses Windows separators"
		if runtime.GOOS != "windows" {
			t.Skip("Go filepath follows the host OS; this path.win32 case can only run on Windows")
		}
		if got := LegacyStatePath(`C:\workspace\repo`); got != `C:\workspace\repo\.herdr-soho` {
			t.Fatalf("legacy state path=%q", got)
		}
	})
	t.Run(`herdr-agents config files are no longer read as the project layer`, func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".agents", "herdr-agents.conf"), []byte("max_workers=11\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// The repository's skill supplies the defaults on every system: a
		// Windows lookup finds none from HOME alone.
		env := platform.Env{"HOME": t.TempDir(), "SOHO_SKILL_DIR": filepath.Join("..", "..", "skills", "soho")}
		if got := ProjectConfigFileUsed(env, root); got != "" {
			t.Fatalf("herdr-agents.conf is still used=%q", got)
		}
		ctx := LoadConfig(env, root)
		// The default 3 must hold: the herdr-agents file's 11 is not read.
		if got := Cfg(&ctx, "max_workers", "", env); got != "3" || CfgSource(&ctx, "max_workers", env) == "project" {
			t.Fatalf("max_workers=%q source=%s", got, CfgSource(&ctx, "max_workers", env))
		}
	})
	t.Run(`the default state directory is always .soho`, func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		if got := StateDirSetting(&Config{Entries: map[string]ConfigEntry{}}, platform.Env{}, root); got != ".soho" {
			t.Fatalf("state dir setting = %q, want .soho even with the former dir present", got)
		}
	})
}
