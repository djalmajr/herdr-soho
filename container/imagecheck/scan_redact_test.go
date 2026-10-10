package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// assertNoToken asserts that the token is absent from out and errb in
// every case: the case-insensitive assertNoLiteral check plus an
// explicit check for the upper-cased token.
func assertNoToken(t *testing.T, out, errb, token string) {
	t.Helper()
	assertNoLiteral(t, out, errb, token)
	for _, s := range []string{out, errb} {
		up := strings.ToUpper(token)
		if strings.Contains(s, up) {
			t.Fatalf("upper-cased token %q appears in the output:\n%s", up, s)
		}
	}
}

// TestRedactEarlyErrors covers the errors that imagecheck context and
// image print before the redacting writer exists (flag parsing and
// the deny-file load): none of them may carry a user-controlled
// value. The two "late" control cases prove that the errors printed
// through the redacting writer are still redacted.
func TestRedactEarlyErrors(t *testing.T) {
	token := "zq" + "Canary" + strconv.Itoa(os.Getpid())
	// A well-formed accept entry whose rule field is the token: it
	// fails the rule validation, and the rule text must not leak.
	entry := token + " 1 1 sha256:" + strings.Repeat("a", 64) + " p"
	type earlyCase struct {
		name       string
		extra      func(t *testing.T, auxDir string) []string
		wantCode   int
		wantStderr string
	}
	cases := []earlyCase{
		{"accept flag value", func(t *testing.T, auxDir string) []string {
			return []string{"--accept", entry}
		}, exitUsage, "rule field cannot be accepted"},
		{"accept flag equals form", func(t *testing.T, auxDir string) []string {
			return []string{"--accept=" + entry}
		}, exitUsage, "rule field cannot be accepted"},
		{"unknown flag", func(t *testing.T, auxDir string) []string {
			return []string{"--" + token}
		}, exitUsage, "unknown flag at argument"},
		{"unknown flag with value", func(t *testing.T, auxDir string) []string {
			return []string{"--x=" + token}
		}, exitUsage, "unknown flag at argument"},
		{"deny file not found", func(t *testing.T, auxDir string) []string {
			return []string{"--deny-file", filepath.Join(auxDir, token, "missing.txt")}
		}, exitIO, "deny-file: not found"},
		{"deny file not found equals form", func(t *testing.T, auxDir string) []string {
			return []string{"--deny-file=" + filepath.Join(auxDir, token, "missing.txt")}
		}, exitIO, "deny-file: not found"},
		{"deny file short token line 1", func(t *testing.T, auxDir string) []string {
			if err := os.MkdirAll(filepath.Join(auxDir, token), 0o755); err != nil {
				t.Fatal(err)
			}
			writeAuxFile(t, auxDir, token+"/deny.txt", "abc\n")
			return []string{"--deny-file", filepath.Join(auxDir, token, "deny.txt")}
		}, exitUsage, "deny-file: line 1: token shorter than 4 bytes"},
		{"deny file short token line 3", func(t *testing.T, auxDir string) []string {
			if err := os.MkdirAll(filepath.Join(auxDir, token), 0o755); err != nil {
				t.Fatal(err)
			}
			writeAuxFile(t, auxDir, token+"/deny.txt", "first\nsecond\nxyz\n")
			return []string{"--deny-file", filepath.Join(auxDir, token, "deny.txt")}
		}, exitUsage, "deny-file: line 3: token shorter than 4 bytes"},
		{"deny file is a directory", func(t *testing.T, auxDir string) []string {
			if err := os.MkdirAll(filepath.Join(auxDir, token), 0o755); err != nil {
				t.Fatal(err)
			}
			return []string{"--deny-file", filepath.Join(auxDir, token)}
		}, exitIO, "deny-file: cannot be read"},
		{"late accept file read redacted", func(t *testing.T, auxDir string) []string {
			deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
			return []string{"--deny-file", deny,
				"--accept-file", filepath.Join(auxDir, token, "accept.txt")}
		}, exitIO, "[deny-token:1]"},
		{"late accept file line names no value", func(t *testing.T, auxDir string) []string {
			deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
			accept := writeAuxFile(t, auxDir, "accept.txt", entry+"\n")
			return []string{"--deny-file", deny, "--accept-file", accept}
		}, exitUsage, "rule field cannot be accepted"},
	}
	for _, cmd := range []string{"context", "image"} {
		for _, c := range cases {
			t.Run(cmd+"/"+c.name, func(t *testing.T) {
				auxDir := t.TempDir()
				extra := c.extra(t, auxDir)
				var code int
				var out, errb string
				if cmd == "context" {
					dir := t.TempDir()
					writeContextFile(t, dir, "src/a.txt", "x\n")
					extra = append([]string{"--allow", "src"}, extra...)
					code, out, errb = runContextCLI(t, dir, extra...)
				} else {
					archDir := t.TempDir()
					archive := filepath.Join(archDir, "a.tar")
					if err := os.WriteFile(archive, []byte("x\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					code, out, errb = runImageCLI(t, archive, extra...)
				}
				if code != c.wantCode {
					t.Fatalf("code = %d, want %d; stderr=%s", code, c.wantCode, errb)
				}
				if !strings.Contains(errb, c.wantStderr) {
					t.Fatalf("stderr = %q, want a mention of %q", errb, c.wantStderr)
				}
				assertNoToken(t, out, errb, token)
			})
		}
	}
}
