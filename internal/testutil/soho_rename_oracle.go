// Oracle for the DJA-318 herdr-soho -> soho setup-text rename over the
// frozen legacy parity goldens.
//
// The frozen goldens (internal/testdata/legacy: setuptext.json and the
// setup, plan, doctor and probe goldens) predate the setup-text rename:
// their expected setup block keeps the herdr-soho markers, the herdr-soho
// skill and command names and the herdr-soho peer marker, their expected
// hook outputs carry the exact pre-rename herdr-soho native PATH commands,
// and their expected doctor lines name the herdr-soho block and hooks. The
// Go implementation now writes the soho block, the 'soho hook reminder' and
// 'soho hook doctor' commands and the soho doctor lines. This package
// provides the narrow, exact-string adaptation that rewrites only those
// frozen setup-text strings to the soho contract so the frozen
// expectations compare against the soho behavior. Seeds, the frozen
// pre-native shell commands (adapted by ApplyHookOracle to the pre-rename
// literals, which this oracle then renames), unrelated fields, ordering
// and every other frozen expectation pass through byte-identical.
package testutil

import (
	"strings"
)

// SohoRenameOracleBlockPrevious is the exact standard setup block of the
// herdr-soho generation as the frozen goldens carry it after the DJA-330
// peer-bullet adaptation (ApplyPeerBlockOracle has already run before this
// oracle at every consumer). It is frozen here as inert test data,
// independently of the production setuptext package: if the production
// block drifts from the soho contract, the differential comparisons fail
// on purpose.
const SohoRenameOracleBlockPrevious = "<!-- herdr-soho:start -->\n## Multi-agent workflow (herdr-soho)\n\nInside Herdr (`HERDR_ENV=1`) non-trivial work in this project runs through\nthe `herdr-soho` skill. The calling agent is the **orchestrator**: it\ndecomposes the objective, writes one brief per slice, spawns role workers in\nsibling panes, waits on their report files, integrates, runs the gates and\nowns git. Load the skill (`/herdr-soho`) before planning such work.\n\n- **Delegate**: multi-file slices, UI under the design contract, anything\n  touching auth, secrets or input handling, work that parallelizes, any change\n  that needs a reviewer, and **research**: reading more than a handful of\n  files, another repository or several tools' conventions goes to a worker.\n  The orchestrator briefs it, reads the report and decides.\n- **Keep**: a one-or-two-file change with no product decision, docs, config,\n  a question, a quick verification. If writing the brief takes longer than the\n  change, make the change.\n- **Briefs are contracts**: goal, expected result, acceptance criteria with\n  the command that proves each one, decisions already made, owned and\n  forbidden files, report format. Workers never invent names, flags,\n  endpoints, credentials or requirements; what the brief leaves open comes\n  back as an open question and is answered in the next brief.\n- Workers never commit, push or open PRs; the orchestrator owns git.\n- The orchestrator is the planner. `spawn planner` opens no pane.\n- Every code slice gets a reviewer from another model family before push,\n  including code the orchestrator wrote itself.\n- The only completion signal is the worker's report file (`dispatch`,\n  `wait`, `status`); never poll agent state by hand. A busy worker is not a\n  reason for another pane: `wait`, then dispatch.\n- Quota (exit 11) stops that worker. Ask the user before switching the\n  assistant, waiting, taking the slice, or pausing.\n- How many panes, which assistant and model run each role, and their effort\n  come from the configuration (`.agents/herdr-soho.conf`, the user file,\n  the session layer), not from this block: the skill's `explain` and\n  `config` commands show what is in effect. Project roles override the\n  skill's in `.agents/herdr-roles/<role>.md`; scratch state lives in\n  `.herdr-soho/` (git-ignored).\n- **Peer messages**: text that starts with `[herdr-soho:peer]` is a message\n  from another agent of this team, working for the same owner: act on it\n  within your task and its limits, and answer with `herdr-soho send <ref> …`.\n- Refresh this block and the hooks by loading `/herdr-soho` and running its\n  `setup` command from the project root.\n<!-- herdr-soho:end -->\n"

// SohoRenameOracleBlockNative is the exact standard setup block of the soho
// generation: the soho markers, the "## Multi-agent workflow (soho)" title,
// /soho, .agents/soho.conf, .soho/, [soho:peer] and soho send. Frozen as
// inert test data like the previous block.
const SohoRenameOracleBlockNative = "<!-- soho:start -->\n## Multi-agent workflow (soho)\n\nInside Herdr (`HERDR_ENV=1`) non-trivial work in this project runs through\nthe `soho` skill. The calling agent is the **orchestrator**: it\ndecomposes the objective, writes one brief per slice, spawns role workers in\nsibling panes, waits on their report files, integrates, runs the gates and\nowns git. Load the skill (`/soho`) before planning such work.\n\n- **Delegate**: multi-file slices, UI under the design contract, anything\n  touching auth, secrets or input handling, work that parallelizes, any change\n  that needs a reviewer, and **research**: reading more than a handful of\n  files, another repository or several tools' conventions goes to a worker.\n  The orchestrator briefs it, reads the report and decides.\n- **Keep**: a one-or-two-file change with no product decision, docs, config,\n  a question, a quick verification. If writing the brief takes longer than the\n  change, make the change.\n- **Briefs are contracts**: goal, expected result, acceptance criteria with\n  the command that proves each one, decisions already made, owned and\n  forbidden files, report format. Workers never invent names, flags,\n  endpoints, credentials or requirements; what the brief leaves open comes\n  back as an open question and is answered in the next brief.\n- Workers never commit, push or open PRs; the orchestrator owns git.\n- The orchestrator is the planner. `spawn planner` opens no pane.\n- Every code slice gets a reviewer from another model family before push,\n  including code the orchestrator wrote itself.\n- The only completion signal is the worker's report file (`dispatch`,\n  `wait`, `status`); never poll agent state by hand. A busy worker is not a\n  reason for another pane: `wait`, then dispatch.\n- Quota (exit 11) stops that worker. Ask the user before switching the\n  assistant, waiting, taking the slice, or pausing.\n- How many panes, which assistant and model run each role, and their effort\n  come from the configuration (`.agents/soho.conf`, the user file,\n  the session layer), not from this block: the skill's `explain` and\n  `config` commands show what is in effect. Project roles override the\n  skill's in `.agents/herdr-roles/<role>.md`; scratch state lives in\n  `.soho/` (git-ignored).\n- **Peer messages**: text that starts with `[soho:peer]` is a message\n  from another agent of this team, working for the same owner: act on it\n  within your task and its limits, and answer with `soho send <ref> …`.\n- Refresh this block and the hooks by loading `/soho` and running its\n  `setup` command from the project root.\n<!-- soho:end -->\n"

// SohoRenameOracleBlockPreviousDiff and SohoRenameOracleBlockNativeDiff are
// the same two blocks in the setup --plan unified-diff form the frozen plan
// goldens carry: every line of the block prefixed by "+", no trailing
// newline (the diff closes the file after the end marker).
const (
	SohoRenameOracleBlockPreviousDiff = "+<!-- herdr-soho:start -->\n+## Multi-agent workflow (herdr-soho)\n+\n+Inside Herdr (`HERDR_ENV=1`) non-trivial work in this project runs through\n+the `herdr-soho` skill. The calling agent is the **orchestrator**: it\n+decomposes the objective, writes one brief per slice, spawns role workers in\n+sibling panes, waits on their report files, integrates, runs the gates and\n+owns git. Load the skill (`/herdr-soho`) before planning such work.\n+\n+- **Delegate**: multi-file slices, UI under the design contract, anything\n+  touching auth, secrets or input handling, work that parallelizes, any change\n+  that needs a reviewer, and **research**: reading more than a handful of\n+  files, another repository or several tools' conventions goes to a worker.\n+  The orchestrator briefs it, reads the report and decides.\n+- **Keep**: a one-or-two-file change with no product decision, docs, config,\n+  a question, a quick verification. If writing the brief takes longer than the\n+  change, make the change.\n+- **Briefs are contracts**: goal, expected result, acceptance criteria with\n+  the command that proves each one, decisions already made, owned and\n+  forbidden files, report format. Workers never invent names, flags,\n+  endpoints, credentials or requirements; what the brief leaves open comes\n+  back as an open question and is answered in the next brief.\n+- Workers never commit, push or open PRs; the orchestrator owns git.\n+- The orchestrator is the planner. `spawn planner` opens no pane.\n+- Every code slice gets a reviewer from another model family before push,\n+  including code the orchestrator wrote itself.\n+- The only completion signal is the worker's report file (`dispatch`,\n+  `wait`, `status`); never poll agent state by hand. A busy worker is not a\n+  reason for another pane: `wait`, then dispatch.\n+- Quota (exit 11) stops that worker. Ask the user before switching the\n+  assistant, waiting, taking the slice, or pausing.\n+- How many panes, which assistant and model run each role, and their effort\n+  come from the configuration (`.agents/herdr-soho.conf`, the user file,\n+  the session layer), not from this block: the skill's `explain` and\n+  `config` commands show what is in effect. Project roles override the\n+  skill's in `.agents/herdr-roles/<role>.md`; scratch state lives in\n+  `.herdr-soho/` (git-ignored).\n+- **Peer messages**: text that starts with `[herdr-soho:peer]` is a message\n+  from another agent of this team, working for the same owner: act on it\n+  within your task and its limits, and answer with `herdr-soho send <ref> …`.\n+- Refresh this block and the hooks by loading `/herdr-soho` and running its\n+  `setup` command from the project root.\n+<!-- herdr-soho:end -->"
	SohoRenameOracleBlockNativeDiff   = "+<!-- soho:start -->\n+## Multi-agent workflow (soho)\n+\n+Inside Herdr (`HERDR_ENV=1`) non-trivial work in this project runs through\n+the `soho` skill. The calling agent is the **orchestrator**: it\n+decomposes the objective, writes one brief per slice, spawns role workers in\n+sibling panes, waits on their report files, integrates, runs the gates and\n+owns git. Load the skill (`/soho`) before planning such work.\n+\n+- **Delegate**: multi-file slices, UI under the design contract, anything\n+  touching auth, secrets or input handling, work that parallelizes, any change\n+  that needs a reviewer, and **research**: reading more than a handful of\n+  files, another repository or several tools' conventions goes to a worker.\n+  The orchestrator briefs it, reads the report and decides.\n+- **Keep**: a one-or-two-file change with no product decision, docs, config,\n+  a question, a quick verification. If writing the brief takes longer than the\n+  change, make the change.\n+- **Briefs are contracts**: goal, expected result, acceptance criteria with\n+  the command that proves each one, decisions already made, owned and\n+  forbidden files, report format. Workers never invent names, flags,\n+  endpoints, credentials or requirements; what the brief leaves open comes\n+  back as an open question and is answered in the next brief.\n+- Workers never commit, push or open PRs; the orchestrator owns git.\n+- The orchestrator is the planner. `spawn planner` opens no pane.\n+- Every code slice gets a reviewer from another model family before push,\n+  including code the orchestrator wrote itself.\n+- The only completion signal is the worker's report file (`dispatch`,\n+  `wait`, `status`); never poll agent state by hand. A busy worker is not a\n+  reason for another pane: `wait`, then dispatch.\n+- Quota (exit 11) stops that worker. Ask the user before switching the\n+  assistant, waiting, taking the slice, or pausing.\n+- How many panes, which assistant and model run each role, and their effort\n+  come from the configuration (`.agents/soho.conf`, the user file,\n+  the session layer), not from this block: the skill's `explain` and\n+  `config` commands show what is in effect. Project roles override the\n+  skill's in `.agents/herdr-roles/<role>.md`; scratch state lives in\n+  `.soho/` (git-ignored).\n+- **Peer messages**: text that starts with `[soho:peer]` is a message\n+  from another agent of this team, working for the same owner: act on it\n+  within your task and its limits, and answer with `soho send <ref> …`.\n+- Refresh this block and the hooks by loading `/soho` and running its\n+  `setup` command from the project root.\n+<!-- soho:end -->"
)

// The exact pre-rename herdr-soho native hook commands and the approved
// soho native commands. The pre-rename literals are the output of
// ApplyHookOracle for the frozen pre-native shell commands; this oracle
// renames them to the soho contract. Frozen as inert test data.
const (
	SohoRenameHookPreviousReminder = "herdr-soho hook reminder"
	SohoRenameHookPreviousDoctor   = "herdr-soho hook doctor"
	SohoRenameHookNativeReminder   = "soho hook reminder"
	SohoRenameHookNativeDoctor     = "soho hook doctor"
)

// The exact frozen herdr-agents generation hook commands: the expected
// outputs of the legacy-reminder/legacy-doctor rows of the frozen parity
// oracle (setuptext.json). DJA-318 retires the herdr-agents generation —
// the recognized legacy generation is now the pre-rename herdr-soho native
// command — so the frozen herdr-agents literals are rewritten to exactly
// the herdr-soho native literals. The literals are inert test data: they
// are never executed.
const (
	SohoRenameLegacyAgentsReminder = "sh -c '[ \"${HERDR_ENV:-}\" = 1 ] && echo \"herdr-agents: this project routes non-trivial work through /herdr-agents — surveys go to a scouter, slices to workers; the orchestrator keeps only one-or-two-file changes.\"; true'"
	SohoRenameLegacyAgentsDoctor   = "sh -c '[ \"${HERDR_ENV:-}\" = 1 ] || exit 0; for script in \"${CLAUDE_PROJECT_DIR:-$PWD}/.agents/skills/herdr-agents/scripts/herdr-agents\" \"${CLAUDE_PROJECT_DIR:-$PWD}/.claude/skills/herdr-agents/scripts/herdr-agents\" \"$HOME/.agents/skills/herdr-agents/scripts/herdr-agents\" \"$HOME/.claude/skills/herdr-agents/scripts/herdr-agents\"; do [ -f \"$script\" ] || continue; sh \"$script\" doctor 2>/dev/null | grep -E \"^warn\" | sed \"s/^warn */herdr-agents doctor: /\"; exit 0; done; echo \"herdr-agents doctor: skill script not found\"; true'"
)

// The exact frozen doctor advisory lines (the 'PROG' token is the test
// suites' normalization of the executable path) and their soho
// replacements.
const (
	sohoRenameDoctorBlockLinePrevious = "no herdr-soho block in AGENTS.md/CLAUDE.md: run 'PROG setup' (writes the delegation rules between <!-- herdr-soho:start/end --> markers)"
	sohoRenameDoctorBlockLineNative   = "no soho block in AGENTS.md/CLAUDE.md: run 'PROG setup' (writes the delegation rules between <!-- soho:start/end --> markers)"
	sohoRenameDoctorHooksLinePrevious = "no herdr-soho hooks in .claude/settings.json: run 'PROG setup' (UserPromptSubmit reminder + SessionStart doctor)"
	sohoRenameDoctorHooksLineNative   = "no soho hooks in .claude/settings.json: run 'PROG setup' (UserPromptSubmit reminder + SessionStart doctor)"
)

// The exact frozen unified diff of the setup --plan --target case over a
// file carrying the herdr-soho block (the plan golden diffs the whole file;
// the frozen expectation shows the pre-rename behavior that normalized the
// legacy block's custom line away). DJA-318 migrates the herdr-soho block
// in place and keeps the project's line, so the expectation is rewritten to
// the exact in-place diff.
const sohoRenamePlanTargetHunkPrevious = "@@ -1,5 +1,45 @@\n # head\n <!-- herdr-soho:start -->\n-old block line\n+## Multi-agent workflow (herdr-soho)\n+\n+Inside Herdr (`HERDR_ENV=1`) non-trivial work in this project runs through\n+the `herdr-soho` skill. The calling agent is the **orchestrator**: it\n+decomposes the objective, writes one brief per slice, spawns role workers in\n+sibling panes, waits on their report files, integrates, runs the gates and\n+owns git. Load the skill (`/herdr-soho`) before planning such work.\n+\n+- **Delegate**: multi-file slices, UI under the design contract, anything\n+  touching auth, secrets or input handling, work that parallelizes, any change\n+  that needs a reviewer, and **research**: reading more than a handful of\n+  files, another repository or several tools' conventions goes to a worker.\n+  The orchestrator briefs it, reads the report and decides.\n+- **Keep**: a one-or-two-file change with no product decision, docs, config,\n+  a question, a quick verification. If writing the brief takes longer than the\n+  change, make the change.\n+- **Briefs are contracts**: goal, expected result, acceptance criteria with\n+  the command that proves each one, decisions already made, owned and\n+  forbidden files, report format. Workers never invent names, flags,\n+  endpoints, credentials or requirements; what the brief leaves open comes\n+  back as an open question and is answered in the next brief.\n+- Workers never commit, push or open PRs; the orchestrator owns git.\n+- The orchestrator is the planner. `spawn planner` opens no pane.\n+- Every code slice gets a reviewer from another model family before push,\n+  including code the orchestrator wrote itself.\n+- The only completion signal is the worker's report file (`dispatch`,\n+  `wait`, `status`); never poll agent state by hand. A busy worker is not a\n+  reason for another pane: `wait`, then dispatch.\n+- Quota (exit 11) stops that worker. Ask the user before switching the\n+  assistant, waiting, taking the slice, or pausing.\n+- How many panes, which assistant and model run each role, and their effort\n+  come from the configuration (`.agents/herdr-soho.conf`, the user file,\n+  the session layer), not from this block: the skill's `explain` and\n+  `config` commands show what is in effect. Project roles override the\n+  skill's in `.agents/herdr-roles/<role>.md`; scratch state lives in\n+  `.herdr-soho/` (git-ignored).\n+- **Peer messages**: text that starts with `[herdr-soho:peer]` is a message\n+  from another agent of this team, working for the same owner: act on it\n+  within your task and its limits, and answer with `herdr-soho send <ref> …`.\n+- Refresh this block and the hooks by loading `/herdr-soho` and running its\n+  `setup` command from the project root.\n <!-- herdr-soho:end -->\n # tail\n"

// sohoRenamePlanTargetHunkNative is the exact in-place diff the soho plan
// prints for the same frozen seed (the marker lines rename, the project's
// line stays context).
const sohoRenamePlanTargetHunkNative = "@@ -1,5 +1,5 @@\n # head\n-<!-- herdr-soho:start -->\n+<!-- soho:start -->\n old block line\n-<!-- herdr-soho:end -->\n+<!-- soho:end -->\n # tail\n"

// sohoRenamePairs rewrites the frozen setup-text strings in the exact order
// the adaptations compose: the setup --plan --target hunk first (it embeds
// the pre-rename block in diff form and would be mangled by the block
// pair), then the whole block in the diff form, then the raw block, then
// the hook literals, then the retired herdr-agents legacy literals (after
// the hook rename, so the rewritten legacy literals are not renamed again),
// and the doctor advisory lines.
var sohoRenamePairs = [][2]string{
	{sohoRenamePlanTargetHunkPrevious, sohoRenamePlanTargetHunkNative},
	{SohoRenameOracleBlockPreviousDiff, SohoRenameOracleBlockNativeDiff},
	{SohoRenameOracleBlockPrevious, SohoRenameOracleBlockNative},
	{SohoRenameHookPreviousReminder, SohoRenameHookNativeReminder},
	{SohoRenameHookPreviousDoctor, SohoRenameHookNativeDoctor},
	{SohoRenameLegacyAgentsReminder, SohoRenameHookPreviousReminder},
	{SohoRenameLegacyAgentsDoctor, SohoRenameHookPreviousDoctor},
	{sohoRenameDoctorBlockLinePrevious, sohoRenameDoctorBlockLineNative},
	{sohoRenameDoctorHooksLinePrevious, sohoRenameDoctorHooksLineNative},
}

// RewriteSohoRename applies the DJA-318 setup-text rename to one string.
// Matching is whole-segment equality of the frozen strings only: no partial
// substring matching, so a seed or a foreign command that merely mentions
// herdr-soho passes through unchanged. A string without any exact
// occurrence passes through byte-identical.
func RewriteSohoRename(s string) string {
	for _, pair := range sohoRenamePairs {
		s = strings.ReplaceAll(s, pair[0], pair[1])
	}
	return s
}

// ApplySohoRenameOracle recursively walks a decoded JSON value tree
// (objects, arrays and scalar strings) and rewrites every string through
// RewriteSohoRename. Non-string values and strings without a frozen
// setup-text segment pass through unchanged.
func ApplySohoRenameOracle(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = ApplySohoRenameOracle(item)
		}
		return v
	case []any:
		for i := range v {
			v[i] = ApplySohoRenameOracle(v[i])
		}
		return v
	case string:
		return RewriteSohoRename(v)
	default:
		return value
	}
}

const (
	sohoRenameLegacyStartMarker  = "<!-- herdr-soho:start -->"
	sohoRenameLegacyEndMarker    = "<!-- herdr-soho:end -->"
	sohoRenameRetiredStartMarker = "<!-- herdr-agents:start -->"
	sohoRenameNativeStartMarker  = "<!-- soho:start -->"
)

// SohoRenameBlockResultExpected is the documented DJA-318 adaptation of the
// frozen block-result expectations of the setuptext parity oracle
// (setuptext.json) for the two frozen seed classes the rename re-contracts:
//
//   - A seed carrying the herdr-soho block (and no soho block): the frozen
//     expectation shows the pre-rename behavior that normalized the block
//     to the standard text. The soho contract migrates the herdr-soho block
//     in place and keeps the lines the project added: the expected result
//     is the seed with the marker lines and the block's own lines renamed
//     from herdr-soho to soho (HERDR_SOHO to SOHO) and everything outside
//     the block byte-identical.
//   - A seed carrying only the retired herdr-agents block: the frozen
//     expectation shows the pre-rename herdr-agents in-place rename. The
//     soho contract no longer recognizes the herdr-agents generation, so
//     the seed is kept as a user prefix (a trailing newline is normalized
//     to one) and the standard soho block is appended after a blank line.
//
// For every other seed the pair reports ok=false and the string rewrite
// (RewriteSohoRename) applies instead. The rule is documented, not derived
// from the production SetupBlockResult; the differential test compares
// both sides, so a drift on either is caught.
func SohoRenameBlockResultExpected(input string) (string, bool) {
	if strings.Contains(input, sohoRenameNativeStartMarker) {
		return "", false // the soho block already names the current contract
	}
	if strings.Contains(input, sohoRenameLegacyStartMarker) {
		return sohoRenameInPlace(input), true
	}
	if strings.Contains(input, sohoRenameRetiredStartMarker) {
		value := input
		if value != "" && !strings.HasSuffix(value, "\n") {
			value += "\n"
		}
		return value + "\n" + SohoRenameOracleBlockNative, true
	}
	return "", false
}

// sohoRenameInPlace renames the herdr-soho block of the seed in place: the
// marker lines and every line between them rename herdr-soho to soho and
// HERDR_SOHO to SOHO; every line outside the block passes through
// byte-identical.
func sohoRenameInPlace(content string) string {
	lines := strings.Split(content, "\n")
	trailingNewline := strings.HasSuffix(content, "\n")
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}
	inside := false
	for i, line := range lines {
		if strings.Contains(line, sohoRenameLegacyStartMarker) {
			inside = true
		}
		if inside {
			line = strings.ReplaceAll(line, "herdr-soho", "soho")
			line = strings.ReplaceAll(line, "HERDR_SOHO", "SOHO")
		}
		if strings.Contains(lines[i], sohoRenameLegacyEndMarker) {
			inside = false
		}
		lines[i] = line
	}
	result := strings.Join(lines, "\n")
	if trailingNewline {
		result += "\n"
	}
	return result
}
