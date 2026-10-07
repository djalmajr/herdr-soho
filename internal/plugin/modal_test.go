package plugin

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// Fixture and assertion helpers for the modal renderers. The renderers are
// private, so the tests live in the package.

func modalTestEntries(n int) []PickerEntry {
	entries := make([]PickerEntry, 0, n)
	for i := 1; i <= n; i++ {
		entries = append(entries, PickerEntry{
			"ref":             fmt.Sprintf("ref-%02d", i),
			"name":            fmt.Sprintf("agent-%02d", i),
			"kind":            "claude",
			"status":          "working",
			"workspace_label": fmt.Sprintf("ws-%d", (i%3)+1),
			"tab_label":       "main",
			"cwd":             fmt.Sprintf("/tmp/proj-%02d", i),
			"machine":         "local",
		})
	}
	return entries
}

func modalBoardEntries(n int) []PickerEntry {
	statuses := []string{"working", "idle", "blocked", "done"}
	entries := make([]PickerEntry, 0, n)
	for i := 1; i <= n; i++ {
		entries = append(entries, PickerEntry{
			"ref":             fmt.Sprintf("s-%02d", i),
			"name":            fmt.Sprintf("agent-%02d", i),
			"kind":            "pi",
			"status":          statuses[i%len(statuses)],
			"workspace_label": fmt.Sprintf("ws-%d", (i%2)+1),
			"tab_label":       "main",
			"cwd":             fmt.Sprintf("/tmp/board-%02d", i),
			"machine":         "local",
			"title":           fmt.Sprintf("planner: task %02d", i),
		})
	}
	return entries
}

// stripModalSGR removes the renderer's own highlight escapes so the
// visible cells of a line can be measured.
func stripModalSGR(line string) string {
	line = strings.ReplaceAll(line, modalHighlightOn, "")
	return strings.ReplaceAll(line, modalHighlightOff, "")
}

// modalEscapesAllowed fails the test when the line carries any escape byte
// other than the renderer's own highlight on/off pair: no OSC, CSI or raw
// control escape may leak from external fields.
func modalEscapesAllowed(t *testing.T, line string) {
	t.Helper()
	for i := 0; i < len(line); i++ {
		if line[i] != 0x1b {
			continue
		}
		if strings.HasPrefix(line[i:], modalHighlightOn) || strings.HasPrefix(line[i:], modalHighlightOff) {
			i += 3
			continue
		}
		t.Fatalf("unexpected escape sequence in modal line %q", line)
	}
}

// assertModalBounded checks the whole contract on one render: trailing
// newline, at most height logical lines, no wrap, every line at most
// width display columns and no escape leak. It returns the lines.
func assertModalBounded(t *testing.T, out string, width, height int) []string {
	t.Helper()
	if out == "" {
		t.Fatal("empty modal output")
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("missing trailing newline: %q", out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) > height {
		t.Fatalf("output has %d logical lines, popup height is %d:\n%s", len(lines), height, out)
	}
	if !utf8.ValidString(out) {
		t.Fatalf("output is not valid UTF-8: %q", out)
	}
	for _, line := range lines {
		visible := stripModalSGR(line)
		if strings.ContainsRune(visible, '\n') || strings.ContainsRune(visible, '\r') {
			t.Fatalf("line wraps or carries a carriage return: %q", line)
		}
		if w := displayWidth(visible); w > width {
			t.Fatalf("line exceeds the popup width %d (visible %d): %q", width, w, line)
		}
		modalEscapesAllowed(t, line)
	}
	return lines
}

// modalHighlightedLines returns the lines wrapped in the selection
// highlight; the modal must never highlight more than one row.
func modalHighlightedLines(t *testing.T, lines []string) []string {
	t.Helper()
	var out []string
	for _, line := range lines {
		if strings.Contains(line, modalHighlightOn) {
			out = append(out, line)
		}
	}
	return out
}

func TestRenderPickerModalLargeRegistryKeepsSelectionVisible(t *testing.T) {
	for _, test := range []struct {
		name        string
		selected    int
		selectedRef string
		topOut      string // a row that must have scrolled out of the window
	}{
		{name: "middle", selected: 49, selectedRef: "ref-50", topOut: "ref-01"},
		{name: "last", selected: 99, selectedRef: "ref-100", topOut: "ref-01"},
		{name: "first", selected: 0, selectedRef: "ref-01", topOut: "ref-100"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := NewPickerState()
			state.Entries = modalTestEntries(100)
			state.Selected = test.selected
			out := renderPickerModal(state, 60, 20, "")
			lines := assertModalBounded(t, out, 60, 20)
			if len(lines) != 20 {
				t.Fatalf("windowed list should fill the popup, got %d lines:\n%s", len(lines), out)
			}
			if lines[0] != "Pick pane" || lines[1] != "> " {
				t.Fatalf("header not at the top: %q / %q", lines[0], lines[1])
			}
			if !strings.Contains(lines[len(lines)-1], "c Copy · Enter Focus · Esc Close") {
				t.Fatalf("footer wrong: %q", lines[len(lines)-1])
			}
			highlighted := modalHighlightedLines(t, lines)
			if len(highlighted) != 1 {
				t.Fatalf("expected exactly one highlighted row, got %d:\n%s", len(highlighted), out)
			}
			if !strings.Contains(stripModalSGR(highlighted[0]), test.selectedRef) {
				t.Fatalf("selected %s not visible in the highlighted row: %q", test.selectedRef, highlighted[0])
			}
			if !strings.Contains(out, test.selectedRef) {
				t.Fatalf("selected %s missing from the output:\n%s", test.selectedRef, out)
			}
			if strings.Contains(out, test.topOut) {
				t.Fatalf("row %s should have scrolled out of the window:\n%s", test.topOut, out)
			}
			// No spare line in a 17-row window of 100: no detail line.
			if strings.Contains(out, " · /tmp/proj-") {
				t.Fatalf("detail line should be absent without spare space:\n%s", out)
			}
		})
	}
}

func TestRenderPickerModalTinyAndShortTerminals(t *testing.T) {
	state := NewPickerState()
	state.Entries = modalTestEntries(100)
	state.Selected = 99
	for _, tc := range []struct {
		width  int
		height int
	}{
		{8, 1}, {8, 2}, {8, 4}, {12, 1}, {12, 3}, {12, 6}, {24, 2}, {24, 4}, {24, 10},
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func(t *testing.T) {
			out := renderPickerModal(state, tc.width, tc.height, "")
			lines := assertModalBounded(t, out, tc.width, tc.height)
			// The cancellation guidance is never lost, even on a one-line
			// popup at eight columns.
			if !strings.Contains(lines[len(lines)-1], "Esc") {
				t.Fatalf("cancel guidance lost at %dx%d: %q", tc.width, tc.height, lines[len(lines)-1])
			}
			if !strings.HasSuffix(lines[len(lines)-1], "Esc") {
				t.Fatalf("footer should retain cancel guidance at %dx%d: %q", tc.width, tc.height, lines[len(lines)-1])
			}
		})
	}
}

func TestRenderPickerModalEmptyLoadingFailureAndNotice(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		out := renderPickerModal(NewPickerState(), 40, 8, "")
		lines := assertModalBounded(t, out, 40, 8)
		found := false
		for _, line := range lines {
			if line == "no panes" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing the empty state line:\n%s", out)
		}
		if !strings.Contains(lines[len(lines)-1], "c Copy · Enter Focus · Esc") {
			t.Fatalf("footer wrong: %q", lines[len(lines)-1])
		}
	})
	t.Run("loading", func(t *testing.T) {
		local := NewPickerState()
		local.Loading, local.LoadingLocal = 1, true
		out := renderPickerModal(local, 40, 8, "")
		if !strings.Contains(out, "loading local…") {
			t.Fatalf("missing local loading line:\n%s", out)
		}
		remote := NewPickerState()
		remote.Loading = 2
		out = renderPickerModal(remote, 40, 8, "")
		if !strings.Contains(out, "loading remote…") {
			t.Fatalf("missing remote loading line:\n%s", out)
		}
	})
	t.Run("no results", func(t *testing.T) {
		state := NewPickerState()
		state.Entries = modalTestEntries(5)
		state.Query = "zzz"
		out := renderPickerModal(state, 40, 8, "")
		if !strings.Contains(out, `no results for "zzz"`) {
			t.Fatalf("missing the no-results line:\n%s", out)
		}
	})
	t.Run("failures trim from the front", func(t *testing.T) {
		state := NewPickerState()
		state.Entries = modalTestEntries(5)
		state.Selected = 0
		state.Failures = []PickerFailure{
			{Label: "m1", Cause: "c1"},
			{Label: "m2", Cause: "c2"},
			{Label: "m3", Cause: "c3"},
			{Label: "m4", Cause: "c4"},
		}
		// Height 6: title + search + footer leave two status lines, and
		// the earliest failures are the ones dropped.
		lines := assertModalBounded(t, renderPickerModal(state, 40, 6, ""), 40, 6)
		text := strings.Join(lines, "\n")
		if strings.Contains(text, "m1") || strings.Contains(text, "m2") {
			t.Fatalf("earliest failures should be trimmed:\n%s", text)
		}
		if !strings.Contains(text, "machine m3: failed (c3)") || !strings.Contains(text, "machine m4: failed (c4)") {
			t.Fatalf("latest failures should be kept:\n%s", text)
		}
	})
	t.Run("notice line", func(t *testing.T) {
		state := NewPickerState()
		state.Entries = modalTestEntries(3)
		out := renderPickerModal(state, 40, 10, "focus pending")
		lines := assertModalBounded(t, out, 40, 10)
		found := false
		for _, line := range lines {
			if line == "focus pending" {
				found = true
			}
		}
		if !found {
			t.Fatalf("notice line missing:\n%s", out)
		}
	})
}

func TestRenderPickerModalSanitizesExternalText(t *testing.T) {
	state := NewPickerState()
	state.Entries = []PickerEntry{
		{
			"ref":             "\x1b[31mred\x1b[0m",
			"name":            "bad\x1b]0;evil\x07name",
			"kind":            "claude",
			"status":          "a\x01b\x02c",
			"workspace_label": "\x1b[2Jall",
			"tab_label":       "tab",
			"cwd":             "/tmp/\x1b]52;c;UEFO\x07x",
		},
	}
	state.Query = "bad"
	state.Failures = []PickerFailure{{Label: "win\x1b]0;evil\x07dows", Cause: "exit 1: boom\x1b[2J"}}
	out := renderPickerModal(state, 48, 10, "n\x1b]52;c;UEFO\x07x")
	lines := assertModalBounded(t, out, 48, 10)
	text := strings.Join(lines, "\n")
	if strings.Contains(text, "evil") {
		t.Fatalf("OSC payload leaked into the modal:\n%s", out)
	}
	if strings.ContainsRune(out, '\x07') || strings.ContainsRune(out, 0x01) || strings.ContainsRune(out, 0x02) {
		t.Fatalf("control byte leaked into the modal:\n%q", out)
	}
	for _, line := range lines {
		if strings.Contains(line, "\x1b[31m") || strings.Contains(line, "\x1b[2J") {
			t.Fatalf("CSI sequence leaked into the modal: %q", line)
		}
	}
	// The sanitized fields still reach the modal.
	if !strings.Contains(text, "badname") || !strings.Contains(text, "machine windows: failed (exit 1: boom)") {
		t.Fatalf("sanitized fields missing:\n%s", out)
	}
	if !strings.Contains(text, "nx") {
		t.Fatalf("sanitized notice missing:\n%s", out)
	}
	// A control-carrying query is displayed sanitized (and, matching
	// nothing, produces the no-results state).
	state.Query = "a\x1b[31m b"
	out = renderPickerModal(state, 48, 10, "")
	lines = assertModalBounded(t, out, 48, 10)
	if lines[1] != "> a b" {
		t.Fatalf("query not sanitized in the search line: %q", lines[1])
	}
	if !strings.Contains(out, `no results for "a b"`) {
		t.Fatalf("no-results state missing for the unmatched query:\n%s", out)
	}
}

func TestRenderPickerModalWideAndCombiningUnicode(t *testing.T) {
	state := NewPickerState()
	state.Entries = []PickerEntry{
		{"ref": "r1", "name": "部署-部署", "kind": "claude", "status": "working", "workspace_label": "ws-1", "tab_label": "t", "cwd": "/tmp/部署"},
		{"ref": "r2", "name": "café\u0301 long", "kind": "claude", "status": "working", "workspace_label": "ws-2", "tab_label": "t", "cwd": "/tmp/café\u0301"},
		{"ref": "r3", "name": strings.Repeat("n", 40), "kind": "claude", "status": "working", "workspace_label": "ws-1", "tab_label": "t", "cwd": strings.Repeat("/long/path/", 10)},
	}
	state.Selected = 2
	for _, width := range []int{12, 20, 40} {
		out := renderPickerModal(state, width, 10, "")
		lines := assertModalBounded(t, out, width, 10)
		highlighted := modalHighlightedLines(t, lines)
		if len(highlighted) != 1 || !strings.Contains(stripModalSGR(highlighted[0]), "r3") {
			t.Fatalf("selected r3 not highlighted at width %d:\n%s", width, out)
		}
		// A cut row must end with the ellipsis, never a split rune.
		if width < 40 {
			found := false
			for _, line := range lines {
				if strings.HasSuffix(stripModalSGR(line), "…") {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected an ellipsized cut at width %d:\n%s", width, out)
			}
		}
	}
}

func TestRenderPickerModalDetailOnlyWhenSpacePermits(t *testing.T) {
	state := NewPickerState()
	state.Entries = modalTestEntries(3)
	state.Selected = 1
	out := renderPickerModal(state, 40, 10, "")
	lines := assertModalBounded(t, out, 40, 10)
	detail := "ref-02 · /tmp/proj-02"
	found := false
	for _, line := range lines {
		if line == detail {
			found = true
		}
	}
	if !found {
		t.Fatalf("detail line for the selected row missing with spare space:\n%s", out)
	}
	// The selected row keeps its highlight and sits in the window.
	highlighted := modalHighlightedLines(t, lines)
	if len(highlighted) != 1 || !strings.Contains(stripModalSGR(highlighted[0]), "ref-02") {
		t.Fatalf("selected row wrong:\n%s", out)
	}
	if len(lines) > 10 {
		t.Fatalf("detail must not push the modal past the height:\n%s", out)
	}
}

func TestRenderBoardModalPinFollowsTheRef(t *testing.T) {
	entries := modalBoardEntries(30)
	t.Run("reorder pin", func(t *testing.T) {
		state := NewBoardState()
		state.Entries = entries
		state.refreshing = true
		state.selectedRef = "s-17"
		state.Selected = 3 // a stale index that sorts to a different row
		out := renderBoardModal(state, 60, 15, "")
		lines := assertModalBounded(t, out, 60, 15)
		if lines[0] != "30 agents · 7 working · 8 blocked · 8 idle · 7 done" {
			t.Fatalf("totals title wrong: %q", lines[0])
		}
		highlighted := modalHighlightedLines(t, lines)
		if len(highlighted) != 1 {
			t.Fatalf("expected one pinned row, got %d:\n%s", len(highlighted), out)
		}
		if !strings.Contains(stripModalSGR(highlighted[0]), "agent-17") {
			t.Fatalf("cursor does not follow the pinned ref (agent-17):\n%s", highlighted[0])
		}
		if !strings.HasPrefix(stripModalSGR(highlighted[0]), ">") {
			t.Fatalf("pinned row missing the cursor prefix: %q", highlighted[0])
		}
		if !strings.Contains(lines[len(lines)-1], "c Copy · Enter Focus · Esc Close") {
			t.Fatalf("footer wrong: %q", lines[len(lines)-1])
		}
	})
	t.Run("plain selection", func(t *testing.T) {
		state := NewBoardState()
		state.Entries = entries
		state.Selected = 5
		out := renderBoardModal(state, 60, 15, "")
		lines := assertModalBounded(t, out, 60, 15)
		name := pickerString(state.visible()[5]["name"])
		highlighted := modalHighlightedLines(t, lines)
		if len(highlighted) != 1 || !strings.Contains(stripModalSGR(highlighted[0]), name) {
			t.Fatalf("cursor not on the selected visible row %s:\n%s", name, out)
		}
	})
	t.Run("missing pin draws no cursor", func(t *testing.T) {
		state := NewBoardState()
		state.Entries = entries
		state.refreshing = true
		state.selectedRef = "gone"
		state.Selected = 5
		out := renderBoardModal(state, 60, 15, "")
		lines := assertModalBounded(t, out, 60, 15)
		if highlighted := modalHighlightedLines(t, lines); len(highlighted) != 0 {
			t.Fatalf("a missing pin must not cursor any row:\n%s", out)
		}
		found := false
		for _, line := range lines {
			if line == "selected session left this load; pick another with ↑/↓" {
				found = true
			}
		}
		if !found {
			t.Fatalf("pinned-missing guidance missing:\n%s", out)
		}
		// The window anchors at the top instead of the stale selection;
		// the first ws-1 group holds the even refs.
		if !strings.Contains(out, "agent-02") {
			t.Fatalf("window should start at the top row:\n%s", out)
		}
	})
	t.Run("filter keeps the pin visible", func(t *testing.T) {
		state := NewBoardState()
		state.Entries = entries
		state.Query = "ws-2"
		state.refreshing = true
		state.selectedRef = "s-17" // an odd ref lives in ws-2
		out := renderBoardModal(state, 60, 15, "")
		lines := assertModalBounded(t, out, 60, 15)
		text := strings.Join(lines, "\n")
		if strings.Contains(text, "ws-1") {
			t.Fatalf("the filter must hide ws-1 rows:\n%s", out)
		}
		highlighted := modalHighlightedLines(t, lines)
		if len(highlighted) != 1 || !strings.Contains(stripModalSGR(highlighted[0]), "agent-17") {
			t.Fatalf("pinned row not visible after the filter:\n%s", out)
		}
	})
	t.Run("empty board", func(t *testing.T) {
		out := renderBoardModal(NewBoardState(), 40, 8, "")
		text := strings.Join(assertModalBounded(t, out, 40, 8), "\n")
		if !strings.Contains(text, "no agents") {
			t.Fatalf("missing the empty state line:\n%s", out)
		}
	})
}

func TestRenderBoardModalTinyTerminal(t *testing.T) {
	state := NewBoardState()
	state.Entries = modalBoardEntries(30)
	state.Selected = 29
	for _, tc := range []struct {
		width  int
		height int
	}{
		{8, 1}, {12, 3}, {24, 2}, {24, 6}, {40, 4},
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func(t *testing.T) {
			lines := assertModalBounded(t, renderBoardModal(state, tc.width, tc.height, "reloading"), tc.width, tc.height)
			if !strings.Contains(lines[len(lines)-1], "Esc") {
				t.Fatalf("cancel guidance lost at %dx%d: %q", tc.width, tc.height, lines[len(lines)-1])
			}
		})
	}
}

// TestPickerTerminalSizeFailsGracefullyOnNonTerminal runs on every
// platform: a pipe is not a terminal, so the size helpers must return an
// error with zero size instead of panicking or reporting the buffer.
func TestPickerTerminalSizeFailsGracefullyOnNonTerminal(t *testing.T) {
	r, wpipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer wpipe.Close()
	fd := r.Fd()
	w, h, serr := pickerTerminalSize(fd)
	if serr == nil {
		t.Fatalf("expected an error on a pipe, got %dx%d", w, h)
	}
	if w != 0 || h != 0 {
		t.Fatalf("expected zero size on error, got %dx%d", w, h)
	}
	ww, werr := pickerTerminalWidth(fd)
	if werr == nil {
		t.Fatal("pickerTerminalWidth: expected an error on a pipe")
	}
	if ww != 0 {
		t.Fatalf("pickerTerminalWidth: expected 0 on error, got %d", ww)
	}
}

func TestModalRedrawUsesRowsWithoutRawLineFeed(t *testing.T) {
	// A frame exactly as tall/wide as the popup must not scroll the title
	// away or erase the last cell. Raw-mode LF used to cause staircase rows.
	var out bytes.Buffer
	redrawModal(&out, "12345678\nabcdefgh\nEsc     \n")
	got := out.String()
	if strings.ContainsAny(got, "\r\n") || !strings.Contains(got, "\x1b[1;1H12345678\x1b[2;1Habcdefgh\x1b[3;1HEsc     ") {
		t.Fatalf("frame is not positioned by row: %q", got)
	}
	if !strings.HasPrefix(got, "\x1b[H\x1b[J") || !strings.HasSuffix(got, "Esc     ") {
		t.Fatalf("frame clears content after its final cell: %q", got)
	}
}

func TestModalVeryNarrowDoesNotWrapSelectionMarker(t *testing.T) {
	for width := 1; width <= 3; width++ {
		picker := NewPickerState()
		picker.Entries = modalTestEntries(1)
		board := NewBoardState()
		board.Entries = modalBoardEntries(1)
		assertModalBounded(t, renderPickerModal(picker, width, 6, ""), width, 6)
		assertModalBounded(t, renderBoardModal(board, width, 6, ""), width, 6)
	}
}

func TestBoardNavigationErrorSurvivesRefresh(t *testing.T) {
	state := NewBoardState()
	state.Entries = modalBoardEntries(2)
	state.NavTarget = state.Entries[0]
	state.NavPending = true
	boardNavApplyResult(state, NavigationResult{Cause: "target disappeared"})
	fresh := NewBoardState()
	fresh.Entries = state.Entries
	fresh.UpdatedAt = "12:00:00"
	applyBoardUpdate(state, fresh)
	if !strings.Contains(renderBoardModal(state, 100, 10, ""), "target disappeared") || state.Exit != "" || state.Copied != nil {
		t.Fatal("refresh dropped navigation failure or caused copy/exit")
	}
}

func TestModalCancellationAfterIncompleteCSI(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"\x1b[\x1b", "esc"}, {"\x1b[\x03", "ctrl-c"}, {"\x1b[\r", "enter"},
		{"\x1b[" + strings.Repeat("1", 40) + "\x03", "ctrl-c"},
	} {
		keys := TerminalKeys{}
		got := ""
		for _, r := range tc.input {
			got = keys.Feed(r)
		}
		if keys.EscPending {
			got = keys.FlushEsc()
		}
		if got != tc.want || keys.CSIPending {
			t.Errorf("%q: got=%q want=%q pending=%v", tc.input, got, tc.want, keys.CSIPending)
		}
	}
}

func TestLegacyTeamRedrawRetainsScrollingFrames(t *testing.T) {
	var out bytes.Buffer
	redrawArea(&out, strings.Repeat("team row\n", 30))
	got := out.String()
	if !strings.HasPrefix(got, "\x1b[Hteam row\x1b[K\r\n") || !strings.HasSuffix(got, "\x1b[J") || strings.Contains(got, "\x1b[25;1H") {
		t.Fatalf("team frame lost scrolling/legacy delimiters: %q", got)
	}
}
