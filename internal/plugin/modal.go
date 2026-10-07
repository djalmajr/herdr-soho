package plugin

import (
	"fmt"
	"io"
	"strings"
)

// modalDimensions reads the current viewport again on every draw, including
// after a resize. Pipes and test writers use a bounded default viewport.
func modalDimensions(w io.Writer, fallbackWidth int) (width, height int) {
	width, height = fallbackWidth, 24
	if width <= 0 {
		width = 80
	}
	if terminal, ok := w.(interface{ Fd() uintptr }); ok {
		if cols, rows, err := pickerTerminalSize(terminal.Fd()); err == nil && cols > 0 && rows > 0 {
			width, height = cols, rows
		}
	}
	return width, height
}

// The modal renderers draw the picker/board for the host's popup pane.
// The host supplies the popup border, so the output is a bounded sequence
// of logical lines (trailing newline, no border of its own): a title and
// the search on top, one line per visible entry with the selection
// highlighted, the status block and the control footer at the bottom.
// Every line is cut to the popup width in display columns; external fields
// are stripped of control bytes first, so no OSC/CSI escape can leak.

const (
	modalHighlightOn  = "\x1b[7m" // reverse video: theme-neutral selection highlight
	modalHighlightOff = "\x1b[0m"
)

// modalText is the modal's field display: control characters are stripped
// and empty values read as "-", like the legacy pickers.
func modalText(value any) string {
	text := StripPickerControls(pickerString(value))
	if text == "" {
		return "-"
	}
	return text
}

// modalRow joins the columns with double spaces, ellipsizing the last
// column and hard-cutting the result so the line never exceeds budget
// display columns (wide characters count as two).
func modalRow(cols []string, budget int) string {
	if budget <= 0 || len(cols) == 0 {
		return ""
	}
	last := cols[len(cols)-1]
	lead := ""
	if len(cols) > 1 {
		lead = strings.Join(cols[:len(cols)-1], "  ")
	}
	sep := "  "
	lastBudget := budget - displayWidth(lead) - displayWidth(sep)
	if lastBudget < 0 {
		lastBudget = 0
	}
	if displayWidth(last) > lastBudget {
		last = displaySlice(last, lastBudget)
	}
	line := lead
	if line == "" {
		line = last
	} else if last != "" {
		line += sep + last
	}
	if displayWidth(line) > budget {
		line = displaySlice(line, budget)
	}
	return line
}

// modalFooterLine joins the footer segments with " · " from the right,
// dropping the leftmost (least essential) segments until the line fits,
// so the last segment — the cancellation guidance — survives even on a
// tiny popup.
func modalFooterLine(segments []string, width int) string {
	line := ""
	for i := len(segments) - 1; i >= 0; i-- {
		next := segments[i]
		if line != "" {
			next += " · " + line
		}
		if displayWidth(next) <= width {
			line = next
		}
	}
	if line == "" {
		line = displaySlice(segments[len(segments)-1], width)
	}
	return line
}

func modalControls(count int, noun string, width int) string {
	if count != 1 {
		noun += "s"
	}
	cancel := "Esc Close"
	if width < 42 {
		cancel = "Esc"
	}
	return modalFooterLine([]string{
		fmt.Sprintf("%d %s", count, noun), "↑↓ select", "c Copy", "Enter Focus", cancel,
	}, width)
}

// modalLayout assembles the bounded modal: the title and search on top,
// the row window (the selected row kept visible), an optional detail line
// for the selected row, the status block and the footer at the bottom.
// Priority when height is tight: the footer always, then the search, then
// the title; the rows always keep at least one line when there is room,
// and the status block is trimmed from its front (the earliest lines) to
// what fits. Every line is cut to width display columns; the total never
// exceeds height lines.
func modalLayout(title, search string, rows []string, sel int, detail string, status []string, footer string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	cut := func(line string) string {
		if displayWidth(line) > width {
			return displaySlice(line, width)
		}
		return line
	}
	title, search, footer = cut(title), cut(search), cut(footer)
	for i, line := range status {
		status[i] = cut(line)
	}
	detail = cut(detail)
	n := len(rows)
	top := 0
	switch {
	case height >= 3:
		top = 2 // title and search
	case height == 2:
		top = 1 // search only
	}
	middle := height - top - 1 // the footer always fits
	if middle < 0 {
		middle = 0
	}
	statusShown := status
	rowsShown := rows
	if n > 0 {
		statusCap := middle - 1 // leave a line for the viewport when there is one
		if statusCap < 0 {
			statusCap = 0
		}
		if len(status) > statusCap {
			statusShown = status[len(status)-statusCap:]
		}
		rowsCap := middle - len(statusShown)
		if rowsCap < 0 {
			rowsCap = 0
		}
		if sel >= 0 && detail != "" && rowsCap > n {
			rowsCap-- // the spare line becomes the detail line
		}
		rowsShown = nil
		if rowsCap > 0 {
			offset := 0
			if rowsCap < n {
				offset = sel - rowsCap/2
				if offset < 0 {
					offset = 0
				}
				if offset > n-rowsCap {
					offset = n - rowsCap
				}
			}
			end := offset + rowsCap
			if end > n {
				end = n
			}
			rowsShown = rows[offset:end]
		}
	} else {
		if len(status) > middle {
			statusShown = status[len(status)-middle:]
		}
		rowsShown = nil
	}
	lines := make([]string, 0, height)
	if top > 1 {
		lines = append(lines, title)
	}
	if top > 0 {
		lines = append(lines, search)
	}
	lines = append(lines, rowsShown...)
	if n > 0 && sel >= 0 && detail != "" && len(lines) < height-1-len(statusShown) {
		lines = append(lines, detail)
	}
	lines = append(lines, statusShown...)
	lines = append(lines, footer)
	return strings.Join(lines, "\n") + "\n"
}

// pickerModalRow renders one picker entry: ref, name, kind, status and
// workspace/tab, within budget display columns. The " * " / "   " marker
// prefix belongs to the caller; the cwd is left to the detail line.
func pickerModalRow(entry PickerEntry, budget int) string {
	workspace := modalText(entry["workspace_label"]) + "/" + modalText(entry["tab_label"])
	return modalRow([]string{
		modalText(entry["ref"]),
		modalText(entry["name"]),
		modalText(entry["kind"]),
		modalText(entry["status"]),
		workspace,
	}, budget)
}

// pickerModalDetail is the selected entry's ref and cwd as one concise
// line; empty when both are missing.
func pickerModalDetail(entry PickerEntry, width int) string {
	parts := []string{}
	if ref := StripPickerControls(pickerString(entry["ref"])); ref != "" {
		parts = append(parts, ref)
	}
	if cwd := StripPickerControls(pickerString(entry["cwd"])); cwd != "" {
		parts = append(parts, cwd)
	}
	if len(parts) == 0 {
		return ""
	}
	return displaySlice(strings.Join(parts, " · "), width)
}

// unifiedSessionOptions builds the production (enhanced) session popup's
// options from the shared runtime state: every pane - the unknown/empty/
// "-" kinds as terminals - the scope/status/text filters, the pinned
// selection (no cursor at all when the pin is missing), the counts and
// the board totals over the visible unified sessions.
func unifiedSessionOptions(state *BoardState, notice string) SessionRenderOptions {
	sel := state.Selected
	if state.refreshing && state.selectedRef != "" {
		sel = state.pinnedIndex()
	}
	return SessionRenderOptions{
		View:          state.ViewState.View,
		Sort:          state.ViewState.Sort,
		Descending:    state.ViewState.Descending,
		SearchFocused: state.ViewState.SearchFocused,
		Entries:       state.visible(),
		Total:         len(sessionFilterCandidates(state.Entries, state.ViewState)),
		Selected:      sel,
		PinnedMissing: state.pinnedMissing(),
		Query:         state.Query,
		Loading:       state.Loading,
		LoadingLocal:  state.LoadingLocal,
		Failures:      state.Failures,
		Notice:        notice,
		Totals:        boardTotals(state.visible(), state.UpdatedAt),
		Noun:          "session",
		Board:         true,
		Scope:         state.ViewState.Scope,
		Status:        state.ViewState.Status,
	}
}

// renderSessionModal renders one frame of the owned popup from the shared
// runtime state: the unified session popup when the enhanced view is on,
// the command's legacy contract otherwise (the board's totals modal, or
// the picker's "Pick pane" modal when the picker flavor keeps its
// non-TTY contract).
func renderSessionModal(state *BoardState, width, height int, notice string) string {
	if state == nil {
		return ""
	}
	if state.ViewState.Enabled {
		return renderSessionPopup(unifiedSessionOptions(state, notice), width, height)
	}
	if state.flavorPicker {
		return renderPickerLegacyModal(state.visible(), state.Selected, len(state.Entries), state.Query, state.Loading, state.LoadingLocal, state.Failures, notice, width, height)
	}
	return renderBoardModal(state, width, height, notice)
}

// renderPickerLegacyModal is the picker command's non-TTY contract (the
// "Pick pane" modal) over any list: the rows with the full ref, the
// selected row's ref/cwd detail, the loading/failure/empty status and the
// "c Copy · Enter Focus · Esc Close" footer, all bounded to width × height.
func renderPickerLegacyModal(list []PickerEntry, selected, total int, query string, loading int, loadingLocal bool, failures []PickerFailure, notice string, width, height int) string {
	n := len(list)
	sel := -1
	if n > 0 {
		sel = selected
		if sel < 0 {
			sel = 0
		}
		if sel > n-1 {
			sel = n - 1
		}
	}
	rows := make([]string, 0, n)
	for i, entry := range list {
		marker := "  "
		if i == sel {
			marker = " * "
		}
		line := displaySlice(marker+pickerModalRow(entry, width-3), width)
		if i == sel {
			line = modalHighlightOn + line + modalHighlightOff
		}
		rows = append(rows, line)
	}
	detail := ""
	if sel >= 0 {
		detail = pickerModalDetail(list[sel], width)
	}
	var status []string
	if n == 0 && total > 0 {
		status = append(status, fmt.Sprintf("no results for %q", StripPickerControls(query)))
	}
	if loading > 0 {
		if loadingLocal {
			status = append(status, "loading local…")
		} else {
			status = append(status, "loading remote…")
		}
	}
	for _, failure := range failures {
		status = append(status, "machine "+StripPickerControls(failure.Label)+": failed ("+StripPickerControls(failure.Cause)+")")
	}
	if n == 0 && total == 0 && loading == 0 && len(failures) == 0 {
		status = append(status, "no panes")
	}
	if note := StripPickerControls(notice); note != "" {
		status = append(status, note)
	}
	footer := modalControls(n, "pane", width)
	return modalLayout("Pick pane", "> "+StripPickerControls(query), rows, sel, detail, status, footer, width, height)
}

// pickerEnhancedOptions builds the shared production options for the
// picker state type, so the pinned picker render entry points keep the
// same unified session popup as the board runtime.
func pickerEnhancedOptions(state *PickerState, notice string) SessionRenderOptions {
	return SessionRenderOptions{
		View:          state.ViewState.View,
		Sort:          state.ViewState.Sort,
		Descending:    state.ViewState.Descending,
		SearchFocused: state.ViewState.SearchFocused,
		Entries:       state.visible(),
		Total:         len(sessionFilterCandidates(state.Entries, state.ViewState)),
		Selected:      state.Selected,
		Query:         state.Query,
		Loading:       state.Loading,
		LoadingLocal:  state.LoadingLocal,
		Failures:      state.Failures,
		Notice:        notice,
		Totals:        boardTotals(state.visible(), ""),
		Noun:          "session",
		Board:         true,
		Scope:         state.ViewState.Scope,
		Status:        state.ViewState.Status,
	}
}

// renderPickerModal renders the picker popup. When the session view is
// enabled it delegates to the shared session popup renderer (the unified
// session options, like the board runtime); otherwise it keeps the legacy
// modal contract: the "Pick pane" title, the active search, the row
// window around state.Selected, the selected row's ref/cwd detail when a
// line is spare, the loading/failure/empty status and the "c Copy ·
// Enter Focus · Esc Close" footer, all bounded to width × height.
func renderPickerModal(state *PickerState, width, height int, notice string) string {
	if state == nil {
		return ""
	}
	if state.ViewState.Enabled {
		return renderSessionPopup(pickerEnhancedOptions(state, notice), width, height)
	}
	return renderPickerLegacyModal(state.visible(), state.Selected, len(state.Entries), state.Query, state.Loading, state.LoadingLocal, state.Failures, notice, width, height)
}

// renderBoardModal renders the team board popup. When the session view
// is enabled it delegates to the shared session popup renderer (the
// cursor follows the pinned ref during a refresh, and a missing pin
// draws no cursor); otherwise it keeps the legacy contract: the totals
// title, the active search, the row window following the pinned ref
// during a refresh (no cursor at all when the pin is missing), the
// selected row's ref/cwd detail, the loading/failure/empty/pinned-
// missing status and the control footer, all bounded to width × height.
func renderBoardModal(state *BoardState, width, height int, notice string) string {
	if state == nil {
		return ""
	}
	if state.ViewState.Enabled {
		return renderSessionPopup(unifiedSessionOptions(state, notice), width, height)
	}
	list := state.visible()
	n := len(list)
	cursor := -1
	if n > 0 {
		cursor = state.Selected
		if cursor < 0 {
			cursor = 0
		}
		if cursor > n-1 {
			cursor = n - 1
		}
	}
	if state.refreshing && state.selectedRef != "" {
		// While the generation runs the cursor follows the pinned ref,
		// resolved by ref; a missing pin draws no cursor on any row.
		cursor = state.pinnedIndex()
	}
	rows := make([]string, 0, n)
	for i, entry := range list {
		marker := "   "
		if i == cursor {
			marker = ">" + boardMarker(entry) + " "
		}
		workspace := modalText(entry["workspace_label"]) + "/" + modalText(entry["tab_label"])
		workspace = modalText(entry["machine"]) + "/" + workspace
		line := displaySlice(marker+modalRow([]string{
			modalText(entry["name"]),
			modalText(entry["kind"]),
			modalText(entry["status"]),
			workspace,
			BoardTitle(entry["title"]),
		}, width-3), width)
		if i == cursor {
			line = modalHighlightOn + line + modalHighlightOff
		}
		rows = append(rows, line)
	}
	detail := ""
	if cursor >= 0 {
		detail = pickerModalDetail(list[cursor], width)
	}
	agents := boardAgents(state.Entries)
	var status []string
	if n == 0 && len(agents) > 0 {
		status = append(status, fmt.Sprintf("no results for %q", StripPickerControls(state.Query)))
	}
	if state.Loading > 0 {
		if state.LoadingLocal {
			status = append(status, "loading local…")
		} else {
			status = append(status, "loading remote…")
		}
	}
	for _, failure := range state.Failures {
		status = append(status, "machine "+StripPickerControls(failure.Label)+": failed ("+StripPickerControls(failure.Cause)+")")
	}
	if n == 0 && len(agents) == 0 && state.Loading == 0 && len(state.Failures) == 0 {
		status = append(status, "no agents")
	}
	if state.pinnedMissing() {
		status = append(status, "selected session left this load; pick another with ↑/↓")
	}
	if note := StripPickerControls(notice); note != "" {
		status = append(status, note)
	}
	footer := modalControls(n, "agent", width)
	return modalLayout(boardTotals(agents, state.UpdatedAt), "> "+StripPickerControls(state.Query), rows, cursor, detail, status, footer, width, height)
}
