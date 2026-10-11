package testutil

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRoleOracleFrozenDataIsPinned(t *testing.T) {
	// The frozen role data is documented by SHA-256: any edit to the
	// constants without a deliberate hash update fails here.
	got := sha256Hex(RoleOracleRow) + "\n" + sha256Hex(RoleOracleRowAnchor) + "\n" + sha256Hex(RoleOracleDetectEntry) + "\n" + sha256Hex(RoleOracleDetectEntryAnchor)
	if got != RoleOracleFrozenHashes {
		t.Fatalf("frozen role oracle data drifted\nsha256:\n%s\ndocumented:\n%s", got, RoleOracleFrozenHashes)
	}
	// The inserted texts are the shipped role's table row and detect entry.
	if !strings.HasPrefix(RoleOracleRow, "job-orchestrator   claude   opus ") || !strings.Contains(RoleOracleRow, "file: <SKILL>/roles/job-orchestrator.md\n") {
		t.Fatalf("table row literal changed: %q", RoleOracleRow)
	}
	if !strings.Contains(RoleOracleDetectEntry, "\"key\": \"role.job-orchestrator.kind\"") || !strings.Contains(RoleOracleDetectEntry, "\"value\": \"claude\"") || !strings.Contains(RoleOracleDetectEntry, "\"source\": \"role\"") {
		t.Fatalf("detect entry literal changed: %q", RoleOracleDetectEntry)
	}
}

func TestRoleOracleRowInsertsAtExactSortedPosition(t *testing.T) {
	// The table fragment is the sorted tail of the real `roles` output:
	// header, the anchor inspector row, and the planner row. The insertion
	// must land between the anchor and the planner row and change no other
	// byte.
	header := "ROLE               KIND     MODEL                    EFFORT   MODE       FROM\n"
	planner := "planner            claude   fable                    high     read-only  kind: role config (defaults); effort: role file; file: <SKILL>/roles/planner.md\n"
	in := header + RoleOracleRowAnchor + planner
	want := header + RoleOracleRowAnchor + RoleOracleRow + planner
	if got := ApplyRoleOracleToRolesTable(in); got != want {
		t.Fatalf("table adaptation =\n%q\nwant\n%q", got, want)
	}
	if n := strings.Count(want, RoleOracleRow); n != 1 {
		t.Fatalf("adapted table carries %d inserted rows, want 1", n)
	}
}

func TestRoleOracleDetectEntryInsertsAtExactSortedPosition(t *testing.T) {
	// The document fragment is the tail of a real role_kinds array: the
	// designer entry, the anchor inspector entry, and the planner entry.
	// The insertion must land between the anchor and the planner entry and
	// change no other byte.
	designer := "      {\n        \"key\": \"role.designer.kind\",\n        \"value\": \"agy\",\n        \"source\": \"role\"\n      },\n"
	planner := "      {\n        \"key\": \"role.planner.kind\",\n        \"value\": \"claude\",\n        \"source\": \"role\"\n      },\n"
	researcher := "      {\n        \"key\": \"role.researcher.kind\",\n        \"value\": \"grok\",\n        \"source\": \"role\"\n      }\n"
	doc := "{\"config\":{\"role_kinds\":[\n" + designer + RoleOracleDetectEntryAnchor + planner + researcher + "    ]}}\n"
	want := "{\"config\":{\"role_kinds\":[\n" + designer + RoleOracleDetectEntryAnchor + RoleOracleDetectEntry + planner + researcher + "    ]}}\n"
	got := ApplyRoleOracleToDetectDocument(doc)
	if got != want {
		t.Fatalf("document adaptation =\n%s\nwant\n%s", got, want)
	}
	// The adapted document is still valid JSON with the entry at the exact
	// sorted position.
	var parsed struct {
		Config struct {
			RoleKinds []struct {
				Key string `json:"key"`
			} `json:"role_kinds"`
		} `json:"config"`
	}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("adapted document is not valid JSON: %v", err)
	}
	keys := make([]string, len(parsed.Config.RoleKinds))
	for i, row := range parsed.Config.RoleKinds {
		keys[i] = row.Key
	}
	wantKeys := []string{"role.designer.kind", "role.inspector.kind", "role.job-orchestrator.kind", "role.planner.kind", "role.researcher.kind"}
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("adapted key order = %v, want %v", keys, wantKeys)
	}
}

func TestRoleOracleRefusesMissingAndDuplicatedAnchors(t *testing.T) {
	// Inputs without the anchor are returned unchanged, so the caller test
	// fails on the real difference instead of a silently wrong insertion.
	noAnchorTable := "ROLE               KIND     MODEL                    EFFORT   MODE       FROM\nplanner            claude   fable                    high     read-only  kind: role config (defaults); effort: role file; file: <SKILL>/roles/planner.md\n"
	if got := ApplyRoleOracleToRolesTable(noAnchorTable); got != noAnchorTable {
		t.Fatalf("anchor-less table changed: %q", got)
	}
	// A duplicated anchor is ambiguous: refuse.
	twiceAnchorTable := noAnchorTable + RoleOracleRowAnchor + RoleOracleRowAnchor
	if got := ApplyRoleOracleToRolesTable(twiceAnchorTable); got != twiceAnchorTable {
		t.Fatalf("duplicated-anchor table changed: %q", got)
	}
	noAnchorDoc := "{\"config\":{\"role_kinds\":[\n" + "      {\n        \"key\": \"role.planner.kind\",\n        \"value\": \"claude\",\n        \"source\": \"role\"\n      },\n" + "    ]}}\n"
	if got := ApplyRoleOracleToDetectDocument(noAnchorDoc); got != noAnchorDoc {
		t.Fatalf("anchor-less document changed: %s", got)
	}
	twiceAnchorDoc := "{\"config\":{\"role_kinds\":[\n" + RoleOracleDetectEntryAnchor + RoleOracleDetectEntryAnchor + "    ]}}\n"
	if got := ApplyRoleOracleToDetectDocument(twiceAnchorDoc); got != twiceAnchorDoc {
		t.Fatalf("duplicated-anchor document changed: %s", got)
	}
	// Non-role outputs pass through unchanged.
	for _, s := range []string{"", "role implementer not found\n"} {
		if got := ApplyRoleOracleToRolesTable(s); got != s {
			t.Fatalf("table %q changed: %q", s, got)
		}
	}
}

func TestRoleOracleSecondApplicationIsRefused(t *testing.T) {
	// Applying the adaptation twice is not idempotent-silent: the second
	// application on already-adapted text is refused, so the row is never
	// doubled.
	in := "ROLE               KIND     MODEL                    EFFORT   MODE       FROM\n" + RoleOracleRowAnchor + "planner            claude   fable                    high     read-only  kind: role config (defaults); effort: role file; file: <SKILL>/roles/planner.md\n"
	once := ApplyRoleOracleToRolesTable(in)
	if n := strings.Count(once, RoleOracleRow); n != 1 {
		t.Fatalf("first application inserted %d rows, want 1", n)
	}
	twice := ApplyRoleOracleToRolesTable(once)
	if twice != once {
		t.Fatalf("second application changed the output:\nonce  %s\ntwice %s", once, twice)
	}
	if n := strings.Count(twice, RoleOracleRow); n != 1 {
		t.Fatalf("second application left %d rows, want 1", n)
	}
	doc := "{\"config\":{\"role_kinds\":[\n" + RoleOracleDetectEntryAnchor + "      {\n        \"key\": \"role.planner.kind\",\n        \"value\": \"claude\",\n        \"source\": \"role\"\n      },\n      {\n        \"key\": \"role.researcher.kind\",\n        \"value\": \"grok\",\n        \"source\": \"role\"\n      }\n" + "    ]}}\n"
	onceDoc := ApplyRoleOracleToDetectDocument(doc)
	if n := strings.Count(onceDoc, RoleOracleDetectEntry); n != 1 {
		t.Fatalf("first document application inserted %d entries, want 1", n)
	}
	if twiceDoc := ApplyRoleOracleToDetectDocument(onceDoc); twiceDoc != onceDoc {
		t.Fatalf("second document application changed the output")
	}
}

func TestRoleOracleLanesOffDataIsPinned(t *testing.T) {
	got := sha256Hex(RoleOracleLanesOffKindsPrevious) + "\n" + sha256Hex(RoleOracleLanesOffKindsAdapted) + "\n" + sha256Hex(RoleOracleLanesOffCountPrevious) + "\n" + sha256Hex(RoleOracleLanesOffCountAdapted)
	if got != RoleOracleLanesOffHashes {
		t.Fatalf("frozen lanes-off oracle data drifted\nsha256:\n%s\ndocumented:\n%s", got, RoleOracleLanesOffHashes)
	}
}

func TestRoleOracleLanesOffDoctorReplacesExactlyTwoLines(t *testing.T) {
	head := "ok     herdr 0.9.1\n"
	tail := "first_run: true\n"
	in := head + RoleOracleLanesOffKindsPrevious + "ok     state dir writable\n" + RoleOracleLanesOffCountPrevious + tail
	want := head + RoleOracleLanesOffKindsAdapted + "ok     state dir writable\n" + RoleOracleLanesOffCountAdapted + tail
	if got := ApplyRoleOracleToLanesOffDoctor(in); got != want {
		t.Fatalf("adapted =\n%q\nwant\n%q", got, want)
	}
	// A second application is refused (unchanged).
	if got := ApplyRoleOracleToLanesOffDoctor(want); got != want {
		t.Fatalf("second application changed the text:\n%q", got)
	}
	// Missing or duplicated frozen lines leave the input unchanged.
	for _, s := range []string{
		head + RoleOracleLanesOffCountPrevious + tail,
		head + RoleOracleLanesOffKindsPrevious + tail,
		in + RoleOracleLanesOffKindsPrevious,
	} {
		if got := ApplyRoleOracleToLanesOffDoctor(s); got != s {
			t.Fatalf("changed an input it must refuse:\n%q\n->\n%q", s, got)
		}
	}
}
