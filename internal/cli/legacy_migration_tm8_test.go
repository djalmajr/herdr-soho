package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/setuptext"
)

func mustJSONQuote(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTM8LegacyMigration(t *testing.T) {
	t.Run(`before setup, the legacy SessionStart hook finds no script once herdr-agents is removed (exit 0)`, func(t *testing.T) { // JS: "before setup, the legacy SessionStart hook finds no script once herdr-agents is removed (exit 0)"
		t.Skip("fora: o caso executa o hook shell antigo contra a instalação de skill removida; esse harness de instalação não existe no comando Go")
	})
	t.Run(`herdr-soho setup renames the old block in place, replaces the old hooks, and keeps the project's own content and hooks`, func(t *testing.T) { // JS: "herdr-soho setup renames the old block in place, replaces the old hooks, and keeps the project's own content and hooks"
		env, repo := tm8CommandFixture(t)
		oldStart, oldEnd := "<!-- herdr-agents:start -->", "<!-- herdr-agents:end -->"
		newStart, newEnd := "<!-- herdr-soho:start -->", "<!-- herdr-soho:end -->"
		before := "# Project\n\nOwn intro.\n\n" + oldStart + "\nold block body\n" + oldEnd + "\n\nOwn tail.\n"
		tm8File(t, filepath.Join(repo, "AGENTS.md"), before)
		settings := `{"permissions":{"allow":["Bash(git status)"]},"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":` + mustJSONQuote(t, setuptext.LegacyHookReminder()) + `}]},{"hooks":[{"type":"command","command":"sh -c 'echo my own herdr-agents note'"}]}],"SessionStart":[{"hooks":[{"type":"command","command":` + mustJSONQuote(t, setuptext.LegacyHookDoctor()) + `}]}]}}` + "\n"
		tm8File(t, filepath.Join(repo, ".claude", "settings.json"), settings)
		code, _, errOut := runIn(t, []string{"setup"}, env, repo)
		agents, agentsErr := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
		settingsOut, settingsErr := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
		if code != 0 || agentsErr != nil || string(agents) != "# Project\n\nOwn intro.\n\n"+newStart+"\nold block body\n"+newEnd+"\n\nOwn tail.\n" {
			t.Fatalf("setup code=%d AGENTS=%q readErr=%v stderr=%q", code, agents, agentsErr, errOut)
		}
		var doc struct {
			Permissions struct {
				Allow []string `json:"allow"`
			} `json:"permissions"`
			Hooks map[string][]struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if settingsErr == nil {
			settingsErr = json.Unmarshal(settingsOut, &doc)
		}
		commands := tm8HookCommands(doc.Hooks)
		if settingsErr != nil || strings.Contains(strings.Join(commands, "\n"), setuptext.LegacyHookReminder()) || strings.Contains(strings.Join(commands, "\n"), setuptext.LegacyHookDoctor()) || !containsString(commands, setuptext.SetupHookDoctor()) || !containsString(commands, setuptext.SetupHookReminder()) || !containsString(commands, "sh -c 'echo my own herdr-agents note'") || len(doc.Permissions.Allow) != 1 || doc.Permissions.Allow[0] != "Bash(git status)" {
			t.Fatalf("setup hooks=%s err=%v permissions=%v", settingsOut, settingsErr, doc.Permissions.Allow)
		}
	})
	t.Run(`after setup, the new SessionStart hook runs the new doctor with no legacy block or hook line`, func(t *testing.T) { // JS: "after setup, the new SessionStart hook runs the new doctor with no legacy block or hook line"
		env, repo := tm8CommandFixture(t)
		if code, out, errOut := runIn(t, []string{"setup"}, env, repo); code != 0 {
			t.Fatalf("setup code=%d out=%q err=%q", code, out, errOut)
		}
		settings, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
		var doc struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if err == nil {
			err = json.Unmarshal(settings, &doc)
		}
		commands := tm8HookCommands(doc.Hooks)
		if err != nil || !containsString(commands, setuptext.SetupHookDoctor()) || containsString(commands, setuptext.LegacyHookDoctor()) {
			t.Fatalf("new hook settings=%s err=%v", settings, err)
		}
		code, out, errOut := runIn(t, []string{"doctor"}, env, repo)
		if code != 0 || errOut != "" || !strings.Contains(out, "instruction block present in AGENTS.md") || !strings.Contains(out, "Claude hooks present in .claude/settings.json") || strings.Contains(out, "legacy herdr-agents") || strings.Contains(out, "skill script not found") {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run(`setup --dry-run previews the in-place rename and refuses an unterminated legacy block, writing nothing`, func(t *testing.T) { // JS: "setup --dry-run previews the in-place rename and refuses an unterminated legacy block, writing nothing"
		env, repo := tm8CommandFixture(t)
		path := filepath.Join(repo, "AGENTS.md")
		oldStart, oldEnd := "<!-- herdr-agents:start -->", "<!-- herdr-agents:end -->"
		before := "# Project\n\n" + oldStart + "\nold block body\n" + oldEnd + "\n"
		tm8File(t, path, before)
		code, out, errOut := runIn(t, []string{"setup", "--dry-run", "--no-hooks"}, env, repo)
		actual, err := os.ReadFile(path)
		if code != 0 || errOut != "" || err != nil || string(actual) != before || !strings.Contains(out, filepath.Base(path)) || !strings.Contains(out, "old block body") || !strings.Contains(out, "herdr-soho:start") {
			t.Fatalf("dry run code=%d out=%q err=%q file=%q/%v", code, out, errOut, actual, err)
		}
		open := "# Project\n\n" + oldStart + "\nno end\n"
		if err := os.WriteFile(path, []byte(open), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, errOut = runIn(t, []string{"setup", "--dry-run", "--no-hooks"}, env, repo)
		actual, err = os.ReadFile(path)
		if code != 4 || out != "" || err != nil || string(actual) != open || !strings.Contains(errOut, "incomplete") {
			t.Fatalf("unterminated block code=%d out=%q err=%q file=%q/%v", code, out, errOut, actual, err)
		}
	})
}

func tm8HookCommands(hooks map[string][]struct {
	Hooks []struct {
		Command string `json:"command"`
	} `json:"hooks"`
}) []string {
	var commands []string
	for _, entries := range hooks {
		for _, entry := range entries {
			for _, hook := range entry.Hooks {
				commands = append(commands, hook.Command)
			}
		}
	}
	return commands
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
