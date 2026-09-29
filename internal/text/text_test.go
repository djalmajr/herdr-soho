package text

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func TestPortedTextCases(t *testing.T) {
	t.Run("sanitizeCause: one line, printable only, spaces collapsed, at most 200 chars", func(t *testing.T) {
		// JS: "sanitizeCause: one line, printable only, spaces collapsed, at most 200 chars"
		if got := SanitizeCause("a\nb\tc"); got != "a b c" {
			t.Fatalf("SanitizeCause() = %q", got)
		}
		if got := SanitizeCause("x\x1b[31my\r\n z"); got != "x[31my z" {
			t.Fatalf("SanitizeCause(controls) = %q", got)
		}
		if got := SanitizeCause("a  b   c "); got != "a b c" {
			t.Fatalf("SanitizeCause(spaces) = %q", got)
		}
		if got := SanitizeCause(strings.Repeat("a", 300)); len(got) != 200 {
			t.Fatalf("SanitizeCause length = %d", len(got))
		}
		if got := SanitizeCause(""); got != "" {
			t.Fatalf("SanitizeCause(empty) = %q", got)
		}
	})
	t.Run("sanitizeCause: the shared text helper (text.mjs)", func(t *testing.T) {
		// JS: "sanitizeCause: the shared text helper (text.mjs)"
		if got := SanitizeCause("a\nb\tc"); got != "a b c" {
			t.Fatalf("SanitizeCause() = %q", got)
		}
		if got := SanitizeCause("x\x1b[31my\r\n z"); got != "x[31my z" {
			t.Fatalf("SanitizeCause(controls) = %q", got)
		}
		if got := SanitizeCause("a  b   c "); got != "a b c" {
			t.Fatalf("SanitizeCause(spaces) = %q", got)
		}
		if got := SanitizeCause(strings.Repeat("a", 300)); len(got) != 200 {
			t.Fatalf("SanitizeCause length = %d", len(got))
		}
	})
	t.Run("redactSecrets: the four sed passes in order", func(t *testing.T) {
		// JS: "redactSecrets: the four sed passes in order"
		cases := map[string]string{
			"Bearer abc123.~+/": "Bearer [redacted]", "bearer xyz-123": "Bearer [redacted]",
			"pk-proj-abcdefgh12": "[redacted]", "token=sk_live_abcdefghij": "token=[redacted]",
			"secret: sk_test_a1b2c3 rest": "secret: [redacted] rest", "sk-ant-123": "sk-ant-123",
		}
		for input, want := range cases {
			if got := RedactSecrets(input); got != want {
				t.Errorf("RedactSecrets(%q) = %q, want %q", input, got, want)
			}
		}
	})
	t.Run("redactSecrets: the test-probe.sh hyphen-key inputs (ported)", func(t *testing.T) {
		// JS: "redactSecrets: the test-probe.sh hyphen-key inputs (ported)"
		cases := map[string]string{
			"sk-proj-abcDEF123456": "[redacted]", "sk-ant-api01-XYZ12345": "[redacted]",
			"pk-live12345678": "[redacted]", "key sk_live_987654321": "key [redacted]",
		}
		for input, want := range cases {
			if got := RedactSecrets(input); got != want {
				t.Errorf("RedactSecrets(%q) = %q, want %q", input, got, want)
			}
		}
	})
}

func TestTextDifferential(t *testing.T) {
	type row struct {
		Input    *string `json:"input"`
		Sanitize string  `json:"sanitize"`
		Redact   string  `json:"redact"`
		List     string  `json:"list"`
		Word     string  `json:"word"`
		HasWord  *bool   `json:"hasWord"`
	}
	data, err := os.ReadFile("testdata/text.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []row
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if row.HasWord == nil && row.Input == nil {
				t.Fatalf("text.json row %d has no differential operation", i)
			}
			if row.HasWord != nil && row.Input != nil {
				t.Fatalf("text.json row %d has conflicting differential operations", i)
			}
			if row.HasWord != nil {
				if got := HasWord(row.List, row.Word); got != *row.HasWord {
					t.Fatalf("HasWord(%q, %q) = %t", row.List, row.Word, got)
				}
				return
			}
			if got := SanitizeCause(*row.Input); got != row.Sanitize {
				t.Errorf("SanitizeCause(%q) = %q, JS %q", *row.Input, got, row.Sanitize)
			}
			if got := RedactSecrets(*row.Input); got != row.Redact {
				t.Errorf("RedactSecrets(%q) = %q, JS %q", *row.Input, got, row.Redact)
			}
		})
	}
}

func TestASCIITextHelpers(t *testing.T) {
	if got := ASCIILower("API ſ K İ Z"); got != "api ſ K İ z" {
		t.Fatalf("ASCIILower = %q", got)
	}
	if got := ASCIILower("á😀ABC"); got != "á😀abc" {
		t.Fatalf("ASCIILower = %q", got)
	}
}

func TestJSLowerDifferential(t *testing.T) {
	// Mutation captured: using Go's simple lowercase mapping diverges from Node's full and contextual mappings.
	type row struct {
		Input string `json:"input"`
		Lower string `json:"lower"`
	}
	data, err := os.ReadFile("testdata/jslower.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []row
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := JSLower(row.Input); got != row.Lower {
				t.Fatalf("JSLower(%q) = %q, Node toLowerCase() %q", row.Input, got, row.Lower)
			}
		})
	}
}

func TestStripMarksNFDDifferential(t *testing.T) {
	// Mutation captured: skipping a canonical decomposition leaves marks in headings.
	type row struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	}
	data, err := os.ReadFile("testdata/nfd.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []row
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) < 4 {
		t.Fatalf("nfd.json has only %d differential cases", len(rows))
	}
	for i, row := range rows {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := StripMarksNFD(row.Input); got != row.Output {
				t.Fatalf("StripMarksNFD(%q) = %q, Node NFD without marks %q", row.Input, got, row.Output)
			}
		})
	}
}

func TestStripMarksNFDOrdersSurvivingCombiningMarks(t *testing.T) {
	// Mutation captured: omitting canonical ordering leaves surviving marks unlike String.prototype.normalize('NFD').
	for _, tc := range []struct {
		name, input, want string
	}{
		{name: "Devanagari nukta and virama", input: "\u0930\u094d\u093c", want: "\u0930\u093c\u094d"},
		{name: "Latin starter with marks outside removed range", input: "a\U00001dff\U00001dce", want: "a\U00001dce\U00001dff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripMarksNFD(tc.input); got != tc.want {
				t.Fatalf("StripMarksNFD(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestToWellFormedUTF8AgainstNodeWriteFile(t *testing.T) {
	type row struct {
		Units       []uint16 `json:"units"`
		ExpectedHex string   `json:"expectedHex"`
	}
	data, err := os.ReadFile("testdata/wellformed.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []row
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			input := encodeWTF8Units(row.Units)
			got := hex.EncodeToString([]byte(ToWellFormedUTF8(input)))
			if got != row.ExpectedHex {
				t.Fatalf("ToWellFormedUTF8() hex=%s, Node fs.writeFileSync=%s", got, row.ExpectedHex)
			}
		})
	}
}

func encodeWTF8Units(units []uint16) string {
	var out []byte
	for i := 0; i < len(units); i++ {
		unit := units[i]
		if 0xd800 <= unit && unit <= 0xdbff && i+1 < len(units) && 0xdc00 <= units[i+1] && units[i+1] <= 0xdfff {
			out = utf8.AppendRune(out, rune(utf16.DecodeRune(rune(unit), rune(units[i+1]))))
			i++
		} else if unit >= 0xd800 && unit <= 0xdfff {
			out = append(out, 0xe0|byte(unit>>12), 0x80|byte((unit>>6)&0x3f), 0x80|byte(unit&0x3f))
		} else {
			out = utf8.AppendRune(out, rune(unit))
		}
	}
	return string(out)
}

func TestCompareUTF16(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"a", "b", -1}, {"é", "😀", -1}, {"\uE000", "😀", 1}, {"x", "x", 0}, {"a", "aa", -1},
	} {
		if got := CompareUTF16(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareUTF16(%q, %q)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSortUTF16(t *testing.T) {
	// Mutation captured: sorting by UTF-8 bytes places U+E000 before the emoji.
	values := []string{"\uE000", "😀", "a"}
	SortUTF16(values)
	if got := strings.Join(values, ","); got != "a,😀,\uE000" {
		t.Fatalf("SortUTF16() = %q, want %q", got, "a,😀,\uE000")
	}
}

func TestSortUTF16MatchesDefaultJavaScriptOrder(t *testing.T) {
	t.Run("compareCodeUnits follows default JavaScript sort order", func(t *testing.T) { // JS: "compareCodeUnits follows default JavaScript sort order"
		// Mutation captured: locale or UTF-8 ordering reverses at least one of these pairs.
		for _, tc := range []struct {
			input []string
			want  []string
		}{
			{[]string{"B", "a"}, []string{"B", "a"}},
			{[]string{"é", "f"}, []string{"f", "é"}},
			{[]string{"\uE000", "😀"}, []string{"😀", "\uE000"}},
		} {
			SortUTF16(tc.input)
			if len(tc.input) != len(tc.want) || tc.input[0] != tc.want[0] || tc.input[1] != tc.want[1] {
				t.Fatalf("SortUTF16() = %#v, want %#v", tc.input, tc.want)
			}
		}
		if got := CompareUTF16("same", "same"); got != 0 {
			t.Fatalf("CompareUTF16(equal) = %d, want 0", got)
		}
	})
}

func TestStripMarksNFDKeepsTheGraphemeJoinerBoundary(t *testing.T) {
	// Mutation captured: removing U+034F before the canonical ordering lets
	// U+0591 (class 220) move ahead of U+0592 (class 230) across it; the JS
	// orders the whole NFD string first. Oracle: node, "a֒͏֑"
	// .normalize("NFD").replace(/[̀-ͯ]/g, "") is 61 592 591.
	if got, want := StripMarksNFD("a֒͏֑"), "a֑֒"; got != want {
		t.Fatalf("StripMarksNFD = %U, want %U", []rune(got), []rune(want))
	}
	// Without the joiner the two marks are one segment and are ordered.
	if got, want := StripMarksNFD("a֑֒"), "a֑֒"; got != want {
		t.Fatalf("StripMarksNFD = %U, want %U", []rune(got), []rune(want))
	}
}
