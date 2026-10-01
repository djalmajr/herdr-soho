package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// orphanFixture builds a hermetic env: a state dir with the given roster and
// released-panes rows, a fake herdr answering the agent list and the
// workspace pane list, and the orchestrator pane from env (when set).
func orphanFixture(t *testing.T, rosterRows, releasedRows []string, agentsJSON, panesJSON, orchestratorPane string) (platform.Env, *Config, string) {
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
	released := "# name\tpane\tkind\treleased\n"
	if len(releasedRows) > 0 {
		released += strings.Join(releasedRows, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(state, "released-panes.tsv"), []byte(released), 0o600); err != nil {
		t.Fatal(err)
	}
	return env, &ctx, cwd
}

func TestOrphansOfMatchesOnlyReleasedPanes(t *testing.T) {
	agents := `[{"name":"review-3","pane_id":"ws:p2","agent_status":"idle"},{"name":"review-4","pane_id":"ws:p3","agent_status":"working"},{"name":"build","pane_id":"ws:p4","agent_status":"done"},{"name":"stray","pane_id":"ws:p5","agent_status":"idle"},{"name":"other-ws-review","pane_id":"w9:p1","agent_status":"idle"},{"name":"rostered","pane_id":"ws:p1","agent_status":"idle"},{"name":"orch","pane_id":"ws:p0","agent_status":"idle"},{"name":"no-pane","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p0"},{"pane_id":"ws:p1"},{"pane_id":"ws:p2"},{"pane_id":"ws:p3"},{"pane_id":"ws:p4"},{"pane_id":"ws:p5"}]`
	released := []string{"review-3\tws:p2\tgrok\t20261001T000000", "build\tws:p4\tgrok\t20261001T000000"}
	env, ctx, cwd := orphanFixture(t, []string{"rostered\tws:p1\tgrok\treviewer\txai\t1\t/repo\tnow\tm\tfull\treviewer\treview"}, released, agents, panes, "ws:p0")
	got := Orphans(ctx, env, cwd)
	// review-4 (a lane-shaped name) is not an orphan: it is not a pane this
	// project released.
	want := []Orphan{{Name: "build", Pane: "ws:p4", State: "done"}, {Name: "review-3", Pane: "ws:p2", State: "idle"}}
	if len(got) != len(want) {
		t.Fatalf("orphans=%#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("orphans=%#v want %#v", got, want)
		}
	}
}

func TestOrphansOfSameNameOtherPaneIsNotAnOrphan(t *testing.T) {
	// The registry matches pane and name together: an agent with a released
	// name in a different pane is not an orphan.
	agents := `[{"name":"review-3","pane_id":"ws:p9","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p2"},{"pane_id":"ws:p9"}]`
	released := []string{"review-3\tws:p2\tgrok\t20261001T000000"}
	env, ctx, cwd := orphanFixture(t, nil, released, agents, panes, "")
	if got := Orphans(ctx, env, cwd); len(got) != 0 {
		t.Fatalf("same name in another pane counted as orphan: %#v", got)
	}
}

func TestOrphansOfReleasedRosterAndOrchestratorPanesAreNotOrphans(t *testing.T) {
	agents := `[{"name":"review","pane_id":"ws:p0","agent_status":"idle"},{"name":"review-2","pane_id":"ws:p1","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p0"},{"pane_id":"ws:p1"},{"pane_id":"ws:p2"}]`
	released := []string{"review\tws:p0\tgrok\t20261001T000000", "review-2\tws:p1\tgrok\t20261001T000000"}
	env, ctx, cwd := orphanFixture(t, []string{"review-2\tws:p1\tgrok\treviewer\txai\t1\t/repo\tnow\tm\tfull\treviewer\treview"}, released, agents, panes, "ws:p0")
	if got := Orphans(ctx, env, cwd); len(got) != 0 {
		t.Fatalf("the orchestrator's or a roster's pane counted as orphan: %#v", got)
	}
}

func TestOrphansOfForeignWorkspaceIsNotAnOrphan(t *testing.T) {
	agents := `[{"name":"review","pane_id":"w9:p1","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p1"}]`
	released := []string{"review\tw9:p1\tgrok\t20261001T000000"}
	env, ctx, cwd := orphanFixture(t, nil, released, agents, panes, "")
	if got := Orphans(ctx, env, cwd); len(got) != 0 {
		t.Fatalf("foreign workspace agent counted as orphan: %#v", got)
	}
}

func TestFindOrphanReportsTheReleasedPaneOnly(t *testing.T) {
	agents := `[{"name":"review-3","pane_id":"ws:p2","agent_status":"idle"}]`
	panes := `[{"pane_id":"ws:p2"}]`
	released := []string{"review-3\tws:p2\tgrok\t20261001T000000"}
	env, ctx, cwd := orphanFixture(t, nil, released, agents, panes, "")
	orphan, ok := FindOrphan("review-3", ctx, env, cwd)
	if !ok || orphan.Pane != "ws:p2" || orphan.State != "idle" {
		t.Fatalf("FindOrphan(review-3)=%#v ok=%v", orphan, ok)
	}
	// The same name released in another pane is a different row, not this
	// live agent.
	env2, ctx2, cwd2 := orphanFixture(t, nil, []string{"review-3\tws:p5\tgrok\t20261001T000000"}, agents, `[{"pane_id":"ws:p2"},{"pane_id":"ws:p5"}]`, "")
	if _, ok := FindOrphan("review-3", ctx2, env2, cwd2); ok {
		t.Fatal("the released name in another pane is not the live agent")
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
	state := StateDir(&ctx, env, cwd)
	if err := os.WriteFile(filepath.Join(state, "released-panes.tsv"), []byte("# name\tpane\tkind\treleased\nreview\tws:p1\tgrok\t20261001T000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Orphans(&ctx, env, cwd); len(got) != 0 {
		t.Fatalf("failed pane list classified an orphan: %#v", got)
	}
}

// releasedPanesWriteFixture is a hermetic state dir with a fake herdr whose
// `pane get` answers the pruning probe: okPanes stay, gonePanes report
// pane_not_found, and every other pane fails with a non-not-found error.
func releasedPanesWriteFixture(t *testing.T, okPanes, gonePanes []string) (platform.Env, *Config, string, string) {
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
	bin := t.TempDir()
	rules := []fakecli.Rule{}
	for _, pane := range okPanes {
		rules = append(rules, fakecli.Rule{Argv: []string{"pane", "get", pane}, Stdout: `{"result":{"pane":{"pane_id":"` + pane + `"}}}`})
	}
	for _, pane := range gonePanes {
		rules = append(rules, fakecli.Rule{Argv: []string{"pane", "get", pane}, Stderr: `{"error":{"code":"pane_not_found","message":"gone"}}`, Code: 1})
	}
	rules = append(rules, fakecli.Rule{AnyArgs: true, Stderr: `{"error":{"code":"boom","message":"down"}}`, Code: 1})
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	env["PATH"] = bin
	env["HERDR_SOHO_FAKECLI_CONFIG"] = bin
	oldNow := platform.Now
	platform.Now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { platform.Now = oldNow })
	ctx := LoadConfig(env, cwd)
	state := StateDir(&ctx, env, cwd)
	return env, &ctx, cwd, state
}

func writeFixtureRows(t *testing.T, state string, rows []string) {
	t.Helper()
	released := "# name\tpane\tkind\treleased\n"
	if len(rows) > 0 {
		released += strings.Join(rows, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(state, "released-panes.tsv"), []byte(released), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFixtureRows(t *testing.T, state string) []ReleasedPane {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(state, "released-panes.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) == 0 || lines[0] != "# name\tpane\tkind\treleased" {
		t.Fatalf("registry header missing: %q", raw)
	}
	rows := make([]ReleasedPane, 0, len(lines)-1)
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		rows = append(rows, ReleasedPane{Name: f[0], Pane: f[1], Kind: f[2], Released: f[3]})
	}
	return rows
}

func TestReleasedPanesRecordAndForget(t *testing.T) {
	env, ctx, cwd, state := releasedPanesWriteFixture(t, []string{"ws:p2"}, nil)
	ReleasedPanesRecord(ctx, env, cwd, "review-3", "ws:p2", "grok")
	want := []ReleasedPane{{Name: "review-3", Pane: "ws:p2", Kind: "grok", Released: "20261001T120000"}}
	if got := readFixtureRows(t, state); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("record=%#v want %#v", got, want)
	}
	// A repeated release of the same (name, pane) refreshes the row, it does
	// not duplicate it.
	ReleasedPanesRecord(ctx, env, cwd, "review-3", "ws:p2", "grok")
	if got := readFixtureRows(t, state); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("repeat record=%#v want %#v", got, want)
	}
	// A pane re-rostered by a spawn is forgotten.
	ReleasedPanesForget(ctx, env, cwd, "ws:p2")
	if got := readFixtureRows(t, state); len(got) != 0 {
		t.Fatalf("forget=%#v want none", got)
	}
}

func TestReleasedPanesWritePrunesGonePanes(t *testing.T) {
	// A row whose pane is gone (pane_not_found) is pruned on the next write;
	// a row whose pane get fails for any other cause is kept.
	env, ctx, cwd, state := releasedPanesWriteFixture(t, nil, []string{"ws:pGone"})
	writeFixtureRows(t, state, []string{
		"keep\tws:ok\tgrok\t20261001T000000",
		"gone\tws:pGone\tgrok\t20261001T000000",
		"down\tws:down\tgrok\t20261001T000000",
	})
	ReleasedPanesRecord(ctx, env, cwd, "fresh", "ws:fresh", "grok")
	got := readFixtureRows(t, state)
	want := []ReleasedPane{
		{Name: "keep", Pane: "ws:ok", Kind: "grok", Released: "20261001T000000"},
		{Name: "down", Pane: "ws:down", Kind: "grok", Released: "20261001T000000"},
		{Name: "fresh", Pane: "ws:fresh", Kind: "grok", Released: "20261001T120000"},
	}
	if len(got) != len(want) {
		t.Fatalf("prune=%#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("prune=%#v want %#v", got, want)
		}
	}
}

func TestReleasedPanesRefusedInsideSkill(t *testing.T) {
	// Like the other state files, the registry refuses to be written inside
	// the skill dir; the refusal dies before anything is written.
	env, ctx, cwd, _ := releasedPanesWriteFixture(t, nil, nil)
	env["HERDR_SOHO_DIR"] = env.Get("HERDR_SOHO_SKILL_DIR")
	_, ok := func() (_ []string, ok bool) {
		defer func() {
			if value := recover(); value != nil {
				if exit, is := value.(*platform.ExitError); is && exit.Code == 2 {
					ok = true
				} else {
					t.Fatalf("unexpected panic: %#v", value)
				}
			}
		}()
		ReleasedPanesRecord(ctx, env, cwd, "review-3", "ws:p2", "grok")
		return nil, false
	}()
	if !ok {
		t.Fatal("the record did not die with the skill-dir refusal")
	}
	skillState := filepath.Join(env.Get("HERDR_SOHO_SKILL_DIR"), "ws")
	if _, err := os.Stat(filepath.Join(skillState, "released-panes.tsv")); !os.IsNotExist(err) {
		t.Fatalf("the refusal wrote a registry file: %v", err)
	}
}
