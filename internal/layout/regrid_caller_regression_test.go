package layout

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// movedPanesOf returns the first argument (the moved pane) of every
// "pane move" call in the recorded calls.
func movedPanesOf(t *testing.T, env platform.Env) []string {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(env["HERDR_SOHO_FAKECLI_CONFIG"], "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, call := range calls {
		if len(call.Argv) >= 3 && call.Argv[0] == "pane" && call.Argv[1] == "move" {
			out = append(out, call.Argv[2])
		}
	}
	return out
}

func hasCall(t *testing.T, env platform.Env, args ...string) bool {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(env["HERDR_SOHO_FAKECLI_CONFIG"], "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if reflect.DeepEqual(call.Argv, args) {
			return true
		}
	}
	return false
}

func countCalls(t *testing.T, env platform.Env, args ...string) int {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(env["HERDR_SOHO_FAKECLI_CONFIG"], "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for _, call := range calls {
		if len(call.Argv) >= len(args) && reflect.DeepEqual(call.Argv[:len(args)], args) {
			n++
		}
	}
	return n
}

func TestCmdRegridRegisteredCallerIsNeverRelocated(t *testing.T) {
	t.Run(`regrid: a caller still registered on the roster is never moved or closed, even from a stale tab`, func(t *testing.T) {
		// Mutation captured: dropping the caller exclusion or trusting the stale
		// HERDR_TAB_ID parks the caller pane into herd-park and moves foreign panes.
		// The caller (promoted sub-orchestrator "sub", pane C) is live in t-live
		// while the inherited HERDR_TAB_ID says t-old; its own pane, the unrelated
		// pane U, and the caller's identity must never be relocated.
		before := `{"result":{"panes":[{"pane_id":"C","tab_id":"t-live"},{"pane_id":"p1","tab_id":"t-live"},{"pane_id":"U","tab_id":"t-live"},{"pane_id":"p2","tab_id":"t1"}]}}`
		after := `{"result":{"panes":[{"pane_id":"C","tab_id":"t-live"},{"pane_id":"p1","tab_id":"t-live"},{"pane_id":"U","tab_id":"t-live"},{"pane_id":"p2","tab_id":"t-live"}]}}`
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 1, Stdout: before},
			{Argv: []string{"agent", "get", "sub"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "w1"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"agent", "get", "w2"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "t-live", "--split", "right", "--target-pane", "C", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2"}}}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 2, Stdout: after},
			{Argv: []string{"pane", "move", "p1", "--new-tab", "--label", "herd-park", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1","tab_id":"park-1"}}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "park-1", "--split", "down", "--target-pane", "p1", "--ratio", "0.5", "--no-focus"}},
			{Argv: []string{"pane", "move", "p1", "--tab", "t-live", "--split", "right", "--target-pane", "C", "--ratio", "0.5000", "--no-focus"}},
			{Argv: []string{"pane", "move", "p2", "--tab", "t-live", "--split", "down", "--target-pane", "p1", "--ratio", "0.5000", "--no-focus"}},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 3, Stdout: after},
		}
		env, ctx, cwd, dir := layoutFake(t, rules)
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"], env["HERDR_SOHO_SPLIT_MAX_PANES"] = "t-old", "C", "4"
		roster := "sub\tC\tgrok\tsub-orchestrator\nw1\tp1\tgrok\timplementer\nw2\tp2\tgrok\tscouter\n"
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(roster), 0o600); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "herd-tab")
		if err := os.WriteFile(file, []byte("t1\therd\tauto\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := platform.Stdout
		var out bytes.Buffer
		platform.Stdout = &out
		defer func() { platform.Stdout = old }()
		if code := CmdRegrid(nil, ctx, env, cwd); code != 0 {
			t.Fatalf("CmdRegrid()=%d", code)
		}
		// The summary names the live tab, not the stale inherited one.
		if out.String() != "{\"regridded\":[{\"tab\":\"t-live\",\"label\":\"caller\",\"panes\":3,\"cols\":2}]}\n" {
			t.Fatalf("output=%q", out.String())
		}
		// The caller's own pane and the unrelated pane are never moved; nothing is closed.
		moved := movedPanesOf(t, env)
		for _, p := range moved {
			if p == "C" || p == "U" {
				t.Fatalf("caller or foreign pane relocated: moved=%v", moved)
			}
		}
		if countCalls(t, env, "pane", "close") > 0 {
			t.Fatalf("a pane was closed: moved=%v", moved)
		}
		// Fast path: the inherited pane is live in the pane list, so no alias
		// read (herdr pane current --current) was needed.
		if n := countCalls(t, env, "pane", "current", "--current"); n != 0 {
			t.Fatalf("fast path performed %d alias reads", n)
		}
		// The pull-back and the rebuild target the live tab with the caller as split anchor.
		if !hasCall(t, env, "pane", "move", "p2", "--tab", "t-live", "--split", "right", "--target-pane", "C", "--ratio", "0.5", "--no-focus") {
			t.Fatalf("pull-back did not target the live tab")
		}
		if !hasCall(t, env, "pane", "move", "p1", "--tab", "t-live", "--split", "right", "--target-pane", "C", "--ratio", "0.5000", "--no-focus") {
			t.Fatalf("grid rebuild did not target the live tab")
		}
		// The emptied herd tab is forgotten; the roster is untouched (no re-ids happened).
		stored, err := os.ReadFile(file)
		if err != nil || len(stored) != 0 {
			t.Fatalf("herd-tab=%q err=%v", stored, err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
		if err != nil || string(got) != roster {
			t.Fatalf("roster=%q err=%v", got, err)
		}
	})
}

func TestCmdRegridGenuineSubOrchestratorStillCounts(t *testing.T) {
	t.Run(`regrid: a genuine sub-orchestrator worker still counts toward the pull-back capacity`, func(t *testing.T) {
		// Mutation captured: excluding every sub-orchestrator (or counting the
		// caller) pulls the wrong set of workers back into the caller tab.
		// cap=3, caller C (registered) + genuine sub-orchestrator s1 in t0,
		// workers p2 (t1) and p3 (t2) in herd tabs: exactly p2 is pulled.
		live1 := `{"result":{"panes":[{"pane_id":"C","tab_id":"t0"},{"pane_id":"q1","tab_id":"t0"},{"pane_id":"p2","tab_id":"t1"},{"pane_id":"p3","tab_id":"t2"}]}}`
		live2 := `{"result":{"panes":[{"pane_id":"C","tab_id":"t0"},{"pane_id":"q1","tab_id":"t0"},{"pane_id":"p2","tab_id":"t0"},{"pane_id":"p3","tab_id":"t2"}]}}`
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 1, Stdout: live1},
			{Argv: []string{"agent", "get", "sub"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "s1"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"tab", "get", "t2"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"agent", "get", "w2"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "t0", "--split", "right", "--target-pane", "C", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2"}}}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 2, Stdout: live2},
			{Argv: []string{"pane", "move", "q1", "--new-tab", "--label", "herd-park", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"q1","tab_id":"park-1"}}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "park-1", "--split", "down", "--target-pane", "q1", "--ratio", "0.5", "--no-focus"}},
			{Argv: []string{"pane", "move", "q1", "--tab", "t0", "--split", "right", "--target-pane", "C", "--ratio", "0.5000", "--no-focus"}},
			{Argv: []string{"pane", "move", "p2", "--tab", "t0", "--split", "down", "--target-pane", "q1", "--ratio", "0.5000", "--no-focus"}},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 3, Stdout: live2},
			{Argv: []string{"tab", "rename", "t2", "scout"}},
		}
		env, ctx, cwd, dir := layoutFake(t, rules)
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"], env["HERDR_SOHO_SPLIT_MAX_PANES"] = "t0", "C", "3"
		roster := "sub\tC\tgrok\tsub-orchestrator\ns1\tq1\tgrok\tsub-orchestrator\nw2\tp2\tgrok\tscouter\nw3\tp3\tgrok\tscouter\n"
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(roster), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "herd-tab"), []byte("t1\therd\tauto\nt2\therd\tauto\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := platform.Stdout
		var out bytes.Buffer
		platform.Stdout = &out
		defer func() { platform.Stdout = old }()
		if code := CmdRegrid(nil, ctx, env, cwd); code != 0 {
			t.Fatalf("CmdRegrid()=%d", code)
		}
		if out.String() != "{\"regridded\":[{\"tab\":\"t0\",\"label\":\"caller\",\"panes\":3,\"cols\":2}]}\n" {
			t.Fatalf("output=%q", out.String())
		}
		// s1 counts against the cap: with one worker already in t0 and cap 3,
		// p2 is pulled but p3 stops the pull-back. Counting the caller would
		// pull nothing; excluding s1 too would pull p3 as well.
		if n := countCalls(t, env, "pane", "move", "p2", "--tab", "t0", "--split", "right", "--target-pane", "C", "--ratio", "0.5", "--no-focus"); n != 1 {
			t.Fatalf("pull-back of p2 called %d times", n)
		}
		for _, p := range movedPanesOf(t, env) {
			if p == "C" || p == "p3" {
				t.Fatalf("caller or the capped worker relocated: moved=%v", movedPanesOf(t, env))
			}
		}
		// The caller stays in the roster; the solo t2 keeps its entry (relabelled).
		got, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
		if err != nil || string(got) != roster {
			t.Fatalf("roster=%q err=%v", got, err)
		}
		stored, err := os.ReadFile(filepath.Join(dir, "herd-tab"))
		if err != nil || !strings.Contains(string(stored), "t2") || strings.Contains(string(stored), "t1") {
			t.Fatalf("herd-tab=%q err=%v", stored, err)
		}
	})
}

func TestCmdRegridPartialParkTracksReturnedIDs(t *testing.T) {
	t.Run(`regrid: a failed park still records the moved pane's actual id and targets it`, func(t *testing.T) {
		// Mutation captured: ignoring the returned park id keeps a stale roster
		// row and splits the next parked pane off a dead pane.
		live := `{"result":{"panes":[{"pane_id":"C","tab_id":"t0"},{"pane_id":"p1","tab_id":"t0"},{"pane_id":"p2","tab_id":"t0"}]}}`
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 1, Stdout: live},
			{Argv: []string{"agent", "get", "w1"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "w2"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 2, Stdout: live},
			{Argv: []string{"pane", "move", "p1", "--new-tab", "--label", "herd-park", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1-park","tab_id":"park-1"}}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "park-1", "--split", "down", "--target-pane", "p1-park", "--ratio", "0.5", "--no-focus"}, Code: 1},
		}
		env, ctx, cwd, dir := layoutFake(t, rules)
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t0", "C"
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("w1\tp1\tgrok\timplementer\nw2\tp2\tgrok\tscouter\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			CmdRegrid(nil, ctx, env, cwd)
		}()
		// The successful move is recorded even though the park then failed:
		// w1 holds the actual returned id, w2 keeps the one that never moved.
		got, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
		if err != nil || string(got) != "w1\tp1-park\tgrok\timplementer\nw2\tp2\tgrok\tscouter\n" {
			t.Fatalf("roster=%q err=%v", got, err)
		}
		e, ok := recovered.(*platform.ExitError)
		if !ok || e.Code != 4 || e.Msg != "regrid: could not park the workers of tab t0 in a temporary tab" {
			t.Fatalf("panic=%#v", recovered)
		}
		// The follow-up park move split off the actual id, not the stale one.
		if !hasCall(t, env, "pane", "move", "p2", "--tab", "park-1", "--split", "down", "--target-pane", "p1-park", "--ratio", "0.5", "--no-focus") {
			t.Fatalf("the second park move did not target the actual id")
		}
		if countCalls(t, env, "pane", "close") > 0 {
			t.Fatalf("a worker pane was closed after the failed park")
		}
	})
}

func TestCmdRegridPartialGridFailurePreservesLiveWorkers(t *testing.T) {
	t.Run(`regrid: a failed move-back keeps the workers alive, persists the actual ids, and says where each is`, func(t *testing.T) {
		// Mutation captured: not tracking a successful move's returned id, or
		// reporting the stale ids, loses the location of the live workers.
		live := `{"result":{"panes":[{"pane_id":"C","tab_id":"t0"},{"pane_id":"p1","tab_id":"t0"},{"pane_id":"p2","tab_id":"t0"}]}}`
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 1, Stdout: live},
			{Argv: []string{"agent", "get", "w1"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "w2"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 2, Stdout: live},
			{Argv: []string{"pane", "move", "p1", "--new-tab", "--label", "herd-park", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1-park","tab_id":"park-1"}}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "park-1", "--split", "down", "--target-pane", "p1-park", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2-park"}}}}`},
			{Argv: []string{"pane", "move", "p1-park", "--tab", "t0", "--split", "right", "--target-pane", "C", "--ratio", "0.5000", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1-back"}}}}`},
			{Argv: []string{"pane", "move", "p2-park", "--tab", "t0", "--split", "down", "--target-pane", "p1-back", "--ratio", "0.5000", "--no-focus"}, Code: 1},
		}
		env, ctx, cwd, dir := layoutFake(t, rules)
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t0", "C"
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("w1\tp1\tgrok\timplementer\nw2\tp2\tgrok\tscouter\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			CmdRegrid(nil, ctx, env, cwd)
		}()
		e, ok := recovered.(*platform.ExitError)
		if !ok || e.Code != 4 {
			t.Fatalf("panic=%#v", recovered)
		}
		want := "regrid: a move back into t0 failed; p2-park are alive in tab park-1 (label herd-park) and p1-back are alive in tab t0; the roster already holds their current pane ids"
		if e.Msg != want {
			t.Fatalf("panic=%q want %q", e.Msg, want)
		}
		// Every successful move's actual id is persisted: w1 moved twice
		// (park, then back) and holds p1-back; w2 holds the park id.
		got, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
		if err != nil || string(got) != "w1\tp1-back\tgrok\timplementer\nw2\tp2-park\tgrok\tscouter\n" {
			t.Fatalf("roster=%q err=%v", got, err)
		}
		// The second grid move split off the actual id of the first one.
		if !hasCall(t, env, "pane", "move", "p2-park", "--tab", "t0", "--split", "down", "--target-pane", "p1-back", "--ratio", "0.5000", "--no-focus") {
			t.Fatalf("the grid did not target the actual moved id")
		}
		// Nothing is closed or killed on the failure path.
		if countCalls(t, env, "pane", "close") > 0 {
			t.Fatalf("a worker pane was closed after the failed move-back")
		}
	})
}

func TestCallerContextPrefersLiveTab(t *testing.T) {
	t.Run(`callerContext: a live inherited pane takes the no-extra-read fast path`, func(t *testing.T) {
		// The alias channel is poisoned: if the fast path read it, the
		// resolution would fail and the assertion would catch it.
		env, ctx, _, dir := layoutFake(t, []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Code: 3, Stderr: "poison"}})
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t-old", "C"
		live := []any{jsonjs.O("pane_id", "C", "tab_id", "t-live"), jsonjs.O("pane_id", "X", "tab_id", "t-x")}
		if tab, pane, refuse := callerContext(ctx, env, live, dir, "ws"); tab != "t-live" || pane != "C" || refuse != "" {
			t.Fatalf("callerContext()=%q,%q,%q want t-live,C,none", tab, pane, refuse)
		}
	})
	t.Run(`callerContext: an unresolvable inherited pane refuses with an explicit identity error`, func(t *testing.T) {
		env, ctx, _, dir := layoutFake(t, []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Code: 1, Stderr: "cannot resolve current pane: server is not running"}})
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t-old", "C"
		live := []any{jsonjs.O("pane_id", "X", "tab_id", "t-x")}
		tab, pane, refuse := callerContext(ctx, env, live, dir, "ws")
		if tab != "" || pane != "" || refuse == "" {
			t.Fatalf("unresolvable alias callerContext()=%q,%q,%q want none,none,error", tab, pane, refuse)
		}
		if !strings.Contains(refuse, "C") || !strings.Contains(refuse, "refusing the whole regrid") {
			t.Fatalf("refusal=%q is not an explicit identity error", refuse)
		}
	})
	t.Run(`callerContext: the alias channel re-identifies the caller row to the actual pane`, func(t *testing.T) {
		env, ctx, _, dir := layoutFake(t, []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"C1","tab_id":"t-live","workspace_id":"ws"}}}`}})
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t-old", "C"
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("sub\tC\tgrok\tsub-orchestrator\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		live := []any{jsonjs.O("pane_id", "C1", "tab_id", "t-live"), jsonjs.O("pane_id", "X", "tab_id", "t-x")}
		if tab, pane, refuse := callerContext(ctx, env, live, dir, "ws"); tab != "t-live" || pane != "C1" || refuse != "" {
			t.Fatalf("callerContext()=%q,%q,%q want t-live,C1,none", tab, pane, refuse)
		}
		got, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
		if err != nil || string(got) != "sub\tC1\tgrok\tsub-orchestrator\n" {
			t.Fatalf("roster=%q err=%v", got, err)
		}
	})
	t.Run(`callerContext: an alias that maps to another workspace is refused by the state scope`, func(t *testing.T) {
		env, ctx, _, dir := layoutFake(t, []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"C1","tab_id":"t-x","workspace_id":"other"}}}`}})
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t-old", "C"
		live := []any{jsonjs.O("pane_id", "C1", "tab_id", "t-x")}
		tab, pane, refuse := callerContext(ctx, env, live, dir, "ws")
		if tab != "" || pane != "" || refuse == "" {
			t.Fatalf("foreign-workspace alias callerContext()=%q,%q,%q want none,none,error", tab, pane, refuse)
		}
		if !strings.Contains(refuse, "other") || !strings.Contains(refuse, "ws") {
			t.Fatalf("refusal=%q does not name both workspaces", refuse)
		}
	})
	t.Run(`callerContext: an alias result without a workspace scope is refused`, func(t *testing.T) {
		// Missing scope is uncertainty, not permission.
		env, ctx, _, dir := layoutFake(t, []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"C1","tab_id":"t-live"}}}`}})
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t-old", "C"
		live := []any{jsonjs.O("pane_id", "C1", "tab_id", "t-live")}
		tab, pane, refuse := callerContext(ctx, env, live, dir, "ws")
		if tab != "" || pane != "" || refuse == "" {
			t.Fatalf("missing-scope alias callerContext()=%q,%q,%q want none,none,error", tab, pane, refuse)
		}
		if !strings.Contains(refuse, "workspace scope") {
			t.Fatalf("refusal=%q does not name the missing scope", refuse)
		}
	})
	t.Run(`callerContext: a resolved pane that is not live in the reported tab is refused`, func(t *testing.T) {
		// The alias says tab t-x, but the pane is live in t-live: a mismatch is
		// refused instead of guessed.
		env, ctx, _, dir := layoutFake(t, []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"C1","tab_id":"t-x","workspace_id":"ws"}}}`}})
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t-old", "C"
		live := []any{jsonjs.O("pane_id", "C1", "tab_id", "t-live")}
		tab, pane, refuse := callerContext(ctx, env, live, dir, "ws")
		if tab != "" || pane != "" || refuse == "" {
			t.Fatalf("topology-mismatch alias callerContext()=%q,%q,%q want none,none,error", tab, pane, refuse)
		}
		if !strings.Contains(refuse, "C1") || !strings.Contains(refuse, "t-x") {
			t.Fatalf("refusal=%q does not name the mismatched pane and tab", refuse)
		}
	})
	t.Run(`callerContext: no HERDR_PANE_ID means no caller context and no alias read`, func(t *testing.T) {
		env := platform.Env{"HERDR_TAB_ID": "t-old"}
		live := []any{jsonjs.O("pane_id", "C", "tab_id", "t-live")}
		if tab, pane, refuse := callerContext(nil, env, live, "", "ws"); tab != "" || pane != "" || refuse != "" {
			t.Fatalf("no HERDR_PANE_ID callerContext()=%q,%q,%q want none,none,none", tab, pane, refuse)
		}
	})
}

func TestWithoutCallerDropsOnlyTheCallingPane(t *testing.T) {
	// Mutation captured: dropping more than the caller loses genuine workers;
	// dropping nothing counts the caller as a worker.
	if got := withoutCaller([]string{"C", "p1", "p2"}, "C"); len(got) != 2 || got[0] != "p1" || got[1] != "p2" {
		t.Fatalf("withoutCaller()=%v", got)
	}
	if got := withoutCaller([]string{"C", "p1"}, ""); len(got) != 2 {
		t.Fatalf("empty caller withoutCaller()=%v", got)
	}
}

func TestCmdRegridReidentifiesStaleCallerRowViaPaneCurrent(t *testing.T) {
	t.Run(`regrid: a stale inherited caller id is resolved through pane current and its roster row is re-identified`, func(t *testing.T) {
		// The caller's pane was re-idi'd: the inherited HERDR_PANE_ID=C0 is not
		// live, and only the native alias channel (herdr pane current
		// --current) still resolves it — to pane C1 in tab t-live. The caller
		// row is re-identified to C1, the regrid runs against the actual tab,
		// and the caller pane is never moved or closed.
		live := `{"result":{"panes":[{"pane_id":"C1","tab_id":"t-live"},{"pane_id":"p1","tab_id":"t-live"}]}}`
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 1, Stdout: live},
			{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"C1","tab_id":"t-live","workspace_id":"ws"}}}`},
			{Argv: []string{"agent", "get", "sub"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "w1"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 2, Stdout: live},
			{Argv: []string{"pane", "move", "p1", "--new-tab", "--label", "herd-park", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1","tab_id":"park-1"}}}}`},
			{Argv: []string{"pane", "move", "p1", "--tab", "t-live", "--split", "right", "--target-pane", "C1", "--ratio", "0.5000", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1"}}}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 3, Stdout: live},
		}
		env, ctx, cwd, dir := layoutFake(t, rules)
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t-old", "C0"
		roster := "sub\tC0\tgrok\tsub-orchestrator\nw1\tp1\tgrok\timplementer\n"
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(roster), 0o600); err != nil {
			t.Fatal(err)
		}
		old := platform.Stdout
		var out bytes.Buffer
		platform.Stdout = &out
		defer func() { platform.Stdout = old }()
		if code := CmdRegrid(nil, ctx, env, cwd); code != 0 {
			t.Fatalf("CmdRegrid()=%d", code)
		}
		if out.String() != `{"regridded":[{"tab":"t-live","label":"caller","panes":2,"cols":2}]}`+"\n" {
			t.Fatalf("output=%q", out.String())
		}
		// The stale row is re-identified to the actual pane id; the worker row is untouched.
		got, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
		if err != nil || string(got) != "sub\tC1\tgrok\tsub-orchestrator\nw1\tp1\tgrok\timplementer\n" {
			t.Fatalf("roster=%q err=%v", got, err)
		}
		// The alias channel was read exactly once (slow path only).
		if n := countCalls(t, env, "pane", "current", "--current"); n != 1 {
			t.Fatalf("alias read called %d times", n)
		}
		// Neither the stale nor the actual caller pane was moved; nothing was closed.
		for _, p := range movedPanesOf(t, env) {
			if p == "C0" || p == "C1" {
				t.Fatalf("caller pane relocated: moved=%v", movedPanesOf(t, env))
			}
		}
		if countCalls(t, env, "pane", "close") > 0 {
			t.Fatalf("a pane was closed")
		}
	})
}

func TestCmdRegridUnresolvedCallerRefusesTheWholeCommand(t *testing.T) {
	// The inherited caller identity exists (HERDR_PANE_ID=C-old) but is stale
	// and cannot be resolved safely. The live roster ALREADY holds the
	// caller's actual pane C-new in a herd tab t1 with two more roster panes
	// (p1, p2): a caller-block-only refusal would let the herd-tab rebuild
	// relocate the caller (withoutCaller(empty) includes it). The whole
	// command is refused before any pane move, pane close, tab create, or
	// state mutation, with a nonzero exit and an explicit identity/scope
	// error.
	t.Run(`regrid: an unresolved caller identity refuses the whole command while the roster holds the caller's live pane in a herd tab`, func(t *testing.T) {
		live := `{"result":{"panes":[{"pane_id":"C-new","tab_id":"t1"},{"pane_id":"p1","tab_id":"t1"},{"pane_id":"p2","tab_id":"t1"}]}}`
		roster := "sub\tC-new\tgrok\tsub-orchestrator\nw1\tp1\tgrok\timplementer\nw2\tp2\tgrok\tscouter\n"
		cases := []struct {
			name string
			rule fakecli.Rule
			want string
		}{
			{
				"an unavailable alias call",
				fakecli.Rule{Argv: []string{"pane", "current", "--current"}, Code: 1, Stderr: "cannot resolve current pane: server is not running"},
				"regrid: caller identity unresolved (the inherited caller pane C-old is not live in workspace ws and herdr pane current --current could not resolve it); refusing the whole regrid before any pane move, pane close, tab create or state change",
			},
			{
				"an alias result with no workspace scope",
				fakecli.Rule{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"C-new","tab_id":"t1"}}}`},
				"regrid: caller identity unresolved (the resolved caller identity has no workspace scope (missing scope is uncertainty, not permission)); refusing the whole regrid before any pane move, pane close, tab create or state change",
			},
			{
				"an alias result that maps outside the state scope",
				fakecli.Rule{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"C-new","tab_id":"t1","workspace_id":"other"}}}`},
				"regrid: caller identity unresolved (the caller resolves to workspace other, outside this state scope ws); refusing the whole regrid before any pane move, pane close, tab create or state change",
			},
			{
				"an alias result that does not match the live topology",
				fakecli.Rule{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"C-ghost","tab_id":"t1","workspace_id":"ws"}}}`},
				"regrid: caller identity unresolved (the resolved caller pane C-ghost is not live in tab t1 of workspace ws); refusing the whole regrid before any pane move, pane close, tab create or state change",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				rules := []fakecli.Rule{
					{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
					{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 1, Stdout: live},
					tc.rule,
					{Argv: []string{"agent", "get", "sub"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
					{Argv: []string{"agent", "get", "w1"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
					{Argv: []string{"agent", "get", "w2"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
					{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
				}
				env, ctx, cwd, dir := layoutFake(t, rules)
				env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t-old", "C-old"
				if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(roster), 0o600); err != nil {
					t.Fatal(err)
				}
				herdTab := "t1\therd\tauto\n"
				if err := os.WriteFile(filepath.Join(dir, "herd-tab"), []byte(herdTab), 0o600); err != nil {
					t.Fatal(err)
				}
				var panicked any
				func() {
					defer func() { panicked = recover() }()
					CmdRegrid(nil, ctx, env, cwd)
				}()
				e, isExit := panicked.(*platform.ExitError)
				if !isExit {
					t.Fatalf("panic=%#v (want a nonzero exit with the identity/scope error)", panicked)
				}
				if e.Code == 0 {
					t.Fatalf("exit code 0 on an unresolved caller identity: %q", e.Msg)
				}
				if e.Msg != tc.want {
					t.Fatalf("panic=%q want %q", e.Msg, tc.want)
				}
				// No mutating call of any kind: no move, no close, no tab create.
				if n := countCalls(t, env, "pane", "move"); n != 0 {
					t.Fatalf("%d pane moves were made with an unresolved caller identity", n)
				}
				if n := countCalls(t, env, "pane", "close"); n != 0 {
					t.Fatalf("%d panes were closed with an unresolved caller identity", n)
				}
				if n := countCalls(t, env, "tab", "create"); n != 0 {
					t.Fatalf("%d tabs were created with an unresolved caller identity", n)
				}
				// The alias was attempted exactly once; no retry, no stale fallback.
				if n := countCalls(t, env, "pane", "current", "--current"); n != 1 {
					t.Fatalf("alias read called %d times", n)
				}
				// No state mutation: the roster (including the live C-new row) and
				// the herd-tab file are byte-identical.
				got, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
				if err != nil || string(got) != roster {
					t.Fatalf("roster=%q err=%v", got, err)
				}
				stored, err := os.ReadFile(filepath.Join(dir, "herd-tab"))
				if err != nil || string(stored) != herdTab {
					t.Fatalf("herd-tab=%q err=%v", stored, err)
				}
			})
		}
	})
}

func TestCmdRegridPartialGridFailureTracksUnchangedIDMoves(t *testing.T) {
	t.Run(`regrid: a successful grid move that keeps its id is reported at the destination, and the bounded recovery moves the still-parked worker back`, func(t *testing.T) {
		// Mutation captured: treating r.Changes as the move-success list puts an
		// unchanged-id success at the source — p1, which already moved into the
		// caller tab, would be reported as still parked. p1 and p2 keep their
		// ids through every move; only the tracking can tell them apart.
		live := `{"result":{"panes":[{"pane_id":"C","tab_id":"t0"},{"pane_id":"p1","tab_id":"t0"},{"pane_id":"p2","tab_id":"t0"}]}}`
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 1, Stdout: live},
			{Argv: []string{"agent", "get", "w1"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "w2"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 2, Stdout: live},
			{Argv: []string{"pane", "move", "p1", "--new-tab", "--label", "herd-park", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1","tab_id":"park-1"}}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "park-1", "--split", "down", "--target-pane", "p1", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2"}}}}`},
			{Argv: []string{"pane", "move", "p1", "--tab", "t0", "--split", "right", "--target-pane", "C", "--ratio", "0.5000", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1"}}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "t0", "--split", "down", "--target-pane", "p1", "--ratio", "0.5000", "--no-focus"}, Code: 1},
			{Argv: []string{"pane", "move", "p2", "--tab", "t0", "--split", "right", "--target-pane", "C", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2"}}}}`},
		}
		env, ctx, cwd, dir := layoutFake(t, rules)
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t0", "C"
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("w1\tp1\tgrok\timplementer\nw2\tp2\tgrok\tscouter\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			CmdRegrid(nil, ctx, env, cwd)
		}()
		e, ok := recovered.(*platform.ExitError)
		if !ok || e.Code != 4 {
			t.Fatalf("panic=%#v", recovered)
		}
		// p1's unchanged-id success is reported at the destination (t0), and the
		// bounded recovery moved p2 back into t0: no worker is left parked.
		want := "regrid: a move back into t0 failed; p1 p2 are alive in tab t0; the roster already holds their current pane ids"
		if e.Msg != want {
			t.Fatalf("panic=%q want %q", e.Msg, want)
		}
		// No id changed anywhere: the roster is byte-identical.
		got, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
		if err != nil || string(got) != "w1\tp1\tgrok\timplementer\nw2\tp2\tgrok\tscouter\n" {
			t.Fatalf("roster=%q err=%v", got, err)
		}
		// The bounded recovery attempted p2 exactly once and never closed a pane.
		if n := countCalls(t, env, "pane", "move", "p2", "--tab", "t0", "--split", "right", "--target-pane", "C", "--ratio", "0.5", "--no-focus"); n != 1 {
			t.Fatalf("recovery move called %d times (want exactly one attempt)", n)
		}
		if countCalls(t, env, "pane", "close") > 0 {
			t.Fatalf("a worker pane was closed after the failed move-back")
		}
	})
}

func TestCmdRegridFailedRecoveryPreservesLiveWorkers(t *testing.T) {
	t.Run(`regrid: a failed bounded recovery still names the actual locations and never closes the parked worker`, func(t *testing.T) {
		// The grid fails on p2 and the best-effort recovery move fails too: p2
		// must be reported where it actually is (the park tab) and p1 where it
		// actually is (t0, after an unchanged-id success). Bounded means exactly
		// one recovery attempt per the first still-parked worker, no retries.
		live := `{"result":{"panes":[{"pane_id":"C","tab_id":"t0"},{"pane_id":"p1","tab_id":"t0"},{"pane_id":"p2","tab_id":"t0"}]}}`
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 1, Stdout: live},
			{Argv: []string{"agent", "get", "w1"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "w2"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Call: 2, Stdout: live},
			{Argv: []string{"pane", "move", "p1", "--new-tab", "--label", "herd-park", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1","tab_id":"park-1"}}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "park-1", "--split", "down", "--target-pane", "p1", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2"}}}}`},
			{Argv: []string{"pane", "move", "p1", "--tab", "t0", "--split", "right", "--target-pane", "C", "--ratio", "0.5000", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1"}}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "t0", "--split", "down", "--target-pane", "p1", "--ratio", "0.5000", "--no-focus"}, Code: 1},
			{Argv: []string{"pane", "move", "p2", "--tab", "t0", "--split", "right", "--target-pane", "C", "--ratio", "0.5", "--no-focus"}, Code: 1, Stderr: "split failed"},
		}
		env, ctx, cwd, dir := layoutFake(t, rules)
		env["HERDR_TAB_ID"], env["HERDR_PANE_ID"] = "t0", "C"
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("w1\tp1\tgrok\timplementer\nw2\tp2\tgrok\tscouter\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			CmdRegrid(nil, ctx, env, cwd)
		}()
		e, ok := recovered.(*platform.ExitError)
		if !ok || e.Code != 4 {
			t.Fatalf("panic=%#v", recovered)
		}
		want := "regrid: a move back into t0 failed; p2 are alive in tab park-1 (label herd-park) and p1 are alive in tab t0; the roster already holds their current pane ids"
		if e.Msg != want {
			t.Fatalf("panic=%q want %q", e.Msg, want)
		}
		got, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
		if err != nil || string(got) != "w1\tp1\tgrok\timplementer\nw2\tp2\tgrok\tscouter\n" {
			t.Fatalf("roster=%q err=%v", got, err)
		}
		// Bounded: exactly one recovery attempt, no retries.
		if n := countCalls(t, env, "pane", "move", "p2", "--tab", "t0", "--split", "right", "--target-pane", "C", "--ratio", "0.5", "--no-focus"); n != 1 {
			t.Fatalf("recovery move called %d times (want exactly one attempt)", n)
		}
		// The parked worker is preserved, not closed or killed.
		if countCalls(t, env, "pane", "close") > 0 {
			t.Fatalf("a worker pane was closed during the failed recovery")
		}
	})
}

func TestCmdRegridCallerInOwnHerdTabIsNotPulledBack(t *testing.T) {
	t.Run(`regrid: the caller's own recorded herd tab is skipped by the pull-back, so its workers are parked and gridded exactly once`, func(t *testing.T) {
		// P1 (final regrid review): the resolved caller C-new already lives in
		// the recorded herd tab t1 with workers p1 and p2; the inherited env
		// holds the stale C-old and the alias resolves C-new/t1/ws. The initial
		// worker set already contains t1's workers, so the pull-back loop must
		// skip t1 == callerTab instead of appending and moving the same
		// workers into their own tab again (which pulled p1 back into t1 and
		// left it parked twice, printing panes:4). Only p1 and p2 may ever be
		// moved; no caller or unrelated worker change may occur.
		live := `{"result":{"panes":[{"pane_id":"C-new","tab_id":"t1"},{"pane_id":"p1","tab_id":"t1"},{"pane_id":"p2","tab_id":"t1"}]}}`
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: live},
			{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"C-new","tab_id":"t1","workspace_id":"ws"}}}`},
			{Argv: []string{"agent", "get", "sub"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "w1"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "w2"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"tab", "get"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"tab", "create"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"tab_id":"t-new"},"root_pane":{"pane_id":"r-new"}}}`},
			// park + grid back into the caller tab (grid ratios are 0.5000, pull-back 0.5)
			{Argv: []string{"pane", "move", "p1", "--new-tab", "--label", "herd-park", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1-park","tab_id":"park-1"}}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "park-1", "--split", "down", "--target-pane", "p1-park", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2-park"}}}}`},
			{Argv: []string{"pane", "move", "p1-park", "--tab", "t1", "--split", "right", "--target-pane", "C-new", "--ratio", "0.5000", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1"}}}}`},
			{Argv: []string{"pane", "move", "p2-park", "--tab", "t1", "--split", "down", "--target-pane", "p1", "--ratio", "0.5000", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2"}}}}`},
			// the herd-tab rebuild of t1's workers into a fresh tab
			{Argv: []string{"pane", "move", "p1", "--tab", "t-new", "--split", "right", "--target-pane", "r-new", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1"}}}}`},
			{Argv: []string{"pane", "close", "r-new"}},
			{Argv: []string{"pane", "move", "p2", "--tab", "t-new", "--split", "right", "--target-pane", "p1", "--ratio", "0.5000", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2"}}}}`},
			// the same-tab pull-back the P1 performed (the fixed code never issues these)
			{Argv: []string{"pane", "move", "p1", "--tab", "t1", "--split", "right", "--target-pane", "C-new", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p1"}}}}`},
			{Argv: []string{"pane", "move", "p2", "--tab", "t1", "--split", "right", "--target-pane", "C-new", "--ratio", "0.5", "--no-focus"}, Stdout: `{"result":{"move_result":{"pane":{"pane_id":"p2"}}}}`},
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
		if panicked != nil {
			e, isExit := panicked.(*platform.ExitError)
			if !isExit {
				t.Fatalf("panic=%#v", panicked)
			}
			t.Fatalf("CmdRegrid failed: %q", e.Msg)
		}
		// Unique worker count: the caller plus exactly its two workers, each
		// parked and gridded once; the rebuilt tab reports its two workers.
		if out.String() != `{"regridded":[{"tab":"t1","label":"caller","panes":3,"cols":2},{"tab":"t-new","label":"herd","panes":2,"cols":2}]}`+"\n" {
			t.Fatalf("output=%q", out.String())
		}
		if n := countCalls(t, env, "pane", "current", "--current"); n != 1 {
			t.Fatalf("alias read called %d times", n)
		}
		var pullBack int
		parked := map[string]int{}
		allowed := map[string]bool{"p1": true, "p2": true, "p1-park": true, "p2-park": true}
		for _, call := range readMoves(t, env) {
			argv := call.Argv
			if argv[2] == "C-new" || argv[2] == "C-old" {
				t.Fatalf("caller relocated: %v", argv)
			}
			if !allowed[argv[2]] {
				t.Fatalf("an unrelated worker was moved: %v", argv)
			}
			tab, ratio := "", ""
			for i := 0; i < len(argv)-1; i++ {
				switch argv[i] {
				case "--tab":
					tab = argv[i+1]
				case "--ratio":
					ratio = argv[i+1]
				}
			}
			// Pull-back and recovery split off the caller at ratio 0.5; grid
			// moves use 0.5000. A 0.5 move into the caller tab is the P1.
			if tab == "t1" && ratio == "0.5" {
				pullBack++
			}
			// Parking is the new-tab move plus the down-splits into the park
			// tab; each worker must appear exactly once.
			if (len(argv) > 3 && argv[3] == "--new-tab") || tab == "park-1" {
				parked[argv[2]]++
			}
		}
		if pullBack != 0 {
			t.Fatalf("the caller's own herd tab was pulled back into itself %d time(s)", pullBack)
		}
		if len(parked) != 2 || parked["p1"] != 1 || parked["p2"] != 1 {
			t.Fatalf("workers were not parked exactly once: parked=%v", parked)
		}
		// No worker or caller pane is closed (only the created tab root may be).
		for _, call := range readCalls(t, env) {
			if len(call.Argv) >= 3 && call.Argv[0] == "pane" && call.Argv[1] == "close" && (call.Argv[2] == "C-new" || call.Argv[2] == "p1" || call.Argv[2] == "p2") {
				t.Fatalf("a worker or caller pane was closed: %v", call.Argv)
			}
		}
		// The roster is byte-identical: every park/grid id round-tripped.
		got, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
		if err != nil || string(got) != roster {
			t.Fatalf("roster=%q err=%v", got, err)
		}
		// t1's herd-tab entry was replaced by the rebuilt tab.
		stored, err := os.ReadFile(filepath.Join(dir, "herd-tab"))
		if err != nil || string(stored) != "t-new\therd\tmanual\n" {
			t.Fatalf("herd-tab=%q err=%v", stored, err)
		}
	})
}

func readMoves(t *testing.T, env platform.Env) []fakecli.Call {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(env["HERDR_SOHO_FAKECLI_CONFIG"], "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out []fakecli.Call
	for _, call := range calls {
		if len(call.Argv) >= 3 && call.Argv[0] == "pane" && call.Argv[1] == "move" {
			out = append(out, call)
		}
	}
	return out
}

func readCalls(t *testing.T, env platform.Env) []fakecli.Call {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(env["HERDR_SOHO_FAKECLI_CONFIG"], "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	return calls
}
