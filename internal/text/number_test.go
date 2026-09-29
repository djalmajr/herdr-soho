package text

import (
	"math"
	"testing"
)

func TestParseJSNumber(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  float64
		ok    bool
	}{
		{input: "", want: 0, ok: true},
		{input: "60000 ", want: 60_000, ok: true},
		{input: "1e3", want: 1_000, ok: true},
		{input: "12.5", want: 12.5, ok: true},
		{input: "0x10", want: 16, ok: true},
		{input: "xyz", ok: false},
		{input: "+0x10", ok: false},
	} {
		got, ok := ParseJSNumber(tc.input)
		if ok != tc.ok || ok && got != tc.want {
			t.Errorf("ParseJSNumber(%q) = %v, %t; want %v, %t", tc.input, got, ok, tc.want, tc.ok)
		}
	}
	if got, ok := ParseJSNumber("Infinity"); !ok || !math.IsInf(got, 1) {
		t.Fatalf("ParseJSNumber(Infinity) = %v, %t", got, ok)
	}
}
