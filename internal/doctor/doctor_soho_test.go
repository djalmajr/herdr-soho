package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/setuptext"
)

// TestDoctorSohoBlockAndHookRecognition pins the DJA-318 doctor contract:
// the soho block and the soho hooks are recognized as present, and the
// herdr-soho block and the herdr-soho hooks (the pre-rename native command
// and the previous shell commands) are recognized as the older generation
// with the guidance to run the native setup.
func TestDoctorSohoBlockAndHookRecognition(t *testing.T) {
	newBlock := setuptext.SetupStart + "\nproject text\n" + setuptext.SetupEnd + "\n"
	legacyBlock := setuptext.LegacySetupStart + "\nproject text\n" + setuptext.LegacySetupEnd + "\n"
	hookDoc := func(command string) string {
		return `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":` + quoteCommand(command) + `}]}]}}`
	}
	cases := []struct {
		name, agents, settings string
		want                   []string
		unwant                 []string
	}{
		{
			"the soho block and the soho hooks are present",
			"# A\n\n" + newBlock, hookDoc(setuptext.SetupHookDoctor()),
			[]string{"instruction block present", "ok     Claude hooks present"},
			[]string{"an older instruction block", "an older version of the hooks", "no soho hooks"},
		},
		{
			"a herdr-soho block warns for the in-place setup rename",
			"# A\n\n" + legacyBlock, hookDoc(setuptext.SetupHookDoctor()),
			[]string{"an older instruction block", "setup' to rename it in place (its text is kept)"},
			[]string{"instruction block present"},
		},
		{
			"the pre-rename herdr-soho native hooks warn for the setup replacement",
			"# A\n\n" + newBlock, hookDoc(setuptext.LegacyHookDoctor()),
			[]string{"an older version of the hooks in .claude/settings.json", "setup' to replace them"},
			[]string{"ok     Claude hooks present", "no soho hooks"},
		},
		{
			"the previous herdr-soho shell hooks warn for the setup replacement",
			"# A\n\n" + newBlock, hookDoc(setuptext.PreviousHookDoctor()),
			[]string{"old herdr-soho shell hooks in .claude/settings.json", "to replace them with the native hook commands"},
			[]string{"ok     Claude hooks present", "no soho hooks"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			env := platform.Env{"HOME": root, "SOHO_DIR": filepath.Join(root, "state"), "SOHO_SKILL_DIR": root, "PATH": "", "HERDR_ENV": "1"}
			ctx := core.Config{Entries: map[string]core.ConfigEntry{}}
			if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(tc.agents), 0o600); err != nil {
				t.Fatal(err)
			}
			settings := filepath.Join(root, ".claude", "settings.json")
			if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(settings, []byte(tc.settings), 0o600); err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			DoctorCheck(&ctx, env, root, &out)
			got := out.String()
			for _, line := range tc.want {
				if !strings.Contains(got, line) {
					t.Fatalf("expected %q in:\n%s", line, got)
				}
			}
			for _, line := range tc.unwant {
				if strings.Contains(got, line) {
					t.Fatalf("did not expect %q in:\n%s", line, got)
				}
			}
		})
	}
}
