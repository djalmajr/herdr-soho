package plugin

import (
	"strings"
	"unicode"
)

// displayRuneWidth is the terminal-column cost of one rune. CJK
// ideographs (and their extensions), Hangul, CJK punctuation and the
// fullwidth forms take two columns; combining marks (Mn) and U+200D take
// none; everything else takes one.
func displayRuneWidth(r rune) int {
	switch {
	case r == 0x200D:
		return 0
	case unicode.Is(unicode.Mn, r):
		return 0
	case (r >= 0x1100 && r <= 0x115F) || // Hangul Jamo
		(r >= 0x2E80 && r <= 0x2FDF) || // CJK radicals and ideograph supplements
		(r >= 0x3000 && r <= 0x303F) || // CJK symbols and punctuation
		(r >= 0x3130 && r <= 0x318F) || // Hangul Compatibility Jamo
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Unified Ideographs Extension A
		(r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0xA960 && r <= 0xA97F) || // Hangul Jamo Extended-A
		(r >= 0xAC00 && r <= 0xD7A3) || // Hangul Syllables
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
		(r >= 0xFF00 && r <= 0xFFEF) || // Halfwidth and Fullwidth Forms
		(r >= 0x1F300 && r <= 0x1FAFF) || // emoji block
		(r >= 0x20000 && r <= 0x2EBEF): // CJK Unified Ideographs Extension B and later
		return 2
	default:
		return 1
	}
}

// displayWidth measures the terminal columns of text: wide characters
// count as two, combining marks and U+200D count as zero.
func displayWidth(text string) int {
	n := 0
	for _, r := range text {
		n += displayRuneWidth(r)
	}
	return n
}

// displaySlice returns a whole-rune prefix of text that fits `width`
// display columns. When the text does not fit, it ends with a single
// ellipsis (one column), so the result never exceeds width and never
// splits a rune in half.
func displaySlice(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(text) <= width {
		return text
	}
	var b strings.Builder
	used := 0
	for _, r := range text {
		w := displayRuneWidth(r)
		if used+w > width-1 { // keep one column for the ellipsis
			break
		}
		b.WriteRune(r)
		used += w
	}
	b.WriteRune('…')
	return b.String()
}
