package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestWaitMirroringEmptyExitErrorRecordsFriction(t *testing.T) { // JS: "DieError vazio no mirror depois do friction"
	base := t.TempDir()
	cwd := filepath.Join(base, "cwd")
	stateRoot := filepath.Join(base, "state")
	tmp := filepath.Join(base, "tmp")
	bin := filepath.Join(base, "bin")
	state := filepath.Join(stateRoot, "w0test")
	reportDir := filepath.Join(tmp, "herdr-soho", "w0test", "reports")
	for _, dir := range []string{cwd, filepath.Join(state, "wait"), filepath.Join(state, "reports"), reportDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte("# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\nw0test\tp0a\tclaude\timplementer\t\t\t\t\t\t\t\t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(reportDir, "w0test.md")
	if err := os.WriteFile(report, []byte("done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "last-report-w0test"), []byte(report+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paneJSON := `{"result":{"pane":{"workspace_id":"w0test"}}}`
	rules := []fakecli.Rule{
		{Argv: []string{"pane", "current", "--current"}, Call: 1, Stdout: paneJSON},
		{Argv: []string{"pane", "current", "--current"}, Call: 2, Stdout: paneJSON},
		{Argv: []string{"pane", "current", "--current"}, Call: 3, Stdout: paneJSON},
		{Argv: []string{"pane", "current", "--current"}, Call: 4, Stdout: paneJSON},
		{Argv: []string{"pane", "current", "--current"}, Call: 5, Stderr: "down\n", Code: 1},
	}
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	env := envFrom(testutil.CleanEnv(t))
	env["PATH"] = bin
	env["HOME"] = filepath.Join(base, "home")
	env["XDG_CONFIG_HOME"] = filepath.Join(base, "config")
	env["HERDR_ENV"] = "1"
	env["HERDR_SOHO_DIR"] = stateRoot
	env["HERDR_SOHO_SKILL_DIR"] = testSkillDir(t)
	env["HERDR_SOHO_PRESSURE_DISK_FREE_PERCENT"] = "0"
	env["HERDR_SOHO_PRESSURE_SWAP_PERCENT"] = "0"
	env["TMPDIR"] = tmp
	env["HERDR_SOHO_WAIT_POLL_MS"] = "1"
	delete(env, "HERDR_WORKSPACE_ID")
	env = withFakeCLI(env, bin)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })

	code := Run([]string{"wait", "w0test", "--timeout", "2000"}, env)
	if code != 1 || !strings.HasSuffix(stderr.String(), "herdr-soho: \n") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
	log, err := os.ReadFile(filepath.Join(state, "friction.log"))
	if err != nil || !strings.Contains(string(log), "error(exit 1)\twait\t\n") {
		t.Fatalf("friction log=%q err=%v", log, err)
	}
}
