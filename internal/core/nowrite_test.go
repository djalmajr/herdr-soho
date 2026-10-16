package core

import (
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

// DJA-318: the read-only gate is SOHO_NOWRITE=1 alone. The former
// HERDR_SOHO_NOWRITE is no longer read, whatever the new name holds.
func TestNowriteSohoNameDecides(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  platform.Env
		want bool
	}{
		{"absent", platform.Env{}, false},
		{"new 1", platform.Env{"SOHO_NOWRITE": "1"}, true},
		{"new 0", platform.Env{"SOHO_NOWRITE": "0"}, false},
		{"new empty", platform.Env{"SOHO_NOWRITE": ""}, false},
		{"new 1 with legacy 0", platform.Env{"SOHO_NOWRITE": "1", "HERDR_SOHO_NOWRITE": "0"}, true},
		{"new 0 with legacy 1", platform.Env{"SOHO_NOWRITE": "0", "HERDR_SOHO_NOWRITE": "1"}, false},
		{"legacy 1 only", platform.Env{"HERDR_SOHO_NOWRITE": "1"}, false},
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
