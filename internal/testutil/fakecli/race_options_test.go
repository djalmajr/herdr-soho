package fakecli

import (
	"strings"
	"testing"
)

func TestRaceOptionsWithoutBaseGORACE(t *testing.T) {
	if got := RaceOptions([]string{"PATH=/bin", "FOO=bar", "no-equals-entry"}); got != "atexit_sleep_ms=0" {
		t.Fatalf("RaceOptions = %q, want %q", got, "atexit_sleep_ms=0")
	}
}

func TestRaceOptionsKeepsDetectorOptionsAndReplacesAtexitSleep(t *testing.T) {
	base := []string{"GORACE=halt_on_error=1 atexit_sleep_ms=1000"}
	if got := RaceOptions(base); got != "halt_on_error=1 atexit_sleep_ms=0" {
		t.Fatalf("RaceOptions = %q, want %q", got, "halt_on_error=1 atexit_sleep_ms=0")
	}
}

// A lowercase gorace key is honored the same way Env honors it today: Env
// drops the base entry from the environment it returns and carries its
// options into the GORACE entry it appends.
func TestRaceOptionsHonorsLowercaseKeyTheWayEnvDoes(t *testing.T) {
	base := []string{"gorace=halt_on_error=1 atexit_sleep_ms=1000", "KEEP=me"}
	const want = "halt_on_error=1 atexit_sleep_ms=0"
	if got := RaceOptions(base); got != want {
		t.Fatalf("RaceOptions = %q, want %q", got, want)
	}
	if got := envGorace(t, Env(base, t.TempDir())); got != want {
		t.Fatalf("Env GORACE = %q, want %q", got, want)
	}
}

// Env's GORACE entry is exactly "GORACE=" plus RaceOptions on the same
// base, which keeps Env's output byte-identical after the extraction.
func TestEnvGoraceEntryIsRaceOptions(t *testing.T) {
	base := []string{"GORACE=halt_on_error=1 atexit_sleep_ms=1000", "KEEP=me"}
	dir := t.TempDir()
	env := Env(base, dir)
	if got, want := envGorace(t, env), RaceOptions(base); got != want {
		t.Fatalf("Env GORACE = %q, want %q (GORACE=+RaceOptions)", got, want)
	}
	foundKeep := false
	for _, entry := range env {
		if entry == "KEEP=me" {
			foundKeep = true
		}
	}
	if !foundKeep {
		t.Fatalf("Env dropped an unrelated base entry: %v", env)
	}
}

// envGorace returns the value of the GORACE entry of env (any key case) and
// fails when the entry is missing or appears more than once.
func envGorace(t *testing.T, env []string) string {
	t.Helper()
	got := ""
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !strings.EqualFold(key, "gorace") {
			continue
		}
		if got != "" {
			t.Fatalf("multiple GORACE entries in %v", env)
		}
		got = entry[len(key)+1:]
	}
	if got == "" {
		t.Fatalf("no GORACE entry in %v", env)
	}
	return got
}
