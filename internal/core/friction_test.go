package core_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/cli"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestCleanHeldRosterLockRecordsFriction(t *testing.T) {
	// Mutation captured: a plain lock timeout exits clean without an error row in friction.log.
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	skill := filepath.Join(root, "skill")
	state := filepath.Join(root, "state", "ws")
	for _, dir := range []string{bin, skill, state} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# herdr-soho\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte("ghost\tpane\tgrok\timplementer\txai\t0\t/tmp\tnow\t\t\t\t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(state, "agents.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{
		"HERDR_ENV": "1", "HERDR_SOHO_DIR": filepath.Join(root, "state"), "HERDR_SOHO_FAKECLI_CONFIG": bin,
		"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HOME": root, "PATH": bin, "TMPDIR": root,
	}
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var stdout, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &stdout, &stderr
	defer func() { platform.Stdout, platform.Stderr = oldOut, oldErr }()
	if code := cli.Run([]string{"clean"}, env); code != 4 {
		t.Fatalf("clean exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	log, err := os.ReadFile(filepath.Join(state, "friction.log"))
	if err != nil || !strings.Contains(string(log), "error(exit 4)\tclean\troster lock ") {
		t.Fatalf("friction.log=%q err=%v", log, err)
	}
}
