package core

import (
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// TestJobFinishingTimeoutConfigValidation: the machine key
// job_finishing_timeout is known to ConfigKeyOk, validates to 1..1440
// minutes in ConfigValueOk (the same path as job_timeout), and stays out of
// ConfigScalarKeys (the scalar oracle's row order is frozen and the machine
// keys have their own loader).
func TestJobFinishingTimeoutConfigValidation(t *testing.T) {
	env := platform.Env{}
	cwd := t.TempDir()
	if !ConfigKeyOk("job_finishing_timeout") {
		t.Fatal("ConfigKeyOk(job_finishing_timeout) = false")
	}
	for _, key := range ConfigScalarKeys {
		if key == "job_finishing_timeout" {
			t.Fatal("job_finishing_timeout joined ConfigScalarKeys")
		}
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"1", true},
		{"10", true},
		{"1440", true},
		// The sibling keys take a zero-padded decimal (the digit pattern
		// and Atoi accept it), so this key does too: 010 is 10.
		{"010", true},
		{"0", false},
		{"-1", false},
		{"1441", false},
		{"10m", false},
		{"1.5", false},
		{"", false},
		{" 10", false},
		{"+5", false},
	} {
		if got := ConfigValueOk("job_finishing_timeout", tc.value, env, cwd); got != tc.want {
			t.Errorf("ConfigValueOk(job_finishing_timeout, %q) = %t, want %t", tc.value, got, tc.want)
		}
		// The sibling job_timeout (the same 1..1440 range) must agree on
		// every case: one validation path.
		if got, sibling := ConfigValueOk("job_finishing_timeout", tc.value, env, cwd), ConfigValueOk("job_timeout", tc.value, env, cwd); got != sibling {
			t.Errorf("job_finishing_timeout %q = %t but job_timeout = %t", tc.value, got, sibling)
		}
	}
	// The generic refusal still applies: a value with the banned
	// characters is refused before the range is looked at.
	if ConfigValueOk("job_finishing_timeout", "10#x", env, cwd) {
		t.Fatal("a value with a comment marker was accepted")
	}
}
