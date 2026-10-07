package text

import "testing"

// TestJSLowerUnicodeVersionPinning guards the version and provenance of the
// generated tables. The differential oracle (testdata/jslower.json) only pins
// the observable behavior for its inputs; these constants additionally pin
// the exact Unicode version and the SHA256 of the normative source bytes the
// tables were generated from, so a regeneration from a different or tampered
// Unicode source fails here even if the oracle inputs would still pass.
func TestJSLowerUnicodeVersionPinning(t *testing.T) {
	if got := lowerCase17Version; got != "17.0.0" {
		t.Fatalf("lowerCase17Version = %q, want %q", got, "17.0.0")
	}
	want := map[string]string{
		"UnicodeData.txt":           "2e1efc1dcb59c575eedf5ccae60f95229f706ee6d031835247d843c11d96470c",
		"DerivedCoreProperties.txt": "24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08",
		"SpecialCasing.txt":         "efc25faf19de21b92c1194c111c932e03d2a5eaf18194e33f1156e96de4c9588",
	}
	for name, sum := range want {
		if got := lowerCase17SourceSHA256[name]; got != sum {
			t.Errorf("lowerCase17SourceSHA256[%q] = %s, want %s", name, got, sum)
		}
	}
	// U+03A3 must stay out of the table: its lowercase mapping is contextual
	// (final sigma), handled by JSLower.
	if _, ok := jslower17[rune(0x03A3)]; ok {
		t.Fatal("jslower17 must not contain U+03A3 (final sigma is contextual)")
	}
	// Named context anchors for the final-sigma rule around the pinned tables.
	if got := JSLower("A\u03A3"); got != "a\u03C2" {
		t.Fatalf(`JSLower("A\u03A3") = %q, want "a\u03C2" (final sigma)`, got)
	}
	if got := JSLower("A\u03A3B"); got != "a\u03C3b" {
		t.Fatalf(`JSLower("A\u03A3B") = %q, want "a\u03C3b" (non-final sigma)`, got)
	}
	// U+0897 (ARABIC PEPET) is Case_Ignorable in Unicode 17.0.0 and therefore
	// does not break the final-sigma context.
	if got := JSLower("A\u0897\u03A3"); got != "a\u0897\u03C2" {
		t.Fatalf(`JSLower("A\u0897\u03A3") = %q, want "a\u0897\u03C2"`, got)
	}
}
