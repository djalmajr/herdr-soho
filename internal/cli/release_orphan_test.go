package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// newOrphanReleaseFixture builds a release fixture whose live-agent list and
// workspace pane list come from the given JSON. The roster holds only the
// in-roster worker, so any other live agent is a candidate orphan.
func newOrphanReleaseFixture(t *testing.T, agentsJSON, panesJSON, orchestratorPane string, extraRules []fakecli.Rule) *releaseFixture {
	t.Helper()
	env, cwd := commandFixture(t)
	env["HERDR_ENV"] = "1"
	env["HERDR_SOHO_REGRID"] = "off"
	state := filepath.Join(env.Get("HERDR_SOHO_DIR"), "ws")
	if err := os.MkdirAll(filepath.Join(state, "wait"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte("# header\n"+releaseRow("worker", "p-worker", "1", "implementer", false, "/tmp/work")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := append([]fakecli.Rule{
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":` + agentsJSON + `}}`},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":` + panesJSON + `}}`},
	}, extraRules...)
	fakeDir := t.TempDir()
	if _, err := fakecli.Install(t, fakeDir, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	env = withFakeCLI(env, fakeDir)
	env["HERDR_ENV"] = "1"
	if orchestratorPane != "" {
		env["HERDR_PANE_ID"] = orchestratorPane
	}
	return &releaseFixture{env: env, cwd: cwd, state: state, fakeDir: fakeDir, configFile: filepath.Join(fakeDir, "herdr.json")}
}

const (
	orphanAgents = `[{"name":"review-3","pane_id":"ws:p2","agent_status":"idle"}]`
	orphanPanes  = `[{"pane_id":"ws:p2"}]`
)

func TestReleaseCloseIdleOrphanClosesThePane(t *testing.T) {
	for _, state := range []string{"idle", "done"} {
		t.Run("state "+state, func(t *testing.T) {
			f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []fakecli.Rule{
				{Argv: []string{"agent", "get", "review-3"}, Stdout: `{"result":{"agent":{"name":"review-3","agent_status":"` + state + `"}}}`},
				{Argv: []string{"pane", "close", "ws:p2"}},
			})
			r := f.run(t, "review-3", "--close")
			want := "released review-3 (pane ws:p2 closed; it was no longer in the roster)\n"
			if r.code != 0 || r.stdout != want || r.stderr != "" || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 1 {
				t.Fatalf("release status=%d stdout=%q stderr=%q calls=%#v", r.code, r.stdout, r.stderr, r.calls)
			}
		})
	}
}

func TestReleaseCloseWorkingOrphanRefusesWithoutForce(t *testing.T) {
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []fakecli.Rule{
		{Argv: []string{"agent", "get", "review-3"}, Stdout: `{"result":{"agent":{"name":"review-3","agent_status":"working"}}}`},
		{Argv: []string{"pane", "close", "ws:p2"}},
	})
	r := f.run(t, "review-3", "--close")
	want := "herdr-soho: agent 'review-3' is not in the roster and is working; pass --force to close its pane\n"
	if r.code != 10 || r.stderr != want || r.stdout != "" || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 0 {
		t.Fatalf("release status=%d stdout=%q stderr=%q calls=%#v", r.code, r.stdout, r.stderr, r.calls)
	}
}

func TestReleaseCloseOrphanUnqueryableRefusesWithoutForce(t *testing.T) {
	// An unqueryable orphan is not confirmed idle: it is refused with its
	// state and --force offers the escape hatch, like the working case.
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []fakecli.Rule{
		{Argv: []string{"agent", "get", "review-3"}, Stderr: `{"error":{"code":"permission_denied","message":"not allowed"}}`, Code: 1},
		{Argv: []string{"pane", "close", "ws:p2"}},
	})
	r := f.run(t, "review-3", "--close")
	want := "herdr-soho: agent 'review-3' is not in the roster and is unavailable; pass --force to close its pane\n"
	if r.code != 10 || r.stderr != want || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseOrphanForceClosesWithoutStateCheck(t *testing.T) {
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []fakecli.Rule{
		{Argv: []string{"pane", "close", "ws:p2"}},
	})
	r := f.run(t, "review-3", "--close", "--force")
	want := "released review-3 (pane ws:p2 closed; it was no longer in the roster)\n"
	if r.code != 0 || r.stdout != want || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 1 || len(callsTo(r.calls, "agent", "get", "review-3")) != 0 {
		t.Fatalf("release status=%d stdout=%q stderr=%q calls=%#v", r.code, r.stdout, r.stderr, r.calls)
	}
}

func TestReleaseCloseNonLaneNameIsNotAnOrphan(t *testing.T) {
	agents := `[{"name":"iphone-advisor","pane_id":"ws:p2","agent_status":"idle"}]`
	f := newOrphanReleaseFixture(t, agents, orphanPanes, "", nil)
	r := f.run(t, "iphone-advisor", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'iphone-advisor' is not in the roster") || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseOrchestratorPaneIsNeverClosed(t *testing.T) {
	// A live agent with a lane name in the orchestrator's own pane is the
	// orchestrator, not an orphan: today's not-in-roster error, no close.
	agents := `[{"name":"review","pane_id":"ws:p0","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p0"},{"pane_id":"ws:p2"}]`
	f := newOrphanReleaseFixture(t, agents, panes, "ws:p0", []fakecli.Rule{
		{Argv: []string{"agent", "get", "review"}, Stdout: `{"result":{"agent":{"name":"review","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "close", "ws:p0"}},
	})
	r := f.run(t, "review", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review' is not in the roster") || len(callsTo(r.calls, "pane", "close", "ws:p0")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseAgentOfAnotherWorkspaceIsNotAnOrphan(t *testing.T) {
	agents := `[{"name":"review-3","pane_id":"w9:p1","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p2"}]`
	f := newOrphanReleaseFixture(t, agents, panes, "", []fakecli.Rule{
		{Argv: []string{"agent", "get", "review-3"}, Stdout: `{"result":{"agent":{"name":"review-3","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "close", "w9:p1"}},
	})
	r := f.run(t, "review-3", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review-3' is not in the roster") || len(callsTo(r.calls, "pane", "close", "w9:p1")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseRosterPaneIsNotAnOrphanPane(t *testing.T) {
	// A live agent named like a lane worker but sitting in a roster pane is
	// a rostered worker, not an orphan: today's not-in-roster error, no close.
	agents := `[{"name":"review-3","pane_id":"p-worker","agent_status":"idle"}]`
	f := newOrphanReleaseFixture(t, agents, `[{"pane_id":"p-worker"}]`, "", []fakecli.Rule{
		{Argv: []string{"agent", "get", "review-3"}, Stdout: `{"result":{"agent":{"name":"review-3","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "close", "p-worker"}},
	})
	r := f.run(t, "review-3", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review-3' is not in the roster") || len(callsTo(r.calls, "pane", "close", "p-worker")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseRosteredAgentKeepsTheRosterFlow(t *testing.T) {
	// The roster worker is in the roster: its release follows the roster
	// flow (pane closed, title cleared, row removed), not the orphan flow.
	agents := `[{"name":"worker","pane_id":"p-worker","agent_status":"idle"}]`
	f := newOrphanReleaseFixture(t, agents, `[{"pane_id":"p-worker"}]`, "", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"name":"worker","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "close", "p-worker"}},
		{Argv: []string{"pane", "report-metadata", "p-worker", "--source", "herdr-soho", "--clear-title"}},
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"old"}}}`},
		{Argv: []string{"tab", "rename", "t1", "herd"}},
	})
	addReleaseReport(t, f.state, "worker", false)
	r := f.run(t, "worker", "--close")
	if r.code != 0 || r.stdout != "closed pane p-worker\nreleased worker\n" || strings.Contains(r.stderr, "stays open") || len(callsTo(r.calls, "pane", "close", "p-worker")) != 1 || core.RosterLine(f.state, "worker") != "" {
		t.Fatalf("release status=%d stdout=%q stderr=%q roster=%q", r.code, r.stdout, r.stderr, core.RosterLine(f.state, "worker"))
	}
}

func TestReleaseWithoutCloseNamesTheOrphanFollowUp(t *testing.T) {
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"name":"worker","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "report-metadata", "p-worker", "--source", "herdr-soho", "--clear-title"}},
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"old"}}}`},
		{Argv: []string{"tab", "rename", "t1", "herd"}},
	})
	addReleaseReport(t, f.state, "worker", false)
	r := f.run(t, "worker")
	want := "herdr-soho: release: the pane p-worker stays open; close it later with: herdr-soho release worker --close\n"
	if r.code != 0 || r.stderr != want || strings.Contains(r.stdout, "stays open") {
		t.Fatalf("release status=%d stdout=%q stderr=%q calls=%#v", r.code, r.stdout, r.stderr, r.calls)
	}
	// The hint is not an error: no friction entry is recorded.
	if _, err := os.Stat(filepath.Join(f.state, "friction.log")); !os.IsNotExist(err) {
		t.Fatalf("the hint wrote friction: %v", err)
	}
}

func TestReleaseCloseWithoutOrphanKeepsPlainMessage(t *testing.T) {
	// No live agent with the name: the plain not-in-roster error, exit 3.
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", nil)
	r := f.run(t, "review-5", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review-5' is not in the roster") || len(r.calls) != 2 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseOrphanWithoutCloseIsNotAnOrphanRelease(t *testing.T) {
	// Only --close takes the orphan path; the plain release of a name that
	// left the roster keeps today's exit 3.
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []fakecli.Rule{
		{Argv: []string{"pane", "close", "ws:p2"}},
	})
	r := f.run(t, "review-3")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review-3' is not in the roster") || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}
