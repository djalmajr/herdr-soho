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
// composed path whole on a line that opens with the prompt marker (or the
// recognized queue chrome before it, lineOpensWithPrompt), whole once the box
// lines of a full-screen TUI are joined back (opencode wraps the path inside
// its box, so a box line holds only a prefix such as `…/briefs/<agent>-`,
// which every brief of that agent shares; earlier steps may precede the
// prompt, and the path must reassemble, wrap by wrap, from the box line that
// opens the prompt with the marker, so a box of prose quoting the file proves
// nothing), or whole once the history lines pi wraps a consumed prompt into
// are reassembled. The path must sit whole at a path boundary
// (holdsPathBoundary for the line check, blockMatchesPath for the box and
// wrapped blocks): a longer file name that only contains the path (composed +
// "-other", an embedded same-prefix name) is not this prompt. A quoted prose
// line that merely mentions or quotes the marker — with a longer or the exact
// file name — is not a prompt or queue line and never proves the prompt. The
// queue rule of QueuedPromptEvidence applies only to lines outside the `┃` box
// and opens with the marker as well. When the screen carries pi's two
// input-box borders (PiInputRegion), every rule here — the whole-line check,
// the `┃` box join, and the queue rules — skips the lines between the borders:
// there a prompt sits typed and not sent yet, so the composed path whole on
// one box line is not arrival. Without the two borders the whole screen is
// inspected, as before.
func PromptEvidence(screen, composed string) bool {
	if composed == "" {
		return false
	}
	lines := strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n")
	boxStart, boxEnd, inBox := 0, 0, false
	if s, e, ok := PiInputRegion(screen); ok {
		boxStart, boxEnd, inBox = s, e, true
	}
	outsideRegion := func(i int) bool {
		return !inBox || i < boxStart || i >= boxEnd
	}
	// A line proves the whole path only when it opens with the prompt marker
	// (or the recognized queue chrome before it) and the path sits whole at a
	// path boundary: quoted prose that merely mentions the marker or the file
	// is not this prompt.
	for i, line := range lines {
		if outsideRegion(i) && lineOpensWithPrompt(line) && holdsPathBoundary(line, nil, composed) {
			return true
		}
	}
	var boxLines []string
	var outside strings.Builder
	for i, line := range lines {
		if !outsideRegion(i) {
			continue
		}
		head := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(head, boxPrefix) {
			boxLines = append(boxLines, strings.TrimLeft(strings.TrimPrefix(head, boxPrefix), " \t"))
			continue
		}
		outside.WriteString(line)
		outside.WriteString("\n")
	}
	// The box's own text proves the path only from a genuine anchored prompt
	// line inside the box: earlier steps may precede the prompt, but the path
	// must reassemble, wrap by wrap, from the line that opens the prompt with
	// the marker — so a box of prose quoting the exact file, or prose after an
	// unrelated anchored marker, keeps the delivery uncertain.
	boxMarker := strings.TrimRight(PromptMarker, " ")
	for k := range boxLines {
		if !lineOpensWithMarker(boxLines[k], boxMarker) {
			continue
		}
		var block strings.Builder
		for _, cont := range boxLines[k:] {
			block.WriteString(cont)
			block.WriteString("\n")
		}
		if blockMatchesPath(block.String(), PromptMarker+composed) {
			return true
		}
	}
	if wrappedBlockEvidence(screen, lines, composed) {
		return true
	}
	return QueuedPromptEvidence(outside.String(), composed)
}

// wrappedBlockEvidence reports the composed path reassembled from the lines pi
// wraps a consumed prompt into: the line that holds "Read the file" opens a
// block of up to 8 following lines, cut at the first empty line, and the
// block's original text must begin with "Read the file" plus the composed
// path, matching rune by rune while skipping only the whitespace (spaces and
// line breaks) between the pattern's runes, so pi's own wrapping does not
// defeat the match. The path must end at a word boundary: right after its
// last rune the block ends or the next original rune is whitespace, so a
// longer path on screen (`.md-later`, a radical still followed by its
// continuation) never proves the shorter composed one. A block that opens
// inside pi's input box never counts: there a prompt sits typed and not sent
// yet. Without the two box borders the block rule runs over the whole screen,
// as the `┃` box rule does today.
func wrappedBlockEvidence(screen string, lines []string, composed string) bool {
	boxStart, boxEnd, inBox := 0, 0, false
	if s, e, ok := PiInputRegion(screen); ok {
		boxStart, boxEnd, inBox = s, e, true
	}
	pattern := PromptMarker + composed
	// pi can wrap exactly after the marker's space, leaving the line as
	// "Read the file" alone; the whitespace-insensitive match below still
	// gates the proof.
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
		if blockMatchesPath(block, pattern) {
			return true
		}
	}
	return false
}

// blockMatchesPath reports whether the block's original text starts with the
// pattern: each non-whitespace rune of the pattern must meet the same rune in
// the text, and only whitespace (unicode.IsSpace, line breaks included) may
// sit between the pattern's runes. After the pattern's last rune the text
// must end, or the next original rune must be whitespace: a path that
// continues on the screen (`.md-later`, a radical still followed by `-…`)
// does not prove the shorter composed one.
func blockMatchesPath(block, pattern string) bool {
	text := []rune(block)
	pat := []rune(pattern)
	ti, pi := 0, 0
	for pi < len(pat) {
		for ti < len(text) && unicode.IsSpace(text[ti]) {
			ti++
		}
		p := pat[pi]
		pi++
		if unicode.IsSpace(p) {
			continue
		}
		if ti >= len(text) || text[ti] != p {
			return false
		}
		ti++
	}
	return ti >= len(text) || unicode.IsSpace(text[ti])
}

// isPiBorderLine reports whether the line is one of pi's input-box borders,
// ignoring whitespace: a line composed only of '─' (U+2500), or a line that,
// without its end spaces, starts with at least two '─', ends with at least
// two '─', and carries '─' in at least half of its runes — a working pi marks
// the top border with its activity indicator, like `── ⠴ Working ──…`. A
// plain text line with a '─' in the middle is not a border.
func isPiBorderLine(line string) bool {
	content := strings.TrimFunc(line, isJSWhitespace)
	if content == "" {
		return false
	}
	runes := []rune(content)
	border, plain := 0, true
	for _, r := range runes {
		if r == '─' {
			border++
		} else {
			plain = false
		}
	}
	if plain {
		return true
	}
	if border*2 < len(runes) {
		return false
	}
	return strings.HasPrefix(string(runes[:2]), "──") && strings.HasSuffix(string(runes), "──")
}

// PiInputRegion reports the line index range [start, end) of pi's input box in
// the screen's lines: the lines between the last two border lines (isPiBorderLine:
// lines of '─', or the working pi's `── ⠴ Working ──…` top border), ignoring
// whitespace (pi's borders, chat history above, footer below). ok is false when
// the screen has fewer than two such lines; the caller then keeps the
// whole-screen behavior.
func PiInputRegion(screen string) (start, end int, ok bool) {
	lines := strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n")
	borders := make([]int, 0, 2)
	for i, line := range lines {
		if isPiBorderLine(line) {
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
// lines: a line is proof when it opens with the prompt marker (or the recognized queue
// chrome before it, lineOpensWithPrompt) and shows the composed file's identity — its
// full exact basename or its radical (the base name without .brief.md) with a valid
// boundary — so the queue may clip away the leading directories and still prove the
// file. A common directory fragment of the composed path, an incomplete timestamp or
// name, a queue chrome line that carries no identity, a bare "Read the file", and a
// quoted prose line that mentions the marker or names the file — with a longer or the
// exact name — never prove the prompt: an old or unrelated queued brief keeps the
// delivery uncertain instead of closing the dispatch as queued.
func QueuedPromptEvidence(screen, composed string) bool {
	base := filepath.Base(composed)
	radical := strings.TrimSuffix(base, ".brief.md")
	if radical == "" {
		return false
	}
	for _, line := range LastNonEmptyLines(screen, 15) {
		if lineOpensWithPrompt(line) && lineShowsComposedIdentity(line, base, radical) {
			return true
		}
	}
	return false
}

// lineOpensWithMarker reports whether the line opens with marker at an
// anchored position: marker at the line start (after leading spaces and
// tabs), or right after the recognized queue chrome — the arrow marker or the
// "Steering:" label, the two queue prefixes pi shows — with marker next.
// Prose that mentions or quotes the marker later in a sentence is not a
// prompt or queue line and never proves the prompt by itself.
func lineOpensWithMarker(line, marker string) bool {
	head := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(head, marker) {
		return true
	}
	rest := ""
	switch {
	case strings.HasPrefix(head, "\u21b3"):
		rest = head[len("\u21b3"):]
	case strings.HasPrefix(head, "Steering:"):
		rest = head[len("Steering:"):]
	default:
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(rest, " \t"), marker)
}

// lineOpensWithPrompt is the anchored prompt-marker check: the marker with
// its trailing space, so the line carries the prompt text, not just the word
// sequence of the marker.
func lineOpensWithPrompt(line string) bool {
	return lineOpensWithMarker(line, PromptMarker)
}

// lineShowsComposedIdentity reports whether the line shows the composed file's identity
// in one of its whitespace fields: after the field's display clips (the quotes and
// ellipses the TUI wraps a truncated path in), the field must end with the full basename
// or with the radical alone, and the text before that
// suffix is empty only when no clip stood in front of it, or it ends at a path separator,
// so the file name is a whole path element, not a clipped or embedded part of another.
// A clipped leading directory keeps proving the file; a longer name that only contains
// the identity (build-…-final.brief.md, xbuild-…), a name clipped mid-rune, and a clip
// that lands right before the name do not.
func lineShowsComposedIdentity(line, base, radical string) bool {
	for _, field := range strings.Fields(line) {
		tail, leadClipped := stripFieldClips(field)
		for _, suffix := range []string{base, radical} {
			if !strings.HasSuffix(tail, suffix) {
				continue
			}
			before := tail[:len(tail)-len(suffix)]
			if strings.HasSuffix(before, "/") || strings.HasSuffix(before, `\`) {
				return true
			}
			if before == "" && !leadClipped {
				return true
			}
		}
	}
	return false
}

// stripFieldClips removes the leading and trailing display clips of a queue field — the
// quotes and ellipses the TUI wraps a truncated path in — and reports whether a leading
// clip was present: a leading clip right before the file name could as well have clipped
// a longer name (xbuild-…), so it never validates the boundary on its own.
func stripFieldClips(field string) (string, bool) {
	leadClipped := false
	for {
		changed := false
		if strings.HasPrefix(field, "\u2026") {
			field = strings.TrimPrefix(field, "\u2026")
			leadClipped, changed = true, true
		} else if strings.HasPrefix(field, "...") {
			field = strings.TrimPrefix(field, "...")
			leadClipped, changed = true, true
		}
		if trimmed := strings.TrimLeft(field, `'"`); trimmed != field {
			field, changed = trimmed, true
		}
		if strings.HasSuffix(field, "\u2026") {
			field = strings.TrimSuffix(field, "\u2026")
			changed = true
		} else if strings.HasSuffix(field, "...") {
			field = strings.TrimSuffix(field, "...")
			changed = true
		}
		if trimmed := strings.TrimRight(field, `'"`); trimmed != field {
			field, changed = trimmed, true
		}
		if !changed {
			return field, leadClipped
		}
	}
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
