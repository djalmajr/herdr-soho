package plugin

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// TestSessionViewSearchFocusLiteralKeys: in the search focus every
// printable key - including p/w/s/v/r - is text; in the list focus the
// picker treats them as sort/view keys. The legacy input keeps typing them
// into the query and ignores Tab.
func TestSessionViewSearchFocusLiteralKeys(t *testing.T) {
	p := NewPickerState()
	p.ViewState.Enabled = true
	p.Entries = sessionViewEntries()
	if got := p.FeedChunk("\t"); got != "" || !p.ViewState.SearchFocused {
		t.Fatalf("tab action=%q searchFocused=%v", got, p.ViewState.SearchFocused)
	}
	for _, key := range []string{"p", "w", "s", "v", "r"} {
		if got := p.FeedChunk(key); got != "" {
			t.Fatalf("%s in the search focus returned %q", key, got)
		}
	}
	if p.Query != "pwsvr" || !p.ViewState.SearchFocused {
		t.Fatalf("query=%q searchFocused=%v want pwsvr/focused", p.Query, p.ViewState.SearchFocused)
	}
	if p.ViewState.Sort != sessionSortPane || p.ViewState.Descending || p.ViewState.View != sessionViewTable {
		t.Fatalf("the search focus changed the sort/view state: %+v", p.ViewState)
	}
	// The picker's list focus types r into the search (no refresh there).
	p2 := NewPickerState()
	p2.ViewState.Enabled = true
	p2.Entries = sessionViewEntries()
	if got := p2.FeedChunk("r"); got != "" || p2.Query != "r" || !p2.ViewState.SearchFocused {
		t.Fatalf("picker r: action=%q query=%q focused=%v", got, p2.Query, p2.ViewState.SearchFocused)
	}
	// Legacy: p/w/s/v/r are search text and Tab does nothing.
	l := NewPickerState()
	l.Entries = sessionViewEntries()
	for _, key := range []string{"p", "w", "s", "v", "r"} {
		if got := l.FeedChunk(key); got != "" {
			t.Fatalf("legacy %s returned %q", key, got)
		}
	}
	if l.Query != "pwsvr" || l.ViewState.SearchFocused {
		t.Fatalf("legacy query=%q focused=%v", l.Query, l.ViewState.SearchFocused)
	}
	if got := l.FeedChunk("\t"); got != "" || l.ViewState.SearchFocused || l.Query != "pwsvr" {
		t.Fatalf("legacy tab action=%q focused=%v query=%q", got, l.ViewState.SearchFocused, l.Query)
	}
}

// TestSessionViewTabRawAndKitty: raw Tab and the kitty plain-Tab encodings
// toggle the search/list focus on the picker and the board; a modified Tab
// is ignored, and the legacy input ignores every Tab.
func TestSessionViewTabRawAndKitty(t *testing.T) {
	check := func(t *testing.T, name string, feed func(string) string, chunks []string, focused func() bool) {
		t.Helper()
		want := []bool{true, false, true}
		for i, chunk := range chunks {
			if got := feed(chunk); got != "" {
				t.Fatalf("%s %q returned action %q", name, chunk, got)
			}
			if focused() != want[i] {
				t.Fatalf("%s after %q: searchFocused=%v want %v", name, chunk, focused(), want[i])
			}
		}
	}
	picker := NewPickerState()
	picker.ViewState.Enabled = true
	picker.Entries = sessionViewEntries()
	check(t, "picker", picker.FeedChunk, []string{"\t", "\x1b[9u", "\x1b[9;1u"}, func() bool { return picker.ViewState.SearchFocused })
	// Modified Tab is not a focus toggle.
	if got := picker.FeedChunk("\x1b[9;2u"); got != "" || !picker.ViewState.SearchFocused {
		t.Fatalf("shift-tab action=%q focused=%v want ignored/stay", got, picker.ViewState.SearchFocused)
	}
	if got := picker.FeedChunk("\x1b[9;5u"); got != "" || !picker.ViewState.SearchFocused {
		t.Fatalf("ctrl-tab action=%q focused=%v want ignored/stay", got, picker.ViewState.SearchFocused)
	}
	board := NewBoardState()
	board.ViewState.Enabled = true
	board.Entries = sessionViewEntries()
	check(t, "board", board.FeedChunk, []string{"\t", "\x1b[9u", "\x1b[9;1u"}, func() bool { return board.ViewState.SearchFocused })
	legacy := NewPickerState()
	legacy.Entries = sessionViewEntries()
	legacy.FeedChunk("\t")
	legacy.FeedChunk("\x1b[9u")
	if legacy.ViewState.SearchFocused {
		t.Fatal("the legacy input must not enter the search focus on Tab")
	}
}

// TestSessionViewSortKeysAndDirections: a different sort starts ascending;
// repeating the current sort toggles descending; v toggles the view; the
// selection follows the full ref through every step.
func TestSessionViewSortKeysAndDirections(t *testing.T) {
	p := NewPickerState()
	p.ViewState.Enabled = true
	p.Entries = sessionViewEntries()
	p.Selected = 3 // local/w6:p6 in the default pane-ascending order
	steps := []struct {
		key        string
		sort       string
		descending bool
		view       string
		index      int
	}{
		{"w", sessionSortWorkspace, false, sessionViewTable, 3},
		{"w", sessionSortWorkspace, true, sessionViewTable, 2},
		{"p", sessionSortPane, false, sessionViewTable, 3},
		{"s", sessionSortStatus, false, sessionViewTable, 3},
		{"v", sessionSortStatus, false, sessionViewTree, 3},
	}
	for i, step := range steps {
		if got := p.FeedChunk(step.key); got != "" {
			t.Fatalf("step %d (%s) action=%q", i, step.key, got)
		}
		vs := p.ViewState
		if vs.Sort != step.sort || vs.Descending != step.descending || vs.View != step.view {
			t.Fatalf("step %d (%s): sort=%q desc=%v view=%q want %q/%v/%q", i, step.key, vs.Sort, vs.Descending, vs.View, step.sort, step.descending, step.view)
		}
		if ref := pickerString(p.visible()[p.Selected]["ref"]); ref != "local/w6:p6" {
			t.Fatalf("step %d (%s): selection=%q want local/w6:p6", i, step.key, ref)
		}
		if p.Selected != step.index {
			t.Fatalf("step %d (%s): index=%d want %d", i, step.key, p.Selected, step.index)
		}
	}
}

// TestSessionViewBoardRefreshKey: r keeps its board refresh semantics -
// list focus in the enhanced view, and empty query in the legacy one;
// in the search focus r is text; Ctrl+R refreshes at any time, like today.
func TestSessionViewBoardRefreshKey(t *testing.T) {
	b := NewBoardState()
	b.ViewState.Enabled = true
	b.Entries = sessionViewEntries()
	if got := b.FeedChunk("r"); got != "update" || b.Query != "" || b.ViewState.SearchFocused {
		t.Fatalf("r empty query: action=%q query=%q focused=%v", got, b.Query, b.ViewState.SearchFocused)
	}
	if got := b.FeedChunk("o"); got != "" || b.Query != "o" || !b.ViewState.SearchFocused {
		t.Fatalf("o: action=%q query=%q focused=%v", got, b.Query, b.ViewState.SearchFocused)
	}
	if got := b.FeedChunk("r"); got != "" || b.Query != "or" {
		t.Fatalf("r in the search focus: action=%q query=%q want or (text)", got, b.Query)
	}
	if got := b.FeedChunk("\x12"); got != "update" || b.Query != "or" {
		t.Fatalf("legacy ctrl+r: action=%q query=%q", got, b.Query)
	}
	if got := b.FeedChunk("\x1b[114;5u"); got != "update" || b.Query != "or" {
		t.Fatalf("enhanced ctrl+r: action=%q query=%q", got, b.Query)
	}
	b.FeedChunk("\t")
	if got := b.FeedChunk("r"); got != "update" || b.Query != "or" || b.ViewState.SearchFocused {
		t.Fatalf("r with a list-focus filter: action=%q query=%q focused=%v", got, b.Query, b.ViewState.SearchFocused)
	}
	b.FeedChunk("\t")
	if got := b.FeedChunk("\x7f"); got != "" || b.Query != "o" || !b.ViewState.SearchFocused {
		t.Fatalf("backspace: action=%q query=%q focused=%v", got, b.Query, b.ViewState.SearchFocused)
	}
	// The picker has no refresh: r is search text and Ctrl+R is ignored.
	p := NewPickerState()
	p.ViewState.Enabled = true
	p.Entries = sessionViewEntries()
	if got := p.FeedChunk("r"); got != "" || p.Query != "r" || !p.ViewState.SearchFocused {
		t.Fatalf("picker r: action=%q query=%q focused=%v", got, p.Query, p.ViewState.SearchFocused)
	}
	if got := p.FeedChunk("\x12"); got != "" || p.Query != "r" {
		t.Fatalf("picker legacy ctrl+r: action=%q query=%q", got, p.Query)
	}
	if got := p.FeedChunk("\x1b[114;5u"); got != "" || p.Query != "r" {
		t.Fatalf("picker enhanced ctrl+r: action=%q query=%q", got, p.Query)
	}
}

// TestSessionViewBackspaceSearch: backspace enters the search focus and
// edits the query. Clearing the query keeps search focused, so subsequent
// p/w/s/v letters remain text until Tab returns to the list.
func TestSessionViewBackspaceSearch(t *testing.T) {
	p := NewPickerState()
	p.ViewState.Enabled = true
	p.Entries = sessionViewEntries()
	if got := p.FeedChunk("ab"); got != "" || p.Query != "ab" || !p.ViewState.SearchFocused {
		t.Fatalf("ab: action=%q query=%q focused=%v", got, p.Query, p.ViewState.SearchFocused)
	}
	if got := p.FeedChunk("\x7f"); got != "" || p.Query != "a" || !p.ViewState.SearchFocused {
		t.Fatalf("backspace: action=%q query=%q focused=%v", got, p.Query, p.ViewState.SearchFocused)
	}
	if got := p.FeedChunk("\x7f"); got != "" || p.Query != "" || !p.ViewState.SearchFocused {
		t.Fatalf("emptying backspace: action=%q query=%q focused=%v", got, p.Query, p.ViewState.SearchFocused)
	}
	if got := p.FeedChunk("\x7f"); got != "" || p.Query != "" || !p.ViewState.SearchFocused {
		t.Fatalf("no-op backspace: action=%q query=%q focused=%v", got, p.Query, p.ViewState.SearchFocused)
	}
	// From the list focus with a surviving query, backspace enters the
	// search and edits it.
	p.FeedChunk("x")
	p.FeedChunk("\t")
	if p.ViewState.SearchFocused || p.Query != "x" {
		t.Fatalf("tab back to the list: focused=%v query=%q", p.ViewState.SearchFocused, p.Query)
	}
	if got := p.FeedChunk("\x7f"); got != "" || p.Query != "" || !p.ViewState.SearchFocused {
		t.Fatalf("backspace from the list focus: action=%q query=%q focused=%v", got, p.Query, p.ViewState.SearchFocused)
	}
	p.FeedChunk("pwsv")
	if p.Query != "pwsv" || p.ViewState.View != sessionViewTable || p.ViewState.Descending {
		t.Fatalf("letters after clearing search changed controls: query=%q state=%+v", p.Query, p.ViewState)
	}
	b := NewBoardState()
	b.ViewState.Enabled = true
	b.FeedChunk("q\x7fr")
	if b.Query != "r" || !b.ViewState.SearchFocused {
		t.Fatalf("board search lost focus after clearing: query=%q state=%+v", b.Query, b.ViewState)
	}
}

// sessionLoopLastFrame returns the last modal frame of the loop's output:
// the bytes from its absolute-home clear onward.
func sessionLoopLastFrame(screen string) string {
	if i := strings.LastIndex(screen, "\x1b[H\x1b[J"); i >= 0 {
		return screen[i:]
	}
	return screen
}

// TestSessionViewEnabledFollowsTTY exercises the interactive loops: w is
// a sort key on a TTY and literal query input on a pipe. Tab then selects
// search focus, where p/w/s are ordinary text. Assertions use visible UI.
func TestSessionViewEnabledFollowsTTY(t *testing.T) {
	local := `{"result":{"snapshot":{"workspaces":[{"workspace_id":"ws-a","label":"alpha"}],"tabs":[{"tab_id":"ws-a:t1","label":"1"}],"agents":[{"pane_id":"ws-a:p1","name":"worker","agent":"codex","agent_status":"working","cwd":"/tmp/worker"}],"panes":[{"pane_id":"ws-a:p1","workspace_id":"ws-a","tab_id":"ws-a:t1"}]}}}`
	for _, board := range []bool{false, true} {
		for _, isTTY := range []bool{false, true} {
			name := "picker pipe"
			if board {
				name = "board pipe"
			}
			if isTTY {
				name = strings.TrimSuffix(name, "pipe") + "TTY"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
					{Argv: []string{"api", "snapshot"}, Stdout: local},
					{Argv: []string{"machine", "list", "--json"}, Stdout: "[]"},
				})
				if err != nil {
					t.Fatal(err)
				}
				env := boardEnv(t, dir, herdr)
				input, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				output, err := os.CreateTemp(t.TempDir(), "session-loop-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = input.Close(); _ = writer.Close(); _ = output.Close() })
				done := make(chan int, 1)
				go func() {
					if board {
						done <- runBoardLoopWithSignals(env, platform.Current(), herdr, input, output, isTTY, make(chan os.Signal, 1))
					} else {
						done <- runPickerLoopWithSignals(env, platform.Current(), herdr, input, output, isTTY, make(chan os.Signal, 1))
					}
				}()
				waitVisible := func(needle string) string {
					t.Helper()
					deadline := time.Now().Add(10 * time.Second)
					var frame string
					for time.Now().Before(deadline) {
						data, _ := os.ReadFile(output.Name())
						frame = StripPickerControls(sessionLoopLastFrame(string(data)))
						if strings.Contains(frame, needle) {
							return frame
						}
						time.Sleep(10 * time.Millisecond)
					}
					t.Fatalf("missing visible %q:\n%s", needle, frame)
					return frame
				}
				waitVisible("worker")
				if _, err := writer.Write([]byte("w")); err != nil {
					t.Fatal(err)
				}
				if isTTY {
					waitVisible("sort Workspace")
				} else {
					waitVisible("> w")
				}
				if _, err := writer.Write([]byte("\tpws")); err != nil {
					t.Fatal(err)
				}
				if isTTY {
					waitVisible("> pws")
				} else {
					waitVisible("> wpws")
				}
				_, _ = writer.Write([]byte("\x1b"))
				_ = writer.Close()
				select {
				case code := <-done:
					if code != 0 {
						t.Fatalf("loop exit=%d", code)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("loop did not close after Esc")
				}
			})
		}
	}
}
