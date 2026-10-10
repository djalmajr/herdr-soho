package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// runCLI runs the imagecheck dispatch with the given argv (the program
// name excluded) and returns the exit code and the captured writers.
func runCLI(args []string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// runContextCLI runs `imagecheck context <dir> [extra...]`.
func runContextCLI(t *testing.T, dir string, extra ...string) (int, string, string) {
	t.Helper()
	args := append([]string{"context", dir}, extra...)
	return runCLI(args)
}

// runImageCLI runs `imagecheck image <archive> [extra...]`.
func runImageCLI(t *testing.T, archive string, extra ...string) (int, string, string) {
	t.Helper()
	args := append([]string{"image", archive}, extra...)
	return runCLI(args)
}

// writeContextFile writes content to dir/name (creating parents) with
// the name expressed in slash separators.
func writeContextFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeAuxFile writes one auxiliary file (deny-file, accept-file)
// outside the tree being scanned.
func writeAuxFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// lineWith returns the first line of out that starts with prefix, or
// "" when there is none.
func lineWith(out, prefix string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}

// wantLen returns the byte length of the first match of the named
// rule in content, computed with the rule's own pattern: the scanner
// must report exactly that match length (the body runs of some rules
// include trailing line breaks, so it is not always len(token)).
func wantLen(t *testing.T, rule, content string) int {
	t.Helper()
	rules := make([]scanRule, 0, len(scanRules)+len(hostPathRules))
	rules = append(rules, scanRules...)
	rules = append(rules, hostPathRules...)
	for _, r := range rules {
		if r.name != rule {
			continue
		}
		m := r.pattern.FindStringIndex(content)
		if m == nil {
			t.Fatalf("no %s match in the fixture", rule)
		}
		return m[1] - m[0]
	}
	t.Fatalf("unknown rule %s", rule)
	return 0
}

// hasLine reports whether out contains the exact line.
func hasLine(out, line string) bool {
	return strings.Contains(out, line+"\n")
}

// sumOf returns the 64 lowercase hex sha256 of b.
func sumOf(b string) string {
	s := sha256.Sum256([]byte(b))
	return hex.EncodeToString(s[:])
}

// contentLine assembles a content-match line: the kind (finding or
// accepted), the rule, the display path, the offset, the match length
// and the sha256 of the whole file.
func contentLine(kind, rule, display string, off, length int, sum string) string {
	return kind + "\t" + rule + "\t" + display + "\t" + strconv.Itoa(off) +
		"\t" + strconv.Itoa(length) + "\tsha256:" + sum
}

// denyLine assembles a deny-token finding line: no length and no hash,
// so nothing about the literal leaks beyond its line number.
func denyLine(rule, display string, off int) string {
	return "finding\t" + rule + "\t" + display + "\t" + strconv.Itoa(off)
}

// makeEntry assembles one valid accept entry string for the match of
// token at off in the file with content.
func makeEntry(rule, path, content, token string, off, length int) string {
	return rule + " " + strconv.Itoa(off) + " " + strconv.Itoa(length) +
		" sha256:" + sumOf(content) + " " + path
}

// entryOf is makeEntry for the first occurrence of token in content.
func entryOf(rule, path, content, token string) string {
	return makeEntry(rule, path, content, token, strings.Index(content, token), len(token))
}

// The fake token builders assemble scanner-shaped tokens at runtime so
// no matchable literal lives in this source file.
func tokAnthropic() string      { return "sk-ant-" + "api03-" + strings.Repeat("A", 90) }
func tokOpenAI() string         { return "sk-proj-" + strings.Repeat("B", 48) }
func tokXAI() string            { return "xai-" + strings.Repeat("C", 64) }
func tokGitHub() string         { return "ghp_" + strings.Repeat("D", 40) }
func tokGitHubPat() string      { return "github_pat_" + strings.Repeat("E", 64) }
func tokNPM() string            { return "npm_" + strings.Repeat("F", 36) }
func tokAWS() string            { return "AKIA" + "ABCDEFGHIJKLMNOP" }
func tokAWSDocsExample() string { return "AKIA" + "IOSFODNN7EXAMPLE" }
func tokSlack() string          { return "xoxb-" + strings.Repeat("G", 16) }
func tokGoogle() string         { return "AIza" + strings.Repeat("H", 35) }

// The fake PEM builders assemble private-key-shaped blocks at runtime:
// a header, optional PEM header lines and a base64 body of at least
// 64 characters from the body alphabet.
func fakePEMPKCS8() string {
	return "-----BEGIN " + "PRIVATE KEY-----\n" +
		strings.Repeat("Q", 80) + "\n-----END " + "PRIVATE KEY-----\n"
}

// fakePEMEncrypted is a legacy RSA key with Proc-Type/DEK-Info header
// lines, a blank separator line and an encrypted body.
func fakePEMEncrypted() string {
	return "-----BEGIN " + "RSA " + "PRIVATE KEY-----\n" +
		"Proc-Type: 4,ENCRYPTED\n" +
		"DEK-Info: AES-128-CBC, 0123456789ABCDEF\n" +
		"\n" +
		strings.Repeat("R", 90) + "\n" +
		"-----END " + "RSA " + "PRIVATE KEY-----\n"
}

// fakePEMCRLF is an EC key with CRLF line endings.
func fakePEMCRLF() string {
	return "-----BEGIN " + "EC " + "PRIVATE KEY-----\r\n" +
		strings.Repeat("S", 70) + "\r\n"
}

// fakePEMHeaderOnly is the bare format string that binaries and docs
// embed; it has no body and must not count as a finding.
func fakePEMHeaderOnly() string { return "-----BEGIN " + "RSA " + "PRIVATE KEY-----" }

func TestContextContentRules(t *testing.T) {
	cases := []struct {
		name  string
		rule  string
		token func() string
	}{
		{"private-key-pkcs8", "private-key", fakePEMPKCS8},
		{"private-key-legacy-encrypted", "private-key", fakePEMEncrypted},
		{"private-key-crlf", "private-key", fakePEMCRLF},
		{"anthropic-key", "anthropic-key", tokAnthropic},
		{"openai-key", "openai-key", tokOpenAI},
		{"xai-key", "xai-key", tokXAI},
		{"github-token", "github-token", tokGitHub},
		{"github-token", "github-token", tokGitHubPat},
		{"npm-token", "npm-token", tokNPM},
		{"aws-access-key", "aws-access-key", tokAWS},
		{"slack-token", "slack-token", tokSlack},
		{"google-api-key", "google-api-key", tokGoogle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			name := "src/secret.txt"
			content := "key=" + c.token() + "\n"
			writeContextFile(t, dir, name, content)
			code, out, errb := runContextCLI(t, dir, "--allow", "src")
			if code != 1 {
				t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
			}
			off := strings.Index(content, c.token())
			want := contentLine("finding", c.rule, name, off, wantLen(t, c.rule, content), sumOf(content))
			if got := lineWith(out, "finding\t"); got != want {
				t.Fatalf("finding line = %q, want %q", got, want)
			}
			wantSum := "summary\tfiles=1\tbytes=" + strconv.Itoa(len(content)) +
				"\tfindings=1\taccepted=0\tunmatched=0"
			if !hasLine(out, wantSum) {
				t.Fatalf("summary missing: got %q", out)
			}
			if strings.Contains(out, c.token()) || strings.Contains(errb, c.token()) {
				t.Fatal("matched content leaked to the output")
			}
		})
	}
}

func TestContextContentRulesNegative(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"private-key-certificate", "-----BEGIN " + "CERTIFICATE-----\n"},
		{"private-key-header-only", fakePEMHeaderOnly() + "\n"},
		{"private-key-quoted", fakePEMHeaderOnly() + `"` + "format" + "\n"},
		{"private-key-short-body", fakePEMHeaderOnly() + "\n" + strings.Repeat("T", 20) + "\n"},
		{"anthropic-key", "sk-ant-" + "abc03-" + strings.Repeat("A", 79) + "\n"},
		{"openai-key", "sk-proj-" + strings.Repeat("B", 39) + "\n"},
		{"xai-key", "xai-" + strings.Repeat("C", 59) + "\n"},
		{"github-token", "ghp_" + strings.Repeat("D", 35) + "\n"},
		{"npm-token", "npm_" + strings.Repeat("F", 35) + "\n"},
		{"aws-access-key", "AKIA" + "IOSFODNN7EXAMPLE" + "\n"},
		{"slack-token", "xoxb-" + strings.Repeat("G", 9) + "\n"},
		{"google-api-key", "AIza" + strings.Repeat("H", 34) + "\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeContextFile(t, dir, "src/negative.txt", c.content)
			code, out, errb := runContextCLI(t, dir, "--allow", "src")
			if code != 0 {
				t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
			}
			if lineWith(out, "finding\t") != "" {
				t.Fatalf("unexpected finding in %q", out)
			}
			wantSum := "summary\tfiles=1\tbytes=" + strconv.Itoa(len(c.content)) +
				"\tfindings=0\taccepted=0\tunmatched=0"
			if !hasLine(out, wantSum) {
				t.Fatalf("summary missing: got %q", out)
			}
		})
	}
}

func TestContextDenyFile(t *testing.T) {
	ctxDir := t.TempDir()
	auxDir := t.TempDir()
	token := strings.Repeat("Z", 16)
	// line 1: comment, line 2: blank, line 3: padded token
	deny := writeAuxFile(t, auxDir, "deny.txt", "# comment\n\n  "+token+"  \n")
	name := "src/data.txt"
	content := "before " + token + " after\n"
	writeContextFile(t, ctxDir, name, content)
	code, out, errb := runContextCLI(t, ctxDir, "--allow", "src", "--deny-file", deny)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	want := denyLine("deny-token:3", name, strings.Index(content, token))
	if got := lineWith(out, "finding\t"); got != want {
		t.Fatalf("finding line = %q, want %q", got, want)
	}
	if strings.Contains(out, token) || strings.Contains(errb, token) {
		t.Fatal("deny token leaked to the output")
	}
	if !hasLine(out, "summary\tfiles=1\tbytes="+strconv.Itoa(len(content))+"\tfindings=1\taccepted=0\tunmatched=0") {
		t.Fatalf("summary missing: got %q", out)
	}
}

func TestContextDenyFileShortToken(t *testing.T) {
	ctxDir := t.TempDir()
	auxDir := t.TempDir()
	short := "xy9" // 3 bytes: below the 4-byte minimum
	deny := writeAuxFile(t, auxDir, "deny.txt", "ok12\n"+short+"\n")
	writeContextFile(t, ctxDir, "src/x.txt", "hello\n")
	code, out, errb := runContextCLI(t, ctxDir, "--allow", "src", "--deny-file", deny)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%s", code, errb)
	}
	if !strings.Contains(errb, "line 2") {
		t.Fatalf("stderr should cite the line number: %q", errb)
	}
	if strings.Contains(errb, short) || strings.Contains(out, short) {
		t.Fatal("short token leaked to the output")
	}
}

// TestContextDenyCaseInsensitive matches a deny token in a different
// ASCII case than the file content holds.
func TestContextDenyCaseInsensitive(t *testing.T) {
	ctxDir := t.TempDir()
	auxDir := t.TempDir()
	// Built from parts: the token "CanaryWord".
	token := "Can" + "aryWord"
	deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
	name := "src/data.txt"
	content := "xx" + "canaryWORD" + "xx\n"
	writeContextFile(t, ctxDir, name, content)
	code, out, errb := runContextCLI(t, ctxDir, "--allow", "src", "--deny-file", deny)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	want := denyLine("deny-token:1", name, 2)
	if got := lineWith(out, "finding\t"); got != want {
		t.Fatalf("finding line = %q, want %q", got, want)
	}
	if strings.Contains(out, token) || strings.Contains(errb, token) {
		t.Fatal("deny token leaked to the output")
	}
	if !hasLine(out, "summary\tfiles=1\tbytes="+strconv.Itoa(len(content))+"\tfindings=1\taccepted=0\tunmatched=0") {
		t.Fatalf("summary missing: got %q", out)
	}
}

func TestContextBlockBoundary(t *testing.T) {
	token := tokXAI() // 68 bytes, longer than the 4 KiB overlap
	for _, tc := range []struct {
		name  string
		start int
	}{
		{"crossing", scanBlock - 10},                    // starts in block 1, ends in block 2
		{"insideOverlap", scanBlock - scanOverlap + 16}, // fully inside the overlap
		{"atEOF", scanBlock - len(token)},               // file is exactly one block
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			content := strings.Repeat("x", tc.start) + token
			writeContextFile(t, dir, "src/big.log", content)
			code, out, errb := runContextCLI(t, dir, "--allow", "src")
			if code != 1 {
				t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
			}
			want := contentLine("finding", "xai-key", "src/big.log", tc.start, len(token), sumOf(content))
			if got := lineWith(out, "finding\t"); got != want {
				t.Fatalf("finding line = %q, want %q", got, want)
			}
			wantSum := "summary\tfiles=1\tbytes=" + strconv.Itoa(tc.start+len(token)) +
				"\tfindings=1\taccepted=0\tunmatched=0"
			if !hasLine(out, wantSum) {
				t.Fatalf("summary missing: got %q", out)
			}
			if strings.Contains(out, token) || strings.Contains(errb, token) {
				t.Fatal("token leaked to the output")
			}
		})
	}
	// A PEM private key (header plus body) crossing the block boundary.
	pem := fakePEMPKCS8()
	dir := t.TempDir()
	start := scanBlock - 40
	content := strings.Repeat("x", start) + pem
	writeContextFile(t, dir, "src/big.pem", content)
	code, out, errb := runContextCLI(t, dir, "--allow", "src")
	if code != 1 {
		t.Fatalf("pem crossing: code = %d, want 1; stderr=%s", code, errb)
	}
	want := contentLine("finding", "private-key", "src/big.pem", start, wantLen(t, "private-key", content), sumOf(content))
	if got := lineWith(out, "finding\tprivate-key\t"); got != want {
		t.Fatalf("pem crossing: finding line = %q, want %q", got, want)
	}
	if strings.Contains(out, pem) || strings.Contains(errb, pem) {
		t.Fatal("pem block leaked to the output")
	}
}

// TestContextPEMHeaderCrossingOverlap keeps a PEM private key whose
// header region is longer than the 4 KiB overlap: the BEGIN marker
// 5000 bytes before the 1 MiB block boundary is carried so the full
// match is found with its full length, and an open region beyond
// maxPEMCarry fails closed.
func TestContextPEMHeaderCrossingOverlap(t *testing.T) {
	marker := "-----BEGIN " + "RSA " + "PRIVATE KEY-----"
	headers := ""
	for i := 0; i < 100; i++ {
		headers += "Hdr" + string(rune('A'+i%26)) + "-: " + strings.Repeat("v", 41) + "\n"
	}
	body := strings.Repeat("Z", 80)
	pem := marker + "\n" + headers + body + "\n-----END " + "RSA " + "PRIVATE KEY-----\n"
	if len(headers) <= scanOverlap {
		t.Fatalf("headers must be longer than the overlap: %d", len(headers))
	}
	dir := t.TempDir()
	name := "src/bigkey"
	start := scanBlock - 5000
	content := strings.Repeat("x", start) + pem
	writeContextFile(t, dir, name, content)
	code, out, errb := runContextCLI(t, dir, "--allow", "src")
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if c := strings.Count(out, "finding\tprivate-key\t"); c != 1 {
		t.Fatalf("private-key findings = %d, want 1:\n%s", c, out)
	}
	want := contentLine("finding", "private-key", name, start, wantLen(t, "private-key", content), sumOf(content))
	if got := lineWith(out, "finding\tprivate-key\t"); got != want {
		t.Fatalf("finding line = %q, want %q", got, want)
	}
	if strings.Contains(out, marker) || strings.Contains(errb, marker) {
		t.Fatal("pem marker leaked to the output")
	}
}

// TestContextPEMHeaderFailClosed proves the fail-closed finding when
// keeping the tail from the open marker would exceed maxPEMCarry: the
// private-key finding is reported at the marker offset with the length
// from the marker to the window end.
func TestContextPEMHeaderFailClosed(t *testing.T) {
	saved := maxPEMCarry
	maxPEMCarry = scanOverlap + 100
	t.Cleanup(func() { maxPEMCarry = saved })
	marker := "-----BEGIN " + "RSA " + "PRIVATE KEY-----"
	headers := ""
	for i := 0; i < 100; i++ {
		headers += "Hdr" + string(rune('A'+i%26)) + "-: " + strings.Repeat("v", 52) + "\n"
	}
	pem := marker + "\n" + headers + strings.Repeat("Z", 80) + "\n"
	dir := t.TempDir()
	name := "src/longkey"
	start := scanBlock - 5000
	content := strings.Repeat("x", start) + pem
	writeContextFile(t, dir, name, content)
	code, out, errb := runContextCLI(t, dir, "--allow", "src")
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if c := strings.Count(out, "finding\tprivate-key\t"); c != 1 {
		t.Fatalf("private-key findings = %d, want 1:\n%s", c, out)
	}
	want := contentLine("finding", "private-key", name, start, 5000, sumOf(content))
	if got := lineWith(out, "finding\tprivate-key\t"); got != want {
		t.Fatalf("finding line = %q, want %q", got, want)
	}
	if strings.Contains(out, marker) || strings.Contains(errb, marker) {
		t.Fatal("pem marker leaked to the output")
	}
}

// TestContextPEMHeaderCrossingOverlapCRLF: the crossing carry with
// CRLF line breaks — the \r half of each break is consumed with its
// line, a lone trailing \r stays open, and the full-length match
// still lands at the marker.
func TestContextPEMHeaderCrossingOverlapCRLF(t *testing.T) {
	marker := "-----BEGIN " + "RSA " + "PRIVATE KEY-----"
	headers := ""
	for i := 0; i < 100; i++ {
		headers += "Hdr" + string(rune('A'+i%26)) + "-: " + strings.Repeat("v", 41) + "\r\n"
	}
	pem := marker + "\r\n" + headers + strings.Repeat("Z", 80) + "\r\n-----END " + "RSA " + "PRIVATE KEY-----\r\n"
	dir := t.TempDir()
	name := "src/crlfkey"
	start := scanBlock - 5000
	content := strings.Repeat("x", start) + pem
	writeContextFile(t, dir, name, content)
	code, out, errb := runContextCLI(t, dir, "--allow", "src")
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if c := strings.Count(out, "finding\tprivate-key\t"); c != 1 {
		t.Fatalf("private-key findings = %d, want 1:\n%s", c, out)
	}
	want := contentLine("finding", "private-key", name, start, wantLen(t, "private-key", content), sumOf(content))
	if got := lineWith(out, "finding\tprivate-key\t"); got != want {
		t.Fatalf("finding line = %q, want %q", got, want)
	}
	if strings.Contains(out, marker) || strings.Contains(errb, marker) {
		t.Fatal("pem marker leaked to the output")
	}
}

// TestDenyTokenLongerThanWindow: a deny token longer than the scan
// window must not carry a negative tail; the token is still found at
// its absolute offset and an absent token in an equally sized file is
// not reported.
func TestDenyTokenLongerThanWindow(t *testing.T) {
	auxDir := t.TempDir()
	token := strings.Repeat("W", scanBlock+10)
	deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
	t.Run("present", func(t *testing.T) {
		dir := t.TempDir()
		start := 100
		pad := 2*scanBlock - start - len(token)
		content := strings.Repeat("a", start) + token + strings.Repeat("b", pad)
		if len(content) != 2*scanBlock {
			t.Fatalf("content length = %d, want %d", len(content), 2*scanBlock)
		}
		writeContextFile(t, dir, "src/wide.txt", content)
		code, out, errb := runContextCLI(t, dir, "--allow", "src", "--deny-file", deny)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		want := denyLine("deny-token:1", "src/wide.txt", start)
		if got := lineWith(out, "finding\t"); got != want {
			t.Fatalf("finding line = %q, want %q", got, want)
		}
		if strings.Contains(out, token) || strings.Contains(errb, token) {
			t.Fatal("deny token leaked to the output")
		}
	})
	t.Run("absent", func(t *testing.T) {
		dir := t.TempDir()
		content := strings.Repeat("a", 2*scanBlock)
		writeContextFile(t, dir, "src/wide.txt", content)
		code, out, errb := runContextCLI(t, dir, "--allow", "src", "--deny-file", deny)
		if code != 0 {
			t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
		}
		if got := lineWith(out, "finding\t"); got != "" {
			t.Fatalf("unexpected finding line %q in:\n%s", got, out)
		}
	})
}

// TestPEMLaterMarkerCarried: with maxPEMCarry lowered, the first open
// marker fails closed (its open region exceeds the limit) and a second
// marker whose open tail fits the limit is carried to its full length.
func TestPEMLaterMarkerCarried(t *testing.T) {
	saved := maxPEMCarry
	maxPEMCarry = scanOverlap + 100
	t.Cleanup(func() { maxPEMCarry = saved })
	marker := "-----BEGIN " + "RSA " + "PRIVATE KEY-----"
	headers1 := ""
	for i := 0; i < 100; i++ {
		headers1 += "Hdr" + string(rune('A'+i%26)) + "-: " + strings.Repeat("v", 52) + "\n"
	}
	headers2 := ""
	for i := 0; i < 80; i++ {
		headers2 += "Hdr" + string(rune('A'+i%26)) + "-: " + strings.Repeat("w", 48) + "\n"
	}
	pem1 := marker + "\n" + headers1 + "\n"
	pem2 := marker + "\n" + headers2 + strings.Repeat("Z", 80) + "\n-----END " + "RSA " + "PRIVATE KEY-----\n"
	start1 := scanBlock - 5000
	start2 := 2*scanBlock - 4150
	filler := start2 - (start1 + len(pem1))
	content := strings.Repeat("x", start1) + pem1 + strings.Repeat("x", filler) + pem2
	dir := t.TempDir()
	name := "src/pem.txt"
	writeContextFile(t, dir, name, content)
	code, out, errb := runContextCLI(t, dir, "--allow", "src")
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if c := strings.Count(out, "finding\tprivate-key\t"); c != 2 {
		t.Fatalf("private-key findings = %d, want 2:\n%s", c, out)
	}
	// The first marker fails closed at its offset with the length
	// from the marker to the window end.
	want1 := contentLine("finding", "private-key", name, start1, 5000, sumOf(content))
	if !hasLine(out, want1) {
		t.Fatalf("fail-closed line missing in:\n%s", out)
	}
	// The second marker is carried to its full length.
	wantLen2 := len(marker) + 1 + len(headers2) + 81
	want2 := contentLine("finding", "private-key", name, start2, wantLen2, sumOf(content))
	if !hasLine(out, want2) {
		t.Fatalf("full-length line missing in:\n%s", out)
	}
	if strings.Contains(out, marker) || strings.Contains(errb, marker) {
		t.Fatal("pem marker leaked to the output")
	}
}

func TestContextNotAllowlisted(t *testing.T) {
	t.Run("belowAndOutside", func(t *testing.T) {
		dir := t.TempDir()
		writeContextFile(t, dir, "src/a.go", "package a\n")
		writeContextFile(t, dir, "src.txt", "x\n")
		writeContextFile(t, dir, "docs/b.md", "# b\n")
		code, out, errb := runContextCLI(t, dir, "--allow", "src")
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		got := map[string]bool{}
		for _, l := range strings.Split(out, "\n") {
			parts := strings.Split(l, "\t")
			if len(parts) == 4 && parts[0] == "finding" && parts[1] == "not-allowlisted" {
				got[parts[2]] = true
			}
		}
		if !got["src.txt"] || !got["docs/b.md"] || got["src/a.go"] {
			t.Fatalf("not-allowlisted = %v", got)
		}
		if !hasLine(out, "summary\tfiles=3\tbytes=16\tfindings=2\taccepted=0\tunmatched=0") {
			t.Fatalf("summary missing: got %q", out)
		}
	})
	t.Run("prefixEqualsPath", func(t *testing.T) {
		dir := t.TempDir()
		writeContextFile(t, dir, "src", "a file named src\n")
		code, out, errb := runContextCLI(t, dir, "--allow", "src")
		if code != 0 {
			t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
		}
		if lineWith(out, "finding\t") != "" {
			t.Fatalf("unexpected finding in %q", out)
		}
	})
}

func TestContextForbiddenName(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, ".env.local", "X=1\n")
	writeContextFile(t, dir, "Id_RSA", "k\n")
	writeContextFile(t, dir, "key.pem", "pem\n")
	writeContextFile(t, dir, "a/.git/config", "c\n")
	writeContextFile(t, dir, "secrets/.NETRC", "n\n")
	writeContextFile(t, dir, "ok/env.local", "plain\n")
	writeContextFile(t, dir, "ok/keys.txt", "plain\n")
	code, out, errb := runContextCLI(t, dir,
		"--allow", ".env.local", "--allow", "Id_RSA", "--allow", "key.pem",
		"--allow", "a/.git", "--allow", "secrets/.NETRC", "--allow", "ok")
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	got := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		parts := strings.Split(l, "\t")
		if len(parts) == 4 && parts[0] == "finding" && parts[1] == "forbidden-name" && parts[3] == "-" {
			got[parts[2]] = true
		}
	}
	for _, p := range []string{".env.local", "Id_RSA", "key.pem", "a/.git", "a/.git/config", "secrets/.NETRC"} {
		if !got[p] {
			t.Fatalf("missing forbidden-name for %s; got %v", p, got)
		}
	}
	for _, p := range []string{"ok/env.local", "ok/keys.txt"} {
		if got[p] {
			t.Fatalf("unexpected forbidden-name for %s", p)
		}
	}
	if lineWith(out, "finding\tnot-allowlisted\t") != "" {
		t.Fatalf("allow prefixes leaked: %q", out)
	}
}

func TestContextSymlinkNotRead(t *testing.T) {
	dir := t.TempDir()
	content := "k=" + tokXAI() + "\n"
	writeContextFile(t, dir, "real.txt", content)
	if err := os.Symlink("real.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runContextCLI(t, dir, "--allow", "real.txt", "--allow", "link")
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if lineWith(out, "finding\txai-key\tlink\t") != "" {
		t.Fatal("symlink content was read")
	}
	if lineWith(out, "finding\txai-key\treal.txt\t") == "" {
		t.Fatalf("missing finding on the target: %q", out)
	}
	if !hasLine(out, "summary\tfiles=2\tbytes="+strconv.Itoa(len(content))+"\tfindings=1\taccepted=0\tunmatched=0") {
		t.Fatalf("summary missing: got %q", out)
	}
	if strings.Contains(out, tokXAI()) || strings.Contains(errb, tokXAI()) {
		t.Fatal("token leaked to the output")
	}
}

// TestContextPerMatch replaces the old first-match deduplication:
// every match of a rule in one file is reported at its own offset with
// the same file sha256, and accepts bind to the individual match.
func TestContextPerMatch(t *testing.T) {
	dir := t.TempDir()
	tok := tokNPM()
	content := "a " + tok + " b " + tok + "\n"
	name := "src/two.txt"
	writeContextFile(t, dir, name, content)
	code, out, errb := runContextCLI(t, dir, "--allow", "src")
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	off1 := strings.Index(content, tok)
	off2 := off1 + len(tok) + 3
	want1 := contentLine("finding", "npm-token", name, off1, len(tok), sumOf(content))
	want2 := contentLine("finding", "npm-token", name, off2, len(tok), sumOf(content))
	if got := lineWith(out, "finding\tnpm-token\t"); got != want1 {
		t.Fatalf("first finding line = %q, want %q", got, want1)
	}
	if !hasLine(out, want2) {
		t.Fatalf("second finding missing in:\n%s", out)
	}
	if strings.Contains(out, tok) || strings.Contains(errb, tok) {
		t.Fatal("token leaked to the output")
	}

	// Accepting both matches: exit 0.
	code, out, errb = runContextCLI(t, dir, "--allow", "src",
		"--accept", entryOf("npm-token", name, content, tok),
		"--accept", makeEntry("npm-token", name, content, tok, off2, len(tok)))
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	if !hasLine(out, contentLine("accepted", "npm-token", name, off1, len(tok), sumOf(content))) {
		t.Fatalf("first accepted line missing in:\n%s", out)
	}
	if !hasLine(out, contentLine("accepted", "npm-token", name, off2, len(tok), sumOf(content))) {
		t.Fatalf("second accepted line missing in:\n%s", out)
	}
	if strings.Contains(out, "finding\t") {
		t.Fatalf("findings not suppressed:\n%s", out)
	}
	if !hasLine(out, "summary\tfiles=1\tbytes="+strconv.Itoa(len(content))+"\tfindings=0\taccepted=2\tunmatched=0") {
		t.Fatalf("summary missing:\n%s", out)
	}
}

// TestContextPerMatchAccept: two different synthetic aws-access-key
// tokens in one file produce two findings with their own offsets,
// lengths and the same file sha256; accepting only the first exactly
// leaves the second as a finding.
func TestContextPerMatchAccept(t *testing.T) {
	dir := t.TempDir()
	tok1 := "AKIA" + strings.Repeat("K", 16)
	tok2 := "AKIA" + strings.Repeat("M", 16)
	name := "src/keys.txt"
	content := "one=" + tok1 + "\ntwo=" + tok2 + "\n"
	writeContextFile(t, dir, name, content)
	code, out, errb := runContextCLI(t, dir, "--allow", "src")
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	off1 := strings.Index(content, tok1)
	off2 := strings.Index(content, tok2)
	sum := sumOf(content)
	want1 := contentLine("finding", "aws-access-key", name, off1, len(tok1), sum)
	want2 := contentLine("finding", "aws-access-key", name, off2, len(tok2), sum)
	if got := lineWith(out, "finding\t"); got != want1 {
		t.Fatalf("first finding line = %q, want %q", got, want1)
	}
	if !hasLine(out, want2) {
		t.Fatalf("second finding missing in:\n%s", out)
	}
	// Accepting only the first exactly leaves the second as a finding.
	code, out, errb = runContextCLI(t, dir, "--allow", "src",
		"--accept", makeEntry("aws-access-key", name, content, tok1, off1, len(tok1)))
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if !hasLine(out, contentLine("accepted", "aws-access-key", name, off1, len(tok1), sum)) {
		t.Fatalf("accepted line missing in:\n%s", out)
	}
	if !hasLine(out, want2) {
		t.Fatalf("second match must stay a finding:\n%s", out)
	}
	if !hasLine(out, "summary\tfiles=1\tbytes="+strconv.Itoa(len(content))+"\tfindings=1\taccepted=1\tunmatched=0") {
		t.Fatalf("summary missing:\n%s", out)
	}
	for _, tok := range []string{tok1, tok2} {
		if strings.Contains(out, tok) || strings.Contains(errb, tok) {
			t.Fatal("token leaked to the output")
		}
	}
}

// TestAcceptBoundToBytes: an entry built from scan A accepts the
// match; after the file bytes change, the same entry is unmatched and
// both matches are findings.
func TestAcceptBoundToBytes(t *testing.T) {
	dir := t.TempDir()
	tok := tokAWS()
	name := "src/creds.txt"
	content1 := "k=" + tok + "\n"
	writeContextFile(t, dir, name, content1)
	off := strings.Index(content1, tok)
	entry := makeEntry("aws-access-key", name, content1, tok, off, len(tok))

	// The entry built from the current bytes accepts the match.
	code, out, errb := runContextCLI(t, dir, "--allow", "src", "--accept", entry)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	if !hasLine(out, contentLine("accepted", "aws-access-key", name, off, len(tok), sumOf(content1))) {
		t.Fatalf("accepted line missing in:\n%s", out)
	}
	if strings.Contains(out, tok) || strings.Contains(errb, tok) {
		t.Fatal("token leaked to the output")
	}

	// Appending a second real-key-shaped token changes the file bytes:
	// the entry from scan A is unmatched and both matches are findings.
	content2 := content1 + "k2=" + tok + "\n"
	writeContextFile(t, dir, name, content2)
	code, out, errb = runContextCLI(t, dir, "--allow", "src", "--accept", entry)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if got := lineWith(out, "unmatched-accept\t"); got != "unmatched-accept\taccept\t1" {
		t.Fatalf("unmatched-accept line = %q", got)
	}
	sum2 := sumOf(content2)
	off2 := len(content1) + len("k2=")
	wantA := contentLine("finding", "aws-access-key", name, off, len(tok), sum2)
	wantB := contentLine("finding", "aws-access-key", name, off2, len(tok), sum2)
	if got := lineWith(out, "finding\t"); got != wantA {
		t.Fatalf("first finding line = %q, want %q", got, wantA)
	}
	if !hasLine(out, wantB) {
		t.Fatalf("second finding missing in:\n%s", out)
	}
	if !hasLine(out, "summary\tfiles=1\tbytes="+strconv.Itoa(len(content2))+"\tfindings=2\taccepted=0\tunmatched=1") {
		t.Fatalf("summary missing:\n%s", out)
	}
	if strings.Contains(out, tok) || strings.Contains(errb, tok) {
		t.Fatal("token leaked to the output")
	}
}

// TestImageLayerIndexRebind: an entry from a scan of one archive does
// not accept a different token at the same path in another archive.
// The archives do not depend on the rootfs value of the config.
func TestImageLayerIndexRebind(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"diff_ids": []string{}}}
	tokX := tokAnthropic()
	tokY := tokOpenAI()
	bodyX := "path=" + tokX + "\n"
	bodyY := "path=" + tokY + "\n"
	archiveX := writeSavedArchive(t, dir, "a.tar", cfg, [][]testEntry{{{Name: "opt/bin", Body: bodyX}}}, false)
	code, out, errb := runImageCLI(t, archiveX)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	offX := strings.Index(bodyX, tokX)
	entry := makeEntry("anthropic-key", "opt/bin", bodyX, tokX, offX, len(tokX))
	if !hasLine(out, contentLine("finding", "anthropic-key", "layer0:opt/bin", offX, len(tokX), sumOf(bodyX))) {
		t.Fatalf("finding missing in:\n%s", out)
	}

	archiveY := writeSavedArchive(t, dir, "b.tar", cfg, [][]testEntry{{{Name: "opt/bin", Body: bodyY}}}, false)
	code, out, errb = runImageCLI(t, archiveY, "--accept", entry)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	offY := strings.Index(bodyY, tokY)
	if !hasLine(out, contentLine("finding", "openai-key", "layer0:opt/bin", offY, len(tokY), sumOf(bodyY))) {
		t.Fatalf("Y finding missing in:\n%s", out)
	}
	if got := lineWith(out, "unmatched-accept\t"); got != "unmatched-accept\taccept\t1" {
		t.Fatalf("unmatched-accept line = %q", got)
	}
	for _, tok := range []string{tokX, tokY} {
		if strings.Contains(out, tok) || strings.Contains(errb, tok) {
			t.Fatal("token leaked to the output")
		}
	}
}

// TestImageSameNameAcrossLayers: the same key path in two different
// layers with different bytes yields two separate findings — no
// cross-file deduplication — and an accept derived from the layer-0
// match accepts only that finding while the layer-1 finding stays
// active.
func TestImageSameNameAcrossLayers(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"diff_ids": []string{}}}
	tok0 := "AKIA" + strings.Repeat("P", 16)
	tok1 := "AKIA" + strings.Repeat("Q", 16)
	body0 := "path=" + tok0 + "\n"
	body1 := "path=" + tok1 + "\n"
	layers := [][]testEntry{
		{{Name: "opt/bin", Body: body0}},
		{{Name: "opt/bin", Body: body1}},
	}
	archive := writeSavedArchive(t, dir, "img.tar", cfg, layers, false)
	off := strings.Index(body0, tok0)
	l0 := contentLine("finding", "aws-access-key", "layer0:opt/bin", off, len(tok0), sumOf(body0))
	l1 := contentLine("finding", "aws-access-key", "layer1:opt/bin", off, len(tok1), sumOf(body1))
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if !hasLine(out, l0) || !hasLine(out, l1) {
		t.Fatalf("missing layer findings in:\n%s", out)
	}
	if c := strings.Count(out, "finding\taws-access-key\t"); c != 2 {
		t.Fatalf("aws-access-key findings = %d, want 2:\n%s", c, out)
	}
	// An accept derived from the layer-0 match accepts only that
	// finding; the layer-1 finding stays active.
	entry := makeEntry("aws-access-key", "opt/bin", body0, tok0, off, len(tok0))
	code, out, errb = runImageCLI(t, archive, "--accept", entry)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if !hasLine(out, contentLine("accepted", "aws-access-key", "layer0:opt/bin", off, len(tok0), sumOf(body0))) {
		t.Fatalf("layer0 accepted line missing in:\n%s", out)
	}
	if !hasLine(out, l1) {
		t.Fatalf("layer1 line must stay a finding:\n%s", out)
	}
	for _, tok := range []string{tok0, tok1} {
		if strings.Contains(out, tok) || strings.Contains(errb, tok) {
			t.Fatal("token leaked to the output")
		}
	}
}

// TestImageSameNameTwiceInOneLayer: a tar layer holding two same-name
// entries with different bytes yields two findings of the same
// display; an accept binds to the first entry's sha256 and leaves the
// second active.
func TestImageSameNameTwiceInOneLayer(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"diff_ids": []string{}}}
	tok0 := "AKIA" + strings.Repeat("R", 16)
	tok1 := "AKIA" + strings.Repeat("S", 16)
	body0 := "path=" + tok0 + "\n"
	body1 := "path=" + tok1 + "\n"
	layers := [][]testEntry{
		{{Name: "opt/bin", Body: body0}, {Name: "opt/bin", Body: body1}},
	}
	archive := writeSavedArchive(t, dir, "img.tar", cfg, layers, false)
	off := strings.Index(body0, tok0)
	f0 := contentLine("finding", "aws-access-key", "layer0:opt/bin", off, len(tok0), sumOf(body0))
	f1 := contentLine("finding", "aws-access-key", "layer0:opt/bin", off, len(tok1), sumOf(body1))
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if !hasLine(out, f0) || !hasLine(out, f1) {
		t.Fatalf("missing same-name findings in:\n%s", out)
	}
	if c := strings.Count(out, "finding\taws-access-key\t"); c != 2 {
		t.Fatalf("aws-access-key findings = %d, want 2:\n%s", c, out)
	}
	entry := makeEntry("aws-access-key", "opt/bin", body0, tok0, off, len(tok0))
	code, out, errb = runImageCLI(t, archive, "--accept", entry)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if !hasLine(out, contentLine("accepted", "aws-access-key", "layer0:opt/bin", off, len(tok0), sumOf(body0))) {
		t.Fatalf("first entry accepted line missing in:\n%s", out)
	}
	if !hasLine(out, f1) {
		t.Fatalf("second entry line must stay a finding:\n%s", out)
	}
	for _, tok := range []string{tok0, tok1} {
		if strings.Contains(out, tok) || strings.Contains(errb, tok) {
			t.Fatal("token leaked to the output")
		}
	}
}

// TestStaleEntryUnmatched: a well-formed entry that matches nothing
// gives an unmatched-accept and the real finding stays.
func TestStaleEntryUnmatched(t *testing.T) {
	dir := t.TempDir()
	tok := tokNPM()
	name := "src/stale.txt"
	content := "k=" + tok + "\n"
	writeContextFile(t, dir, name, content)
	off := strings.Index(content, tok)
	// Well-formed, but the sha256 is of bytes this scan never saw: the
	// file changed after the review.
	stale := makeEntry("npm-token", name, content+"changed\n", tok, off, len(tok))
	code, out, errb := runContextCLI(t, dir, "--allow", "src", "--accept", stale)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if got := lineWith(out, "unmatched-accept\t"); got != "unmatched-accept\taccept\t1" {
		t.Fatalf("unmatched-accept line = %q", got)
	}
	if !hasLine(out, contentLine("finding", "npm-token", name, off, len(tok), sumOf(content))) {
		t.Fatalf("real finding missing in:\n%s", out)
	}
	if !hasLine(out, "summary\tfiles=1\tbytes="+strconv.Itoa(len(content))+"\tfindings=1\taccepted=0\tunmatched=1") {
		t.Fatalf("summary missing:\n%s", out)
	}
	if strings.Contains(out, tok) || strings.Contains(errb, tok) {
		t.Fatal("token leaked to the output")
	}
}

func TestContextAccept(t *testing.T) {
	dir := t.TempDir()
	token := tokAnthropic()
	name := "src/creds.txt"
	content := "k=" + token + "\n"
	writeContextFile(t, dir, name, content)
	off := strings.Index(content, token)
	entry := entryOf("anthropic-key", name, content, token)

	// Baseline: a finding and exit 1.
	code, out, errb := runContextCLI(t, dir, "--allow", "src")
	if code != 1 {
		t.Fatalf("baseline code = %d, want 1; stderr=%s", code, errb)
	}
	if !hasLine(out, contentLine("finding", "anthropic-key", name, off, len(token), sumOf(content))) {
		t.Fatalf("baseline finding missing: %q", out)
	}

	// Accepted: the same match with the accepted kind and exit 0.
	code, out, errb = runContextCLI(t, dir, "--allow", "src", "--accept", entry)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	if !hasLine(out, contentLine("accepted", "anthropic-key", name, off, len(token), sumOf(content))) {
		t.Fatalf("accepted line missing: %q", out)
	}
	if strings.Contains(out, "finding\t") {
		t.Fatalf("finding not suppressed: %q", out)
	}
	if !hasLine(out, "summary\tfiles=1\tbytes="+strconv.Itoa(len(content))+"\tfindings=0\taccepted=1\tunmatched=0") {
		t.Fatalf("summary missing: %q", out)
	}
	if strings.Contains(out, token) || strings.Contains(errb, token) {
		t.Fatal("token leaked to the output")
	}
}

// TestDenyTokenCannotBeAccepted: a deny token can no longer be
// accepted; the old path=rule accept form is a usage error that names
// the new format.
func TestDenyTokenCannotBeAccepted(t *testing.T) {
	dir := t.TempDir()
	auxDir := t.TempDir()
	writeContextFile(t, dir, "src/data.txt", "x\n")
	denyTok := strings.Repeat("M", 16)
	deny := writeAuxFile(t, auxDir, "deny.txt", denyTok+"\n")
	// The old src/data.txt=deny-token:1 accept test is now a usage
	// error: deny tokens can never be accepted.
	code, out, errb := runContextCLI(t, dir, "--allow", "src", "--deny-file", deny,
		"--accept", "deny-token:1 3 16 sha256:"+strings.Repeat("0", 64)+" src/data.txt")
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%s", code, errb)
	}
	if !strings.Contains(errb, "cannot be accepted") {
		t.Fatalf("stderr should say the rule cannot be accepted: %q", errb)
	}
	// The old path=rule form fails the validation too, and the message
	// must say the format changed.
	code, out, errb = runContextCLI(t, dir, "--allow", "src", "--accept", "src/data.txt=deny-token:1")
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%s", code, errb)
	}
	if !strings.Contains(errb, "<rule> <offset> <length> sha256:<file-sha256> <path>") {
		t.Fatalf("stderr should name the new format: %q", errb)
	}
	if strings.Contains(errb, denyTok) || strings.Contains(out, denyTok) {
		t.Fatal("deny token leaked to the output")
	}
}

// TestAcceptEntryValidation covers the usage errors of the accept
// entry format: the old path=rule form, non-acceptable rules, bad
// offset/length/sha, the old layer-index path and duplicates. Every
// message cites the line or the flag index only, never the entry.
func TestAcceptEntryValidation(t *testing.T) {
	dir := t.TempDir()
	auxDir := t.TempDir()
	writeContextFile(t, dir, "src/a.txt", "x\n")
	sha := "sha256:" + strings.Repeat("0", 64)
	good := "npm-token 3 4 " + sha + " src/a.txt"
	cases := []struct {
		name  string
		entry string
		want  string // a substring of stderr
	}{
		{"old form", "src/a.txt=npm-token", "sha256:<file-sha256>"},
		{"rule deny-token", "deny-token:1 3 4 " + sha + " src/a.txt", "cannot be accepted"},
		{"rule credential-path", "credential-path 3 4 " + sha + " src/a.txt", "cannot be accepted"},
		{"rule git-dir", "git-dir 3 4 " + sha + " src/a.txt", "cannot be accepted"},
		{"rule forbidden-name", "forbidden-name 3 4 " + sha + " src/a.txt", "cannot be accepted"},
		{"rule not-allowlisted", "not-allowlisted 3 4 " + sha + " src/a.txt", "cannot be accepted"},
		{"rule config-env-secret", "config-env-secret:API_TOKEN 3 4 " + sha + " src/a.txt", "cannot be accepted"},
		{"bad offset negative", "npm-token -1 4 " + sha + " src/a.txt", "offset"},
		{"bad offset non-numeric", "npm-token 1.5 4 " + sha + " src/a.txt", "offset"},
		{"bad length zero", "npm-token 3 0 " + sha + " src/a.txt", "length"},
		{"bad length non-numeric", "npm-token 3 x " + sha + " src/a.txt", "length"},
		{"bad sha short", "npm-token 3 4 sha256:" + strings.Repeat("0", 63) + " src/a.txt", "sha256"},
		{"bad sha uppercase", "npm-token 3 4 sha256:" + strings.Repeat("A", 64) + " src/a.txt", "sha256"},
		{"bad path layer index", "npm-token 3 4 " + sha + " layer3:etc/data", "layer"},
		{"too few fields", "npm-token 3 4", "5 fields"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errb := runContextCLI(t, dir, "--allow", "src", "--accept", c.entry)
			if code != 2 {
				t.Fatalf("code = %d, want 2; stderr=%s", code, errb)
			}
			if !strings.Contains(errb, c.want) {
				t.Fatalf("stderr = %q, want a mention of %q", errb, c.want)
			}
			if !strings.Contains(errb, "--accept 1") {
				t.Fatalf("stderr should cite the flag index: %q", errb)
			}
			if strings.Contains(errb, c.entry) || strings.Contains(out, c.entry) {
				t.Fatal("entry text leaked to the output")
			}
		})
	}
	// A malformed accept-file line cites the line number only.
	t.Run("accept file cites line", func(t *testing.T) {
		accept := writeAuxFile(t, auxDir, "accept.txt", "# ok\nsrc/a.txt=npm-token\n")
		code, out, errb := runContextCLI(t, dir, "--allow", "src", "--accept-file", accept)
		if code != 2 {
			t.Fatalf("code = %d, want 2; stderr=%s", code, errb)
		}
		if !strings.Contains(errb, "line 2") {
			t.Fatalf("stderr should cite the line number: %q", errb)
		}
		if !strings.Contains(errb, "sha256:<file-sha256>") {
			t.Fatalf("stderr should name the new format: %q", errb)
		}
		if strings.Contains(errb, "src/a.txt=npm-token") || strings.Contains(out, "src/a.txt=npm-token") {
			t.Fatal("entry text leaked to the output")
		}
	})
	// The same five fields twice, across file and flags, is a usage
	// error.
	t.Run("duplicate", func(t *testing.T) {
		accept := writeAuxFile(t, auxDir, "accept.txt", "# ok\n"+good+"\n")
		code, out, errb := runContextCLI(t, dir, "--allow", "src", "--accept-file", accept, "--accept", good)
		if code != 2 {
			t.Fatalf("code = %d, want 2; stderr=%s", code, errb)
		}
		if !strings.Contains(errb, "duplicate") || !strings.Contains(errb, "line 2") || !strings.Contains(errb, "flag 1") {
			t.Fatalf("stderr = %q", errb)
		}
		if strings.Contains(errb, good) || strings.Contains(out, good) {
			t.Fatal("entry text leaked to the output")
		}
	})
}

func TestContextFlagsBeforeAndAfterPositional(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "src/a.txt", "x\n")
	after, outAfter, errbAfter := runContextCLI(t, dir, "--allow", "src")
	if after != 0 {
		t.Fatalf("flags-after code = %d, want 0; stderr=%s", after, errbAfter)
	}
	before, outBefore, errbBefore := runCLI([]string{"context", "--allow", "src", dir})
	if before != 0 {
		t.Fatalf("flags-before code = %d, want 0; stderr=%s", before, errbBefore)
	}
	if outBefore != outAfter {
		t.Fatalf("flag position changed the output:\n%s\n%s", outBefore, outAfter)
	}
}

func TestContextAndImageUsageErrors(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "src/a.txt", "x\n")
	cases := []struct {
		name string
		args []string
	}{
		{"no subcommand", []string{}},
		{"context missing dir", []string{"context"}},
		{"context without allow", []string{"context", dir}},
		{"context two positionals", []string{"context", dir, dir, "--allow", "src"}},
		{"context unknown flag", []string{"context", dir, "--allow", "src", "--bogus"}},
		{"context allow without value", []string{"context", dir, "--allow"}},
		{"context accept bad sha", []string{"context", dir, "--allow", "src", "--accept",
			"npm-token 1 1 sha256:" + strings.Repeat("0", 63) + " src/a.txt"}},
		{"context accept old form", []string{"context", dir, "--allow", "src", "--accept", "src/a.txt=npm-token"}},
		{"image missing archive", []string{"image"}},
		{"image takes no allow", []string{"image", dir, "--allow", "x"}},
		{"context accept-file without value", []string{"context", dir, "--allow", "src", "--accept-file"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, _ := runCLI(c.args)
			if code != 2 {
				t.Fatalf("code = %d, want 2", code)
			}
		})
	}
}

func TestContextAndImageIOErrors(t *testing.T) {
	dir := t.TempDir()
	cases := [][]string{
		{"context", filepath.Join(dir, "missing"), "--allow", "src"},
		{"image", filepath.Join(dir, "missing.tar")},
		{"context", dir, "--allow", "src", "--deny-file", filepath.Join(dir, "nope.txt")},
	}
	for i, args := range cases {
		code, _, _ := runCLI(args)
		if code != 4 {
			t.Fatalf("case %d: code = %d, want 4", i, code)
		}
	}
}

func TestImageCredentialPath(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"diff_ids": []string{}}}
	b1 := "registry=https://example.invalid/\n"
	b2 := "host example\n"
	b3 := "[default]\n"
	b4 := "example.invalid:\n"
	b5 := "127.0.0.1 localhost\n"
	b6 := "bak\n"
	layers := [][]testEntry{
		{
			{Name: "home/", Dir: true},
			{Name: "home/agent/", Dir: true},
			{Name: "home/agent/.npmrc", Body: b1},
			{Name: "home/agent/.ssh/", Dir: true},
			{Name: "home/agent/.ssh/known_hosts", Body: b2},
			{Name: "root/.aws/credentials", Body: b3},
			{Name: "home/bob/.config/gh/hosts.yml", Body: b4},
			{Name: "home/agent/.wh.npmrc", Body: "whiteout\n"},
			{Name: "etc/hosts", Body: b5},
			{Name: "home/agent/.npmrc.bak", Body: b6},
		},
	}
	archive := writeSavedArchive(t, dir, "img.tar", cfg, layers, false)
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	for _, w := range []string{
		"finding\tcredential-path\tlayer0:home/agent/.npmrc\t-",
		"finding\tcredential-path\tlayer0:home/agent/.ssh/known_hosts\t-",
		"finding\tcredential-path\tlayer0:root/.aws/credentials\t-",
		"finding\tcredential-path\tlayer0:home/bob/.config/gh/hosts.yml\t-",
	} {
		if !hasLine(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
	for _, bad := range []string{".wh.npmrc", "etc/hosts", ".npmrc.bak"} {
		if strings.Contains(out, bad) {
			t.Fatalf("unexpected %q flagged in:\n%s", bad, out)
		}
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The non-empty .wh.npmrc is not a whiteout marker, so it is scanned
	// and counted; the archive's index files (manifest.json) are scanned
	// too.
	nIdx, idxBytes := indexFilesSize(t, archive)
	nbytes := len(cfgBytes) + len(b1) + len(b2) + len(b3) + len(b4) + len(b5) + len(b6) + len("whiteout\n") + idxBytes
	if !hasLine(out, "summary\tfiles="+strconv.Itoa(8+nIdx)+"\tbytes="+strconv.Itoa(nbytes)+"\tfindings=4\taccepted=0\tunmatched=0") {
		t.Fatalf("summary missing in:\n%s", out)
	}
}

func TestImageGitDir(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"diff_ids": []string{}}}
	layers := [][]testEntry{
		{
			{Name: "opt/app/.git/", Dir: true},
			{Name: "opt/app/.git/HEAD", Body: "ref: refs/heads/main\n"},
			{Name: "opt/app/main.go", Body: "package app\n"},
		},
	}
	archive := writeSavedArchive(t, dir, "img.tar", cfg, layers, false)
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	for _, w := range []string{
		"finding\tgit-dir\tlayer0:opt/app/.git\t-",
		"finding\tgit-dir\tlayer0:opt/app/.git/HEAD\t-",
	} {
		if !hasLine(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "opt/app/main.go") {
		t.Fatalf("main.go flagged in:\n%s", out)
	}
}

func TestImageConfigEnvSecret(t *testing.T) {
	dir := t.TempDir()
	secretValue := "v-" + strings.Repeat("s", 16)
	cfg := map[string]any{
		"config": map[string]any{
			"Env": []string{
				"API_TOKEN=" + secretValue,
				"PATH=/usr/bin",
				"MY_SECRET=", // empty value: not a finding
				"DB_PASSWORD=pw",
				"api_key=k", // case-insensitive match
			},
		},
		"rootfs": map[string]any{"diff_ids": []string{}},
	}
	archive := writeSavedArchive(t, dir, "img.tar", cfg, nil, false)
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	for _, w := range []string{
		"finding\tconfig-env-secret:API_TOKEN\tconfig\t-",
		"finding\tconfig-env-secret:DB_PASSWORD\tconfig\t-",
		"finding\tconfig-env-secret:api_key\tconfig\t-",
	} {
		if !hasLine(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
	for _, bad := range []string{"config-env-secret:PATH", "config-env-secret:MY_SECRET", secretValue} {
		if strings.Contains(out, bad) || strings.Contains(errb, bad) {
			t.Fatalf("unexpected %q in the output", bad)
		}
	}
}

func TestImageHostPath(t *testing.T) {
	base := map[string]any{"diff_ids": []string{}}
	t.Run("darwin", func(t *testing.T) {
		dir := t.TempDir()
		cfg := map[string]any{
			"config": map[string]any{
				"Env": []string{"HOME=/home/agent"},
				"Cmd": []string{"sh", "-c", "cat /Users/someone/x"},
			},
			"rootfs": base,
		}
		archive := writeSavedArchive(t, dir, "img.tar", cfg, nil, false)
		code, out, errb := runImageCLI(t, archive)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		cfgBytes, _ := json.Marshal(cfg)
		off := bytes.Index(cfgBytes, []byte("/Users/someone"))
		want := contentLine("finding", "host-path", "config", off, len("/Users/someone"), sumOf(string(cfgBytes)))
		if got := lineWith(out, "finding\thost-path\t"); got != want {
			t.Fatalf("host-path line = %q, want %q", got, want)
		}
		if strings.Count(out, "host-path") != 1 {
			t.Fatalf("/home/agent must not add a finding:\n%s", out)
		}
	})
	t.Run("windows", func(t *testing.T) {
		dir := t.TempDir()
		// One real backslash per separator in the value; the JSON
		// text escapes them to double backslashes.
		value := "C:" + `\` + "Users" + `\` + "dj"
		cfg := map[string]any{
			"config": map[string]any{
				"Env": []string{"PROFILE=" + value},
			},
			"rootfs": base,
		}
		archive := writeSavedArchive(t, dir, "img.tar", cfg, nil, false)
		code, out, errb := runImageCLI(t, archive)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		cfgBytes, _ := json.Marshal(cfg)
		needle := []byte(`C:\\Users\\`)
		off := bytes.Index(cfgBytes, needle)
		want := contentLine("finding", "host-path", "config", off, len(needle), sumOf(string(cfgBytes)))
		if got := lineWith(out, "finding\thost-path\t"); got != want {
			t.Fatalf("host-path line = %q, want %q", got, want)
		}
	})
	t.Run("linux", func(t *testing.T) {
		dir := t.TempDir()
		cfg := map[string]any{
			"config": map[string]any{
				"Env": []string{"HOME=/home/agent", "DATA=/home/data"},
			},
			"rootfs": base,
		}
		archive := writeSavedArchive(t, dir, "img.tar", cfg, nil, false)
		code, out, errb := runImageCLI(t, archive)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		cfgBytes, _ := json.Marshal(cfg)
		needle := []byte("/home/data")
		off := bytes.Index(cfgBytes, needle)
		want := contentLine("finding", "host-path", "config", off, len(needle), sumOf(string(cfgBytes)))
		if got := lineWith(out, "finding\thost-path\t"); got != want {
			t.Fatalf("host-path line = %q, want %q (excluded /home/agent would win first)", got, want)
		}
	})
	t.Run("homeAgentOnly", func(t *testing.T) {
		dir := t.TempDir()
		cfg := map[string]any{
			"config": map[string]any{
				"Env": []string{"HOME=/home/agent"},
			},
			"rootfs": base,
		}
		archive := writeSavedArchive(t, dir, "img.tar", cfg, nil, false)
		code, out, errb := runImageCLI(t, archive)
		if code != 0 {
			t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
		}
		if lineWith(out, "finding\t") != "" {
			t.Fatalf("unexpected finding in:\n%s", out)
		}
	})
}

func TestImageConfigContentAndDeny(t *testing.T) {
	dir := t.TempDir()
	auxDir := t.TempDir()
	slack := tokSlack()
	cfg := map[string]any{
		"config":  map[string]any{},
		"rootfs":  map[string]any{"diff_ids": []string{}},
		"history": []map[string]any{{"created_by": "RUN echo " + slack}},
	}
	denyTok := strings.Repeat("Q", 20)
	deny := writeAuxFile(t, auxDir, "deny.txt", denyTok+"\n")
	layBody := "pre " + denyTok + " post\n"
	layers := [][]testEntry{{{Name: "etc/data", Body: layBody}}}
	archive := writeSavedArchive(t, dir, "img.tar", cfg, layers, false)
	code, out, errb := runImageCLI(t, archive, "--deny-file", deny)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	cfgBytes, _ := json.Marshal(cfg)
	offCfg := bytes.Index(cfgBytes, []byte(slack))
	wantCfg := contentLine("finding", "slack-token", "config", offCfg, wantLen(t, "slack-token", string(cfgBytes)), sumOf(string(cfgBytes)))
	if got := lineWith(out, "finding\tslack-token\tconfig\t"); got != wantCfg {
		t.Fatalf("config finding = %q, want %q", got, wantCfg)
	}
	wantLayer := denyLine("deny-token:1", "layer0:etc/data", strings.Index(layBody, denyTok))
	if got := lineWith(out, "finding\tdeny-token:1\t"); got != wantLayer {
		t.Fatalf("layer finding = %q, want %q", got, wantLayer)
	}
	if strings.Contains(out, slack) || strings.Contains(out, denyTok) || strings.Contains(errb, denyTok) {
		t.Fatal("token leaked to the output")
	}
	nIdx, idxBytes := indexFilesSize(t, archive)
	if !hasLine(out, "summary\tfiles="+strconv.Itoa(2+nIdx)+"\tbytes="+strconv.Itoa(len(cfgBytes)+len(layBody)+idxBytes)+"\tfindings=2\taccepted=0\tunmatched=0") {
		t.Fatalf("summary missing in:\n%s", out)
	}
}

func TestImageGzipLayers(t *testing.T) {
	dir := t.TempDir()
	tok := tokNPM()
	cfg := map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"diff_ids": []string{}}}
	layers := [][]testEntry{
		{{Name: "etc/data", Body: "t=" + tok + "\n"}},
		{{Name: "home/agent/.npmrc", Body: "registry=off\n"}},
	}
	archive := writeSavedArchive(t, dir, "img.tar", cfg, layers, true)
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if lineWith(out, "finding\tnpm-token\tlayer0:etc/data\t") == "" {
		t.Fatalf("npm token not found in gzip layer:\n%s", out)
	}
	if !hasLine(out, "finding\tcredential-path\tlayer1:home/agent/.npmrc\t-") {
		t.Fatalf("credential-path missing:\n%s", out)
	}
	if strings.Contains(out, tok) || strings.Contains(errb, tok) {
		t.Fatal("token leaked to the output")
	}
}

func TestContextAcceptFile(t *testing.T) {
	dir := t.TempDir()
	auxDir := t.TempDir()
	name1 := "src/a.txt"
	content1 := "k=" + tokAnthropic() + "\n"
	writeContextFile(t, dir, name1, content1)
	name2 := "src/b.txt"
	content2 := "k=" + tokXAI() + "\n"
	writeContextFile(t, dir, name2, content2)
	// line 1: comment, line 2: blank, line 3: padded valid entry
	accept := writeAuxFile(t, auxDir, "accept.txt", "# reviewed\n\n  "+entryOf("anthropic-key", name1, content1, tokAnthropic())+"  \n")
	code, out, errb := runContextCLI(t, dir, "--allow", "src",
		"--accept-file", accept, "--accept", entryOf("xai-key", name2, content2, tokXAI()))
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	off1 := strings.Index(content1, tokAnthropic())
	if !hasLine(out, contentLine("accepted", "anthropic-key", name1, off1, len(tokAnthropic()), sumOf(content1))) {
		t.Fatalf("file accept not applied:\n%s", out)
	}
	off2 := strings.Index(content2, tokXAI())
	if !hasLine(out, contentLine("accepted", "xai-key", name2, off2, len(tokXAI()), sumOf(content2))) {
		t.Fatalf("cli accept not applied:\n%s", out)
	}
	if strings.Contains(out, "finding\t") {
		t.Fatalf("findings not suppressed:\n%s", out)
	}
	if !hasLine(out, "summary\tfiles=2\tbytes="+strconv.Itoa(len(content1)+len(content2))+"\tfindings=0\taccepted=2\tunmatched=0") {
		t.Fatalf("summary missing:\n%s", out)
	}
	for _, tok := range []string{tokAnthropic(), tokXAI()} {
		if strings.Contains(out, tok) || strings.Contains(errb, tok) {
			t.Fatal("token leaked to the output")
		}
	}
}

func TestAcceptFileMalformed(t *testing.T) {
	dir := t.TempDir()
	auxDir := t.TempDir()
	writeContextFile(t, dir, "src/a.txt", "x\n")
	// The old path=rule form fails the validation; the message names
	// the new format and cites the line number only.
	accept := writeAuxFile(t, auxDir, "accept.txt", "# ok\nsrc/a.txt=npm-token\n")
	code, out, errb := runContextCLI(t, dir, "--allow", "src", "--accept-file", accept)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%s", code, errb)
	}
	if !strings.Contains(errb, "line 2") {
		t.Fatalf("stderr should cite the line number: %q", errb)
	}
	if !strings.Contains(errb, "<rule> <offset> <length> sha256:<file-sha256> <path>") {
		t.Fatalf("stderr should name the new format: %q", errb)
	}
	if strings.Contains(errb, "src/a.txt=npm-token") || strings.Contains(out, "src/a.txt=npm-token") {
		t.Fatal("entry text leaked to the output")
	}
}

func TestAcceptFileMissing(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "src/a.txt", "x\n")
	code, _, _ := runContextCLI(t, dir, "--allow", "src", "--accept-file", filepath.Join(dir, "nope.txt"))
	if code != 4 {
		t.Fatalf("code = %d, want 4", code)
	}
}

func TestAcceptFileRepeated(t *testing.T) {
	dir := t.TempDir()
	auxDir := t.TempDir()
	writeContextFile(t, dir, "src/a.txt", "x\n")
	a := writeAuxFile(t, auxDir, "a.txt", "")
	b := writeAuxFile(t, auxDir, "b.txt", "")
	code, _, errb := runContextCLI(t, dir, "--allow", "src", "--accept-file", a, "--accept-file", b)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%s", code, errb)
	}
}

func TestImageAcceptFile(t *testing.T) {
	dir := t.TempDir()
	auxDir := t.TempDir()
	tok := tokNPM()
	body := "t=" + tok + "\n"
	cfg := map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"diff_ids": []string{}}}
	layers := [][]testEntry{{{Name: "etc/data", Body: body}}}
	archive := writeSavedArchive(t, dir, "img.tar", cfg, layers, false)
	// The entry names the entry name without the layer index.
	off := strings.Index(body, tok)
	accept := writeAuxFile(t, auxDir, "accept.txt", "# ok\n"+makeEntry("npm-token", "etc/data", body, tok, off, len(tok))+"\n")
	code, out, errb := runImageCLI(t, archive, "--accept-file", accept)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	if !hasLine(out, contentLine("accepted", "npm-token", "layer0:etc/data", off, len(tok), sumOf(body))) {
		t.Fatalf("accepted line missing:\n%s", out)
	}
	if strings.Contains(out, tok) || strings.Contains(errb, tok) {
		t.Fatal("token leaked to stdout")
	}
}

func TestImageAcceptFileOldFormRejected(t *testing.T) {
	dir := t.TempDir()
	auxDir := t.TempDir()
	tok := tokNPM()
	cfg := map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"diff_ids": []string{}}}
	layers := [][]testEntry{{{Name: "etc/data", Body: "t=" + tok + "\n"}}}
	archive := writeSavedArchive(t, dir, "img.tar", cfg, layers, false)
	accept := writeAuxFile(t, auxDir, "accept.txt", "# ok\nlayer0:etc/data=npm-token\n")
	code, out, errb := runImageCLI(t, archive, "--accept-file", accept)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%s", code, errb)
	}
	if !strings.Contains(errb, "line 2") {
		t.Fatalf("stderr should cite the line number: %q", errb)
	}
	if strings.Contains(errb, tok) || strings.Contains(out, tok) {
		t.Fatal("token leaked to the output")
	}
}

func TestImageInvalidConfigFails(t *testing.T) {
	dir := t.TempDir()
	// A trailing comma makes the config unparseable; the scan must not
	// pass a config it could not read for secret Env entries.
	secretValue := "v-" + strings.Repeat("q", 16)
	cfg := []byte(`{"config":{"Env":["API_TOKEN=` + secretValue + `",]},"rootfs":{"diff_ids":[]}}`)
	manifest := []byte(`[{"Config":"blobs/sha256/config","Layers":[]}]`)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range []struct {
		name string
		body []byte
	}{{"blobs/sha256/config", cfg}, {"manifest.json", manifest}} {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "img.tar")
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runImageCLI(t, archive)
	if code != 4 {
		t.Fatalf("code = %d, want 4; stdout=%q stderr=%q", code, out, errb)
	}
	if strings.Contains(out, secretValue) || strings.Contains(errb, secretValue) {
		t.Fatal("config value leaked to the output")
	}
}

func TestContextDenyTokenLongerThanOverlap(t *testing.T) {
	ctxDir := t.TempDir()
	auxDir := t.TempDir()
	token := strings.Repeat("L", scanOverlap+904) // longer than the default tail
	deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
	// Starts before the default 4 KiB tail of the first window and ends
	// after the first block, so only a tail as long as the token keeps it
	// whole inside one window.
	start := scanBlock - scanOverlap - 100
	content := strings.Repeat("a", start) + token + strings.Repeat("b", 100)
	writeContextFile(t, ctxDir, "src/big.txt", content)
	code, out, errb := runContextCLI(t, ctxDir, "--allow", "src", "--deny-file", deny)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	want := denyLine("deny-token:1", "src/big.txt", start)
	if got := lineWith(out, "finding\t"); got != want {
		t.Fatalf("finding line = %q, want %q", got, want)
	}
	if strings.Contains(out, token) || strings.Contains(errb, token) {
		t.Fatal("deny token leaked to the output")
	}
}

// indexFilesSize returns how many archive index files (img.Others) the
// image scan reads from archive and their total size.
func indexFilesSize(t *testing.T, archive string) (int, int) {
	t.Helper()
	img, err := openSaved(archive)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, o := range img.Others {
		n += len(o.Data)
	}
	return len(img.Others), n
}
