package core_test

import (
	"testing"

	"github.com/djalmajr/soho/internal/core"
)

func TestParseTimeoutMSValid(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"500", 500},
		{"0", 0},
		{"007", 7},
		{"10800", 10800},
		{"9223372036854775807", 9223372036854775807},
		{"500ms", 500},
		{"0ms", 0},
		{"9223372036854775807ms", 9223372036854775807},
		{"1s", 1000},
		{"30s", 30000},
		{"1m", 60000},
		{"90m", 5400000},
		{"1h", 3600000},
		{"2h", 7200000},
		{"2562047788015h", 9223372036854000000},
	}
	for _, tc := range cases {
		if got, ok := core.ParseTimeoutMS(tc.raw); !ok || got != tc.want {
			t.Errorf("ParseTimeoutMS(%q) = %d, %v; want %d, true", tc.raw, got, ok, tc.want)
		}
	}
}

func TestParseTimeoutMSRefused(t *testing.T) {
	cases := []string{
		"",            // no number
		"1.5", "1.5s", // decimals
		"-5", "-5s", "+5", // signs
		" 5", "5 ", "1 s", // whitespace
		"1S", "1M", "1H", "500MS", // upper-case unit
		"1h30m", "1h 30m", // combinations
		"ms", "s", "m", "h", // unit without a number
		"1x",          // unknown unit
		"1e3", "0x10", // not a plain integer
		"18446744073709551616",  // 2^64: int64 overflow
		"9223372036854775808",   // 2^63: int64 overflow
		"9999999999999999999ms", // overflow in the number
		"2562047788016h",        // overflow in the multiplication (2562047788015h is the last valid)
	}
	for _, raw := range cases {
		if got, ok := core.ParseTimeoutMS(raw); ok {
			t.Errorf("ParseTimeoutMS(%q) = %d, true; want refused", raw, got)
		}
	}
}

func TestFormatTimeoutMS(t *testing.T) {
	cases := map[int64]string{
		0:          "0ms",
		999:        "999ms",
		10800:      "10800ms",
		1500:       "1500ms",
		1000:       "1s",
		90000:      "90s",
		60000:      "1m",
		5400000:    "90m",
		1200000:    "20m",
		3600000:    "1h",
		10800000:   "3h",
		5400000000: "1500h",
	}
	for ms, want := range cases {
		if got := core.FormatTimeoutMS(ms); got != want {
			t.Errorf("FormatTimeoutMS(%d) = %q; want %q", ms, got, want)
		}
	}
}

func TestIsPlainTimeoutMS(t *testing.T) {
	plain := []string{"0", "500", "999", "10800000"}
	units := []string{"500ms", "1s", "90s", "1m", "90m", "2h", "", "1.5", "-5", " 5"}
	for _, raw := range plain {
		if !core.IsPlainTimeoutMS(raw) {
			t.Errorf("IsPlainTimeoutMS(%q) = false; want true", raw)
		}
	}
	for _, raw := range units {
		if core.IsPlainTimeoutMS(raw) {
			t.Errorf("IsPlainTimeoutMS(%q) = true; want false", raw)
		}
	}
}
