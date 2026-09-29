package provider

import (
	"path/filepath"
	"strings"
)

const PromptMarker = "Read the file "

// LastNonEmptyLines returns the last n visible non-empty lines after CRLF normalization.
func LastNonEmptyLines(screen string, n int) []string {
	lines := strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !isJSEmpty(line) {
			kept = append(kept, line)
		}
	}
	start := len(kept) - n
	if n == 0 {
		start = 0 // JavaScript slice(-0) is slice(0).
	} else if n < 0 {
		start = -n
	}
	if start < 0 {
		start = 0
	}
	if start > len(kept) {
		start = len(kept)
	}
	return kept[start:]
}

// PromptSitsInInput detects the dispatched prompt in the last 15 non-empty screen lines.
func PromptSitsInInput(screen string) bool {
	for _, line := range LastNonEmptyLines(screen, 15) {
		if strings.Contains(line, PromptMarker) {
			return true
		}
	}
	return false
}

// QueuedPromptEvidence reports whether the screen carries the dispatched prompt in the
// agent's queue with the composed path truncated. It inspects only the last 15 non-empty
// lines: a line is proof when it contains "Read the file" and (a) carries the radical of the
// composed file (its base name without .brief.md), (b) shows at least 8 characters of a path
// prefix that is a substring of the composed path, or (c) sits on a queue chrome line that
// starts with the arrow marker or "Steering:" after its leading spaces.
func QueuedPromptEvidence(screen, composed string) bool {
	radical := strings.TrimSuffix(filepath.Base(composed), ".brief.md")
	for _, line := range LastNonEmptyLines(screen, 15) {
		if !strings.Contains(line, "Read the file") {
			continue
		}
		if strings.Contains(line, radical) {
			return true
		}
		fragment := queuedPathFragment(line)
		if len([]rune(fragment)) >= 8 && fragment != "" && strings.Contains(composed, fragment) {
			return true
		}
		if head := strings.TrimLeft(line, " \t"); strings.HasPrefix(head, "\u21b3") || strings.HasPrefix(head, "Steering:") {
			return true
		}
	}
	return false
}

// queuedPathFragment returns the text after "Read the file ", without its leading quotes
// and ellipsis, up to the next ellipsis.
func queuedPathFragment(line string) string {
	i := strings.Index(line, PromptMarker)
	if i < 0 {
		return ""
	}
	// The leading quote and ellipsis go first: Cursor shows '.../herdr-so…'.
	tail := strings.TrimLeft(line[i+len(PromptMarker):], `'"`)
	tail = strings.TrimPrefix(tail, "\u2026")
	tail = strings.TrimPrefix(tail, "...")
	cut := len(tail)
	for _, marker := range []string{"\u2026", "..."} {
		if j := strings.Index(tail, marker); j >= 0 && j < cut {
			cut = j
		}
	}
	return tail[:cut]
}

// MarkerSeq returns the second whitespace-delimited field, or an empty string.
func MarkerSeq(markerText string) string {
	fields := strings.FieldsFunc(strings.TrimFunc(markerText, isJSWhitespace), isJSWhitespace)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

// MarkerSeqChanged reports whether a known stored sequence differs from the current sequence.
func MarkerSeqChanged(markerText, curSeq string) bool {
	seq := MarkerSeq(markerText)
	return seq != "" && curSeq != "" && curSeq != seq
}
