package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// JS: "pi and opencode warn on approvals edits; FULL dies 2" — the exit-2 half,
// through the spawn command in process (the warnings are in internal/kinds).
func TestSpawnInvalidApprovalExitsTwo(t *testing.T) {
	env, repo := commandFixture(t)
	bin := t.TempDir()
	if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{{AnyArgs: true}}); err != nil {
		t.Fatal(err)
	}
	env["PATH"] = bin
	env["HERDR_SOHO_FAKECLI_CONFIG"] = bin
	env["HERDR_ENV"] = "1"
	env["HERDR_SOHO_LANES"] = "off"
	env["HERDR_SOCKET_PATH"] = filepath.Join(t.TempDir(), "missing", "herdr.sock")
	env["USERPROFILE"] = env["HOME"]
	code, out, errOut := runIn(t, []string{"spawn", "implementer", "--kind", "pi", "--approvals", "FULL"}, env, repo)
	if code != 2 || !strings.Contains(out+errOut, "invalid approvals 'FULL' (ask|edits|full)") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}
