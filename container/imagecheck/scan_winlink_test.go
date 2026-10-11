package main

import (
	"bytes"
	"strings"
	"testing"
)

// printFindings prints the findings of f in the scanner's line format
// and returns the output.
func printFindings(t *testing.T, f *scanFindings) string {
	t.Helper()
	var buf bytes.Buffer
	f.print(&buf)
	return buf.String()
}

// TestSlashLinkTarget: a symlink target read with os.Readlink is
// converted to slash form only when the OS separator is not "/"; on
// Unix a backslash is a legal name byte and must stay.
func TestSlashLinkTarget(t *testing.T) {
	if got := slashLinkTarget(`..\dir\.\canaryx`, '\\'); got != "../dir/./canaryx" {
		t.Fatalf("backslash sep: got %q, want %q", got, "../dir/./canaryx")
	}
	if got := slashLinkTarget(`..\dir\.\canaryx`, '/'); got != `..\dir\.\canaryx` {
		t.Fatalf("slash sep must stay unchanged: got %q", got)
	}
	if got := slashLinkTarget("", '\\'); got != "" {
		t.Fatalf("empty backslash sep: got %q, want empty", got)
	}
	if got := slashLinkTarget("", '/'); got != "" {
		t.Fatalf("empty slash sep: got %q, want empty", got)
	}
}

// TestCheckContextLink: the symlink-target rules run on the raw target,
// on its slash form when the separator is not "/" and on the resolved
// name, at @link; a backslash target is not converted when the
// separator is "/". No real symlinks are involved, so the test runs
// unchanged on every OS.
func TestCheckContextLink(t *testing.T) {
	token := "dir/can" + "aryx"
	toks := []denyToken{{line: 1, needle: asciiLower([]byte(token))}}
	denyLine := "finding\tdeny-token:1\tsrc/lnk\t@link"
	forbiddenLine := "finding\tforbidden-name\tsrc/lnk\t@link"
	run := func(sep byte, target string, useToks bool) string {
		f := newFindings()
		var tk []denyToken
		if useToks {
			tk = toks
		}
		checkContextLink("src/lnk", target, sep, "src/lnk", tk, f)
		return printFindings(t, f)
	}
	t.Run("backslash-target-backslash-sep", func(t *testing.T) {
		out := run('\\', `..\dir\.\canaryx`, true)
		if !hasLine(out, denyLine) {
			t.Fatalf("resolved-target deny finding missing in:\n%s", out)
		}
		if n := strings.Count(out, "finding\t"); n != 1 {
			t.Fatalf("findings = %d, want exactly 1:\n%s", n, out)
		}
	})
	t.Run("backslash-target-slash-sep-not-converted", func(t *testing.T) {
		// With the "/" separator the backslashes are name bytes: the
		// target is not converted, so the resolved name never holds
		// the token and no finding appears.
		out := run('/', `..\dir\.\canaryx`, true)
		if strings.Contains(out, "deny-token") {
			t.Fatalf("backslash target must not be converted on a slash separator:\n%s", out)
		}
		if n := strings.Count(out, "finding\t"); n != 0 {
			t.Fatalf("findings = %d, want 0:\n%s", n, out)
		}
	})
	t.Run("slash-target-slash-sep-same-findings", func(t *testing.T) {
		// The same call with the "/" separator and a slash target
		// gives the same finding.
		out := run('/', `../dir/./canaryx`, true)
		if !hasLine(out, denyLine) {
			t.Fatalf("resolved-target deny finding missing in:\n%s", out)
		}
		if n := strings.Count(out, "finding\t"); n != 1 {
			t.Fatalf("findings = %d, want exactly 1:\n%s", n, out)
		}
	})
	t.Run("forbidden-resolved-target", func(t *testing.T) {
		out := run('\\', `..\.git\config`, false)
		if !hasLine(out, forbiddenLine) {
			t.Fatalf("forbidden-name @link finding missing in:\n%s", out)
		}
		if n := strings.Count(out, "finding\t"); n != 1 {
			t.Fatalf("findings = %d, want exactly 1:\n%s", n, out)
		}
	})
	t.Run("forbidden-resolved-target-slash-sep", func(t *testing.T) {
		// The same call with the "/" separator and a slash target
		// gives the same finding.
		out := run('/', `../.git/config`, false)
		if !hasLine(out, forbiddenLine) {
			t.Fatalf("forbidden-name @link finding missing in:\n%s", out)
		}
		if n := strings.Count(out, "finding\t"); n != 1 {
			t.Fatalf("findings = %d, want exactly 1:\n%s", n, out)
		}
	})
	t.Run("harmless-target", func(t *testing.T) {
		out := run('\\', `..\dir\.\other`, true)
		if n := strings.Count(out, "finding\t"); n != 0 {
			t.Fatalf("findings = %d, want 0:\n%s", n, out)
		}
	})
}
