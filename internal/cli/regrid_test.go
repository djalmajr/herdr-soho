package cli

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

func regridCommandFixture(t *testing.T, rules []fakecli.Rule) (platform.Env, string, string) {
	t.Helper()
	env, cwd := commandFixture(t)
	env["HERDR_ENV"], env["HERDR_SOHO_REGRID"], env["HERDR_WORKSPACE_ID"] = "1", "on", "ws"
	state := filepath.Join(env.Get("HERDR_SOHO_DIR"), "ws")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	return withFakeCLI(env, bin), cwd, state
}

func TestRegridEntryFailureRecordsFriction(t *testing.T) {
	t.Run(`cmdRegrid: the entry exits 4 with the message and a friction entry`, func(t *testing.T) { // JS: "cmdRegrid: the entry exits 4 with the message and a friction entry"
		// Mutation captured: dropping DieError friction recording hides a failed move from the command's observable recovery log.
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 1, Stdout: `{"result":{"panes":[{"pane_id":"C","tab_id":"t0"},{"pane_id":"p1","tab_id":"t0"}]}}`},
			{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 2, Stdout: `{"result":{"panes":[{"pane_id":"C","tab_id":"t0"},{"pane_id":"p1","tab_id":"t0"}]}}`},
			{Argv: []string{"pane", "move", "p1", "--new-tab", "--label", "herd-park", "--no-focus"}, Code: 1},
		}
		env, cwd, state := regridCommandFixture(t, rules)
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t0", "C"
		if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte("worker\tp1\tcursor\timplementer\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		oldOut, oldErr := platform.Stdout, platform.Stderr
		var out, stderr bytes.Buffer
		platform.Stdout, platform.Stderr = &out, &stderr
		code := Run([]string{"regrid"}, env)
		platform.Stdout, platform.Stderr = oldOut, oldErr
		friction, err := os.ReadFile(filepath.Join(state, "friction.log"))
		if err != nil || code != 4 || out.Len() != 0 || !strings.Contains(stderr.String(), "regrid: could not park the workers of tab t0 in a temporary tab") || !strings.Contains(string(friction), "error(exit 4)\tregrid\tregrid: could not park the workers of tab t0 in a temporary tab") {
			t.Fatalf("code=%d stdout=%q stderr=%q friction=%q err=%v cwd=%q", code, out.String(), stderr.String(), friction, err, cwd)
		}
	})
}

func TestReleaseAutomaticRegridLifecycle(t *testing.T) {
	t.Run(`release --close: the automatic regrid rebuilds the herd tab silently`, func(t *testing.T) { // JS: "release --close: the automatic regrid rebuilds the herd tab silently"
		// Mutation captured: bypassing automatic regrid after close leaves the remaining worker tab unreconstructed.
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "close", "pa"}},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, ArgvPrefix: true, Stdout: `{"result":{"panes":[{"pane_id":"q2","tab_id":"t1"},{"pane_id":"q3","tab_id":"t1"}]}}`},
			{Argv: []string{"pane", "list"}, ArgvPrefix: true, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"tab", "get"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"agent", "get"}, ArgvPrefix: true, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"tab", "create"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"tab_id":"t-new"},"root_pane":{"pane_id":"r-new"}}}`},
			{Argv: []string{"pane", "move"}, ArgvPrefix: true, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"q2"}}}}`},
			{Argv: []string{"pane", "close", "r-new"}},
		}
		env, _ := commandFixture(t)
		env["HERDR_ENV"], env["HERDR_SOHO_REGRID"], env["HERDR_WORKSPACE_ID"] = "1", "on", "ws"
		state := filepath.Join(env.Get("HERDR_SOHO_DIR"), "ws")
		if err := os.MkdirAll(filepath.Join(state, "reports"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte("worker\tpa\tcursor\timplementer\tprovider\t1\t/tmp\tstarted\nother1\tq2\tcursor\tscouter\tprovider\t1\t/tmp\tstarted\nother2\tq3\tcursor\tscouter\tprovider\t1\t/tmp\tstarted\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, "herd-tab"), []byte("t1\therd\tauto\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		report := filepath.Join(state, "reports", "worker.md")
		if err := os.WriteFile(report, []byte("done\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(core.LastReportPath(state, "worker"), []byte(report+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		bin := t.TempDir()
		if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
			t.Fatal(err)
		}
		env = withFakeCLI(env, bin)
		oldOut, oldErr := platform.Stdout, platform.Stderr
		var out, stderr bytes.Buffer
		platform.Stdout, platform.Stderr = &out, &stderr
		code := Run([]string{"release", "worker", "--close"}, env)
		platform.Stdout, platform.Stderr = oldOut, oldErr
		calls, err := fakecli.ReadCallsForConfig(filepath.Join(env["HERDR_SOHO_FAKECLI_CONFIG"], "herdr.json"))
		if err != nil || code != 0 || stderr.Len() != 0 || out.String() != "closed pane pa\nreleased worker\n" || !hasCallPrefix(calls, "tab", "create") {
			t.Fatalf("code=%d out=%q stderr=%q calls=%#v err=%v", code, out.String(), stderr.String(), calls, err)
		}
	})
	t.Run(`release --close: a failed automatic regrid is the bash warning and keeps the release code`, func(t *testing.T) { // JS: "release --close: a failed automatic regrid is the bash warning and keeps the release code"
		// Mutation captured: propagating the regrid DieError changes the successful release status and skips the warning contract.
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "close", "pa"}},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, ArgvPrefix: true, Stdout: `{"result":{"panes":[{"pane_id":"q2","tab_id":"t1"},{"pane_id":"q3","tab_id":"t1"}]}}`},
			{Argv: []string{"pane", "list"}, ArgvPrefix: true, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"tab", "get"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"agent", "get"}, ArgvPrefix: true, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"tab", "create"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"tab_id":"t-new"},"root_pane":{"pane_id":"r-new"}}}`},
			{Argv: []string{"pane", "move"}, ArgvPrefix: true, Code: 1},
		}
		env, _ := commandFixture(t)
		env["HERDR_ENV"], env["HERDR_SOHO_REGRID"], env["HERDR_WORKSPACE_ID"] = "1", "on", "ws"
		state := filepath.Join(env.Get("HERDR_SOHO_DIR"), "ws")
		if err := os.MkdirAll(filepath.Join(state, "reports"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte("worker\tpa\tcursor\timplementer\tprovider\t1\t/tmp\tstarted\nother1\tq2\tcursor\tscouter\tprovider\t1\t/tmp\tstarted\nother2\tq3\tcursor\tscouter\tprovider\t1\t/tmp\tstarted\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, "herd-tab"), []byte("t1\therd\tauto\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		report := filepath.Join(state, "reports", "worker.md")
		if err := os.WriteFile(report, []byte("done\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(core.LastReportPath(state, "worker"), []byte(report+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		bin := t.TempDir()
		if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
			t.Fatal(err)
		}
		env = withFakeCLI(env, bin)
		oldOut, oldErr := platform.Stdout, platform.Stderr
		var out, stderr bytes.Buffer
		platform.Stdout, platform.Stderr = &out, &stderr
		code := Run([]string{"release", "worker", "--close"}, env)
		platform.Stdout, platform.Stderr = oldOut, oldErr
		friction, err := os.ReadFile(filepath.Join(state, "friction.log"))
		if err != nil || code != 0 || out.String() != "closed pane pa\nreleased worker\n" || !strings.Contains(stderr.String(), "regrid after release failed; panes left as they are (see friction)") || !strings.Contains(string(friction), "error(exit 4)\trelease\t") {
			t.Fatalf("code=%d out=%q stderr=%q friction=%q err=%v", code, out.String(), stderr.String(), friction, err)
		}
	})
	t.Run(`release without --close: the relabel branch is untouched by the regrid`, func(t *testing.T) { // JS: "release without --close: the relabel branch is untouched by the regrid"
		// Mutation captured: triggering automatic regrid without --close moves panes after a release that should only clear its title.
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "report-metadata", "pa", "--source", "herdr-soho", "--clear-title"}},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, ArgvPrefix: true, Stdout: `{"result":{"panes":[]}}`},
		}
		env, _ := commandFixture(t)
		env["HERDR_ENV"], env["HERDR_SOHO_REGRID"], env["HERDR_WORKSPACE_ID"] = "1", "on", "ws"
		state := filepath.Join(env.Get("HERDR_SOHO_DIR"), "ws")
		if err := os.MkdirAll(filepath.Join(state, "reports"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte("worker\tpa\tcursor\timplementer\tprovider\t1\t/tmp\tstarted\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		report := filepath.Join(state, "reports", "worker.md")
		if err := os.WriteFile(report, []byte("done\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(core.LastReportPath(state, "worker"), []byte(report+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		bin := t.TempDir()
		if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
			t.Fatal(err)
		}
		env = withFakeCLI(env, bin)
		oldOut, oldErr := platform.Stdout, platform.Stderr
		var out, stderr bytes.Buffer
		platform.Stdout, platform.Stderr = &out, &stderr
		code := Run([]string{"release", "worker"}, env)
		platform.Stdout, platform.Stderr = oldOut, oldErr
		calls, err := fakecli.ReadCallsForConfig(filepath.Join(env["HERDR_SOHO_FAKECLI_CONFIG"], "herdr.json"))
		// The release without --close now names the pane that stays open (the
		// orphan follow-up of the release slice); the relabel branch must still
		// not move panes.
		wantErr := "herdr-soho: release: the pane pa stays open; close it later with: herdr-soho release worker --close\n"
		if err != nil || code != 0 || out.String() != "released worker\n" || stderr.String() != wantErr || hasCallPrefix(calls, "pane", "move") || len(callsTo(calls, "pane", "report-metadata", "pa", "--source", "herdr-soho", "--clear-title")) != 1 {
			t.Fatalf("code=%d out=%q stderr=%q calls=%#v err=%v", code, out.String(), stderr.String(), calls, err)
		}
	})
}

func hasCallPrefix(calls []fakecli.Call, prefix ...string) bool {
	for _, call := range calls {
		if len(call.Argv) >= len(prefix) && strings.Join(call.Argv[:len(prefix)], "\x00") == strings.Join(prefix, "\x00") {
			return true
		}
	}
	return false
}
