package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// orphanDoctorRules answers the doctor's herdr probes with a workspace that
// holds a released pane's agent (review-3), the orchestrator's own pane, a
// live agent with no released row, and a same-named agent in another
// workspace.
func orphanDoctorRules(agentsJSON string) []fakecli.Rule {
	return []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":` + agentsJSON + `}}`},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[{"pane_id":"ws:p1"},{"pane_id":"ws:p2"},{"pane_id":"ws:p3"},{"pane_id":"ws:p4"}]}}`},
	}
}

func writeDoctorReleasedPanes(t *testing.T, root string, rows ...string) {
	t.Helper()
	state := filepath.Join(root, "state", "ws")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	released := "# name\tpane\tkind\treleased\n"
	if len(rows) > 0 {
		released += strings.Join(rows, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(state, "released-panes.tsv"), []byte(released), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorWarnsPerOrphanPane(t *testing.T) {
	// Mutation captured: dropping the orphan check or listing every live
	// agent leaves released panes invisible (or foreign panes visible) to
	// doctor. The doctor lists only what is in the released-panes registry.
	agents := `[{"name":"review-3","pane_id":"ws:p2","agent_status":"idle"},{"name":"review","pane_id":"ws:p1","agent_status":"idle"},{"name":"stray","pane_id":"ws:p3","agent_status":"idle"},{"name":"build-9","pane_id":"ws:p4","agent_status":"idle"},{"name":"review-4","pane_id":"w9:p1","agent_status":"idle"}]`
	env, root := paneContextFixture(t, orphanDoctorRules(agents))
	env["HERDR_WORKSPACE_ID"] = "ws"
	env["HERDR_PANE_ID"] = "ws:p1"
	writeDoctorReleasedPanes(t, root, "review-3\tws:p2\tgrok\t20261001T000000", "review\tws:p1\tgrok\t20261001T000000")
	got := doctorCheckOutput(t, env, root)
	want := "warn   orphan pane: 'review-3' (ws:p2, idle) is not in the roster; close it with: herdr-soho release review-3 --close\n"
	if !strings.Contains(got, want) {
		t.Fatalf("doctor lacks the orphan warn:\n%s", got)
	}
	// The orchestrator's own pane (released but never to be warned), a live
	// agent without a released row, and a foreign-workspace agent are not
	// orphans.
	for _, not := range []string{"'review' (ws:p1", "'stray'", "build-9", "review-4"} {
		if strings.Contains(got, not) {
			t.Fatalf("doctor reports a non-orphan (%s):\n%s", not, got)
		}
	}
}

func TestDoctorHasNoOrphanLineWithoutOrphans(t *testing.T) {
	// No released-panes registry: nothing is an orphan, even for live
	// lane-named agents.
	agents := `[{"name":"review-3","pane_id":"ws:p2","agent_status":"idle"},{"name":"stray","pane_id":"ws:p3","agent_status":"idle"}]`
	env, root := paneContextFixture(t, orphanDoctorRules(agents))
	env["HERDR_WORKSPACE_ID"] = "ws"
	env["HERDR_PANE_ID"] = "ws:p1"
	got := doctorCheckOutput(t, env, root)
	if strings.Contains(got, "orphan pane:") {
		t.Fatalf("doctor reported an orphan with no released rows:\n%s", got)
	}
}

func TestDoctorOrphanLineNeedsAHerdrContext(t *testing.T) {
	// Outside a Herdr pane there is no live-agent context: no orphan line and
	// no herdr agent list call (the doctor only warns about the context).
	agents := `[{"name":"review-3","pane_id":"ws:p2","agent_status":"idle"}]`
	env, root := paneContextFixture(t, orphanDoctorRules(agents))
	env["HERDR_ENV"] = ""
	delete(env, "HERDR_PANE_ID")
	writeDoctorReleasedPanes(t, root, "review-3\tws:p2\tgrok\t20261001T000000")
	got := doctorCheckOutput(t, env, root)
	if strings.Contains(got, "orphan pane:") {
		t.Fatalf("doctor reported an orphan outside Herdr:\n%s", got)
	}
}

func TestDoctorOrphanLineSurvivesAFailingAgentList(t *testing.T) {
	// A failing agent list is swallowed: the doctor keeps going, with no
	// orphan line rather than a mid-doctor death.
	rules := []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"agent", "list"}, Stderr: `{"error":{"code":"boom","message":"nope"}}`, Code: 1},
	}
	env, root := paneContextFixture(t, rules)
	env["HERDR_WORKSPACE_ID"] = "ws"
	writeDoctorReleasedPanes(t, root, "review-3\tws:p2\tgrok\t20261001T000000")
	got := doctorCheckOutput(t, env, root)
	if strings.Contains(got, "orphan pane:") {
		t.Fatalf("doctor reported an orphan from a failed list:\n%s", got)
	}
	if !strings.Contains(got, "first_run:") {
		t.Fatalf("the doctor stopped before the end:\n%s", got)
	}
}
