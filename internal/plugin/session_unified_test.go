package plugin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// unifiedTestEntries is the mixed session registry: agent panes (the kind
// carries the CLI name) and terminal panes (the unknown/empty/"-" kinds),
// so the unified display and the filters have both worlds.
func unifiedTestEntries() []PickerEntry {
	return []PickerEntry{
		boardEntry("local/a1", "local", "ws-a", "alpha", "orchestrator", "claude", "working", "orchestrator: ship"),
		boardEntry("local/a2", "local", "ws-a", "alpha", "implementer-1", "codex", "idle", "implementer: task"),
		boardEntry("local/a3", "local", "ws-a", "alpha", "researcher", "grok", "blocked", "research: perf"),
		boardEntry("local/a4", "local", "ws-a", "alpha", "inspector", "pi", "done", "inspect: log"),
		boardEntry("local/a5", "local", "ws-b", "beta", "implementer-2", "claude", "working", ""),
		boardEntry("local/a6", "local", "ws-b", "beta", "planner", "codex", "idle", "plan: next"),
		boardEntry("local/a7", "local", "ws-b", "beta", "tasker", "grok", "", "tasks: queue"),
		boardEntry("local/a8", "local", "ws-b", "beta", "reviewer", "pi", "done", "review: diff"),
		boardEntry("local/a9", "local", "ws-c", "gamma", "scouter", "claude", "working", "scout: api"),
		boardEntry("local/a10", "local", "ws-c", "gamma", "designer", "codex", "blocked", "design: ui"),
		boardEntry("local/t1", "local", "ws-a", "alpha", "build-watch", "", "", "tail: build"), // empty kind: terminal
		boardEntry("local/t2", "local", "ws-b", "beta", "log-tail", "-", "", ""),               // "-" kind: terminal
		boardEntry("local/t3", "local", "ws-c", "gamma", "metrics", "host", "", ""),            // real kind: agent pane
	}
}

func unifiedTestState() *BoardState {
	s := NewBoardState()
	s.Entries = unifiedTestEntries()
	s.ViewState.Enabled = true
	return s
}

func TestUnifiedKindAndAgentClassification(t *testing.T) {
	cases := []struct {
		kind string
		want bool // sessionAgentOf
	}{
		{"claude", true}, {"codex", true}, {"pi", true}, {"host", true},
		{"UNKNOWN", true}, // a real kind is an agent; only the literal string "unknown" is not
		{"", false}, {"-", false}, {"unknown", false},
		{"\x1b[31m-\x1b[0m", false}, // hostile kind: controls stripped, "-" means terminal
		{"\x1b[31munknown\x1b[0m", false},
		{"\x00", false}, // NUL-stripped to empty: terminal
	}
	for _, item := range cases {
		entries := []PickerEntry{boardEntry("local/x", "local", "ws", "ws", "n", item.kind, "working", "")}
		if got := sessionAgentOf(entries[0]); got != item.want {
			t.Fatalf("sessionAgentOf(%q)=%v want %v", item.kind, got, item.want)
		}
	}
	// The table cell shows the kind for agent panes and "terminal" for the
	// unknown/empty/"-" kinds.
	if got, want := sessionAgentLabel(boardEntry("local/x", "local", "ws", "ws", "n", "claude", "working", "")), "claude"; got != want {
		t.Fatalf("agent label kind=%q got %q want %q", "claude", got, want)
	}
	if got, want := sessionAgentLabel(boardEntry("local/x", "local", "ws", "ws", "n", "", "working", "")), "terminal"; got != want {
		t.Fatalf("agent label empty kind got %q want %q", got, want)
	}
	if got, want := sessionAgentLabel(boardEntry("local/x", "local", "ws", "ws", "n", "-", "working", "")), "terminal"; got != want {
		t.Fatalf("agent label '-' kind got %q want %q", got, want)
	}
	if got, want := sessionAgentLabel(boardEntry("local/x", "local", "ws", "ws", "n", "unknown", "working", "")), "terminal"; got != want {
		t.Fatalf("agent label 'unknown' kind got %q want %q", got, want)
	}
	if got, want := sessionAgentLabel(boardEntry("local/x", "local", "ws", "ws", "n", "UNKNOWN", "working", "")), "UNKNOWN"; got != want {
		t.Fatalf("agent label 'UNKNOWN' kind got %q want %q", got, want)
	}
	// The unified display shows every pane: the default scope keeps the
	// terminal panes, and the agents scope drops exactly them.
	entries := unifiedTestEntries()
	all := sessionFilterCandidates(entries, SessionViewState{Enabled: true})
	if len(all) != len(entries) {
		t.Fatalf("default scope shows %d of %d panes", len(all), len(entries))
	}
	agents := sessionFilterCandidates(entries, SessionViewState{Enabled: true, Scope: sessionScopeAgents})
	if len(agents) != 11 {
		t.Fatalf("agents scope shows %d panes, want 11", len(agents))
	}
	for _, entry := range agents {
		if !sessionAgentOf(entry) {
			t.Fatalf("agents scope kept the terminal pane %q", pickerString(entry["ref"]))
		}
	}
	terminals := sessionFilterCandidates(entries, SessionViewState{Enabled: true, Scope: sessionScopeTerminals})
	if len(terminals) != 2 {
		t.Fatalf("terminals scope shows %d panes, want 2", len(terminals))
	}
}

func TestUnifiedStatusFilterMatchesPinnedRows(t *testing.T) {
	s := unifiedTestState()
	want := map[string]string{
		sessionStatusAll:     "13",
		sessionStatusWorking: "3",
		sessionStatusBlocked: "2",
		sessionStatusIdle:    "2",
		sessionStatusDone:    "2",
		sessionStatusUnknown: "4", // the status-less panes (tasker, build-watch, log-tail, metrics)
	}
	for status, n := range want {
		s.ViewState.Status = status
		if got := fmt.Sprint(len(s.visible())); got != n {
			t.Fatalf("status %q shows %s, want %s", status, got, n)
		}
	}
}

func TestUnifiedScopeStatusCyclesAndSearchFocus(t *testing.T) {
	s := unifiedTestState()
	// List focus: f cycles the scope, t the status, and neither enters the
	// search.
	s.ApplyKey("f")
	if s.ViewState.Scope != sessionScopeAgents {
		t.Fatalf("f first=%q want %q", s.ViewState.Scope, sessionScopeAgents)
	}
	s.ApplyKey("f")
	if s.ViewState.Scope != sessionScopeTerminals {
		t.Fatalf("f second=%q want %q", s.ViewState.Scope, sessionScopeTerminals)
	}
	s.ApplyKey("f")
	if s.ViewState.Scope != sessionScopeAll {
		t.Fatalf("f third=%q want %q", s.ViewState.Scope, sessionScopeAll)
	}
	if s.Query != "" {
		t.Fatalf("f leaked into the search: %q", s.Query)
	}
	statuses := []string{}
	for i := 0; i < 6; i++ {
		s.ApplyKey("t")
		statuses = append(statuses, s.ViewState.Status)
	}
	if want := []string{sessionStatusWorking, sessionStatusBlocked, sessionStatusIdle, sessionStatusDone, sessionStatusUnknown, sessionStatusAll}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("t cycle=%#v want %#v", statuses, want)
	}
	if s.Query != "" {
		t.Fatalf("t leaked into the search: %q", s.Query)
	}
	// Search focus: f/t are text like every other printable.
	s.ApplyKey("tab")
	s.ApplyKey("f")
	s.ApplyKey("t")
	if s.Query != "ft" {
		t.Fatalf("search focus f/t query=%q want %q", s.Query, "ft")
	}
	// Legacy (disabled view): f/t are plain search text.
	legacy := NewBoardState()
	legacy.Entries = unifiedTestEntries()
	legacy.ApplyKey("f")
	if legacy.Query != "f" || legacy.ViewState.Scope != "" {
		t.Fatalf("legacy f query=%q scope=%q; both must stay text", legacy.Query, legacy.ViewState.Scope)
	}
}

func TestUnifiedFiltersCombineAndCountsReflectUnified(t *testing.T) {
	// Text query + scope + status combine.
	s := unifiedTestState()
	s.Query = "watch" // only build-watch (terminal t1) matches the name
	s.ApplyKey("f")   // agents
	if got := len(s.visible()); got != 0 {
		t.Fatalf("query+agents scope shows %d panes, want 0 (the only match is a terminal)", got)
	}
	// Counts reflect the unified sessions: Total is the scope/status
	// candidates before the query; the totals line the visible unified
	// sessions with their mixed noun.
	s = unifiedTestState()
	opts := unifiedSessionOptions(s, "")
	if opts.Total != 13 {
		t.Fatalf("unfiltered Total=%d want 13 (every pane)", opts.Total)
	}
	if opts.Totals != "13 sessions · 3 working · 2 blocked · 2 idle · 2 done" {
		t.Fatalf("mixed totals=%q want the unified noun and per-status counts", opts.Totals)
	}
	// An all-agent registry keeps the legacy noun.
	agentOnly := NewBoardState()
	agentOnly.Entries = unifiedTestEntries()[:8]
	agentOnly.ViewState.Enabled = true
	opt := unifiedSessionOptions(agentOnly, "")
	if strings.Contains(opt.Totals, "sessions") {
		t.Fatalf("all-agent totals=%q must keep the agents noun", opt.Totals)
	}
	// The scope filter narrows the candidates before the query.
	s.ViewState.Scope = sessionScopeTerminals
	opts = unifiedSessionOptions(s, "")
	if opts.Total != 2 {
		t.Fatalf("terminals scope Total=%d want 2", opts.Total)
	}
	// The footer discloses the active filters; a fresh state's default
	// frame still shows them, at the default all.
	frame := renderSessionPopup(opts, 80, 24)
	if !strings.Contains(frame, "· f Terminals") {
		t.Fatalf("footer missing the active scope filter:\n%s", frame)
	}
	fresh := unifiedTestState()
	def := renderSessionPopup(unifiedSessionOptions(fresh, ""), 80, 24)
	if !strings.Contains(def, "· f All") || !strings.Contains(def, "· t All") {
		t.Fatalf("default frame must disclose the default filters:\n%s", def)
	}
}

func TestUnifiedFilterPreservesSelectedRef(t *testing.T) {
	s := unifiedTestState()
	// The board order (workspace groups, name) puts local/a5 at index 5.
	s.Selected = 5
	s.selectedRef = "local/a5"
	s.ApplyKey("f") // agents scope: the terminal rows drop, a5 stays by ref
	if s.Selected != 4 || s.selectedRef != "local/a5" {
		t.Fatalf("agents scope moved the selection to %d/%q want 4/local/a5", s.Selected, s.selectedRef)
	}
	s.ApplyKey("f") // terminals scope: a5 leaves -> clamped, ref re-pinned
	if got := s.selectedRef; got == "local/a5" {
		t.Fatalf("terminal scope kept the dropped ref %q", got)
	}
	if s.Selected < 0 || s.Selected >= len(s.visible()) {
		t.Fatalf("clamped selection %d out of %d", s.Selected, len(s.visible()))
	}
	if s.Copied != nil {
		t.Fatalf("filter change copied something")
	}
	// The per-change re-pin: each explicit filter change keeps the row
	// selected before the change when it is still visible, and re-pins the
	// clamped row when it left the list. Select local/a6 (idle) and cycle
	// through every status back to all.
	s = unifiedTestState()
	s.Selected = 7 // local/a6 (idle)
	s.selectedRef = "local/a6"
	wantTrace := []string{"local/a9", "local/a10", "local/a6", "local/a8", "local/t2", "local/t2"}
	for i, wantRef := range wantTrace {
		s.ApplyKey("t")
		if s.selectedRef != wantRef {
			t.Fatalf("t cycle step %d re-pinned %q want %q", i+1, s.selectedRef, wantRef)
		}
	}
	// A row still visible keeps the cursor exactly: from the idle filter,
	// the cursor row is local/a6.
	s = unifiedTestState()
	s.Selected = 7 // local/a6 (idle)
	s.selectedRef = "local/a6"
	s.ApplyKey("t") // working: a6 drops, re-pins the clamped row
	s.ApplyKey("t") // blocked: re-pins the clamped row
	s.ApplyKey("t") // idle: the clamped row is local/a6 again
	if s.selectedRef != "local/a6" {
		t.Fatalf("idle step re-pinned %q want local/a6", s.selectedRef)
	}
}

func TestUnifiedPageKeysMeasureTheSharedRenderer(t *testing.T) {
	s := unifiedTestState()
	s.FrameWidth, s.FrameHeight = 80, 12
	// The table page is the shared renderer viewport math for the frame:
	// the row window minus the fixed column header (the detail slot and
	// status rows are accounted by the same math the renderer uses).
	rowsCap, _ := sessionFrameCaps(unifiedSessionOptions(s, ""), 80, 12)
	wantTable := sessionSelectableWindow(unifiedSessionOptions(s, ""), 80, 12)
	if rowsCap <= 0 || wantTable != rowsCap-1 || wantTable < 1 || wantTable > len(s.visible()) {
		t.Fatalf("table page for 80x12=%d (rowsCap %d, %d entries), want rowsCap-1 within [1,%d]",
			wantTable, rowsCap, len(s.visible()), len(s.visible()))
	}
	s.Selected = 0
	s.selectedRef = "local/a1"
	s.ApplyKey("page-down")
	if s.Selected != wantTable {
		t.Fatalf("page-down from 0=%d want %d", s.Selected, wantTable)
	}
	if got, want := s.selectedRef, pickerString(s.visible()[wantTable]["ref"]); got != want {
		t.Fatalf("page-down re-pinned %q want %q (explicit movement)", got, want)
	}
	// Repeated paging at the bound stays clamped, no implicit move past.
	for i := 0; i < 4; i++ {
		s.ApplyKey("page-down")
	}
	if s.Selected != len(s.visible())-1 {
		t.Fatalf("page-down at the bound=%d want last", s.Selected)
	}
	for i := 0; i < 4; i++ {
		s.ApplyKey("page-up")
	}
	if s.Selected != 0 {
		t.Fatalf("page-up at the bound=%d want 0", s.Selected)
	}
	// Tiny frame: at least one entry per page.
	s.FrameWidth, s.FrameHeight = 40, 3
	if page := sessionSelectableWindow(unifiedSessionOptions(s, ""), 40, 3); page != 1 {
		t.Fatalf("tiny frame page=%d want 1", page)
	}
	// Tree view: the page accounts for the group headings (it fits the
	// frame and is at least one).
	s.ViewState.View = "tree"
	if page := sessionSelectableWindow(unifiedSessionOptions(s, ""), 80, 12); page < 1 || page > len(s.visible()) {
		t.Fatalf("tree page for 80x12=%d, want within [1,%d]", page, len(s.visible()))
	}
	// A pinned-missing page key re-pins the edge, never an implicit
	// neighbor.
	s2 := unifiedTestState()
	s2.FrameWidth, s2.FrameHeight = 80, 12
	s2.refreshing = true
	s2.selectedRef = "gone"
	s2.ApplyKey("page-up")
	if s2.Selected != 0 || s2.selectedRef != "local/t1" {
		t.Fatalf("page-up missing pin=%d/%q want 0/local/t1 (the first visible row)", s2.Selected, s2.selectedRef)
	}
	s3 := unifiedTestState()
	s3.FrameWidth, s3.FrameHeight = 80, 12
	s3.refreshing = true
	s3.selectedRef = "gone"
	s3.ApplyKey("page-down")
	if s3.Selected != len(s3.visible())-1 || s3.selectedRef != "local/a9" {
		t.Fatalf("page-down missing pin=%d/%q want last/local/a9", s3.Selected, s3.selectedRef)
	}
}

func TestUnifiedPageKeysRawAndKittyEncodings(t *testing.T) {
	s := unifiedTestState()
	s.FrameWidth, s.FrameHeight = 80, 12
	page := sessionSelectableWindow(unifiedSessionOptions(s, ""), 80, 12)
	s.Selected = 4
	s.ApplyKey("page-up")
	if s.Selected != 0 {
		t.Fatalf("page-up from 4 with page %d=%d want 0 (clamped)", page, s.Selected)
	}
	// The standard page keys (CSI 5~ / CSI 6~, the kitty protocol's
	// functional-key codes for PageUp/PageDown) drive the same movement.
	raw := unifiedTestState()
	raw.FrameWidth, raw.FrameHeight = 80, 12
	raw.FeedChunk("\x1b[5~") // page-up: clamped to the first row
	if raw.Selected != 0 {
		t.Fatalf("raw 5~=%d want 0", raw.Selected)
	}
	raw.FeedChunk("\x1b[6~") // page-down
	if raw.Selected != page {
		t.Fatalf("raw 6~=%d want %d", raw.Selected, page)
	}
	raw.FeedChunk("\x1b[6~")
	if raw.Selected != min(2*page, len(raw.visible())-1) {
		t.Fatalf("raw 6~ x2=%d want %d", raw.Selected, min(2*page, len(raw.visible())-1))
	}
	raw.FeedChunk("\x1b[6~") // past the bound: clamped
	if raw.Selected != len(raw.visible())-1 {
		t.Fatalf("raw 6~ at the bound=%d want last (%d)", raw.Selected, len(raw.visible())-1)
	}
	// The extended forms carry the one-based modifier field and the
	// :event field; presses (1) and repeats (2) page, releases (3) do not.
	mods := unifiedTestState()
	mods.FrameWidth, mods.FrameHeight = 80, 12
	for _, seq := range []string{"\x1b[6;1~", "\x1b[6;2~", "\x1b[6;5~", "\x1b[6;8~"} {
		mods.Selected = 0
		mods.FeedChunk(seq)
		if mods.Selected != page {
			t.Fatalf("extended page-down %s=%d want %d", seq, mods.Selected, page)
		}
	}
	for _, seq := range []string{"\x1b[6;1:1~", "\x1b[6;2:2~", "\x1b[6;4:1~"} {
		mods.Selected = 0
		mods.FeedChunk(seq)
		if mods.Selected != page {
			t.Fatalf("extended page-down %s=%d want %d (press/repeat page)", seq, mods.Selected, page)
		}
	}
	mid := unifiedTestState()
	mid.FrameWidth, mid.FrameHeight = 80, 12
	mid.Selected = page
	for _, seq := range []string{"\x1b[5;1~", "\x1b[5;3~", "\x1b[5;2:1~"} {
		mid.FeedChunk(seq)
		if mid.Selected != 0 {
			t.Fatalf("extended page-up %s=%d want 0", seq, mid.Selected)
		}
	}
	for _, seq := range []string{"\x1b[5;1:3~", "\x1b[6;4:3~"} {
		mid.FeedChunk(seq)
		if mid.Selected != 0 {
			t.Fatalf("released page key %s=%d want 0 (releases are ignored)", seq, mid.Selected)
		}
	}
	// Malformed modifier/event fields and the non-page keys never page: 1
	// is Home, and there is no 1;N application form for the page keys.
	malformed := unifiedTestState()
	malformed.FrameWidth, malformed.FrameHeight = 80, 12
	for _, seq := range []string{
		"\x1b[5;0~",   // modifier 0 is not one-based
		"\x1b[5;9~",   // beyond the 8-way modifier
		"\x1b[5;1:0~", // event 0 is not press/repeat/release
		"\x1b[5;1:4~", // event 4 is unknown
		"\x1b[5;1:x~", // non-numeric event
		"\x1b[1;5~",   // 1 is Home, never a page key
		"\x1b[1;6~",   // no 1;N application form for the page keys
		"\x1b[15~",    // the other tilde keys (End, F5...)
		"\x1b[7~",     // F1
	} {
		malformed.FeedChunk(seq)
	}
	if malformed.Selected != 0 || malformed.Query != "" {
		t.Fatalf("non-page tilde keys moved=%d query=%q", malformed.Selected, malformed.Query)
	}
	// The kitty protocol has no other page mapping: 73 is the letter I,
	// unmodified it is typed, modified it is ignored.
	letter := unifiedTestState()
	letter.FeedChunk("\x1b[73u") // CSI 73u: the literal I
	if letter.Query != "I" {
		t.Fatalf("kitty 73u typed %q want I", letter.Query)
	}
	letter.FeedChunk("\x1b[73;2u") // shift+I: a modified text key, ignored
	if letter.Query != "I" || letter.Selected != 0 {
		t.Fatalf("modified 73u typed %q / moved %d (want I / 0)", letter.Query, letter.Selected)
	}
	// A hostile CSI must not leak into the search.
	leak := unifiedTestState()
	leak.FrameWidth, leak.FrameHeight = 80, 12
	leak.FeedChunk("\x1b[99999999999999999999;99999999999999999999;99999999999999999999")
	if leak.Query != "" {
		t.Fatalf("oversized CSI leaked into the query: %q", leak.Query)
	}
	if leak.Selected != 0 {
		t.Fatalf("oversized CSI moved the selection to %d", leak.Selected)
	}
}

func TestUnifiedWheelSGRMoveAndIgnore(t *testing.T) {
	s := unifiedTestState()
	s.FrameWidth, s.FrameHeight = 80, 12
	// The SGR report is CSI < Cb ; Cx ; Cy M (button first, then the
	// positive column and row): 65 = wheel down, 64 = wheel up. Three
	// entries per vertical notch.
	s.FeedChunk("\x1b[<65;4;7M") // down, the xterm wheel-down example
	if s.Selected != 3 {
		t.Fatalf("wheel down=%d want 3", s.Selected)
	}
	s.FeedChunk("\x1b[<64;1;1M") // up
	if s.Selected != 0 {
		t.Fatalf("wheel up=%d want 0", s.Selected)
	}
	// The Shift/Ctrl/Meta modifier bits (4/8/16) are valid and do not
	// change the vertical movement: one report moves exactly 3.
	for _, seq := range []string{
		"\x1b[<65;1;1M", "\x1b[<69;1;1M", "\x1b[<73;1;1M", "\x1b[<81;1;1M",
		"\x1b[<77;1;1M", "\x1b[<85;1;1M", "\x1b[<89;1;1M", "\x1b[<93;1;1M",
	} {
		m := unifiedTestState()
		m.FrameWidth, m.FrameHeight = 80, 12
		m.FeedChunk(seq)
		if m.Selected != 3 {
			t.Fatalf("wheel down with modifiers %s=%d want 3 (exactly one notch)", seq, m.Selected)
		}
	}
	for _, seq := range []string{
		"\x1b[<64;1;1M", "\x1b[<68;1;1M", "\x1b[<72;1;1M", "\x1b[<80;1;1M",
		"\x1b[<76;1;1M", "\x1b[<84;1;1M", "\x1b[<88;1;1M", "\x1b[<92;1;1M",
	} {
		m := unifiedTestState()
		m.FrameWidth, m.FrameHeight = 80, 12
		m.FeedChunk(seq)
		if m.Selected != 0 {
			t.Fatalf("wheel up with modifiers %s=%d want 0", seq, m.Selected)
		}
	}
	// A report split across several chunks still moves exactly once.
	chunked := unifiedTestState()
	chunked.FrameWidth, chunked.FrameHeight = 80, 12
	chunked.FeedChunk("\x1b[<6")
	chunked.FeedChunk("5;4;7")
	chunked.FeedChunk("M")
	if chunked.Selected != 3 {
		t.Fatalf("chunked wheel report=%d want 3", chunked.Selected)
	}
	// Horizontal wheels, clicks, releases, the motion/unknown bits and
	// invalid/zero/negative/overflowing coordinates do nothing.
	for _, seq := range []string{
		"\x1b[<66;4;7M", "\x1b[<67;4;7M", "\x1b[<70;4;7M", "\x1b[<75;4;7M", // horizontal wheel
		"\x1b[<0;4;7M", "\x1b[<1;4;7M", "\x1b[<2;4;7M", "\x1b[<3;4;7M", "\x1b[<4;4;7M", "\x1b[<6;4;7M", // clicks
		"\x1b[<96;4;7M", "\x1b[<97;4;7M", // motion bit 32 with the wheel
		"\x1b[<192;4;7M", "\x1b[<193;4;7M", "\x1b[<320;4;7M", // unknown bit 128 / oversized button
		"\x1b[<65;4;7m", "\x1b[<0;4;7m", "\x1b[<66;4;7m", // releases (m)
		"\x1b[<65;0;7M", "\x1b[<65;7;0M", // zero coordinates
		"\x1b[<65;-1;7M", "\x1b[<65;4;-7M", // negative coordinates
		"\x1b[<65;99999999999999999999;7M",                                              // overflowing coordinate
		"\x1b[<;4;7M", "\x1b[<65;;7M", "\x1b[<65;4M", "\x1b[<65;4;7;9M", "\x1b[<65;7;M", // empty fields / malformed
		"\x1b[65;4;7M",  // no '<': not an SGR report
		"\x1b[<65;4;7z", // wrong final byte
	} {
		m := unifiedTestState()
		m.FrameWidth, m.FrameHeight = 80, 12
		m.FeedChunk(seq)
		if m.Selected != 0 || m.Query != "" {
			t.Fatalf("ignored report %q moved to %d / query %q", seq, m.Selected, m.Query)
		}
	}
	// An X10 report (CSI M plus three bytes) is consumed without leaking
	// the coordinates into the search.
	x10 := unifiedTestState()
	x10.FeedChunk("\x1b[M" + string([]byte{32 + 0, 32 + 66, 32 + 67}))
	x10.FeedChunk("q")
	if x10.Query != "q" || x10.Selected != 0 {
		t.Fatalf("X10 report leaked: query=%q selected=%d (want q/0)", x10.Query, x10.Selected)
	}
	// Clamped, re-pinned, no copy/focus, no implicit move.
	s = unifiedTestState()
	s.FrameWidth, s.FrameHeight = 80, 12
	for i := 0; i < 10; i++ {
		s.FeedChunk("\x1b[<65;1;1M")
	}
	if s.Selected != len(s.visible())-1 {
		t.Fatalf("wheel at the bound=%d want last", s.Selected)
	}
	if s.selectedRef != "local/a9" {
		t.Fatalf("wheel re-pinned %q want local/a9 (explicit movement)", s.selectedRef)
	}
	if s.Copied != nil || s.NavPending || s.Exit != "" {
		t.Fatalf("wheel copied/focused/exited: %v/%v/%q", s.Copied, s.NavPending, s.Exit)
	}
	// A missing pin re-pins the edge instead of an implicit neighbor.
	s2 := unifiedTestState()
	s2.FrameWidth, s2.FrameHeight = 80, 12
	s2.refreshing = true
	s2.selectedRef = "gone"
	s2.FeedChunk("\x1b[<65;1;1M")
	if s2.Selected != len(s2.visible())-1 || s2.selectedRef != "local/a9" {
		t.Fatalf("wheel missing pin down=%d/%q want last/local/a9", s2.Selected, s2.selectedRef)
	}
	s3 := unifiedTestState()
	s3.FrameWidth, s3.FrameHeight = 80, 12
	s3.refreshing = true
	s3.selectedRef = "gone"
	s3.FeedChunk("\x1b[<64;1;1M")
	if s3.Selected != 0 || s3.selectedRef != "local/t1" {
		t.Fatalf("wheel missing pin up=%d/%q want 0/local/t1 (the first visible row)", s3.Selected, s3.selectedRef)
	}
	// A hostile oversized SGR report is dropped and the decoder recovers.
	s4 := unifiedTestState()
	s4.FrameWidth, s4.FrameHeight = 80, 12
	huge := "\x1b[<"
	for i := 0; i < 200; i++ {
		huge += "9;"
	}
	huge += "65M"
	s4.FeedChunk(huge)
	if s4.Selected != 0 || s4.Query != "" {
		t.Fatalf("oversized SGR=%d/%q want no movement or leak", s4.Selected, s4.Query)
	}
	s4.FeedChunk("\x1b[B") // the next input still decodes
	if s4.Selected != 1 {
		t.Fatalf("after oversized SGR, down=%d want 1", s4.Selected)
	}
}

func TestUnifiedMouseModeNegotiationAndRestore(t *testing.T) {
	// The DECRQM answers the decoder must record: the exact form CSI ? Ps
	// ; Pm $ y, the values 1/2/3/4 tracked separately, first answer wins.
	prior := TerminalKeys{MouseReplyPending: true}
	for _, r := range "\x1b[?1000;1$y\x1b[?1006;2$y\x1b[?1002;3$y\x1b[?1003;4$y" {
		prior.Feed(r)
	}
	if got := prior.DecrQM; got[1000] != 1 || got[1006] != 2 || got[1002] != 3 || got[1003] != 4 || len(got) != 4 {
		t.Fatalf("DecrQM after DECRQM=%v want 1000:1 1006:2 1002:3 1003:4", got)
	}
	// A late or unsolicited answer never overwrites the initial state.
	for _, r := range "\x1b[?1000;2$y\x1b[?1006;1$y" {
		prior.Feed(r)
	}
	if got := prior.DecrQM; got[1000] != 1 || got[1006] != 2 {
		t.Fatalf("late DECRQM overwrote the initial state: %v", got)
	}
	// Malformed answers are ignored: missing ?, missing $y, extra fields,
	// out-of-range values.
	malformed := TerminalKeys{MouseReplyPending: true}
	for _, r := range "\x1b[1000;1$y\x1b[?1000;1y\x1b[?1000;1;2$y\x1b[?1000;5$y\x1b[?x;1$y" {
		malformed.Feed(r)
	}
	if len(malformed.DecrQM) != 0 {
		t.Fatalf("malformed DECRQM recorded %v", malformed.DecrQM)
	}

	// The negotiation decides only when the required modes are known (or
	// the bound runs out), enables exactly the reset ones, and restores
	// only what it changed, re-enabling the displaced prior-set modes.
	feed := func(d *TerminalKeys, answers string) {
		d.MouseReplyPending = true
		for _, r := range answers {
			d.Feed(r)
		}
		if d.DecrQM == nil {
			d.DecrQM = map[int]int{}
		}
		for _, mode := range mouseModes {
			if _, ok := d.DecrQM[mode]; !ok {
				d.DecrQM[mode] = 2
			}
		}
	}
	// Both wheel modes reset, the drag (1002) and SGR-extended (1016)
	// encodings pre-set: a realistic exclusivity conflict.
	dec := TerminalKeys{}
	m := newMouseNegotiation(&dec)
	m.deadline = time.Now().Add(mouseQueryTimeout)
	if m.poll(io.Discard) {
		t.Fatal("poll decided before the required answers arrived")
	}
	feed(&dec, "\x1b[?1002;1$y\x1b[?1016;1$y\x1b[?1000;2$y\x1b[?1006;2$y")
	out := &bytes.Buffer{}
	if !m.poll(out) {
		t.Fatal("poll did not decide once both required modes are known")
	}
	if got := out.String(); got != "\x1b[?1000h\x1b[?1006h" {
		t.Fatalf("enables=%q want exactly the two reset wheel modes", got)
	}
	if want := []int{1000, 1006}; !reflect.DeepEqual(m.enabled, want) {
		t.Fatalf("enabled=%v want %v", m.enabled, want)
	}
	// The enables displaced the pre-set 1002 (tracking) and 1016
	// (encoding); the restore disables what it enabled first, then
	// re-enables the prior set - coherent order, exact modes.
	restoreOut := &bytes.Buffer{}
	m.restore(restoreOut)
	if got, want := restoreOut.String(), "\x1b[?1000l\x1b[?1006l\x1b[?1002h\x1b[?1016h"; got != want {
		t.Fatalf("restore=%q want %q (previous tracking and encoding, coherent order)", got, want)
	}
	// Unknown is never read as off: a timeout with no answers enables and
	// restores nothing, and the wheel stays honestly unavailable.
	silent := newMouseNegotiation(&TerminalKeys{})
	silent.deadline = time.Now().Add(-time.Millisecond)
	if !silent.poll(io.Discard) {
		t.Fatal("poll must decide on the bound even without answers")
	}
	if len(silent.enabled) != 0 || len(silent.displaced) != 0 {
		t.Fatalf("no-reply negotiation changed state: enabled=%v displaced=%v", silent.enabled, silent.displaced)
	}
	silentOut := &bytes.Buffer{}
	silent.restore(silentOut)
	if silentOut.Len() != 0 {
		t.Fatalf("no-reply restore wrote %q want nothing (unknown is never off)", silentOut.String())
	}
	if silent.wheelAvailable() {
		t.Fatal("wheel reported available without any confirmation")
	}
	// A permanent reset (4) is unsupported: nothing is enabled, nothing
	// is touched.
	perm := TerminalKeys{}
	feed(&perm, "\x1b[?1000;4$y\x1b[?1006;4$y")
	permNeg := newMouseNegotiation(&perm)
	permNeg.deadline = time.Now()
	permOut := &bytes.Buffer{}
	permNeg.poll(permOut)
	if permOut.Len() != 0 || len(permNeg.enabled) != 0 {
		t.Fatalf("permanent reset enabled %q / %v", permOut.String(), permNeg.enabled)
	}
	permNeg.restore(permOut)
	if permOut.Len() != 0 {
		t.Fatalf("permanent reset restore wrote %q", permOut.String())
	}
	// Already on (set 1 / permanent set 3): the popup enables nothing and
	// the wheel is delivered by the terminal's own state.
	already := TerminalKeys{}
	feed(&already, "\x1b[?1000;3$y\x1b[?1006;1$y")
	alreadyNeg := newMouseNegotiation(&already)
	alreadyOut := &bytes.Buffer{}
	alreadyNeg.poll(alreadyOut)
	if alreadyOut.Len() != 0 || len(alreadyNeg.enabled) != 0 || !alreadyNeg.wheelAvailable() {
		t.Fatalf("already-on negotiation=%q enabled=%v available=%v", alreadyOut.String(), alreadyNeg.enabled, alreadyNeg.wheelAvailable())
	}
	// A writer failure marks only the successful enables: the first write
	// lands, the second fails, and the restore touches only the first.
	partial := TerminalKeys{}
	feed(&partial, "\x1b[?1000;2$y\x1b[?1006;2$y\x1b[?1003;1$y")
	partialNeg := newMouseNegotiation(&partial)
	partialNeg.deadline = time.Now()
	half := &halfWriter{}
	partialNeg.poll(half)
	if !reflect.DeepEqual(partialNeg.enabled, []int{1000, 1006}) {
		t.Fatalf("failed write enabled=%v want [1000 1006] conservative restoration", partialNeg.enabled)
	}
	partialOut := &bytes.Buffer{}
	partialNeg.restore(partialOut)
	if got, want := partialOut.String(), "\x1b[?1000l\x1b[?1006l\x1b[?1003h"; got != want {
		t.Fatalf("failed-write restore=%q want %q (only the successfully changed known modes)", got, want)
	}

	// Full lifecycle in one input loop: push, query, the answers on the
	// input stream, the bounded decision, the run, then pop, the exact
	// restore and the cursor.
	out2 := &bytes.Buffer{}
	state := NewBoardState()
	state.Mouse = newMouseNegotiation(&state.TerminalKeys)
	state.Mouse.deadline = time.Now().Add(mouseQueryTimeout)
	code := PickerTerminalWithMouse(true, func() (func() error, error) {
		return func() error { return nil }, nil
	}, out2, state, func() int {
		feed(&state.TerminalKeys, "\x1b[?1002;1$y\x1b[?1000;2$y\x1b[?1006;2$y")
		if !state.pollMouse(out2) {
			t.Fatal("pollMouse did not decide with the answers present")
		}
		return 7
	})
	if code != 7 {
		t.Fatalf("lifecycle code=%d want 7", code)
	}
	want := keyboardPushSeq + mouseQueryAll() + "\x1b[?1000h\x1b[?1006h" + keyboardPopSeq + "\x1b[?1000l\x1b[?1006l\x1b[?1002h" + "\x1b[?25h"
	if got := out2.String(); got != want {
		t.Fatalf("lifecycle output=%q want %q", got, want)
	}
	// A panic restores the mouse modes and re-raises.
	out3 := &bytes.Buffer{}
	state3 := NewBoardState()
	state3.Mouse = newMouseNegotiation(&state3.TerminalKeys)
	state3.Mouse.deadline = time.Now()
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		_ = PickerTerminalWithMouse(true, func() (func() error, error) {
			return func() error { return nil }, nil
		}, out3, state3, func() int {
			feed(&state3.TerminalKeys, "\x1b[?1000;2$y\x1b[?1006;2$y")
			state3.pollMouse(out3)
			panic("boom")
		})
	}()
	if !panicked {
		t.Fatal("panic did not re-raise")
	}
	if !strings.HasSuffix(out3.String(), keyboardPopSeq+"\x1b[?1000l\x1b[?1006l"+"\x1b[?25h") {
		t.Fatalf("panic lifecycle output=%q want the restore before the cursor", out3.String())
	}
	// Non-terminal (pipe): no protocol, no mouse tracking at all.
	out4 := &bytes.Buffer{}
	code4 := PickerTerminalWithMouse(false, func() (func() error, error) {
		return func() error { return nil }, nil
	}, out4, NewBoardState(), func() int { return 0 })
	if code4 != 0 || out4.Len() != 0 {
		t.Fatalf("pipe lifecycle code=%d output=%q want 0 and nothing", code4, out4.String())
	}
	// The query covers the tracking and the encoding families, before any
	// change.
	if got := newMouseNegotiation(&TerminalKeys{}).querySeq(); got != mouseQueryAll() {
		t.Fatalf("query=%q want %q", got, mouseQueryAll())
	}
	// The keyboard push/pop probe answers stay ignored keys.
	keys := TerminalKeys{}
	for _, r := range "\x1b[?0u\x1b[?1u" {
		if k := keys.Feed(r); k != "" {
			t.Fatalf("the keyboard probe answer decoded a key: %q", k)
		}
	}
}

// mouseQueryAll is the expected DECRQM query of the nine tracked modes.
func mouseQueryAll() string {
	want := ""
	for _, mode := range []int{9, 1000, 1001, 1002, 1003, 1005, 1006, 1015, 1016} {
		want += "\x1b[?" + strconv.Itoa(mode) + "$p"
	}
	return want
}

// halfWriter succeeds on the first write and fails afterwards, to prove a
// failed enable is neither marked nor restored.
type halfWriter struct{ done bool }

func (h *halfWriter) Write(p []byte) (int, error) {
	if h.done {
		return 0, errors.New("broken pipe")
	}
	h.done = true
	return len(p), nil
}

// unifiedLoopSnapshot is the picker-flavor loop fixture: two agent panes
// and one terminal pane (no agent record) under one workspace.
func unifiedLoopSnapshot() string {
	return `{"result":{"snapshot":{"workspaces":[{"workspace_id":"w1","label":"alpha"}],"tabs":[{"tab_id":"w1:t1"}],"agents":[{"pane_id":"w1:p1","name":"orchestrator","agent":"claude","agent_status":"working","title":"orchestrator: run"},{"pane_id":"w1:p2","name":"implementer-1","agent":"codex","agent_status":"idle","title":"implementer: task"}],"panes":[{"pane_id":"w1:p1","workspace_id":"w1","tab_id":"w1:t1"},{"pane_id":"w1:p2","workspace_id":"w1","tab_id":"w1:t1"},{"pane_id":"w1:p3","workspace_id":"w1","tab_id":"w1:t1","cwd":"/srv/watch"}]}}}`
}

func lastFrame(t *testing.T, data []byte) string {
	t.Helper()
	frames := strings.Split(string(data), "\x1b[H\x1b[J")
	return frames[len(frames)-1]
}

// frameBandedLines returns the reverse-banded (selected) lines of the
// last drawn frame: the frame has no real newlines - the lines are
// absolute-positioned - so a band is one cursor-positioned segment.
func frameBandedLines(t *testing.T, data []byte) []string {
	t.Helper()
	var out []string
	for _, seg := range frameLineRe.Split(lastFrame(t, data), -1) {
		// A frame line may carry a leading margin space before the band
		// escape; the band (reverse video) marks the selected line.
		if strings.HasPrefix(strings.TrimLeft(seg, " "), "\x1b[7m") {
			out = append(out, seg)
		}
	}
	return out
}

var frameLineRe = regexp.MustCompile(`\x1b\[\d+;1H`)

func frameBandedLineContains(t *testing.T, data []byte, want string) bool {
	t.Helper()
	for _, line := range frameBandedLines(t, data) {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// waitForBandedLine polls until the last drawn frame bands a line that
// contains want.
func waitForBandedLine(t *testing.T, path string, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && frameBandedLineContains(t, data, want) {
			return
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(path)
			t.Fatalf("no banded line containing %q in the last frame:\n%s", want, lastFrame(t, data))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitOutput(t *testing.T, path string, want string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), want) {
			return string(data)
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(path)
			t.Fatalf("output never contained %q:\n%s", want, data)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestUnifiedPickerLoopShowsTerminalsWheelAndFilters(t *testing.T) {
	dir := t.TempDir()
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
		{Argv: []string{"api", "snapshot"}, Stdout: unifiedLoopSnapshot()},
	})
	if err != nil {
		t.Fatal(err)
	}
	env := platform.Env{}
	for _, item := range fakecli.Env(testutil.CleanEnv(t), dir, fakecli.EnvOptions{IncludeBasePath: true}) {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	env["HERDR_BIN_PATH"] = herdr
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "picker-")
	if err != nil {
		_ = input.Close()
		_ = writer.Close()
		t.Fatal(err)
	}
	oldInterval := boardRefreshInterval
	boardRefreshInterval = time.Hour // the 10 s ticker must not interfere
	shutdown := make(chan os.Signal, 1)
	finished := make(chan int, 1)
	loopDone := make(chan struct{})
	t.Cleanup(func() {
		select {
		case shutdown <- os.Interrupt:
		default:
		}
		_ = writer.Close()
		select {
		case <-loopDone:
		case <-time.After(5 * time.Second):
			t.Error("picker loop did not stop during cleanup")
			return // keep its descriptors and shared configuration alive
		}
		_ = input.Close()
		_ = output.Close()
		boardRefreshInterval = oldInterval
	})
	state := NewBoardState()
	state.Mouse = newMouseNegotiation(&state.TerminalKeys)
	state.Mouse.deadline = time.Now().Add(mouseQueryTimeout)
	go func() {
		defer close(loopDone)
		finished <- withPickerTerminalMouse(true, func() (func() error, error) { return func() error { return nil }, nil }, output, state, func() int {
			return runSessionLoopWithSignals(env, "darwin", herdr, input, output, true, shutdown, true, state)
		})
	}()

	// The unified display lists the terminal pane (no agent record).
	waitOutput(t, output.Name(), "w1:p3")
	dat, _ := os.ReadFile(output.Name())
	if !frameBandedLineContains(t, dat, "implementer-1") {
		t.Fatalf("the initial frame does not band the first row:\n%s", lastFrame(t, dat))
	}
	// The SGR mouse negotiation: the DECRQM answers arrive on the input
	// stream, the bounded decision enables the two reset wheel modes, and
	// the exact SGR report shape (button;column;row) moves three entries.
	var replies strings.Builder
	for _, mode := range mouseModes {
		fmt.Fprintf(&replies, "\x1b[?%d;2$y", mode)
	}
	if _, err := writer.Write([]byte(replies.String())); err != nil {
		t.Fatal(err)
	}
	waitOutput(t, output.Name(), "\x1b[?1000h\x1b[?1006h")
	if _, err := writer.Write([]byte("\x1b[<65;4;7M")); err != nil {
		t.Fatal(err)
	}
	waitForBandedLine(t, output.Name(), "w1:p3")
	// f cycles the scope to agents: the terminal row leaves the frame and
	// the footer shows the active filter.
	if _, err := writer.Write([]byte("f")); err != nil {
		t.Fatal(err)
	}
	waitOutput(t, output.Name(), "f Agents")
	dat, _ = os.ReadFile(output.Name())
	if strings.Contains(lastFrame(t, dat), "w1:p3") {
		t.Fatalf("agents scope kept the terminal row:\n%s", lastFrame(t, dat))
	}
	// The raw page-up key returns to the first row (implementer-1).
	if _, err := writer.Write([]byte("\x1b[5~")); err != nil {
		t.Fatal(err)
	}
	waitForBandedLine(t, output.Name(), "implementer-1")
	// Esc cancels with code 0 and the exit restores the exact previous
	// mouse modes (both were reset, so both are disabled).
	if _, err := writer.Write([]byte("\x1b")); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	select {
	case code := <-finished:
		if code != 0 {
			t.Fatalf("picker code=%d want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("picker did not close after Esc")
	}
	dat, _ = os.ReadFile(output.Name())
	if !strings.Contains(string(dat), "\x1b[?1000l\x1b[?1006l") {
		t.Fatalf("the exit did not restore the previous mouse modes:\n%s", lastFrame(t, dat))
	}
}
