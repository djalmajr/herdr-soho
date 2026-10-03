package plugin

import (
	contextpkg "context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestBoardPTYProbeEntrypoint(t *testing.T) {
	executable := os.Getenv("HERDR_SOHO_BOARD_PTY_EXE")
	if executable == "" {
		return
	}
	os.Exit(RunBoard(platform.EnvFromOS(), platform.Current(), executable))
}

func boardEntry(ref, machine, wsID, wsLabel, name, kind, status, title string) PickerEntry {
	entry := PickerEntry{
		"ref":             ref,
		"machine":         machine,
		"workspace_id":    wsID,
		"workspace_label": wsLabel,
		"tab_id":          wsID + ":t1",
		"pane_id":         ref,
		"name":            name,
	}
	if kind != "" {
		entry["kind"] = kind
	}
	if status != "" {
		entry["status"] = status
	}
	if title != "" {
		entry["title"] = title
	}
	return entry
}

func boardEnv(t *testing.T, dir, herdr string) platform.Env {
	t.Helper()
	env := platform.Env{}
	for _, item := range fakecli.Env(testutil.CleanEnv(t), dir, fakecli.EnvOptions{IncludeBasePath: true}) {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	env["HERDR_BIN_PATH"] = herdr
	return env
}

func TestBoardOrderGroupsAndOrchestratorFirst(t *testing.T) {
	// Machines arrive with the remote first; local must still come first,
	// workspaces sort by workspace_label, and within a workspace the
	// orchestrator names come before the rest, by name.
	entries := []PickerEntry{
		boardEntry("win/p1", "win", "ws-p", "pinar", "implementer-3", "codex", "blocked", "implementer: fix the login flow"),
		boardEntry("local/a1", "local", "ws-a", "alpha", "orchestrator-10", "claude", "working", "orchestrator: ship the board"),
		boardEntry("win/p2", "win", "ws-p", "pinar", "orchestrator", "claude", "working", "orchestrator: oversee"),
		boardEntry("local/z1", "local", "ws-z", "zeta", "worker-b", "grok", "idle", "research: perf"),
		boardEntry("local/a2", "local", "ws-a", "alpha", "researcher", "codex", "idle", ""),
		boardEntry("local/z2", "local", "ws-z", "zeta", "orchestrator-1", "codex", "done", ""),
		boardEntry("local/a3", "local", "ws-a", "alpha", "", "", "", ""), // no agent: excluded
	}
	got := make([]string, 0, len(entries))
	for _, entry := range boardAgents(entries) {
		got = append(got, pickerString(entry["ref"]))
	}
	want := []string{"local/a1", "local/a2", "local/z2", "local/z1", "win/p2", "win/p1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("board order=%#v want %#v", got, want)
	}
	state := NewBoardState()
	state.Entries = entries
	render := RenderBoard(state, 100)
	lines := strings.Split(strings.TrimSuffix(render, "\n"), "\n")
	headerIndex := map[string]int{}
	for i, line := range lines {
		if strings.HasPrefix(line, "· ") || strings.Contains(line, " (ws-") {
			headerIndex[line] = i
		}
	}
	wantHeaders := []string{"local · alpha (ws-a)", "local · zeta (ws-z)", "win · pinar (ws-p)"}
	var positions []int
	for _, header := range wantHeaders {
		pos, ok := headerIndex[header]
		if !ok {
			t.Fatalf("missing group header %q in:\n%s", header, render)
		}
		positions = append(positions, pos)
	}
	if positions[0] > positions[1] || positions[1] > positions[2] {
		t.Fatalf("group headers out of order:\n%s", render)
	}
	wantRows := map[string]bool{
		">* orchestrator-10 claude working ship the board":  true, // selected: cursor column + marker
		"   researcher codex idle -":                        true, // cursor + idle marker are both spaces
		"   orchestrator-1 codex done -":                    true,
		"   worker-b grok idle perf":                        true,
		" * orchestrator claude working oversee":            true,
		" ! implementer-3 codex blocked fix the login flow": true,
	}
	for _, line := range lines {
		delete(wantRows, line)
	}
	if len(wantRows) > 0 {
		t.Fatalf("missing or cursorless board rows %#v in:\n%s", wantRows, render)
	}
	if strings.Contains(render, "agentes") {
		t.Fatalf("the count footer line is still there:\n%s", render)
	}
	if !strings.HasPrefix(lines[0], "6 agents") {
		t.Fatalf("totals line=%q want the 6-agent count first", lines[0])
	}
}

func TestBoardFilterMatchesTitle(t *testing.T) {
	state := NewBoardState()
	state.Entries = []PickerEntry{
		boardEntry("local/a1", "local", "ws-a", "alpha", "scouter-1", "codex", "working", "board: explore the api"),
		boardEntry("local/a2", "local", "ws-a", "alpha", "implementer-1", "claude", "idle", "fix the login flow"),
		boardEntry("local/a3", "local", "ws-a", "alpha", "orchestrator", "codex", "working", "ship the board"),
	}
	state.Query = "board api"
	got := make([]string, 0)
	for _, entry := range state.visible() {
		got = append(got, pickerString(entry["ref"]))
	}
	if !reflect.DeepEqual(got, []string{"local/a1"}) {
		t.Fatalf("title filter=%#v want [local/a1]", got)
	}
	state.Query = "claude idle"
	got = got[:0]
	for _, entry := range state.visible() {
		got = append(got, pickerString(entry["ref"]))
	}
	if !reflect.DeepEqual(got, []string{"local/a2"}) {
		t.Fatalf("picker-field filter still works: got=%#v want [local/a2]", got)
	}
	state.Query = ""
	if n := len(state.visible()); n != 3 {
		t.Fatalf("empty query shows all agents: got=%d want 3", n)
	}
}

func TestBoardMarkersTotalsAndClock(t *testing.T) {
	entries := []PickerEntry{
		boardEntry("local/a1", "local", "ws-a", "alpha", "orchestrator-1", "claude", "working", "one"),
		boardEntry("local/a2", "local", "ws-a", "alpha", "implementer-1", "codex", "blocked", "two"),
		boardEntry("local/a3", "local", "ws-a", "alpha", "scouter-1", "grok", "idle", "three"),
		boardEntry("local/a4", "local", "ws-a", "alpha", "worker-1", "codex", "done", "four"),
		boardEntry("local/a5", "local", "ws-a", "alpha", "worker-2", "codex", "working", "five"),
	}
	state := NewBoardState()
	state.Entries = entries
	render := RenderBoard(state, 100)
	lines := strings.Split(strings.TrimSuffix(render, "\n"), "\n")
	if lines[0] != "5 agents · 2 working · 1 blocked · 1 idle · 1 done" {
		t.Fatalf("totals line=%q", lines[0])
	}
	// Rows carry the cursor column (only the selected row) before the
	// status marker: line 0 totals, line 1 query, line 2 the group header.
	wantRows := []string{
		">* orchestrator-1 claude working one",
		" ! implementer-1 codex blocked two",
		"   scouter-1 grok idle three", // cursor + idle marker are both spaces
		"   worker-1 codex done four",
		" * worker-2 codex working five",
	}
	for i, want := range wantRows {
		if lines[3+i] != want {
			t.Fatalf("row %d=%q want %q", i, lines[3+i], want)
		}
	}
	// The cursor follows up/down.
	state.ApplyKey("down")
	render = RenderBoard(state, 100)
	if !strings.Contains(render, ">! implementer-1") || strings.Contains(render, ">* orchestrator-1") {
		t.Fatalf("cursor did not follow down:\n%s", render)
	}
	state.ApplyKey("up")
	render = RenderBoard(state, 100)
	if !strings.Contains(render, ">* orchestrator-1") {
		t.Fatalf("cursor did not follow up:\n%s", render)
	}
	// The count footer is gone: the top totals line already gives it.
	if strings.Contains(render, "agentes") {
		t.Fatalf("the count footer is still there:\n%s", render)
	}
	// Zero-count statuses stay out of the line.
	idle := NewBoardState()
	idle.Entries = []PickerEntry{boardEntry("local/a1", "local", "ws-a", "alpha", "orchestrator-1", "claude", "working", "one")}
	if render := RenderBoard(idle, 100); strings.Contains(render, "1 agente") || !strings.Contains(render, ">* orchestrator-1 claude working one") {
		t.Fatalf("idle board render:\n%s", render)
	}
	if line := strings.SplitN(RenderBoard(idle, 100), "\n", 2)[0]; line != "1 agents · 1 working" {
		t.Fatalf("totals line without zero statuses=%q", line)
	}
	// The last-update time is the clock's HH:MM:SS, local.
	oldNow := boardNow
	boardNow = func() time.Time { return time.Date(2026, 10, 3, 9, 45, 30, 0, time.Local) }
	defer func() { boardNow = oldNow }()
	state.UpdatedAt = boardNow().Format("15:04:05")
	if line := strings.SplitN(RenderBoard(state, 100), "\n", 2)[0]; line != "5 agents · 2 working · 1 blocked · 1 idle · 1 done · 09:45:30" {
		t.Fatalf("totals line with time=%q", line)
	}
	// The cut width subtracts the cursor column: the agent lines still fit
	// the board width (cursor + marker + space = 3 prefix columns).
	narrow := strings.Split(strings.TrimSuffix(RenderBoard(state, 30), "\n"), "\n")
	for i := 3; i < 8; i++ {
		if jsLength(narrow[i]) > 30 {
			t.Fatalf("agent line wider than the 30-column board: %q", narrow[i])
		}
	}
}

func TestBoardTitleStripsRolePrefixAndControls(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"orchestrator: ship the board", "ship the board"},
		{"implementer: fix: the login", "fix: the login"}, // only the role prefix goes
		{"security-reviewer: scan the api", "scan the api"},
		{"no prefix title", "no prefix title"},
		{"a b: c d", "a b: c d"}, // the segment before ": " has a space: not a role
		{"orchestrator: \x1b[31mred\x1b[0m task\x00x", "red taskx"},
		{"", ""},
	} {
		if got := BoardTitle(tc.in); got != tc.want {
			t.Fatalf("BoardTitle(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
	entry := boardEntry("local/a1", "local", "ws-a", "alpha", "implementer-3", "codex", "blocked", "implementer: a very long task title that must be cut")
	if line := BoardEntryLine(entry, 40); line != "implementer-3 codex blocked a very long…" {
		t.Fatalf("cut row=%q", line)
	}
}

func TestBoardUpdateKeepsSelectionByRef(t *testing.T) {
	live := NewBoardState()
	live.Entries = []PickerEntry{
		boardEntry("local/a1", "local", "ws-a", "alpha", "orchestrator-1", "claude", "working", "one"),
		boardEntry("local/a2", "local", "ws-a", "alpha", "implementer-1", "codex", "idle", "two"),
		boardEntry("local/a3", "local", "ws-a", "alpha", "scouter-1", "grok", "idle", "three"),
	}
	live.Selected = 1 // local/a2
	// A fresh load reorders the entries and adds one; the selection must
	// stay on local/a2.
	update := NewBoardState()
	update.Entries = []PickerEntry{
		boardEntry("local/a3", "local", "ws-a", "alpha", "scouter-1", "grok", "idle", "three"),
		boardEntry("local/a2", "local", "ws-a", "alpha", "implementer-1", "codex", "blocked", "two"),
		boardEntry("local/a4", "local", "ws-a", "alpha", "worker-9", "codex", "idle", "four"),
		boardEntry("local/a1", "local", "ws-a", "alpha", "orchestrator-1", "claude", "working", "one"),
	}
	update.FinishedMachines = []string{"local"}
	applyBoardUpdate(live, update)
	selected := pickerString(live.visible()[live.Selected]["ref"])
	if selected != "local/a2" {
		t.Fatalf("selection after update=%q want local/a2 (visible=%#v)", selected, live.visible())
	}
	// The ref disappears mid-generation: the selection stays where it is -
	// only the complete load may fall back to the top.
	update2 := NewBoardState()
	update2.Entries = []PickerEntry{
		boardEntry("local/a1", "local", "ws-a", "alpha", "orchestrator-1", "claude", "working", "one"),
		boardEntry("local/a4", "local", "ws-a", "alpha", "worker-9", "codex", "idle", "four"),
	}
	update2.FinishedMachines = []string{"local"}
	applyBoardUpdate(live, update2)
	if live.Selected != 1 {
		t.Fatalf("selection mid-generation after the ref disappeared=%d want 1 (the load is not complete yet)", live.Selected)
	}
	// Only the complete load (UpdatedAt set) drops the selection to the top.
	update3 := NewBoardState()
	update3.UpdatedAt = "12:00:00"
	update3.Entries = append([]PickerEntry(nil), update2.Entries...)
	update3.FinishedMachines = []string{"local"}
	applyBoardUpdate(live, update3)
	if live.Selected != 0 {
		t.Fatalf("selection after the ref disappeared at the end of the load=%d want 0", live.Selected)
	}
	// The query is kept across updates.
	live.Query = "two"
	update4 := NewBoardState()
	update4.Entries = []PickerEntry{boardEntry("local/a2", "local", "ws-a", "alpha", "implementer-1", "codex", "idle", "two")}
	update4.FinishedMachines = []string{"local"}
	applyBoardUpdate(live, update4)
	if live.Query != "two" {
		t.Fatalf("query after update=%q want two", live.Query)
	}
}

// TestBoardPendingMachineKeepsAllOldRows: a machine that has not answered
// the new load keeps ALL of its old rows - not just the first - already on
// the refresh's first publication, and the selection keeps working on them.
func TestBoardPendingMachineKeepsAllOldRows(t *testing.T) {
	s := NewBoardState()
	s.Entries = []PickerEntry{
		boardEntry("middle/a", "middle", "w", "w", "a", "codex", "idle", "a"),
		boardEntry("middle/b", "middle", "w", "w", "b", "codex", "idle", "b"),
	}
	s.Selected = 1
	u := NewBoardState()
	u.LoadingLocal = true
	u.Loading = 1
	applyBoardUpdate(s, u)
	if got := boardRefs(s.Entries); len(got) != 2 || got[0] != "middle/a" || got[1] != "middle/b" {
		t.Fatalf("pending machine rows=%v want both old rows of the middle machine", got)
	}
	s.ApplyKey("enter")
	if got := pickerString(s.LastEntry["ref"]); got != "middle/b" {
		t.Fatalf("Enter copied %q want middle/b (the second pending row)", got)
	}
}

// TestBoardEmptyGenerationDropsOldRefsAtCompletion: the second load's finds
// all succeed without agents. A machine is current once its find terminates
// (with or without rows), so its old rows drop out as it finishes - not only
// at the end - and the complete load publishes only the new (empty) result:
// the old refs are gone and the selection falls to the top.
func TestBoardEmptyGenerationDropsOldRefsAtCompletion(t *testing.T) {
	env, exeA, _, release := boardStreamFixture(t, "middle/w1:p1", "middle")
	live := NewBoardState()
	gateA := boardRemoteGate(t, release, "a", []string{"slow", "fast", "middle"}, live)
	loadBoardEntries(contextpkg.Background(), live, exeA, env, platform.Current(), gateA)
	if len(live.visible()) == 0 {
		t.Fatalf("the first load left the board empty: %#v", live.Entries)
	}
	live.Selected = 1 // a non-top row (the board lists local first)
	exeEmpty, err := fakecli.Install(t, filepath.Dir(exeA), "empty", []fakecli.Rule{{AnyArgs: true, Stdout: ""}})
	if err != nil {
		t.Fatal(err)
	}
	loaded := NewBoardState()
	pubs := 0
	loadBoardEntries(contextpkg.Background(), loaded, exeEmpty, env, platform.Current(), func() {
		pubs++
		applyBoardUpdate(live, cloneBoardState(loaded))
		// Completion is recorded per machine, not by the presence of rows:
		// a finished machine must not keep its old rows mid-reload.
		finished := cloneBoardState(loaded).FinishedMachines
		for _, machine := range finished {
			for _, entry := range live.Entries {
				if pickerString(entry["machine"]) == machine {
					t.Fatalf("publication %d: machine %q finished the empty load but its old row %q remains (rows=%v)", pubs, machine, pickerString(entry["ref"]), boardRefs(live.Entries))
				}
			}
		}
	})
	loaded.UpdatedAt = boardNow().Format("15:04:05")
	applyBoardUpdate(live, cloneBoardState(loaded))
	if len(live.Entries) != 0 {
		t.Fatalf("the completed empty load kept old rows: %v", boardRefs(live.Entries))
	}
	if live.Selected != 0 {
		t.Fatalf("selection after the empty completion=%d want 0", live.Selected)
	}
}

// TestBoardMachineLeavingTheListDisappearsAtCompletion: a machine that is no
// longer in the machine list keeps its old rows while the new load is in
// flight (its find never runs, so it never finishes), but the complete load
// publishes only the new result, so its rows disappear and the selection
// falls to the top.
func TestBoardMachineLeavingTheListDisappearsAtCompletion(t *testing.T) {
	localRow := boardEntry("local/a1", "local", "w", "alpha", "orchestrator-1", "claude", "working", "one")
	live := NewBoardState()
	live.Entries = []PickerEntry{localRow, boardEntry("gone/g1", "gone", "w", "beta", "worker-1", "codex", "idle", "two")}
	live.Selected = 1
	update := NewBoardState()
	update.Entries = []PickerEntry{localRow}
	update.FinishedMachines = []string{"local"}
	applyBoardUpdate(live, update)
	if got := boardRefs(live.Entries); len(got) != 2 || got[1] != "gone/g1" {
		t.Fatalf("mid-load rows=%v want the gone machine's row to stay until the load completes", got)
	}
	update2 := NewBoardState()
	update2.UpdatedAt = boardNow().Format("15:04:05")
	update2.Entries = []PickerEntry{localRow}
	update2.FinishedMachines = []string{"local"}
	applyBoardUpdate(live, update2)
	if got := boardRefs(live.Entries); len(got) != 1 || got[0] != "local/a1" {
		t.Fatalf("final rows=%v want only the new load's rows (the gone machine disappears)", got)
	}
	if live.Selected != 0 {
		t.Fatalf("selection after the machine left=%d want 0", live.Selected)
	}
}

// TestBoardFailedMachineSwapsOldRowsForFailureLine: a machine whose find
// fails in the new load terminates its find, so its old rows drop out (even
// mid-load) and today's failure line takes their place.
func TestBoardFailedMachineSwapsOldRowsForFailureLine(t *testing.T) {
	localRow := boardEntry("local/a1", "local", "w", "alpha", "orchestrator-1", "claude", "working", "one")
	live := NewBoardState()
	live.Entries = []PickerEntry{localRow,
		boardEntry("slow/s1", "slow", "w", "beta", "worker-1", "codex", "idle", "two"),
		boardEntry("slow/s2", "slow", "w", "beta", "worker-2", "codex", "idle", "three")}
	live.Selected = 2
	update := NewBoardState()
	update.Entries = []PickerEntry{localRow}
	update.FinishedMachines = []string{"local", "slow"}
	update.Failures = []PickerFailure{{Label: "slow", Cause: "exit 127: no rule"}}
	applyBoardUpdate(live, update)
	if got := boardRefs(live.Entries); len(got) != 1 || got[0] != "local/a1" {
		t.Fatalf("mid-load rows=%v want the failed machine's old rows swapped out", got)
	}
	if render := RenderBoard(live, 80); !strings.Contains(render, "máquina slow: falhou") {
		t.Fatalf("the failure line is missing from the render:\n%s", render)
	}
	update2 := NewBoardState()
	update2.UpdatedAt = boardNow().Format("15:04:05")
	update2.Entries = []PickerEntry{localRow}
	update2.FinishedMachines = []string{"local", "slow"}
	update2.Failures = []PickerFailure{{Label: "slow", Cause: "exit 127: no rule"}}
	applyBoardUpdate(live, update2)
	if got := boardRefs(live.Entries); len(got) != 1 || got[0] != "local/a1" {
		t.Fatalf("final rows=%v want only the new load's rows", got)
	}
}

func TestBoardRefreshKeyRules(t *testing.T) {
	state := NewBoardState()
	state.Entries = []PickerEntry{boardEntry("local/a1", "local", "ws-a", "alpha", "orchestrator-1", "claude", "working", "one")}
	// r with an empty filter refreshes and does not enter the filter.
	if action := state.FeedChunk("r"); action != "update" || state.Query != "" {
		t.Fatalf("r with empty filter action=%q query=%q", action, state.Query)
	}
	// r with a non-empty filter is plain text.
	state.Query = "on"
	if action := state.FeedChunk("r"); action != "" || state.Query != "onr" {
		t.Fatalf("r with a filter action=%q query=%q", action, state.Query)
	}
	// Ctrl-R (0x12) refreshes at any time, keeping the filter.
	if action := state.FeedChunk(string(rune(0x12))); action != "update" || state.Query != "onr" {
		t.Fatalf("ctrl-r action=%q query=%q", action, state.Query)
	}
}

func TestBoardLoadMachineFailureBecomesStatusLine(t *testing.T) {
	dir := t.TempDir()
	local := `{"ref":"local/w1:p1","machine":"local","workspace_id":"w1","workspace_label":"appliance","tab_id":"w1:t1","pane_id":"w1:p1","name":"orchestrator","kind":"claude","status":"working","title":"orchestrator: run"}` + "\n"
	cli, err := fakecli.Install(t, dir, "herdr-soho", []fakecli.Rule{
		{Argv: []string{"find", "--json"}, Stdout: local},
		{Argv: []string{"find", "--json", "--machine", "win"}, Code: 1, Stderr: "boom"},
	})
	if err != nil {
		t.Fatal(err)
	}
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{{Argv: []string{"machine", "list", "--json"}, Stdout: `[{"label":"win","enabled":true}]`}})
	if err != nil {
		t.Fatal(err)
	}
	state := NewBoardState()
	loadBoardEntries(contextpkg.Background(), state, cli, boardEnv(t, dir, herdr), "darwin", func() {})
	if len(state.Failures) != 1 || state.Failures[0].Label != "win" || state.Failures[0].Cause != "exit 1: boom" {
		t.Fatalf("failures=%#v", state.Failures)
	}
	if len(state.Entries) != 1 || pickerString(state.Entries[0]["ref"]) != "local/w1:p1" {
		t.Fatalf("entries=%#v", state.Entries)
	}
	if !strings.Contains(RenderBoard(state, 100), "máquina win: falhou (exit 1: boom)") {
		t.Fatalf("board render without the failure line:\n%s", RenderBoard(state, 100))
	}
}

func TestBoardRefreshDuringLoadWaitsAndRunsOnce(t *testing.T) {
	dir := t.TempDir()
	local := `{"ref":"local/w1:p1","machine":"local","workspace_id":"w1","workspace_label":"alpha","tab_id":"w1:t1","pane_id":"local/w1:p1","name":"orchestrator","kind":"claude","status":"working","title":"orchestrator: run"}` + "\n" +
		`{"ref":"local/w1:p2","machine":"local","workspace_id":"w1","workspace_label":"alpha","tab_id":"w1:t1","pane_id":"local/w1:p2","name":"implementer-1","kind":"codex","status":"idle","title":"implementer: task"}` + "\n"
	m := `{"ref":"m/p9","machine":"m","workspace_id":"w9","workspace_label":"pinar","tab_id":"w9:t1","pane_id":"m/p9","name":"researcher","kind":"grok","status":"working","title":"research: x"}` + "\n"
	cli, err := fakecli.Install(t, dir, "herdr-soho", []fakecli.Rule{
		{Argv: []string{"find", "--json"}, Stdout: local, Delay: 400},
		{Argv: []string{"find", "--json", "--machine", "m"}, Stdout: m, Delay: 400},
	})
	if err != nil {
		t.Fatal(err)
	}
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{{Argv: []string{"machine", "list", "--json"}, Stdout: `[{"label":"m","enabled":true}]`}})
	if err != nil {
		t.Fatal(err)
	}
	env := boardEnv(t, dir, herdr)
	oldNow := boardNow
	boardNow = func() time.Time { return time.Date(2026, 10, 3, 9, 45, 30, 0, time.Local) }
	defer func() { boardNow = oldNow }()
	oldInterval := boardRefreshInterval
	boardRefreshInterval = time.Hour // the 10 s ticker must not interfere
	defer func() { boardRefreshInterval = oldInterval }()

	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "board-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = input.Close()
		_ = writer.Close()
		_ = output.Close()
	})
	shutdown := make(chan os.Signal, 1)
	finished := make(chan int, 1)
	go func() { finished <- runBoardLoopWithSignals(env, "darwin", cli, input, output, false, shutdown) }()

	findCalls := func() int {
		calls, err := fakecli.ReadCalls(filepath.Join(dir, "herdr-soho.calls.jsonl"))
		if err != nil {
			return 0
		}
		n := 0
		for _, call := range calls {
			if reflect.DeepEqual(call.Argv, []string{"find", "--json"}) {
				n++
			}
		}
		return n
	}

	// The first load takes at least 800 ms (400 ms local + 400 ms remote);
	// this r lands while it is in progress.
	time.Sleep(150 * time.Millisecond)
	if _, err := writer.Write([]byte("r")); err != nil {
		t.Fatal(err)
	}
	// The in-flight load is not started again; the queued refresh runs
	// exactly once after it ends: local finds go 1 -> 2, and stop there.
	deadline := time.Now().Add(30 * time.Second) // generous: Windows process starts can be slow
	for time.Now().Before(deadline) && findCalls() < 2 {
		select {
		case code := <-finished:
			t.Fatalf("board exited while refreshing, code=%d", code)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if findCalls() != 2 {
		t.Fatalf("local finds after a refresh during a load=%d want 2 (one coalesced, no duplicate)", findCalls())
	}
	time.Sleep(300 * time.Millisecond) // the single r is consumed: nothing more follows
	if findCalls() != 2 {
		t.Fatalf("local finds settled at %d want 2 (the queued refresh must not stack)", findCalls())
	}
	data, _ := os.ReadFile(output.Name())
	if !strings.Contains(string(data), "3 agents · 2 working · 1 idle · 09:45:30") || !strings.Contains(string(data), "m · pinar (w9)") {
		t.Fatalf("board after the coalesced refresh missing the fresh data in:\n%s", data)
	}
	if _, err := writer.Write([]byte("\x1b")); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	select {
	case code := <-finished:
		if code != 0 {
			t.Fatalf("board code=%d want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("board did not close after Esc")
	}
}

func TestBridgeBoardOpensBoardPane(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	data, _ := json.Marshal(map[string]any{"result": map[string]any{"pane": map[string]any{"workspace_id": "ws-a", "cwd": cwd}}})
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
		{Argv: []string{"pane", "get", "pane-a"}, Stdout: string(data)},
		{Argv: BoardArguments(), Stdout: "opened\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	env := platform.Env{}
	for _, entry := range fakecli.Env(os.Environ(), dir, fakecli.EnvOptions{IncludeBasePath: true}) {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	env["HERDR_BIN_PATH"] = herdr
	env["HERDR_PLUGIN_CONTEXT_JSON"] = `{"workspace_id":"ws-a","tab_id":"tab-a","focused_pane_id":"pane-a"}`
	result, err := Bridge("board", env, platform.Current(), "")
	if err != nil || result.Code != 0 || !strings.Contains(result.Out, "target workspace=ws-a pane=pane-a") {
		t.Fatalf("bridge board result=%+v err=%v", result, err)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(dir, "herdr.json"))
	if err != nil || len(calls) != 2 {
		t.Fatalf("herdr calls=%#v err=%v", calls, err)
	}
	want := []string{"plugin", "pane", "open", "--plugin", "djalmajr.herdr-soho", "--entrypoint", "board", "--placement", "overlay", "--focus"}
	if !reflect.DeepEqual(calls[1].Argv, want) {
		t.Fatalf("pane open argv=%#v want %#v", calls[1].Argv, want)
	}
}

func TestBridgeUnknownActionListsBoard(t *testing.T) {
	_, err := Bridge("frobnicate", platform.Env{}, "darwin", "")
	bridgeErr, ok := err.(*BridgeError)
	if !ok || bridgeErr.Code != ExitInvalidTarget {
		t.Fatalf("error=%v", err)
	}
	if !strings.Contains(bridgeErr.Message, "(use 'doctor', 'roster', 'board' or 'pick')") {
		t.Fatalf("unknown action message=%q", bridgeErr.Message)
	}
}

// boardStreamFixture installs two fake herdr-soho CLIs (one per load
// generation, "a" and "b") plus the fake herdr machine list. Each remote
// find of a generation blocks on its own release file (a WaitFile gate named
// after the generation and the machine), so the test - not the wall clock -
// fixes each load's arrival order. Load b's middle row uses the ref and name
// given by the test.
func boardStreamFixture(t *testing.T, middleBRef, middleBName string) (platform.Env, string, string, func(string, string) error) {
	t.Helper()
	dir := t.TempDir()
	gates := filepath.Join(dir, "gates")
	if err := os.MkdirAll(gates, 0o755); err != nil {
		t.Fatal(err)
	}
	local := `{"ref":"local/w1:p1","machine":"local","workspace_id":"w1","tab_id":"w1:t1","pane_id":"w1:p1","name":"local","kind":"codex","status":"idle"}` + "\n"
	mkRules := func(load string) []fakecli.Rule {
		rules := []fakecli.Rule{{Argv: []string{"find", "--json"}, Stdout: local}}
		for _, machine := range []string{"slow", "fast", "middle"} {
			ref, name := machine+"/w1:p1", machine
			if machine == "middle" && load == "b" {
				ref, name = middleBRef, middleBName
			}
			row := fmt.Sprintf(`{"ref":"%s","machine":"%s","workspace_id":"w1","tab_id":"w1:t1","pane_id":"%s","name":"%s","kind":"codex","status":"idle"}`+"\n", ref, machine, ref, name)
			rules = append(rules, fakecli.Rule{Argv: []string{"find", "--json", "--machine", machine}, WaitFile: filepath.Join(gates, load+"-"+machine), Stdout: row})
		}
		return rules
	}
	exeA, err := fakecli.Install(t, dir, "herdr-soho-a", mkRules("a"))
	if err != nil {
		t.Fatal(err)
	}
	exeB, err := fakecli.Install(t, dir, "herdr-soho-b", mkRules("b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{{Argv: []string{"machine", "list", "--json"}, Stdout: `[{"label":"slow","enabled":true},{"label":"fast","enabled":true},{"label":"middle","enabled":true}]`}}); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{}
	for _, item := range fakecli.Env(testutil.CleanEnv(t), dir, fakecli.EnvOptions{IncludeBasePath: true}) {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	env["HERDR_BIN_PATH"] = filepath.Join(dir, "herdr")
	release := func(load, machine string) error {
		return os.WriteFile(filepath.Join(gates, load+"-"+machine), []byte("go"), 0o644)
	}
	t.Cleanup(func() {
		// Unblock finds still waiting after the test (a failed wait) so the
		// fakes exit instead of polling their release file forever.
		for _, load := range []string{"a", "b"} {
			for _, machine := range []string{"slow", "fast", "middle"} {
				_ = os.WriteFile(filepath.Join(gates, load+"-"+machine), []byte("go"), 0o644)
			}
		}
	})
	return env, exeA, exeB, release
}

// boardRemoteGate returns the publish closure that starts the load's gate
// releases once the loader reaches the remote finds (the publication with
// the remote loading count set). It waits 150 ms first so every find is
// already spawned and polling (the parent starts them back to back), then
// releases the gates 250 ms apart: the fakes poll their release file every
// 20 ms, so the arrival order is exactly `order`.
func boardRemoteGate(t *testing.T, release func(string, string) error, load string, order []string, loaded *BoardState) func() {
	t.Helper()
	started := false
	return func() {
		if started || loaded.LoadingLocal || loaded.Loading != 3 || len(loaded.Entries) == 0 {
			return
		}
		started = true
		stop := make(chan struct{})
		t.Cleanup(func() { close(stop) })
		go func() {
			time.Sleep(150 * time.Millisecond)
			for i, machine := range order {
				if i > 0 {
					select {
					case <-stop:
						return
					case <-time.After(250 * time.Millisecond):
					}
				}
				if err := release(load, machine); err != nil {
					select {
					case <-stop:
						return
					default:
						t.Error(err)
					}
					return
				}
			}
		}()
	}
}

func boardRefs(list []PickerEntry) []string {
	refs := make([]string, len(list))
	for i, entry := range list {
		refs[i] = pickerString(entry["ref"])
	}
	return refs
}

// TestBoardStreamingSelectionKeepsSelectedRef is the review's selection
// probe as a test: two real loads through the fakes, every publication of the
// second applied by the production consumer. The selection follows the ref -
// not a row index, load b re-arriving the machines in another order - across
// the whole refresh, and Enter still copies it.
func TestBoardStreamingSelectionKeepsSelectedRef(t *testing.T) {
	env, exeA, exeB, release := boardStreamFixture(t, "middle/w1:p1", "middle")
	// Load 1 (open): arrival fast, middle, slow.
	live := NewBoardState()
	gateA := boardRemoteGate(t, release, "a", []string{"fast", "middle", "slow"}, live)
	loadBoardEntries(contextpkg.Background(), live, exeA, env, platform.Current(), gateA)
	list := live.visible()
	middle := -1
	for i, entry := range list {
		if pickerString(entry["ref"]) == "middle/w1:p1" {
			middle = i
		}
	}
	if middle < 0 {
		t.Fatalf("the first load has no middle/w1:p1 row: %#v", list)
	}
	live.Selected = middle
	before := pickerString(list[live.Selected]["ref"])
	// Load 2 (refresh): arrival slow, fast, middle - middle re-arrives last
	// (the old row must stay on screen until then) and ends up on another
	// row than in load 1.
	loaded := NewBoardState()
	pubs := 0
	gateB := boardRemoteGate(t, release, "b", []string{"slow", "fast", "middle"}, loaded)
	loadBoardEntries(contextpkg.Background(), loaded, exeB, env, platform.Current(), func() {
		pubs++
		gateB()
		applyBoardUpdate(live, cloneBoardState(loaded))
		list := live.visible()
		if len(list) == 0 {
			t.Fatalf("publication %d left the board empty", pubs)
		}
		if ref := pickerString(list[live.Selected]["ref"]); ref != "middle/w1:p1" {
			t.Fatalf("publication %d: the selection left middle/w1:p1 (ref=%q entries=%#v)", pubs, ref, live.Entries)
		}
	})
	// The loader stamps the clock and publishes the final state (like the
	// production loop does after loadBoardEntries returns).
	loaded.UpdatedAt = boardNow().Format("15:04:05")
	applyBoardUpdate(live, cloneBoardState(loaded))
	wantIdx := -1
	for i, entry := range live.visible() {
		if pickerString(entry["ref"]) == "middle/w1:p1" {
			wantIdx = i
		}
	}
	if wantIdx < 0 {
		t.Fatalf("the second load lost middle/w1:p1: %#v", live.visible())
	}
	if wantIdx == middle {
		t.Fatalf("the fixture must move the middle row (load 1=%d, load 2=%d); the test would be vacuous", middle, wantIdx)
	}
	if live.Selected != wantIdx {
		t.Fatalf("selection=%d want %d (the ref moved across rows; the selection must follow the ref)", live.Selected, wantIdx)
	}
	live.ApplyKey("enter")
	after := pickerString(live.LastEntry["ref"])
	if before != "middle/w1:p1" || after != "middle/w1:p1" {
		t.Fatalf("Enter copied %q want middle/w1:p1 (before=%q)", after, before)
	}
}

// TestBoardSelectionFallsToTopOnlyWhenTheLoadIsComplete: the selected ref
// disappears mid-load (the middle machine now returns a different agent). The
// selection must not jump to the top while the load is incomplete; only the
// complete load may do it.
func TestBoardSelectionFallsToTopOnlyWhenTheLoadIsComplete(t *testing.T) {
	env, exeA, exeB, release := boardStreamFixture(t, "middle/w2:p2", "middle-2")
	live := NewBoardState()
	gateA := boardRemoteGate(t, release, "a", []string{"fast", "middle", "slow"}, live)
	loadBoardEntries(contextpkg.Background(), live, exeA, env, platform.Current(), gateA)
	list := live.visible()
	middle := -1
	for i, entry := range list {
		if pickerString(entry["ref"]) == "middle/w1:p1" {
			middle = i
		}
	}
	if middle <= 0 {
		t.Fatalf("the first load must have middle/w1:p1 on a non-top row: %#v", list)
	}
	live.Selected = middle
	loaded := NewBoardState()
	pubs := 0
	goneAt := -1
	gateB := boardRemoteGate(t, release, "b", []string{"slow", "fast", "middle"}, loaded)
	loadBoardEntries(contextpkg.Background(), loaded, exeB, env, platform.Current(), func() {
		pubs++
		gateB()
		applyBoardUpdate(live, cloneBoardState(loaded))
		list := live.visible()
		if len(list) == 0 {
			t.Fatalf("publication %d left the board empty", pubs)
		}
		present := false
		for _, entry := range list {
			if pickerString(entry["ref"]) == "middle/w1:p1" {
				present = true
				break
			}
		}
		if present {
			if ref := pickerString(list[live.Selected]["ref"]); ref != "middle/w1:p1" {
				t.Fatalf("publication %d: the selection left middle/w1:p1 (ref=%q)", pubs, ref)
			}
			return
		}
		if goneAt < 0 {
			goneAt = pubs
		}
		if live.Selected == 0 {
			t.Fatalf("publication %d: the selection fell to the top before the load completed (gone at %d)", pubs, goneAt)
		}
	})
	// The loader stamps the clock and publishes the final state: only now may
	// the selection fall to the top (the ref is gone).
	loaded.UpdatedAt = boardNow().Format("15:04:05")
	applyBoardUpdate(live, cloneBoardState(loaded))
	if goneAt <= 0 {
		t.Fatalf("the fixture never showed the ref disappearing mid-load (publications=%d); the test would be vacuous", pubs)
	}
	if live.Selected != 0 {
		t.Fatalf("selection=%d after the complete load want 0 (the ref is gone)", live.Selected)
	}
}

// TestBoardCutMeasuresDisplayColumns: titles with CJK characters (two
// columns each), an emoji (the U+1F300-U+1FAFF block, two columns) and
// combining marks (zero columns) must never make the agent row pass the
// requested width - measured in display columns - and the cut must never
// split a rune.
func TestBoardCutMeasuresDisplayColumns(t *testing.T) {
	titles := []string{
		strings.Repeat("界", 30),                          // CJK: two columns per character
		"task " + "\U0001F600" + strings.Repeat("x", 40), // emoji: two columns
		strings.Repeat("a\u0300", 25),                    // combining mark: zero columns
	}
	for _, title := range titles {
		for _, width := range []int{10, 20, 30} {
			s := NewBoardState()
			s.Entries = []PickerEntry{boardEntry("local/p", "local", "ws", "alpha", "agent", "codex", "idle", title)}
			lines := strings.Split(strings.TrimSuffix(RenderBoard(s, width), "\n"), "\n")
			row := lines[3] // totals, query, header, agent row
			if w := displayWidth(row); w > width {
				t.Fatalf("width %d, title %q: row %q has display width %d", width, title, row, w)
			}
			if !utf8.ValidString(row) {
				t.Fatalf("width %d, title %q: the cut split a rune: %q", width, title, row)
			}
		}
	}
}
