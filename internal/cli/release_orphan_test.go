package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// newOrphanReleaseFixture builds a release fixture whose live-agent list,
// workspace pane list and released-panes registry come from the given JSON
// and rows. The roster holds only the in-roster worker.
func newOrphanReleaseFixture(t *testing.T, agentsJSON, panesJSON, orchestratorPane string, releasedRows []string, extraRules []fakecli.Rule) *releaseFixture {
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
	released := "# name\tpane\tkind\treleased\n"
	if len(releasedRows) > 0 {
		released += strings.Join(releasedRows, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(state, "released-panes.tsv"), []byte(released), 0o600); err != nil {
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

// releasedPanesRows reads the fixture registry rows of one name (test helper;
// core owns the production reader).
func releasedPanesRows(t *testing.T, state, name string) []core.ReleasedPane {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(state, "released-panes.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := []core.ReleasedPane{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) >= 4 && f[0] == name {
			rows = append(rows, core.ReleasedPane{Name: f[0], Pane: f[1], Kind: f[2], Released: f[3]})
		}
	}
	return rows
}

func releasedRow(name, pane string) string {
	return name + "\t" + pane + "\tgrok\t20261001T000000"
}

func TestReleaseCloseReleasedOrphanClosesAndForgets(t *testing.T) {
	// A pane this project released (a registry row) and still idle is an
	// orphan: --close closes it and the row leaves the registry.
	for _, state := range []string{"idle", "done"} {
		t.Run("state "+state, func(t *testing.T) {
			f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []string{releasedRow("review-3", "ws:p2")}, []fakecli.Rule{
				{Argv: []string{"agent", "get", "review-3"}, Stdout: `{"result":{"agent":{"name":"review-3","agent_status":"` + state + `"}}}`},
				{Argv: []string{"pane", "close", "ws:p2"}},
				{Argv: []string{"pane", "get", "ws:p2"}, Stdout: `{"result":{"pane":{"pane_id":"ws:p2"}}}`},
			})
			r := f.run(t, "review-3", "--close")
			want := "released review-3 (pane ws:p2 closed; it was no longer in the roster)\n"
			if r.code != 0 || r.stdout != want || r.stderr != "" || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 1 {
				t.Fatalf("release status=%d stdout=%q stderr=%q calls=%#v", r.code, r.stdout, r.stderr, r.calls)
			}
			if row := releasedPanesRows(t, f.state, "review-3"); len(row) != 0 {
				t.Fatalf("the registry row survived the close: %#v", row)
			}
		})
	}
}

func TestReleaseCloseWorkingOrphanRefusesWithoutForce(t *testing.T) {
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []string{releasedRow("review-3", "ws:p2")}, []fakecli.Rule{
		{Argv: []string{"agent", "get", "review-3"}, Stdout: `{"result":{"agent":{"name":"review-3","agent_status":"working"}}}`},
		{Argv: []string{"pane", "close", "ws:p2"}},
	})
	r := f.run(t, "review-3", "--close")
	want := "herdr-soho: agent 'review-3' is not in the roster and is working; pass --force to close its pane\n"
	if r.code != 10 || r.stderr != want || r.stdout != "" || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseOrphanUnqueryableRefusesWithoutForce(t *testing.T) {
	// An unqueryable orphan is not confirmed idle: it is refused with its
	// state and --force offers the escape hatch, like the working case.
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []string{releasedRow("review-3", "ws:p2")}, []fakecli.Rule{
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
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []string{releasedRow("review-3", "ws:p2")}, []fakecli.Rule{
		{Argv: []string{"pane", "close", "ws:p2"}},
		{Argv: []string{"pane", "get", "ws:p2"}, Stdout: `{"result":{"pane":{"pane_id":"ws:p2"}}}`},
	})
	r := f.run(t, "review-3", "--close", "--force")
	want := "released review-3 (pane ws:p2 closed; it was no longer in the roster)\n"
	if r.code != 0 || r.stdout != want || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 1 || len(callsTo(r.calls, "agent", "get", "review-3")) != 0 || len(releasedPanesRows(t, f.state, "review-3")) != 0 {
		t.Fatalf("release status=%d stdout=%q stderr=%q calls=%#v", r.code, r.stdout, r.stderr, r.calls)
	}
}

func TestReleaseCloseOrphanFailedCloseExits4AndKeepsTheRow(t *testing.T) {
	// A failed pane close must not read as a release: exit 4 names the manual
	// command, and the registry row stays (the pane is still open).
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []string{releasedRow("review-3", "ws:p2")}, []fakecli.Rule{
		{Argv: []string{"agent", "get", "review-3"}, Stdout: `{"result":{"agent":{"name":"review-3","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "close", "ws:p2"}, Stderr: `{"error":{"code":"boom","message":"refused"}}`, Code: 1},
		{Argv: []string{"pane", "get", "ws:p2"}, Stdout: `{"result":{"pane":{"pane_id":"ws:p2"}}}`},
	})
	r := f.run(t, "review-3", "--close")
	want := "herdr-soho: release: could not close the pane ws:p2 of 'review-3'; close it by hand with: herdr pane close ws:p2\n"
	if r.code != 4 || r.stderr != want || strings.Contains(r.stdout, "released") || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 1 {
		t.Fatalf("release status=%d stdout=%q stderr=%q calls=%#v", r.code, r.stdout, r.stderr, r.calls)
	}
	if row := releasedPanesRows(t, f.state, "review-3"); len(row) != 1 {
		t.Fatalf("the registry row did not stay after the failed close: %#v", row)
	}
}

func TestReleaseCloseOtherProjectPaneIsNotAnOrphan(t *testing.T) {
	// Reviewer case: another project in the same workspace holds a live
	// review-3 in pane ws:other. It is not a pane this project released, so
	// the release is today's not-in-roster error and nothing is closed.
	agents := `[{"name":"review-3","pane_id":"ws:other","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:other"}]`
	f := newOrphanReleaseFixture(t, agents, panes, "", nil, []fakecli.Rule{
		{Argv: []string{"agent", "get", "review-3"}, Stdout: `{"result":{"agent":{"name":"review-3","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "close", "ws:other"}},
	})
	r := f.run(t, "review-3", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review-3' is not in the roster") || len(callsTo(r.calls, "pane", "close", "ws:other")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseSubOrchestratorWorkerIsNotAnOrphan(t *testing.T) {
	// Reviewer case: a sub-orchestrator's review-2 is a live agent of the
	// workspace, but not a pane this project released.
	agents := `[{"name":"review-2","pane_id":"ws:s2","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:s2"}]`
	f := newOrphanReleaseFixture(t, agents, panes, "", nil, []fakecli.Rule{
		{Argv: []string{"agent", "get", "review-2"}, Stdout: `{"result":{"agent":{"name":"review-2","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "close", "ws:s2"}},
	})
	r := f.run(t, "review-2", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review-2' is not in the roster") || len(callsTo(r.calls, "pane", "close", "ws:s2")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseSkillRoleNameIsNotAnOrphan(t *testing.T) {
	// Reviewer case with lanes=off: sub-orchestrator is a skill role name and
	// was the round-1 false positive. The registry decides, and this project
	// never released that pane.
	agents := `[{"name":"sub-orchestrator","pane_id":"ws:p3","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p3"}]`
	f := newOrphanReleaseFixture(t, agents, panes, "", nil, []fakecli.Rule{
		{Argv: []string{"agent", "get", "sub-orchestrator"}, Stdout: `{"result":{"agent":{"name":"sub-orchestrator","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "close", "ws:p3"}},
	})
	f.env["HERDR_SOHO_LANES"] = "off"
	r := f.run(t, "sub-orchestrator", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'sub-orchestrator' is not in the roster") || len(callsTo(r.calls, "pane", "close", "ws:p3")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseNonLaneNameIsNotAnOrphan(t *testing.T) {
	agents := `[{"name":"iphone-advisor","pane_id":"ws:p2","agent_status":"idle"}]`
	f := newOrphanReleaseFixture(t, agents, orphanPanes, "", nil, nil)
	r := f.run(t, "iphone-advisor", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'iphone-advisor' is not in the roster") || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseSameNameOtherPaneIsNotAnOrphan(t *testing.T) {
	// The registry row is for ws:p2; the live review-3 sits in ws:p9. The
	// pane and the name must both match, so this agent is not an orphan.
	agents := `[{"name":"review-3","pane_id":"ws:p9","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p2"},{"pane_id":"ws:p9"}]`
	f := newOrphanReleaseFixture(t, agents, panes, "", []string{releasedRow("review-3", "ws:p2")}, []fakecli.Rule{
		{Argv: []string{"agent", "get", "review-3"}, Stdout: `{"result":{"agent":{"name":"review-3","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "close", "ws:p9"}},
	})
	r := f.run(t, "review-3", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review-3' is not in the roster") || len(callsTo(r.calls, "pane", "close", "ws:p9")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseOrchestratorPaneIsNeverClosed(t *testing.T) {
	// Even released by this project, the orchestrator's own pane is never
	// closed: today's not-in-roster error.
	agents := `[{"name":"review","pane_id":"ws:p0","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p0"},{"pane_id":"ws:p2"}]`
	f := newOrphanReleaseFixture(t, agents, panes, "ws:p0", []string{releasedRow("review", "ws:p0")}, []fakecli.Rule{
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
	f := newOrphanReleaseFixture(t, agents, panes, "", []string{releasedRow("review-3", "w9:p1")}, []fakecli.Rule{
		{Argv: []string{"agent", "get", "review-3"}, Stdout: `{"result":{"agent":{"name":"review-3","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "close", "w9:p1"}},
	})
	r := f.run(t, "review-3", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review-3' is not in the roster") || len(callsTo(r.calls, "pane", "close", "w9:p1")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseCloseRosterPaneIsNotAnOrphanPane(t *testing.T) {
	// Released earlier and since re-rostered in a different agent: the roster
	// pane wins, and the release of the name is the plain error.
	agents := `[{"name":"review-3","pane_id":"p-worker","agent_status":"idle"}]`
	f := newOrphanReleaseFixture(t, agents, `[{"pane_id":"p-worker"}]`, "", []string{releasedRow("review-3", "p-worker")}, []fakecli.Rule{
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
	// flow (pane closed, title cleared, row removed), not the orphan flow,
	// and it does not touch the registry.
	agents := `[{"name":"worker","pane_id":"p-worker","agent_status":"idle"}]`
	f := newOrphanReleaseFixture(t, agents, `[{"pane_id":"p-worker"}]`, "", nil, []fakecli.Rule{
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
	if row := releasedPanesRows(t, f.state, "worker"); len(row) != 0 {
		t.Fatalf("the roster release wrote a registry row: %#v", row)
	}
}

func TestReleaseWithoutCloseRecordsTheReleasedPane(t *testing.T) {
	// The release without --close registers the pane in released-panes.tsv
	// (name, pane, kind, time) and keeps the stderr hint.
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", nil, []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"name":"worker","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "report-metadata", "p-worker", "--source", "herdr-soho", "--clear-title"}},
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"old"}}}`},
		{Argv: []string{"tab", "rename", "t1", "herd"}},
	})
	oldNow := platform.Now
	platform.Now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { platform.Now = oldNow })
	addReleaseReport(t, f.state, "worker", false)
	r := f.run(t, "worker")
	want := "herdr-soho: release: the pane p-worker stays open; close it later with: herdr-soho release worker --close\n"
	if r.code != 0 || r.stderr != want || strings.Contains(r.stdout, "stays open") {
		t.Fatalf("release status=%d stdout=%q stderr=%q calls=%#v", r.code, r.stdout, r.stderr, r.calls)
	}
	rows := releasedPanesRows(t, f.state, "worker")
	if len(rows) != 1 || rows[0].Pane != "p-worker" || rows[0].Kind != "grok" || rows[0].Released != "20261001T120000" {
		t.Fatalf("the registry row was not recorded: %#v", rows)
	}
	// The hint is not an error: no friction entry is recorded.
	if _, err := os.Stat(filepath.Join(f.state, "friction.log")); !os.IsNotExist(err) {
		t.Fatalf("the hint wrote friction: %v", err)
	}
}

func TestReleaseWithoutCloseNeverRecordsAGivenPane(t *testing.T) {
	// A pane the skill did not create (spawn --pane, roster created=0) is
	// never registered, so a later release --close cannot close it as an
	// orphan: the skill never closes a pane it did not create.
	// The released agent stays live in its own pane of this workspace, the
	// case where a registry row would make it an orphan.
	f := newOrphanReleaseFixture(t, `[{"name":"worker","pane_id":"p-worker","agent_status":"idle"}]`, `[{"pane_id":"p-worker"}]`, "", nil, []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"name":"worker","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "report-metadata", "p-worker", "--source", "herdr-soho", "--clear-title"}},
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"old"}}}`},
		{Argv: []string{"tab", "rename", "t1", "herd"}},
		{Argv: []string{"pane", "close", "p-worker"}},
	})
	if err := os.WriteFile(filepath.Join(f.state, "agents.tsv"), []byte("# header\n"+releaseRow("worker", "p-worker", "0", "implementer", false, "/tmp/work")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	addReleaseReport(t, f.state, "worker", false)
	r := f.run(t, "worker")
	if r.code != 0 || strings.Contains(r.stderr, "stays open") {
		t.Fatalf("release status=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
	if rows := releasedPanesRows(t, f.state, "worker"); len(rows) != 0 {
		t.Fatalf("a given pane was registered: %#v", rows)
	}
	again := f.run(t, "worker", "--close")
	if again.code != 3 {
		t.Fatalf("release --close of a given pane: status=%d stdout=%q stderr=%q", again.code, again.stdout, again.stderr)
	}
	for _, call := range again.calls {
		if len(call.Argv) >= 2 && call.Argv[0] == "pane" && call.Argv[1] == "close" {
			t.Fatalf("a given pane was closed: %#v", again.calls)
		}
	}
}

func TestReleaseWithoutClosePrunesGoneRows(t *testing.T) {
	// The registry write prunes rows whose pane is gone from Herdr.
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []string{releasedRow("old", "ws:pGone")}, []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"name":"worker","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "report-metadata", "p-worker", "--source", "herdr-soho", "--clear-title"}},
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"old"}}}`},
		{Argv: []string{"tab", "rename", "t1", "herd"}},
		{Argv: []string{"pane", "get", "ws:pGone"}, Stderr: `{"error":{"code":"pane_not_found","message":"gone"}}`, Code: 1},
	})
	addReleaseReport(t, f.state, "worker", false)
	r := f.run(t, "worker")
	if r.code != 0 {
		t.Fatalf("release status=%d stderr=%q", r.code, r.stderr)
	}
	if row := releasedPanesRows(t, f.state, "old"); len(row) != 0 {
		t.Fatalf("the gone row was not pruned: %#v", row)
	}
	if row := releasedPanesRows(t, f.state, "worker"); len(row) != 1 || row[0].Pane != "p-worker" {
		t.Fatalf("the new row was not recorded: %#v", row)
	}
}

func TestReleaseWithoutCloseRefusedInsideSkill(t *testing.T) {
	// Like the other state files, the registry refuses to be written inside
	// the skill dir; the release dies with the refusal and nothing is written.
	env, cwd := commandFixture(t)
	env["HERDR_ENV"] = "1"
	env["HERDR_SOHO_REGRID"] = "off"
	env["HERDR_SOHO_DIR"] = env.Get("HERDR_SOHO_SKILL_DIR")
	fakeDir := t.TempDir()
	if _, err := fakecli.Install(t, fakeDir, "herdr", []fakecli.Rule{{AnyArgs: true}}); err != nil {
		t.Fatal(err)
	}
	env = withFakeCLI(env, fakeDir)
	state := filepath.Join(env.Get("HERDR_SOHO_DIR"), "ws")
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr strings.Builder
	platform.Stdout, platform.Stderr = &out, &stderr
	code := Run([]string{"release", "worker"}, env)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	if code != 2 || !strings.Contains(stderr.String(), "would be inside the herdr-soho skill") {
		t.Fatalf("release status=%d stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(state, "released-panes.tsv")); !os.IsNotExist(err) {
		t.Fatalf("the refusal wrote a registry file: %v", err)
	}
	_ = cwd
}

func TestReleaseCloseWithoutOrphanKeepsPlainMessage(t *testing.T) {
	// No registry row and no live match: the plain not-in-roster error, exit 3.
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", nil, nil)
	r := f.run(t, "review-5", "--close")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review-5' is not in the roster") || len(r.calls) != 2 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}

func TestReleaseOrphanWithoutCloseIsNotAnOrphanRelease(t *testing.T) {
	// Only --close takes the orphan path; the plain release of a name that
	// left the roster keeps today's exit 3.
	f := newOrphanReleaseFixture(t, orphanAgents, orphanPanes, "", []string{releasedRow("review-3", "ws:p2")}, []fakecli.Rule{
		{Argv: []string{"pane", "close", "ws:p2"}},
	})
	r := f.run(t, "review-3")
	if r.code != 3 || !strings.Contains(r.stderr, "agent 'review-3' is not in the roster") || len(callsTo(r.calls, "pane", "close", "ws:p2")) != 0 {
		t.Fatalf("release status=%d stderr=%q calls=%#v", r.code, r.stderr, r.calls)
	}
}
