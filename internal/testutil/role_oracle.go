// Role oracle adapter for the frozen legacy parity goldens.
//
// The frozen oracles (internal/testdata/legacy) predate the shipped
// job-orchestrator role: the job contract requires shipping that role in the
// embedded skill, so the native `roles` table now carries one more row and
// the `setup --detect` and doctor/setup parity outputs carry one more
// role_kinds entry than the legacy goldens record. This file provides the
// narrow, exact-insertion adaptation for those expected outputs: the
// inserted text and its anchor are frozen literals (never derived from
// production code, so production drift fails the tests on purpose), the
// insertion lands at the exact sorted position (immediately after the
// inspector row/entry, before the planner row/entry), and every other byte
// of the expected output passes through unchanged.
package testutil

import (
	"strings"
)

// The frozen inserted and anchor texts, exactly as the native commands
// print them for the shipped skills/herdr-soho/roles/job-orchestrator.md.
// The table forms carry the skill directory in the <SKILL> form that the
// parity tests normalize to before comparing. They are inert test data:
// never executed, only recognized and inserted by the ApplyRoleOracle
// functions.
const (
	// RoleOracleRow is the `roles` table row for the shipped role.
	RoleOracleRow = "job-orchestrator   claude   opus                     high     edit       kind: role file; model: model.claude.worker (defaults); effort: role file; file: <SKILL>/roles/job-orchestrator.md\n"

	// RoleOracleRowAnchor is the exact `roles` table row immediately before
	// RoleOracleRow in the sorted table (the inspector row).
	RoleOracleRowAnchor = "inspector          agy      gemini|opus              high     read-only  kind: role file; model: model.agy.worker (defaults); effort: role file; file: <SKILL>/roles/inspector.md\n"

	// RoleOracleDetectEntry is the `setup --detect` role_kinds JSON entry
	// for the shipped role, in the golden's indentation.
	RoleOracleDetectEntry = "      {\n        \"key\": \"role.job-orchestrator.kind\",\n        \"value\": \"claude\",\n        \"source\": \"role\"\n      },\n"

	// RoleOracleDetectEntryAnchor is the exact role_kinds JSON entry
	// immediately before RoleOracleDetectEntry in the sorted array (the
	// inspector entry).
	RoleOracleDetectEntryAnchor = "      {\n        \"key\": \"role.inspector.kind\",\n        \"value\": \"agy\",\n        \"source\": \"role\"\n      },\n"
)

// RoleOracleFrozenHashes is the documented SHA-256 of the frozen role data
// above (hex, table row, table anchor, detect entry, detect entry anchor).
// TestRoleOracleFrozenDataIsPinned re-derives all four hashes and fails on
// any drift.
const RoleOracleFrozenHashes = "ddb3817fc6c21c7977e36fc57ddfe1ac89dba02f15f778b89adcb541c8003a69\na2f34441238dcfe301b003f45477e34554d54e02b7604bc3f58e87e2b0223ef0\ndac4296c1e8a1673111327a310b0a976e398de45e9e26fcbd0d7e4499005454b\nfca861c375b5b6016dfb915278a5a575476716ca03f3065f4c16fe863ea0291f"

// ApplyRoleOracleToRolesTable inserts RoleOracleRow into an expected
// `roles` table output at the exact sorted position: immediately after the
// anchor inspector row (RoleOracleRowAnchor) and before the planner row.
// It returns the input unchanged when the anchor row is absent or present
// more than once, or when RoleOracleRow is already present (a second
// application on adapted text is refused, never doubled). Every other byte
// passes through unchanged.
func ApplyRoleOracleToRolesTable(table string) string {
	return insertRoleOracle(table, RoleOracleRowAnchor, RoleOracleRow)
}

// ApplyRoleOracleToDetectDocument inserts RoleOracleDetectEntry into an
// expected `setup --detect` (or any role_kinds JSON) document at the exact
// sorted position: immediately after the anchor inspector entry
// (RoleOracleDetectEntryAnchor) and before the planner entry. The refusal
// and byte-preservation rules are the same as
// ApplyRoleOracleToRolesTable.
func ApplyRoleOracleToDetectDocument(doc string) string {
	return insertRoleOracle(doc, RoleOracleDetectEntryAnchor, RoleOracleDetectEntry)
}

// insertRoleOracle inserts row after the single anchor occurrence and
// refuses (returning the input unchanged) when the anchor is absent,
// duplicated, or the row is already present.
func insertRoleOracle(s, anchor, row string) string {
	if strings.Contains(s, row) {
		return s
	}
	if n := strings.Count(s, anchor); n != 1 {
		return s
	}
	at := strings.Index(s, anchor) + len(anchor)
	return s[:at] + row + s[at:]
}

// The lanes-off doctor adaptation. With lanes=off the doctor counts the kind
// of every installed role (except documenter and planner), so the shipped
// job-orchestrator role adds its kind (claude) to "kinds in use"; in the
// frozen kinds-lanes-off fixture only the fake grok is on the PATH, so one
// ok line becomes a warning and the summary count moves by one. This cannot
// be an insertion: it is an exact replacement of those two whole lines,
// applied only to that scenario's expected doctor output.
const (
	// RoleOracleLanesOffKindsPrevious is the frozen golden line.
	RoleOracleLanesOffKindsPrevious = "ok     kinds in use and installed: grok\n"
	// RoleOracleLanesOffKindsAdapted is the line with the shipped role.
	RoleOracleLanesOffKindsAdapted = "warn   kinds in use but not in PATH: claude (roles or lanes using them will fail to start)\n"
	// RoleOracleLanesOffCountPrevious is the frozen golden summary line.
	RoleOracleLanesOffCountPrevious = "8 ok, 6 warning(s)\n"
	// RoleOracleLanesOffCountAdapted is the summary with the shipped role.
	RoleOracleLanesOffCountAdapted = "7 ok, 7 warning(s)\n"
)

// RoleOracleLanesOffHashes is the documented SHA-256 of the four lanes-off
// literals above (hex, in declaration order); TestRoleOracleLanesOffDataIsPinned
// re-derives them and fails on any drift.
const RoleOracleLanesOffHashes = "ee412a551f93941ddbbff019887f11e260dc9e8965583ccfd7986e3f6b03a45a\nbe1a847234fa3b4a39d05411cc997a343447ba0289abbb3093fac37fa3210ffa\n2e618705bc3e237efa6b02b13f342a2790961560cff5bc505face8ca63540edf\nb7220150c13447904d844edd8dd9f2233759595c9c1d28a9f9b85cecb7c59085"

// ApplyRoleOracleToLanesOffDoctor replaces, in the expected kinds-lanes-off
// doctor output, each frozen line with its adapted line. It returns the
// input unchanged unless both frozen lines occur exactly once and neither
// adapted line is present, so a second application or an unrelated output
// is never changed. Every other byte passes through unchanged.
func ApplyRoleOracleToLanesOffDoctor(out string) string {
	if strings.Contains(out, RoleOracleLanesOffKindsAdapted) || strings.Contains(out, RoleOracleLanesOffCountAdapted) {
		return out
	}
	if strings.Count(out, RoleOracleLanesOffKindsPrevious) != 1 || strings.Count(out, RoleOracleLanesOffCountPrevious) != 1 {
		return out
	}
	out = strings.Replace(out, RoleOracleLanesOffKindsPrevious, RoleOracleLanesOffKindsAdapted, 1)
	return strings.Replace(out, RoleOracleLanesOffCountPrevious, RoleOracleLanesOffCountAdapted, 1)
}
