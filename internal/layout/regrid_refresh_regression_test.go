package layout

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// Park re-ids the workers and the grid keeps those new ids. The herd rebuild
// must move the current ids. A rebuild that still joins the roster to the
// pre-park pane list drops the herd tab instead.
func TestCmdRegridRefreshesParkIDsBeforeHerdRebuild(t *testing.T) {
	live := `{"result":{"panes":[{"pane_id":"C-new","tab_id":"t1"},{"pane_id":"p1","tab_id":"t1"},{"pane_id":"p2","tab_id":"t1"}]}}`
	rules := []fakecli.Rule{
		{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 1, Stdout: live},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 2, Stdout: live},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[{"pane_id":"C-new","tab_id":"t1"},{"pane_id":"p1-park","tab_id":"t1"},{"pane_id":"p2-park","tab_id":"t1"}]}}`},
		{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"C-new","tab_id":"t1","workspace_id":"ws"}}}`},
		{Argv: []string{"agent", "get", "sub"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
		{Argv: []string{"agent", "get", "w1"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
		{Argv: []string{"agent", "get", "w2"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
		{Argv: []string{"tab", "get"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
		{Argv: []string{"tab", "create"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"tab_id":"t-new"},"root_pane":{"pane_id":"r-new"}}}`},
		{Argv: []string{"pane", "move", "p1", "--new-tab", "--label", "herd-park", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1-park","tab_id":"park-1"}}}}`},
		{Argv: []string{"pane", "move", "p2", "--tab", "park-1", "--split", "down", "--target-pane", "p1-park", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2-park"}}}}`},
		{Argv: []string{"pane", "move", "p1-park", "--tab", "t1", "--split", "right", "--target-pane", "C-new", "--ratio", "0.5000", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1-park"}}}}`},
		{Argv: []string{"pane", "move", "p2-park", "--tab", "t1", "--split", "down", "--target-pane", "p1-park", "--ratio", "0.5000", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2-park"}}}}`},
		{Argv: []string{"pane", "move", "p1-park", "--tab", "t-new", "--split", "right", "--target-pane", "r-new", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1-park"}}}}`},
		{Argv: []string{"pane", "close", "r-new"}},
		{Argv: []string{"pane", "move", "p2-park", "--tab", "t-new", "--split", "right", "--target-pane", "p1-park", "--ratio", "0.5000", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2-park"}}}}`},
	}
	env, ctx, cwd, dir := layoutFake(t, rules)
	env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t-old", "C-old"
	roster := "sub\tC-new\tgrok\tsub-orchestrator\nw1\tp1\tgrok\timplementer\nw2\tp2\tgrok\tscouter\n"
	if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "herd-tab"), []byte("t1\therd\tmanual\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := platform.Stdout
	var out bytes.Buffer
	platform.Stdout = &out
	defer func() { platform.Stdout = old }()
	var panicked any
	func() {
		defer func() { panicked = recover() }()
		CmdRegrid(nil, ctx, env, cwd)
	}()
	moves := movedPanesOf(t, env)
	listCalls := countCalls(t, env, "pane", "list", "--workspace", "ws")
	stored, readErr := os.ReadFile(filepath.Join(dir, "herd-tab"))
	finalRoster, rosterErr := os.ReadFile(filepath.Join(dir, "agents.tsv"))
	if panicked != nil || !strings.Contains(out.String(), `"tab":"t-new"`) || string(stored) != "t-new\therd\tmanual\n" || !strings.Contains(string(finalRoster), "p1-park") || !strings.Contains(string(finalRoster), "p2-park") {
		t.Fatalf("park re-id was not rebuilt under the current ids: panic=%#v output=%q herd-tab=%q herd-err=%v roster=%q roster-err=%v moves=%v pane-list-workspace-calls=%d", panicked, out.String(), stored, readErr, finalRoster, rosterErr, moves, listCalls)
	}
}
