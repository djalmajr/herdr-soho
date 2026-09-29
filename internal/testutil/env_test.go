package testutil

import (
	"os"
	"strings"
	"testing"
)

func TestCleanEnvDropsInheritedHerdrValuesAndUsesMissingSocket(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOHO_DIR", "inherited-state")
	t.Setenv("HERDR_SOCKET_PATH", "inherited.sock")

	env := CleanEnv(t)
	values := make(map[string]string, len(env))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[strings.ToUpper(key)] = value
		}
	}
	if _, ok := values["HERDR_ENV"]; ok {
		t.Fatal("inherited HERDR_ENV reached the fixture")
	}
	if _, ok := values["HERDR_SOHO_DIR"]; ok {
		t.Fatal("inherited HERDR_SOHO_DIR reached the fixture")
	}
	socket := values["HERDR_SOCKET_PATH"]
	if socket == "" || socket == "inherited.sock" {
		t.Fatalf("HERDR_SOCKET_PATH=%q", socket)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("HERDR_SOCKET_PATH exists or could not be checked: %v", err)
	}
}

func TestCleanEnvGivesEachTestItsOwnTMPDIR(t *testing.T) {
	// Mutation captured: keeping the inherited TMPDIR lets a model cache one
	// package writes there change another package's output.
	inherited := t.TempDir()
	t.Setenv("TMPDIR", inherited)
	var got []string
	for _, item := range CleanEnv(t) {
		if strings.HasPrefix(item, "TMPDIR=") {
			got = append(got, item)
		}
	}
	if len(got) != 1 || got[0] == "TMPDIR="+inherited {
		t.Fatalf("TMPDIR entries = %q, want one test-owned directory", got)
	}
}
