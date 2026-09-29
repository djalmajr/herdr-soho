package setuptext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/djalmajr/herdr-soho/internal/testutil"
)

func ptr(value string) *string { return &value }
func TestSetupTextCases(t *testing.T) {
	t.Run(`// JS: "setupBlock: markers, heading, no absolute path (test-setup.sh rule)"`, func(t *testing.T) {
		block := SetupBlock()
		if !strings.HasPrefix(block, SetupStart+"\n## Multi-agent workflow (herdr-soho)\n") || !strings.HasSuffix(block, SetupEnd+"\n") || strings.Contains(block, "/Users/") || strings.Contains(block, `C:\\Users\\`) {
			t.Fatal("setup block markers or portability changed")
		}
	})
	t.Run(`// JS: "setupBlock: exact text (neutral block: flow + brief contract, config for the rest)"`, func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "skills", "herdr-soho", "scripts", "test", "golden", "parity-setup.json"))
		if err != nil {
			t.Fatal(err)
		}
		var goldens map[string]struct {
			Files []struct {
				Rel     string  `json:"rel"`
				Content *string `json:"content"`
			} `json:"files"`
		}
		if err := json.Unmarshal(data, &goldens); err != nil {
			t.Fatal(err)
		}
		var want *string
		for _, file := range goldens["setup-fresh"].Files {
			if file.Rel == "repo/AGENTS.md" && file.Content != nil {
				content := strings.TrimPrefix(*file.Content, "# Agent instructions\n\n")
				want = &content
			}
		}
		if want == nil || SetupBlock() != *want {
			t.Fatalf("setup block differs from parity golden: got %q, want %v", SetupBlock(), want)
		}
	})
	t.Run("setupHookReminder: exact text, no trailing newline", func(t *testing.T) { // JS: "setupHookReminder: exact text, no trailing newline"
		if strings.HasSuffix(SetupHookReminder(), "\n") || !strings.Contains(SetupHookReminder(), "HERDR_ENV") {
			t.Fatal("reminder literal changed")
		}
	})
	t.Run("setupHookDoctor: exact text, no trailing newline", func(t *testing.T) { // JS: "setupHookDoctor: exact text, no trailing newline"
		if strings.HasSuffix(SetupHookDoctor(), "\n") || !strings.Contains(SetupHookDoctor(), "herdr-soho/scripts/herdr-soho") {
			t.Fatal("doctor hook literal changed")
		}
	})
	t.Run("setupHookDoctor runs the first available launcher from the four candidates", func(t *testing.T) { // JS: "setupHookDoctor runs the first available launcher from the four candidates"
		if _, err := exec.LookPath("sh"); err != nil {
			if runtime.GOOS == "windows" {
				t.Skip("SessionStart hook execution requires sh from Git Bash or WSL on Windows")
			}
			t.Fatalf("sh unavailable: %v", err)
		}
		root := t.TempDir()
		launcher := filepath.Join(root, ".agents", "skills", "herdr-soho", "scripts", "herdr-soho")
		if err := os.MkdirAll(filepath.Dir(launcher), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(launcher, []byte("#!/bin/sh\necho warn first-candidate\n"), 0755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", "-c", SetupHookDoctor())
		cmd.Env = append(testutil.CleanEnv(t), "HERDR_ENV=1", "CLAUDE_PROJECT_DIR="+root, "HOME="+root)
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != "herdr-soho doctor: first-candidate\n" {
			t.Fatalf("hook output=%q err=%v", output, err)
		}
	})
	t.Run("setupBlockResult: absent and empty files", func(t *testing.T) { // JS: "setupBlockResult: absent and empty files"
		if got := SetupBlockResult(nil); got == nil || *got != SetupBlock() {
			t.Fatal("absent block mismatch")
		}
		if got := SetupBlockResult(ptr("")); got == nil || *got != "\n"+SetupBlock() {
			t.Fatal("empty block mismatch")
		}
	})
	t.Run("setupBlockResult: append keeps one blank line before the block", func(t *testing.T) { // JS: "setupBlockResult: append keeps one blank line before the block"
		for _, s := range []string{"hello", "hello\n"} {
			if got := SetupBlockResult(ptr(s)); got == nil || *got != "hello\n\n"+SetupBlock() {
				t.Errorf("append %q mismatch", s)
			}
		}
	})
	t.Run("setupBlockResult: replaces the range between the markers (bash awk semantics)", func(t *testing.T) { // JS: "setupBlockResult: replaces the range between the markers (bash awk semantics)"
		cases := []struct{ in, want string }{{"before\n" + SetupStart + "\nold\n" + SetupEnd + "\nafter\n", "before\n" + SetupBlock() + "after\n"}, {"before\n" + SetupStart + "\nold\n" + SetupEnd, "before\n" + SetupBlock()}, {SetupStart + "\nx\n" + SetupEnd + "\n", SetupBlock()}}
		for _, c := range cases {
			if got := SetupBlockResult(ptr(c.in)); got == nil || *got != c.want {
				t.Errorf("replace mismatch: %v", got)
			}
		}
	})
	t.Run("setupBlockResult: no end marker truncates everything after the start line (bash behavior)", func(t *testing.T) { // JS: "setupBlockResult: no end marker truncates everything after the start line (bash behavior)"
		if got := SetupBlockResult(ptr("before\n" + SetupStart + "\nold stuff\n")); got == nil || *got != "before\n"+SetupBlock() {
			t.Fatalf("result=%v", got)
		}
	})
	t.Run("setupBlockResult: end before start is consumed; a second start repeats the block; a line holding both counts as start", func(t *testing.T) { // JS: "setupBlockResult: end before start is consumed; a second start repeats the block; a line holding both counts as start"
		cases := []struct{ in, want string }{{"x\n" + SetupEnd + "\n" + SetupStart + "\ny\n" + SetupEnd + "\n", "x\n" + SetupBlock()}, {"a\n" + SetupStart + "\n1\n" + SetupStart + "\n2\n" + SetupEnd + "\n", "a\n" + SetupBlock() + SetupBlock()}, {"a\n" + SetupStart + SetupEnd + "\nb\n", "a\n" + SetupBlock()}}
		for _, c := range cases {
			if got := SetupBlockResult(ptr(c.in)); got == nil || *got != c.want {
				t.Errorf("markers result=%v, want %q", got, c.want)
			}
		}
	})
	t.Run("setupBlockResult: legacy block is renamed in place and keeps its own text", func(t *testing.T) { // JS: "setupBlockResult: a legacy block is renamed in place and keeps its own text"
		in := "before herdr-agents\n" + LegacySetupStart + "\n## workflow herdr-agents\nHERDR_AGENTS_DIR\ncustom\n" + LegacySetupEnd + "\nafter\n"
		want := "before herdr-agents\n" + SetupStart + "\n## workflow herdr-soho\nHERDR_SOHO_DIR\ncustom\n" + SetupEnd + "\nafter\n"
		if got := SetupBlockResult(ptr(in)); got == nil || *got != want {
			t.Fatalf("legacy rename result=%v", got)
		}
		if got := SetupBlockResult(ptr(LegacySetupStart + "\nno end\n")); got != nil {
			t.Fatalf("unterminated legacy=%v", got)
		}
		both := SetupStart + "\nx\n" + SetupEnd + "\n" + LegacySetupStart + "\ny\n" + LegacySetupEnd + "\n"
		if got := SetupBlockResult(ptr(both)); got == nil || *got != SetupBlock()+SetupBlock() {
			t.Fatalf("both marker kinds result=%v", got)
		}
	})
	t.Run("setupBlockResult: a file without the legacy markers is byte-identical to today", func(t *testing.T) { // JS: "setupBlockResult: a file without the legacy markers is byte-identical to today"
		plain := "this project used to run herdr-agents panes\n"
		if got := SetupBlockResult(ptr(plain)); got == nil || *got != plain+"\n"+SetupBlock() {
			t.Fatalf("plain file result=%v", got)
		}
		if got := SetupBlockResult(ptr("x\n" + SetupStart + "\ny\n" + SetupEnd + "\n")); got == nil || *got != "x\n"+SetupBlock() {
			t.Fatalf("current markers result=%v", got)
		}
	})
	t.Run("legacy hooks: pinned sha256 and length of the exact pre-rename commands", func(t *testing.T) { // JS: "legacy hooks: pinned sha256 and length of the exact pre-rename commands"
		cases := []struct {
			s    string
			n    int
			hash string
		}{{LegacyHookReminder(), 220, "76ecb6d012d80693694393dbd61fe423132cbe1cad4e805984ca59a479e57f7e"}, {LegacyHookDoctor(), 526, "aeefff35cf1dded4818f047ede0bbd5c3cc2aa0812c378e913dc694a5ae59535"}}
		for _, c := range cases {
			sum := sha256.Sum256([]byte(c.s))
			if len(utf16.Encode([]rune(c.s))) != c.n || hex.EncodeToString(sum[:]) != c.hash {
				t.Errorf("legacy hook hash/length mismatch: %d %x", len(utf16.Encode([]rune(c.s))), sum)
			}
		}
	})
	t.Run("settingsHooksResult: absent file and {} gain both hooks, nothing else", func(t *testing.T) { // JS: "settingsHooksResult: absent file and {} gain both hooks, nothing else"
		a, b := SettingsHooksResult(nil), SettingsHooksResult(ptr("{}"))
		if a == nil || b == nil || *a != *b {
			t.Fatalf("absent={} mismatch: %v %v", a, b)
		}
	})
	t.Run("settingsHooksResult: null hooks and false event entries are replaced", func(t *testing.T) {
		for _, seed := range []string{`{"hooks":null}`, `{"hooks":{"UserPromptSubmit":false,"SessionStart":false}}`} {
			result := SettingsHooksResult(ptr(seed))
			if result == nil {
				t.Fatalf("SettingsHooksResult(%s) = nil", seed)
			}
			var document struct {
				Hooks map[string]json.RawMessage `json:"hooks"`
			}
			if err := json.Unmarshal([]byte(*result), &document); err != nil {
				t.Fatal(err)
			}
			for _, event := range []string{"UserPromptSubmit", "SessionStart"} {
				var entries []json.RawMessage
				if err := json.Unmarshal(document.Hooks[event], &entries); err != nil {
					t.Fatalf("%s: %v", event, err)
				}
				if len(entries) != 1 {
					t.Fatalf("%s entries = %d, want generated hook", event, len(entries))
				}
			}
		}
	})
	t.Run("settingsHooksResult: other hooks and keys stay, same order; new events appended", func(t *testing.T) { // JS: "settingsHooksResult: other hooks and keys stay, same order; new events appended"
		seed := `{"other":{"x":1},"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo keep-me"}]}],"SessionStart":[{"hooks":[{"type":"command","command":"bash something-else.sh"}]}]}}`
		got := SettingsHooksResult(ptr(seed))
		if got == nil {
			t.Fatal("merge refused valid settings")
		}
		parsed, err := jsonjs.Parse([]byte(*got))
		if err != nil {
			t.Fatal(err)
		}
		doc := parsed.(*jsonjs.Object)
		if !reflect.DeepEqual(doc.Keys(), []string{"other", "hooks"}) {
			t.Fatalf("top-level key order=%#v", doc.Keys())
		}
		if !strings.Contains(*got, `"other"`) || !strings.Contains(*got, `"PreToolUse"`) || !strings.Contains(*got, "echo keep-me") || !strings.HasSuffix(*got, "}\n") {
			t.Fatalf("preserved settings=%s", *got)
		}
	})
	t.Run("settingsHooksResult: current hooks are replaced and other commands are preserved", func(t *testing.T) { // JS: "settingsHooksResult: current hooks are replaced and other commands are preserved"
		seedObj, err := json.Marshal(map[string]any{"hooks": map[string]any{
			"UserPromptSubmit": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "sh -c herdr-soho setup"}}}, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": SetupHookReminder()}}}},
			"SessionStart":     []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "sh -c herdr-soho doctor"}}}, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": SetupHookDoctor()}}}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		first := SettingsHooksResult(ptr(string(seedObj)))
		if first == nil {
			t.Fatal("merge refused current hooks")
		}
		second := SettingsHooksResult(first)
		if second == nil || *second != *first {
			t.Fatalf("rerun not stable: %v / %v", first, second)
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(*first), &doc); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(*first, "sh -c herdr-soho setup") || !strings.Contains(*first, "herdr-soho: this project routes") || !strings.Contains(*first, "herdr-soho/scripts/herdr-soho") {
			t.Fatalf("unexpected merged commands: %s", *first)
		}
	})
	t.Run("settingsHooksResult: the exact legacy commands are removed like the current ones", func(t *testing.T) { // JS: "settingsHooksResult: the exact legacy commands are removed like the current ones"
		seed, err := json.Marshal(map[string]any{"hooks": map[string]any{"UserPromptSubmit": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": LegacyHookReminder()}}}, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": SetupHookReminder()}}}}, "SessionStart": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": LegacyHookDoctor()}}}, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": SetupHookDoctor()}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		first := SettingsHooksResult(ptr(string(seed)))
		if first == nil {
			t.Fatal("legacy hooks merge refused")
		}
		second := SettingsHooksResult(first)
		if second == nil || *second != *first {
			t.Fatalf("legacy hooks stacked: %v / %v", first, second)
		}
		var doc struct {
			Hooks map[string][]any `json:"hooks"`
		}
		if err := json.Unmarshal([]byte(*first), &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Hooks["UserPromptSubmit"]) != 1 || len(doc.Hooks["SessionStart"]) != 1 {
			t.Fatalf("old entries kept: %#v", doc.Hooks)
		}
	})
	t.Run("settingsHooksResult: a user command that only contains herdr-agents is kept", func(t *testing.T) { // JS: "settingsHooksResult: a user command that only contains herdr-agents is kept"
		seed, err := json.Marshal(map[string]any{"hooks": map[string]any{"UserPromptSubmit": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "sh -c 'my-herdr-agents-thing run'"}}}}, "SessionStart": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo herdr-agents legacy cleanup"}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		got := SettingsHooksResult(ptr(string(seed)))
		if got == nil || !strings.Contains(*got, "my-herdr-agents-thing run") || !strings.Contains(*got, "echo herdr-agents legacy cleanup") || !strings.Contains(*got, "herdr-soho: this project routes") || !strings.Contains(*got, "herdr-soho/scripts/herdr-soho") {
			t.Fatalf("user commands not preserved: %v", got)
		}
	})
	t.Run("settingsHooksResult: invalid or wrong-shaped documents are refused (null → die 4)", func(t *testing.T) { // JS: "settingsHooksResult: invalid or wrong-shaped documents are refused (null → die 4)"
		for _, bad := range []string{"NOT-JSON", "42", "[1, 2]", `{"hooks": [1]}`, `{"hooks": {"UserPromptSubmit": {}}}`, `{"hooks": {"UserPromptSubmit": [{"hooks": [{"type": "command", "command": 123}]}]}}`} {
			if got := SettingsHooksResult(ptr(bad)); got != nil {
				t.Errorf("accepted %s", bad)
			}
		}
	})
}

func TestSetupTextDifferential(t *testing.T) {
	type row struct {
		Kind     string  `json:"kind"`
		Input    *string `json:"input"`
		Expected *string `json:"expected"`
	}
	data, err := os.ReadFile("testdata/setuptext.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []row
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if r.Kind == "" {
				t.Fatalf("setuptext.json row %d has empty kind", i)
			}
			var got *string
			switch r.Kind {
			case "block":
				got = ptr(SetupBlock())
			case "reminder":
				got = ptr(SetupHookReminder())
			case "doctor":
				got = ptr(SetupHookDoctor())
			case "legacy-reminder":
				got = ptr(LegacyHookReminder())
			case "legacy-doctor":
				got = ptr(LegacyHookDoctor())
			case "block-result":
				got = SetupBlockResult(r.Input)
			case "hooks-result":
				got = SettingsHooksResult(r.Input)
			default:
				t.Fatalf("setuptext.json row %d has unknown kind %q", i, r.Kind)
			}
			if !reflect.DeepEqual(got, r.Expected) {
				t.Fatalf("%s = %#v JS=%#v", r.Kind, got, r.Expected)
			}
		})
	}
}
