package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestDoctorRoutesThroughGoAndReportsFirstRun(t *testing.T) { // JS: "doctor first-run detection (test-friendly.sh)"
	env, repo := commandFixture(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	if code := Run([]string{"doctor"}, env); code != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), "first_run: true\n") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
	settings := filepath.Join(repo, ".claude", "settings.json")
	if !strings.Contains(out.String(), "no herdr-soho hooks in .claude/settings.json") {
		t.Fatalf("missing hook diagnosis: %s", out.String())
	}
	_ = settings
}

func TestInitRenamesTakenOrchestratorNameThroughFakeHerdr(t *testing.T) { // JS: "init: when orchestrator is live the rename takes the unique suffix"
	env, repo := commandFixture(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	fakeDir := t.TempDir()
	rules := []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"agent", "get", "p1"}, Stdout: `{"result":{"agent":{"name":"reviewer"}}}`},
		{Argv: []string{"agent", "get", "p1"}, Stdout: `{"result":{"agent":{"name":"reviewer"}}}`},
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[{"name":"orchestrator"}]}}`},
		{Argv: []string{"agent", "rename", "p1", "orchestrator-2"}},
		{Argv: []string{"pane", "get", "p1"}, Stdout: `{"result":{"pane":{"title":"current task","workspace_id":"ws"}}}`},
	}
	if _, err = fakecli.Install(t, fakeDir, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	env = withFakeCLI(env, fakeDir)
	env["HERDR_ENV"] = "1"
	env["HERDR_PANE_ID"] = "p1"
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	if code := Run([]string{"init"}, env); code != 0 || !strings.Contains(out.String(), `"orchestrator": "orchestrator-2"`) || !strings.Contains(out.String(), `"title": "current task"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
}
