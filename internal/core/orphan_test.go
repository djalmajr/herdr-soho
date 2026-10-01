package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// orphanFixture builds a hermetic env: a state dir with the given roster rows,
// a fake herdr answering the agent list and the workspace pane list, and the
// orchestrator pane from env (when set).
func orphanFixture(t *testing.T, lanesOff bool, rosterRows []string, agentsJSON string, panesJSON string, orchestratorPane string) (platform.Env, *Config, string) {
	t.Helper()
	root := t.TempDir()
	cwd := filepath.Join(root, "repo")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{}
	for _, entry := range testutil.CleanEnv(t) {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	env["HERDR_SOHO_SKILL_DIR"] = testSkillDir(t)
	env["HERDR_WORKSPACE_ID"] = "ws"
	env["HERDR_SOHO_DIR"] = filepath.Join(root, "state")
	env["HOME"] = filepath.Join(root, "home")
	env["USERPROFILE"] = env.Get("HOME")
	env["XDG_CONFIG_HOME"] = filepath.Join(root, "config")
	roles := filepath.Join(root, "roles")
	if err := os.MkdirAll(roles, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"implementer", "reviewer"} {
		if err := os.WriteFile(filepath.Join(roles, role+".md"), []byte("---\nkind: grok\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env["HERDR_SOHO_ROLES"] = roles
	if lanesOff {
		env["HERDR_SOHO_LANES"] = "off"
	}
	if orchestratorPane != "" {
		env["HERDR_PANE_ID"] = orchestratorPane
	}
	bin := t.TempDir()
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":` + agentsJSON + `}}`},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":` + panesJSON + `}}`},
	}
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	env["PATH"] = bin
	env["HERDR_SOHO_FAKECLI_CONFIG"] = bin
	ctx := LoadConfig(env, cwd)
	state := StateDir(&ctx, env, cwd)
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n"
	if len(rosterRows) > 0 {
		roster += strings.Join(rosterRows, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	return env, &ctx, cwd
}

func TestWorkerNameMatches(t *testing.T) {
	for _, tc := range []struct {
		name, base string
		want       bool
	}{
		{"review", "review", true},
		{"review-2", "review", true},
		{"review-10", "review", true},
		{"reviewer", "review", false},
		{"review-x", "review", false},
		{"review-", "review", false},
		{"review-2-3", "review", false},
		{"build", "build", true},
		{"build-2x", "build", false},
		{"", "review", false},
		{"review", "", false},
		{"orch", "build", false},
		{"b", "build", false},
	} {
		if got := WorkerNameMatches(tc.name, tc.base); got != tc.want {
			t.Errorf("WorkerNameMatches(%q, %q)=%v want %v", tc.name, tc.base, got, tc.want)
		}
	}
}

func TestOrphansOfClassifiesByWorkspaceRosterPaneAndName(t *testing.T) {
	agents := `[{"name":"review-3","pane_id":"ws:p2","agent_status":"idle"},{"name":"review-4","pane_id":"ws:p3","agent_status":"working"},{"name":"build","pane_id":"ws:p4","agent_status":"done"},{"name":"stray","pane_id":"ws:p5","agent_status":"idle"},{"name":"other-ws-review","pane_id":"w9:p1","agent_status":"idle"},{"name":"rostered","pane_id":"ws:p1","agent_status":"idle"},{"name":"orch","pane_id":"ws:p0","agent_status":"idle"},{"name":"no-pane","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p0"},{"pane_id":"ws:p1"},{"pane_id":"ws:p2"},{"pane_id":"ws:p3"},{"pane_id":"ws:p4"},{"pane_id":"ws:p5"}]`
	env, ctx, cwd := orphanFixture(t, false, []string{"rostered\tws:p1\tgrok\treviewer\txai\t1\t/repo\tnow\tm\tfull\treviewer\treview"}, agents, panes, "ws:p0")
	got := Orphans(ctx, env, cwd)
	want := []Orphan{{Name: "build", Pane: "ws:p4", State: "done"}, {Name: "review-3", Pane: "ws:p2", State: "idle"}, {Name: "review-4", Pane: "ws:p3", State: "working"}}
	if len(got) != len(want) {
		t.Fatalf("orphans=%#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("orphans=%#v want %#v", got, want)
		}
	}
}

func TestOrphansOfLanesOffUsesKnownRoleNames(t *testing.T) {
	agents := `[{"name":"reviewer","pane_id":"ws:p1","agent_status":"idle"},{"name":"reviewer-2","pane_id":"ws:p2","agent_status":"done"},{"name":"implementer-2","pane_id":"ws:p3","agent_status":"working"},{"name":"iphone-advisor","pane_id":"ws:p4","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p1"},{"pane_id":"ws:p2"},{"pane_id":"ws:p3"},{"pane_id":"ws:p4"}]`
	env, ctx, cwd := orphanFixture(t, true, nil, agents, panes, "")
	got := Orphans(ctx, env, cwd)
	// The role files of the fixture are implementer and reviewer; the name
	// iphone-advisor is not a worker name of this project.
	want := []Orphan{{Name: "implementer-2", Pane: "ws:p3", State: "working"}, {Name: "reviewer", Pane: "ws:p1", State: "idle"}, {Name: "reviewer-2", Pane: "ws:p2", State: "done"}}
	if len(got) != len(want) {
		t.Fatalf("orphans=%#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("orphans=%#v want %#v", got, want)
		}
	}
}

func TestOrphansOfForeignWorkspaceIsNotAnOrphan(t *testing.T) {
	agents := `[{"name":"review","pane_id":"w9:p1","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p1"}]`
	env, ctx, cwd := orphanFixture(t, false, nil, agents, panes, "")
	if got := Orphans(ctx, env, cwd); len(got) != 0 {
		t.Fatalf("foreign workspace agent counted as orphan: %#v", got)
	}
}

func TestOrphansOfOrchestratorPaneIsNeverAnOrphan(t *testing.T) {
	agents := `[{"name":"review","pane_id":"ws:p0","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p0"},{"pane_id":"ws:p1"}]`
	env, ctx, cwd := orphanFixture(t, false, nil, agents, panes, "ws:p0")
	if got := Orphans(ctx, env, cwd); len(got) != 0 {
		t.Fatalf("the orchestrator's own pane counted as orphan: %#v", got)
	}
}

func TestFindOrphanReportsExactNameOnly(t *testing.T) {
	agents := `[{"name":"review-3","pane_id":"ws:p2","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p2"}]`
	env, ctx, cwd := orphanFixture(t, false, nil, agents, panes, "")
	orphan, ok := FindOrphan("review-3", ctx, env, cwd)
	if !ok || orphan.Pane != "ws:p2" || orphan.State != "idle" {
		t.Fatalf("FindOrphan(review-3)=%#v ok=%v", orphan, ok)
	}
	if _, ok := FindOrphan("review", ctx, env, cwd); ok {
		t.Fatal("the base name is not the orphan's name")
	}
	if _, ok := FindOrphan("review-4", ctx, env, cwd); ok {
		t.Fatal("a name without a live agent is not an orphan")
	}
}

func TestOrphansOfFailedPaneListClassifiesNothing(t *testing.T) {
	// A failing pane list (no matching rule: exit 127) reads as no workspace
	// pane, so the live agent is not an orphan rather than a false positive.
	root := t.TempDir()
	cwd := filepath.Join(root, "repo")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{}
	for _, entry := range testutil.CleanEnv(t) {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	env["HERDR_SOHO_SKILL_DIR"] = testSkillDir(t)
	env["HERDR_WORKSPACE_ID"] = "ws"
	env["HERDR_SOHO_DIR"] = filepath.Join(root, "state")
	env["HOME"] = filepath.Join(root, "home")
	bin := t.TempDir()
	if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[{"name":"review","pane_id":"ws:p1","agent_status":"idle"}]}}`}}); err != nil {
		t.Fatal(err)
	}
	env["PATH"] = bin
	env["HERDR_SOHO_FAKECLI_CONFIG"] = bin
	ctx := LoadConfig(env, cwd)
	if got := Orphans(&ctx, env, cwd); len(got) != 0 {
		t.Fatalf("failed pane list classified an orphan: %#v", got)
	}
}
