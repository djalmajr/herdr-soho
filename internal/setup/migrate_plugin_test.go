package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/testutil"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// mpListJSON wraps plugin rows in the 'herdr plugin list --json' envelope.
func mpListJSON(plugins string) string {
	return `{"id":"cli:plugin","result":{"plugins":[` + plugins + `]}}`
}

const (
	mpFormerLocal  = `{"plugin_id":"djalmajr.herdr-soho","source":{"kind":"local"},"plugin_root":"/tmp/herdr-soho/plugin","enabled":true}`
	mpFormerGitHub = `{"plugin_id":"djalmajr.herdr-soho","source":{"kind":"github"},"plugin_root":"/tmp/herdr-soho/plugin","enabled":true}`
	mpSohoPlugin   = `{"plugin_id":"djalmajr.soho","source":{"kind":"local"},"plugin_root":"/tmp/soho/plugin","enabled":true}`
	mpOtherPlugin  = `{"plugin_id":"other.plugin","source":{"kind":"github"},"enabled":false}`

	mpConfigShortcuts = `# Herdr shortcuts
[[keys.command]]
key = "cmd+p"
type = "plugin_action"
command = "djalmajr.herdr-soho.pick"

# manual note about djalmajr.herdr-soho.pick
[[keys.command]]
key = "cmd+k"
type = "plugin_action"
command = "outra.coisa"
`

	mpConfigShortcutsMigrated = `# Herdr shortcuts
[[keys.command]]
key = "cmd+p"
type = "plugin_action"
command = "djalmajr.soho.pick"

# manual note about djalmajr.herdr-soho.pick
[[keys.command]]
key = "cmd+k"
type = "plugin_action"
command = "outra.coisa"
`

	mpConfigOneShortcut = `[[keys.command]]
key = "cmd+p"
type = "plugin_action"
command = "djalmajr.herdr-soho.pick"
`

	mpConfigOneShortcutMigrated = `[[keys.command]]
key = "cmd+p"
type = "plugin_action"
command = "djalmajr.soho.pick"
`

	mpConfigTwoShortcuts = `[[keys.command]]
key = "cmd+p"
type = "plugin_action"
command = "djalmajr.herdr-soho.pick"

[[keys.command]]
key = "cmd+j"
type = "plugin_action"
command = "djalmajr.herdr-soho.jump"
`
)

// migratePluginFixture builds a hermetic env with a fake herdr on PATH and a
// Herdr config tree: config.toml sits three levels above the fake
// 'plugin config-dir djalmajr.soho' output (plugins/config/<id>).
func migratePluginFixture(t *testing.T) (platform.Env, string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	herdr := filepath.Join(root, "herdr")
	pluginConfig := filepath.Join(herdr, "plugins", "config")
	for _, dir := range []string{bin, pluginConfig} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := platform.Env{}
	for _, item := range fakecli.Env(testutil.CleanEnv(t), bin) {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	// Every executable the migration resolves must come from the fixture.
	env["PATH"] = bin
	if runtime.GOOS == "windows" {
		env["PATHEXT"] = ".EXE;.CMD"
	}
	return env, root, bin, filepath.Join(herdr, "config.toml"), filepath.Join(pluginConfig, sohoPluginID)
}

// mpCalls returns the ordered argv of every fake herdr call.
func mpCalls(t *testing.T, bin string) [][]string {
	t.Helper()
	calls, err := fakecli.ReadCalls(filepath.Join(bin, "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	argv := make([][]string, 0, len(calls))
	for _, call := range calls {
		argv = append(argv, call.Argv)
	}
	return argv
}

func TestMigratePlugin(t *testing.T) {
	t.Run(`without the former plugin: no line and no write command`, func(t *testing.T) { // DJA-318 N4b criterion 1.
		env, _, bin, _, _ := migratePluginFixture(t)
		if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
			{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpOtherPlugin)},
		}); err != nil {
			t.Fatal(err)
		}
		if lines := migratePlugin(env, "v0.3.0", false); len(lines) != 0 {
			t.Fatalf("expected no lines, got %v", lines)
		}
		want := [][]string{{"plugin", "list", "--json"}}
		if got := mpCalls(t, bin); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls=%#v", got)
		}
	})
	t.Run(`former local + release version: install, shortcut rename with backup, reload and unlink`, func(t *testing.T) { // DJA-318 N4b criterion 2.
		env, _, bin, configToml, configDir := migratePluginFixture(t)
		if err := os.WriteFile(configToml, []byte(mpConfigShortcuts), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
			{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpFormerLocal)},
			{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Stdout: configDir},
			{Argv: []string{"plugin", "install", "djalmajr/soho/plugin", "--ref", "v0.3.0", "--yes"}},
			{Argv: []string{"server", "reload-config"}},
			{Argv: []string{"plugin", "unlink", "djalmajr.herdr-soho"}},
		}); err != nil {
			t.Fatal(err)
		}
		lines := migratePlugin(env, "v0.3.0", false)
		if len(lines) != 2 {
			t.Fatalf("lines=%v", lines)
		}
		const prefix = "migrate: plugin: 1 shortcut(s) now call djalmajr.soho (backup "
		if !strings.HasPrefix(lines[0], prefix) || !strings.HasSuffix(lines[0], ")") {
			t.Fatalf("shortcut line=%q", lines[0])
		}
		backup := strings.TrimSuffix(strings.TrimPrefix(lines[0], prefix), ")")
		if !strings.HasPrefix(backup, configToml+".bak-soho-") {
			t.Fatalf("backup=%q", backup)
		}
		if got, err := os.ReadFile(backup); err != nil || string(got) != mpConfigShortcuts {
			t.Fatalf("backup content=%q err=%v", got, err)
		}
		if got, err := os.ReadFile(configToml); err != nil || string(got) != mpConfigShortcutsMigrated {
			t.Fatalf("config content=%q err=%v", got, err)
		}
		if lines[1] != "migrate: plugin: removed the former djalmajr.herdr-soho (unlink)" {
			t.Fatalf("removal line=%q", lines[1])
		}
		want := [][]string{
			{"plugin", "list", "--json"},
			{"plugin", "install", "djalmajr/soho/plugin", "--ref", "v0.3.0", "--yes"},
			{"plugin", "config-dir", "djalmajr.soho"},
			{"server", "reload-config"},
			{"plugin", "unlink", "djalmajr.herdr-soho"},
		}
		if got := mpCalls(t, bin); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls=%#v", got)
		}
	})
	t.Run(`former plugin installed from GitHub (source kind not local): uninstall`, func(t *testing.T) { // DJA-318 N4b criterion 3.
		env, _, bin, configToml, configDir := migratePluginFixture(t)
		if err := os.WriteFile(configToml, []byte(mpConfigOneShortcut), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
			{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpFormerGitHub)},
			{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Stdout: configDir},
			{Argv: []string{"plugin", "install", "djalmajr/soho/plugin", "--ref", "v0.3.0", "--yes"}},
			{Argv: []string{"server", "reload-config"}},
			{Argv: []string{"plugin", "uninstall", "djalmajr.herdr-soho"}},
		}); err != nil {
			t.Fatal(err)
		}
		lines := migratePlugin(env, "v0.3.0", false)
		if len(lines) != 2 || lines[1] != "migrate: plugin: removed the former djalmajr.herdr-soho (uninstall)" {
			t.Fatalf("lines=%v", lines)
		}
		if got, err := os.ReadFile(configToml); err != nil || string(got) != mpConfigOneShortcutMigrated {
			t.Fatalf("config content=%q err=%v", got, err)
		}
		want := [][]string{
			{"plugin", "list", "--json"},
			{"plugin", "install", "djalmajr/soho/plugin", "--ref", "v0.3.0", "--yes"},
			{"plugin", "config-dir", "djalmajr.soho"},
			{"server", "reload-config"},
			{"plugin", "uninstall", "djalmajr.herdr-soho"},
		}
		if got := mpCalls(t, bin); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls=%#v", got)
		}
	})
	t.Run(`the new plugin is already registered: no install, shortcuts and removal still run`, func(t *testing.T) { // DJA-318 N4b criterion 4.
		env, _, bin, configToml, configDir := migratePluginFixture(t)
		if err := os.WriteFile(configToml, []byte(mpConfigOneShortcut), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
			{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpFormerLocal + "," + mpSohoPlugin)},
			{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Stdout: configDir},
			{Argv: []string{"server", "reload-config"}},
			{Argv: []string{"plugin", "unlink", "djalmajr.herdr-soho"}},
		}); err != nil {
			t.Fatal(err)
		}
		lines := migratePlugin(env, "v0.3.0", false)
		if len(lines) != 2 || !strings.Contains(lines[0], "1 shortcut(s) now call djalmajr.soho") || lines[1] != "migrate: plugin: removed the former djalmajr.herdr-soho (unlink)" {
			t.Fatalf("lines=%v", lines)
		}
		want := [][]string{
			{"plugin", "list", "--json"},
			{"plugin", "config-dir", "djalmajr.soho"},
			{"server", "reload-config"},
			{"plugin", "unlink", "djalmajr.herdr-soho"},
		}
		if got := mpCalls(t, bin); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls=%#v", got)
		}
	})
	for _, version := range []string{"dev", "dev+abc123"} {
		t.Run("development version "+version+": only the manual link line, nothing written", func(t *testing.T) { // DJA-318 N4b criterion 5.
			env, root, bin, configToml, _ := migratePluginFixture(t)
			if err := os.WriteFile(configToml, []byte(mpConfigShortcuts), 0o600); err != nil {
				t.Fatal(err)
			}
			env["SOHO_SKILL_DIR"] = filepath.Join(root, "checkout", "skills", "soho")
			if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
				{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpFormerLocal)},
			}); err != nil {
				t.Fatal(err)
			}
			lines := migratePlugin(env, version, false)
			wantLine := "migrate: plugin: link djalmajr.soho from your checkout first: herdr plugin link " + filepath.Join(root, "checkout", "plugin")
			if len(lines) != 1 || lines[0] != wantLine {
				t.Fatalf("lines=%v want %q", lines, wantLine)
			}
			if got := mpCalls(t, bin); !reflect.DeepEqual(got, [][]string{{"plugin", "list", "--json"}}) {
				t.Fatalf("calls=%#v", got)
			}
			if got, err := os.ReadFile(configToml); err != nil || string(got) != mpConfigShortcuts {
				t.Fatalf("config changed: %q err=%v", got, err)
			}
		})
	}
	t.Run(`install fails: the former plugin stays and the config is untouched`, func(t *testing.T) { // DJA-318 N4b criterion 6.
		env, _, bin, configToml, configDir := migratePluginFixture(t)
		if err := os.WriteFile(configToml, []byte(mpConfigShortcuts), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
			{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpFormerLocal)},
			{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Stdout: configDir},
			{Argv: []string{"plugin", "install", "djalmajr/soho/plugin", "--ref", "v0.3.0", "--yes"}, Code: 3, Stderr: "install boom\n"},
			{Argv: []string{"plugin", "unlink", "djalmajr.herdr-soho"}},
		}); err != nil {
			t.Fatal(err)
		}
		lines := migratePlugin(env, "v0.3.0", false)
		if len(lines) != 1 || lines[0] != "migrate: plugin: install of djalmajr.soho failed: exit 3; the former plugin is kept" {
			t.Fatalf("lines=%v", lines)
		}
		want := [][]string{
			{"plugin", "list", "--json"},
			{"plugin", "install", "djalmajr/soho/plugin", "--ref", "v0.3.0", "--yes"},
		}
		if got := mpCalls(t, bin); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls=%#v", got)
		}
		if got, err := os.ReadFile(configToml); err != nil || string(got) != mpConfigShortcuts {
			t.Fatalf("config changed: %q err=%v", got, err)
		}
	})
	t.Run(`dry: would-run lines with the exact commands, no file and no write command`, func(t *testing.T) { // DJA-318 N4b criterion 7.
		env, _, bin, configToml, configDir := migratePluginFixture(t)
		if err := os.WriteFile(configToml, []byte(mpConfigTwoShortcuts), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
			{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpFormerLocal)},
			{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Stdout: configDir},
		}); err != nil {
			t.Fatal(err)
		}
		lines := migratePlugin(env, "v0.3.0-rc.1", true)
		want := []string{
			"migrate: plugin: would run: herdr plugin install djalmajr/soho/plugin --ref v0.3.0-rc.1 --yes",
			"migrate: plugin: would replace 2 shortcut(s) in " + configToml,
			"migrate: plugin: would run: herdr server reload-config",
			"migrate: plugin: would run: herdr plugin unlink djalmajr.herdr-soho",
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines=%v", lines)
		}
		if got, err := os.ReadFile(configToml); err != nil || string(got) != mpConfigTwoShortcuts {
			t.Fatalf("config changed: %q err=%v", got, err)
		}
		for _, entry := range mustReadDir(t, filepath.Dir(configToml)) {
			if strings.HasPrefix(entry.Name(), "config.toml.bak-soho-") {
				t.Fatalf("dry run wrote a backup: %s", entry.Name())
			}
		}
		wantCalls := [][]string{
			{"plugin", "list", "--json"},
			{"plugin", "config-dir", "djalmajr.soho"},
		}
		if got := mpCalls(t, bin); !reflect.DeepEqual(got, wantCalls) {
			t.Fatalf("calls=%#v", got)
		}
	})
	t.Run(`plugin list fails: one skipped line and nothing else`, func(t *testing.T) { // DJA-318 N4b criterion 8.
		for _, tc := range []struct {
			name   string
			rule   fakecli.Rule
			want   string
			noFake bool
		}{
			{"list exits non-zero", fakecli.Rule{Argv: []string{"plugin", "list", "--json"}, Code: 2}, "migrate: plugin: skipped (herdr plugin list failed: exit 2)", false},
			{"herdr is not on PATH: nothing to migrate, no line", fakecli.Rule{}, "", true},
			{"list succeeds with no output: nothing to migrate, no line", fakecli.Rule{Argv: []string{"plugin", "list", "--json"}, Stdout: ""}, "", false},
			{"list prints invalid JSON", fakecli.Rule{Argv: []string{"plugin", "list", "--json"}, Stdout: "not json\n"}, "migrate: plugin: skipped (herdr plugin list failed: invalid JSON)", false},
			{"list misses the result envelope", fakecli.Rule{Argv: []string{"plugin", "list", "--json"}, Stdout: `{"id":"cli:plugin"}`}, "migrate: plugin: skipped (herdr plugin list failed: invalid JSON)", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				env, _, bin, _, _ := migratePluginFixture(t)
				if tc.noFake {
					env["PATH"] = t.TempDir()
				} else if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{tc.rule}); err != nil {
					t.Fatal(err)
				}
				lines := migratePlugin(env, "v0.3.0", false)
				if tc.want == "" {
					if len(lines) != 0 {
						t.Fatalf("lines=%v want none", lines)
					}
				} else if len(lines) != 1 || lines[0] != tc.want {
					t.Fatalf("lines=%v want %q", lines, tc.want)
				}
				if !tc.noFake {
					if got := mpCalls(t, bin); !reflect.DeepEqual(got, [][]string{{"plugin", "list", "--json"}}) {
						t.Fatalf("calls=%#v", got)
					}
				}
			})
		}
	})
	t.Run(`no Herdr config file: no shortcut to move, the removal runs`, func(t *testing.T) {
		env, _, bin, configToml, configDir := migratePluginFixture(t)
		if err := os.Remove(configToml); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
			{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpFormerLocal + "," + mpSohoPlugin)},
			{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Stdout: configDir},
			{Argv: []string{"plugin", "unlink", "djalmajr.herdr-soho"}},
		}); err != nil {
			t.Fatal(err)
		}
		lines := migratePlugin(env, "v0.3.0", false)
		if want := []string{"migrate: plugin: removed the former djalmajr.herdr-soho (unlink)"}; !reflect.DeepEqual(lines, want) {
			t.Fatalf("lines=%v", lines)
		}
		if got := mpCalls(t, bin); !reflect.DeepEqual(got, [][]string{
			{"plugin", "list", "--json"},
			{"plugin", "config-dir", "djalmajr.soho"},
			{"plugin", "unlink", "djalmajr.herdr-soho"},
		}) {
			t.Fatalf("calls=%#v", got)
		}
	})
	t.Run(`the shortcuts cannot be confirmed: the former plugin is kept`, func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			rules  func(configDir string) []fakecli.Rule
			config func(t *testing.T, configToml string)
			want   []string
			calls  [][]string
		}{
			{
				name: "config-dir fails",
				rules: func(configDir string) []fakecli.Rule {
					return []fakecli.Rule{{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Code: 1, Stderr: "no such plugin\n"}}
				},
				config: func(t *testing.T, configToml string) { mustWritePluginConfig(t, configToml, mpConfigOneShortcut) },
				want:   []string{"migrate: plugin: could not locate the Herdr config (herdr plugin config-dir failed); the former plugin is kept"},
				calls:  [][]string{{"plugin", "list", "--json"}, {"plugin", "config-dir", "djalmajr.soho"}},
			},
			{
				name: "the config path cannot be read",
				rules: func(configDir string) []fakecli.Rule {
					return []fakecli.Rule{{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Stdout: configDir}}
				},
				config: func(t *testing.T, configToml string) {
					if err := os.Remove(configToml); err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
					if err := os.Mkdir(configToml, 0o700); err != nil {
						t.Fatal(err)
					}
				},
				want:  nil,
				calls: [][]string{{"plugin", "list", "--json"}, {"plugin", "config-dir", "djalmajr.soho"}},
			},
			{
				name: "reload-config fails after the shortcuts are written",
				rules: func(configDir string) []fakecli.Rule {
					return []fakecli.Rule{
						{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Stdout: configDir},
						{Argv: []string{"server", "reload-config"}, Code: 1, Stderr: "server down\n"},
					}
				},
				config: func(t *testing.T, configToml string) { mustWritePluginConfig(t, configToml, mpConfigOneShortcut) },
				calls:  [][]string{{"plugin", "list", "--json"}, {"plugin", "config-dir", "djalmajr.soho"}, {"server", "reload-config"}},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				env, _, bin, configToml, configDir := migratePluginFixture(t)
				tc.config(t, configToml)
				rules := append([]fakecli.Rule{{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpFormerLocal + "," + mpSohoPlugin)}}, tc.rules(configDir)...)
				rules = append(rules, fakecli.Rule{Argv: []string{"plugin", "unlink", "djalmajr.herdr-soho"}})
				if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
					t.Fatal(err)
				}
				lines := migratePlugin(env, "v0.3.0", false)
				last := ""
				if len(lines) > 0 {
					last = lines[len(lines)-1]
				}
				if !strings.Contains(last, "the former plugin is kept") {
					t.Fatalf("lines=%v: the last line must say the former plugin is kept", lines)
				}
				if tc.want != nil && !reflect.DeepEqual(lines, tc.want) {
					t.Fatalf("lines=%v want %v", lines, tc.want)
				}
				if got := mpCalls(t, bin); !reflect.DeepEqual(got, tc.calls) {
					t.Fatalf("calls=%#v want %#v (no unlink or uninstall)", got, tc.calls)
				}
			})
		}
	})
	t.Run(`a later run still needs a successful reload before the removal`, func(t *testing.T) {
		env, _, bin, configToml, configDir := migratePluginFixture(t)
		mustWritePluginConfig(t, configToml, mpConfigOneShortcut)
		install := func(reloadCode int) {
			t.Helper()
			// A second install replaces the fake's rules; the fake binary
			// itself refuses to be overwritten.
			exe := "herdr"
			if runtime.GOOS == "windows" {
				exe += ".exe"
			}
			if err := os.Remove(filepath.Join(bin, exe)); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
				{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpFormerLocal + "," + mpSohoPlugin)},
				{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Stdout: configDir},
				{Argv: []string{"server", "reload-config"}, Code: reloadCode, Stderr: "server down\n"},
				{Argv: []string{"plugin", "unlink", "djalmajr.herdr-soho"}},
			}); err != nil {
				t.Fatal(err)
			}
		}
		unlinked := func() bool {
			for _, call := range mpCalls(t, bin) {
				if len(call) > 1 && call[0] == "plugin" && call[1] == "unlink" {
					return true
				}
			}
			return false
		}
		install(1)
		first := migratePlugin(env, "v0.3.0", false)
		if len(first) != 2 || !strings.Contains(first[0], "1 shortcut(s) now call djalmajr.soho") || !strings.Contains(first[1], "the former plugin is kept") || unlinked() {
			t.Fatalf("first run lines=%v unlinked=%v", first, unlinked())
		}
		// The file is migrated now; the server still did not reload it.
		second := migratePlugin(env, "v0.3.0", false)
		if len(second) != 1 || !strings.Contains(second[0], "reload-config failed") || !strings.Contains(second[0], "the former plugin is kept") || unlinked() {
			t.Fatalf("second run lines=%v unlinked=%v", second, unlinked())
		}
		install(0)
		third := migratePlugin(env, "v0.3.0", false)
		if want := []string{"migrate: plugin: removed the former djalmajr.herdr-soho (unlink)"}; !reflect.DeepEqual(third, want) || !unlinked() {
			t.Fatalf("third run lines=%v unlinked=%v", third, unlinked())
		}
	})
	t.Run(`config without former shortcuts: no rename line, no reload, no backup`, func(t *testing.T) {
		env, _, bin, configToml, configDir := migratePluginFixture(t)
		if err := os.WriteFile(configToml, []byte("# untouched\ncommand = \"outra.coisa\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
			{Argv: []string{"plugin", "list", "--json"}, Stdout: mpListJSON(mpFormerLocal + "," + mpSohoPlugin)},
			{Argv: []string{"plugin", "config-dir", "djalmajr.soho"}, Stdout: configDir},
			{Argv: []string{"plugin", "unlink", "djalmajr.herdr-soho"}},
		}); err != nil {
			t.Fatal(err)
		}
		lines := migratePlugin(env, "v0.3.0", false)
		if len(lines) != 1 || lines[0] != "migrate: plugin: removed the former djalmajr.herdr-soho (unlink)" {
			t.Fatalf("lines=%v", lines)
		}
		for _, entry := range mustReadDir(t, filepath.Dir(configToml)) {
			if strings.HasPrefix(entry.Name(), "config.toml.bak-soho-") {
				t.Fatalf("backup without a rename: %s", entry.Name())
			}
		}
		if got := mpCalls(t, bin); !reflect.DeepEqual(got, [][]string{
			{"plugin", "list", "--json"},
			{"plugin", "config-dir", "djalmajr.soho"},
			{"plugin", "unlink", "djalmajr.herdr-soho"},
		}) {
			t.Fatalf("calls=%#v", got)
		}
	})
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func mustWritePluginConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
