package main

import (
	"archive/tar"
	"regexp"
	"strings"
	"testing"
)

// mustPattern compiles a pattern for the test rule tables.
func mustPattern(p string) *regexp.Regexp {
	return regexp.MustCompile(p)
}

// namesTok builds one denyToken from a line number and parts of the
// literal, so no token literal lives in the source as one string.
func namesTok(line int, parts ...string) denyToken {
	return denyToken{line: line, needle: []byte(strings.Join(parts, ""))}
}

func TestNamesLowerASCIIString(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"AaBbZz", "aabbzz"},
		{"already-lower", "already-lower"},
		{"0123456789-_./", "0123456789-_./"},
		{"MiXedCASE\x00byte", "mixedcase\x00byte"},
		{"A\xc3\x89-Z\xc3\x9c", "a\xc3\x89-z\xc3\x9c"}, // only A-Z bytes fold; UTF-8
		// continuation/lead bytes keep
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			if got := lowerASCIIString(c.raw); got != c.want {
				t.Fatalf("lowerASCIIString(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestNamesNormalizeArchiveName(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"/root/.aws/credentials", "root/.aws/credentials"},
		{"//home/agent/.ssh/id_rsa", "home/agent/.ssh/id_rsa"},
		{"./etc/x", "etc/x"},
		{"../../root/.ssh/k", "root/.ssh/k"},
		{"a/../b", "b"},
		{"", "/"},
		{"/", "/"},
		{"..", "/"},
		{"etc/", "etc"},
		{"././x/./y", "x/y"},
		{"../..", "/"},
		{"a/../../x", "x"},
	}
	for _, c := range cases {
		t.Run("raw="+c.raw, func(t *testing.T) {
			if got := normalizeArchiveName(c.raw); got != c.want {
				t.Fatalf("normalizeArchiveName(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

// TestNamesCredentialPathAfterNormalize proves the absolute tar member
// names now reach credentialPath: the credential members are flagged and
// /etc/passwd is not.
func TestNamesCredentialPathAfterNormalize(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"/root/.aws/credentials", true},
		{"//home/agent/.ssh/id_rsa", true},
		{"./root/.docker/config.json", true},
		{"/etc/passwd", false},
		{"/home/agent/notes.txt", false},
	}
	for _, c := range cases {
		t.Run("raw="+c.raw, func(t *testing.T) {
			got := credentialPath(normalizeArchiveName(c.raw))
			if got != c.want {
				t.Fatalf("credentialPath(normalizeArchiveName(%q)) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

func TestNamesLinkTargetName(t *testing.T) {
	symrel := &tar.Header{Name: "etc/pointer", Typeflag: tar.TypeSymlink, Linkname: "../home/agent/.ssh/id_rsa"}
	symabs := &tar.Header{Name: "opt/link", Typeflag: tar.TypeSymlink, Linkname: "/root/.aws/credentials"}
	hard := &tar.Header{Name: "bin/y", Typeflag: tar.TypeLink, Linkname: "./usr/bin/x"}
	reg := &tar.Header{Name: "a/b", Typeflag: tar.TypeReg, Linkname: ""}
	symEmpty := &tar.Header{Name: "a/b", Typeflag: tar.TypeSymlink, Linkname: ""}
	dir := &tar.Header{Name: "d", Typeflag: tar.TypeDir, Linkname: "d"}

	cases := []struct {
		name     string
		normName string
		h        *tar.Header
		want     string
		wantCred bool
	}{
		{"symlink-relative", "etc/pointer", symrel, "home/agent/.ssh/id_rsa", true},
		{"symlink-absolute", "opt/link", symabs, "root/.aws/credentials", true},
		{"hardlink", "bin/y", hard, "usr/bin/x", false},
		{"regular-file", "a/b", reg, "", false},
		{"symlink-empty-linkname", "a/b", symEmpty, "", false},
		{"directory-with-linkname", "d", dir, "", false},
		{"nil-header", "a/b", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := linkTargetName(c.normName, c.h)
			if got != c.want {
				t.Fatalf("linkTargetName(%q, %v) = %q, want %q", c.normName, c.h, got, c.want)
			}
			if got != "" && credentialPath(got) != c.wantCred {
				t.Fatalf("credentialPath(%q) = %v, want %v", got, !c.wantCred, c.wantCred)
			}
		})
	}
}

func TestNamesHeaderMetaFields(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		h := &tar.Header{
			Name:     "a/b",
			Linkname: "../c",
			Uname:    "sampleuser",
			Gname:    "samplegroup",
			PAXRecords: map[string]string{
				"2":  "value2",
				"1":  "value1",
				"10": "value10",
			},
		}
		want := []metaField{
			{"name", "a/b"},
			{"link", "../c"},
			{"uname", "sampleuser"},
			{"gname", "samplegroup"},
			{"pax", "1"},
			{"pax", "value1"},
			{"pax", "10"},
			{"pax", "value10"},
			{"pax", "2"},
			{"pax", "value2"},
		}
		if got := headerMetaFields(h); !fieldsEqual(got, want) {
			t.Fatalf("headerMetaFields = %v, want %v", got, want)
		}
	})
	t.Run("empty-fields-omitted", func(t *testing.T) {
		h := &tar.Header{Name: "only/name"}
		want := []metaField{{"name", "only/name"}}
		if got := headerMetaFields(h); !fieldsEqual(got, want) {
			t.Fatalf("headerMetaFields = %v, want %v", got, want)
		}
	})
	t.Run("empty-pax-value-kept", func(t *testing.T) {
		h := &tar.Header{Name: "x", PAXRecords: map[string]string{"k": ""}}
		want := []metaField{{"name", "x"}, {"pax", "k"}, {"pax", ""}}
		if got := headerMetaFields(h); !fieldsEqual(got, want) {
			t.Fatalf("headerMetaFields = %v, want %v", got, want)
		}
	})
	t.Run("nil-header", func(t *testing.T) {
		if got := headerMetaFields(nil); got != nil {
			t.Fatalf("headerMetaFields(nil) = %v, want nil", got)
		}
	})
}

func fieldsEqual(got, want []metaField) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestNamesDenyHitLines(t *testing.T) {
	canary := namesTok(3, "Canary", "Host")
	t.Run("hit-lowercase-embedded", func(t *testing.T) {
		if got := denyHitLines("x-canaryhost-y", []denyToken{canary}); !intsEqual(got, []int{3}) {
			t.Fatalf("denyHitLines = %v, want [3]", got)
		}
	})
	t.Run("hit-uppercase", func(t *testing.T) {
		if got := denyHitLines("X-CANARYHOST", []denyToken{canary}); !intsEqual(got, []int{3}) {
			t.Fatalf("denyHitLines = %v, want [3]", got)
		}
	})
	t.Run("no-hit-different-spelling", func(t *testing.T) {
		if got := denyHitLines("canary-host", []denyToken{canary}); len(got) != 0 {
			t.Fatalf("denyHitLines = %v, want empty", got)
		}
	})
	t.Run("two-tokens-token-order", func(t *testing.T) {
		toks := []denyToken{
			namesTok(5, "Alpha", "Token"),
			namesTok(2, "Beta", "Token"),
		}
		// Text order is reversed relative to token order.
		if got := denyHitLines("betatoken first, alphatoken second", toks); !intsEqual(got, []int{5, 2}) {
			t.Fatalf("denyHitLines = %v, want [5 2] (token order)", got)
		}
	})
	t.Run("duplicate-occurrence-reported-once", func(t *testing.T) {
		// One token whose needle occurs several times in s reports its
		// line once, not per occurrence.
		toks := []denyToken{namesTok(7, "Dupletoken")}
		if got := denyHitLines("dupletoken mid dupletoken", toks); !intsEqual(got, []int{7}) {
			t.Fatalf("denyHitLines = %v, want [7]", got)
		}
	})
	t.Run("same-needle-two-lines-both-reported", func(t *testing.T) {
		// Two distinct tokens (two deny-file lines) that both occur are
		// both reported, in token order.
		toks := []denyToken{
			namesTok(7, "Dupletoken"),
			namesTok(9, "Dupletoken"),
		}
		if got := denyHitLines("dupletoken", toks); !intsEqual(got, []int{7, 9}) {
			t.Fatalf("denyHitLines = %v, want [7 9]", got)
		}
	})
	t.Run("no-tokens", func(t *testing.T) {
		if got := denyHitLines("anything", nil); len(got) != 0 {
			t.Fatalf("denyHitLines = %v, want empty", got)
		}
	})
}

func intsEqual(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestNamesIsWhiteoutMarker(t *testing.T) {
	cases := []struct {
		name     string
		h        *tar.Header
		normName string
		want     bool
	}{
		{"zero-size-regular", &tar.Header{Typeflag: tar.TypeReg, Size: 0}, "root/.wh.secret", true},
		{"non-empty-regular", &tar.Header{Typeflag: tar.TypeReg, Size: 3}, "root/.wh.secret", false},
		{"directory", &tar.Header{Typeflag: tar.TypeDir, Size: 0}, "root/.wh.d", false},
		{"symlink", &tar.Header{Typeflag: tar.TypeSymlink, Size: 0, Linkname: "x"}, "root/.wh.l", false},
		{"hardlink", &tar.Header{Typeflag: tar.TypeLink, Size: 0, Linkname: "x"}, "root/.wh.l", false},
		{"opaque-zero-size", &tar.Header{Typeflag: tar.TypeReg, Size: 0}, "root/.wh..wh..opq", true},
		{"plain-zero-size", &tar.Header{Typeflag: tar.TypeReg, Size: 0}, "root/plain", false},
		{"wh-in-middle-component", &tar.Header{Typeflag: tar.TypeReg, Size: 0}, "root/.wh.sub/x", false},
		{"nil-header", nil, "root/.wh.secret", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isWhiteoutMarker(c.h, c.normName); got != c.want {
				t.Fatalf("isWhiteoutMarker(%v, %q) = %v, want %v", c.h, c.normName, got, c.want)
			}
		})
	}
}

func TestNamesContentRuleHits(t *testing.T) {
	awsKey := "AKIA" + "ABCDEFGHIJKLMNOP" // synthetic key, not the docs example
	ghToken := "ghp_" + strings.Repeat("D", 40)
	t.Run("aws-key-in-link-target", func(t *testing.T) {
		got := contentRuleHits("link target: "+awsKey+" end", scanRules)
		if !namesEqual(got, []string{"aws-access-key"}) {
			t.Fatalf("contentRuleHits = %v, want [aws-access-key]", got)
		}
	})
	t.Run("docs-example-alone", func(t *testing.T) {
		if got := contentRuleHits(awsDocsExample, scanRules); len(got) != 0 {
			t.Fatalf("contentRuleHits(docs example) = %v, want empty", got)
		}
	})
	t.Run("docs-example-next-to-real-key", func(t *testing.T) {
		got := contentRuleHits(awsDocsExample+" "+awsKey, scanRules)
		if !namesEqual(got, []string{"aws-access-key"}) {
			t.Fatalf("contentRuleHits = %v, want [aws-access-key]", got)
		}
	})
	t.Run("github-token", func(t *testing.T) {
		got := contentRuleHits(ghToken, scanRules)
		if !namesEqual(got, []string{"github-token"}) {
			t.Fatalf("contentRuleHits = %v, want [github-token]", got)
		}
	})
	t.Run("duplicate-rule-name-reported-once", func(t *testing.T) {
		// hostPathRules has three patterns all named "host-path"; two of
		// them match this string, so deduping must keep one entry.
		got := contentRuleHits("/Users/sampleuser/x /home/sampleuser/y", hostPathRules)
		if !namesEqual(got, []string{"host-path"}) {
			t.Fatalf("contentRuleHits = %v, want [host-path]", got)
		}
	})
	t.Run("rules-order", func(t *testing.T) {
		rules := []scanRule{
			{name: "z-rule", pattern: mustPattern(`x`)},
			{name: "a-rule", pattern: mustPattern(`x`)},
		}
		if got := contentRuleHits("x", rules); !namesEqual(got, []string{"z-rule", "a-rule"}) {
			t.Fatalf("contentRuleHits = %v, want [z-rule a-rule] (rules order)", got)
		}
	})
	t.Run("no-match", func(t *testing.T) {
		if got := contentRuleHits("harmless text", scanRules); len(got) != 0 {
			t.Fatalf("contentRuleHits = %v, want empty", got)
		}
	})
}

func namesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestNamesContextLinkName(t *testing.T) {
	cases := []struct {
		rel    string
		target string
		want   string
	}{
		{"src/pointer", "../bin/tool", "bin/tool"},
		{"src/pointer", "/opt/tool", "opt/tool"},
		{"a/b/c", "../../x", "x"},
		{"link", "y", "y"},
		{"src/pointer", "/../../etc/x", "etc/x"},
		{"src/pointer", "./sibling", "src/sibling"},
		{"src/pointer", "", ""},
	}
	for _, c := range cases {
		t.Run("rel="+c.rel+" target="+c.target, func(t *testing.T) {
			if got := contextLinkName(c.rel, c.target); got != c.want {
				t.Fatalf("contextLinkName(%q, %q) = %q, want %q", c.rel, c.target, got, c.want)
			}
		})
	}
}

func TestNamesRedactDenyTokens(t *testing.T) {
	t.Run("case-insensitive-path", func(t *testing.T) {
		toks := []denyToken{namesTok(1, "sample", "user")}
		got := redactDenyTokens("home/SampleUser/notes", toks)
		if want := "home/[deny-token:1]/notes"; got != want {
			t.Fatalf("redactDenyTokens = %q, want %q", got, want)
		}
	})
	t.Run("overlapping-tokens-longest-first", func(t *testing.T) {
		toks := []denyToken{
			namesTok(1, "corp"),
			namesTok(2, "example", "corp"),
		}
		got := redactDenyTokens("examplecorp", toks)
		if want := "[deny-token:2]"; got != want {
			t.Fatalf("redactDenyTokens = %q, want %q", got, want)
		}
		assertNoTokenLiteral(t, got, toks)
	})
	t.Run("overlapping-tokens-shorter-alone", func(t *testing.T) {
		toks := []denyToken{
			namesTok(1, "corp"),
			namesTok(2, "example", "corp"),
		}
		got := redactDenyTokens("corp", toks)
		if want := "[deny-token:1]"; got != want {
			t.Fatalf("redactDenyTokens = %q, want %q", got, want)
		}
		assertNoTokenLiteral(t, got, toks)
	})
	t.Run("longer-wins-at-same-position", func(t *testing.T) {
		// "abcd" is a prefix of "abcdef" at the same start: longest-first
		// must take the longer token, or a fragment of its literal
		// survives the output.
		toks := []denyToken{
			namesTok(9, "abc", "def"),
			namesTok(2, "abc", "d"),
		}
		got := redactDenyTokens("abcdef tail", toks)
		if want := "[deny-token:9] tail"; got != want {
			t.Fatalf("redactDenyTokens = %q, want %q", got, want)
		}
		assertNoTokenLiteral(t, got, toks)
	})
	t.Run("tie-lower-line-first", func(t *testing.T) {
		toks := []denyToken{
			namesTok(4, "duplex", "tok"),
			namesTok(2, "duplex", "tok"),
		}
		if got := redactDenyTokens("duplextok", toks); got != "[deny-token:2]" {
			t.Fatalf("redactDenyTokens = %q, want [deny-token:2]", got)
		}
	})
	t.Run("multiple-occurrences", func(t *testing.T) {
		toks := []denyToken{namesTok(5, "secret", "path")}
		got := redactDenyTokens("x/secretpath/y/SECRETPATH/z", toks)
		if want := "x/[deny-token:5]/y/[deny-token:5]/z"; got != want {
			t.Fatalf("redactDenyTokens = %q, want %q", got, want)
		}
		assertNoTokenLiteral(t, got, toks)
	})
	t.Run("ascii-only-folding", func(t *testing.T) {
		// A non-ASCII accented byte must not fold into an ASCII letter.
		toks := []denyToken{namesTok(1, "sample", "user")}
		s := "sampleéuser"
		if got := redactDenyTokens(s, toks); got != s {
			t.Fatalf("redactDenyTokens(%q) = %q, want unchanged", s, got)
		}
	})
	t.Run("no-hit-unchanged", func(t *testing.T) {
		toks := []denyToken{namesTok(1, "sample", "user")}
		s := "clean/path"
		if got := redactDenyTokens(s, toks); got != s {
			t.Fatalf("redactDenyTokens = %q, want unchanged %q", got, s)
		}
	})
	t.Run("no-tokens-unchanged", func(t *testing.T) {
		s := "home/SampleUser/notes"
		if got := redactDenyTokens(s, nil); got != s {
			t.Fatalf("redactDenyTokens = %q, want unchanged %q", got, s)
		}
	})
}

// assertNoTokenLiteral fails if any token literal survives in got in any
// case (searched case-insensitively).
func assertNoTokenLiteral(t *testing.T, got string, toks []denyToken) {
	t.Helper()
	lg := lowerASCIIString(got)
	for _, tok := range toks {
		needle := lowerASCIIString(string(tok.needle))
		if strings.Contains(lg, needle) {
			t.Fatalf("token literal (line %d) survived in %q", tok.line, got)
		}
	}
}

func TestNamesRedactedDisplay(t *testing.T) {
	cases := []struct {
		prefix  string
		ordinal int
		want    string
	}{
		{"layer3:", 17, "layer3:entry#17"},
		{"", 4, "entry#4"},
		{"ctx:", 1, "ctx:entry#1"},
	}
	for _, c := range cases {
		t.Run(c.prefix, func(t *testing.T) {
			if got := redactedDisplay(c.prefix, c.ordinal); got != c.want {
				t.Fatalf("redactedDisplay(%q, %d) = %q, want %q", c.prefix, c.ordinal, got, c.want)
			}
		})
	}
}

// TestNamesRedactPlaceholderNeverSpellsToken: a token that the
// placeholder itself spells ("deny", "token", "deny-token:1"), or that a
// placeholder forms with the following text, never survives.
func TestNamesRedactPlaceholderNeverSpellsToken(t *testing.T) {
	cases := []struct {
		name string
		s    string
		toks []denyToken
	}{
		{"deny", "a-deny-b", []denyToken{{line: 1, needle: []byte("deny")}}},
		{"token-word", "xxtoken", []denyToken{{line: 4, needle: []byte("token")}}},
		{"placeholder-body", "deny-token:1", []denyToken{{line: 1, needle: []byte("deny-token:1")}}},
		{"boundary-join", "abcdZZZ", []denyToken{{line: 1, needle: []byte("abcd")}, {line: 2, needle: []byte("]zzz")}}},
		{"introduces-other", "secretword", []denyToken{{line: 7, needle: []byte("secretword")}, {line: 8, needle: []byte("ken:7")}}},
		{"case-mixed", "DeNy", []denyToken{{line: 3, needle: []byte("deny")}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redactDenyTokens(c.s, c.toks)
			lg := lowerASCIIString(got)
			for _, tk := range c.toks {
				if strings.Contains(lg, lowerASCIIString(string(tk.needle))) {
					t.Fatalf("token %q survives in %q", tk.needle, got)
				}
			}
			if got == c.s {
				t.Fatalf("nothing redacted in %q", got)
			}
		})
	}
	// Control: an ordinary token keeps its informative placeholder.
	if got := redactDenyTokens("home/sampleuser/x", []denyToken{{line: 2, needle: []byte("sampleuser")}}); got != "home/[deny-token:2]/x" {
		t.Fatalf("ordinary redaction = %q", got)
	}
}
