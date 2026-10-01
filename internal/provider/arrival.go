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
// composed path whole on a line, whole once the box lines of a full-screen TUI
// are joined back (opencode wraps the path inside its box, so a box line holds
// only a prefix such as `…/briefs/<agent>-`, which every brief of that agent
// shares), or whole once the history lines pi wraps a consumed prompt into are
// reassembled. The fragment and queue rules of QueuedPromptEvidence apply only
// to lines outside the `┃` box.
func PromptEvidence(screen, composed string) bool {
	if composed == "" {
		return false
	}
	if strings.Contains(screen, composed) {
		return true
	}
	lines := strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n")
	var joined, outside strings.Builder
	for _, line := range lines {
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
	if wrappedBlockEvidence(screen, lines, composed) {
		return true
	}
	return QueuedPromptEvidence(outside.String(), composed)
}

// wrappedBlockEvidence reports the composed path reassembled from the lines pi
// wraps a consumed prompt into: the line that holds "Read the file" opens a
// block of up to 8 following lines, cut at the first empty line, and the block
// joined without any whitespace must begin with "Read the file" plus the
// composed path, so lines that spell the path without the marker above do not
// prove it. A block that opens inside pi's input box never counts: there a
// prompt sits typed and not sent yet. Without the two box borders the block
// rule runs over the whole screen, as the `┃` box rule does today.
func wrappedBlockEvidence(screen string, lines []string, composed string) bool {
	boxStart, boxEnd, inBox := 0, 0, false
	if s, e, ok := PiInputRegion(screen); ok {
		boxStart, boxEnd, inBox = s, e, true
	}
	prefix := stripSpace(PromptMarker + composed)
	// pi can wrap exactly after the marker's space, leaving the line as
	// "Read the file" alone; the joined prefix below still gates the proof.
	marker := strings.TrimRight(PromptMarker, " ")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), boxPrefix) {
			continue // box lines keep their own join rule
		}
		if inBox && i >= boxStart && i < boxEnd {
			continue // typed in the input box, not sent yet
		}
		if !strings.Contains(line, marker) {
			continue
		}
		block := line
		for n := 1; n <= 8 && i+n < len(lines) && !isJSEmpty(lines[i+n]); n++ {
			block += "\n" + lines[i+n]
		}
		if strings.HasPrefix(stripSpace(block), prefix) {
			return true
		}
	}
	return false
}

// PiInputRegion reports the line index range [start, end) of pi's input box in
// the screen's lines: the lines between the last two lines composed only of
// '─' (U+2500), ignoring whitespace (pi's borders, chat history above, footer
// below). ok is false when the screen has fewer than two such lines; the
// caller then keeps the whole-screen behavior.
func PiInputRegion(screen string) (start, end int, ok bool) {
	lines := strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n")
	borders := make([]int, 0, 2)
	for i, line := range lines {
		content := strings.TrimFunc(line, isJSWhitespace)
		if content == "" {
			continue
		}
		border := true
		for _, r := range content {
			if r != '─' {
				border = false
				break
			}
		}
		if border {
			borders = append(borders, i)
		}
	}
	if len(borders) < 2 {
		return 0, 0, false
	}
	return borders[len(borders)-2] + 1, borders[len(borders)-1], true
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
