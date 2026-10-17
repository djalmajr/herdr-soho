// DJA-379 intentional contract adaptation for the frozen
// retirement-dispatch parity golden.
//
// The frozen dispatch scenarios
// (internal/testdata/legacy/parity-dispatch.json) predate the DJA-379
// report route: with the state dir outside the worker's cwd, the composed
// prompt and the report go to $TMPDIR/soho/<ws>/reports instead of the
// state dir's briefs and reports dirs. This package provides the narrow,
// exact-string adaptation that rewrites only those frozen paths so the
// four affected scenarios (dispatch-quota, dispatch-titled, run-no-wait,
// run-wait) compare against the native behavior. Seeds, unrelated fields,
// ordering and every other frozen expectation pass through byte-identical;
// the frozen golden bytes are never regenerated.
//
// ApplyDispatchMirrorEntries is the run-wait-only companion step: when the
// wait settles done, wait's mirrorReport copies the settled report, the
// composed brief and the sidecar from $TMPDIR/soho/<ws>/reports back to the
// state dir, so the run-wait final inventory carries both copies; the step
// keeps the three state-dir entries in the expected inventory, each with
// the content of its adapted tmp/soho copy.
package testutil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The exact frozen legacy paths of the dispatch scenarios' build worker,
// and their DJA-379 replacements, frozen here as inert test data in the
// <TS> placeholder form the parity normalization already uses. They are
// never executed; they exist only to be recognized and rewritten by
// ApplyDispatchReportDirOracle. The production dispatch code never derives
// these paths from these literals.
const (
	DispatchBriefOraclePrevious   = "state/ws/briefs/build-<TS>.md"
	DispatchBriefOracleNative     = "tmp/soho/ws/reports/build-<TS>.brief.md"
	DispatchReportOraclePrevious  = "state/ws/reports/build-<TS>.md"
	DispatchReportOracleNative    = "tmp/soho/ws/reports/build-<TS>.md"
	DispatchSidecarOraclePrevious = "state/ws/briefs/build-<TS>.dispatch.json"
	DispatchSidecarOracleNative   = "tmp/soho/ws/reports/build-<TS>.dispatch.json"
)

// dispatchReportDirPairs is the exact old→new DJA-379 rewrite set. No old
// path is a substring of another old path (the .current.md report pointer
// keeps its state-dir form) and no replacement reintroduces an old path, so
// the order of the passes is immaterial; the paths carry no JSON
// metacharacters, so the raw and the JSON-quoted forms coincide.
var dispatchReportDirPairs = [][2]string{
	{DispatchBriefOraclePrevious, DispatchBriefOracleNative},
	{DispatchReportOraclePrevious, DispatchReportOracleNative},
	{DispatchSidecarOraclePrevious, DispatchSidecarOracleNative},
}

// ApplyDispatchReportDirOracle applies the DJA-379 intentional contract
// adaptation to one raw retirement-dispatch scenario document: it replaces
// the exact frozen state-dir brief, report and sidecar paths with the
// $TMPDIR/soho/<ws>/reports paths everywhere they appear (the step JSONs,
// the herdr.log, the final inventory keys, the composed brief content and
// the last-report pointer). Matching is exact-string equality only: no
// prefix or regex matching, no field dropping, and every other frozen
// expectation passes through byte-identical. It reports an error when any
// of the three pairs is absent from the document, so the adaptation is
// never applied silently over a changed golden: a frozen scenario that no
// longer carries a pair fails loudly instead of passing through untouched.
func ApplyDispatchReportDirOracle(raw string) (string, error) {
	missing := []string{}
	for _, pair := range dispatchReportDirPairs {
		if !strings.Contains(raw, pair[0]) {
			missing = append(missing, pair[0])
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("DJA-379 dispatch adaptation: the golden no longer carries %s; the frozen bytes must stay untouched", strings.Join(missing, ", "))
	}
	for _, pair := range dispatchReportDirPairs {
		raw = strings.ReplaceAll(raw, pair[0], pair[1])
	}
	return raw, nil
}

// The run-wait state-dir mirror entries and the adapted tmp/soho entries
// they copy: wait's mirrorReport (internal/wait/wait.go, on a done settle)
// copies the settled report, the composed brief and the sidecar from
// $TMPDIR/soho/<ws>/reports back to the state dir's reports and briefs
// dirs, so the run-wait final inventory carries both copies. The pairs are
// frozen here as inert test data, like the DJA-379 rewrite pairs above.
var dispatchMirrorEntries = [][2]string{
	{"state/ws/reports/build-<TS>.md", "tmp/soho/ws/reports/build-<TS>.md"},
	{"state/ws/briefs/build-<TS>.md", "tmp/soho/ws/reports/build-<TS>.brief.md"},
	{"state/ws/briefs/build-<TS>.dispatch.json", "tmp/soho/ws/reports/build-<TS>.dispatch.json"},
}

// ApplyDispatchMirrorEntries is the run-wait-only step of the DJA-379
// adaptation, applied to the already pair-adapted scenario document: it
// keeps the three original state-dir entries in the expected final
// inventory, each carrying the same content as the adapted tmp/soho copy
// (they are copies). It reports an error when an adapted tmp/soho entry is
// missing, so the step is never applied silently over a document the pair
// adaptation did not produce. The document is re-marshaled, which
// canonicalizes key order and escapes; the test consumes the document only
// through json.Unmarshal, where the difference is invisible.
func ApplyDispatchMirrorEntries(adapted string) (string, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(adapted), &doc); err != nil {
		return "", fmt.Errorf("DJA-379 dispatch mirror: parse the adapted run-wait document: %v", err)
	}
	files, ok := doc["files"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("DJA-379 dispatch mirror: the run-wait document has no files inventory")
	}
	for _, entry := range dispatchMirrorEntries {
		stateKey, tmpKey := entry[0], entry[1]
		content, ok := files[tmpKey]
		if !ok {
			return "", fmt.Errorf("DJA-379 dispatch mirror: the adapted inventory is missing %s; run the pair adaptation first", tmpKey)
		}
		files[stateKey] = content
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("DJA-379 dispatch mirror: marshal the mirrored document: %v", err)
	}
	return string(out), nil
}
