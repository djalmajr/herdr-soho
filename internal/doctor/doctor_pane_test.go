package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// paneContextFixture builds a hermetic environment with a fake herdr on PATH
// and a socket path that cannot resolve a live session.
func paneContextFixture(t *testing.T, rules []fakecli.Rule) (platform.Env, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "bin")
	state := filepath.Join(root, "state")
	skill := filepath.Join(root, "skill")
	for _, dir := range []string{home, bin, state, skill} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{
		"HOME": home, "USERPROFILE": home, "PATH": bin,
		"HERDR_ENV":                 "1",
		"HERDR_SOCKET_PATH":         filepath.Join(root, "missing.sock"),
		"HERDR_SOHO_DIR":            state,
		"HERDR_SOHO_FAKECLI_CONFIG": bin,
		// The state must stay outside the skill dir: StateDir refuses a
		// state dir inside the skill (exit 2), so the fixture's skill is a
		// sibling, not the root that holds the state.
		"HERDR_SOHO_SKILL_DIR": skill,
	}
	return env, root
}

func doctorCheckOutput(t *testing.T, env platform.Env, root string) string {
	t.Helper()
	ctx := core.Config{Entries: map[string]core.ConfigEntry{}}
	var out strings.Builder
	DoctorCheck(&ctx, env, root, &out)
	return out.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func TestDoctorInheritedPaneLiveKeepsOKLine(t *testing.T) {
	// A live HERDR_PANE_ID keeps the ok line when the workspace matches or
	// HERDR_WORKSPACE_ID is unset.
	for _, tc := range []struct{ name, ws string }{
		{"workspace matches the pane", "ws"},
		{"HERDR_WORKSPACE_ID unset", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, root := paneContextFixture(t, []fakecli.Rule{
				{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
				{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
				{Argv: []string{"pane", "get", "w0test:p0a"}, Stdout: `{"result":{"pane":{"title":"orchestrator","workspace_id":"ws"}}}`},
			})
			env["HERDR_PANE_ID"] = "w0test:p0a"
			if tc.ws != "" {
				env["HERDR_WORKSPACE_ID"] = tc.ws
			}
			got := doctorCheckOutput(t, env, root)
			if !strings.HasPrefix(got, "ok     inside Herdr (HERDR_ENV=1)\n") {
				t.Fatalf("first line=%q:\n%s", firstLine(got), got)
			}
			if strings.Contains(got, "Herdr context invalid") {
				t.Fatalf("unexpected context warning:\n%s", got)
			}
		})
	}
}

func TestDoctorInheritedPaneDeadWarns(t *testing.T) {
	env, root := paneContextFixture(t, []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"pane", "get", "w0test:p0a"}, Stderr: `{"error":{"code":"pane_gone","message":"pane is not alive"}}`, Code: 1},
	})
	env["HERDR_PANE_ID"], env["HERDR_WORKSPACE_ID"] = "w0test:p0a", "ws"
	got := doctorCheckOutput(t, env, root)
	want := "warn   Herdr context invalid: HERDR_PANE_ID=w0test:p0a is not a live pane (pane_gone: pane is not alive)\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("first line=%q, want %q:\n%s", firstLine(got), want, got)
	}
	if strings.Contains(got, "ok     inside Herdr (HERDR_ENV=1)") {
		t.Fatalf("a dead pane was still reported inside Herdr:\n%s", got)
	}
}

func TestDoctorInheritedPaneOtherWorkspaceWarns(t *testing.T) {
	env, root := paneContextFixture(t, []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"pane", "get", "w0test:p0a"}, Stdout: `{"result":{"pane":{"title":"orchestrator","workspace_id":"w0test:other"}}}`},
	})
	env["HERDR_PANE_ID"], env["HERDR_WORKSPACE_ID"] = "w0test:p0a", "ws"
	got := doctorCheckOutput(t, env, root)
	want := "warn   Herdr context invalid: pane w0test:p0a belongs to workspace 'w0test:other', not HERDR_WORKSPACE_ID 'ws'\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("first line=%q, want %q:\n%s", firstLine(got), want, got)
	}
	if strings.Contains(got, "ok     inside Herdr (HERDR_ENV=1)") {
		t.Fatalf("a foreign-workspace pane was still reported inside Herdr:\n%s", got)
	}
}

func TestDoctorCurrentPaneFailureWarns(t *testing.T) {
	env, root := paneContextFixture(t, []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"pane", "current", "--current"}, Stderr: "cannot resolve current pane: server is not running", Code: 1},
	})
	got := doctorCheckOutput(t, env, root)
	want := "warn   Herdr context invalid: HERDR_PANE_ID is unset and herdr pane current failed (cannot resolve current pane: server is not running)\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("first line=%q, want %q:\n%s", firstLine(got), want, got)
	}
	if strings.Contains(got, "ok     inside Herdr (HERDR_ENV=1)") {
		t.Fatalf("a failing pane current was still reported inside Herdr:\n%s", got)
	}
}

func TestDoctorCurrentPaneLiveKeepsOKLine(t *testing.T) {
	env, root := paneContextFixture(t, []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"workspace_id":"ws"}}}`},
	})
	env["HERDR_WORKSPACE_ID"] = "ws"
	got := doctorCheckOutput(t, env, root)
	if !strings.HasPrefix(got, "ok     inside Herdr (HERDR_ENV=1)\n") {
		t.Fatalf("first line=%q:\n%s", firstLine(got), got)
	}
	if strings.Contains(got, "Herdr context invalid") {
		t.Fatalf("unexpected context warning:\n%s", got)
	}
}

func runCmdInit(t *testing.T, env platform.Env, root string) (string, string) {
	t.Helper()
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr strings.Builder
	platform.Stdout, platform.Stderr = &out, &stderr
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	CmdInit(&core.Config{Entries: map[string]core.ConfigEntry{}}, env, root)
	return out.String(), stderr.String()
}

func TestInitDeadInheritedPaneLeavesPaneIDEmpty(t *testing.T) {
	env, root := paneContextFixture(t, []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Stdout: `{"result":{"agent":{"name":"orchestrator"}}}`},
		{Argv: []string{"pane", "get", "w0test:p0a"}, Stderr: `{"error":{"code":"pane_gone","message":"pane is not alive"}}`, Code: 1},
	})
	env["HERDR_PANE_ID"], env["HERDR_WORKSPACE_ID"] = "w0test:p0a", "ws"
	out, stderr := runCmdInit(t, env, root)
	if !strings.Contains(out, `"pane_id": ""`) {
		t.Fatalf("pane_id was not left empty:\n%s", out)
	}
	warning := "init: HERDR_PANE_ID 'w0test:p0a' is not a live pane; pane title left as is and pane_id left empty"
	if !strings.Contains(stderr, warning) {
		t.Fatalf("missing init warning:\n%s", stderr)
	}
	if !strings.Contains(stderr, "warn   Herdr context invalid: HERDR_PANE_ID=w0test:p0a is not a live pane (pane_gone: pane is not alive)") {
		t.Fatalf("doctor did not flag the dead pane:\n%s", stderr)
	}
	friction, err := os.ReadFile(filepath.Join(root, "state", "ws", "friction.log"))
	if err != nil || !strings.Contains(string(friction), warning) {
		t.Fatalf("friction=%q err=%v", friction, err)
	}
}

func TestInitLiveInheritedPaneKeepsPaneID(t *testing.T) {
	env, root := paneContextFixture(t, []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Stdout: `{"result":{"agent":{"name":"orchestrator"}}}`},
		{Argv: []string{"pane", "get", "w0test:p0a"}, Stdout: `{"result":{"pane":{"title":"existing","workspace_id":"ws"}}}`},
	})
	env["HERDR_PANE_ID"], env["HERDR_WORKSPACE_ID"] = "w0test:p0a", "ws"
	out, stderr := runCmdInit(t, env, root)
	if !strings.Contains(out, `"pane_id": "w0test:p0a"`) || !strings.Contains(out, `"title": "existing"`) {
		t.Fatalf("live pane was not reported:\n%s", out)
	}
	if !strings.Contains(stderr, "ok     inside Herdr (HERDR_ENV=1)") || strings.Contains(stderr, "Herdr context invalid") {
		t.Fatalf("context line wrong:\n%s", stderr)
	}
	if strings.Contains(stderr, "not a live pane") {
		t.Fatalf("unexpected dead-pane warning:\n%s", stderr)
	}
}
