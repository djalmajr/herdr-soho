package cli

// Regression for the confirmed ownership P3: an explicit bare ASCII
// identifier assignment ("B =") inside the Owned files section binds its
// backtick-quoted value to the identifier; the value is not an owned path.
// The tests drive the real ownedPaths and warnOwnedOverlap with temporary
// roster and brief files.

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestDispatchBaseDefinitionOwnedAssignment(t *testing.T) {
	t.Run("ownedPaths: the historical bare-identifier assignment is not an owned path", func(t *testing.T) {
		// The verified historical pattern: "B =" at the end of a prose line
		// and the parent on the next line is the assignment's value, not an
		// owned path; the real path on the assignment line stays owned.
		body := "## Owned files\nSomente `B/verifier/checks.go`, tests in that folder\ne new proofs. B =\n`/work/base`.\nPreserve baseline before editing.\n"
		if got, want := ownedPaths(body, nil), []string{"B/verifier/checks.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("owned paths=%v, want %v", got, want)
		}
	})
	t.Run("ownedPaths: a same-line assignment keeps the other real paths on the line owned", func(t *testing.T) {
		body := "## Owned files\n- `internal/a.go` B = `/work/base`\n- `internal/b.go`\nB = `/work/base`, keep `internal/n.go`\n- `internal/o.go`\n"
		if got, want := ownedPaths(body, nil), []string{"internal/a.go", "internal/b.go", "internal/n.go", "internal/o.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("owned paths=%v, want %v", got, want)
		}
	})
	t.Run("ownedPaths: a next-line assignment consumes only the value span", func(t *testing.T) {
		body := "## Owned files\n- `internal/a.go`\ne new proofs. B =\n`/work/base` and `internal/c.go`\n- `internal/d.go`\n"
		if got, want := ownedPaths(body, nil), []string{"internal/a.go", "internal/c.go", "internal/d.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("owned paths=%v, want %v", got, want)
		}
	})
	t.Run("ownedPaths: CRLF around the assignment and its value", func(t *testing.T) {
		body := "## Owned files\r\nSomente `B/verifier/checks.go`, tests in that folder\r\ne new proofs. B =\r\n`/work/base`.\r\nPreserve baseline before editing.\r\n"
		if got, want := ownedPaths(body, nil), []string{"B/verifier/checks.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("owned paths=%v, want %v", got, want)
		}
	})
	t.Run("ownedPaths: an equals sign inside a backtick filename is not assignment syntax", func(t *testing.T) {
		body := "## Owned files\n- `a=b.go`\n- `internal/e.go`\n"
		if got, want := ownedPaths(body, nil), []string{"a=b.go", "internal/e.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("owned paths=%v, want %v", got, want)
		}
	})
	t.Run("ownedPaths: a digit-leading token is not a bare ASCII identifier", func(t *testing.T) {
		for _, token := range []string{"1B", "99BASE", "2_base"} {
			body := "## Owned files\n" + token + " = `/work/base`\n"
			if got, want := ownedPaths(body, nil), []string{"/work/base"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("invalid identifier %q suppressed real ownership: %v", token, got)
			}
		}
	})
	t.Run("ownedPaths: the ASCII tail of a non-ASCII token is not an identifier", func(t *testing.T) {
		for _, token := range []string{"ÁB", "BÁC", "ñ_base", "B٣C"} {
			for _, body := range []string{
				"## Owned files\n" + token + " = `/work/base`\n",
				"## Owned files\n" + token + " =\n`/work/base`\n",
			} {
				if got, want := ownedPaths(body, nil), []string{"/work/base"}; !reflect.DeepEqual(got, want) {
					t.Fatalf("token %q suppressed real ownership in %q: %v", token, body, got)
				}
			}
		}
	})
	t.Run("ownedPaths: an ASCII identifier after non-ASCII prose and a space is still an assignment", func(t *testing.T) {
		body := "## Owned files\nDefinição: B = `/work/base`\n- `internal/h.go`\n"
		if got, want := ownedPaths(body, nil), []string{"internal/h.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("owned paths=%v, want %v", got, want)
		}
	})
	t.Run("ownedPaths: a quoted filename with = as the assignment value is the value", func(t *testing.T) {
		body := "## Owned files\nbase B = `a=b.go`\n- `internal/f.go`\n"
		if got, want := ownedPaths(body, nil), []string{"internal/f.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("owned paths=%v, want %v", got, want)
		}
	})
	t.Run("ownedPaths: the pending assignment does not swallow a real list item", func(t *testing.T) {
		body := "## Owned files\ne new proofs. B =\n- `internal/g.go`\n"
		if got, want := ownedPaths(body, nil), []string{"internal/g.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("owned paths=%v, want %v", got, want)
		}
	})
	t.Run("ownedPaths: the pending assignment does not cross a section heading", func(t *testing.T) {
		body := "## Owned files\ne new proofs. B =\n## Next\n- `internal/h.go`\n"
		if got, want := ownedPaths(body, nil), []string{}; !reflect.DeepEqual(got, want) {
			t.Fatalf("owned paths=%v, want %v", got, want)
		}
	})
	t.Run("ownedPaths: the pending assignment stops at a deeper heading and does not reach past it", func(t *testing.T) {
		body := "## Owned files\ne new proofs. B =\n### detail\n`/work/base`\n- `internal/i.go`\n"
		if got, want := ownedPaths(body, nil), []string{"/work/base", "internal/i.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("owned paths=%v, want %v", got, want)
		}
	})
	t.Run("ownedPaths: == and unquoted text after = are not assignments", func(t *testing.T) {
		cases := []struct {
			name string
			body string
			want []string
		}{
			{"a == comparison keeps the following span owned", "## Owned files\nif x == 1 then `internal/j.go`\n", []string{"internal/j.go"}},
			{"unquoted text after = does not pend for the next line", "## Owned files\nnote B = /work/base\n`internal/k.go`\n", []string{"internal/k.go"}},
			{"an excluded line with an assignment does not pend", "## Owned files\nexceto B =\n`internal/l.go`\n", []string{"internal/l.go"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := ownedPaths(tc.body, nil); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("owned paths=%v, want %v", got, tc.want)
				}
			})
		}
	})
	t.Run("ownedPaths: whitespace around the identifier and =", func(t *testing.T) {
		cases := []struct {
			name string
			body string
			want []string
		}{
			{"no spaces", "## Owned files\nB=`/work/base`\n- `internal/m.go`\n", []string{"internal/m.go"}},
			{"extra spaces and tabs", "## Owned files\n\tB   =  \t`/work/base`\n- `internal/p.go`\n", []string{"internal/p.go"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := ownedPaths(tc.body, nil); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("owned paths=%v, want %v", got, tc.want)
				}
			})
		}
	})

	// warnOwnedOverlap runs the real pending-roster/brief flow: the peer's
	// last report points at a pending brief whose Owned files section is
	// theirs.
	overlap := func(t *testing.T, mine, theirs string) string {
		t.Helper()
		h := newTM3bHarness(t, "# Brief — base definition\n\n# Goal\nGoal.\n\n## Owned files\n"+mine+"\n\n# Report\nReport.\n")
		for _, dir := range []string{filepath.Join(h.ws, "briefs"), filepath.Join(h.ws, "reports")} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		pending := filepath.Join(h.ws, "briefs", "peer.md")
		if err := os.WriteFile(pending, []byte("# Brief\n## Owned files\n"+theirs+"\n# Report contract\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		report := filepath.Join(h.ws, "reports", "peer.md")
		if err := os.WriteFile(filepath.Join(h.ws, "last-report-peer"), []byte(report+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		roster := "peer\tw0test:p0b\tcodex\timplementer\topenai\t0\t" + h.root + "\tnow\tgpt-5\ttask\timplementer\n"
		if err := os.WriteFile(filepath.Join(h.ws, "agents.tsv"), []byte(roster), 0o600); err != nil {
			t.Fatal(err)
		}
		oldErr := platform.Stderr
		var stderr bytes.Buffer
		platform.Stderr = &stderr
		t.Cleanup(func() { platform.Stderr = oldErr })
		warnOwnedOverlap(h.brief, "implementer", "build", h.ws, h.ctx, h.env, h.root, h.root)
		return stderr.String()
	}
	historical := "Somente `B/verifier/checks.go`, tests in that folder\ne new proofs. B =\n`/work/base`.\nPreserve baseline before editing."
	t.Run("warnOwnedOverlap: the historical bare-identifier assignment is not a false overlap", func(t *testing.T) {
		if got := overlap(t, historical, "- `/work/base/driver/`"); strings.Contains(got, "owns files") {
			t.Fatalf("false overlap warning: %q", got)
		}
	})
	t.Run("warnOwnedOverlap: the CRLF historical assignment is not a false overlap", func(t *testing.T) {
		crlf := strings.ReplaceAll(historical, "\n", "\r\n")
		if got := overlap(t, crlf, "- `/work/base/driver/`"); strings.Contains(got, "owns files") {
			t.Fatalf("false CRLF overlap warning: %q", got)
		}
	})
	t.Run("warnOwnedOverlap: a same-line assignment is not a false overlap", func(t *testing.T) {
		if got := overlap(t, "- `internal/a.go` B = `/work/base`\n- `internal/b.go`", "- `/work/base/driver/`"); strings.Contains(got, "owns files") {
			t.Fatalf("false same-line overlap warning: %q", got)
		}
	})
	t.Run("warnOwnedOverlap: an explicit real parent ownership still warns", func(t *testing.T) {
		got := overlap(t, "- `/work/base`", "- `/work/base/driver/`")
		if !strings.Contains(got, "owns files that 'peer' is still editing: /work/base") {
			t.Fatalf("explicit parent overlap warning missing: %q", got)
		}
	})
	t.Run("warnOwnedOverlap: a real narrow overlap still warns", func(t *testing.T) {
		got := overlap(t, "- `internal/a.go`", "- `internal/a.go`")
		if !strings.Contains(got, "owns files that 'peer' is still editing: internal/a.go") {
			t.Fatalf("narrow overlap warning missing: %q", got)
		}
	})
}
