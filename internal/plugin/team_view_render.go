package plugin

// The Team view's two presentation forms (DJA-375 B): the tree (the
// default) and the table, over the CLI's JSON reads (soho roster
// --json / soho roles --json). The BoardState persists the open form in
// the team-view preference; the PickerState keeps it in memory for the
// run. The tree groups the effective roles by lane (lanes in
// alphabetical order, the uniform lanes by mode) and lists the roles
// without a lane under OUTSIDE LANES; the table shows every agent and
// role in its columns, the roles' file paths never reach the display.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The view's presentation forms and the preference file's values.
const (
	teamFormTree  = "tree"
	teamFormTable = "table"
)

// teamViewFormOf normalizes the view's form: anything that is not the
// table reads as the tree (a missing or invalid saved value included).
func teamViewFormOf(form string) string {
	if form == teamFormTable {
		return teamFormTable
	}
	return teamFormTree
}

// teamViewFooterSegments is the view's control guidance, three lines from
// top to bottom (the list's footer keeps its line count): the navigation
// line, the form switch and the way back. The last segment of each line
// is the one that must survive a tiny width, so Back wins.
func teamViewFooterSegments(form string) [][]string {
	other := "Table"
	if teamViewFormOf(form) == teamFormTable {
		other = "Tree"
	}
	return [][]string{
		{"↑↓ Scroll", "PgUp/PgDn Page"},
		{"v " + other},
		{"i/Esc Back", "Ctrl-C Close"},
	}
}

// teamViewRoster is the roster read of the team view (soho roster
// --json): the workspace's settings, the team's agents and the panes
// that sit in the workspace without being the team.
type teamViewRoster struct {
	Workspace string                 `json:"workspace"`
	Scope     string                 `json:"scope"`
	Settings  teamViewRosterSettings `json:"settings"`
	Agents    []teamViewRosterAgent  `json:"agents"`
	Others    []teamViewRosterOther  `json:"others"`
}

// teamViewRosterSettings is the roster's settings line values.
type teamViewRosterSettings struct {
	Layout       string `json:"layout"`
	ReuseWorkers string `json:"reuse_workers"`
	MultiRole    string `json:"multi_role"`
	AutoApprove  string `json:"auto_approve"`
}

// teamViewRosterAgent is one team agent of the roster read.
type teamViewRosterAgent struct {
	Name         string `json:"name"`
	Role         string `json:"role"`
	PreviousRole string `json:"previous_role"`
	Kind         string `json:"kind"`
	Pane         string `json:"pane"`
	Tab          string `json:"tab"`
	State        string `json:"state"`
	Report       string `json:"report"`
	Cwd          string `json:"cwd"`
	Task         string `json:"task"`
}

// teamViewRosterOther is a workspace pane that is not the team's.
type teamViewRosterOther struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Pane      string `json:"pane"`
	Tab       string `json:"tab"`
	State     string `json:"state"`
	Workspace string `json:"workspace"`
}

// teamViewRoles is the roles read of the team view (soho roles --json):
// one object per effective role, with its lane and where each value came
// from (the role file's path never reaches the display).
type teamViewRoles struct {
	Roles []teamViewRole `json:"roles"`
}

// teamViewRole is one effective role of the roles read.
type teamViewRole struct {
	Role       string `json:"role"`
	Kind       string `json:"kind"`
	Model      string `json:"model"`
	Effort     string `json:"effort"`
	Mode       string `json:"mode"`
	Lane       string `json:"lane"`
	KindFrom   string `json:"kind_from"`
	ModelFrom  string `json:"model_from"`
	EffortFrom string `json:"effort_from"`
	File       string `json:"file"`
}

// teamViewParseRoster decodes the roster read's joined output.
func teamViewParseRoster(text string) (*teamViewRoster, error) {
	var roster teamViewRoster
	if err := json.Unmarshal([]byte(text), &roster); err != nil {
		return nil, err
	}
	return &roster, nil
}

// teamViewParseRoles decodes the roles read's joined output.
func teamViewParseRoles(text string) (*teamViewRoles, error) {
	var roles teamViewRoles
	if err := json.Unmarshal([]byte(text), &roles); err != nil {
		return nil, err
	}
	return &roles, nil
}

// teamViewSeg is one styled span of a view line: the style wraps only the
// span (the width math runs on the plain text), so a cut never splits a
// style in half.
type teamViewSeg struct {
	style string
	text  string
}

// teamViewLine joins the line's spans, cut to the avail width: the cut
// consumes the spans from the left, the way teamCut cuts a plain line
// (no ellipsis, no wrap).
func teamViewLine(avail int, segs ...teamViewSeg) string {
	if avail <= 0 {
		return ""
	}
	var out strings.Builder
	remaining := avail
	for _, seg := range segs {
		if remaining <= 0 {
			break
		}
		text := seg.text
		if displayWidth(text) > remaining {
			text = teamCut(text, remaining)
		}
		if text == "" {
			break
		}
		if seg.style != "" {
			out.WriteString(seg.style + text + sessionStyleReset)
		} else {
			out.WriteString(text)
		}
		remaining -= displayWidth(text)
	}
	return out.String()
}

// teamViewCell is the table's cell: the sanitized value, or "-" when
// empty.
func teamViewCell(value string) string {
	if v := StripPickerControls(value); v != "" {
		return v
	}
	return "-"
}

// teamViewChain is the roles' values chain (kind · model · effort): an
// empty value is omitted, and an all-empty chain reads "-".
func teamViewChain(kind, model, effort string) string {
	var parts []string
	for _, value := range []string{StripPickerControls(kind), StripPickerControls(model), StripPickerControls(effort)} {
		if value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " · ")
}

// teamViewRoleFrom is the table's FROM cell: when the three origins
// agree, the single value; otherwise the per-value origins. The role
// file's path never appears.
func teamViewRoleFrom(role teamViewRole) string {
	k := StripPickerControls(role.KindFrom)
	m := StripPickerControls(role.ModelFrom)
	e := StripPickerControls(role.EffortFrom)
	if k == m && m == e {
		if k == "" {
			return "-"
		}
		return k
	}
	return fmt.Sprintf("kind: %s; model: %s; effort: %s", k, m, e)
}

// teamViewTop is the view's first section: the bold title and the dim
// settings line (present only with the loaded roster's settings).
func teamViewTop(v *pickerTeamView, avail int) []string {
	lines := []string{teamViewLine(avail, teamViewSeg{sessionStyleBold, pickerTeamTitle(v)})}
	if v.roster != nil {
		settings := fmt.Sprintf("layout %s · reuse workers %s · multi-role %s · auto-approve %s",
			StripPickerControls(v.roster.Settings.Layout),
			StripPickerControls(v.roster.Settings.ReuseWorkers),
			StripPickerControls(v.roster.Settings.MultiRole),
			StripPickerControls(v.roster.Settings.AutoApprove))
		lines = append(lines, teamViewLine(avail, teamViewSeg{sessionStyleDim, settings}))
	}
	return lines
}

// teamViewCauseLine is the failed call's one block line: "<name>:
// <cause>", the cause the view's load reports (or the invalid-output
// cause when the read's output does not decode).
func teamViewCauseLine(name, cause string, avail int) string {
	if cause == "" {
		cause = "invalid output"
	}
	return teamViewLine(avail, teamViewSeg{"", name + ": " + StripPickerControls(cause)})
}

// teamViewAgentsBlock is the roster's block in the open form: the tree's
// AGENTS (n) section or the table's AGENTS section, or the one cause
// line when the roster read failed (the other block keeps its content).
func teamViewAgentsBlock(v *pickerTeamView, avail int) []string {
	if v.roster == nil {
		return []string{teamViewCauseLine("roster", v.rosterFailure, avail)}
	}
	if teamViewFormOf(v.form) == teamFormTable {
		return teamViewAgentsTable(v.roster, avail)
	}
	return teamViewAgentsTree(v.roster, avail)
}

// teamViewAgentsTree is the tree's AGENTS section: the bold count header
// (the team's agents plus the outside panes), the team's agents first
// (JSON order) and then the outside ones, every column padded to its own
// widest display width, two spaces between the columns, the whole line
// cut at the width (never wrapped). With no agent at all it shows the
// placeholder.
func teamViewAgentsTree(roster *teamViewRoster, avail int) []string {
	n := len(roster.Agents) + len(roster.Others)
	lines := []string{teamViewLine(avail, teamViewSeg{sessionStyleBold, fmt.Sprintf("AGENTS (%d)", n)})}
	if n == 0 {
		return append(lines, teamViewLine(avail, teamViewSeg{"", "  no agent in this workspace"}))
	}
	type agentRow struct {
		cells   []string
		outside bool
	}
	rows := make([]agentRow, 0, n)
	for _, a := range roster.Agents {
		role := StripPickerControls(a.Role)
		if prev := StripPickerControls(a.PreviousRole); prev != "" {
			role += " (" + prev + ")"
		}
		task := StripPickerControls(a.Task)
		if task == "" {
			task = "-"
		}
		rows = append(rows, agentRow{cells: []string{
			StripPickerControls(a.Name), role, StripPickerControls(a.Kind),
			StripPickerControls(a.Pane), StripPickerControls(a.State), task,
		}})
	}
	for _, o := range roster.Others {
		// The outside pane's role is "-" and its last column reads
		// "not in the team" in dim.
		rows = append(rows, agentRow{cells: []string{
			StripPickerControls(o.Name), "-", StripPickerControls(o.Kind),
			StripPickerControls(o.Pane), StripPickerControls(o.State), "not in the team",
		}, outside: true})
	}
	widths := make([]int, 6)
	for _, row := range rows {
		for i, cell := range row.cells {
			if w := displayWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	for _, row := range rows {
		var prefix strings.Builder
		prefix.WriteString("  ")
		for i := 0; i < 5; i++ {
			prefix.WriteString(row.cells[i])
			prefix.WriteString(strings.Repeat(" ", widths[i]-displayWidth(row.cells[i])))
			prefix.WriteString("  ")
		}
		last := row.cells[5]
		style := ""
		if row.outside {
			style = sessionStyleDim
		}
		lines = append(lines, teamViewLine(avail, teamViewSeg{"", prefix.String()}, teamViewSeg{style, last}))
	}
	return lines
}

// teamViewAgentsTable is the table's AGENTS section: the bold title, the
// dim header line, the team's agents (JSON order) and then the outside
// ones (ROLE and REPORT "-", TASK "not in the team"), and the header
// line plus the message with no agent at all.
func teamViewAgentsTable(roster *teamViewRoster, avail int) []string {
	header := []string{"NAME", "ROLE", "KIND", "PANE", "STATE", "REPORT", "TASK"}
	var rows [][]string
	for _, a := range roster.Agents {
		role := teamViewCell(a.Role)
		if prev := StripPickerControls(a.PreviousRole); prev != "" {
			role += " (" + prev + ")"
		}
		rows = append(rows, []string{
			teamViewCell(a.Name), role, teamViewCell(a.Kind), teamViewCell(a.Pane),
			teamViewCell(a.State), teamViewCell(a.Report), teamViewCell(a.Task),
		})
	}
	for _, o := range roster.Others {
		rows = append(rows, []string{
			teamViewCell(o.Name), "-", teamViewCell(o.Kind), teamViewCell(o.Pane),
			teamViewCell(o.State), "-", "not in the team",
		})
	}
	lines := []string{teamViewLine(avail, teamViewSeg{sessionStyleBold, "AGENTS"})}
	lines = append(lines, teamViewTableLines(avail, header, rows)...)
	if len(rows) == 0 {
		lines = append(lines, teamViewLine(avail, teamViewSeg{"", "no agent in this workspace"}))
	}
	return lines
}

// teamViewRolesBlock is the roles read's block content in the open form,
// with the blank lines between its own sections (the tree's lanes and
// OUTSIDE LANES are separate sections; the table's is one): or the one
// cause line when the roles read failed (the other block keeps its
// content).
func teamViewRolesBlock(v *pickerTeamView, avail int) []string {
	if v.roles == nil {
		return []string{teamViewCauseLine("roles", v.rolesFailure, avail)}
	}
	if teamViewFormOf(v.form) == teamFormTable {
		return teamViewRolesTable(v.roles, avail)
	}
	var out []string
	for i, section := range teamViewLaneSections(v.roles, avail) {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, section...)
	}
	return out
}

// teamViewLaneGroups groups the roles by lane: the lane's name (the
// sanitized value), the lane's roles and the roles without a lane.
func teamViewLaneGroups(roles *teamViewRoles) (lanes map[string][]teamViewRole, outside []teamViewRole) {
	lanes = map[string][]teamViewRole{}
	for _, r := range roles.Roles {
		if lane := StripPickerControls(r.Lane); lane != "" {
			lanes[lane] = append(lanes[lane], r)
		} else {
			outside = append(outside, r)
		}
	}
	return lanes, outside
}

// teamViewLaneSections is the tree's lane sections: one section per lane
// (alphabetical order), then OUTSIDE LANES (absent with no outside role).
func teamViewLaneSections(roles *teamViewRoles, avail int) [][]string {
	lanes, outside := teamViewLaneGroups(roles)
	var names []string
	for name := range lanes {
		names = append(names, name)
	}
	sort.Strings(names)
	var sections [][]string
	for _, name := range names {
		sections = append(sections, teamViewLaneSection(name, lanes[name], avail))
	}
	if section := teamViewOutsideSection(outside, avail); section != nil {
		sections = append(sections, section)
	}
	return sections
}

// teamViewModeOrder is the lane's mode line order: edit, read-only, the
// other modes in alphabetical order.
func teamViewModeOrder(modes map[string][]string) []string {
	var order []string
	for _, mode := range []string{"edit", "read-only"} {
		if _, ok := modes[mode]; ok {
			order = append(order, mode)
		}
	}
	var others []string
	for mode := range modes {
		if mode != "edit" && mode != "read-only" {
			others = append(others, mode)
		}
	}
	sort.Strings(others)
	return append(order, others...)
}

// teamViewLaneSection is one tree lane section: the bold "LANE <name>"
// header (plus the kind · model · effort chain when every role of the
// lane shares the three values) and the body. A uniform lane lists one
// line per mode (the label padded to the lane's widest mode label, the
// roles in alphabetical order, the list wrapped between roles with the
// continuation at the first role's column); a non-uniform lane gives
// every role one OUTSIDE-LANES format line plus its mode.
func teamViewLaneSection(name string, roles []teamViewRole, avail int) []string {
	uniform := true
	for _, r := range roles[1:] {
		if StripPickerControls(r.Kind) != StripPickerControls(roles[0].Kind) ||
			StripPickerControls(r.Model) != StripPickerControls(roles[0].Model) ||
			StripPickerControls(r.Effort) != StripPickerControls(roles[0].Effort) {
			uniform = false
			break
		}
	}
	var header []teamViewSeg
	header = append(header, teamViewSeg{sessionStyleBold, "LANE " + name})
	if uniform {
		header = append(header, teamViewSeg{"", "  " + teamViewChain(roles[0].Kind, roles[0].Model, roles[0].Effort)})
	}
	lines := []string{teamViewLine(avail, header...)}
	if !uniform {
		nameW := 0
		for _, r := range roles {
			if w := displayWidth(StripPickerControls(r.Role)); w > nameW {
				nameW = w
			}
		}
		sorted := append([]teamViewRole(nil), roles...)
		sort.Slice(sorted, func(i, j int) bool {
			return StripPickerControls(sorted[i].Role) < StripPickerControls(sorted[j].Role)
		})
		for _, r := range sorted {
			mode := StripPickerControls(r.Mode)
			if mode == "" {
				mode = "-"
			}
			cell := StripPickerControls(r.Role)
			line := "  " + cell + strings.Repeat(" ", nameW-displayWidth(cell)) + "  " +
				teamViewChain(r.Kind, r.Model, r.Effort) + "  " + mode
			lines = append(lines, teamViewLine(avail, teamViewSeg{"", line}))
		}
		return lines
	}
	modes := map[string][]string{}
	for _, r := range roles {
		mode := StripPickerControls(r.Mode)
		if mode == "" {
			// A role with no mode sits in the "-" group.
			mode = "-"
		}
		modes[mode] = append(modes[mode], StripPickerControls(r.Role))
	}
	labelW := 0
	for mode := range modes {
		if w := displayWidth(mode); w > labelW {
			labelW = w
		}
	}
	for _, mode := range teamViewModeOrder(modes) {
		names := modes[mode]
		sort.Strings(names)
		prefix := "  " + mode + strings.Repeat(" ", labelW-displayWidth(mode)) + "  "
		lines = append(lines, teamViewRoleListLines(prefix, names, avail)...)
	}
	return lines
}

// teamViewOutsideSection is the tree's OUTSIDE LANES section: the bold
// header, one line per outside role (alphabetical order, the name padded
// to the section's widest name, two spaces and the chain), the planner
// gaining the dim "the orchestrator" note. It is nil with no outside
// role (the section is gone).
func teamViewOutsideSection(roles []teamViewRole, avail int) []string {
	if len(roles) == 0 {
		return nil
	}
	nameW := 0
	for _, r := range roles {
		if w := displayWidth(StripPickerControls(r.Role)); w > nameW {
			nameW = w
		}
	}
	sorted := append([]teamViewRole(nil), roles...)
	sort.Slice(sorted, func(i, j int) bool {
		return StripPickerControls(sorted[i].Role) < StripPickerControls(sorted[j].Role)
	})
	lines := []string{teamViewLine(avail, teamViewSeg{sessionStyleBold, "OUTSIDE LANES"})}
	for _, r := range sorted {
		name := StripPickerControls(r.Role)
		line := "  " + name + strings.Repeat(" ", nameW-displayWidth(name)) + "  " +
			teamViewChain(r.Kind, r.Model, r.Effort)
		if name == "planner" {
			lines = append(lines, teamViewLine(avail,
				teamViewSeg{"", line},
				teamViewSeg{sessionStyleDim, "  the orchestrator"}))
			continue
		}
		lines = append(lines, teamViewLine(avail, teamViewSeg{"", line}))
	}
	return lines
}

// teamViewRoleListLines wraps the lane mode's role list after the mode
// label: the first line continues the label line and the continuations
// start at the first role's column; the break happens only between
// roles (a role wider than the whole line is hard-cut, one piece per
// line, so the wrap always advances).
func teamViewRoleListLines(prefix string, roles []string, avail int) []string {
	if avail <= 0 {
		return []string{""}
	}
	prefix = teamCut(prefix, avail)
	prefixW := displayWidth(prefix)
	var lines []string
	line := prefix
	used := prefixW
	flush := func() {
		lines = append(lines, line)
		line = strings.Repeat(" ", prefixW)
		used = prefixW
	}
	for i, role := range roles {
		sep := 0
		if i > 0 {
			sep = 2 // ", "
		}
		for {
			if used+sep+displayWidth(role) <= avail {
				if sep > 0 {
					line += ", "
					used += sep
					sep = 0
				}
				line += role
				used += displayWidth(role)
				break
			}
			if i > 0 && used > prefixW {
				// The role does not fit after the previous one: the
				// break happens between roles, at the role column; the
				// continuation starts the role, not its separator.
				flush()
				sep = 0
				continue
			}
			// A single role is wider than the empty line: hard-cut one
			// piece (teamHardPiece always consumes a piece).
			space := avail - used
			if space < 1 {
				space = 1
			}
			piece, consumed := teamHardPiece(role, space)
			line += piece
			used += displayWidth(piece)
			role = role[consumed:]
			if role == "" {
				break
			}
			sep = 0 // the continuation is the same role
			flush()
		}
	}
	lines = append(lines, line)
	return lines
}

// teamViewTableLines is one table's dim header line and rows: every
// column but the last is padded to the widest cell between the header
// and the rows, the columns are two spaces apart, and every line is cut
// at the width (never wrapped).
func teamViewTableLines(avail int, header []string, rows [][]string) []string {
	n := len(header)
	widths := make([]int, n)
	measure := func(cells []string) {
		for i, cell := range cells {
			if w := displayWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	measure(header)
	for _, row := range rows {
		measure(row)
	}
	var out []string
	emit := func(cells []string, style string) {
		var b strings.Builder
		for i, cell := range cells {
			if i > 0 {
				b.WriteString("  ")
			}
			if i < n-1 {
				b.WriteString(cell)
				b.WriteString(strings.Repeat(" ", widths[i]-displayWidth(cell)))
			} else {
				b.WriteString(cell)
			}
		}
		out = append(out, teamViewLine(avail, teamViewSeg{style, b.String()}))
	}
	emit(header, sessionStyleDim)
	for _, row := range rows {
		emit(row, "")
	}
	return out
}

// teamViewRolesTable is the table's ROLES section: the bold title, the
// dim header line and one line per effective role (alphabetical order,
// the empty values as "-", the FROM column the role's origins, never the
// file path).
func teamViewRolesTable(roles *teamViewRoles, avail int) []string {
	header := []string{"ROLE", "KIND", "MODEL", "EFFORT", "MODE", "FROM"}
	sorted := append([]teamViewRole(nil), roles.Roles...)
	sort.Slice(sorted, func(i, j int) bool {
		return StripPickerControls(sorted[i].Role) < StripPickerControls(sorted[j].Role)
	})
	rows := make([][]string, 0, len(sorted))
	for _, r := range sorted {
		rows = append(rows, []string{
			teamViewCell(r.Role), teamViewCell(r.Kind), teamViewCell(r.Model),
			teamViewCell(r.Effort), teamViewCell(r.Mode), teamViewRoleFrom(r),
		})
	}
	lines := []string{teamViewLine(avail, teamViewSeg{sessionStyleBold, "ROLES"})}
	lines = append(lines, teamViewTableLines(avail, header, rows)...)
	return lines
}

// teamViewTree is the tree form's unscrolled content at avail display
// columns: the title section, the roster's block (AGENTS or its cause)
// and the roles' block (the lanes, OUTSIDE LANES or the cause), one
// blank line between the sections, every line cut at the width.
func teamViewTree(v *pickerTeamView, avail int) []string {
	lines := teamViewTop(v, avail)
	lines = append(lines, "")
	lines = append(lines, teamViewAgentsBlock(v, avail)...)
	roles := teamViewRolesBlock(v, avail)
	if len(roles) > 0 {
		lines = append(lines, "")
		lines = append(lines, roles...)
	}
	return lines
}

// teamViewTable is the table form's unscrolled content at avail display
// columns: the title section, the roster's block (the AGENTS section or
// its cause) and the roles' block (the ROLES section or its cause), one
// blank line between the sections, every line cut at the width.
func teamViewTable(v *pickerTeamView, avail int) []string {
	lines := teamViewTop(v, avail)
	lines = append(lines, "")
	lines = append(lines, teamViewAgentsBlock(v, avail)...)
	lines = append(lines, "")
	lines = append(lines, teamViewRolesBlock(v, avail)...)
	return lines
}
