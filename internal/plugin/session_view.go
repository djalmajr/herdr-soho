package plugin

import (
	"sort"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/sessionref"
)

// SessionViewState holds the presentation and input focus of a native
// popup. Enabled leaves the legacy non-TTY input and ordering contracts
// intact.
type SessionViewState struct {
	Enabled       bool
	View          string
	Sort          string
	Descending    bool
	SearchFocused bool
	// Scope keeps the unified session list on all sessions, the agent
	// panes or the terminal panes; f cycles it in the list focus.
	Scope string
	// Status keeps the unified session list on all statuses or one of
	// them; t cycles it in the list focus, and "unknown" is the rest.
	Status string
}

// The enhanced popup's presentation views and sort keys.
const (
	sessionViewTable     = "table"
	sessionViewTree      = "tree"
	sessionSortPane      = "pane"
	sessionSortWorkspace = "workspace"
	sessionSortStatus    = "status"
)

// The unified session list's scope and status filters. Empty values read
// as "all", like the view and sort defaults.
const (
	sessionScopeAll       = "all"
	sessionScopeAgents    = "agents"
	sessionScopeTerminals = "terminals"

	sessionStatusAll     = "all"
	sessionStatusWorking = "working"
	sessionStatusBlocked = "blocked"
	sessionStatusIdle    = "idle"
	sessionStatusDone    = "done"
	sessionStatusUnknown = "unknown"
)

// sessionKind is the entry's sanitized kind.
func sessionKind(entry PickerEntry) string {
	return StripPickerControls(pickerString(entry["kind"]))
}

// sessionAgentOf is the unified display's classification: a pane with a
// real agent kind is an agent; the unknown string kind ("unknown"), an
// empty kind and "-" are terminals.
func sessionAgentOf(entry PickerEntry) bool {
	kind := sessionKind(entry)
	return kind != "" && kind != "-" && kind != "unknown"
}

// sessionAgentLabel is the table's agent cell: the kind, or "terminal"
// for the unknown/empty/"-" kinds.
func sessionAgentLabel(entry PickerEntry) string {
	if sessionAgentOf(entry) {
		return sessionKind(entry)
	}
	return "terminal"
}

// sessionScopeMatches applies the scope filter; empty reads "all".
func sessionScopeMatches(entry PickerEntry, scope string) bool {
	switch scope {
	case sessionScopeAgents:
		return sessionAgentOf(entry)
	case sessionScopeTerminals:
		return !sessionAgentOf(entry)
	default:
		return true
	}
}

// sessionStatusMatches applies the status filter; "unknown" is every
// status that is not one of the four named ones (including the empty
// status).
func sessionStatusMatches(entry PickerEntry, status string) bool {
	actual := StripPickerControls(pickerString(entry["status"]))
	switch status {
	case sessionStatusUnknown:
		switch actual {
		case sessionStatusWorking, sessionStatusBlocked, sessionStatusIdle, sessionStatusDone:
			return false
		}
		return true
	case sessionStatusWorking, sessionStatusBlocked, sessionStatusIdle, sessionStatusDone:
		return actual == status
	default:
		return true
	}
}

// sessionFilterCandidates is the scope and status filter without the text
// query: the candidates the visible unified list is narrowed from.
func sessionFilterCandidates(entries []PickerEntry, vs SessionViewState) []PickerEntry {
	out := make([]PickerEntry, 0, len(entries))
	for _, entry := range entries {
		if sessionScopeMatches(entry, vs.Scope) && sessionStatusMatches(entry, vs.Status) {
			out = append(out, entry)
		}
	}
	return out
}

// sessionFilterEntries is the unified session filter: scope, status and
// the text query (board fields, task title included), combined in that
// order.
func sessionFilterEntries(entries []PickerEntry, query string, vs SessionViewState) []PickerEntry {
	return filterPickerEntries(sessionFilterCandidates(entries, vs), query, boardFilterFields)
}

// sessionScopeCycle is the f cycle of the list focus: all, agents,
// terminals.
func sessionScopeCycle(scope string) string {
	switch scope {
	case "", sessionScopeAll:
		return sessionScopeAgents
	case sessionScopeAgents:
		return sessionScopeTerminals
	default:
		return sessionScopeAll
	}
}

// sessionStatusCycle is the t cycle of the list focus: all, working,
// blocked, idle, done, unknown.
func sessionStatusCycle(status string) string {
	switch status {
	case "", sessionStatusAll:
		return sessionStatusWorking
	case sessionStatusWorking:
		return sessionStatusBlocked
	case sessionStatusBlocked:
		return sessionStatusIdle
	case sessionStatusIdle:
		return sessionStatusDone
	case sessionStatusDone:
		return sessionStatusUnknown
	default:
		return sessionStatusAll
	}
}

// sessionScopeLabel is the footer label of the active scope filter.
func sessionScopeLabel(scope string) string {
	switch scope {
	case sessionScopeAgents:
		return "Agents"
	case sessionScopeTerminals:
		return "Terminals"
	default:
		return "All"
	}
}

// sessionStatusLabel is the footer label of the active status filter.
func sessionStatusLabel(status string) string {
	switch status {
	case sessionStatusWorking:
		return "Working"
	case sessionStatusBlocked:
		return "Blocked"
	case sessionStatusIdle:
		return "Idle"
	case sessionStatusDone:
		return "Done"
	case sessionStatusUnknown:
		return "Unknown"
	default:
		return "All"
	}
}

// sessionWorkspaceKey keeps equally named workspaces distinct when IDs exist.
func sessionWorkspaceKey(entry PickerEntry) string {
	if id := pickerString(entry["workspace_id"]); id != "" {
		return "id:" + id
	}
	return "label:" + pickerString(entry["workspace_label"])
}

func sessionPaneName(entry PickerEntry) string {
	for _, field := range []string{"name", "title", "pane_id", "ref"} {
		if text := StripPickerControls(pickerString(entry[field])); text != "" {
			return text
		}
	}
	return "-"
}

// sessionMachineOf is the entry's machine; a missing machine reads as the
// local one, like the navigation does.
func sessionMachineOf(entry PickerEntry) string {
	if machine := pickerString(entry["machine"]); machine != "" {
		return machine
	}
	return sessionref.LocalMachine
}

// sessionStatusRank ranks statuses for the s sort: blocked, working, idle,
// done; empty and unrecognized statuses rank last.
func sessionStatusRank(entry PickerEntry) int {
	switch StripPickerControls(pickerString(entry["status"])) {
	case "blocked":
		return 0
	case "working":
		return 1
	case "idle":
		return 2
	case "done":
		return 3
	default:
		return 4
	}
}

// sessionSortRow is one entry's sortable keys: the machine group (local
// first, the other machines by label), the workspace group (label, then
// identity) and the item keys (status rank, pane name, full ref).
type sessionSortRow struct {
	entry   PickerEntry
	machine int
	wsGroup [3]string
	status  int
	pane    string
	paneRaw string
	ref     string
}

func sessionSortRows(entries []PickerEntry) []sessionSortRow {
	machines := map[string]bool{}
	for _, entry := range entries {
		machines[sessionMachineOf(entry)] = true
	}
	other := make([]string, 0, len(machines))
	for machine := range machines {
		if machine != sessionref.LocalMachine {
			other = append(other, machine)
		}
	}
	// The other machines are ranked by label case-insensitively, with the
	// raw label as the deterministic tie-break.
	sort.Slice(other, func(i, j int) bool {
		li, lj := strings.ToLower(other[i]), strings.ToLower(other[j])
		if li != lj {
			return li < lj
		}
		return other[i] < other[j]
	})
	rank := map[string]int{sessionref.LocalMachine: 0}
	for i, machine := range other {
		rank[machine] = i + 1
	}
	rows := make([]sessionSortRow, 0, len(entries))
	for _, entry := range entries {
		pane := sessionPaneName(entry)
		label := StripPickerControls(pickerString(entry["workspace_label"]))
		rows = append(rows, sessionSortRow{
			entry:   entry,
			machine: rank[sessionMachineOf(entry)],
			wsGroup: [3]string{strings.ToLower(label), sessionWorkspaceKey(entry), label},
			status:  sessionStatusRank(entry),
			pane:    strings.ToLower(pane),
			paneRaw: pane,
			ref:     pickerString(entry["ref"]),
		})
	}
	return rows
}

// sessionLess compares two sortable rows for the given view state. The
// machine and workspace grouping is never reversed; Descending reverses
// only the chosen sort key: the workspace-group order for w, and the pane
// name or the status rank inside each workspace for p and s. Equal values
// are tied deterministically by pane name and then full ref.
func sessionLess(rows []sessionSortRow, i, j int, vs SessionViewState) bool {
	a, b := rows[i], rows[j]
	if a.machine != b.machine {
		return a.machine < b.machine
	}
	if g := compareSessionGroups(a.wsGroup, b.wsGroup); g != 0 {
		if vs.Sort == sessionSortWorkspace && vs.Descending {
			return g > 0
		}
		return g < 0
	}
	if vs.Sort == sessionSortStatus {
		if a.status != b.status {
			if vs.Descending {
				return a.status > b.status
			}
			return a.status < b.status
		}
		if a.pane != b.pane {
			return a.pane < b.pane
		}
		if a.paneRaw != b.paneRaw {
			return a.paneRaw < b.paneRaw
		}
		return a.ref < b.ref
	}
	// The pane name is the item key for p; Descending reverses it only
	// while p is the chosen key - the within-group order for w stays
	// ascending. The ref tie-break never reverses.
	if a.pane != b.pane {
		if vs.Descending && vs.Sort != sessionSortWorkspace {
			return a.pane > b.pane
		}
		return a.pane < b.pane
	}
	if a.paneRaw != b.paneRaw {
		if vs.Descending && vs.Sort != sessionSortWorkspace {
			return a.paneRaw > b.paneRaw
		}
		return a.paneRaw < b.paneRaw
	}
	return a.ref < b.ref
}

// compareSessionGroups orders workspace groups: label case-insensitively,
// then identity (sessionWorkspaceKey), then the raw label. -1/0/1.
func compareSessionGroups(a, b [3]string) int {
	for _, pair := range [][2]string{{a[0], b[0]}, {a[1], b[1]}, {a[2], b[2]}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// sessionVisibleEntries orders the enhanced popup's visible entries: the
// local machine first, the other machines by label case-insensitively, the
// workspaces within a machine by label case-insensitively then identity
// (sessionWorkspaceKey, machine scoped), and the entries within a workspace
// by the chosen sort key. The caller passes the list in the legacy order
// when the enhanced view is off; this function only serves the enhanced
// view.
func sessionVisibleEntries(entries []PickerEntry, vs SessionViewState) []PickerEntry {
	rows := sessionSortRows(entries)
	sort.SliceStable(rows, func(i, j int) bool { return sessionLess(rows, i, j, vs) })
	out := make([]PickerEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.entry)
	}
	return out
}

// sessionApplySort selects one enhanced sort key (p/w/s): a different sort
// starts ascending; repeating the current sort toggles descending.
func sessionApplySort(vs *SessionViewState, sortKey string) {
	if vs.Sort == sortKey {
		vs.Descending = !vs.Descending
		return
	}
	vs.Sort = sortKey
	vs.Descending = false
}

// sessionToggleView switches the presentation between the table and the
// tree; anything that is not the tree goes to the tree.
func sessionToggleView(vs *SessionViewState) {
	if vs.View == sessionViewTree {
		vs.View = sessionViewTable
	} else {
		vs.View = sessionViewTree
	}
}

// sessionKeepSelection returns the index of the entry that kept the given
// full ref after the visible list re-ordered, or the clamped selection when
// the ref is not in the list. It is the full-ref selection rule of the
// enhanced view: sorting and view switches never silently choose a
// different pane.
func sessionKeepSelection(list []PickerEntry, selected int, ref string) int {
	if ref != "" {
		for i, entry := range list {
			if pickerString(entry["ref"]) == ref {
				return i
			}
		}
	}
	if selected < 0 {
		selected = 0
	}
	if n := len(list); n > 0 && selected > n-1 {
		selected = n - 1
	}
	return selected
}
