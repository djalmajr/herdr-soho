package text

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ASCIILower folds only ASCII uppercase letters, matching JavaScript /i without u.
func ASCIILower(value string) string {
	bytes := []byte(value)
	for i, b := range bytes {
		if b >= 'A' && b <= 'Z' {
			bytes[i] = b + ('a' - 'A')
		}
	}
	return string(bytes)
}

// JSLower matches String.prototype.toLowerCase, including full mappings and final sigma.
func JSLower(value string) string {
	runes := []rune(value)
	var result []rune
	for i, r := range runes {
		switch r {
		case '\u0130':
			result = append(result, 'i', '\u0307')
		case '\u03a3':
			if hasCasedBefore(runes, i) && !hasCasedAfter(runes, i+1) {
				result = append(result, '\u03c2')
			} else {
				result = append(result, '\u03c3')
			}
		default:
			result = append(result, unicode.ToLower(r))
		}
	}
	return string(result)
}

// StripMarksNFD matches the dispatch heading normalization's canonical NFD
// decomposition and removal of U+0300 through U+036F.
func StripMarksNFD(value string) string {
	// U+034F (combining grapheme joiner) has class 0 and sits in the removed
	// range: in the JS it still splits the canonical ordering, because
	// normalize('NFD') orders the whole string before the replace. It is its
	// own only NFD source, so it is kept as a boundary until the ordering is
	// done; the table maps every other code point of the range to "".
	decomposed := make([]rune, 0, len(value))
	for _, r := range value {
		if r == '\u034f' {
			decomposed = append(decomposed, r)
		} else if mapped, ok := nfdMappings[r]; ok {
			decomposed = append(decomposed, []rune(mapped)...)
		} else {
			decomposed = append(decomposed, r)
		}
	}
	segmentStart := 0
	for i, r := range decomposed {
		if _, combining := nfdCombiningClasses[r]; !combining {
			sortCombiningSegment(decomposed[segmentStart:i])
			segmentStart = i + 1
		}
	}
	sortCombiningSegment(decomposed[segmentStart:])

	result := decomposed[:0]
	for _, r := range decomposed {
		if r < '\u0300' || r > '\u036f' {
			result = append(result, r)
		}
	}
	return string(result)
}

func sortCombiningSegment(runes []rune) {
	sort.SliceStable(runes, func(i, j int) bool {
		return nfdCombiningClasses[runes[i]] < nfdCombiningClasses[runes[j]]
	})
}

func hasCasedBefore(runes []rune, index int) bool {
	for i := index - 1; i >= 0; i-- {
		if isCaseIgnorable(runes[i]) {
			continue
		}
		return isCased(runes[i])
	}
	return false
}

func hasCasedAfter(runes []rune, index int) bool {
	for i := index; i < len(runes); i++ {
		if isCaseIgnorable(runes[i]) {
			continue
		}
		return isCased(runes[i])
	}
	return false
}

func isCased(r rune) bool {
	return unicode.Is(unicode.Lu, r) || unicode.Is(unicode.Ll, r) || unicode.Is(unicode.Lt, r) || unicode.Is(unicode.Other_Uppercase, r) || unicode.Is(unicode.Other_Lowercase, r)
}

func isCaseIgnorable(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Lm, r) || unicode.Is(unicode.Sk, r) || strings.ContainsRune("'\u002e\u003a\u00ad\u00b7\u0387\u055f\u05f4\u2018\u2019\u2024\u2027\ufe13\ufe52\ufe55\uff07\uff0e\uff1a", r)
}

// ToWellFormedUTF8 replaces WTF-8 encodings of lone UTF-16 surrogates with U+FFFD,
// matching Node's fs.writeFileSync(string) conversion to UTF-8.
func ToWellFormedUTF8(value string) string {
	bytes := []byte(value)
	result := make([]byte, 0, len(bytes))
	for i := 0; i < len(bytes); {
		if i+2 < len(bytes) && bytes[i] == 0xed && bytes[i+1]&0xe0 == 0xa0 && bytes[i+2]&0xc0 == 0x80 {
			result = append(result, '\xef', '\xbf', '\xbd')
			i += 3
			continue
		}
		_, size := utf8.DecodeRune(bytes[i:])
		if size == 1 && bytes[i] >= utf8.RuneSelf {
			result = append(result, '\xef', '\xbf', '\xbd')
			i++
			continue
		}
		result = append(result, bytes[i:i+size]...)
		i += size
	}
	return string(result)
}
