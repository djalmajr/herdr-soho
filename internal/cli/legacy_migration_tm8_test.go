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
		if code != 0 || errOut != "" || !strings.Contains(out, "instruction block present in AGENTS.md") || !strings.Contains(out, "Claude hooks present in .claude/settings.json") || strings.Contains(out, "skill script not found") {
			t.Fatalf("doctor code=%d out=%q err=%q", code, out, errOut)
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
