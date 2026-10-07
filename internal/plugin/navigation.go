package plugin

import (
	contextpkg "context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/peer"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/sessionref"
)

// navigationTimeoutMS caps one whole navigation: the fresh snapshot, the
// neighbor verification, the focus call and the final verification share
// this single deadline.
const navigationTimeoutMS = 5_000

// navigationRemoteNote is the English feedback shown when a remote focus
// succeeds: it changes that server's selection; it cannot switch the
// machine the local client shows.
const navigationRemoteNote = "focused %s on machine %s; the local client still shows its current machine"

// csiCap bounds a pending CSI sequence (parameter bytes after "ESC [").
// Real keyboard sequences are far shorter; anything longer is not a key.
const csiCap = 32

// TerminalKeys is the shared bounded raw-input decoder used by the picker
// and the board. It recognizes the legacy encodings (CR/LF, 0x03, 0x12,
// bare ESC, CSI A/B arrows, CSI 5~/6~ page keys) and the enhanced
// encodings that flag 1 (disambiguate) of the kitty keyboard protocol
// re-encodes: CSI 13u (Enter), CSI 27u (Escape), CSI 99;5u (Ctrl+C),
// CSI 114;5u (Ctrl+R), CSI 13;5u (Ctrl+Enter), plus the modifyOtherKeys
// encoding CSI 27;5;13~ of Ctrl+Enter and the SGR mouse reports CSI
// <Cb;Cx;Cy M (vertical wheel only). The standard page keys are CSI 5~
// and CSI 6~, optionally carrying the one-based modifier and :event
// fields of the extended forms; the kitty protocol has no other page
// mapping, so CSI 73u stays the literal letter I. DECRQM answers
// (CSI ? Ps ; Pm $ y) of the mouse modes the popup negotiates are
// recorded (first answer wins), query replies, key-release events and
// unknown CSI are ignored - never treated as user actions or search
// characters. An X10 mouse report (CSI M) consumes its three following
// bytes without leaking them.
type TerminalKeys struct {
	EscPending bool // bare ESC seen; a following '[' opens a CSI sequence
	CSIPending bool // inside a CSI sequence
	buf        [csiCap]byte
	n          int
	discardCSI bool // oversized sequence: consume through its final byte
	x10Mouse   int  // X10 CSI M report: the following bytes to consume
	// DecrQM records, from the DECRQM answers seen on the input stream,
	// the state each DEC mode was in before the popup may change it
	// (1 set, 2 reset, 3 permanent set, 4 permanent reset); the first
	// answer of a mode wins, so a late or unsolicited answer never
	// overwrites the initial state.
	DecrQM            map[int]int
	discardPageTail   bool // malformed extended page report: consume through tilde
	MouseReplyPending bool // accept only replies to this popup's pending bounded query
}

// Feed classifies one raw input rune. It returns a key token ("enter",
// "esc", "backspace", "ctrl-c", "ctrl-enter", "update", "up", "down",
// "page-up", "page-down", "wheel-up", "wheel-down") or a printable
// character, or "" for the bytes that must be ignored.
func (d *TerminalKeys) Feed(r rune) string {
	if d.discardPageTail {
		if r == '~' {
			d.discardPageTail = false
			return ""
		}
		if r >= 0x20 && r != 0x7f {
			return ""
		}
		d.discardPageTail = false
	}

	if d.x10Mouse > 0 {
		// The X10 mouse report's three following bytes (ch, cx, cy) are
		// consumed whatever they are, so they never leak into the search.
		d.x10Mouse--
		return ""
	}
	if d.CSIPending {
		switch {
		case r >= 0x40 && r <= 0x7e:
			final := byte(r)
			params := d.buf[:d.n]
			if len(params) > 0 && params[0] == '<' && final != 'M' && final != 'm' && final != '~' {
				d.CSIPending, d.discardCSI, d.n = false, false, 0
				return ""
			}
			if final != '~' && (strings.HasPrefix(string(params), "5;") || strings.HasPrefix(string(params), "6;")) && strings.Contains(string(params), ":") {
				d.CSIPending, d.discardCSI, d.n = false, false, 0
				d.discardPageTail = true
				return ""
			}
			d.CSIPending = false
			d.n = 0
			if d.discardCSI {
				d.discardCSI = false
				return ""
			}
			if final == 'y' {
				d.decrqmAnswer(params)
				return ""
			}
			if final == 'M' && len(params) == 0 {
				// An X10 mouse report: CSI M followed by three bytes.
				d.x10Mouse = 3
				return ""
			}
			return csiKey(params, final)
		case r >= 0x20 && r < 0x40:
			if d.discardCSI || d.n >= csiCap {
				// Do not leak the tail of an unknown sequence into search.
				d.discardCSI = true
			} else {
				d.buf[d.n] = byte(r)
				d.n++
			}
			return ""
		default:
			// A control key aborts an incomplete CSI. Reprocess it so Esc,
			// Ctrl+C and Enter remain usable after malformed terminal input.
			d.CSIPending, d.discardCSI, d.n = false, false, 0
			return d.Feed(r)
		}
	}
	if d.EscPending {
		d.EscPending = false
		if r == '[' {
			d.CSIPending = true
			d.n = 0
			return ""
		}
		// Legacy standalone ESC: the following byte is not a sequence.
		return "esc"
	}
	switch r {
	case 0x1b:
		d.EscPending = true
		return ""
	case '\r', '\n':
		return "enter"
	case 0x7f, 0x08:
		return "backspace"
	case 0x03:
		return "ctrl-c"
	case 0x12:
		return "update"
	case '\t':
		return "tab"
	default:
		if r >= 0x20 && r != 0x7f {
			return string(r)
		}
		return ""
	}
}

// FlushEsc resolves a bare ESC that got nothing after it.
func (d *TerminalKeys) FlushEsc() string {
	if !d.EscPending {
		return ""
	}
	d.EscPending = false
	return "esc"
}

// csiKey decides the meaning of a completed CSI sequence.
func csiKey(params []byte, final byte) string {
	switch final {
	case 'A':
		if arrowKey(params) {
			return "up"
		}
	case 'B':
		if arrowKey(params) {
			return "down"
		}
	case 'u':
		return kittyKey(params)
	case '~':
		return tildeKey(params)
	case 'M':
		return sgrMouseKey(params) // SGR press; the release (m) is ignored
	default:
		return "" // unknown CSI: never a user action (incl. SGR 'm' release)
	}
	return ""
}

func arrowKey(params []byte) bool {
	if len(params) == 0 || string(params) == "1" {
		return true
	}
	fields := strings.Split(string(params), ";")
	if len(fields) != 2 || fields[0] != "1" {
		return false
	}
	_, event, ok := keyboardModifiers(fields[1])
	return ok && event != 3
}

// csiFieldInt reads one parameter field; -1 when missing or not numeric.
func csiFieldInt(fields []string, i int) int {
	if i >= len(fields) || fields[i] == "" {
		return -1
	}
	v, err := strconv.Atoi(fields[i])
	if err != nil {
		return -1
	}
	return v
}

// keyboardModifiers decodes the protocol's one-based modifier mask and
// optional :event (1 press, 2 repeat, 3 release). Caps/Num Lock are state
// bits, not events; they must not change the meaning of Ctrl+Enter.
func keyboardModifiers(field string) (mods, event int, ok bool) {
	parts := strings.Split(field, ":")
	if len(parts) > 2 {
		return 0, 0, false
	}
	encoded := 1
	var err error
	if parts[0] != "" {
		encoded, err = strconv.Atoi(parts[0])
		if err != nil || encoded < 1 || encoded > 256 {
			return 0, 0, false
		}
	}
	event = 1
	if len(parts) == 2 {
		event, err = strconv.Atoi(parts[1])
		if err != nil || event < 1 || event > 3 {
			return 0, 0, false
		}
	}
	return (encoded - 1) &^ (64 | 128), event, true
}

// kittyKey decodes CSI <codepoint>[;<modifiers>[:event]]u. Release events
// and modified text keys are ignored, never typed or treated as actions.
func kittyKey(params []byte) string {
	fields := strings.Split(string(params), ";")
	if len(fields) > 2 {
		return ""
	}
	code := csiFieldInt(fields, 0)
	if code <= 0 {
		return ""
	}
	modifierField := ""
	if len(fields) > 1 {
		modifierField = fields[1]
	}
	mod, event, ok := keyboardModifiers(modifierField)
	if !ok || event == 3 {
		return ""
	}
	ctrl := mod == 4
	switch code {
	case 9: // Tab, plain
		if mod == 0 {
			return "tab"
		}
		return ""
	case 13: // Enter
		if ctrl {
			return "ctrl-enter"
		}
		if mod == 0 {
			return "enter"
		}
		return ""
	case 27: // Escape, plain
		if mod == 0 {
			return "esc"
		}
		return ""
	case 99: // c
		if ctrl {
			return "ctrl-c"
		}
		if mod == 0 {
			return "c"
		}
		return ""
	case 114: // r
		if ctrl {
			return "update"
		}
		if mod == 0 {
			return "r"
		}
		return ""
	}
	if mod != 0 {
		return ""
	}
	if code >= 0x20 && code <= 0x7e {
		return string(rune(code))
	}
	return ""
}

// tildeKey decodes modifyOtherKeys CSI 27;<modifiers>;<keycode>~, the
// Ctrl-combined Enter (keycode 13), and the standard page keys CSI 5~ / CSI
// 6~: plain, or with the one-based modifier field and the :event field of
// the extended forms (PageUp is 5 and PageDown is 6 in the kitty protocol
// functional-key definitions). Releases (event 3) are ignored, and 1 is
// Home, never a page key.
func tildeKey(params []byte) string {
	fields := strings.Split(string(params), ";")
	if fields[0] == "5" || fields[0] == "6" {
		switch len(fields) {
		case 1:
		case 2:
			if !tildeModifierEventOK(fields[1]) {
				return ""
			}
		default:
			return ""
		}
		if fields[0] == "5" {
			return "page-up"
		}
		return "page-down"
	}
	if len(fields) != 3 || fields[0] != "27" {
		return ""
	}
	mod, event, ok := keyboardModifiers(fields[1])
	key := csiFieldInt(fields, 2)
	if !ok || key <= 0 {
		return ""
	}
	if event == 3 || mod != 4 {
		return "" // release or a non-Ctrl combination
	}
	if key == 13 {
		return "ctrl-enter"
	}
	return ""
}

// tildeModifierEventOK is the modifier field of the extended tilde forms:
// the one-based modifier (1 none .. 8 all, the xterm modifyOtherKeys
// convention), optionally followed by :event; only press (1) and repeat (2)
// count - a release (3) or any other value is not a page operation.
func tildeModifierEventOK(field string) bool {
	modField, event, hasEvent := strings.Cut(field, ":")
	if v, err := strconv.Atoi(modField); err != nil || v < 1 || v > 8 {
		return false
	}
	if !hasEvent {
		return true
	}
	e, err := strconv.Atoi(event)
	return err == nil && e >= 1 && e <= 2
}

// sgrMouseKey decodes the SGR mouse report CSI < Cb ; Cx ; Cy M: the
// button first, then the positive 1-based column and row (the xterm wheel
// mice encoding: 64 = wheel up, 65 = wheel down, plus the Shift/Ctrl/Meta
// modifier bits 4/8/16). The '<' prefix is required; only the press (M)
// reaches here. Motion (bit 32), the unknown bit (128), clicks, the
// horizontal wheel and zero/negative/overflowing coordinates are ignored
// - the report never leaks into the search.
func sgrMouseKey(params []byte) string {
	text := strings.TrimPrefix(string(params), "<")
	if text == string(params) {
		return "" // no '<': not an SGR report
	}
	fields := strings.Split(text, ";")
	if len(fields) != 3 {
		return ""
	}
	cb, err := strconv.Atoi(fields[0])
	if err != nil || cb < 0 || cb > 255 {
		return ""
	}
	cx, err := strconv.Atoi(fields[1])
	if err != nil || cx < 1 {
		return "" // coordinates are positive 1-based values
	}
	cy, err := strconv.Atoi(fields[2])
	if err != nil || cy < 1 {
		return ""
	}
	if cb&(32|128) != 0 {
		return "" // motion or unknown bits: not a plain wheel press
	}
	if cb&64 == 0 {
		return "" // a click or release, not a wheel
	}
	switch cb & 3 {
	case 0:
		return "wheel-up"
	case 1:
		return "wheel-down"
	}
	return "" // horizontal wheel
}

// decrqmAnswer records one DECRQM answer in the exact form CSI ? Ps ; Pm
// $ y: one mode and one state (1 set, 2 reset, 3 permanent set, 4
// permanent reset), tracked separately. The first answer of a mode wins, so
// a late or unsolicited answer never overwrites the initial state the
// popup read.
func (d *TerminalKeys) decrqmAnswer(params []byte) {
	if !d.MouseReplyPending {
		return
	}

	text := string(params)
	// The '$' intermediate byte sits in the CSI parameter range, so it is
	// already buffered; the exact form is "?Ps;Pm$".
	if !strings.HasPrefix(text, "?") || !strings.HasSuffix(text, "$") {
		return
	}
	fields := strings.Split(text[1:len(text)-1], ";")
	if len(fields) != 2 {
		return
	}
	mode, err := strconv.Atoi(fields[0])
	if err != nil || !isMouseMode(mode) {
		return
	}
	value, err := strconv.Atoi(fields[1])
	if err != nil || value < 0 || value > 4 {
		return
	}
	if d.DecrQM == nil {
		d.DecrQM = map[int]int{}
	}
	if _, seen := d.DecrQM[mode]; !seen {
		d.DecrQM[mode] = value
	}
}

// NavigationResult is the outcome of one navigation attempt.
type NavigationResult struct {
	OK     bool
	Remote bool   // the target lives on a saved machine
	Cause  string // failure cause, shown in the overlay
	Note   string // English remote-display note, shown in a toast
}

// navBudget bounds every call of one navigation with the shared deadline.
type navBudget struct {
	ctx contextpkg.Context
}

// timeoutMS returns the time left in the shared deadline, in ms (>= 1).
func (b *navBudget) timeoutMS() int {
	deadline, ok := b.ctx.Deadline()
	if !ok {
		return navigationTimeoutMS
	}
	ms := int(time.Until(deadline) / time.Millisecond)
	if ms < 1 {
		ms = 1
	}
	return ms
}

type navRunner func(exe string, args []string, opts platform.RunOptions) platform.RunResult

// runCLI executes one herdr CLI call under the shared deadline and reports
// a cause when the call could not be trusted.
func (b *navBudget) runCLI(run navRunner, args []string) (platform.RunResult, string) {
	if err := b.ctx.Err(); err != nil {
		return platform.RunResult{}, "navigation cancelled"
	}
	res := run("herdr", args, platform.RunOptions{Context: b.ctx, TimeoutMs: b.timeoutMS()})
	if err := b.ctx.Err(); err != nil {
		cause := "navigation cancelled"
		if errors.Is(err, contextpkg.DeadlineExceeded) {
			cause = fmt.Sprintf("navigation exceeded its %d s deadline", navigationTimeoutMS/1000)
		}
		return res, cause
	}
	switch {
	case res.NotFound:
		return res, "herdr executable was not found"
	case res.TimedOut:
		return res, fmt.Sprintf("navigation exceeded its %d s deadline", navigationTimeoutMS/1000)
	case res.Error != "":
		return res, "command failed"
	}
	if res.Status == nil || *res.Status != 0 {
		code := -1
		if res.Status != nil {
			code = *res.Status
		}
		return res, fmt.Sprintf("command failed (exit %d)", code)
	}
	return res, ""
}

// snapshot runs a fresh `api snapshot` on the target machine and returns
// the snapshot object; a stale or malformed response is rejected.
func (b *navBudget) snapshot(run navRunner, machineArgs []string) (map[string]any, string) {
	res, cause := b.runCLI(run, append(machineArgs, "api", "snapshot"))
	if cause != "" {
		return nil, cause
	}
	return parseNavigationSnapshot(res.Stdout)
}

func parseNavigationSnapshot(stdout string) (map[string]any, string) {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		return nil, "snapshot is not valid JSON"
	}
	result, _ := decoded["result"].(map[string]any)
	if result == nil {
		return nil, "snapshot response is missing the expected result"
	}
	if t, _ := result["type"].(string); t != "session_snapshot" {
		return nil, "unexpected snapshot response type"
	}
	snap, ok := result["snapshot"].(map[string]any)
	if !ok {
		return nil, "snapshot is missing the pane data"
	}
	return snap, ""
}

// navigationFreshPane looks the pane up in the fresh snapshot and returns
// its live identity (kind, tab, workspace) - never the stale row.
func navigationFreshPane(snap map[string]any, machine, paneID string) (peer.SessionEntry, bool) {
	for _, e := range peer.SessionEntries(snap, machine) {
		if e.PaneID == paneID {
			return e, true
		}
	}
	return peer.SessionEntry{}, false
}

// agentFocus focuses a live agent pane by pane ID and verifies the
// response names the target as focused.
func (b *navBudget) agentFocus(run navRunner, machineArgs []string, paneID string) string {
	res, cause := b.runCLI(run, append(machineArgs, "agent", "focus", paneID))
	if cause != "" {
		return cause
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &decoded); err != nil {
		return "agent focus could not be verified"
	}
	result, _ := decoded["result"].(map[string]any)
	agent, _ := result["agent"].(map[string]any)
	id, _ := agent["pane_id"].(string)
	focused, _ := agent["focused"].(bool)
	if id != paneID || !focused {
		return "agent focus could not be verified"
	}
	return ""
}

type navRect struct{ x, y, w, h int }

type navPaneGeom struct {
	id   string
	rect navRect
}

func navigationRect(v any) navRect {
	r, _ := v.(map[string]any)
	num := func(k string) int {
		f, _ := r[k].(float64)
		return int(f)
	}
	return navRect{x: num("x"), y: num("y"), w: num("width"), h: num("height")}
}

// navigationTabPanes returns the panes (with fresh rects) of the target's
// tab from the snapshot's layouts.
func navigationTabPanes(snap map[string]any, tabID string) ([]navPaneGeom, bool) {
	layouts, _ := snap["layouts"].([]any)
	for _, lv := range layouts {
		layout, _ := lv.(map[string]any)
		if layout == nil {
			continue
		}
		if id, _ := layout["tab_id"].(string); id != tabID {
			continue
		}
		panesAny, _ := layout["panes"].([]any)
		panes := make([]navPaneGeom, 0, len(panesAny))
		for _, pv := range panesAny {
			p, _ := pv.(map[string]any)
			if p == nil {
				continue
			}
			id, _ := p["pane_id"].(string)
			if id == "" {
				continue
			}
			panes = append(panes, navPaneGeom{id: id, rect: navigationRect(p["rect"])})
		}
		return panes, true
	}
	return nil, false
}

type navCandidate struct {
	source string
	dir    string
}

// navigationCandidates shortlists, from the fresh layout rects, the panes
// that share an edge with the target. Each candidate is still verified
// with a `pane neighbor` call before any focus is attempted.
func navigationCandidates(panes []navPaneGeom, targetID string) []navCandidate {
	var target *navPaneGeom
	for i := range panes {
		if panes[i].id == targetID {
			target = &panes[i]
			break
		}
	}
	if target == nil {
		return nil
	}
	overlapX := func(a, b navRect) int {
		return min(a.x+a.w, b.x+b.w) - max(a.x, b.x)
	}
	overlapY := func(a, b navRect) int {
		return min(a.y+a.h, b.y+b.h) - max(a.y, b.y)
	}
	out := []navCandidate{}
	for _, p := range panes {
		if p.id == targetID {
			continue
		}
		switch {
		case p.rect.x+p.rect.w == target.rect.x && overlapY(p.rect, target.rect) > 0:
			// p is on the target's left: the target is p's right neighbor.
			out = append(out, navCandidate{source: p.id, dir: "right"})
		case target.rect.x+target.rect.w == p.rect.x && overlapY(p.rect, target.rect) > 0:
			out = append(out, navCandidate{source: p.id, dir: "left"})
		case p.rect.y+p.rect.h == target.rect.y && overlapX(p.rect, target.rect) > 0:
			out = append(out, navCandidate{source: p.id, dir: "down"})
		case target.rect.y+target.rect.h == p.rect.y && overlapX(p.rect, target.rect) > 0:
			out = append(out, navCandidate{source: p.id, dir: "up"})
		}
	}
	return out
}

func navigationNeighborIs(stdout, target string) bool {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		return false
	}
	result, _ := decoded["result"].(map[string]any)
	neighbor, _ := result["neighbor"].(map[string]any)
	id, _ := neighbor["neighbor_pane_id"].(string)
	return id == target
}

func navigationFocusIs(stdout, target string) bool {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		return false
	}
	result, _ := decoded["result"].(map[string]any)
	focus, _ := result["focus"].(map[string]any)
	id, _ := focus["focused_pane_id"].(string)
	return id == target
}

func navigationTabFocusIs(stdout, tabID string) bool {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		return false
	}
	result, _ := decoded["result"].(map[string]any)
	tab, _ := result["tab"].(map[string]any)
	id, _ := tab["tab_id"].(string)
	focused, _ := tab["focused"].(bool)
	return id == tabID && focused
}

// shellFocus focuses a live shell pane: a single-pane tab uses `tab
// focus`; otherwise a source in the same live tab is verified with
// `pane neighbor` and focused with `pane focus --pane source --direction`.
func (b *navBudget) shellFocus(run navRunner, machineArgs []string, snap map[string]any, paneID, tabID string) string {
	panes, ok := navigationTabPanes(snap, tabID)
	if !ok {
		return "tab layout was not found in the snapshot"
	}
	if len(panes) == 1 {
		res, cause := b.runCLI(run, append(machineArgs, "tab", "focus", tabID))
		if cause != "" {
			return cause
		}
		if !navigationTabFocusIs(res.Stdout, tabID) {
			return "tab focus could not be verified"
		}
		return ""
	}
	for _, cand := range navigationCandidates(panes, paneID) {
		nres, cause := b.runCLI(run, append(machineArgs, "pane", "neighbor", "--direction", cand.dir, "--pane", cand.source))
		if cause != "" {
			return cause
		}
		if !navigationNeighborIs(nres.Stdout, paneID) {
			continue
		}
		fres, cause := b.runCLI(run, append(machineArgs, "pane", "focus", "--pane", cand.source, "--direction", cand.dir))
		if cause != "" {
			return cause
		}
		if navigationFocusIs(fres.Stdout, paneID) {
			return ""
		}
	}
	return "no verified neighbor leads to the selected pane"
}

func navigationFocused(snap map[string]any, paneID, tabID, workspaceID string) bool {
	panes, _ := snap["focused_pane_id"].(string)
	tabs, _ := snap["focused_tab_id"].(string)
	workspaces, _ := snap["focused_workspace_id"].(string)
	return panes == paneID && tabs == tabID && workspaces == workspaceID
}

// NavigateSelection focuses the live pane of one selected entry using the
// supported CLI surface. It cross-checks the exact ref/machine/pane ID of
// the row, re-reads the target's identity from a fresh snapshot on that
// machine (never the stale name), fails without any focus operation when
// the pane is gone, focuses agent panes with `agent focus <pane_id>` and
// shell panes with a verified neighbor + `pane focus` (or `tab focus` on
// a single-pane tab), and only succeeds when a final snapshot shows the
// intended workspace/tab/pane focused. Every call shares one
// navigationTimeoutMS deadline carried by ctx; nothing else is invoked
// (no clipboard, no text delivery, no key injection).
func NavigateSelection(ctx contextpkg.Context, entry PickerEntry, env platform.Env, platformName string) NavigationResult {
	deadlineCtx, stop := contextpkg.WithTimeout(ctx, navigationTimeoutMS*time.Millisecond)
	defer stop()
	b := navBudget{ctx: deadlineCtx}
	run := pickerDiscoveryRun(env, platformName)

	ref := pickerString(entry["ref"])
	machine := pickerString(entry["machine"])
	paneID := pickerString(entry["pane_id"])
	if machine == "" {
		machine = sessionref.LocalMachine
	}
	parsed := sessionref.ParseRef(ref)
	if parsed == nil || parsed.Machine != machine || parsed.PaneID != paneID {
		return NavigationResult{Cause: "selected reference does not match its machine and pane"}
	}
	machineArgs := sessionref.HerdrMachineArgs(machine)

	snap, cause := b.snapshot(run, machineArgs)
	if cause != "" {
		return NavigationResult{Cause: cause}
	}
	fresh, ok := navigationFreshPane(snap, machine, paneID)
	if !ok {
		return NavigationResult{Cause: "selected pane no longer exists on this machine"}
	}
	kind := pickerString(fresh.Kind)
	tabID := pickerString(fresh.TabID)
	workspaceID := pickerString(fresh.WorkspaceID)

	var focusCause string
	if kind != "" {
		focusCause = b.agentFocus(run, machineArgs, paneID)
	} else {
		focusCause = b.shellFocus(run, machineArgs, snap, paneID, tabID)
	}
	if focusCause != "" {
		return NavigationResult{Cause: focusCause}
	}

	final, cause := b.snapshot(run, machineArgs)
	if cause != "" {
		return NavigationResult{Cause: cause}
	}
	if !navigationFocused(final, paneID, tabID, workspaceID) {
		return NavigationResult{Cause: "final pane focus could not be verified"}
	}
	note := ""
	remote := machine != sessionref.LocalMachine
	if remote {
		note = fmt.Sprintf(navigationRemoteNote, ref, machine)
	}
	return NavigationResult{OK: true, Remote: remote, Note: note}
}
