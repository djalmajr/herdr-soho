// Oracle adapter for the frozen legacy parity goldens.
//
// The frozen oracles (internal/testdata/legacy and internal/setuptext/
// testdata) predate the native hooks: their expected hook outputs embed the
// exact pre-native herdr-soho shell commands. This package provides the
// narrow, exact-string adaptation that rewrites only those frozen command
// strings to the approved native literals so the expected outputs compare
// against the native behavior. Seeds, unrelated fields, ordering and every
// other frozen expectation pass through byte-identical.
package testutil

import (
	"strings"
)

// Approved native hook commands, frozen independently of the production
// setuptext package: the adapter must never derive expected text from the
// production SetupHook functions. If production drifts from these literals,
// the differential and TM7 oracle comparisons fail on purpose.
const (
	HookOracleNativeReminder = "herdr-soho hook reminder"
	HookOracleNativeDoctor   = "herdr-soho hook doctor"
)

// The exact pre-native herdr-soho hook commands, frozen here as inert test
// data. They are never executed; they exist only to be recognized and
// rewritten by ApplyHookOracle. The production previous-hook tests pin the
// same strings independently (setuptext_test.go literals, hash-pinned
// legacy data in setuptext.go), so drift on either side is caught.
const (
	HookOraclePreviousReminder = "sh -c '[ \"${HERDR_ENV:-}\" = 1 ] && echo \"herdr-soho: this project routes non-trivial work through /herdr-soho — surveys go to a scouter, slices to workers; the orchestrator keeps only one-or-two-file changes.\"; true'"
	HookOraclePreviousDoctor   = "sh -c '[ \"${HERDR_ENV:-}\" = 1 ] || exit 0; for script in \"${CLAUDE_PROJECT_DIR:-$PWD}/.agents/skills/herdr-soho/scripts/herdr-soho\" \"${CLAUDE_PROJECT_DIR:-$PWD}/.claude/skills/herdr-soho/scripts/herdr-soho\" \"$HOME/.agents/skills/herdr-soho/scripts/herdr-soho\" \"$HOME/.claude/skills/herdr-soho/scripts/herdr-soho\"; do [ -f \"$script\" ] || continue; sh \"$script\" doctor 2>/dev/null | grep -E \"^warn\" | sed \"s/^warn */herdr-soho doctor: /\"; exit 0; done; echo \"herdr-soho doctor: skill script not found\"; true'"
)

// HookOraclePreviousHashes is the documented SHA-256 of the frozen previous
// command data above (hex, reminder then doctor). TestHookOraclePreviousData
// re-derives both hashes from the constants and fails on any drift.
const HookOraclePreviousHashes = "48defc34fbd8210f767f41fec51290cc97e9117fda31c95e55d3085fb88926de\n04032cb1cafa53e80378456adfe44c8c015bfa577977c4a96094e2c01846f309"

// hookOracleRewrites pairs each frozen previous command with the approved
// native literal it is rewritten to.
var hookOracleRewrites = [][2]string{
	{HookOraclePreviousReminder, HookOracleNativeReminder},
	{HookOraclePreviousDoctor, HookOracleNativeDoctor},
}

// ApplyHookOracle recursively walks a decoded JSON value tree (objects,
// arrays and scalar strings) and, inside every string, replaces the exact
// previous hook commands with the approved native literals. Both the raw
// form and the JSON-quoted form (how a command appears inside nested
// textual JSON such as a settings.json document or a unified diff of one)
// are matched. Matching is whole-command equality only: no substring
// matching, no field dropping, no collapsing of unrelated output.
// Non-string values and strings without an exact command pass through
// unchanged.
func ApplyHookOracle(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = ApplyHookOracle(item)
		}
		return v
	case []any:
		for i := range v {
			v[i] = ApplyHookOracle(v[i])
		}
		return v
	case string:
		return rewriteHookString(v)
	default:
		return value
	}
}

// rewriteHookString applies the exact replacements to one string. The
// JSON-quoted forms are replaced first so the raw pass never sees a
// partially rewritten document.
func rewriteHookString(s string) string {
	for _, pair := range hookOracleRewrites {
		s = strings.ReplaceAll(s, jsonHookEscape(pair[0]), pair[1])
	}
	for _, pair := range hookOracleRewrites {
		s = strings.ReplaceAll(s, pair[0], pair[1])
	}
	return s
}

// jsonHookEscape is the JSON string-body form of command in the escape
// style of the frozen oracles (JS JSON.stringify): double quotes and
// backslashes are escaped, while < > & stay literal. The frozen commands
// contain no backslashes or control characters, so this is exact for them.
// (Go's encoding/json would additionally escape < > & and would not match
// the frozen form.)
func jsonHookEscape(command string) string {
	escaped := strings.ReplaceAll(command, `\`, `\\`)
	return strings.ReplaceAll(escaped, `"`, `\"`)
}
