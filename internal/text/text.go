package text

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
)

var (
	ansiKeyRE   = regexp.MustCompile(`(sk|pk|rk)_(live|test)_[A-Za-z0-9]+`)
	hyphenKeyRE = regexp.MustCompile(`(sk|pk|rk)-[A-Za-z0-9_-]{8,}`)
	bearerRE    = regexp.MustCompile(`[Bb]earer [A-Za-z0-9._~+/-]+`)
	keyValueRE  = regexp.MustCompile(`(api[_-]?key|token|secret|password)=[^ ]*`)
)

// SanitizeCause makes one printable ASCII line and truncates it to 200 UTF-16 code units.
func SanitizeCause(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s)
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			continue
		}
		if r == ' ' {
			if lastSpace {
				continue
			}
			lastSpace = true
		} else {
			lastSpace = false
		}
		b.WriteRune(r)
	}
	out := strings.Trim(b.String(), " ")
	units := utf16.Encode([]rune(out))
	if len(units) > 200 {
		units = units[:200]
	}
	return string(utf16.Decode(units))
}

// RedactSecrets applies the four JS replacement passes in their original order.
func RedactSecrets(s string) string {
	s = ansiKeyRE.ReplaceAllString(s, "[redacted]")
	s = hyphenKeyRE.ReplaceAllString(s, "[redacted]")
	s = bearerRE.ReplaceAllString(s, "Bearer [redacted]")
	return keyValueRE.ReplaceAllString(s, "$1=[redacted]")
}

// HasWord implements the bash space-delimited membership check.
func HasWord(list, word string) bool {
	return strings.Contains(" "+list+" ", " "+word+" ")
}

// CompareUTF16 compares strings by UTF-16 code units, matching JavaScript's default string order.
func CompareUTF16(a, b string) int {
	left, right := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}

// SortUTF16 sorts strings by UTF-16 code units, matching JavaScript's default string order.
func SortUTF16(values []string) {
	sort.Slice(values, func(i, j int) bool { return CompareUTF16(values[i], values[j]) < 0 })
}
