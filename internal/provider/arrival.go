package provider

import (
	"path/filepath"
	"strings"
	"unicode"
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

// boxPrefix starts a line of a full-screen TUI's own message box (opencode
// draws `┃  ` before every line of a message and wraps it inside the box).
const boxPrefix = "\u2503"

// PromptEvidence reports whether the screen shows this dispatch's prompt: the
// composed path whole on a line, or whole once the box lines of a full-screen
// TUI are joined back (opencode wraps the path inside its box, so a box line
// holds only a prefix such as `…/briefs/<agent>-`, which every brief of that
// agent shares). The fragment and queue rules of QueuedPromptEvidence apply
// only to lines outside such a box.
func PromptEvidence(screen, composed string) bool {
	if composed == "" {
		return false
	}
	if strings.Contains(screen, composed) {
		return true
	}
	var joined, outside strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n") {
		head := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(head, boxPrefix) {
			joined.WriteString(strings.TrimPrefix(head, boxPrefix))
			continue
		}
		outside.WriteString(line)
		outside.WriteString("\n")
	}
	if strings.Contains(stripSpace(joined.String()), stripSpace(composed)) {
		return true
	}
	return QueuedPromptEvidence(outside.String(), composed)
}

// stripSpace drops every whitespace rune: a box wraps at a space as well.
func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
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
