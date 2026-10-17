package plugin

import (
	"os"
	"strings"

	contextpkg "context"

	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/sessionref"
)

// pickerTeamView is the session popup's read-only team view: the soho
// team roster and the effective role configuration of the selected row's
// workspace, for rows of the local machine, in two presentation forms
// (the tree by default, the table after the v switch). The same type
// serves the picker's state and the board runtime's state (the runtime
// the picker command runs on). The two CLI reads (roster --json and
// roles --json, exactly) run off the key path with SOHO_NOWRITE=1
// through the shared teamCall, and only their finished result comes back
// to the list's goroutine, where applyTeamView applies it. A late result
// of another row or of a superseded open is discarded, and nothing here
// writes: a failed or undecodable read shows its cause in its own block
// and the other block keeps its content.
type pickerTeamView struct {
	// target is the soho team target of the selected row (the row's
	// workspace_id, pane_id and cwd; the reads need no tab id).
	target teamTarget
	// entryRef is the row's full ref: a result applies only while the
	// view still sits on this row.
	entryRef string
	// machine and workspaceLabel feed the frame title
	// ("TEAM  <workspace> · <machine>").
	machine        string
	workspaceLabel string
	// remote is true for a row of another machine: the view shows that
	// remote team views are not available yet and never calls the CLI.
	remote bool
	// targetErr is non-empty when the row's cwd failed validation: the
	// view shows the cause and no CLI call is made.
	targetErr string
	// loaded is true once the finished result was applied; until then
	// the view shows "loading team…".
	loaded bool
	// loadSeq is the open's sequence (the owning state's teamLoadSeq at
	// the open): a result of another open (a stale sequence) is
	// discarded.
	loadSeq int
	// roster and roles are the decoded reads (soho roster --json / soho
	// roles --json); nil with their failure's cause, whose block shows
	// "<name>: <cause>" while the other block keeps its content.
	roster        *teamViewRoster
	rosterFailure string
	roles         *teamViewRoles
	rolesFailure  string
	// form is the open presentation (teamFormTree by default,
	// teamFormTable after the v switch): the owning state remembers the
	// choice for the run (the board also persists it).
	form string
	// scroll is the view's scroll offset (the arrow and page keys); the
	// renderer applies it with scrollLines, like the team panel.
	scroll int
}

// pickerTeamViewResult is one finished team-view load: the loader builds
// it off the key path (the decoded reads, or their causes) and the
// list's goroutine applies it, so the live state is only touched by
// that goroutine, like the team panel's load.
type pickerTeamViewResult struct {
	seq         int
	entryRef    string
	roster      *teamViewRoster
	rosterCause string
	roles       *teamViewRoles
	rolesCause  string
}

// openTeamView builds the read-only team view of one row (shared by
// the picker's and the board's states): the row's
// workspace_id, pane_id and cwd build the team target and the cwd is
// validated like resolveTeamTarget does (it must exist and be a
// directory); a row of another machine opens the view with the
// remote-machine message and no CLI call at all. seq is the open's sequence (the
// owning state's teamLoadSeq at the open): a result of another open
// (a stale sequence) is discarded. The view opens in the tree form; the
// owning state then applies the run's remembered form.
func openTeamView(seq int, entry PickerEntry) *pickerTeamView {
	machine := sessionMachineOf(entry)
	v := &pickerTeamView{
		target: teamTarget{
			WorkspaceID: pickerString(entry["workspace_id"]),
			PaneID:      pickerString(entry["pane_id"]),
			Cwd:         pickerString(entry["cwd"]),
		},
		entryRef:       pickerString(entry["ref"]),
		machine:        machine,
		workspaceLabel: pickerString(entry["workspace_label"]),
		remote:         machine != sessionref.LocalMachine,
		loadSeq:        seq,
		form:           teamFormTree,
	}
	if !v.remote {
		if cause := pickerTeamCwdCause(v.target.Cwd); cause != "" {
			// The target is invalid: the view shows the cause; no
			// CLI call.
			v.targetErr = cause
		}
	}
	return v
}

// openPickerTeamView opens the read-only team view on the selected row.
// Every open starts a new load sequence, so a late result of the
// previous open (or of another row) is discarded, and the open takes the
// run's remembered form (in memory, like the list's).
func (s *PickerState) openPickerTeamView() {
	list := s.visible()
	if len(list) == 0 {
		return
	}
	entry := list[min(s.Selected, len(list)-1)]
	s.teamLoadSeq++
	s.TeamView = openTeamView(s.teamLoadSeq, entry)
	s.TeamView.form = teamViewFormOf(s.teamViewForm)
}

// pickerTeamCwdCause validates the row's cwd like resolveTeamTarget does:
// it must exist and be a directory. It returns the cause the view shows,
// or "" when the cwd is valid.
func pickerTeamCwdCause(cwd string) string {
	if cwd == "" {
		return "row has no cwd"
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return "pane cwd does not exist: " + cwd
	}
	if !info.IsDir() {
		return "pane cwd is not a directory: " + cwd
	}
	return ""
}

// teamViewRosterCall runs the roster read (roster --json, a NOWRITE read
// through the shared teamCall) and decodes the joined output; an output
// that does not decode fails with "invalid output", the view's own block
// shows the cause and the other block keeps its content.
func teamViewRosterCall(ctx contextpkg.Context, exe string, env platform.Env, platformName string, target teamTarget) (*teamViewRoster, string) {
	lines, cause := teamCall(ctx, exe, env, platformName, target, []string{"roster", "--json"}, true)
	if cause != "" {
		return nil, cause
	}
	roster, err := teamViewParseRoster(strings.Join(lines, "\n"))
	if err != nil {
		return nil, "invalid output"
	}
	return roster, ""
}

// teamViewRolesCall runs the roles read (roles --json, a NOWRITE read
// through the shared teamCall) and decodes the joined output; an output
// that does not decode fails with "invalid output", the view's own block
// shows the cause and the other block keeps its content.
func teamViewRolesCall(ctx contextpkg.Context, exe string, env platform.Env, platformName string, target teamTarget) (*teamViewRoles, string) {
	lines, cause := teamCall(ctx, exe, env, platformName, target, []string{"roles", "--json"}, true)
	if cause != "" {
		return nil, cause
	}
	roles, err := teamViewParseRoles(strings.Join(lines, "\n"))
	if err != nil {
		return nil, "invalid output"
	}
	return roles, ""
}

// runPickerTeamViewLoad runs the open view's two read-only CLI calls
// (roster --json and roles --json, exactly, each a NOWRITE read through
// the shared teamCall) sequentially with the shared call deadline, off
// the key path, and publishes the finished result for the loop to apply
// on its own goroutine - the picker's discovery-load pattern (loadPickerEntries
// and the team panel's loader): the load runs in the loop's worker and
// only the finished result crosses to the live state. It does not touch
// the live state itself, it never writes, and a cancelled context
// publishes nothing (the view stays loading; re-opening it starts a new
// sequence).
func runPickerTeamViewLoad(ctx contextpkg.Context, exe string, env platform.Env, platformName string, v *pickerTeamView, publish func(pickerTeamViewResult)) {
	if v == nil || v.remote || v.targetErr != "" {
		return
	}
	res := pickerTeamViewResult{seq: v.loadSeq, entryRef: v.entryRef}
	res.roster, res.rosterCause = teamViewRosterCall(ctx, exe, env, platformName, v.target)
	if ctx.Err() != nil {
		return
	}
	res.roles, res.rolesCause = teamViewRolesCall(ctx, exe, env, platformName, v.target)
	if ctx.Err() != nil {
		return
	}
	publish(res)
}

// applyTeamView folds one finished load into the open view and reports
// whether it applied (shared by the picker's and the board's
// states): a result of another row or of a superseded open (a stale
// sequence) is discarded, the view of a remote or invalid-target row
// never takes a result, and an already applied view is not loaded
// twice.
func applyTeamView(view *pickerTeamView, res pickerTeamViewResult) bool {
	if view == nil || view.remote || view.targetErr != "" || view.loaded {
		return false
	}
	if res.seq != view.loadSeq || res.entryRef != view.entryRef {
		return false
	}
	view.roster = res.roster
	view.rosterFailure = res.rosterCause
	view.roles = res.roles
	view.rolesFailure = res.rolesCause
	view.loaded = true
	return true
}

// applyPickerTeamView applies one finished load to the picker's open
// view, on the picker's goroutine.
func (s *PickerState) applyPickerTeamView(res pickerTeamViewResult) bool {
	return applyTeamView(s.TeamView, res)
}

// teamViewToggleForm switches the open view between the tree and the
// table and resets its scroll (shared by the picker's and the board's
// states); anything that is not the table goes to the tree.
func teamViewToggleForm(view *pickerTeamView) {
	if view == nil {
		return
	}
	if teamViewFormOf(view.form) == teamFormTree {
		view.form = teamFormTable
	} else {
		view.form = teamFormTree
	}
	view.scroll = 0
}

// teamViewScrollMove moves the open team view one line (up/down) or one
// page (page-up/page-down) with the team panel's page size, clamped at
// the top; the bottom clamps by scrollLines in the renderer, like the
// panel. It is shared by the picker's and the board's states.
func teamViewScrollMove(view *pickerTeamView, key string) {
	switch key {
	case "up":
		view.scroll--
	case "down":
		view.scroll++
	case "page-up":
		view.scroll -= teamViewScrollPage
	case "page-down":
		view.scroll += teamViewScrollPage
	}
	if view.scroll < 0 {
		view.scroll = 0
	}
}

// teamViewScroll scrolls the picker's open team view.
func (s *PickerState) teamViewScroll(key string) {
	teamViewScrollMove(s.TeamView, key)
}
