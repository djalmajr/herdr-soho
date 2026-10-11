// scan_acceptfile_test.go guards the reviewed accept lists' comment
// lines: a comment must never carry a content-rule hit, a provider
// token prefix with an attached body, or a long mixed-class run of the
// kind a credential body shows. The checks never echo the offending
// bytes; a violation names only the line number and the reason.

package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// tokenMarkers are the provider token prefixes a comment word must not
// carry a body after. Each is assembled from two string parts at
// runtime so the source never holds a contiguous marker.
var tokenMarkers = []string{
	"sk" + "-",
	"sk" + "-ant-",
	"sk" + "-proj-",
	"sk" + "-svcacct-",
	"xai" + "-",
	"ghp" + "_",
	"gho" + "_",
	"ghu" + "_",
	"ghs" + "_",
	"ghr" + "_",
	"github" + "_pat_",
	"npm" + "_",
	"xoxb" + "-",
	"xoxa" + "-",
	"xoxp" + "-",
	"xoxr" + "-",
	"xoxs" + "-",
	"AKI" + "A",
	"ASI" + "A",
	"AIz" + "a",
}

// tokenWordTrim is the set of surrounding characters stripped from each
// word before the marker and mixed-class checks.
const tokenWordTrim = "`'\"()[]{}<>,.;:"

// tokenMarkerBare reports whether w is exactly one of the token
// markers, or an ASCII upper-cased form of an sk family marker; a
// bare marker in a comment is allowed.
func tokenMarkerBare(w string) bool {
	lw := lowerASCIIString(w)
	for _, m := range tokenMarkers {
		if w == m {
			return true
		}
		if strings.HasPrefix(m, "sk"+"-") && lw == m {
			return true
		}
	}
	return false
}

// tokenPrefixViolation reports whether the stripped word starts with a
// provider token marker and carries a body after it. Marker comparison
// is case-sensitive, except for the sk family, which is also checked
// on the lower-cased word.
func tokenPrefixViolation(w string) bool {
	if tokenMarkerBare(w) {
		return false
	}
	lw := lowerASCIIString(w)
	for _, m := range tokenMarkers {
		skFamily := strings.HasPrefix(m, "sk"+"-")
		if strings.HasPrefix(w, m) || (skFamily && strings.HasPrefix(lw, m)) {
			return true
		}
	}
	return false
}

// mixedClassViolation reports whether the stripped word is at least 12
// bytes and contains an ASCII upper-case letter, a lower-case letter
// and a digit, the shape a credential body shows.
func mixedClassViolation(w string) bool {
	if len(w) < 12 {
		return false
	}
	var up, lo, dig bool
	for i := 0; i < len(w); i++ {
		switch c := w[i]; {
		case c >= 'A' && c <= 'Z':
			up = true
		case c >= 'a' && c <= 'z':
			lo = true
		case c >= '0' && c <= '9':
			dig = true
		}
	}
	return up && lo && dig
}

// acceptCommentViolations checks every comment line of an accept list
// (a trimmed line starting with "#") against the content rules, the
// provider token markers and the mixed-class shape. The returned
// descriptions name only the line number and the reason; they never
// carry the offending bytes.
func acceptCommentViolations(text string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(line int, reason string) {
		d := "line " + strconv.Itoa(line) + ": " + reason
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		num := i + 1
		for _, r := range contentRuleHits(line, scanRules) {
			add(num, "content rule "+r)
		}
		for _, w := range strings.Fields(line) {
			w = strings.Trim(w, tokenWordTrim)
			if w == "" {
				continue
			}
			if tokenPrefixViolation(w) {
				add(num, "token prefix with attached body")
			}
			if mixedClassViolation(w) {
				add(num, "mixed-class run")
			}
		}
	}
	return out
}

// acceptTupleCount counts the accept tuples of one accept list: the
// non-blank lines that do not start with "#".
func acceptTupleCount(text string) int {
	n := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line != "" && !strings.HasPrefix(line, "#") {
			n++
		}
	}
	return n
}

func TestAcceptFileCommentsCarryNoKeyMaterial(t *testing.T) {
	for _, name := range []string{
		"../image-scan-accept.linux-arm64.txt",
		"../image-scan-accept.linux-amd64.txt",
	} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(data)
		if n := acceptTupleCount(text); n < 20 {
			t.Errorf("%s: %d accept tuples, want at least 20 (the read is real)", name, n)
		}
		for _, v := range acceptCommentViolations(text) {
			t.Errorf("%s: %s", name, v)
		}
	}
}

// canaryBody returns n bytes of deterministic mixed-case alphanumeric
// text built from a counter: it has no relation to any real or copied
// key.
func canaryBody(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			b.WriteByte(byte('A' + i%26))
		case 1:
			b.WriteByte(byte('a' + i%26))
		case 2:
			b.WriteByte(byte('0' + i%10))
		}
	}
	return b.String()
}

func TestAcceptCommentCheckCatchesCanaries(t *testing.T) {
	var lines []string
	canaryLines := []int{}
	bodies := []string{canaryBody(8), canaryBody(48)}
	for _, m := range tokenMarkers {
		for _, body := range bodies {
			lines = append(lines, "# canary line "+m+body)
			canaryLines = append(canaryLines, len(lines))
		}
	}
	bareLines := []int{}
	for _, m := range tokenMarkers {
		lines = append(lines, "# bare marker "+m+" ")
		bareLines = append(bareLines, len(lines))
	}
	// Case-variant canaries: an upper-cased bare family marker is
	// allowed, the same marker with a lower-case-only letter body is
	// not. Both are built at runtime from the first family marker.
	var loBody strings.Builder
	for i := 0; i < 10; i++ {
		loBody.WriteByte(byte('a' + i%26))
	}
	upperWord := strings.ToUpper(tokenMarkers[0]) + loBody.String()
	lines = append(lines, "# canary line "+upperWord)
	upperCanaryLine := len(lines)
	lines = append(lines, "# bare marker "+strings.ToUpper(tokenMarkers[0])+" ")
	bareLines = append(bareLines, len(lines))
	ordinaryLine := len(lines) + 1
	lines = append(lines, "# ordinary words UPPER_SNAKE CamelCase libgnutls30t64 sha256 x86_64-unknown-linux-musl")
	text := strings.Join(lines, "\n") + "\n"

	violations := acceptCommentViolations(text)
	if len(violations) == 0 {
		t.Fatal("no violation reported for any canary")
	}
	byLine := map[string][]string{}
	for _, v := range violations {
		i := strings.IndexByte(v, ':')
		if i <= 0 {
			t.Fatalf("violation without a line number: %q", v)
		}
		byLine[v[:i]] = append(byLine[v[:i]], v[i+2:])
		for _, body := range bodies {
			if strings.Contains(v, body) {
				t.Errorf("violation carries the canary body: %q", v)
			}
		}
		for _, word := range []string{upperWord, strings.ToUpper(tokenMarkers[0])} {
			if strings.Contains(v, word) {
				t.Errorf("violation carries a canary word: %q", v)
			}
		}
	}
	hasReason := func(n int, r string) bool {
		for _, x := range byLine["line "+strconv.Itoa(n)] {
			if x == r {
				return true
			}
		}
		return false
	}
	for _, n := range canaryLines {
		if _, ok := byLine["line "+strconv.Itoa(n)]; !ok {
			t.Errorf("canary on line %d not reported", n)
		}
	}
	if !hasReason(upperCanaryLine, "token prefix with attached body") {
		t.Errorf("upper-cased family canary on line %d not reported with the token prefix reason", upperCanaryLine)
	}
	for _, n := range bareLines {
		if _, ok := byLine["line "+strconv.Itoa(n)]; ok {
			t.Errorf("bare marker on line %d reported: %v", n, violations)
		}
	}
	if _, ok := byLine["line "+strconv.Itoa(ordinaryLine)]; ok {
		t.Errorf("ordinary words on line %d reported: %v", ordinaryLine, violations)
	}
}

// TestAcceptCommentCheckSourceHasNoMarker asserts that none of the
// token markers occurs contiguously in this test file's source, comments
// included; a failure names only the marker index, never the marker.
func TestAcceptCommentCheckSourceHasNoMarker(t *testing.T) {
	data, err := os.ReadFile("scan_acceptfile_test.go")
	if err != nil {
		t.Fatalf("read scan_acceptfile_test.go: %v", err)
	}
	src := string(data)
	for i, m := range tokenMarkers {
		if strings.Contains(src, m) {
			t.Errorf("marker %d occurs contiguously in scan_acceptfile_test.go", i)
		}
	}
}
