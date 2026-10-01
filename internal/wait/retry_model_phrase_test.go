package wait

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// TestStatusModelRetryPhraseNoCause guards the real case: a working agent whose
// screen shows the model talking about a retry ("Retrying with the correct
// text.") must not be reported as a provider retry. The status line keeps the
// five-column form (no "retrying:" cause) and the exit code is unchanged.
func TestStatusModelRetryPhraseNoCause(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(base, "cwd")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(base, "state")
	if err := os.MkdirAll(filepath.Join(state, "ws", "wait"), 0o700); err != nil {
		t.Fatal(err)
	}
	row := "worker\tp0a\tclaude\timplementer\tanthropic\t\t\t\tmodel-x\t\t\t\n"
	if err := os.WriteFile(filepath.Join(state, "ws", "agents.tsv"), []byte("# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n"+row), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(base, "bin")
	modelPhrase := "Retrying with the correct text.\n"
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`},
		{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Stdout: modelPhrase},
	}
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{
		"PATH":                      bin,
		"HERDR_ENV":                 "1",
		"HERDR_SOHO_DIR":            state,
		"HERDR_WORKSPACE_ID":        "ws",
		"HERDR_SOCKET_PATH":         filepath.Join(base, "none.sock"),
		"HERDR_SOHO_FAKECLI_CONFIG": bin,
	}
	ctx := &core.Config{Entries: map[string]core.ConfigEntry{}}
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	code := CmdStatus([]string{"worker"}, ctx, env, cwd)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	outStr, errStr := out.String(), stderr.String()
	if strings.Contains(outStr, "retrying:") {
		t.Fatalf("out=%q carries a retrying: cause for the model's wording", outStr)
	}
	if code != 0 || outStr != "worker\tworking\t\t-\t-\n" {
		t.Fatalf("code=%d out=%q stderr=%q, want the five-column working line", code, outStr, errStr)
	}
}
