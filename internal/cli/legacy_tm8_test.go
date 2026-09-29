package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
)

func TestTM8LegacyCLI(t *testing.T) {
	t.Run(`config (launcher): the legacy user file is read under the user layer, shown flagged, and not created`, func(t *testing.T) { // JS: "config (launcher): the legacy user file is read under the user layer, shown flagged, and not created"
		env, repo := tm8CommandFixture(t)
		legacy := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-agents", "config")
		current := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho", "config")
		tm8File(t, legacy, "# keep\nherd_label=crew\n")
		code, out, errOut := runIn(t, []string{"config"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "herd_label") || !strings.Contains(out, legacy+" (legacy)") {
			t.Fatalf("config code=%d out=%q err=%q", code, out, errOut)
		}
		if _, err := os.Stat(current); !os.IsNotExist(err) {
			t.Fatalf("read created current config: %v", err)
		}
	})
	t.Run(`config (launcher): the legacy project file is read under the project layer and shown flagged`, func(t *testing.T) { // JS: "config (launcher): the legacy project file is read under the project layer and shown flagged"
		env, repo := tm8CommandFixture(t)
		legacy := filepath.Join(repo, ".agents", "herdr-agents.conf")
		tm8File(t, legacy, "max_workers=5\n")
		code, out, errOut := runIn(t, []string{"config"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "max_workers") || !strings.Contains(out, legacy+" (legacy)") {
			t.Fatalf("config code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`config (launcher): HERDR_AGENTS_LAYOUT is read as HERDR_SOHO_LAYOUT before the layers load`, func(t *testing.T) { // JS: "config (launcher): HERDR_AGENTS_LAYOUT is read as HERDR_SOHO_LAYOUT before the layers load"
		env, repo := tm8CommandFixture(t)
		env["HERDR_AGENTS_LAYOUT"] = "columns"
		code, out, errOut := runIn(t, []string{"config"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "layout") || !strings.Contains(out, "columns") {
			t.Fatalf("config code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`config set --user (launcher): the first write copies the legacy user file (content and mode), warns, keeps the legacy intact, and does not copy again`, func(t *testing.T) { // JS: "config set --user (launcher): the first write copies the legacy user file (content and mode), warns, keeps the legacy intact, and does not copy again"
		env, repo := tm8CommandFixture(t)
		legacy := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-agents", "config")
		current := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho", "config")
		old := []byte("# keep this comment\nherd_label=crew\n")
		tm8File(t, legacy, string(old))
		if err := os.Chmod(legacy, 0o640); err != nil {
			t.Fatal(err)
		}
		code, _, errOut := runIn(t, []string{"config", "set", "layout", "tab", "--user"}, env, repo)
		got, readErr := os.ReadFile(current)
		if code != 0 || readErr != nil || string(got) != string(old)+"layout=tab\n" || !strings.Contains(errOut, "copied legacy config "+legacy+" to "+current) {
			t.Fatalf("first set code=%d file=%q readErr=%v err=%q", code, got, readErr, errOut)
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(current)
			if err != nil || info.Mode().Perm() != 0o640 {
				t.Fatalf("copied mode=%v err=%v", info, err)
			}
		}
		code, _, errOut = runIn(t, []string{"config", "set", "layout", "tab", "--user"}, env, repo)
		still, oldErr := os.ReadFile(legacy)
		if code != 0 || strings.Contains(errOut, "copied legacy config") || oldErr != nil || string(still) != string(old) {
			t.Fatalf("second set code=%d err=%q legacy=%q readErr=%v", code, errOut, still, oldErr)
		}
	})
	t.Run(`doctor --fix (launcher): without --panes a legacy-only project reads panes from the legacy file and migrates it`, func(t *testing.T) { // JS: "doctor --fix (launcher): without --panes a legacy-only project reads panes from the legacy file and migrates it"
		env, repo := tm8CommandFixture(t)
		oldPath, newPath := filepath.Join(repo, ".agents", "herdr-agents.conf"), filepath.Join(repo, ".agents", "herdr-soho.conf")
		tm8File(t, oldPath, "panes=3\nherd_label=crew\n")
		code, _, errOut := runIn(t, []string{"doctor", "--fix"}, env, repo)
		got, readErr := os.ReadFile(newPath)
		legacy, oldErr := os.ReadFile(oldPath)
		if code != 0 || readErr != nil || !strings.Contains(string(got), "panes=3\n") || !strings.Contains(errOut, "copied legacy config "+oldPath+" to "+newPath) || oldErr != nil || string(legacy) != "panes=3\nherd_label=crew\n" {
			t.Fatalf("doctor fix code=%d new=%q readErr=%v legacy=%q oldErr=%v err=%q", code, got, readErr, legacy, oldErr, errOut)
		}
	})
	t.Run(`setup (launcher): a team choice in the legacy project file counts, so no config prompt`, func(t *testing.T) { // JS: "setup (launcher): a team choice in the legacy project file counts, so no config prompt"
		env, repo := tm8CommandFixture(t)
		old := filepath.Join(repo, ".agents", "herdr-agents.conf")
		tm8File(t, old, "lane.build.kind=codex\n")
		code, out, errOut := runIn(t, []string{"setup", "--no-hooks"}, env, repo)
		if code != 0 || strings.Contains(errOut, "sets neither multi_role") {
			t.Fatalf("setup with legacy team code=%d out=%q err=%q", code, out, errOut)
		}
		if _, err := os.Stat(filepath.Join(repo, ".agents", "herdr-soho.conf")); !os.IsNotExist(err) {
			t.Fatalf("setup without --panes created new project config: %v", err)
		}
	})
	t.Run(`config set (launcher): a symlinked legacy config file is copied through the link on the first write`, func(t *testing.T) { // JS: "config set (launcher): a symlinked legacy config file is copied through the link on the first write"
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation may require elevated privileges on Windows")
		}
		env, repo := tm8CommandFixture(t)
		legacy := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-agents", "config")
		current := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho", "config")
		target := filepath.Join(filepath.Dir(legacy), "target")
		tm8File(t, target, "herd_label=crew\n")
		if err := os.Symlink(target, legacy); err != nil {
			t.Fatal(err)
		}
		code, _, errOut := runIn(t, []string{"config", "set", "layout", "tab", "--user"}, env, repo)
		link, linkErr := os.Readlink(legacy)
		data, readErr := os.ReadFile(current)
		if code != 0 || !strings.Contains(errOut, "copied legacy config "+legacy+" to "+current) || linkErr != nil || link != target || readErr != nil || string(data) != "herd_label=crew\nlayout=tab\n" {
			t.Fatalf("migration code=%d err=%q link=%q/%v data=%q/%v", code, errOut, link, linkErr, data, readErr)
		}
	})
	t.Run(`config (launcher): state_dir from the defaults shows the legacy directory the commands use`, func(t *testing.T) { // JS: "config (launcher): state_dir from the defaults shows the legacy directory the commands use"
		env, repo := tm8CommandFixture(t)
		legacyState := filepath.Join(repo, ".herdr-agents")
		if err := os.MkdirAll(legacyState, 0o700); err != nil {
			t.Fatal(err)
		}
		code, out, errOut := runIn(t, []string{"config"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "state_dir          .herdr-agents                  defaults") || core.DefaultStateDirName(repo) != ".herdr-agents" {
			t.Fatalf("config code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`setup + doctor (launcher): a separate CLAUDE.md that keeps the legacy block is named`, func(t *testing.T) { // JS: "setup + doctor (launcher): a separate CLAUDE.md that keeps the legacy block is named"
		env, repo := tm8CommandFixture(t)
		legacyBlock := "<!-- herdr-agents:start -->\nold\n<!-- herdr-agents:end -->\n"
		tm8File(t, filepath.Join(repo, "AGENTS.md"), "# A\n\n"+legacyBlock)
		tm8File(t, filepath.Join(repo, "CLAUDE.md"), "# C\n\n"+legacyBlock)
		code, setupOut, setupErr := runIn(t, []string{"setup", "--no-hooks"}, env, repo)
		if code != 0 {
			t.Fatalf("setup code=%d out=%q err=%q", code, setupOut, setupErr)
		}
		if !strings.Contains(setupErr, "CLAUDE.md exists separately and still has the legacy herdr-agents block") {
			t.Fatalf("setup did not name CLAUDE.md: %q", setupErr)
		}
		code, out, errOut := runIn(t, []string{"doctor"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "legacy herdr-agents instruction block in CLAUDE.md") {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
		}
		if code, _, errOut = runIn(t, []string{"setup", "--no-hooks", "--target", "CLAUDE.md"}, env, repo); code != 0 {
			t.Fatalf("CLAUDE setup code=%d err=%q", code, errOut)
		}
		code, out, errOut = runIn(t, []string{"doctor"}, env, repo)
		if code != 0 || strings.Contains(out, "legacy herdr-agents instruction block") {
			t.Fatalf("doctor after CLAUDE migration code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`doctor (launcher): without any legacy nothing new is printed`, func(t *testing.T) { // JS: "doctor (launcher): without any legacy nothing new is printed"
		env, repo := tm8CommandFixture(t)
		code, out, errOut := runIn(t, []string{"doctor"}, env, repo)
		for _, phrase := range []string{"legacy environment variable", "legacy user config", "legacy project config", "legacy state dir"} {
			if strings.Contains(out+errOut, phrase) {
				t.Fatalf("unexpected %q in doctor output: %q %q", phrase, out, errOut)
			}
		}
		if code != 0 {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`doctor (launcher): the copied env variable is named`, func(t *testing.T) { // JS: "doctor (launcher): the copied env variable is named"
		env, repo := tm8CommandFixture(t)
		env["HERDR_AGENTS_LAYOUT"] = "columns"
		code, out, errOut := runIn(t, []string{"doctor"}, env, repo)
		want := "legacy environment variable HERDR_AGENTS_LAYOUT is read as HERDR_SOHO_LAYOUT; rename it"
		if code != 0 || errOut != "" || !strings.Contains(out, want) {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`doctor (launcher): the legacy user config in use is named with the copy destination`, func(t *testing.T) { // JS: "doctor (launcher): the legacy user config in use is named with the copy destination"
		env, repo := tm8CommandFixture(t)
		oldPath := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-agents", "config")
		newPath := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho", "config")
		tm8File(t, oldPath, "herd_label=crew\n")
		code, out, errOut := runIn(t, []string{"doctor"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "legacy user config in use: "+oldPath+" (the next 'config set --user' copies it to "+newPath+")") {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`doctor (launcher): the legacy user config next to the new one is no longer read`, func(t *testing.T) { // JS: "doctor (launcher): the legacy user config next to the new one is no longer read"
		env, repo := tm8CommandFixture(t)
		oldPath := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-agents", "config")
		newPath := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho", "config")
		tm8File(t, oldPath, "herd_label=crew\n")
		tm8File(t, newPath, "herd_label=crew\n")
		code, out, errOut := runIn(t, []string{"doctor"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "legacy user config "+oldPath+" is no longer read ("+newPath+" exists); remove it once you no longer need it") {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`doctor (launcher): the legacy project config in use is named with the copy destination`, func(t *testing.T) { // JS: "doctor (launcher): the legacy project config in use is named with the copy destination"
		env, repo := tm8CommandFixture(t)
		oldPath, newPath := filepath.Join(repo, ".agents", "herdr-agents.conf"), filepath.Join(repo, ".agents", "herdr-soho.conf")
		tm8File(t, oldPath, "max_workers=5\n")
		code, out, errOut := runIn(t, []string{"doctor"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "legacy project config in use: "+oldPath+" (the next 'config set' copies it to "+newPath+")") {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`doctor (launcher): the legacy project config next to the new one is no longer read`, func(t *testing.T) { // JS: "doctor (launcher): the legacy project config next to the new one is no longer read"
		env, repo := tm8CommandFixture(t)
		oldPath, newPath := filepath.Join(repo, ".agents", "herdr-agents.conf"), filepath.Join(repo, ".agents", "herdr-soho.conf")
		tm8File(t, oldPath, "max_workers=5\n")
		tm8File(t, newPath, "max_workers=5\n")
		code, out, errOut := runIn(t, []string{"doctor"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "legacy project config "+oldPath+" is no longer read ("+newPath+" exists); remove it once you no longer need it") {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`doctor (launcher): the legacy state dir in use is named when nothing pins the state dir`, func(t *testing.T) { // JS: "doctor (launcher): the legacy state dir in use is named when nothing pins the state dir"
		env, repo := tm8CommandFixture(t)
		delete(env, "HERDR_SOHO_DIR")
		legacy := filepath.Join(repo, ".herdr-agents")
		if err := os.MkdirAll(legacy, 0o700); err != nil {
			t.Fatal(err)
		}
		code, out, errOut := runIn(t, []string{"doctor"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "legacy state dir in use: "+legacy) || !strings.Contains(out, "state dir writable: "+legacy) {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`doctor (launcher): the legacy state dir next to the new one is no longer used`, func(t *testing.T) { // JS: "doctor (launcher): the legacy state dir next to the new one is no longer used"
		env, repo := tm8CommandFixture(t)
		oldPath, newPath := filepath.Join(repo, ".herdr-agents"), filepath.Join(repo, ".herdr-soho")
		if err := os.MkdirAll(oldPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(newPath, 0o700); err != nil {
			t.Fatal(err)
		}
		code, out, errOut := runIn(t, []string{"doctor"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "legacy state dir "+oldPath+" is no longer used ("+newPath+" exists); clean it once its reports are no longer needed") {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`setup --plan (launcher): the before side seeds from the legacy project file when the new one is absent`, func(t *testing.T) { // JS: "setup --plan (launcher): the before side seeds from the legacy project file when the new one is absent"
		env, repo := tm8CommandFixture(t)
		tm8File(t, filepath.Join(repo, ".agents", "herdr-agents.conf"), "lane.build.roles=scouter,implementer\n")
		code, out, errOut := runIn(t, []string{"setup", "--plan", "--panes", "4"}, env, repo)
		if code != 0 || !strings.Contains(errOut, "lane roles in") || !strings.Contains(errOut, "are custom") || !strings.Contains(out, "  lane.build.roles     (unset) → scouter,implementer") || !strings.Contains(out, filepath.Join(repo, ".agents", "herdr-soho.conf")+"\n") {
			t.Fatalf("setup plan code=%d out=%q err=%q", code, out, errOut)
		}
		if _, err := os.Stat(filepath.Join(repo, ".agents", "herdr-soho.conf")); !os.IsNotExist(err) {
			t.Fatalf("setup plan wrote new config: %v", err)
		}
	})
}

func TestTM8ParityConfigGoldens(t *testing.T) {
	for _, tc := range []struct{ title, scenario string }{
		{`parity: config on a fresh repo`, "config-fresh"},                                                            // JS: "parity: config on a fresh repo"
		{`parity: config set creates the project file`, "config-set-fresh"},                                           // JS: "parity: config set creates the project file"
		{`parity: config set preserves comments, collapses duplicates, appends`, "config-set-rewrite"},                // JS: "parity: config set preserves comments, collapses duplicates, appends"
		{`parity: dotted keys (role/lane/model) are written`, "config-set-dotted"},                                    // JS: "parity: dotted keys (role/lane/model) are written"
		{`parity: invalid keys/values refuse with rc 2 and leave the file alone`, "config-set-invalid"},               // JS: "parity: invalid keys/values refuse with rc 2 and leave the file alone"
		{`parity: --user writes the user file, not the project file`, "config-set-user"},                              // JS: "parity: --user writes the user file, not the project file"
		{`parity: values reach the file verbatim (backslashes, no key injection)`, "config-set-verbatim"},             // JS: "parity: values reach the file verbatim (backslashes, no key injection)"
		{`parity: lane.<name>.roles validates the roles`, "config-set-lane-roles"},                                    // JS: "parity: lane.<name>.roles validates the roles"
		{`parity: session set writes the layer and config shows source session`, "session-set"},                       // JS: "parity: session set writes the layer and config shows source session"
		{`parity: session clear drops one key, then removes the layer`, "session-clear"},                              // JS: "parity: session clear drops one key, then removes the layer"
		{`parity: session errors (bad subcommand/key/value, usage) leave the file alone`, "session-errors-seeded"},    // JS: "parity: session errors (bad subcommand/key/value, usage) leave the file alone"
		{`parity: env beats session; session beats project`, "precedence"},                                            // JS: "parity: env beats session; session beats project"
		{`parity: without a resolvable workspace set refuses, config still works`, "no-workspace"},                    // JS: "parity: without a resolvable workspace set refuses, config still works"
		{`parity: roles table and role JSON (including errors)`, "roles-role"},                                        // JS: "parity: roles table and role JSON (including errors)"
		{`parity: a project role dir shadows the skill role`, "project-roles"},                                        // JS: "parity: a project role dir shadows the skill role"
		{`parity: relative state dir keeps the .gitignore entry current (state_root side effect)`, "state-gitignore"}, // JS: "parity: relative state dir keeps the .gitignore entry current (state_root side effect)"
		{`parity: bare ` + "`session`" + ` shows the empty-session message`, "session-bare-show"},                     // JS: "parity: bare `session` shows the empty-session message"
	} {
		t.Run(tc.title, func(t *testing.T) { // JS: parity-config.test.mjs title is the subtest name.
			runTM8ConfigGolden(t, tc.scenario)
		})
	}
}
