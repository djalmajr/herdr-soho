// Package reportscan reads completion and review markers from worker reports.
package reportscan

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

var (
	partialMarker  = regexp.MustCompile("[`*]*\\[partial\\][`*]*")
	stateBefore    = regexp.MustCompile(`^[ \t]*(?:[-*+][ \t]+|[0-9]+\.[ \t]+|#{1,6}[ \t]+|>[ \t]*|\[[ xX]\][ \t]+)?[ \t]*$`)
	cellBefore     = regexp.MustCompile(`\|[ \t]*$`)
	cellAfter      = regexp.MustCompile(`^[ \t]*(?:\||$)`)
	dashBefore     = regexp.MustCompile("[:\\x{2014}\\x{2013}-][`*]*[ \\t]+$")
	fenceOpen      = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	fenceClose     = regexp.MustCompile("^ {0,3}(`{3,}|~{3,}) *$")
	anyPartial     = regexp.MustCompile(`\[partial\]`)
	anyDone        = regexp.MustCompile(`\[done\]`)
	anySkipped     = regexp.MustCompile(`\[skipped\]`)
	reviewHeaderRE = regexp.MustCompile(`^findings: ([0-9]+) \(p0 ([0-9]+), p1 ([0-9]+), p2 ([0-9]+), p3 ([0-9]+)\) \| verdict: (pass|fail)$`)
)

// PartialCount counts report lines that state a [partial] item outside fenced
// code blocks. A line counts at most once.
func PartialCount(text string) int {
	if text == "" {
		return 0
	}
	count := 0
	inFence, fenceChar, fenceLen := false, byte(0), 0
	for _, line := range strings.Split(text, "\n") {
		open := fenceOpen.FindStringSubmatch(line)
		if !inFence && open != nil {
			inFence, fenceChar, fenceLen = true, open[1][0], len(open[1])
			continue
		}
		if inFence {
			close := fenceClose.FindStringSubmatch(line)
			if close != nil && close[1][0] == fenceChar && len(close[1]) >= fenceLen {
				inFence = false
			}
			continue
		}
		line = lowerASCII(line)
		if !anyPartial.MatchString(line) || (anyDone.MatchString(line) && anySkipped.MatchString(line)) {
			continue
		}
		if markerInStatePosition(line) {
			count++
		}
	}
	return count
}

func markerInStatePosition(line string) bool {
	for _, loc := range partialMarker.FindAllStringIndex(line, -1) {
		before, after := line[:loc[0]], line[loc[1]:]
		if stateBefore.MatchString(before) || (cellBefore.MatchString(before) && cellAfter.MatchString(after)) || dashBefore.MatchString(before) {
			return true
		}
	}
	return false
}

// PartialCountFile counts partial markers in a report; unreadable files count 0.
func PartialCountFile(file string) int {
	text, err := platform.ReadTextFile(file)
	if err != nil {
		return 0
	}
	return PartialCount(text)
}

// ReviewHeader is the parsed first-line review summary.
type ReviewHeaderResult struct {
	Findings float64            `json:"findings"`
	Severity map[string]float64 `json:"severity"`
	Verdict  string             `json:"verdict"`
}

// ParseReviewHeader recognizes a review header only on the first non-empty line.
func ReviewHeader(text string) *ReviewHeaderResult {
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimFunc(raw, isJSTrimSpace)
		if line == "" {
			continue
		}
		match := reviewHeaderRE.FindStringSubmatch(lowerASCII(line))
		if match == nil {
			return nil
		}
		return &ReviewHeaderResult{
			Findings: jsNumber(match[1]),
			Severity: map[string]float64{"P0": jsNumber(match[2]), "P1": jsNumber(match[3]), "P2": jsNumber(match[4]), "P3": jsNumber(match[5])},
			Verdict:  strings.ToLower(match[6]),
		}
	}
	return nil
}

func lowerASCII(value string) string {
	bytes := []byte(value)
	for i, b := range bytes {
		if b >= 'A' && b <= 'Z' {
			bytes[i] = b + ('a' - 'A')
		}
	}
	return string(bytes)
}

func isJSTrimSpace(r rune) bool {
	return r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r' || r == ' ' ||
		r == '\u00A0' || r == '\u1680' || (r >= '\u2000' && r <= '\u200A') ||
		r == '\u2028' || r == '\u2029' || r == '\u202F' || r == '\u205F' ||
		r == '\u3000' || r == '\uFEFF'
}

func jsNumber(value string) float64 {
	n, _ := strconv.ParseFloat(value, 64)
	return n
}
