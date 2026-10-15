package plugin

import (
	"fmt"
	"strings"
	"testing"
)

func TestSessionStatusFailureCoreKeepsCloseAndStatusInViewport(t *testing.T) {
	for _, view := range []string{"table", "tree"} {
		for _, height := range []int{10, 11, 12, 16, 24} {
			for _, failures := range []int{0, 1, 2, 7, 15, 30} {
				state := NewBoardState()
				state.ViewState.Enabled, state.ViewState.View = true, view
				state.Entries = sessionTestEntries(3)
				for i := 0; i < failures; i++ {
					state.Failures = append(state.Failures, PickerFailure{Label: fmt.Sprintf("m%d", i), Cause: "down"})
				}
				frame := renderSessionModal(state, 80, height, "")
				lines := assertSessionBounded(t, frame, 80, height)
				if len(lines) != height {
					t.Fatalf("view=%s height=%d failures=%d: %d rows exceed the fixed viewport", view, height, failures, len(lines))
				}
				// Consume the actual cursor-addressed stream as a terminal:
				// extra CUP rows would clamp and overwrite the close row.
				screen := newMiniScreen(80, height)
				screen.apply(buildModalFrame(frame))
				if got := screen.line(height - 2); !strings.Contains(got, "Enter Focus · Esc Close") {
					t.Fatalf("view=%s height=%d failures=%d: close row overwritten: %q", view, height, failures, got)
				}
				if got := screen.line(height - 1); !strings.Contains(got, "────") {
					t.Fatalf("view=%s height=%d failures=%d: status divider overwritten: %q", view, height, failures, got)
				}
				if got := strings.TrimSpace(screen.line(height)); got != "STATUS  Ready" {
					t.Fatalf("view=%s height=%d failures=%d: final status=%q", view, height, failures, got)
				}
				if failures > 0 {
					// Decorative detail dividers must yield before the most
					// recent diagnostic, even when older failures are trimmed.
					want := fmt.Sprintf("machine m%d: failed (down)", failures-1)
					if !strings.Contains(stripSessionSGR(frame), want) {
						t.Fatalf("view=%s height=%d failures=%d: latest failure hidden: %q", view, height, failures, want)
					}
				}
			}
		}
	}
}

// Regression tests for the shared session popup's lower block: the
// selected-metadata dividers (above and below the detail row) and the
// fixed status strip below the shortcuts (divider + Ready/notice line),
// including the tiny-height degradation and the page-window parity.

// statusBottomOrder checks the exact lower-section order of one frame:
// list, divider, detail, divider, shortcuts, divider, fixed status.
func statusBottomOrder(t *testing.T, lines []string, width, height int) {
	t.Helper()
	if len(lines) != height {
		t.Fatalf("%dx%d: frame must fill the viewport, got %d lines", width, height, len(lines))
	}
	isDivider := func(line string) bool {
		visible := strings.TrimLeft(stripSessionSGR(line), " ")
		return visible != "" && strings.Count(visible, "\u2500") == len([]rune(visible))
	}
	if !strings.HasSuffix(stripSessionSGR(lines[len(lines)-1]), "Ready") {
		t.Fatalf("%dx%d: the fixed status line must close the frame: %q", width, height, lines[len(lines)-1])
	}
	if !isDivider(lines[len(lines)-2]) {
		t.Fatalf("%dx%d: the divider must separate shortcuts and status: %q", width, height, lines[len(lines)-2])
	}
	if !strings.HasSuffix(stripSessionSGR(lines[len(lines)-3]), "Esc Close") {
		t.Fatalf("%dx%d: the shortcut rows must sit above the divider: %q", width, height, lines[len(lines)-3])
	}
	if !strings.Contains(stripSessionSGR(lines[len(lines)-4]), "s Status") {
		t.Fatalf("%dx%d: footer filter line wrong: %q", width, height, lines[len(lines)-4])
	}
	if !strings.Contains(stripSessionSGR(lines[len(lines)-5]), "\u2191\u2193 Select") {
		t.Fatalf("%dx%d: footer navigation line wrong: %q", width, height, lines[len(lines)-5])
	}
	if !isDivider(lines[len(lines)-6]) {
		t.Fatalf("%dx%d: the divider below the detail missing: %q", width, height, lines[len(lines)-6])
	}
	if !strings.Contains(stripSessionSGR(lines[len(lines)-7]), "ref-01") {
		t.Fatalf("%dx%d: the detail line missing between the dividers: %q", width, height, lines[len(lines)-7])
	}
	if !isDivider(lines[len(lines)-8]) {
		t.Fatalf("%dx%d: the divider above the detail missing: %q", width, height, lines[len(lines)-8])
	}
}

func TestSessionStatusBottomBlockOrder(t *testing.T) {
	for _, view := range []string{"table", "tree"} {
		state := NewPickerState()
		state.Entries = sessionTestEntries(40)
		state.ViewState.Enabled = true
		state.ViewState.View = view
		state.Selected = 0
		out := renderPickerModal(state, 80, 24, "")
		lines := assertSessionBounded(t, out, 80, 24)
		statusBottomOrder(t, lines, 80, 24)
		// The detail is drawn exactly once, between its two dividers -
		// never a second copy.
		if got := strings.Count(out, "ref-01 \u00b7 main \u00b7 /tmp/proj-01"); got != 1 {
			t.Fatalf("view=%s: the detail row appears %d times, want 1:\n%s", view, got, out)
		}
	}
}

func TestSessionStatusReadyCopiedFocusAndFailure(t *testing.T) {
	// The operator's sequence over the board consumer path: idle
	// (Ready), copy (truthful notice with the plain machine/pane
	// reference), the honest OSC 52 fallback, navigation focus (outranks
	// the copy), failure. The consumer's notice precedence is verified
	// on the redrawSession transport output; the fixed line layout and
	// emphasis on the shared renderer's frame.
	state := NewBoardState()
	state.ViewState.Enabled = true
	state.Entries = sessionTestEntries(3)
	state.Selected = 0

	// idle: the transport frame carries the fixed strip with Ready.
	var buf strings.Builder
	redrawSession(&buf, state, 80) // 80x24 default viewport
	if !strings.Contains(buf.String(), "STATUS  ") || !strings.Contains(buf.String(), "Ready") {
		t.Fatalf("idle transport frame missing the fixed strip:\n%q", buf.String())
	}
	lines := assertSessionBounded(t, renderSessionModal(state, 80, 24, ""), 80, 24)
	if got := strings.TrimLeft(stripSessionSGR(lines[len(lines)-1]), " "); got != "STATUS  Ready" {
		t.Fatalf("idle status line=%q want STATUS  Ready", got)
	}
	if strings.Contains(lines[len(lines)-1], sessionStyleBold) {
		t.Fatalf("the idle content must keep normal emphasis: %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[len(lines)-1], sessionStyleDim+sessionStatusStripLabel) {
		t.Fatalf("the STATUS label must be dim chrome: %q", lines[len(lines)-1])
	}

	// c copies and keeps the modal open: the confirmation is the
	// consumer-resolved notice and lands in the fixed strip, the
	// confirmed copy in bold so it stays visible.
	if action := state.ApplyKey("c"); action != "copy" || state.Copied == nil || *state.Copied != "ref-01" {
		t.Fatalf("c: action=%q copied=%v want copy ref-01", action, state.Copied)
	}
	if state.Exit != "" {
		t.Fatalf("the copy closed the modal: exit=%q", state.Exit)
	}
	state.CopyNotice = copyNoticeFor(ClipboardResult{Path: "pbcopy"}, *state.Copied)
	buf.Reset()
	redrawSession(&buf, state, 80)
	if !strings.Contains(buf.String(), "Copied ref-01") {
		t.Fatalf("the transport frame never confirmed the copy:\n%q", buf.String())
	}
	lines = assertSessionBounded(t, renderSessionModal(state, 80, 24, state.CopyNotice), 80, 24)
	if got := strings.TrimLeft(stripSessionSGR(lines[len(lines)-1]), " "); got != "STATUS  Copied ref-01" {
		t.Fatalf("copied status line=%q want STATUS  Copied ref-01", got)
	}
	if !strings.Contains(lines[len(lines)-1], sessionStyleBold+"Copied ref-01"+sessionStyleReset) {
		t.Fatalf("the confirmed copy must be bold: %q", lines[len(lines)-1])
	}
	statusBottomOrderFooter(t, lines)

	// The OSC 52 fallback must not claim confirmed clipboard success:
	// the honest unconfirmed wording carries the plain reference and
	// keeps normal emphasis (never bold).
	state.CopyNotice = copyNoticeFor(ClipboardResult{Path: "osc52"}, *state.Copied)
	buf.Reset()
	redrawSession(&buf, state, 80)
	if !strings.Contains(buf.String(), "Copied via OSC 52 (unconfirmed): ref-01") {
		t.Fatalf("the transport frame missing the honest osc52 wording:\n%q", buf.String())
	}
	lines = assertSessionBounded(t, renderSessionModal(state, 80, 24, state.CopyNotice), 80, 24)
	if got := strings.TrimLeft(stripSessionSGR(lines[len(lines)-1]), " "); got != "STATUS  Copied via OSC 52 (unconfirmed): ref-01" {
		t.Fatalf("osc52 status line=%q want the honest unconfirmed wording", got)
	}
	if strings.Contains(lines[len(lines)-1], sessionStyleBold) {
		t.Fatalf("the unconfirmed fallback must not be styled as confirmed: %q", lines[len(lines)-1])
	}

	// A started navigation outranks the copy notice in the same strip
	// (a fresh state, like the loop's Enter path: the pending navigation
	// takes over the status area).
	nav := NewBoardState()
	nav.ViewState.Enabled = true
	nav.Entries = sessionTestEntries(3)
	nav.Selected = 0
	nav.CopyNotice = copyNoticeFor(ClipboardResult{Path: "pbcopy"}, "ref-01")
	nav.NavPending = true
	nav.NavTarget = sessionTestEntries(3)[0]
	buf.Reset()
	redrawSession(&buf, nav, 80)
	if !strings.Contains(buf.String(), "Focusing selected pane\u2026 Esc cancels") || strings.Contains(buf.String(), "Copied ref-01") {
		t.Fatalf("the navigation guidance must supersede the copy notice:\n%q", buf.String())
	}
	lines = assertSessionBounded(t, renderSessionModal(nav, 80, 24, "Focusing selected pane\u2026 Esc cancels"), 80, 24)
	if got := strings.TrimLeft(stripSessionSGR(lines[len(lines)-1]), " "); got != "STATUS  Focusing selected pane\u2026 Esc cancels" {
		t.Fatalf("focus status line=%q want the navigation guidance", got)
	}

	// A failed navigation keeps the failure visible in the status core
	// while the strip falls back to the copy notice, and the modal stays
	// open throughout.
	boardNavApplyResult(nav, NavigationResult{Cause: "selected pane no longer exists"})
	if nav.Exit != "" || nav.NavPending {
		t.Fatalf("the failed navigation must keep the modal open: exit=%q pending=%v", nav.Exit, nav.NavPending)
	}
	buf.Reset()
	redrawSession(&buf, nav, 80)
	if !strings.Contains(buf.String(), "machine local: failed (selected pane no longer exists)") {
		t.Fatalf("the navigation failure missing from the transport frame:\n%q", buf.String())
	}
	lines = assertSessionBounded(t, renderSessionModal(nav, 80, 24, nav.CopyNotice), 80, 24)
	if got := strings.TrimLeft(stripSessionSGR(lines[len(lines)-1]), " "); got != "STATUS  Copied ref-01" {
		t.Fatalf("post-failure status line=%q want the copy notice back", got)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "machine local: failed (selected pane no longer exists)") {
		t.Fatalf("the navigation failure missing from the status core:\n%s", strings.Join(lines, "\n"))
	}
}

// statusBottomOrderFooter pins the shortcut block right above the fixed
// strip (divider + status) after the copy/focus/failure sequence.
func statusBottomOrderFooter(t *testing.T, lines []string) {
	t.Helper()
	if len(lines) != 24 {
		t.Fatalf("frame must stay 24 lines, got %d", len(lines))
	}
	if !strings.HasSuffix(stripSessionSGR(lines[len(lines)-3]), "Esc Close") {
		t.Fatalf("the shortcut rows must stay above the divider: %q", lines[len(lines)-3])
	}
	if !strings.Contains(stripSessionSGR(lines[len(lines)-3]), "c Copy") || !strings.Contains(stripSessionSGR(lines[len(lines)-3]), "Enter Focus") {
		t.Fatalf("the c Copy / Enter Focus commands must stay unchanged: %q", lines[len(lines)-3])
	}
}

func TestSessionStatusStripPreservesRowPositionWhenCopying(t *testing.T) {
	for _, view := range []string{"table", "tree"} {
		state := NewPickerState()
		state.Entries = sessionTestEntries(40)
		state.ViewState.Enabled = true
		state.ViewState.View = view
		state.Selected = 7
		idle := strings.Split(strings.TrimSuffix(renderPickerModal(state, 80, 24, ""), "\n"), "\n")
		copied := strings.Split(strings.TrimSuffix(renderPickerModal(state, 80, 24, "Copied ref-08"), "\n"), "\n")
		if len(idle) != len(copied) {
			t.Fatalf("view=%s: copying changed the frame height %d -> %d", view, len(idle), len(copied))
		}
		for i := 0; i < len(idle)-1; i++ {
			if idle[i] != copied[i] {
				t.Fatalf("view=%s: copying moved row %d:\nidle:  %q\ncopied: %q", view, i, idle[i], copied[i])
			}
		}
		if !strings.HasSuffix(stripSessionSGR(idle[len(idle)-1]), "Ready") {
			t.Fatalf("view=%s: idle status line wrong: %q", view, idle[len(idle)-1])
		}
		if !strings.HasSuffix(stripSessionSGR(copied[len(copied)-1]), "Copied ref-08") {
			t.Fatalf("view=%s: copied status line wrong: %q", view, copied[len(copied)-1])
		}
		// The selection band and the detail row keep their positions.
		idleBand := sessionHighlightedLines(t, idle)
		copiedBand := sessionHighlightedLines(t, copied)
		for i, line := range idle {
			if strings.Contains(line, modalHighlightOn) && !strings.Contains(copied[i], modalHighlightOn) {
				t.Fatalf("view=%s: the selection band moved when copying", view)
			}
		}
		if len(idleBand) != 1 || len(copiedBand) != 1 {
			t.Fatalf("view=%s: the single selection band must be preserved", view)
		}
	}
}

func TestSessionStatusNoticeSanitizedAndBounded(t *testing.T) {
	state := NewPickerState()
	state.Entries = sessionTestEntries(3)
	state.ViewState.Enabled = true
	state.Selected = 0
	long := strings.Repeat("\u90e8\u7f72-e", 60) // wide runes, well over the frame width
	hostile := "\x1b[31mCopied\x1b[0m local/w3:p\x1b]52;c;UEFO\x07more\ttab\nnewline\x01\x02"
	for _, notice := range []string{long, hostile} {
		out := renderPickerModal(state, 60, 16, notice)
		lines := assertSessionBounded(t, out, 60, 16)
		status := strings.TrimLeft(stripSessionSGR(lines[len(lines)-1]), " ")
		if displayWidth(status) > 59 {
			t.Fatalf("the status line exceeds the frame width: %q", status)
		}
		if notice == long {
			if !strings.HasPrefix(status, "\u2026") {
				t.Fatalf("a long notice must be ellipsized head-first, keeping its tail: %q", status)
			}
		} else {
			if !strings.Contains(status, "Copied local/w3:p") || !strings.Contains(status, "more tab newline") {
				t.Fatalf("the notice must be sanitized to a single clean line: %q", status)
			}
		}
	}
}

func TestSessionStatusTreeAndTableSweep(t *testing.T) {
	entries := sessionTestEntries(40)
	for _, view := range []string{"table", "tree"} {
		for _, width := range []int{30, 60, 100} {
			for _, height := range []int{7, 8, 10, 16, 24} {
				state := NewPickerState()
				state.Entries = entries
				state.ViewState.Enabled = true
				state.ViewState.View = view
				state.Selected = 12
				out := renderPickerModal(state, width, height, "")
				lines := assertSessionBounded(t, out, width, height)
				if len(lines) != height {
					t.Fatalf("view=%s %dx%d: frame must fill the viewport, got %d lines", view, width, height, len(lines))
				}
				// One visible selected leaf wherever the reserved
				// window has room (it always does at these bounds).
				bands := sessionHighlightedLines(t, lines)
				if len(bands) != 1 {
					t.Fatalf("view=%s %dx%d: expected one selected leaf, got %d:\n%s", view, width, height, len(bands), out)
				}
				if height >= 10 {
					if !strings.HasSuffix(stripSessionSGR(lines[len(lines)-1]), "Ready") || !strings.HasSuffix(stripSessionSGR(lines[len(lines)-3]), "Esc Close") {
						t.Fatalf("view=%s %dx%d: the strip must sit below the shortcuts:\n%s", view, width, height, out)
					}
				} else {
					if !strings.HasSuffix(stripSessionSGR(lines[len(lines)-1]), "Ready") || !strings.HasSuffix(stripSessionSGR(lines[len(lines)-2]), "Esc Close") {
						t.Fatalf("view=%s %dx%d: the degraded strip must sit below the shortcuts:\n%s", view, width, height, out)
					}
				}
			}
		}
	}
}

func TestSessionStatusPageWindowParity(t *testing.T) {
	entries := sessionTestEntries(40)
	geos := []struct{ width, height, sel int }{
		{60, 24, 12}, {60, 24, 35}, {48, 16, 5}, {80, 12, 20}, {40, 12, 30}, {40, 3, 10},
	}
	for _, view := range []string{"table", "tree"} {
		for _, geo := range geos {
			state := NewBoardState()
			state.Entries = entries
			state.ViewState.Enabled = true
			state.ViewState.View = view
			state.Selected = geo.sel
			state.FrameWidth, state.FrameHeight = geo.width, geo.height
			page := sessionSelectableWindow(unifiedSessionOptions(state, ""), geo.width, geo.height)
			if page < 1 || page > len(entries) {
				t.Fatalf("view=%s %dx%d: page=%d out of [1,%d]", view, geo.width, geo.height, page, len(entries))
			}
			// The rendered frame shows exactly the selectable rows the
			// page math reserves: the fixture names are the only
			// selectable-row text in the frame.
			frame := renderBoardModal(state, geo.width, geo.height, "")
			lines := assertSessionBounded(t, frame, geo.width, geo.height)
			shown := 0
			for _, line := range lines {
				if strings.Contains(stripSessionSGR(line), "agent-") {
					shown++
				}
			}
			if shown != page {
				t.Fatalf("view=%s %dx%d: the frame shows %d selectable rows, the page math reserves %d:\n%s",
					view, geo.width, geo.height, shown, page, frame)
			}
			// The explicit page movement steps by exactly that count, from
			// the same selection the math and the frame measured (the tree
			// page counts the leaves of the window at the selection, so a
			// window beside a group header is smaller near the edges).
			pageState := NewBoardState()
			pageState.Entries = entries
			pageState.ViewState.Enabled = true
			pageState.ViewState.View = view
			pageState.FrameWidth, pageState.FrameHeight = geo.width, geo.height
			pageState.Selected = geo.sel
			pageState.ApplyKey("page-down")
			want := geo.sel + page
			if want > len(entries)-1 {
				want = len(entries) - 1
			}
			if pageState.Selected != want {
				t.Fatalf("view=%s %dx%d: page-down from %d with page %d=%d want %d", view, geo.width, geo.height, geo.sel, page, pageState.Selected, want)
			}
		}
	}
}

func TestSessionStatusTinyHeightsKeepNavigation(t *testing.T) {
	entries := sessionTestEntries(40)
	for _, height := range []int{1, 2, 3, 4, 5, 6} {
		state := NewPickerState()
		state.Entries = entries
		state.ViewState.Enabled = true
		state.Selected = 39
		out := renderPickerModal(state, 80, height, "")
		lines := assertSessionBounded(t, out, 80, height)
		if len(lines) != height {
			t.Fatalf("height %d: frame must fill the viewport, got %d lines:\n%s", height, len(lines), out)
		}
		// The fixed strip degrades deterministically: no status line,
		// no divider, the footer keeps the bottom anchor.
		if strings.Contains(strings.Join(lines, "\n"), "Ready") {
			t.Fatalf("height %d: the status line must degrade, not overflow:\n%s", height, out)
		}
		last := stripSessionSGR(lines[len(lines)-1])
		if !strings.Contains(last, "Esc") {
			t.Fatalf("height %d: Esc guidance must survive: %q", height, last)
		}
		// At least one selectable row wherever the window has room.
		if height >= 4 {
			bands := sessionHighlightedLines(t, lines)
			if len(bands) != 1 {
				t.Fatalf("height %d: one selectable row must stay visible, got %d:\n%s", height, len(bands), out)
			}
		}
	}
	// The detail row itself survives without its dividers (they drop
	// first): at 80x5 the frame is header, search, row, detail, footer.
	state := NewPickerState()
	state.Entries = sessionTestEntries(3)
	state.ViewState.Enabled = true
	state.Selected = 0
	out := renderPickerModal(state, 80, 5, "")
	lines := assertSessionBounded(t, out, 80, 5)
	if got := stripSessionSGR(lines[len(lines)-2]); !strings.Contains(got, "ref-01 \u00b7 main \u00b7 /tmp/proj-01") {
		t.Fatalf("the detail row must keep its reserved line without dividers: %q", got)
	}
}

func TestSessionStatusDetailDividersDegradeBeforeContent(t *testing.T) {
	state := NewPickerState()
	state.Entries = sessionTestEntries(40)
	state.ViewState.Enabled = true
	state.Selected = 0
	// Ordinary size: both dividers reserve two rows with the detail.
	ordinary := strings.Split(strings.TrimSuffix(renderPickerModal(state, 80, 12, ""), "\n"), "\n")
	statusBottomOrder(t, ordinary, 80, 12)
	// Tiny size: the optional dividers drop, the content (the window
	// and the detail row) keeps its room.
	tiny := strings.Split(strings.TrimSuffix(renderPickerModal(state, 80, 10, ""), "\n"), "\n")
	if len(tiny) != 10 {
		t.Fatalf("80x10: frame must fill the viewport, got %d lines", len(tiny))
	}
	// Bottom-up: status, divider, three shortcuts, then the detail row
	// directly above (no divider room) with the window line above it.
	if !strings.Contains(stripSessionSGR(tiny[len(tiny)-6]), "ref-01") {
		t.Fatalf("80x10: the detail row lost its reserved line:\n%s", strings.Join(tiny, "\n"))
	}
	if !strings.Contains(stripSessionSGR(tiny[len(tiny)-7]), "agent-01") {
		t.Fatalf("80x10: the window row must sit directly above the detail (no divider):\n%s", strings.Join(tiny, "\n"))
	}
	if !strings.Contains(stripSessionSGR(tiny[len(tiny)-5]), "\u2191\u2193 Select") {
		t.Fatalf("80x10: the shortcuts must sit above the detail:\n%s", strings.Join(tiny, "\n"))
	}
}

func TestSessionStatusPinnedRefSafety(t *testing.T) {
	entries := sessionTestEntries(6)
	t.Run("cursor follows the pinned ref during refresh", func(t *testing.T) {
		state := NewBoardState()
		state.Entries = entries
		state.ViewState.Enabled = true
		state.refreshing = true
		state.selectedRef = "ref-05"
		state.Selected = 1
		out := renderBoardModal(state, 80, 24, "")
		lines := assertSessionBounded(t, out, 80, 24)
		bands := sessionHighlightedLines(t, lines)
		if len(bands) != 1 || !strings.Contains(stripSessionSGR(bands[0]), "agent-05") {
			t.Fatalf("the cursor must follow the pinned ref:\n%s", out)
		}
		if got := strings.TrimLeft(stripSessionSGR(lines[len(lines)-1]), " "); !strings.HasSuffix(got, "Ready") {
			t.Fatalf("the fixed strip must keep its position during refresh: %q", got)
		}
	})
	t.Run("missing pin draws no cursor and keeps the strip", func(t *testing.T) {
		state := NewBoardState()
		state.Entries = entries
		state.ViewState.Enabled = true
		state.refreshing = true
		state.selectedRef = "gone"
		state.Selected = 1
		out := renderBoardModal(state, 80, 24, "")
		lines := assertSessionBounded(t, out, 80, 24)
		if bands := sessionHighlightedLines(t, lines); len(bands) != 0 {
			t.Fatalf("a missing pin must not highlight any row:\n%s", out)
		}
		if !strings.Contains(strings.Join(lines, "\n"), "selected session left this load; pick another with \u2191/\u2193") {
			t.Fatalf("the pin-missing guidance missing:\n%s", out)
		}
		if got := strings.TrimLeft(stripSessionSGR(lines[len(lines)-1]), " "); !strings.HasSuffix(got, "Ready") {
			t.Fatalf("the fixed strip must keep its position with a missing pin: %q", got)
		}
	})
}

func TestSessionStatusLabelEmphasisAndNarrowWidth(t *testing.T) {
	state := NewPickerState()
	state.Entries = sessionTestEntries(3)
	state.ViewState.Enabled = true
	state.Selected = 0
	confirmed := copyNoticeFor(ClipboardResult{Path: "pbcopy"}, "local/w14:p6M")
	osc52 := copyNoticeFor(ClipboardResult{Path: "osc52"}, "local/w14:p6M")
	// Ordinary width: the dim STATUS label and the content; the
	// confirmed copy is bold, the fallback and the idle state are not.
	var lines []string
	for _, tc := range []struct {
		notice, content string
		bold            bool
	}{
		{"", "Ready", false},
		{confirmed, confirmed, true},
		{osc52, osc52, false},
		{"Focusing selected pane\u2026 Esc cancels", "Focusing selected pane\u2026 Esc cancels", false},
	} {
		out := renderPickerModal(state, 80, 16, tc.notice)
		lines = assertSessionBounded(t, out, 80, 16)
		raw := lines[len(lines)-1]
		if !strings.Contains(raw, sessionStyleDim+sessionStatusStripLabel) {
			t.Fatalf("notice=%q: the dim STATUS label missing: %q", tc.notice, raw)
		}
		if strings.Contains(raw, sessionStyleBold) != tc.bold {
			t.Fatalf("notice=%q: bold emphasis wrong (want %v): %q", tc.notice, tc.bold, raw)
		}
		if tc.bold && !strings.Contains(raw, sessionStyleBold+tc.content+sessionStyleReset) {
			t.Fatalf("notice=%q: the bold content wrong: %q", tc.notice, raw)
		}
		if !strings.Contains(stripSessionSGR(raw), tc.content) {
			t.Fatalf("notice=%q: the content missing: %q", tc.notice, raw)
		}
	}
	// Narrow width: the notice outranks the label - the full notice fits
	// without the label and is never clipped for the label's sake.
	out := renderPickerModal(state, 28, 16, confirmed)
	lines = assertSessionBounded(t, out, 29, 16)
	if got := strings.TrimLeft(stripSessionSGR(lines[len(lines)-1]), " "); got != "Copied local/w14:p6M" {
		t.Fatalf("narrow status line=%q want the full notice without the label", got)
	}
	// Very narrow width: the clipped notice keeps its tail - the useful
	// pane reference - not its head.
	out = renderPickerModal(state, 19, 16, confirmed)
	lines = assertSessionBounded(t, out, 19, 16)
	got := strings.TrimLeft(stripSessionSGR(lines[len(lines)-1]), " ")
	if !strings.HasPrefix(got, "\u2026") || !strings.HasSuffix(got, "local/w14:p6M") {
		t.Fatalf("very narrow status line=%q want the ellipsized head and the kept pane reference", got)
	}
	if displayWidth(got) != 18 {
		t.Fatalf("very narrow status line width=%d want 18: %q", displayWidth(got), got)
	}
	// Tiny width without the left pad: the content alone still fits.
	out = renderPickerModal(state, 5, 16, "")
	lines = assertSessionBounded(t, out, 5, 16)
	if got := stripSessionSGR(lines[len(lines)-1]); !strings.Contains(got, "Ready") {
		t.Fatalf("tiny idle status line=%q want Ready", got)
	}
}
