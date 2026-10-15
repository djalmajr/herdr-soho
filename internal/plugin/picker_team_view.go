package plugin

import (
	"os"

	contextpkg "context"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/sessionref"
)

// pickerTeamView is the picker's read-only team view (DJA-317 slice A):
// the soho team roster and the effective role configuration of the
// selected row's workspace, for rows of the local machine. The two CLI
// reads (roster and roles, exactly) run off the key path with
// HERDR_SOHO_NOWRITE=1 through the shared teamCall, and only their
// finished result comes back to the list's goroutine, where
// applyPickerTeamView applies it. A late result of another row or of a
// superseded open is discarded, and nothing here writes: a failed call
// shows its cause in its own block and the other block keeps its content.
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
	// remote is true for a row of another machine: the view shows the
	// slice-B message and never calls the CLI (the remote path is
	// slice B).
	remote bool
	// targetErr is non-empty when the row's cwd failed validation: the
	// view shows the cause and no CLI call is made.
	targetErr string
	// loaded is true once the finished result was applied; until then
	// the view shows "loading team…".
	loaded bool
	// loadSeq is the open's sequence (the picker's teamLoadSeq at the
	// open): a result of another open (stale sequence) is discarded.
	loadSeq       int
	rosterLines   []string
	rosterFailure string
	rolesLines    []string
	rolesFailure  string
	// scroll is the view's scroll offset (the arrow and page keys); the
	// renderer applies it with scrollLines, like the team panel.
	scroll int
}

// pickerTeamViewResult is one finished team-view load: the loader builds
// it off the key path and the list's goroutine applies it, so the live
// state is only touched by that goroutine, like the team panel's load.
type pickerTeamViewResult struct {
	seq         int
	entryRef    string
	rosterLines []string
	rosterCause string
	rolesLines  []string
	rolesCause  string
}

// openPickerTeamView opens the read-only team view on the selected row.
// The row's workspace_id, pane_id and cwd build the team target and the
// cwd is validated like resolveTeamTarget does (it must exist and be a
// directory); a row of another machine opens the view with the slice-B
// message and no CLI call at all. Every open starts a new load sequence,
// so a late result of the previous open (or of another row) is discarded.
func (s *PickerState) openPickerTeamView() {
	list := s.visible()
	if len(list) == 0 {
		return
	}
	entry := list[min(s.Selected, len(list)-1)]
	machine := sessionMachineOf(entry)
	s.teamLoadSeq++
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
		loadSeq:        s.teamLoadSeq,
	}
	if !v.remote {
		if cause := pickerTeamCwdCause(v.target.Cwd); cause != "" {
			// The target is invalid: the view shows the cause; no
			// CLI call.
			v.targetErr = cause
		}
	}
	s.TeamView = v
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

// runPickerTeamViewLoad runs the open view's two read-only CLI calls
// (roster and roles, exactly, each a NOWRITE read through the shared
// teamCall) sequentially with the shared call deadline, off the key
// path, and publishes the finished result for the loop to apply on its
// own goroutine - the picker's discovery-load pattern (loadPickerEntries
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
	res.rosterLines, res.rosterCause = teamCall(ctx, exe, env, platformName, v.target, []string{"roster"}, true)
	if ctx.Err() != nil {
		return
	}
	res.rolesLines, res.rolesCause = teamCall(ctx, exe, env, platformName, v.target, []string{"roles"}, true)
	if ctx.Err() != nil {
		return
	}
	publish(res)
}

// applyPickerTeamView folds one finished load into the open view and
// reports whether it applied: a result of another row or of a
// superseded open (a stale sequence) is discarded, the view of a remote
// or invalid-target row never takes a result, and an already applied
// view is not loaded twice.
func (s *PickerState) applyPickerTeamView(res pickerTeamViewResult) bool {
	v := s.TeamView
	if v == nil || v.remote || v.targetErr != "" || v.loaded {
		return false
	}
	if res.seq != v.loadSeq || res.entryRef != v.entryRef {
		return false
	}
	v.rosterLines = res.rosterLines
	v.rosterFailure = res.rosterCause
	v.rolesLines = res.rolesLines
	v.rolesFailure = res.rolesCause
	v.loaded = true
	return true
}

// teamViewScroll moves the open team view one line (up/down) or one page
// (page-up/page-down) with the team panel's page size, clamped at the
// top; the bottom clamps by scrollLines in the renderer, like the panel.
func (s *PickerState) teamViewScroll(key string) {
	v := s.TeamView
	switch key {
	case "up":
		v.scroll--
	case "down":
		v.scroll++
	case "page-up":
		v.scroll -= teamViewScrollPage
	case "page-down":
		v.scroll += teamViewScrollPage
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
}
