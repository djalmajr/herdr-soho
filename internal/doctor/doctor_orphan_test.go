package doctor

import (
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// orphanDoctorRules answers the doctor's herdr probes with a workspace that
// holds an idle orphan (review-3), the orchestrator's own pane, a live agent
// that is not a worker name, and a same-named agent in another workspace.
func orphanDoctorRules(agentsJSON string) []fakecli.Rule {
	return []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":` + agentsJSON + `}}`},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[{"pane_id":"ws:p1"},{"pane_id":"ws:p2"},{"pane_id":"ws:p3"}]}}`},
	}
}

func TestDoctorWarnsPerOrphanPane(t *testing.T) {
	// Mutation captured: dropping the orphan check or widening the name rule
	// (any live agent) leaves released panes invisible to doctor.
	agents := `[{"name":"review-3","pane_id":"ws:p2","agent_status":"idle"},{"name":"review","pane_id":"ws:p1","agent_status":"idle"},{"name":"stray","pane_id":"ws:p3","agent_status":"idle"},{"name":"review-4","pane_id":"w9:p1","agent_status":"idle"}]`
	env, root := paneContextFixture(t, orphanDoctorRules(agents))
	env["HERDR_WORKSPACE_ID"] = "ws"
	env["HERDR_PANE_ID"] = "ws:p1"
	got := doctorCheckOutput(t, env, root)
	want := "warn   orphan pane: 'review-3' (ws:p2, idle) is not in the roster; close it with: herdr-soho release review-3 --close\n"
	if !strings.Contains(got, want) {
		t.Fatalf("doctor lacks the orphan warn:\n%s", got)
	}
	// The orchestrator's own pane, a non-worker name and a foreign-workspace
	// agent are not orphans.
	for _, not := range []string{"'review' (ws:p1", "'stray'", "review-4"} {
		if strings.Contains(got, not) {
			t.Fatalf("doctor reports a non-orphan (%s):\n%s", not, got)
		}
	}
}

func TestDoctorHasNoOrphanLineWithoutOrphans(t *testing.T) {
	agents := `[{"name":"review","pane_id":"ws:p1","agent_status":"idle"},{"name":"stray","pane_id":"ws:p3","agent_status":"idle"}]`
	env, root := paneContextFixture(t, orphanDoctorRules(agents))
	env["HERDR_WORKSPACE_ID"] = "ws"
	env["HERDR_PANE_ID"] = "ws:p1"
	got := doctorCheckOutput(t, env, root)
	if strings.Contains(got, "orphan pane:") {
		t.Fatalf("doctor reported an orphan with none:\n%s", got)
	}
}

func TestDoctorOrphanLineNeedsAHerdrContext(t *testing.T) {
	// Outside a Herdr pane there is no live-agent context: no orphan line and
	// no herdr agent list call (the doctor only warns about the context).
	agents := `[{"name":"review-3","pane_id":"ws:p2","agent_status":"idle"}]`
	env, root := paneContextFixture(t, orphanDoctorRules(agents))
	env["HERDR_ENV"] = ""
	delete(env, "HERDR_PANE_ID")
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
	got := doctorCheckOutput(t, env, root)
	if strings.Contains(got, "orphan pane:") {
		t.Fatalf("doctor reported an orphan from a failed list:\n%s", got)
	}
	if !strings.Contains(got, "first_run:") {
		t.Fatalf("the doctor stopped before the end:\n%s", got)
	}
}
