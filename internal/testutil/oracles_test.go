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

// TestPeerBlockDJA374ReplacesOnlyTheBullet pins the documented DJA-374
// adaptation: the exact pre-DJA-374 soho "Peer messages" bullet is replaced
// by the approved replacement and nothing else — the surrounding text and
// the rest of the block pass through byte-identical, in the raw and
// unified-diff forms the frozen goldens carry.
func TestPeerBlockDJA374ReplacesOnlyTheBullet(t *testing.T) {
	// Raw form: the exact previous bullet becomes the approved replacement;
	// the surrounding text passes through byte-identical.
	got, err := RewritePeerBlockDJA374("before\n" + PeerBlockOracleDJA374Previous + "\nafter\n")
	if err != nil {
		t.Fatalf("raw rewrite refused the previous bullet: %v", err)
	}
	if want := "before\n" + PeerBlockOracleDJA374Native + "\nafter\n"; got != want {
		t.Fatalf("raw rewrite = %q\nwant       %q", got, want)
	}
	// The whole soho block: only the bullet changes; the markers, the title
	// and every other line pass through byte-identical.
	oldBlock := strings.Replace(SohoRenameOracleBlockNative, PeerBlockOracleDJA374Native, PeerBlockOracleDJA374Previous, 1)
	if oldBlock == SohoRenameOracleBlockNative {
		t.Fatal("fixture: the soho block pin does not carry the DJA-374 bullet")
	}
	gotBlock, err := RewritePeerBlockDJA374(oldBlock)
	if err != nil {
		t.Fatalf("block rewrite refused the previous bullet: %v", err)
	}
	if gotBlock != SohoRenameOracleBlockNative {
		t.Fatalf("block rewrite changed more than the bullet:\ngot:\n%s\nwant:\n%s", gotBlock, SohoRenameOracleBlockNative)
	}
	// Unified-diff form: the frozen plan goldens carry the bullet with every
	// line prefixed by "+".
	pair := peerBlockDJA374Pair(false, "+")
	gotDiff, err := RewritePeerBlockDJA374("ctx\n" + pair[0] + "\nctx\n")
	if err != nil {
		t.Fatalf("diff rewrite refused the previous bullet: %v", err)
	}
	if want := "ctx\n" + pair[1] + "\nctx\n"; gotDiff != want {
		t.Fatalf("diff rewrite = %q\nwant         %q", gotDiff, want)
	}
}

// TestPeerBlockDJA374FailsWithoutThePreviousBullet is the negative half of
// the DJA-374 contract: a string without the exact previous soho bullet
// (already adapted, the herdr-soho generation bullet, a truncation, plain
// text) makes the step fail and passes through byte-identical — unlike the
// silent DJA-330 no-op, a step that found nothing is a test failure.
func TestPeerBlockDJA374FailsWithoutThePreviousBullet(t *testing.T) {
	cases := map[string]string{
		"already adapted":              PeerBlockOracleDJA374Native,
		"herdr-soho generation bullet": PeerBlockOracleNative,
		"truncated bullet":             strings.TrimSuffix(PeerBlockOracleDJA374Previous, " and answer with `soho send <ref> …`."),
		"plain text":                   "no peer bullet here",
	}
	for name, in := range cases {
		got, err := RewritePeerBlockDJA374(in)
		if err == nil {
			t.Fatalf("%s: expected a failure, got %q", name, got)
		}
		if got != in {
			t.Fatalf("%s: the step mutated the text:\ngot:\n%s\nin:\n%s", name, got, in)
		}
	}
}

// TestPeerBlockDJA374PlanHunk pins the documented DJA-374 adaptation of the
// frozen plan goldens' hunk count: the exact pre-DJA-374 hunk header of the
// plan's AGENTS.md block diff (the 43-line pre-DJA-374 block into the
// one-line seed) is rewritten to the post-DJA-374 header (the 46-line
// block); the rest of the plan output and every non-matching string pass
// through byte-identical.
func TestPeerBlockDJA374PlanHunk(t *testing.T) {
	out := "plan (nothing is written):\n\n<ROOT>/repo/AGENTS.md\n--- a/<ROOT>/repo/AGENTS.md\n+++ b/<ROOT>/repo/AGENTS.md\n@@ -1 +1,45 @@\n # Agent instructions\n+<!-- soho:start -->\n\n<ROOT>/repo/.claude/settings.json\n--- a/<ROOT>/repo/.claude/settings.json\n+++ b/<ROOT>/repo/.claude/settings.json\n@@ -1 +1,34 @@\n{}"
	tree := map[string]any{"steps": []any{map[string]any{"out": out, "rc": float64(0)}}}
	got := ApplyPeerBlockDJA374PlanHunk(tree).(map[string]any)["steps"].([]any)[0].(map[string]any)["out"].(string)
	want := strings.Replace(out, "@@ -1 +1,45 @@\n", "@@ -1 +1,48 @@\n", 1)
	if got != want {
		t.Fatalf("plan hunk adaptation =\n%s\nwant\n%s", got, want)
	}
	// A string without the exact header passes through byte-identical.
	noHunk := "plan (nothing is written):\n@@ -1 +1,34 @@\n{}"
	gotNo := ApplyPeerBlockDJA374PlanHunk(noHunk).(string)
	if gotNo != noHunk {
		t.Fatalf("the non-matching header was altered:\n%s", gotNo)
	}
}
