package plugin

import (
	contextpkg "context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/djalmajr/herdr-soho/internal/peer"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

var pickerProcessStarted func(int)

// pickerDiscoveryTimeoutMS is the picker/board's global discovery deadline:
// the local snapshot, the machine list and every remote query together. The
// tests shorten it; the engine defaults to it when it is not positive.
var pickerDiscoveryTimeoutMS = 30_000

var pickerFilterFields = []string{"ref", "name", "kind", "status", "workspace_label", "tab_label", "cwd", "machine"}

type PickerEntry map[string]any

type PickerFailure struct {
	Label string `json:"label"`
	Cause string `json:"cause"`
}

type PickerState struct {
	ViewState    SessionViewState
	Entries      []PickerEntry
	Query        string
	Selected     int
	Loading      int
	LoadingLocal bool
	Failures     []PickerFailure
	LastEntry    PickerEntry
	Copied       *string
	Exit         string
	TerminalKeys // shared raw-input decoder (EscPending/CSIPending)
	// NavPending is true while a navigation is in flight: Enter and
	// Ctrl+Enter are rejected and the frozen target stays in NavTarget.
	NavPending bool
	NavTarget  PickerEntry
	NavFailure *PickerFailure // action feedback survives discovery publications
}

func NewPickerState() *PickerState {
	return &PickerState{Entries: []PickerEntry{}, Failures: []PickerFailure{}, ViewState: SessionViewState{View: sessionViewTable, Sort: sessionSortPane}}
}

func pickerString(value any) string {
	text, _ := value.(string)
	return text
}

func filterPickerEntries(entries []PickerEntry, query string, fields []string) []PickerEntry {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return entries
	}
	out := make([]PickerEntry, 0, len(entries))
	for _, entry := range entries {
		var fieldsText []string
		for _, key := range fields {
			fieldsText = append(fieldsText, strings.ToLower(pickerString(entry[key])))
		}
		joined := strings.Join(fieldsText, " ")
		match := true
		for _, term := range terms {
			if !strings.Contains(joined, term) {
				match = false
				break
			}
		}
		if match {
			out = append(out, entry)
		}
	}
	return out
}

func FilterPickerEntries(entries []PickerEntry, query string) []PickerEntry {
	return filterPickerEntries(entries, query, pickerFilterFields)
}

// visible is the filtered list in the state's order: the legacy order when
// the enhanced view is off, the grouped order otherwise.
func (s *PickerState) visible() []PickerEntry {
	list := FilterPickerEntries(s.Entries, s.Query)
	if s.ViewState.Enabled {
		return sessionVisibleEntries(list, s.ViewState)
	}
	return list
}

func (s *PickerState) clamp() {
	n := len(s.visible())
	if s.Selected > n-1 {
		s.Selected = n - 1
	}
	if s.Selected < 0 {
		s.Selected = 0
	}
}

func StripPickerControls(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		b := text[i]
		if b == 0x1b {
			if i+1 < len(text) && (text[i+1] == '[' || text[i+1] == ']') {
				kind := text[i+1]
				i += 2
				if kind == '[' {
					for i < len(text) && !(text[i] >= 0x40 && text[i] <= 0x7e) {
						i++
					}
					if i < len(text) {
						i++
					}
				} else {
					for i < len(text) && text[i] != 0x07 && !(text[i] == 0x1b && i+1 < len(text) && text[i+1] == '\\') {
						i++
					}
					if i < len(text) {
						if text[i] == 0x07 {
							i++
						} else {
							i += 2
						}
					}
				}
				continue
			}
			i++
			continue
		}
		if b < 0x20 {
			if b == '\t' || b == '\n' {
				out.WriteByte(' ')
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if size == 0 {
			break
		}
		if r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			i += size
			continue
		}
		out.WriteString(text[i : i+size])
		i += size
	}
	return out.String()
}

// PickerCopyPayload returns only the sanitized machine/pane reference.
// An absent reference cannot be copied as a routable identity.
func PickerCopyPayload(entry PickerEntry) string {
	return StripPickerControls(pickerString(entry["ref"]))
}

func jsLength(text string) int { return len(utf16.Encode([]rune(text))) }
func jsSlice(text string, length int) string {
	units := utf16.Encode([]rune(text))
	if length < 0 {
		length = len(units) + length
	}
	if length < 0 {
		length = 0
	}
	if length > len(units) {
		length = len(units)
	}
	return string(utf16.Decode(units[:length]))
}

func PickerEntryLine(entry PickerEntry, width int) string {
	display := func(v any) string {
		s := StripPickerControls(pickerString(v))
		if s == "" {
			return "-"
		}
		return s
	}
	cols := []string{display(entry["ref"]), display(entry["name"]), display(entry["kind"]), display(entry["status"]), display(entry["workspace_label"]) + "/" + display(entry["tab_label"]), display(entry["cwd"])}
	lead := strings.Join(cols[:len(cols)-1], "  ")
	budget := width - jsLength(lead) - 2
	if budget < 0 {
		budget = 0
	}
	last := cols[len(cols)-1]
	if jsLength(last) > budget {
		if budget > 0 {
			last = jsSlice(last, budget-1) + "…"
		} else {
			last = ""
		}
	}
	line := lead + "  " + last
	if jsLength(line) > width {
		line = jsSlice(line, width)
	}
	return line
}

func RenderPicker(state *PickerState, width int) string {
	list := state.visible()
	lines := []string{"> " + state.Query}
	for i, entry := range list {
		marker := " "
		if i == state.Selected {
			marker = "*"
		}
		rowWidth := width - 2
		if rowWidth < 2 {
			rowWidth = 2
		}
		lines = append(lines, marker+" "+PickerEntryLine(entry, rowWidth))
	}
	if len(list) == 0 && len(state.Entries) > 0 {
		lines = append(lines, "nenhum resultado para \""+state.Query+"\"")
	}
	if state.Loading > 0 {
		if state.LoadingLocal {
			lines = append(lines, "carregando local…")
		} else {
			lines = append(lines, "carregando windows…")
		}
	}
	for _, failure := range state.Failures {
		lines = append(lines, "máquina "+StripPickerControls(failure.Label)+": falhou ("+StripPickerControls(failure.Cause)+")")
	}
	if len(state.Entries) == 0 && state.Loading == 0 && len(state.Failures) == 0 {
		lines = append(lines, "nenhum pane")
	}
	count := "panes"
	if len(list) == 1 {
		count = "pane"
	}
	lines = append(lines, fmt.Sprintf("%d %s", len(list), count))
	return strings.Join(lines, "\n") + "\n"
}

func (s *PickerState) ApplyKey(key string) string {
	switch key {
	case "enter", "ctrl-enter":
		// Enter navigates and focuses the selected pane (Ctrl+Enter stays
		// the undocumented compatibility alias); neither copies. The loop
		// runs the existing NavigateSelection safety checks and closes
		// only on a verified successful navigation.
		return s.startNavigation()
	case "c":
		// c copies the selected pane's full reference in the list focus
		// and keeps the modal open (the loop confirms it in the status
		// area); in the enhanced search focus the same key - bare or the
		// kitty encoded printable c - is ordinary search text. The copy is
		// gated while a navigation is pending and on an empty visible
		// list; an entry without a reference fabricates nothing.
		if s.NavPending || s.Exit != "" {
			return ""
		}
		if s.ViewState.Enabled && s.ViewState.SearchFocused {
			s.applyPickerChar("c")
			return ""
		}
		list := s.visible()
		if len(list) == 0 {
			return ""
		}
		entry := list[min(s.Selected, len(list)-1)]
		payload := PickerCopyPayload(entry)
		if payload == "" {
			return ""
		}
		s.LastEntry = entry
		s.Copied = &payload
		return "copy"
	case "update":
		// The board understands the legacy Ctrl+R; the picker ignores it.
		return ""
	case "esc", "ctrl-c":
		if s.Exit == "" {
			s.Exit = "esc"
		}
		return s.Exit
	case "tab":
		// The enhanced popup toggles the search/list focus; the legacy input
		// ignores the key, as before.
		if s.ViewState.Enabled {
			s.ViewState.SearchFocused = !s.ViewState.SearchFocused
		}
		return ""
	case "backspace":
		if s.ViewState.Enabled {
			// Backspace edits in search focus; Tab returns to the list.
			s.ViewState.SearchFocused = true
		}
		s.Query = jsSlice(s.Query, jsLength(s.Query)-1)
		s.clamp()
		return ""
	case "up":
		if len(s.visible()) > 0 && s.Selected > 0 {
			s.Selected--
		}
		return ""
	case "down":
		if len(s.visible()) > 1 && s.Selected < len(s.visible())-1 {
			s.Selected++
		}
		return ""
	default:
		if jsLength(key) == 1 {
			r, _ := utf8.DecodeRuneInString(key)
			if r >= 0x20 && r != 0x7f {
				s.applyPickerChar(key)
			}
		}
		return ""
	}
}

// startNavigation begins the focus navigation of the selected row: it
// freezes the target (NavPending, NavTarget) so Enter and Ctrl+Enter are
// rejected while the navigation is in flight and a second trigger is a
// no-op; the loop runs the existing NavigateSelection safety checks and
// closes only on a verified successful navigation.
func (s *PickerState) startNavigation() string {
	if s.NavPending || s.Exit != "" {
		return ""
	}
	list := s.visible()
	if len(list) == 0 {
		return ""
	}
	entry := list[min(s.Selected, len(list)-1)]
	s.LastEntry = entry
	s.NavPending = true
	s.NavTarget = entry
	return "navigate"
}

// applySortOrViewKey interprets one enhanced list-focus p/w/s/v key: it
// changes the sort or the presentation and keeps the selection on the full
// ref of the row selected before the change. It reports false when the key
// is not a sort/view key.
func (s *PickerState) applySortOrViewKey(key string) bool {
	list := s.visible()
	ref := ""
	if len(list) > 0 {
		ref = pickerString(list[min(s.Selected, len(list)-1)]["ref"])
	}
	switch key {
	case "p":
		sessionApplySort(&s.ViewState, sessionSortPane)
	case "w":
		sessionApplySort(&s.ViewState, sessionSortWorkspace)
	case "s":
		sessionApplySort(&s.ViewState, sessionSortStatus)
	case "v":
		sessionToggleView(&s.ViewState)
	default:
		return false
	}
	s.Selected = sessionKeepSelection(s.visible(), s.Selected, ref)
	return true
}

// applyPickerChar applies one printable character. The legacy input and the
// enhanced search focus append it to the query; the enhanced list focus
// first interprets the p/w/s/v sort/view keys and then enters the search
// with any other character.
func (s *PickerState) applyPickerChar(key string) {
	if s.ViewState.Enabled {
		if !s.ViewState.SearchFocused && s.applySortOrViewKey(key) {
			return
		}
		s.ViewState.SearchFocused = true
	}
	s.Query += key
	s.clamp()
}

// FeedChunk parses raw input with the shared decoder and applies keys in
// order; it stops at the first action, like the terminal loop. Both the
// legacy encodings (CR, 0x03, 0x08/0x7f, bare ESC, CSI A/B) and the
// enhanced ones enabled by the keyboard-protocol push (CSI 13u, CSI 27u,
// CSI 99;5u, CSI 114;5u, CSI 13;5u, CSI 27;5;13~) are recognized; query
// replies, key-release events and unknown CSI are ignored, and a pending
// sequence is bounded.
func (s *PickerState) FeedChunk(chunk string) string {
	for _, r := range chunk {
		key := s.TerminalKeys.Feed(r)
		if key == "" {
			continue
		}
		if action := s.ApplyKey(key); action != "" {
			return action
		}
	}
	return s.Exit
}

func (s *PickerState) FlushEsc() string {
	if s.TerminalKeys.FlushEsc() == "" {
		return ""
	}
	s.ApplyKey("esc")
	return s.Exit
}

func pickerRun(parent contextpkg.Context, exe string, args []string, env platform.Env, platformName string, timeout int, input string) platform.RunResult {
	opts := platform.RunOptions{
		Context:   parent,
		Env:       env,
		OnStart:   pickerProcessStarted,
		Platform:  platformName,
		Input:     input,
		TimeoutMs: timeout,
	}
	if filepath.IsAbs(exe) {
		return platform.RunExecutable(exe, args, opts)
	}
	return platform.RunCli(exe, args, opts)
}

// pickerDiscoveryRun adapts the discovery engine's injectable CLI operation
// to the picker's runner: the engine's context and per-call timeout are
// honored, and HERDR_BIN_PATH selects the herdr binary when it is set.
func pickerDiscoveryRun(env platform.Env, platformName string) func(string, []string, platform.RunOptions) platform.RunResult {
	return func(exe string, args []string, opts platform.RunOptions) platform.RunResult {
		if exe == "herdr" {
			if herdr := env.Get("HERDR_BIN_PATH"); herdr != "" {
				exe = herdr
			}
		}
		parent := opts.Context
		if parent == nil {
			parent = contextpkg.Background()
		}
		return pickerRun(parent, exe, args, env, platformName, opts.TimeoutMs, "")
	}
}

// pickerEntriesFromBatch converts the engine's session rows into the
// picker/board entry maps.
func pickerEntriesFromBatch(batch peer.SessionResult) []PickerEntry {
	out := make([]PickerEntry, 0, len(batch.Entries))
	for _, entry := range batch.Entries {
		out = append(out, PickerEntry{
			"ref": entry.Ref, "machine": entry.Machine,
			"workspace_id": entry.WorkspaceID, "workspace_label": entry.WorkspaceLabel,
			"tab_id": entry.TabID, "tab_label": entry.TabLabel, "pane_id": entry.PaneID,
			"name": entry.Name, "kind": entry.Kind, "status": entry.Status,
			"cwd": entry.Cwd, "title": entry.Title,
		})
	}
	return out
}

// batchMachines names the completed machine, including a successful empty
// snapshot. Older aggregate callers can still identify it from rows/failures.
func batchMachines(batch peer.SessionResult) []string {
	seen := map[string]bool{}
	out := []string{}
	if batch.Machine != "" {
		seen[batch.Machine] = true
		out = append(out, batch.Machine)
	}
	for _, failure := range batch.Failures {
		if !seen[failure.Machine] {
			seen[failure.Machine] = true
			out = append(out, failure.Machine)
		}
	}
	for _, entry := range batch.Entries {
		if !seen[entry.Machine] {
			seen[entry.Machine] = true
			out = append(out, entry.Machine)
		}
	}
	return out
}

func clonePickerState(source *PickerState) *PickerState {
	clone := *source
	clone.Entries = append([]PickerEntry(nil), source.Entries...)
	clone.Failures = append([]PickerFailure(nil), source.Failures...)
	return &clone
}

// loadPickerEntries loads every pane through the shared discovery engine:
// the local snapshot publishes before the machine enumeration and the remote
// snapshots, at most four remote queries run concurrently, and one global
// deadline covers the local snapshot, the machine list and every remote
// query. The load context carries the close cancellation to every
// subprocess; the load never drops old rows itself - it only appends.
func loadPickerEntries(ctx contextpkg.Context, state *PickerState, env platform.Env, platformName string, publish func()) {
	state.LoadingLocal = true
	state.Loading = 1
	publish()
	final := peer.DiscoverSessions(peer.DiscoverOptions{
		Env:         env,
		All:         true,
		TimeoutMS:   pickerDiscoveryTimeoutMS,
		Concurrency: peer.DiscoverMaxConcurrency,
		Context:     ctx,
		Run:         pickerDiscoveryRun(env, platformName),
	}, func(batch peer.SessionResult) {
		if entries := pickerEntriesFromBatch(batch); len(entries) > 0 {
			state.Entries = appendUniquePicker(state.Entries, entries)
		}
		for _, failure := range batch.Failures {
			state.Failures = append(state.Failures, PickerFailure{Label: failure.Machine, Cause: failure.Cause})
		}
		if state.LoadingLocal {
			// The first batch is always the local snapshot; after it the
			// machine list and the remote queries are in flight.
			state.LoadingLocal = false
			state.Loading = 1
		}
		publish()
	})
	if final.ListCause != "" {
		state.Failures = append(state.Failures, PickerFailure{Label: "máquinas", Cause: final.ListCause})
	}
	state.LoadingLocal = false
	state.Loading = 0
	publish()
}

func appendUniquePicker(dest, next []PickerEntry) []PickerEntry {
	refs := make(map[string]bool, len(dest))
	for _, entry := range dest {
		refs[pickerString(entry["ref"])] = true
	}
	for _, entry := range next {
		ref := pickerString(entry["ref"])
		if !refs[ref] {
			refs[ref] = true
			dest = append(dest, entry)
		}
	}
	return dest
}

func redrawArea(w io.Writer, rendered string) {
	// Keep the team's scrolling overlay and frame delimiters separate from
	// the height-bounded native popups. Raw terminals need explicit CRLF.
	_, _ = fmt.Fprint(w, "\x1b[H")
	for _, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
		_, _ = fmt.Fprint(w, line, "\x1b[K\r\n")
	}
	_, _ = fmt.Fprint(w, "\x1b[J")
}

func redrawModal(w io.Writer, rendered string) {
	_, _ = fmt.Fprint(w, "\x1b[H\x1b[J")
	for row, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
		// Raw terminals do not translate LF to CRLF. Absolute positioning
		// also avoids scrolling when the last row fills the viewport.
		_, _ = fmt.Fprintf(w, "\x1b[%d;1H%s", row+1, line)
	}
}

func RunPicker(env platform.Env, platformName, executable string) (code int) {
	stdin, stdout := os.Stdin, os.Stdout
	isTTY := false
	if info, err := stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		isTTY = true
	}
	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, pickerShutdownSignals()...)
	defer signal.Stop(shutdownSignals)
	state := newSessionState(env, platformName, isTTY)
	return withPickerTerminalMouse(isTTY, func() (func() error, error) { return setPickerRaw(stdin) }, stdout, state, func() int {
		return runSessionLoopWithSignals(env, platformName, executable, stdin, stdout, isTTY, shutdownSignals, true, state)
	})
}

// keyboardPushSeq/keyboardPopSeq implement the kitty keyboard-protocol
// lifecycle around the overlay: flag 1 (disambiguate) is pushed so
// Ctrl+Enter arrives as CSI 13;5u instead of plain CR, and popped before
// the exact previous raw mode is restored. The push/pop query responses
// (CSI ? 1 u / CSI ? 0 u) were probed natively on both hosts; the decoder
// ignores them as unknown CSI.
const (
	keyboardPushSeq = "\x1b[>1u"
	keyboardPopSeq  = "\x1b[<u"
)

// The DEC modes the popup reads before it may change mouse tracking: the
// tracking families (9, 1000, 1001, 1002, 1003) and the encoding families
// (1005, 1006, 1015, 1016). Enabling 1000 or 1006 displaces the other
// members of their exclusive family, so their prior states are captured
// too.
var mouseModes = []int{9, 1000, 1001, 1002, 1003, 1005, 1006, 1015, 1016}

// The two modes the popup enables for the vertical wheel: 1000 (button
// events) and 1006 (SGR encoding). They are the required modes of the
// negotiation: without both known, nothing is enabled.
var mouseWheelModes = []int{1000, 1006}

// mouseQueryTimeout bounds the asynchronous DECRQM negotiation inside the
// one input loop: a terminal that never answers keeps its mouse state
// untouched - unknown is never read as off, and the wheel enhancement
// stays unavailable rather than the terminal being corrupted.
const mouseQueryTimeout = time.Second

// mouseNegotiation is the popup's temporary SGR mouse tracking, negotiated
// asynchronously inside the one input loop: the DECRQM answers arrive on
// the input stream (the same reader that feeds the decoder, no second
// stdin reader), and the enables are written only once the required prior
// states are known. Only a mode the DECRQM answers reported reset (2) is
// enabled; set (1) and permanent set (3) are left alone; permanent reset
// (4) and unknown states keep the terminal untouched.
type mouseNegotiation struct {
	decoder   *TerminalKeys
	deadline  time.Time
	decided   bool
	failed    bool
	enabled   []int // wheel modes successfully enabled, in enable order
	displaced []int // prior-set modes the enables displaced, in restore order
}

func newMouseNegotiation(decoder *TerminalKeys) *mouseNegotiation {
	decoder.MouseReplyPending = true
	return &mouseNegotiation{decoder: decoder}
}

// querySeq is the DECRQM query of every mode the popup may touch; a query
// changes nothing.
func (m *mouseNegotiation) querySeq() string {
	var b strings.Builder
	for _, mode := range mouseModes {
		b.WriteString("\x1b[?" + strconv.Itoa(mode) + "$p")
	}
	return b.String()
}

// poll runs the bounded negotiation: once both required modes are known
// (or the timeout runs out) it enables exactly the wheel modes the DECRQM
// answers reported reset, and records the prior-set modes the enables
// displace. A failed write never marks the mode as enabled. It reports
// whether it just decided.
func isMouseMode(mode int) bool {
	for _, candidate := range mouseModes {
		if candidate == mode {
			return true
		}
	}
	return false
}

func (m *mouseNegotiation) poll(w io.Writer) bool {
	if m == nil || m.decided {
		return false
	}
	known := true
	for _, mode := range mouseModes {
		if _, ok := m.decoder.DecrQM[mode]; !ok {
			known = false
			break
		}
	}
	if !known && time.Now().Before(m.deadline) {
		return false
	}
	m.decided = true
	m.decoder.MouseReplyPending = false
	if !known {
		m.failed = true
		return true
	}
	// A query response of zero means unsupported, never an off state.
	for _, mode := range mouseWheelModes {
		prior := m.decoder.DecrQM[mode]
		if prior != 1 && prior != 2 && prior != 3 {
			m.failed = true
			return true
		}
	}
	families := [][]int{{9, 1000, 1001, 1002, 1003}, {1005, 1006, 1015, 1016}}
	for i, mode := range mouseWheelModes {
		if m.decoder.DecrQM[mode] != 2 {
			continue
		}
		for _, alternate := range families[i] {
			if alternate != mode && m.decoder.DecrQM[alternate] == 3 {
				m.failed = true
				return true
			}
		}
	}
	for i, mode := range mouseWheelModes {
		if m.decoder.DecrQM[mode] != 2 {
			continue
		}
		// Record before writing: a failed/partial write may already have
		// reached the terminal. Cleanup conservatively restores known state.
		m.enabled = append(m.enabled, mode)
		m.displace(families[i])
		seq := "\x1b[?" + strconv.Itoa(mode) + "h"
		if n, err := io.WriteString(w, seq); err != nil || n != len(seq) {
			m.failed = true
			m.restore(w)
			return true
		}
	}
	return true
}

// displace records (and will restore on the exit) the prior-set members of
// one exclusive family that a successful enable disables: only a mutable
// set (1) is re-enabled; a permanent set (3) is left to the terminal.
func (m *mouseNegotiation) displace(family []int) {
	for _, mode := range family {
		if m.decoder.DecrQM[mode] == 1 {
			m.displaced = append(m.displaced, mode)
		}
	}
}

func (m *mouseNegotiation) enabledMode(mode int) bool {
	for _, e := range m.enabled {
		if e == mode {
			return true
		}
	}
	return false
}

// wheelAvailable reports whether the vertical wheel can be delivered:
// every required mode is either already on (set or permanent set) or
// successfully enabled by the popup.
func (m *mouseNegotiation) wheelAvailable() bool {
	if m == nil || !m.decided || m.failed {
		return false
	}
	for _, mode := range mouseWheelModes {
		if prior := m.decoder.DecrQM[mode]; prior == 1 || prior == 3 {
			continue
		}
		if !m.enabledMode(mode) {
			return false
		}
	}
	return true
}

// restore undoes exactly what the popup changed, in a coherent order: the
// wheel modes it successfully enabled are disabled first, then the
// prior-set modes the enables displaced are re-enabled. A mode whose prior
// state was never learned, or that was never changed, is not touched - the
// restore never disables a pre-existing mode blindly.
func (m *mouseNegotiation) restore(w io.Writer) {
	if m == nil {
		return
	}
	for _, mode := range m.enabled {
		_, _ = fmt.Fprintf(w, "\x1b[?%dl", mode)
	}
	for _, mode := range m.displaced {
		_, _ = fmt.Fprintf(w, "\x1b[?%dh", mode)
	}
}

func withPickerTerminal(terminal bool, setRaw func() (func() error, error), stdout io.Writer, run func() int) int {
	return withPickerTerminalMouse(terminal, setRaw, stdout, nil, run)
}

// withPickerTerminalMouse is the terminal lifecycle of the owned popups:
// the raw mode and the kitty keyboard-protocol push around the run, and -
// when a state is given - the temporary SGR mouse tracking of the unified
// session runtime, negotiated asynchronously inside the run (the query is
// written here, the bounded decision and the enables happen in the input
// loop through state.Mouse). On every exit path - cancel, signal, normal
// exit and panic - the keyboard is popped, the exact previous mouse modes
// are restored (only what the popup successfully changed; a mode whose
// prior state was never learned is not touched), the exact previous raw
// mode is restored and the cursor is shown again before the panic
// re-raises. The mouse cleanup is registered before any protocol write.
func withPickerTerminalMouse(terminal bool, setRaw func() (func() error, error), stdout io.Writer, state *BoardState, run func() int) (code int) {
	var restore func() error
	var mouse *mouseNegotiation
	keyboardPushed := false
	rawStarted := false
	defer func() {
		if keyboardPushed {
			_, _ = fmt.Fprint(stdout, keyboardPopSeq)
		}
		mouse.restore(stdout)
		if restore != nil {
			_ = restore()
		}
		if rawStarted {
			_, _ = fmt.Fprint(stdout, "\x1b[?25h")
		}
	}()
	if terminal {
		var err error
		restore, err = setRaw()
		if err != nil {
			restore = nil
			return 1
		}
		rawStarted = true
		n, err := io.WriteString(stdout, keyboardPushSeq)
		keyboardPushed = n == len(keyboardPushSeq)
		if err != nil || !keyboardPushed {
			return 1
		}
		if state != nil {
			state.TerminalKeys.DecrQM = nil
			mouse = newMouseNegotiation(&state.TerminalKeys)
			state.Mouse = mouse
			mouse.deadline = time.Now().Add(mouseQueryTimeout)
			seq := mouse.querySeq()
			if n, err := io.WriteString(stdout, seq); err != nil || n != len(seq) {
				mouse.decided, mouse.failed = true, true
				state.MouseReplyPending = false
				return 1
			}
		}
	}
	return run()
}

// PickerTerminal is the exported form of withPickerTerminal so the tests
// can exercise the protocol lifecycle (push/pop order, restore, re-panic)
// without a real TTY. It negotiates no mouse tracking (nil state), as
// before the unified session runtime.
func PickerTerminal(terminal bool, setRaw func() (func() error, error), stdout io.Writer, run func() int) int {
	return withPickerTerminal(terminal, setRaw, stdout, run)
}

// PickerTerminalWithMouse is the exported form of withPickerTerminalMouse
// so the tests can drive the async SGR mouse negotiation (the DECRQM
// answers arrive on the input stream and the bounded decision happens in
// the run through state.Mouse): the exact-mode restore on every exit path,
// including panic, is exercised without a real TTY.
func PickerTerminalWithMouse(terminal bool, setRaw func() (func() error, error), stdout io.Writer, state *BoardState, run func() int) int {
	return withPickerTerminalMouse(terminal, setRaw, stdout, state, run)
}

// runPickerLoopWithSignals is the picker command's entry into the one
// board-derived session runtime (the picker flavor keeps its non-TTY
// display contract); it is a thin adapter, not a second loop.
func runPickerLoopWithSignals(env platform.Env, platformName, executable string, stdin, stdout *os.File, isTTY bool, shutdownSignals <-chan os.Signal) int {
	return runSessionLoopWithSignals(env, platformName, executable, stdin, stdout, isTTY, shutdownSignals, true, NewBoardState())
}

// applyPickerUpdate merges one discovery publication into the live state:
// the publication's rows, failures and loading flags replace the list, while
// the query, selection, presentation, search focus, decoder and action
// state - and the navigation freeze - stay on the live side. The enhanced
// selection follows the full ref of the selected row, so a publication that
// inserts an earlier machine, workspace or pane re-orders the list without
// moving the selection; the legacy selection keeps the numeric index, as
// before.
func applyPickerUpdate(state, update *PickerState) {
	query, selected, exit, copied, lastEntry, keys, viewState, navPending, navTarget, navFailure := state.Query, state.Selected, state.Exit, state.Copied, state.LastEntry, state.TerminalKeys, state.ViewState, state.NavPending, state.NavTarget, state.NavFailure
	ref := ""
	if viewState.Enabled {
		if list := state.visible(); len(list) > 0 {
			ref = pickerString(list[min(selected, len(list)-1)]["ref"])
		}
	}
	*state = *update
	state.Query, state.Selected, state.Exit, state.Copied, state.LastEntry, state.TerminalKeys, state.ViewState, state.NavPending, state.NavTarget, state.NavFailure = query, selected, exit, copied, lastEntry, keys, viewState, navPending, navTarget, navFailure
	if navFailure != nil {
		state.Failures = append(append([]PickerFailure(nil), state.Failures...), *navFailure)
	}
	state.Selected = sessionKeepSelection(state.visible(), state.Selected, ref)
	state.clamp()
}

// navApplyResult folds one navigation outcome into the picker state: a
// success closes; a failure stays visible with the query and selection
// preserved (an actionable modal) and nothing copied.
func navApplyResult(state *PickerState, res NavigationResult) {
	machine := pickerString(state.NavTarget["machine"])
	state.NavPending = false
	state.NavTarget = nil
	if res.OK {
		state.Exit = "navigate"
		return
	}
	state.NavFailure = &PickerFailure{Label: machine, Cause: res.Cause}
	state.Failures = append(state.Failures, *state.NavFailure)
}
