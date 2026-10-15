package core

import (
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

// TestJobBlockedTimeoutConfigValidation: the machine key job_blocked_timeout
// is known to ConfigKeyOk, validates to 1..43200 minutes (one minute to 30
// days) in ConfigValueOk, and stays out of ConfigScalarKeys (the scalar
// oracle's row order is frozen and the machine keys have their own
// loader).
func TestJobBlockedTimeoutConfigValidation(t *testing.T) {
	env := platform.Env{}
	cwd := t.TempDir()
	if !ConfigKeyOk("job_blocked_timeout") {
		t.Fatal("ConfigKeyOk(job_blocked_timeout) = false")
	}
	for _, key := range ConfigScalarKeys {
		if key == "job_blocked_timeout" {
			t.Fatal("job_blocked_timeout joined ConfigScalarKeys")
		}
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"1", true},
		{"1440", true},
		{"43200", true},
		{"0", false},
		{"43201", false},
		{"-1", false},
		{"90m", false},
		{"1.5", false},
		{"", false},
		{" 1440", false},
	} {
		if got := ConfigValueOk("job_blocked_timeout", tc.value, env, cwd); got != tc.want {
			t.Errorf("ConfigValueOk(job_blocked_timeout, %q) = %t, want %t", tc.value, got, tc.want)
		}
	}
	// The generic refusal still applies: a value with the banned
	// characters is refused before the range is looked at.
	if ConfigValueOk("job_blocked_timeout", "1440#x", env, cwd) {
		t.Fatal("a value with a comment marker was accepted")
	}
}
