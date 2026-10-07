package plugin

import (
	"reflect"
	"strings"
	"testing"
)

// sessionViewEntries is the cross-machine fixture of the enhanced-view
// tests. It exercises: the local machine first, the other machines by label
// case-insensitively (linux before WINDOWS), duplicate workspace labels
// with distinct IDs (alpha w1 vs alpha w6 locally, ALPHA w3 on WINDOWS),
// a case-variant workspace label (BETA w5 vs beta w2), a status that is
// empty (zeta-four) and every named status.
func sessionViewEntries() []PickerEntry {
	return []PickerEntry{
		boardEntry("local/w1:p1", "local", "w1", "alpha", "alpha-one", "grok", "idle", ""),
		boardEntry("local/w1:p9", "local", "w1", "alpha", "alpha-two", "claude", "blocked", ""),
		boardEntry("local/w1:p0", "local", "w1", "alpha", "alpha-zero", "codex", "done", ""),
		boardEntry("local/w6:p6", "local", "w6", "alpha", "alpha-six", "claude", "idle", ""),
		boardEntry("local/w2:p2", "local", "w2", "beta", "beta-two", "codex", "working", ""),
		boardEntry("local/w5:p5", "local", "w5", "BETA", "beta-three", "grok", "idle", ""),
		boardEntry("linux/w4:p4", "linux", "w4", "zeta", "zeta-four", "codex", "", ""),
		boardEntry("WINDOWS/w3:p3", "WINDOWS", "w3", "ALPHA", "alpha-three", "claude", "blocked", ""),
	}
}

// sessionViewSeed is the publication's inserted row: a local workspace
// whose group (alpha, id w0) sorts before every existing local workspace.
func sessionViewSeed() PickerEntry {
	return boardEntry("local/w0:p7", "local", "w0", "alpha", "alpha-seed", "codex", "blocked", "")
}

func sessionRefs(list []PickerEntry) []string {
	refs := make([]string, 0, len(list))
	for _, entry := range list {
		refs = append(refs, pickerString(entry["ref"]))
	}
	return refs
}

func TestSessionViewGroupedOrder(t *testing.T) {
	// Default and p sort order: workspaces by label then identity
	// (alpha w1, alpha w6, beta w2, BETA w5), panes by name inside each.
	paneAsc := []string{"local/w1:p1", "local/w1:p9", "local/w1:p0", "local/w6:p6", "local/w2:p2", "local/w5:p5", "linux/w4:p4", "WINDOWS/w3:p3"}
	// p descending reverses only the pane order inside each workspace.
	paneDesc := []string{"local/w1:p0", "local/w1:p9", "local/w1:p1", "local/w6:p6", "local/w2:p2", "local/w5:p5", "linux/w4:p4", "WINDOWS/w3:p3"}
	// w descending reverses only the workspace-group order, per machine;
	// the within-group order stays ascending and local stays first.
	wsDesc := []string{"local/w5:p5", "local/w2:p2", "local/w6:p6", "local/w1:p1", "local/w1:p9", "local/w1:p0", "linux/w4:p4", "WINDOWS/w3:p3"}
	// s sort ranks blocked, working, idle, done; the empty status (zeta)
	// stays last of its group, and unknown values are not invented.
	statusAsc := []string{"local/w1:p9", "local/w1:p1", "local/w1:p0", "local/w6:p6", "local/w2:p2", "local/w5:p5", "linux/w4:p4", "WINDOWS/w3:p3"}
	// s descending reverses only the status rank inside each workspace.
	statusDesc := []string{"local/w1:p0", "local/w1:p1", "local/w1:p9", "local/w6:p6", "local/w2:p2", "local/w5:p5", "linux/w4:p4", "WINDOWS/w3:p3"}
	cases := []struct {
		name string
		vs   SessionViewState
		want []string
	}{
		{"default pane ascending", SessionViewState{Enabled: true, View: sessionViewTable, Sort: sessionSortPane}, paneAsc},
		{"pane ascending", SessionViewState{Enabled: true, View: sessionViewTable, Sort: sessionSortPane}, paneAsc},
		{"pane descending", SessionViewState{Enabled: true, View: sessionViewTable, Sort: sessionSortPane, Descending: true}, paneDesc},
		{"workspace ascending", SessionViewState{Enabled: true, View: sessionViewTable, Sort: sessionSortWorkspace}, paneAsc},
		{"workspace descending", SessionViewState{Enabled: true, View: sessionViewTable, Sort: sessionSortWorkspace, Descending: true}, wsDesc},
		{"status ascending", SessionViewState{Enabled: true, View: sessionViewTable, Sort: sessionSortStatus}, statusAsc},
		{"status descending", SessionViewState{Enabled: true, View: sessionViewTable, Sort: sessionSortStatus, Descending: true}, statusDesc},
		{"tree view shares the table order", SessionViewState{Enabled: true, View: sessionViewTree, Sort: sessionSortStatus}, statusAsc},
		// The legacy view keeps the caller's (insertion) order.
		{"legacy keeps insertion order", SessionViewState{}, sessionRefs(sessionViewEntries())},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewPickerState()
			s.ViewState = tc.vs
			s.Entries = sessionViewEntries()
			if got, want := sessionRefs(s.visible()), tc.want; !reflect.DeepEqual(got, want) {
				t.Fatalf("visible=%v want %v", got, want)
			}
		})
	}
}

// TestSessionViewGroupingNeverReversed: no sort or direction may move the
// local machine off the top, reorder the machine groups or split them.
func TestSessionViewGroupingNeverReversed(t *testing.T) {
	for _, sortKey := range []string{"", sessionSortPane, sessionSortWorkspace, sessionSortStatus} {
		for _, desc := range []bool{false, true} {
			for _, view := range []string{sessionViewTable, sessionViewTree} {
				s := NewPickerState()
				s.ViewState = SessionViewState{Enabled: true, View: view, Sort: sortKey, Descending: desc}
				s.Entries = sessionViewEntries()
				var blocks []string
				for _, entry := range s.visible() {
					machine := pickerString(entry["machine"])
					if len(blocks) == 0 || blocks[len(blocks)-1] != machine {
						blocks = append(blocks, machine)
					}
				}
				if !reflect.DeepEqual(blocks, []string{"local", "linux", "WINDOWS"}) {
					t.Fatalf("sort=%q desc=%v view=%q: machine blocks=%v want [local linux WINDOWS]", sortKey, desc, view, blocks)
				}
			}
		}
	}
}

// TestSessionViewSamePaneIDDifferentMachines: the same pane ID (and the
// same workspace and pane name) on two machines stays in two machine
// groups, local first - the grouping is machine scoped.
func TestSessionViewSamePaneIDDifferentMachines(t *testing.T) {
	mk := func(ref, machine, kind, status string) PickerEntry {
		return PickerEntry{
			"ref": ref, "machine": machine, "workspace_id": "w1", "workspace_label": "alpha",
			"tab_id": "w1:t1", "pane_id": "p1", "name": "worker", "kind": kind, "status": status,
		}
	}
	entries := []PickerEntry{mk("WINDOWS/p1", "WINDOWS", "claude", "idle"), mk("local/p1", "local", "codex", "working")}
	s := NewPickerState()
	s.ViewState.Enabled = true
	s.Entries = entries
	if got, want := sessionRefs(s.visible()), []string{"local/p1", "WINDOWS/p1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("picker visible=%v want %v", got, want)
	}
	b := NewBoardState()
	b.ViewState.Enabled = true
	b.Entries = entries
	if got, want := sessionRefs(b.visible()), []string{"local/p1", "WINDOWS/p1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("board visible=%v want %v", got, want)
	}
}

// TestSessionViewBoardSharesTheGroupedOrder: the board's enhanced visible
// entries are the same grouped list the picker computes (agents only).
func TestSessionViewBoardSharesTheGroupedOrder(t *testing.T) {
	p := NewPickerState()
	p.ViewState.Enabled = true
	p.Entries = sessionViewEntries()
	b := NewBoardState()
	b.ViewState.Enabled = true
	b.Entries = sessionViewEntries()
	if !reflect.DeepEqual(sessionRefs(p.visible()), sessionRefs(b.visible())) {
		t.Fatalf("picker=%v board=%v", sessionRefs(p.visible()), sessionRefs(b.visible()))
	}
}

// TestSessionViewSelectionSurvivesSortAndView: every p/w/s/v toggle keeps
// the selection on the full ref of the row it was on - the index moves
// when the list re-orders, never the row.
func TestSessionViewSelectionSurvivesSortAndView(t *testing.T) {
	s := NewPickerState()
	s.ViewState.Enabled = true
	s.Entries = sessionViewEntries()
	s.Selected = 3 // local/w6:p6 in the default pane-ascending order
	if got := pickerString(s.visible()[s.Selected]["ref"]); got != "local/w6:p6" {
		t.Fatalf("fixture selection=%q want local/w6:p6", got)
	}
	steps := []struct {
		key         string
		sort        string
		descending  bool
		view        string
		selectedRef string
		index       int
	}{
		{"w", sessionSortWorkspace, false, sessionViewTable, "local/w6:p6", 3},
		{"w", sessionSortWorkspace, true, sessionViewTable, "local/w6:p6", 2},
		{"p", sessionSortPane, false, sessionViewTable, "local/w6:p6", 3},
		{"s", sessionSortStatus, false, sessionViewTable, "local/w6:p6", 3},
		{"s", sessionSortStatus, true, sessionViewTable, "local/w6:p6", 3},
		{"p", sessionSortPane, false, sessionViewTable, "local/w6:p6", 3},
		{"v", sessionSortPane, false, sessionViewTree, "local/w6:p6", 3},
		{"v", sessionSortPane, false, sessionViewTable, "local/w6:p6", 3},
	}
	before := sessionRefs(s.visible())
	for _, step := range steps {
		if got := s.FeedChunk(step.key); got != "" {
			t.Fatalf("%s returned action %q", step.key, got)
		}
		vs := s.ViewState
		if vs.Sort != step.sort || vs.Descending != step.descending || vs.View != step.view {
			t.Fatalf("after %s: view state sort=%q desc=%v view=%q want %q/%v/%q", step.key, vs.Sort, vs.Descending, vs.View, step.sort, step.descending, step.view)
		}
		if ref := pickerString(s.visible()[s.Selected]["ref"]); ref != step.selectedRef {
			t.Fatalf("after %s: selection=%q want %s (index=%d)", step.key, ref, step.selectedRef, s.Selected)
		}
		if s.Selected != step.index {
			t.Fatalf("after %s: index=%d want %d", step.key, s.Selected, step.index)
		}
	}
	if got := sessionRefs(s.visible()); !reflect.DeepEqual(got, before) {
		t.Fatalf("the view toggle changed the visible order: %v vs %v", got, before)
	}
}

// TestSessionViewBoardSelectionSurvivesSortKeys: the board keeps the
// selected full ref through its p/w/s/v keys too.
func TestSessionViewBoardSelectionSurvivesSortKeys(t *testing.T) {
	b := NewBoardState()
	b.ViewState.Enabled = true
	b.Entries = sessionViewEntries()
	b.Selected = 0 // local/w1:p1 in the default order
	for _, key := range []string{"s", "s", "w", "p", "v"} {
		before := pickerString(b.visible()[b.Selected]["ref"])
		if got := b.FeedChunk(key); got != "" {
			t.Fatalf("%s returned action %q", key, got)
		}
		if ref := pickerString(b.visible()[b.Selected]["ref"]); ref != before {
			t.Fatalf("after %s: selection=%q want %s", key, ref, before)
		}
	}
	if b.ViewState.View != sessionViewTree {
		t.Fatalf("view=%q want tree after the v toggle", b.ViewState.View)
	}
}

// TestSessionViewSelectionPreservedThroughPublication: a discovery
// publication that inserts an earlier workspace re-orders the enhanced
// list; the selection follows the full ref, and the view state, query and
// decoder state survive the publication. The legacy selection keeps the
// numeric index, as before.
func TestSessionViewSelectionPreservedThroughPublication(t *testing.T) {
	live := NewPickerState()
	live.ViewState.Enabled = true
	live.Entries = sessionViewEntries()
	live.Query = "alpha" // five rows; index 4 is WINDOWS/w3:p3
	live.Selected = 4
	live.TerminalKeys.EscPending = true
	update := NewPickerState()
	update.Entries = append([]PickerEntry{sessionViewSeed()}, sessionViewEntries()...)
	applyPickerUpdate(live, update)
	if ref := pickerString(live.visible()[live.Selected]["ref"]); ref != "WINDOWS/w3:p3" {
		t.Fatalf("selection after the publication=%q want WINDOWS/w3:p3 (visible=%v)", ref, sessionRefs(live.visible()))
	}
	if !live.ViewState.Enabled || live.ViewState.View != sessionViewTable || live.ViewState.Sort != sessionSortPane {
		t.Fatalf("the publication reset the view state: %+v", live.ViewState)
	}
	if live.Query != "alpha" {
		t.Fatalf("query after the publication=%q want alpha", live.Query)
	}
	if !live.TerminalKeys.EscPending {
		t.Fatal("the publication reset the raw-input decoder state")
	}
	// Legacy control: the numeric index - not the ref - is kept.
	legacy := NewPickerState()
	legacy.Entries = sessionViewEntries()
	legacy.Query = "alpha"
	legacy.Selected = 4
	applyPickerUpdate(legacy, update)
	if got := pickerString(legacy.visible()[legacy.Selected]["ref"]); got != "local/w6:p6" {
		t.Fatalf("legacy selection after the publication=%q want local/w6:p6 (index kept)", got)
	}
}

// TestSessionViewNavTargetFreeze: while a navigation is pending the frozen
// target never changes - not through a publication that re-orders the
// list, not through a sort - and Enter / Ctrl+Enter stay gated.
func TestSessionViewNavTargetFreeze(t *testing.T) {
	s := NewPickerState()
	s.ViewState.Enabled = true
	s.Entries = sessionViewEntries()
	s.Selected = 1 // local/w1:p9
	if got := s.FeedChunk("\x1b[13;5u"); got != "navigate" || !s.NavPending {
		t.Fatalf("ctrl+enter action=%q pending=%v", got, s.NavPending)
	}
	target := pickerString(s.NavTarget["ref"])
	if target != "local/w1:p9" {
		t.Fatalf("nav target=%q want local/w1:p9", target)
	}
	update := NewPickerState()
	update.Entries = append([]PickerEntry{sessionViewSeed()}, sessionViewEntries()...)
	applyPickerUpdate(s, update)
	if !s.NavPending || pickerString(s.NavTarget["ref"]) != target {
		t.Fatalf("publication changed the frozen target: pending=%v target=%v", s.NavPending, s.NavTarget)
	}
	// The selection followed the ref while the target stayed frozen.
	if ref := pickerString(s.visible()[s.Selected]["ref"]); ref != target {
		t.Fatalf("selection after the publication=%q want the frozen ref %s", ref, target)
	}
	if got := s.ApplyKey("w"); got != "" {
		t.Fatalf("sort key while pending=%q", got)
	}
	if !s.NavPending || pickerString(s.NavTarget["ref"]) != target {
		t.Fatalf("sort changed the frozen target: pending=%v target=%v", s.NavPending, s.NavTarget)
	}
	if got := s.ApplyKey("enter"); got != "" || s.Copied != nil {
		t.Fatalf("enter while pending: action=%q copied=%v", got, s.Copied)
	}
	if got := s.ApplyKey("ctrl-enter"); got != "" {
		t.Fatalf("second ctrl-enter while pending=%q", got)
	}

	// The board freezes its target the same way, across a mid-generation
	// refresh that re-orders the visible list.
	b := NewBoardState()
	b.ViewState.Enabled = true
	b.Entries = sessionViewEntries()
	b.Selected = 3 // pin local/w6:p6
	first := NewBoardState()
	first.Entries = append([]PickerEntry{sessionViewSeed()}, sessionViewEntries()...)
	first.FinishedMachines = []string{"local", "linux", "WINDOWS"}
	applyBoardUpdate(b, first)
	if got := b.ApplyKey("ctrl-enter"); got != "navigate" || !b.NavPending {
		t.Fatalf("board ctrl+enter action=%q pending=%v", got, b.NavPending)
	}
	if got := pickerString(b.NavTarget["ref"]); got != "local/w6:p6" {
		t.Fatalf("board nav target=%q want the pinned ref local/w6:p6", got)
	}
	second := NewBoardState()
	second.Entries = sessionViewEntries() // the seed row is gone this generation
	second.FinishedMachines = []string{"local", "linux", "WINDOWS"}
	applyBoardUpdate(b, second)
	if !b.NavPending || pickerString(b.NavTarget["ref"]) != "local/w6:p6" {
		t.Fatalf("board refresh changed the frozen target: pending=%v target=%v", b.NavPending, b.NavTarget)
	}
	if got := b.ApplyKey("enter"); got != "" || b.Copied != nil {
		t.Fatalf("board enter while pending: action=%q copied=%v", got, b.Copied)
	}
	if got := b.ApplyKey("ctrl-enter"); got != "" {
		t.Fatalf("board second ctrl-enter while pending=%q", got)
	}
}

// TestSessionViewBoardMissingPinOnRefresh: with the enhanced view on, a
// board refresh whose pinned ref is missing shows no cursor and suppresses
// copy and navigation until an arrow picks a new ref.
func TestSessionViewBoardMissingPinOnRefresh(t *testing.T) {
	s := NewBoardState()
	s.ViewState.Enabled = true
	s.Entries = []PickerEntry{
		boardEntry("local/a", "local", "w", "w", "a", "codex", "idle", "a"),
		boardEntry("middle/b", "middle", "w", "w", "b", "codex", "idle", "b"),
		boardEntry("slow/c", "slow", "w", "w", "c", "codex", "idle", "c"),
	}
	s.Selected = 1 // middle/b
	u := NewBoardState()
	u.Loading = 1
	u.Entries = []PickerEntry{s.Entries[0]}
	u.FinishedMachines = []string{"local", "middle"}
	applyBoardUpdate(s, u)
	if !s.ViewState.Enabled {
		t.Fatal("the publication reset the enhanced view")
	}
	if !s.pinnedMissing() {
		t.Fatal("the fixture must be in the pinned-missing state")
	}
	if got := s.ApplyKey("enter"); got != "" || s.Copied != nil || s.Exit != "" {
		t.Fatalf("enter with the missing pin: action=%q copied=%v exit=%q", got, s.Copied, s.Exit)
	}
	if got := s.ApplyKey("ctrl-enter"); got != "" || s.NavPending {
		t.Fatalf("ctrl+enter with the missing pin: action=%q pending=%v", got, s.NavPending)
	}
	if got := boardCursorLines(RenderBoard(s, 80)); got != 0 {
		t.Fatalf("the missing pin draws %d cursor rows, want none", got)
	}
	render := RenderBoard(s, 80)
	if !strings.HasSuffix(strings.TrimSuffix(render, "\n"), "a sessão selecionada saiu desta carga; escolha outra com ↑/↓") {
		t.Fatalf("the pinned-missing footer is missing:\n%s", render)
	}
	// An arrow re-pins a visible row and c copies exactly that ref.
	if got := s.ApplyKey("down"); got != "" {
		t.Fatalf("down returned %q", got)
	}
	if ref := pickerString(s.visible()[s.Selected]["ref"]); ref != "slow/c" {
		t.Fatalf("down re-pinned %q want slow/c (the last visible row)", ref)
	}
	if got := s.ApplyKey("c"); got != "copy" || pickerString(s.LastEntry["ref"]) != "slow/c" || s.Exit != "" {
		t.Fatalf("c after the re-pin: action=%q copied=%v exit=%q", got, s.LastEntry, s.Exit)
	}
}

// TestSessionViewQueryClampsSelection: a query change may clamp the
// selection index, as today - it never re-expands a cleared query.
func TestSessionViewQueryClampsSelection(t *testing.T) {
	s := NewPickerState()
	s.ViewState.Enabled = true
	s.Entries = sessionViewEntries()
	s.Selected = 7
	if got := s.FeedChunk("q"); got != "" || s.Query != "q" {
		t.Fatalf("search action=%q query=%q", got, s.Query)
	}
	if s.Selected != 0 {
		t.Fatalf("selection after the unmatched query=%d want 0 (clamped)", s.Selected)
	}
	if got := s.FeedChunk("\x7f"); got != "" || s.Query != "" {
		t.Fatalf("backspace action=%q query=%q", got, s.Query)
	}
	if s.Selected != 0 {
		t.Fatalf("selection after the cleared query=%d want 0 (no re-expand)", s.Selected)
	}
}
