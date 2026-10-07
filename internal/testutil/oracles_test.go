package testutil

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// sha256Hex returns the hex SHA-256 of s.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestHookOraclePreviousDataIsPinned(t *testing.T) {
	// The frozen previous command data is documented by SHA-256: any edit
	// to the constants without a deliberate hash update fails here.
	got := sha256Hex(HookOraclePreviousReminder) + "\n" + sha256Hex(HookOraclePreviousDoctor)
	if got != HookOraclePreviousHashes {
		t.Fatalf("frozen previous hook data drifted\nsha256:\n%s\ndocumented:\n%s", got, HookOraclePreviousHashes)
	}
	// The native literals are the approved PATH commands.
	if HookOracleNativeReminder != "herdr-soho hook reminder" || HookOracleNativeDoctor != "herdr-soho hook doctor" {
		t.Fatalf("native oracle literals changed: %q / %q", HookOracleNativeReminder, HookOracleNativeDoctor)
	}
}

func TestHookOracleReplacesExactRawCommands(t *testing.T) {
	// Both exact previous commands, raw form, are rewritten to the native
	// literals.
	got := rewriteHookString("start " + HookOraclePreviousReminder + " mid " + HookOraclePreviousDoctor + " end")
	want := "start " + HookOracleNativeReminder + " mid " + HookOracleNativeDoctor + " end"
	if got != want {
		t.Fatalf("raw rewrite = %q\nwant    %q", got, want)
	}
}

func TestHookOracleReplacesJSONQuotedCommands(t *testing.T) {
	// The JSON-quoted (escaped) form — how the command appears inside a
	// textual JSON document — is rewritten to the raw native literal.
	for _, command := range []string{HookOraclePreviousReminder, HookOraclePreviousDoctor} {
		quoted := jsonHookEscape(command)
		if !strings.Contains(quoted, `\"`) {
			t.Fatalf("escape form of a frozen command is not JSON-quoted: %s", quoted)
		}
		got := rewriteHookString("{\"command\": \"" + quoted + "\"}")
		var native string
		if command == HookOraclePreviousReminder {
			native = HookOracleNativeReminder
		} else {
			native = HookOracleNativeDoctor
		}
		want := "{\"command\": \"" + native + "\"}"
		if got != want {
			t.Fatalf("json-quoted rewrite = %s\nwant               %s", got, want)
		}
	}
}

func TestHookOracleRecursesValueTrees(t *testing.T) {
	tree := map[string]any{
		"case": "setup-fresh",
		"files": []any{
			map[string]any{"path": ".claude/settings.json", "content": HookOraclePreviousReminder},
			map[string]any{"path": "AGENTS.md", "content": "## workflow herdr-soho\nkeep me"},
		},
		"steps": []any{
			map[string]any{"out": "hooks written", "err": HookOraclePreviousDoctor, "rc": float64(0)},
		},
		"rc":   float64(0),
		"flag": true,
		"none": nil,
	}
	want := map[string]any{
		"case": "setup-fresh",
		"files": []any{
			map[string]any{"path": ".claude/settings.json", "content": HookOracleNativeReminder},
			map[string]any{"path": "AGENTS.md", "content": "## workflow herdr-soho\nkeep me"},
		},
		"steps": []any{
			map[string]any{"out": "hooks written", "err": HookOracleNativeDoctor, "rc": float64(0)},
		},
		"rc":   float64(0),
		"flag": true,
		"none": nil,
	}
	if got := ApplyHookOracle(tree); !reflect.DeepEqual(got, want) {
		t.Fatalf("value-tree adaptation mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestHookOracleNestedTextualJSONDocument(t *testing.T) {
	// A settings.json document as a string leaf: both commands appear
	// JSON-quoted once inside the document. After adaptation the document
	// is still valid JSON, carries the native commands, and every other
	// field and the key order are byte-identical.
	doc := `{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"` + jsonHookEscape(HookOraclePreviousReminder) + `"}]}],"SessionStart":[{"hooks":[{"type":"command","command":"` + jsonHookEscape(HookOraclePreviousDoctor) + `"}]}]},"permissions":{"allow":["Bash(git status)"]}}`
	got := ApplyHookOracle(doc).(string)

	wantDoc := `{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"herdr-soho hook reminder"}]}],"SessionStart":[{"hooks":[{"type":"command","command":"herdr-soho hook doctor"}]}]},"permissions":{"allow":["Bash(git status)"]}}`
	if got != wantDoc {
		t.Fatalf("document adaptation =\n%s\nwant\n%s", got, wantDoc)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("adapted document is not valid JSON: %v", err)
	}
	if _, ok := parsed["permissions"]; !ok {
		t.Fatal("adapted document lost the unrelated permissions field")
	}
	if !strings.Contains(got, "Bash(git status)") {
		t.Fatal("adapted document altered the unrelated permissions value")
	}
}

func TestHookOracleLeavesNonMatchingStringsUntouched(t *testing.T) {
	cases := map[string]string{
		// Whole-command equality only: truncations of the frozen commands
		// are user data, not the command.
		"truncated reminder": strings.TrimSuffix(HookOraclePreviousReminder, " true'"),
		"truncated doctor":   strings.TrimSuffix(HookOraclePreviousDoctor, " true'"),
		"one char changed":   strings.Replace(HookOraclePreviousReminder, "= 1 ]", "= 2 ]", 1),
		// Commands that merely mention herdr-soho are user data.
		"user command mentioning herdr-soho": "sh -c 'echo herdr-soho is running; true'",
		"user command mentioning a path":     "test -f .agents/skills/herdr-soho/scripts/herdr-soho && echo present",
		// The exact native literals are already the target: rewriting must
		// not loop or duplicate.
		"native reminder": HookOracleNativeReminder,
		"native doctor":   HookOracleNativeDoctor,
		"plain text":      "hooks written: .claude/settings.json",
	}
	for name, in := range cases {
		if got := ApplyHookOracle(in); got != in {
			t.Fatalf("%s: %q became %q", name, in, got)
		}
	}
	// Non-string scalars pass through unchanged.
	for _, v := range []any{float64(42), true, nil} {
		if got := ApplyHookOracle(v); !reflect.DeepEqual(got, v) {
			t.Fatalf("non-string %v became %v", v, got)
		}
	}
}

func TestHookOracleIsIdempotent(t *testing.T) {
	doc := "{\"a\":\"" + jsonHookEscape(HookOraclePreviousDoctor) + "\",\"b\":\"" + HookOraclePreviousReminder + "\"}"
	once := ApplyHookOracle(doc).(string)
	twice := ApplyHookOracle(once).(string)
	if once != twice {
		t.Fatalf("second application changed the output:\nonce  %s\ntwice %s", once, twice)
	}
}
