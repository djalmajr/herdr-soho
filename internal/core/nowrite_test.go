package core

import (
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

// DJA-318 C2b: when SOHO_NOWRITE is defined it decides alone — even the
// value 0 — and the legacy HERDR_SOHO_NOWRITE only applies when the new
// name is absent. The two names are no longer OR-combined.
func TestNowriteSohoNameWins(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  platform.Env
		want bool
	}{
		{"absent", platform.Env{}, false},
		{"new 1", platform.Env{"SOHO_NOWRITE": "1"}, true},
		{"new 1 over legacy 0", platform.Env{"SOHO_NOWRITE": "1", "HERDR_SOHO_NOWRITE": "0"}, true},
		{"new 0 over legacy 1", platform.Env{"SOHO_NOWRITE": "0", "HERDR_SOHO_NOWRITE": "1"}, false},
		{"legacy 1 only", platform.Env{"HERDR_SOHO_NOWRITE": "1"}, true},
		{"legacy 0 only", platform.Env{"HERDR_SOHO_NOWRITE": "0"}, false},
		{"legacy junk", platform.Env{"HERDR_SOHO_NOWRITE": "off"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Nowrite(tc.env); got != tc.want {
				t.Fatalf("Nowrite(%v) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}
