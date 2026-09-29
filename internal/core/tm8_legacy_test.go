package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestTM8LegacyCore(t *testing.T) {
	t.Run(`applyLegacyEnv: a lone HERDR_AGENTS_* is copied, the old name stays, and the list is remembered`, func(t *testing.T) { // JS: "applyLegacyEnv: a lone HERDR_AGENTS_* is copied, the old name stays, and the list is remembered"
		env := platform.Env{"HERDR_AGENTS_LAYOUT": "columns"}
		copied := ApplyLegacyEnv(env)
		if env.Get("HERDR_SOHO_LAYOUT") != "columns" || env.Get("HERDR_AGENTS_LAYOUT") != "columns" || strings.Join(copied, ",") != "HERDR_AGENTS_LAYOUT" || strings.Join(LegacyEnvCopied(), ",") != "HERDR_AGENTS_LAYOUT" {
			t.Fatalf("legacy environment copied=%v remembered=%v env=%v", copied, LegacyEnvCopied(), env)
		}
	})
	t.Run(`applyLegacyEnv: a non-empty HERDR_SOHO_* wins and nothing is copied`, func(t *testing.T) { // JS: "applyLegacyEnv: a non-empty HERDR_SOHO_* wins and nothing is copied"
		env := platform.Env{"HERDR_AGENTS_LAYOUT": "legacy", "HERDR_SOHO_LAYOUT": "columns"}
		if copied := ApplyLegacyEnv(env); env.Get("HERDR_SOHO_LAYOUT") != "columns" || len(copied) != 0 {
			t.Fatalf("copied=%v env=%v", copied, env)
		}
	})
	t.Run(`applyLegacyEnv: an empty HERDR_SOHO_* value is replaced; empty old values and other names are ignored`, func(t *testing.T) { // JS: "applyLegacyEnv: an empty HERDR_SOHO_* value is replaced; empty old values and other names are ignored"
		env := platform.Env{"HERDR_AGENTS_LAYOUT": "legacy", "HERDR_SOHO_LAYOUT": "", "HERDR_AGENTS_EMPTY": "", "HERDR_SOHO_EMPTY": "", "HERDR_AGENTSX_LAYOUT": "ignored"}
		copied := ApplyLegacyEnv(env)
		if env.Get("HERDR_SOHO_LAYOUT") != "legacy" || env.Get("HERDR_SOHO_EMPTY") != "" || strings.Join(copied, ",") != "HERDR_AGENTS_LAYOUT" {
			t.Fatalf("copied=%v env=%v", copied, env)
		}
	})
	t.Run(`applyLegacyEnv: the copied old names come back sorted`, func(t *testing.T) { // JS: "applyLegacyEnv: the copied old names come back sorted"
		env := platform.Env{"HERDR_AGENTS_Z": "z", "HERDR_AGENTS_A": "a"}
		if got := strings.Join(ApplyLegacyEnv(env), ","); got != "HERDR_AGENTS_A,HERDR_AGENTS_Z" {
			t.Fatalf("copied order=%q", got)
		}
	})
	t.Run(`legacy config paths: the old directory and file names next to the new ones`, func(t *testing.T) { // JS: "legacy config paths: the old directory and file names next to the new ones"
		env := platform.Env{"HOME": "/home/user", "XDG_CONFIG_HOME": "/config", "USERPROFILE": `C:\Users\user`, "APPDATA": `C:\Users\user\AppData\Roaming`}
		if got := LegacyUserConfigPath("darwin", env); got != filepath.Join("/config", "herdr-agents", "config") {
			t.Fatalf("darwin legacy user config=%q", got)
		}
		if got := LegacyUserConfigPath("linux", platform.Env{"HOME": "/home/user"}); got != filepath.Join("/home/user", ".config", "herdr-agents", "config") {
			t.Fatalf("linux legacy user config=%q", got)
		}
		winEnv := platform.Env{"USERPROFILE": `C:\Users\user`, "APPDATA": `C:\Users\user\AppData\Roaming`}
		if got := LegacyUserConfigPath("win32", winEnv); got != filepath.Join(`C:\Users\user\AppData\Roaming`, "herdr-agents", "config") {
			t.Fatalf("windows legacy user config=%q", got)
		}
		if got := LegacyProjectConfigPath("/repo"); got != filepath.Join("/repo", ".agents", "herdr-agents.conf") {
			t.Fatalf("legacy project config=%q", got)
		}
	})
	t.Run(`legacyStatePath uses Windows separators`, func(t *testing.T) { // JS: "legacyStatePath uses Windows separators"
		if runtime.GOOS != "windows" {
			t.Skip("Go filepath follows the host OS; this JS path.win32 case can only run on Windows")
		}
		if got := LegacyStatePath(`C:\workspace\repo`); got != `C:\workspace\repo\.herdr-agents` {
			t.Fatalf("legacy state path=%q", got)
		}
	})
	t.Run(`effectiveConfigFile: the new file wins, the legacy one wins over an absent new one, absent both gives the new path`, func(t *testing.T) { // JS: "effectiveConfigFile: the new file wins, the legacy one wins over an absent new one, absent both gives the new path"
		dir := t.TempDir()
		newPath, oldPath := filepath.Join(dir, "new"), filepath.Join(dir, "old")
		if got := EffectiveConfigFile(newPath, oldPath); got != newPath {
			t.Fatalf("neither exists=%q", got)
		}
		if err := os.WriteFile(oldPath, []byte("old=1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := EffectiveConfigFile(newPath, oldPath); got != oldPath {
			t.Fatalf("legacy only=%q", got)
		}
		if err := os.WriteFile(newPath, []byte("new=1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := EffectiveConfigFile(newPath, oldPath); got != newPath {
			t.Fatalf("both exist=%q", got)
		}
	})
	t.Run(`defaultStateDirName: .herdr-agents only while .herdr-soho is absent and .herdr-agents is a directory`, func(t *testing.T) { // JS: "defaultStateDirName: .herdr-agents only while .herdr-soho is absent and .herdr-agents is a directory"
		root := t.TempDir()
		legacy := filepath.Join(root, ".herdr-agents")
		if err := os.Mkdir(legacy, 0o700); err != nil {
			t.Fatal(err)
		}
		if got := DefaultStateDirName(root); got != ".herdr-agents" {
			t.Fatalf("legacy only=%q", got)
		}
		if err := os.Mkdir(filepath.Join(root, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		if got := DefaultStateDirName(root); got != ".herdr-soho" {
			t.Fatalf("new dir wins=%q", got)
		}
	})
	t.Run(`migrateLegacyConfigFile: the user and project new paths copy byte for byte with the legacy mode; a second copy and a foreign dest are refused; the legacy file stays`, func(t *testing.T) { // JS: "migrateLegacyConfigFile: the user and project new paths copy byte for byte with the legacy mode; a second copy and a foreign dest are refused; the legacy file stays"
		for _, where := range []string{"user", "project"} {
			t.Run(where, func(t *testing.T) {
				root := t.TempDir()
				env := platform.Env{"HOME": filepath.Join(root, "home"), "XDG_CONFIG_HOME": filepath.Join(root, "config")}
				oldPath, newPath := filepath.Join(root, ".agents", "herdr-agents.conf"), filepath.Join(root, ".agents", "herdr-soho.conf")
				if where == "user" {
					oldPath = LegacyUserConfigPath(platform.Current(), env)
					newPath = platform.UserConfigPath(platform.Current(), env)
				}
				bytes := []byte("# legacy\nlayout=columns\n")
				if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(oldPath, bytes, 0o640); err != nil {
					t.Fatal(err)
				}
				if !MigrateLegacyConfigFile(newPath, env, root) {
					t.Fatal("first migration refused")
				}
				got, err := os.ReadFile(newPath)
				if err != nil || string(got) != string(bytes) {
					t.Fatalf("new bytes=%q err=%v", got, err)
				}
				if runtime.GOOS != "windows" {
					info, err := os.Stat(newPath)
					if err != nil || info.Mode().Perm() != 0o640 {
						t.Fatalf("mode=%v err=%v", info, err)
					}
				}
				if again := MigrateLegacyConfigFile(newPath, env, root); again {
					t.Fatal("second copy overwrote destination")
				}
				still, err := os.ReadFile(oldPath)
				if err != nil || string(still) != string(bytes) {
					t.Fatalf("legacy changed=%q err=%v", still, err)
				}
				if err := os.WriteFile(newPath, []byte("foreign\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if MigrateLegacyConfigFile(newPath, env, root) {
					t.Fatal("foreign destination overwritten")
				}
				foreign, err := os.ReadFile(newPath)
				if err != nil || string(foreign) != "foreign\n" {
					t.Fatalf("foreign destination changed=%q err=%v", foreign, err)
				}
			})
		}
	})
}
