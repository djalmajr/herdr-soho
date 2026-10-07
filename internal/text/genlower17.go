//go:build ignore

// Command genlower17 regenerates lowercase17_table.go from the normative
// Unicode 17.0.0 sources. It must be run with native Go only:
//
//	go run internal/text/genlower17.go <dir-with-three-sources>
//
// The sources must be the exact Unicode 17.0.0 files fetched from:
//
//	https://www.unicode.org/Public/17.0.0/ucd/UnicodeData.txt
//	https://www.unicode.org/Public/17.0.0/ucd/DerivedCoreProperties.txt
//	https://www.unicode.org/Public/17.0.0/ucd/SpecialCasing.txt
//
// The generated table is pinned by the SHA256 of the actual source bytes and
// by the version header of the files, so a regeneration from a different or
// tampered Unicode version fails here instead of silently changing behavior.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	unicodeVersion    = "17.0.0"
	unicodeDataURL    = "https://www.unicode.org/Public/17.0.0/ucd/UnicodeData.txt"
	derivedCoreURL    = "https://www.unicode.org/Public/17.0.0/ucd/DerivedCoreProperties.txt"
	specialCasingURL  = "https://www.unicode.org/Public/17.0.0/ucd/SpecialCasing.txt"
	unicodeDataFile   = "UnicodeData.txt"
	derivedCoreFile   = "DerivedCoreProperties.txt"
	specialCasingFile = "SpecialCasing.txt"
)

// sourceSHA256 pins the exact bytes of the normative sources actually used.
var sourceSHA256 = map[string]string{
	unicodeDataFile:   "2e1efc1dcb59c575eedf5ccae60f95229f706ee6d031835247d843c11d96470c",
	derivedCoreFile:   "24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08",
	specialCasingFile: "efc25faf19de21b92c1194c111c932e03d2a5eaf18194e33f1156e96de4c9588",
}

func main() {
	if len(os.Args) != 2 {
		fatal("usage: genlower17.go <dir-with-three-sources>")
	}
	dir := os.Args[1]

	sums := map[string]string{}
	for _, name := range []string{unicodeDataFile, derivedCoreFile, specialCasingFile} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			fatal("read %s: %v", name, err)
		}
		got := sha256.Sum256(raw)
		sums[name] = hex.EncodeToString(got[:])
		want := sourceSHA256[name]
		if sums[name] != want {
			fatal("source %s sha256 = %s, want pinned %s: refusing to generate from an unexpected Unicode version", name, sums[name], want)
		}
	}

	unicodeData, err := os.ReadFile(filepath.Join(dir, unicodeDataFile))
	if err != nil {
		fatal("read %s: %v", unicodeDataFile, err)
	}
	derivedCore, err := os.ReadFile(filepath.Join(dir, derivedCoreFile))
	if err != nil {
		fatal("read %s: %v", derivedCoreFile, err)
	}
	specialCasing, err := os.ReadFile(filepath.Join(dir, specialCasingFile))
	if err != nil {
		fatal("read %s: %v", specialCasingFile, err)
	}

	simpleLower, err := parseUnicodeData(unicodeData)
	if err != nil {
		fatal("parse %s: %v", unicodeDataFile, err)
	}
	fullLower, finalSigma, langSensitive, err := parseSpecialCasing(specialCasing)
	if err != nil {
		fatal("parse %s: %v", specialCasingFile, err)
	}
	casedRanges, ignorableRanges, err := parseDerivedCoreProperties(derivedCore)
	if err != nil {
		fatal("parse %s: %v", derivedCoreFile, err)
	}

	merged, overrides := buildLowercaseMap(simpleLower, fullLower)
	merged[0x03A3] = "" // final sigma is contextual and handled by JSLower.
	delete(merged, 0x03A3)

	fmt.Printf("unicode data: %d simple lowercase mappings, %d special-casing full mappings, %d overrides, %d language-sensitive entries ignored (locale-insensitive toLowerCase)\n",
		len(simpleLower), len(fullLower), len(overrides), langSensitive)
	fmt.Printf("final sigma rule: %v; cased ranges: %d; case_ignorable ranges: %d\n", finalSigma, len(casedRanges), len(ignorableRanges))
	fmt.Printf("generated table entries: %d\n", len(merged))

	out, err := render(merged, casedRanges, ignorableRanges, sums)
	if err != nil {
		fatal("render: %v", err)
	}
	here, err := os.Getwd()
	if err != nil {
		fatal("getwd: %v", err)
	}
	dst := filepath.Join(here, "lowercase17_table.go")
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		fatal("write %s: %v", dst, err)
	}
	fmt.Printf("wrote %s\n", dst)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genlower17: "+format+"\n", args...)
	os.Exit(1)
}

func hexCP(s string) (rune, bool) {
	v, err := strconv.ParseInt(s, 16, 32)
	if err != nil || v < 0 || v >= 0x110000 {
		return 0, false
	}
	return rune(v), true
}

func esc(r rune) string {
	if r >= 0x10000 {
		return fmt.Sprintf("\\U%08X", r)
	}
	return fmt.Sprintf("\\u%04X", r)
}

// parseUnicodeData reads the header-less, delta-condensed UnicodeData.txt
// layout of Unicode 16+: a line is present only where a property changes,
// so every code point with a non-identity simple (one-to-one) lowercase
// mapping keeps its own line. Field 1 is the code point, field 3 the general
// category, field 14 the simple lowercase mapping. Multi-character and
// contextual mappings are deliberately absent from this file and come from
// SpecialCasing.txt.
func parseUnicodeData(raw []byte) (map[rune]string, error) {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil, fmt.Errorf("no data lines")
	}
	first, _ := hexCP(strings.SplitN(lines[0], ";", 2)[0])
	last := ""
	for n := len(lines) - 1; n >= 0; n-- {
		if lines[n] != "" {
			last = lines[n]
			break
		}
	}
	lastCP, ok := hexCP(strings.SplitN(last, ";", 2)[0])
	if !ok || first != 0 || lastCP != 0x10FFFD {
		return nil, fmt.Errorf("data lines do not span U+0000..U+10FFFD (first U+%04X, last U+%04X): wrong file version or layout", first, lastCP)
	}
	out := make(map[rune]string)
	byGC := map[string]int{}
	nonIdentity := 0
	dataLines := 0
	var prev rune
	for n, line := range lines {
		if line == "" {
			continue
		}
		f := strings.Split(line, ";")
		if len(f) != 15 {
			return nil, fmt.Errorf("line %d: %d fields, want 15: %q", n+1, len(f), line[:min(60, len(line))])
		}
		cp, ok := hexCP(f[0])
		if !ok {
			return nil, fmt.Errorf("line %d: bad code point %q", n+1, f[0])
		}
		if prev != 0 && cp <= prev {
			return nil, fmt.Errorf("line %d: code point U+%04X not strictly increasing (previous U+%04X)", n+1, cp, prev)
		}
		prev = cp
		dataLines++
		byGC[f[2]]++
		lower := f[13]
		if lower == "" {
			continue
		}
		if strings.Contains(lower, " ") {
			return nil, fmt.Errorf("U+%04X: simple lowercase mapping %q is not one-to-one; full mappings belong to SpecialCasing.txt", cp, lower)
		}
		t, ok := hexCP(lower)
		if !ok {
			return nil, fmt.Errorf("U+%04X: bad lowercase target %q", cp, lower)
		}
		out[cp] = string(t)
		nonIdentity++
	}
	if out['A'] != "a" || out['B'] != "b" || out['I'] != "i" || out[0x03A3] != "\u03C3" || out[0x0130] != "i" || out[0x16EA0] != "\U00016EBB" {
		return nil, fmt.Errorf("required anchor mappings missing (U+0041, U+0042, U+0049, U+03A3, U+0130, U+16EA0 are all required in %s)", unicodeVersion)
	}
	if _, exists := out['a']; exists {
		return nil, fmt.Errorf("U+0061 must have the identity lowercase mapping, found %q", out['a'])
	}
	fmt.Printf("unicode data anchors: %d data lines, %d non-identity simple lowercase mappings, gc Lu=%d Lt=%d Ll=%d\n", dataLines, nonIdentity, byGC["Lu"], byGC["Lt"], byGC["Ll"])
	return out, nil
}

// parseSpecialCasing reads "code; lower; title; upper; [condition]; # comment"
// lines, where the mapping columns are space-separated runs of hex code
// points. Unconditional entries supply the full lowercase mappings that are
// not one-to-one. The only language-insensitive conditional entry must be the
// final-sigma rule for U+03A3; language-sensitive entries (lt/tr/az) are
// irrelevant for the locale-insensitive String.prototype.toLowerCase and are
// counted but never applied.
func parseSpecialCasing(raw []byte) (full map[rune]string, finalSigma bool, langSensitive int, err error) {
	full = make(map[rune]string)
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	versionSeen := false
	for n, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, "SpecialCasing-"+unicodeVersion) {
				versionSeen = true
			}
			continue
		}
		body := line
		if i := strings.Index(line, "#"); i >= 0 {
			body = line[:i]
		}
		cols := strings.Split(body, ";")
		if len(cols) != 5 && len(cols) != 6 {
			return nil, false, 0, fmt.Errorf("line %d: %d columns, want 5 or 6: %q", n+1, len(cols), line)
		}
		cp, ok := hexCP(strings.TrimSpace(cols[0]))
		if !ok {
			return nil, false, 0, fmt.Errorf("line %d: bad code point %q", n+1, cols[0])
		}
		var lowerRunes []rune
		for _, h := range strings.Fields(cols[1]) {
			r, ok := hexCP(h)
			if !ok {
				return nil, false, 0, fmt.Errorf("line %d: bad lowercase target %q", n+1, h)
			}
			lowerRunes = append(lowerRunes, r)
		}
		condStr := ""
		if len(cols) == 6 {
			condStr = strings.TrimSpace(cols[4])
			if strings.TrimSpace(cols[5]) != "" {
				return nil, false, 0, fmt.Errorf("line %d: unexpected trailing column %q", n+1, cols[5])
			}
		} else if strings.TrimSpace(cols[4]) != "" {
			return nil, false, 0, fmt.Errorf("line %d: unexpected column %q", n+1, cols[4])
		}
		if len(lowerRunes) == 0 && condStr != "" && !isLanguageID(condStr) {
			return nil, false, 0, fmt.Errorf("line %d: empty lowercase mapping for U+%04X", n+1, cp)
		}
		switch {
		case condStr == "":
			if _, dup := full[cp]; dup {
				return nil, false, 0, fmt.Errorf("line %d: duplicate unconditional entry for U+%04X", n+1, cp)
			}
			full[cp] = string(lowerRunes)
		case condStr == "Final_Sigma":
			if cp != 0x03A3 || len(lowerRunes) != 1 || lowerRunes[0] != 0x03C2 {
				return nil, false, 0, fmt.Errorf("line %d: unexpected final-sigma rule: U+%04X -> %v", n+1, cp, lowerRunes)
			}
			if finalSigma {
				return nil, false, 0, fmt.Errorf("line %d: duplicate final-sigma rule", n+1)
			}
			finalSigma = true
		case isLanguageID(condStr):
			langSensitive++
		default:
			return nil, false, 0, fmt.Errorf("line %d: unsupported casing condition %q (only Final_Sigma and language IDs are representable)", n+1, condStr)
		}
	}
	if !versionSeen {
		return nil, false, 0, fmt.Errorf("no %s version header found: wrong file", unicodeVersion)
	}
	if !finalSigma {
		return nil, false, 0, fmt.Errorf("final-sigma rule for U+03A3 is missing")
	}
	if v, ok := full[0x0130]; !ok || v != "i\u0307" {
		return nil, false, 0, fmt.Errorf("full lowercase mapping of U+0130 must be U+0069 U+0307, got %q", v)
	}
	if _, ok := full[0x03A3]; ok {
		return nil, false, 0, fmt.Errorf("U+03A3 must not have an unconditional mapping; it is contextual")
	}
	return full, finalSigma, langSensitive, nil
}

func isLanguageID(cond string) bool {
	ids := strings.Fields(cond)
	if len(ids) == 0 {
		return false
	}
	// Known Unicode casing language conditions plus the Not_ prefix used by
	// the file format; anything else must be a casing context and fails above.
	for _, id := range ids {
		switch id {
		case "lt", "tr", "az", "Not_Before_Dot", "Not_After_Dot", "Not_After_I", "After_I", "More_Above", "After_Soft_Dotted":
			continue
		default:
			return false
		}
	}
	return true
}

type cpRange struct{ lo, hi rune }

// parseDerivedCoreProperties extracts the Cased and Case_Ignorable range
// lines ("first..last ; Prop # comment"). Each derived property appears in
// exactly one "# Derived Property: ..." section; data lines outside the two
// wanted sections are ignored.
func parseDerivedCoreProperties(raw []byte) (cased, ignorable []cpRange, err error) {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	versionSeen := false
	inCased := false
	inIgnorable := false
	for n, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, "DerivedCoreProperties-"+unicodeVersion) {
				versionSeen = true
			}
			if strings.Contains(trimmed, "# Derived Property:") {
				name := strings.TrimPrefix(trimmed, "# Derived Property:")
				if j := strings.IndexAny(name, "(#"); j >= 0 {
					name = name[:j]
				}
				name = strings.TrimSpace(name)
				inCased = name == "Cased"
				inIgnorable = name == "Case_Ignorable"
			}
			continue
		}
		if !inCased && !inIgnorable {
			continue
		}
		i := strings.Index(trimmed, ";")
		if i < 0 {
			return nil, nil, fmt.Errorf("line %d: no semicolon: %q", n+1, trimmed)
		}
		rest := strings.SplitN(trimmed[i+1:], "#", 2)[0]
		prop := strings.TrimSpace(rest)
		want := "Cased"
		if inIgnorable {
			want = "Case_Ignorable"
		}
		if prop != want {
			return nil, nil, fmt.Errorf("line %d: property %q in the wrong section (want %s)", n+1, prop, want)
		}
		rangePart := strings.TrimSpace(trimmed[:i])
		var r cpRange
		if j := strings.Index(rangePart, ".."); j >= 0 {
			lo, ok1 := hexCP(rangePart[:j])
			hi, ok2 := hexCP(rangePart[j+2:])
			if !ok1 || !ok2 || lo > hi {
				return nil, nil, fmt.Errorf("line %d: bad range %q", n+1, rangePart)
			}
			r = cpRange{lo, hi}
		} else {
			cp, ok := hexCP(rangePart)
			if !ok {
				return nil, nil, fmt.Errorf("line %d: bad range %q", n+1, rangePart)
			}
			r = cpRange{cp, cp}
		}
		if inCased {
			cased = append(cased, r)
		} else {
			ignorable = append(ignorable, r)
		}
	}
	if !versionSeen {
		return nil, nil, fmt.Errorf("no %s version header found: wrong file", unicodeVersion)
	}
	if len(cased) == 0 || len(ignorable) == 0 {
		return nil, nil, fmt.Errorf("Cased or Case_Ignorable ranges missing")
	}
	if !rangeContains(cased, 0x0041) || !rangeContains(cased, 0x03A3) || !rangeContains(cased, 0x16EA0) || !rangeContains(cased, 0xA7F1) || !rangeContains(cased, 0x0345) {
		return nil, nil, fmt.Errorf("Cased section is missing required anchors (U+0041, U+03A3, U+16EA0, U+A7F1, U+0345)")
	}
	if !rangeContains(ignorable, 0x0897) || !rangeContains(ignorable, 0x2018) || !rangeContains(ignorable, 0xA7F1) {
		return nil, nil, fmt.Errorf("Case_Ignorable section is missing required anchors (U+0897, U+2018, U+A7F1)")
	}
	return cased, ignorable, nil
}

func rangeContains(rs []cpRange, cp rune) bool {
	for _, r := range rs {
		if r.lo <= cp && cp <= r.hi {
			return true
		}
	}
	return false
}

// buildLowercaseMap merges the simple one-to-one mappings with the full
// mappings from SpecialCasing.txt and drops identity mappings.
func buildLowercaseMap(simple, full map[rune]string) (map[rune]string, []rune) {
	merged := make(map[rune]string, len(simple)+len(full))
	var overrides []rune
	for cp, v := range simple {
		merged[cp] = v
	}
	for cp, v := range full {
		if old, ok := merged[cp]; ok && old != v {
			overrides = append(overrides, cp)
		}
		merged[cp] = v
	}
	for cp, v := range merged {
		if v == string(cp) {
			delete(merged, cp)
		}
	}
	sort.Slice(overrides, func(i, j int) bool { return overrides[i] < overrides[j] })
	return merged, overrides
}

// rangeTable renders the ranges in the unicode.RangeTable layout used by the
// Go standard library (R16 for U+FFFF and below, R32 above).
func rangeTable(rs []cpRange) (r16 []string, r32 []string) {
	for _, r := range rs {
		lo, hi := hexConst(r.lo), hexConst(r.hi)
		if r.hi <= 0xFFFF {
			r16 = append(r16, fmt.Sprintf("{Lo: %s, Hi: %s, Stride: 1}", lo, hi))
		} else if r.lo > 0xFFFF {
			r32 = append(r32, fmt.Sprintf("{Lo: %s, Hi: %s, Stride: 1}", lo, hi))
		} else {
			// A range that straddles the BMP boundary is split at U+FFFF.
			r16 = append(r16, fmt.Sprintf("{Lo: %s, Hi: 0xFFFF, Stride: 1}", lo))
			r32 = append(r32, fmt.Sprintf("{Lo: 0x10000, Hi: %s, Stride: 1}", hi))
		}
	}
	return r16, r32
}

func hexConst(r rune) string {
	return fmt.Sprintf("0x%X", int(r))
}

func render(merged map[rune]string, cased, ignorable []cpRange, sums map[string]string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by internal/text/genlower17.go against Unicode ")
	b.WriteString(unicodeVersion + "; DO NOT EDIT.\n")
	b.WriteString("//\n")
	b.WriteString("// Source URLs and SHA256 of the actual bytes used:\n")
	b.WriteString("//\n")
	for _, name := range []string{unicodeDataFile, derivedCoreFile, specialCasingFile} {
		b.WriteString(fmt.Sprintf("//\t%s  %s\n", sums[name], mapURL[name]))
	}
	b.WriteString("//\n")
	b.WriteString("// The data files are licensed under the Unicode license; see UNICODE-LICENSE.txt.\n")
	b.WriteString("package text\n")
	b.WriteString("\n")
	b.WriteString("import \"unicode\"\n")
	b.WriteString("\n")
	b.WriteString("// lowerCase17Version is the Unicode version the tables in this file were generated from.\n")
	b.WriteString("const lowerCase17Version = \"")
	b.WriteString(unicodeVersion + "\"\n")
	b.WriteString("\n")
	b.WriteString("// lowerCase17SourceSHA256 records the SHA256 of each normative source file that\n")
	b.WriteString("// internal/text/genlower17.go used to generate this table.\n")
	b.WriteString("var lowerCase17SourceSHA256 = map[string]string{\n")
	for _, name := range []string{unicodeDataFile, derivedCoreFile, specialCasingFile} {
		b.WriteString(fmt.Sprintf("\t%q: %q,\n", name, sums[name]))
	}
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("// jslower17 maps each code point whose Unicode ")
	b.WriteString(unicodeVersion + " full lowercase mapping differs from the\n")
	b.WriteString("// code point itself to that mapping. U+03A3 (GREEK CAPITAL LETTER SIGMA) is\n")
	b.WriteString("// deliberately absent: its final-sigma context is handled by JSLower.\n")
	keys := make([]rune, 0, len(merged))
	for cp := range merged {
		keys = append(keys, cp)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	b.WriteString("var jslower17 = map[rune]string{\n")
	for _, cp := range keys {
		val := merged[cp]
		var sb strings.Builder
		for _, r := range val {
			sb.WriteString(esc(r))
		}
		b.WriteString(fmt.Sprintf("\t'%s': \"%s\",\n", esc(cp), sb.String()))
	}
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("// cased17Table is the Unicode ")
	b.WriteString(unicodeVersion + " Cased property (UCD definition D135). Check it with\n")
	b.WriteString("// unicode.Is(cased17Table, r): both the Go 1.25 unicode.Is(Range, rune) form and\n")
	b.WriteString("// the Go 1.27 unicode.Is(*RangeTable, rune) form accept a *unicode.RangeTable.\n")
	writeRangeTable(&b, "cased17Table", cased)
	b.WriteString("\n")
	b.WriteString("// caseIgnorable17Table is the Unicode ")
	b.WriteString(unicodeVersion + " Case_Ignorable property. Check it with unicode.Is(caseIgnorable17Table, r).\n")
	writeRangeTable(&b, "caseIgnorable17Table", ignorable)
	src := []byte(b.String())
	return format.Source(src)
}

var mapURL = map[string]string{
	unicodeDataFile:   unicodeDataURL,
	derivedCoreFile:   derivedCoreURL,
	specialCasingFile: specialCasingURL,
}

func writeRangeTable(b *strings.Builder, name string, rs []cpRange) {
	r16, r32 := rangeTable(rs)
	fmt.Fprintf(b, "var %s = &unicode.RangeTable{\n", name)
	if len(r16) > 0 {
		b.WriteString("\tR16: []unicode.Range16{\n")
		for _, e := range r16 {
			b.WriteString("\t\t" + e + ",\n")
		}
		b.WriteString("\t},\n")
	}
	if len(r32) > 0 {
		b.WriteString("\tR32: []unicode.Range32{\n")
		for _, e := range r32 {
			b.WriteString("\t\t" + e + ",\n")
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
}
