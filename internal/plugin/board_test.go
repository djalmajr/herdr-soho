package plugin

import (
	contextpkg "context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
	applyBoardUpdate(live, update)
	selected := pickerString(live.visible()[live.Selected]["ref"])
	if selected != "local/a2" {
		t.Fatalf("selection after update=%q want local/a2 (visible=%#v)", selected, live.visible())
	}
	// The ref disappears: the selection falls back to the top, without panics.
	update2 := NewBoardState()
	update2.Entries = []PickerEntry{
		boardEntry("local/a1", "local", "ws-a", "alpha", "orchestrator-1", "claude", "working", "one"),
		boardEntry("local/a4", "local", "ws-a", "alpha", "worker-9", "codex", "idle", "four"),
	}
	applyBoardUpdate(live, update2)
	if live.Selected != 0 {
		t.Fatalf("selection after the ref disappeared=%d want 0", live.Selected)
	}
	// The query is kept across updates.
	live.Query = "two"
	update3 := NewBoardState()
	update3.Entries = []PickerEntry{boardEntry("local/a2", "local", "ws-a", "alpha", "implementer-1", "codex", "idle", "two")}
	applyBoardUpdate(live, update3)
	if live.Query != "two" {
		t.Fatalf("query after update=%q want two", live.Query)
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
