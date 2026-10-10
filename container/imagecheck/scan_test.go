package main

import (
	"archive/tar"
	"bytes"
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

// writeAuxFile writes one auxiliary file (deny-file) outside the tree
// being scanned.
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

// hasLine reports whether out contains the exact line.
func hasLine(out, line string) bool {
	return strings.Contains(out, line+"\n")
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
			want := "finding\t" + c.rule + "\t" + name + "\t" + strconv.Itoa(off)
			if got := lineWith(out, "finding\t"); got != want {
				t.Fatalf("finding line = %q, want %q", got, want)
			}
			wantSum := "summary\tfiles=1\tbytes=" + strconv.Itoa(len(content)) + "\tfindings=1\taccepted=0"
			if !hasLine(out, wantSum) {
				t.Fatalf("summary missing: got %q", out)
			}
			if strings.Contains(out, c.token()) {
				t.Fatal("matched content leaked to stdout")
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
			wantSum := "summary\tfiles=1\tbytes=" + strconv.Itoa(len(c.content)) + "\tfindings=0\taccepted=0"
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
	want := "finding\tdeny-token:3\t" + name + "\t" + strconv.Itoa(strings.Index(content, token))
	if got := lineWith(out, "finding\t"); got != want {
		t.Fatalf("finding line = %q, want %q", got, want)
	}
	if strings.Contains(out, token) || strings.Contains(errb, token) {
		t.Fatal("deny token leaked to the output")
	}
	if !hasLine(out, "summary\tfiles=1\tbytes="+strconv.Itoa(len(content))+"\tfindings=1\taccepted=0") {
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
			want := "finding\txai-key\tsrc/big.log\t" + strconv.Itoa(tc.start)
			if got := lineWith(out, "finding\t"); got != want {
				t.Fatalf("finding line = %q, want %q", got, want)
			}
			wantSum := "summary\tfiles=1\tbytes=" + strconv.Itoa(tc.start+len(token)) + "\tfindings=1\taccepted=0"
			if !hasLine(out, wantSum) {
				t.Fatalf("summary missing: got %q", out)
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
	want := "finding\tprivate-key\tsrc/big.pem\t" + strconv.Itoa(start)
	if got := lineWith(out, "finding\tprivate-key\t"); got != want {
		t.Fatalf("pem crossing: finding line = %q, want %q", got, want)
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
		if !hasLine(out, "summary\tfiles=3\tbytes=16\tfindings=2\taccepted=0") {
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
	if !hasLine(out, "summary\tfiles=2\tbytes="+strconv.Itoa(len(content))+"\tfindings=1\taccepted=0") {
		t.Fatalf("summary missing: got %q", out)
	}
}

func TestContextDedupesByRuleAndPath(t *testing.T) {
	dir := t.TempDir()
	tok := tokNPM()
	content := "a " + tok + " b " + tok + "\n"
	writeContextFile(t, dir, "src/two.txt", content)
	code, out, errb := runContextCLI(t, dir, "--allow", "src")
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	if c := strings.Count(out, "finding\tnpm-token\t"); c != 1 {
		t.Fatalf("npm-token lines = %d, want 1", c)
	}
	first := strings.Index(content, tok)
	want := "finding\tnpm-token\tsrc/two.txt\t" + strconv.Itoa(first)
	if got := lineWith(out, "finding\tnpm-token\t"); got != want {
		t.Fatalf("finding line = %q, want %q (first offset)", got, want)
	}
}

func TestContextAccept(t *testing.T) {
	dir := t.TempDir()
	token := tokAnthropic()
	name := "src/creds.txt"
	content := "k=" + token + "\n"
	writeContextFile(t, dir, name, content)
	off := strconv.Itoa(strings.Index(content, token))

	// Baseline: a finding and exit 1.
	code, out, errb := runContextCLI(t, dir, "--allow", "src")
	if code != 1 {
		t.Fatalf("baseline code = %d, want 1; stderr=%s", code, errb)
	}
	if !hasLine(out, "finding\tanthropic-key\t"+name+"\t"+off) {
		t.Fatalf("baseline finding missing: %q", out)
	}

	// Accepted: the same line with the accepted prefix and exit 0.
	code, out, errb = runContextCLI(t, dir, "--allow", "src", "--accept", name+"=anthropic-key")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	if !hasLine(out, "accepted\tanthropic-key\t"+name+"\t"+off) {
		t.Fatalf("accepted line missing: %q", out)
	}
	if strings.Contains(out, "finding\t") {
		t.Fatalf("finding not suppressed: %q", out)
	}
	if !hasLine(out, "summary\tfiles=1\tbytes="+strconv.Itoa(len(content))+"\tfindings=0\taccepted=1") {
		t.Fatalf("summary missing: %q", out)
	}

	// A rule name with a colon (deny-token:1) splits at the last "=".
	auxDir := t.TempDir()
	denyTok := strings.Repeat("M", 16)
	deny := writeAuxFile(t, auxDir, "deny.txt", denyTok+"\n")
	writeContextFile(t, dir, "src/data.txt", "a "+denyTok+" b\n")
	code, out, errb = runContextCLI(t, dir,
		"--allow", "src", "--deny-file", deny,
		"--accept", name+"=anthropic-key", "--accept", "src/data.txt=deny-token:1")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	if got := lineWith(out, "accepted\tdeny-token:1\t"); !strings.HasPrefix(got, "accepted\tdeny-token:1\tsrc/data.txt\t") {
		t.Fatalf("accepted deny-token line missing: %q", out)
	}
	if strings.Contains(out, "finding\t") {
		t.Fatalf("findings not suppressed: %q", out)
	}
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
		{"context malformed accept", []string{"context", dir, "--allow", "src", "--accept", "noequals"}},
		{"context empty accept path", []string{"context", dir, "--allow", "src", "--accept", "=rule"}},
		{"context empty accept rule", []string{"context", dir, "--allow", "src", "--accept", "path="}},
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
	nbytes := len(cfgBytes) + len(b1) + len(b2) + len(b3) + len(b4) + len(b5) + len(b6)
	if !hasLine(out, "summary\tfiles=7\tbytes="+strconv.Itoa(nbytes)+"\tfindings=4\taccepted=0") {
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
		want := "finding\thost-path\tconfig\t" + strconv.Itoa(bytes.Index(cfgBytes, []byte("/Users/someone")))
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
		want := "finding\thost-path\tconfig\t" + strconv.Itoa(bytes.Index(cfgBytes, []byte(`C:\\Users\\`)))
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
		want := "finding\thost-path\tconfig\t" + strconv.Itoa(bytes.Index(cfgBytes, []byte("/home/data")))
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
	wantCfg := "finding\tslack-token\tconfig\t" + strconv.Itoa(bytes.Index(cfgBytes, []byte(slack)))
	if got := lineWith(out, "finding\tslack-token\tconfig\t"); got != wantCfg {
		t.Fatalf("config finding = %q, want %q", got, wantCfg)
	}
	wantLayer := "finding\tdeny-token:1\tlayer0:etc/data\t" + strconv.Itoa(strings.Index(layBody, denyTok))
	if got := lineWith(out, "finding\tdeny-token:1\t"); got != wantLayer {
		t.Fatalf("layer finding = %q, want %q", got, wantLayer)
	}
	if strings.Contains(out, slack) || strings.Contains(out, denyTok) || strings.Contains(errb, denyTok) {
		t.Fatal("token leaked to the output")
	}
	if !hasLine(out, "summary\tfiles=2\tbytes="+strconv.Itoa(len(cfgBytes)+len(layBody))+"\tfindings=2\taccepted=0") {
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
	accept := writeAuxFile(t, auxDir, "accept.txt", "# reviewed\n\n  "+name1+"=anthropic-key  \n")
	code, out, errb := runContextCLI(t, dir, "--allow", "src",
		"--accept-file", accept, "--accept", name2+"=xai-key")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	if got := lineWith(out, "accepted\tanthropic-key\t"); !strings.HasPrefix(got, "accepted\tanthropic-key\t"+name1+"\t") {
		t.Fatalf("file accept not applied:\n%s", out)
	}
	if got := lineWith(out, "accepted\txai-key\t"); !strings.HasPrefix(got, "accepted\txai-key\t"+name2+"\t") {
		t.Fatalf("cli accept not applied:\n%s", out)
	}
	if strings.Contains(out, "finding\t") {
		t.Fatalf("findings not suppressed:\n%s", out)
	}
	if !hasLine(out, "summary\tfiles=2\tbytes="+strconv.Itoa(len(content1)+len(content2))+"\tfindings=0\taccepted=2") {
		t.Fatalf("summary missing:\n%s", out)
	}
	if strings.Contains(out, tokAnthropic()) || strings.Contains(out, tokXAI()) {
		t.Fatal("token leaked to stdout")
	}
}

func TestAcceptFileMalformed(t *testing.T) {
	dir := t.TempDir()
	auxDir := t.TempDir()
	writeContextFile(t, dir, "src/a.txt", "x\n")
	accept := writeAuxFile(t, auxDir, "accept.txt", "# ok\nnoequals\n")
	code, out, errb := runContextCLI(t, dir, "--allow", "src", "--accept-file", accept)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%s", code, errb)
	}
	if !strings.Contains(errb, "line 2") {
		t.Fatalf("stderr should cite the line number: %q", errb)
	}
	if strings.Contains(errb, "noequals") || strings.Contains(out, "noequals") {
		t.Fatal("malformed entry leaked to the output")
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
	cfg := map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"diff_ids": []string{}}}
	layers := [][]testEntry{{{Name: "etc/data", Body: "t=" + tok + "\n"}}}
	archive := writeSavedArchive(t, dir, "img.tar", cfg, layers, false)
	accept := writeAuxFile(t, auxDir, "accept.txt", "# ok\nlayer0:etc/data=npm-token\n")
	code, out, errb := runImageCLI(t, archive, "--accept-file", accept)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	if got := lineWith(out, "accepted\tnpm-token\t"); !strings.HasPrefix(got, "accepted\tnpm-token\tlayer0:etc/data\t") {
		t.Fatalf("accepted line missing:\n%s", out)
	}
	if strings.Contains(out, tok) {
		t.Fatal("token leaked to stdout")
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
	want := "finding\tdeny-token:1\tsrc/big.txt\t" + strconv.Itoa(start)
	if got := lineWith(out, "finding\t"); got != want {
		t.Fatalf("finding line = %q, want %q", got, want)
	}
	if strings.Contains(out, token) || strings.Contains(errb, token) {
		t.Fatal("deny token leaked to the output")
	}
}
