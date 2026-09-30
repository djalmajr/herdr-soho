// Package spawn implements opening and reusing Herdr workers.
package spawn

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/dispatch"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/kinds"
	"github.com/djalmajr/herdr-soho/internal/layout"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/provider"
	"github.com/djalmajr/herdr-soho/internal/taskreport"
	"github.com/djalmajr/herdr-soho/internal/text"
)

var busyRetryDelay = time.Second

const (
	herdrStartTimeout = 30 * time.Second
	startPollWindow   = 5 * time.Second
)

var agentNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func Warn(message string, ctx *core.Config, env platform.Env, command string) {
	core.Warn(message, filepath.Join(core.StateDirPath(ctx, env, mustGetwd()), "friction.log"), command)
}

func mustGetwd() string { cwd, _ := os.Getwd(); return cwd }

func approvalsRank(mode string) int {
	switch mode {
	case "ask":
		return 1
	case "edits":
		return 2
	case "full":
		return 3
	default:
		return 0
	}
}

func liveAgents(env platform.Env) []any { return herdr.LiveAgents(env, herdr.Timeout) }

func AgentNameTaken(name string, env platform.Env) bool {
	for _, a := range liveAgents(env) {
		if fieldString(a, "name") == name {
			return true
		}
	}
	return false
}

func UniqueName(base string, env platform.Env) string {
	name := base
	for i := 2; AgentNameTaken(name, env); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

func ResolvedRoleKind(role string, ctx *core.Config, env platform.Env, cwd string) string {
	return core.ResolvedRoleKind(role, ctx, env, cwd)
}

func ResolveSpawnEffort(role, lane, kind string, kindLayer int, ctx *core.Config, env platform.Env, cwd, model string) string {
	effort := ""
	if lane != "" {
		layer := kindLayer
		effort = core.LaneAttr(ctx, lane, "effort", &layer, env)
	}
	if effort == "" {
		effort = core.Cfg(ctx, "role_"+strings.ReplaceAll(role, "-", "_")+"_effort", "", env)
	}
	if effort == "" {
		effort = core.Cfg(ctx, "effort_"+kind, "", env)
	}
	if effort == "" {
		if file := core.RoleFile(role, env, cwd); file != "" {
			effort = core.FmGet(file, "effort")
		}
	}
	if text.HasWord(strings.Join(core.EffortLadder, " "), effort) {
		effort = kinds.ClampTo(kinds.ClampTo(effort, kinds.KindEffortCeiling(kind)), core.Cfg(ctx, "max_effort", "", env))
		if kind == "codex" {
			effort = kinds.ClampTo(effort, kinds.CodexEffortCeiling(model, env))
		}
	}
	return effort
}

func ResumeArg(kind string, args []string) string {
	var bad []string
	switch kind {
	case "claude":
		bad = []string{"-c", "--continue", "-r", "--resume"}
	case "cursor":
		bad = []string{"-c", "--cloud", "--resume"}
	}
	for _, arg := range args {
		for _, b := range bad {
			if arg == b || strings.HasPrefix(b, "--") && strings.HasPrefix(arg, b+"=") {
				return arg
			}
		}
	}
	return ""
}

// FindReusable selects an idle compatible worker from the roster.
func FindReusable(role, kind, workerCwd, name, wantModel, wantApprovals string, ctx *core.Config, env platform.Env, cwd string) (string, string, bool) {
	multi := core.Cfg(ctx, "multi_role", "on", env)
	sd := core.StateDirPath(ctx, env, cwd)
	blockedName, blockedCause, crossHit := "", "", ""
	for _, line := range core.RosterRows(sd) {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		at := func(i int) string {
			if i < len(f) {
				return f[i]
			}
			return ""
		}
		nm := at(0)
		if nm == "" || name != "" && nm != name || at(2) != kind || at(6) != workerCwd {
			continue
		}
		isSame := at(3) == role
		wModel, wApprovals := "", ""
		if len(f) >= 9 {
			wModel = at(8)
		}
		if len(f) >= 10 {
			wApprovals = at(9)
		}
		if isSame {
			if len(f) >= 9 && wModel != wantModel {
				continue
			}
			if len(f) >= 10 && approvalsRank(wApprovals) < approvalsRank(defaultValue(wantApprovals, "ask")) {
				continue
			}
		} else {
			if multi != "on" || len(f) < 11 || wantModel == "" || wModel == "" || wModel != wantModel || wApprovals == "" || approvalsRank(defaultValue(wantApprovals, "ask")) == 0 || approvalsRank(wApprovals) < approvalsRank(defaultValue(wantApprovals, "ask")) {
				continue
			}
			if core.IsReviewRole(role) && (core.RoleIsEdit(at(3), env, cwd) || core.HistoryHasEdit(at(10), env, cwd)) {
				continue
			}
		}
		args := ""
		if len(f) >= 14 {
			args = at(13)
		}
		if args != ConfigNativeArgs(kind, "", role, ctx, env, cwd, nil) {
			continue
		}
		report := core.LastReport(sd, nm)
		if report != "" {
			st, err := os.Stat(report)
			if err != nil || st.Size() == 0 {
				continue
			}
		}
		st := herdr.AgentState(nm, env, herdr.Timeout, nil)
		if st.State == "unavailable" {
			if isSame && blockedName == "" {
				blockedName, blockedCause = nm, st.Cause
			}
			continue
		}
		if st.State == "idle" || st.State == "done" {
			if isSame {
				return nm, "", false
			}
			if crossHit == "" {
				crossHit = nm
			}
		}
	}
	if blockedName != "" {
		return "", blockedName + "\x00" + blockedCause, true
	}
	return crossHit, "", false
}

// laneReuseCandidate reports whether the lane worker row (state from
// herdr agent get) is a reuse candidate for this spawn: idle or done, no
// pending report, not locked for a review role, and kind, resolved model
// (roster column 8) and cwd (roster column 6, canonical via
// dispatch.SamePath) all matching the request.
func laneReuseCandidate(f []string, state, role, kind, model, workerCwd, sd string, env platform.Env, cwd string) bool {
	if state != "idle" && state != "done" {
		return false
	}
	nm := fieldAt(f, 0)
	rep := core.LastReport(sd, nm)
	if rep != "" {
		info, err := os.Stat(rep)
		if err != nil || info.Size() == 0 {
			return false
		}
	}
	if core.IsReviewRole(role) && (core.RoleIsEdit(fieldAt(f, 3), env, cwd) || core.HistoryHasEdit(fieldAt(f, 10), env, cwd)) {
		return false
	}
	return fieldAt(f, 2) == kind && fieldAt(f, 8) == model && dispatch.SamePath(fieldAt(f, 6), workerCwd, platform.Current())
}

// laneReusePick scans the lane's live workers in roster order and returns the
// first idle worker that is a reuse candidate for the request, or "" when no
// idle worker matches (each mismatched idle then counts as occupied).
func laneReusePick(sd, lane, role, kind, model, workerCwd string, env platform.Env, cwd string) string {
	for _, row := range core.LaneWorkers(sd, lane) {
		f := strings.Split(row, "\t")
		nm := fieldAt(f, 0)
		if nm == "" {
			continue
		}
		st := herdr.AgentState(nm, env, herdr.Timeout, nil)
		if laneReuseCandidate(f, st.State, role, kind, model, workerCwd, sd, env, cwd) {
			return nm
		}
	}
	return ""
}

func EmitReuse(name, role, kind string, ctx *core.Config, env platform.Env, cwd string) bool {
	sd := core.StateDirPath(ctx, env, cwd)
	line := core.RosterLine(sd, name)
	if line == "" {
		return false
	}
	f := strings.Split(line, "\t")
	prev := fieldAt(f, 3)
	if prev != role {
		core.RosterSetRole(sd, name, role)
		line = core.RosterLine(sd, name)
		f = strings.Split(line, "\t")
	}
	out := jsonjs.O("name", name, "pane_id", fieldAt(f, 1), "kind", kind, "role", role, "family", fieldAt(f, 4), "reused", true, "previous_role", prev, "status", "ready")
	_, _ = fmt.Fprintln(platform.Stdout, jsonjs.StringifyIndent(out, 2))
	return true
}

func fieldAt(values []string, i int) string {
	if i >= 0 && i < len(values) {
		return values[i]
	}
	return ""
}

// renameLaneWorker renames the reused worker's agent to newName with the same
// herdr agent rename call EnsureOrchestratorName uses; it returns the cause
// extracted from the result when the rename failed, empty on success.
func renameLaneWorker(newName, pane string, env platform.Env) string {
	r := platform.RunCli("herdr", []string{"agent", "rename", pane, newName}, platform.RunOptions{Env: env, TimeoutMs: int(herdr.Timeout.Milliseconds())})
	if !r.NotFound && r.Status != nil && *r.Status == 0 {
		return ""
	}
	raw := r.Stderr
	if raw == "" {
		raw = r.Stdout
	}
	if code, msg := renameErrorInfo(raw); code != "" {
		if cause := text.SanitizeCause(code + ": " + msg); cause != "" {
			return cause
		}
	}
	rc := 1
	if r.NotFound {
		rc = 127
	} else if r.Status != nil {
		rc = *r.Status
	}
	if cause := text.SanitizeCause(raw); cause != "" {
		return cause
	}
	return fmt.Sprintf("herdr agent rename failed (exit %d)", rc)
}

func renameErrorInfo(raw string) (string, string) {
	v, err := jsonjs.Parse([]byte(raw))
	if err != nil {
		return "", ""
	}
	o, ok := v.(*jsonjs.Object)
	if !ok {
		return "", ""
	}
	e, ok := o.Get("error")
	if !ok {
		return "", ""
	}
	eo, ok := e.(*jsonjs.Object)
	if !ok {
		return "", ""
	}
	code, _ := eo.Get("code")
	msg, _ := eo.Get("message")
	cs, _ := code.(string)
	ms, _ := msg.(string)
	if cs == "" {
		return "", ""
	}
	return cs, ms
}

// moveNameStateFiles moves the per-agent state files from oldName to newName
// after a successful lane rename, so the worker's earlier session is read back
// under the new name: the last-report pointer (core.LastReportPath), the task
// title file ("task-" plus the name, as internal/core reads and writes it),
// the task report pointer (taskreport.TaskReportPointerPath) and every wait
// marker the wait package stores as wait/<agent>.<marker>. Missing files are
// not an error; a failing rename becomes a warning and the spawn continues.
func moveNameStateFiles(sd, oldName, newName string, ctx *core.Config, env platform.Env) {
	move := func(display, from, to string) {
		if _, err := os.Stat(from); err != nil {
			return
		}
		if err := os.Rename(from, to); err != nil {
			Warn(fmt.Sprintf("spawn: could not move '%s' to the name '%s': %s", display, newName, text.SanitizeCause(err.Error())), ctx, env, "spawn")
		}
	}
	move("last-report-"+oldName, core.LastReportPath(sd, oldName), core.LastReportPath(sd, newName))
	move("task-"+oldName, filepath.Join(sd, "task-"+oldName), filepath.Join(sd, "task-"+newName))
	oldPointer := taskreport.TaskReportPointerPath(sd, oldName)
	move(filepath.Base(oldPointer), oldPointer, taskreport.TaskReportPointerPath(sd, newName))
	if entries, err := os.ReadDir(filepath.Join(sd, "wait")); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), oldName+".") {
				continue
			}
			rest := strings.TrimPrefix(entry.Name(), oldName+".")
			move("wait/"+entry.Name(), filepath.Join(sd, "wait", entry.Name()), filepath.Join(sd, "wait", newName+"."+rest))
		}
	}
}
func fieldString(value any, key string) string {
	if o, ok := value.(*jsonjs.Object); ok {
		v, _ := o.Get(key)
		s, _ := v.(string)
		return s
	}
	return ""
}
func defaultValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

type spawnOptions struct {
	role, name, kind, direction, ratio, cwd, pane, timeout, effort, model, approvals, reuse, tabLabel string
	kindSet                                                                                           bool
	native                                                                                            []string
}

func parseArgs(argv []string, cwd string) spawnOptions {
	o := spawnOptions{role: fieldAt(argv, 0), cwd: cwd}
	if o.role == "" {
		core.DieFriction("spawn: missing role", 2, "", "")
	}
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			o.native = append(o.native, argv[i+1:]...)
			break
		}
		switch a {
		case "--reuse":
			o.reuse = "on"
			continue
		case "--fresh":
			o.reuse = "off"
			continue
		case "--tab-label", "--effort", "--model", "--approvals", "--name", "--kind", "--direction", "--ratio", "--cwd", "--pane", "--timeout":
		default:
			core.DieFriction("spawn: unknown option "+a, 2, "", "")
		}
		if i+1 >= len(argv) {
			core.DieFriction("spawn: "+a+" expects a value", 2, "", "")
		}
		v := argv[i+1]
		i++
		switch a {
		case "--tab-label":
			o.tabLabel = v
		case "--effort":
			o.effort = v
		case "--model":
			o.model = v
		case "--approvals":
			o.approvals = v
		case "--name":
			o.name = v
		case "--kind":
			if v == "" {
				core.DieFriction("spawn: --kind expects a kind", 2, "", "")
			}
			o.kind, o.kindSet = v, true
		case "--direction":
			o.direction = v
		case "--ratio":
			o.ratio = v
		case "--cwd":
			o.cwd = filepath.Clean(filepath.Join(cwd, v))
			if filepath.IsAbs(v) {
				o.cwd = filepath.Clean(v)
			}
		case "--pane":
			o.pane = v
		case "--timeout":
			o.timeout = v
		}
	}
	return o
}

func CmdSpawn(argv []string, ctx *core.Config, env platform.Env, cwd string) {
	o := parseArgs(argv, cwd)
	if o.name != "" && !agentNameRE.MatchString(o.name) {
		core.DieFriction(fmt.Sprintf("invalid agent name '%s' (must match [a-z][a-z0-9_-]{0,31})", o.name), 2, "", "")
	}
	info, err := os.Stat(o.cwd)
	if err != nil || !info.IsDir() {
		core.DieFriction("spawn: --cwd "+o.cwd+" is not a directory", 2, "", "")
	}
	EnsureOrchestratorName(ctx, env)
	core.ResolveRole(o.role, env, cwd)
	if o.role == "planner" {
		core.DieFriction("spawn planner: the orchestrator is the planner and does not open a pane. Plan in this session.", 12, "", "")
	}
	res := core.ResolveRoleSettings(o.role, ctx, env, cwd, core.RoleFlags{Kind: o.kind, Model: o.model, Effort: o.effort, Approvals: o.approvals})
	lane, kind := res.Lane, res.Kind
	if lane == "" && core.LanesEnabled(ctx, env) {
		if core.PanesValue(ctx, env) == "2" && core.IsReviewRole(o.role) && !core.CustomLanesPresent(ctx, env) {
			core.DieFriction(fmt.Sprintf("spawn: with panes=2 the orchestrator reviews (pick its model family by hand); role '%s' has no lane. Use panes=3 or 4, or pane_mode=flex for a temporary reviewer.", o.role), 3, "", "")
		}
		core.DieFriction(fmt.Sprintf("spawn: role '%s' is not in any lane (panes=%s). Add it with lane.<name>.roles, or set lanes=off.", o.role, core.PanesValue(ctx, env)), 3, "", "")
	}
	if kind == "" {
		core.DieFriction(fmt.Sprintf("role %s has no default kind; pass --kind", o.role), 3, "", "")
	}
	kindLayer := core.SpawnKindLayer(ctx, o.role, lane, o.kindSet, env)
	if _, ok := platform.FindExecutable(kinds.KindExe(kind), env, platform.Current()); !ok {
		Warn("executable '"+kinds.KindExe(kind)+"' not found in PATH; herdr agent start may fail", ctx, env, "spawn")
	}
	if o.role == "sub-orchestrator" && kind == "codex" && !strings.Contains(core.Cfg(ctx, "args_codex", "", env), "danger-full-access") {
		Warn("sub-orchestrator on codex: its sandbox blocks the Herdr socket (every 'herdr' call fails with Operation not permitted). Use --kind claude, or set args.codex=-s danger-full-access if you accept that.", ctx, env, "spawn")
	}
	o.effort, o.model, o.approvals = res.Effort, res.ModelSpec, res.Approvals
	modelSpec := o.model
	if o.approvals != "ask" && o.approvals != "edits" && o.approvals != "full" {
		core.DieFriction(fmt.Sprintf("invalid approvals '%s' (ask|edits|full)", o.approvals), 2, "", "")
	}
	if o.timeout == "" {
		o.timeout = core.Cfg(ctx, "spawn_timeout", "60000", env)
	}
	if o.effort != "" {
		if !text.HasWord(strings.Join(core.EffortLadder, " "), o.effort) {
			core.DieFriction(fmt.Sprintf("invalid effort '%s' (low|medium|high|xhigh|max)", o.effort), 2, "", "")
		}
		clamped := clampSpawnEffort(o.effort, kind, core.Cfg(ctx, "max_effort", "max", env))
		if clamped != o.effort {
			Warn(fmt.Sprintf("effort '%s' clamped to '%s' (kind ceiling %s, max_effort %s)", o.effort, clamped, kinds.KindEffortCeiling(kind), core.Cfg(ctx, "max_effort", "max", env)), ctx, env, "spawn")
			o.effort = clamped
		}
	}
	if modelSpec != "" {
		o.model, _ = kinds.ResolveModel(kind, modelSpec, o.effort, env, func(msg string) { Warn(msg, ctx, env, "spawn") })
	}
	if kind == "codex" && o.effort != "" {
		mc := kinds.CodexEffortCeiling(o.model, env)
		if kinds.EffortRank(o.effort) > kinds.EffortRank(mc) {
			if kinds.CodexModelCeiling(o.model, env) != "" {
				Warn(fmt.Sprintf("codex model %s supports up to '%s'; effort '%s' clamped", o.model, mc, o.effort), ctx, env, "spawn")
			} else {
				label := o.model
				if label == "" {
					label = "(CLI default)"
				}
				Warn(fmt.Sprintf("codex model %s is not in ~/.codex/models_cache.json; effort '%s' clamped to '%s'", label, o.effort, mc), ctx, env, "spawn")
			}
			o.effort = mc
		}
	}
	if o.reuse == "" {
		o.reuse = core.Cfg(ctx, "reuse_workers", "on", env)
	}
	sd := core.StateDir(ctx, env, cwd)
	burst := false
	if lane != "" && o.pane == "" {
		d := core.LaneDecide(ctx, lane, o.role, env, cwd, o.reuse == "on")
		for _, gone := range d.Gone {
			core.RosterRemove(sd, gone)
			Warn(fmt.Sprintf("lane '%s' worker '%s' is gone; opening a new pane", lane, gone), ctx, env, "spawn")
		}
		burst = core.PaneMode(ctx, env) == "flex" && has(core.SplitRoles(core.Cfg(ctx, "flex_roles", "reviewer,documenter", env)), o.role) && (d.Decision == "busy" || d.Capacity == 0 && d.Decision == "absent") && len(core.LiveBurstWorkers(sd, env)) < core.FlexExtra(ctx, env)
		switch d.Decision {
		case "reuse":
			// A12: --name never swaps the names of live lane workers. A live
			// worker already named o.name is reused as-is when it is a
			// candidate; when it exists on the lane and is not, the busy-lane
			// rule applies. Only when no live agent is named o.name does a
			// different candidate get renamed to it (the rename flow below).
			selected := ""
			if o.name != "" {
				named := false
				for _, row := range core.LaneWorkers(sd, lane) {
					rf := strings.Split(row, "\t")
					if fieldAt(rf, 0) != o.name {
						continue
					}
					named = true
					st := herdr.AgentState(o.name, env, herdr.Timeout, nil)
					if laneReuseCandidate(rf, st.State, o.role, kind, o.model, o.cwd, sd, env, cwd) {
						selected = o.name
					}
					break
				}
				if named && selected == "" {
					busyLane(lane, o.name, "", o.role, ctx, env, cwd)
				}
			}
			if selected == "" {
				line := core.RosterLine(sd, d.Name)
				f := strings.Split(line, "\t")
				actual := fieldAt(f, 2)
				if core.LaneAttr(ctx, lane, "kind", nil, env) == "" {
					sessionModel := fieldAt(f, 8)
					sessionEffort := ResolveSpawnEffort(fieldAt(f, 3), lane, actual, kindLayer, ctx, env, cwd, sessionModel)
					if actual != kind || sessionEffort != o.effort {
						mismatchLane(d.Name, lane, actual, kind, sessionModel, o.model, sessionEffort, o.effort, ctx, env, cwd)
					}
				} else if actual != kind {
					mismatchSimple(d.Name, lane, actual, kind, ctx, env, cwd)
				}
				// A12: the resolved model (roster column 8) and the cwd
				// (roster column 6, both canonical via dispatch.SamePath) must
				// match the request too; a mismatched idle is not reused and
				// the lane decision proceeds as if it were busy — a new pane
				// when the lane has a slot, exit 13 when it does not.
				if fieldAt(f, 8) != o.model || !dispatch.SamePath(fieldAt(f, 6), o.cwd, platform.Current()) {
					selected = laneReusePick(sd, lane, o.role, kind, o.model, o.cwd, env, cwd)
					if selected == "" {
						if d.N < d.Capacity {
							break // the lane has a slot: open a new pane
						}
						core.DieFriction(fmt.Sprintf("spawn: lane '%s' has no idle worker matching kind '%s', model '%s' and cwd '%s' (idle: '%s' runs %s %s in %s); release it or raise the lane's panes", lane, kind, o.model, o.cwd, d.Name, actual, fieldAt(f, 8), fieldAt(f, 6)), 13, "", "")
					}
				} else {
					selected = d.Name
				}
			}
			line := core.RosterLine(sd, selected)
			f := strings.Split(line, "\t")
			actual := fieldAt(f, 2)
			args := ""
			if len(f) >= 14 {
				args = f[13]
			}
			wanted := ConfigNativeArgs(kind, lane, o.role, ctx, env, cwd, nil)
			if args != wanted {
				obj := jsonjs.O("status", "kind-mismatch", "lane", lane, "name", selected, "session_args", args, "requested_args", wanted)
				_, _ = fmt.Fprintln(platform.Stdout, jsonjs.Stringify(obj))
				Warn(fmt.Sprintf("lane '%s' worker '%s' was started with other native args ('%s'); this spawn wants '%s'. Release the lane, then spawn again.", lane, selected, args, wanted), ctx, env, "spawn")
				platform.Die("", 13)
			}
			name := selected
			renamedTo := ""
			if o.name != "" && o.name != selected {
				if cause := renameLaneWorker(o.name, fieldAt(f, 1), env); cause != "" {
					core.DieFriction(fmt.Sprintf("spawn: could not rename worker '%s' to '%s': %s", selected, o.name, cause), 4, "", "")
				}
				core.RosterRename(sd, selected, o.name)
				moveNameStateFiles(sd, selected, o.name, ctx, env)
				name = o.name
				renamedTo = o.name
			}
			EmitReuse(name, o.role, actual, ctx, env, cwd)
			sameTreeEditors(name, o.role, fieldAt(f, 6), sd, env, cwd, ctx)
			warnMsg := fmt.Sprintf("reusing idle lane '%s' worker '%s' as %s; its session already holds earlier briefs", lane, selected, o.role)
			if renamedTo != "" {
				warnMsg = fmt.Sprintf("reusing idle lane '%s' worker '%s' as %s, renamed to '%s'; its session already holds earlier briefs", lane, selected, o.role, renamedTo)
			}
			Warn(warnMsg, ctx, env, "spawn")
			return
		case "busy":
			if burst {
				break
			}
			if d.Candidate != "" && o.reuse != "on" {
				busyLane(lane, d.Candidate, fmt.Sprintf("lane '%s' already has idle worker '%s'. Release it before --fresh, or dispatch on it. Run 'wait %s', then dispatch.", lane, d.Candidate, d.Candidate), o.role, ctx, env, cwd)
			}
			if d.Capacity == 0 {
				busyLane(lane, d.Name, "", o.role, ctx, env, cwd)
			}
			busyLane(lane, d.Name, fmt.Sprintf("lane '%s' is full (%d of %d: %s). Run 'wait %s', then dispatch.", lane, d.N, d.Capacity, strings.Join(d.Occupants, " "), d.Name), o.role, ctx, env, cwd)
		case "open":
		case "absent":
			if d.Capacity == 0 && !burst {
				busyLane(lane, d.Name, "", o.role, ctx, env, cwd)
			}
		case "unavailable":
			core.DieFriction(fmt.Sprintf("lane '%s' worker '%s' matches but herdr agent get failed (%s). Not spawning a replacement; it may still be live.", lane, d.Name, d.Cause), 4, "", "")
		case "locked":
			core.DieFriction(fmt.Sprintf("lane '%s' worker '%s' has edited and cannot take review role '%s'.", lane, d.Name, o.role), 5, "", "")
		default:
			core.DieFriction(fmt.Sprintf("spawn: unexpected lane decision '%s'", d.Decision), 4, "", "")
		}
	}
	if lane == "" && o.reuse == "on" && o.pane == "" {
		reused, unavailable, _ := FindReusable(o.role, kind, o.cwd, o.name, o.model, o.approvals, ctx, env, cwd)
		if reused != "" {
			previous := fieldAt(strings.Split(core.RosterLine(sd, reused), "\t"), 3)
			EmitReuse(reused, o.role, kind, ctx, env, cwd)
			sameTreeEditors(reused, o.role, o.cwd, sd, env, cwd, ctx)
			if previous == o.role {
				Warn(fmt.Sprintf("reusing idle worker '%s' (%s, %s); its session already holds earlier briefs", reused, kind, o.role), ctx, env, "spawn")
			} else {
				Warn(fmt.Sprintf("reusing idle worker '%s' (%s, was %s, now %s); its session already holds earlier briefs", reused, kind, previous, o.role), ctx, env, "spawn")
			}
			return
		}
		if unavailable != "" {
			parts := strings.SplitN(unavailable, "\x00", 2)
			core.DieFriction(fmt.Sprintf("worker '%s' matches this role, kind and cwd but herdr agent get failed (%s). Not spawning a replacement; it may still be live.", parts[0], fieldAt(parts, 1)), 4, "", "")
		}
	}
	core.EnforceWorkerCap(ctx, env, cwd)
	if o.name != "" {
		if AgentNameTaken(o.name, env) {
			taken := o.name
			o.name = UniqueName(o.name, env)
			if !agentNameRE.MatchString(o.name) {
				core.DieFriction(fmt.Sprintf("invalid agent name '%s' (must match [a-z][a-z0-9_-]{0,31})", o.name), 2, "", "")
			}
			Warn(fmt.Sprintf("agent name '%s' is taken by another pane or workspace; using '%s'", taken, o.name), ctx, env, "spawn")
		}
	} else {
		base := lane
		if base == "" {
			base = o.role
		}
		o.name = UniqueName(base, env)
		if !agentNameRE.MatchString(o.name) {
			core.DieFriction(fmt.Sprintf("invalid agent name '%s' (must match [a-z][a-z0-9_-]{0,31})", o.name), 2, "", "")
		}
		if AgentNameTaken(o.name, env) {
			core.DieFriction(fmt.Sprintf("agent name '%s' is already live", o.name), 3, "", "")
		}
	}
	args := buildArgs(kind, o.approvals, o.model, o.effort, ctx, env)
	native := ConfigNativeArgs(kind, lane, o.role, ctx, env, cwd, func(key, own string) {
		if own == "" {
			own = "?"
		}
		Warn(fmt.Sprintf("%s not passed: those args belong to kind %s and this spawn runs %s", key, own, kind), ctx, env, "spawn")
	})
	if native != "" {
		args = append(args, strings.Fields(native)...)
	}
	agentArgs := append(args, o.native...)
	if resume := ResumeArg(kind, agentArgs); resume != "" {
		core.DieFriction(fmt.Sprintf("spawn: '%s' would make %s resume an earlier session instead of starting a new one (for claude, -c is --continue: it resumes the orchestrator's conversation in this cwd). Remove it from the native args; codex-only flags belong in args.codex", resume, kind), 2, "", "")
	}
	created, placement, autoRegrid := false, "given", false
	layoutMode := core.Cfg(ctx, "layout", "split", env)
	focusBefore := layout.UIFocusedPane(env)
	if o.tabLabel != "" && o.pane != "" {
		Warn("--tab-label ignored: --pane places the worker in a given pane", ctx, env, "spawn")
	}
	if o.pane == "" {
		anchor, autoDir := "overflow", "layout"
		if layoutMode != "tab" && o.tabLabel == "" {
			picked := layout.PickSplitAnchor(ctx, env, cwd)
			if picked != nil {
				if picked.Overflow != "" {
					anchor, autoDir = "overflow", picked.Overflow
				} else {
					anchor, autoDir = picked.Anchor, picked.Direction
				}
			}
		}
		if anchor == "overflow" && o.direction != "" && o.tabLabel == "" {
			anchor = env.Get("HERDR_PANE_ID")
		}
		if anchor == "overflow" || anchor == "" {
			if layoutMode != "tab" && o.tabLabel == "" {
				Warn(fmt.Sprintf("caller tab has no room for another pane (%s); placing '%s' in a herd tab", autoDir, o.name), ctx, env, "spawn")
			}
			t := layout.HerdTabPane(ctx, o.cwd, o.tabLabel, o.role, env, cwd)
			o.pane = t.Pane
			created = true
			o.direction = ""
			placement = "herd"
			autoRegrid = true
		} else {
			if o.direction == "" && o.ratio == "" {
				autoRegrid = true
			}
			if o.direction == "" {
				o.direction = autoDir
			}
			ratio := o.ratio
			if ratio == "" {
				ratio = "0.5"
			}
			s := herdr.PaneSplit(anchor, o.direction, o.cwd, env, &ratio)
			if !s.Ok {
				core.DieFriction("pane split failed", 4, "", "")
			}
			o.pane = s.Pane
			created = true
			placement = "split"
		}
	}
	blocked := startAgent(o, kind, env, created, agentArgs)
	layout.RestoreFocusIfStolen(focusBefore, o.pane, o.direction, env)
	family := kinds.AgentFamily(kind, o.model)
	lastState, lastScreen := "", ""
	if !blocked {
		lastState, lastScreen, blocked = checkStartWindow(o, kind, env, created, agentArgs, ctx)
	}
	if !blocked && lastState != "unavailable" {
		if p := provider.ProviderDetect(lastState, lastScreen); p != nil && p.Status == "provider-error" && p.Auth {
			core.DieFriction(fmt.Sprintf("agent '%s' (%s) hit a terminal provider authentication error right after start: %s; pane %s stays open for inspection", o.name, kind, p.Cause, o.pane), 14, "", "")
		}
	}
	for _, stale := range staleRosterLines(sd, o.name, o.pane) {
		core.RosterRemove(sd, stale[0])
		Warn(fmt.Sprintf("replaced the stale roster line of '%s' (pane %s)", stale[0], stale[1]), ctx, env, "spawn")
	}
	core.RosterAppend(sd, []string{o.name, o.pane, kind, o.role, family, boolInt(created), o.cwd, core.NowStamp(platform.Now()), o.model, defaultValue(o.approvals, "ask"), o.role, lane, boolText(burst), native, o.effort})
	sameTreeEditors(o.name, o.role, o.cwd, sd, env, cwd, ctx)
	if placement == "herd" {
		func() {
			defer func() {
				if recover() != nil {
					Warn("relabel of the herd tabs failed (see friction)", ctx, env, "spawn")
				}
			}()
			layout.HerdTabsRelabel(ctx, env, cwd)
		}()
	}
	out := jsonjs.O("name", o.name, "pane_id", o.pane, "kind", kind, "role", o.role, "family", family, "created_pane", created, "layout", layoutMode, "placement", placement, "effort", defaultValue(o.effort, "default"), "model", defaultValue(o.model, "default"), "model_spec", modelSpec, "approvals", defaultValue(o.approvals, "ask"), "agent_args", strings.Join(agentArgs, " "), "status", func() string {
		if blocked {
			return "blocked_at_startup"
		}
		return "ready"
	}())
	if burst {
		out.Set("burst", true)
	}
	_, _ = fmt.Fprintln(platform.Stdout, jsonjs.StringifyIndent(out, 2))
	if autoRegrid && core.Cfg(ctx, "regrid", "on", env) == "on" {
		layout.AutoRegrid(ctx, env, cwd, "regrid after spawn failed; panes left as inserted (see friction)")
	}
	if blocked {
		Warn(fmt.Sprintf("agent '%s' is blocked during startup (update prompt, login, trust dialog…). Screen follows; ask the user before answering it, then: herdr agent send-keys %s <keys>; herdr agent wait %s --timeout 60000", o.name, o.name, o.name), ctx, env, "spawn")
		_, _ = fmt.Fprint(platform.Stdout, herdr.AgentRead(env, o.name, "visible", intPtr(40)))
		platform.Die("", 7)
	}
}

func clampSpawnEffort(effort, kind, maxEffort string) string {
	return kinds.ClampTo(kinds.ClampTo(effort, kinds.KindEffortCeiling(kind)), maxEffort)
}

func has(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func intPtr(v int) *int { return &v }
func boolInt(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
func boolText(v bool) string {
	if v {
		return "burst"
	}
	return ""
}
func buildArgs(kind, approvals, model, effort string, ctx *core.Config, env platform.Env) []string {
	out := kinds.KindContextArgs(kind, core.Cfg(ctx, "worker_context", "full", env))
	a, err := kinds.KindApprovalArgs(kind, approvals, func(msg string) { Warn(msg, ctx, env, "spawn") })
	if err != nil {
		core.DieFriction(err.Error(), 2, "", "")
	}
	out = append(out, a...)
	out = append(out, kinds.KindModelArgs(kind, model, effort, func(msg string) { Warn(msg, ctx, env, "spawn") })...)
	out = append(out, kinds.KindEffortArgs(kind, effort, model, env, func(msg string) { Warn(msg, ctx, env, "spawn") })...)
	return out
}

func busyLane(lane, name, hint, role string, ctx *core.Config, env platform.Env, cwd string) {
	_, _ = fmt.Fprintln(platform.Stdout, jsonjs.Stringify(jsonjs.O("status", "busy", "lane", lane, "name", name)))
	if hint == "" {
		roles := core.Cfg(ctx, "flex_roles", "reviewer,documenter", env)
		if !has(core.SplitRoles(roles), role) {
			hint = fmt.Sprintf("role '%s' may not use the temporary panel (flex_roles=%s)", role, roles)
		} else {
			live := core.LiveBurstWorkers(core.StateDirPath(ctx, env, cwd), env)
			remedy := "Raise flex_extra."
			if len(live) > 0 {
				remedy = fmt.Sprintf("Release one (release %s), or raise flex_extra.", live[0])
			}
			hint = fmt.Sprintf("lane '%s' only takes a temporary worker (pane_mode=flex) and none is free: flex_extra=%d, live temporary workers: %s. %s", lane, core.FlexExtra(ctx, env), func() string {
				if len(live) == 0 {
					return "none"
				}
				return strings.Join(live, ", ")
			}(), remedy)
		}
		if role == "" {
			// The generic lane-capacity path uses the lane occupant hint below.
			hint = fmt.Sprintf("lane '%s' is full", lane)
		}
	}
	core.Warn(hint, filepath.Join(core.StateDirPath(ctx, env, cwd), "friction.log"), "spawn")
	platform.Die("", 10)
}
func mismatchLane(name, lane, actual, want, sessionModel, requestedModel, sessionEffort, requestedEffort string, ctx *core.Config, env platform.Env, cwd string) {
	obj := jsonjs.O("status", "kind-mismatch", "lane", lane, "name", name, "session_kind", actual, "requested_kind", want, "session_model", sessionModel, "requested_model", requestedModel, "session_effort", sessionEffort, "requested_effort", requestedEffort)
	_, _ = fmt.Fprintln(platform.Stdout, jsonjs.Stringify(obj))
	core.Warn(fmt.Sprintf("lane '%s' worker '%s' is %s (model %s, effort %s); this role wants %s (model %s, effort %s). Set lane.%s.kind or release the lane, then spawn again.", lane, name, actual, defaultValue(sessionModel, "?"), defaultValue(sessionEffort, "?"), want, defaultValue(requestedModel, "?"), defaultValue(requestedEffort, "?"), lane), filepath.Join(core.StateDirPath(ctx, env, cwd), "friction.log"), "spawn")
	platform.Die("", 13)
}
func mismatchSimple(name, lane, actual, want string, ctx *core.Config, env platform.Env, cwd string) {
	obj := jsonjs.O("status", "kind-mismatch", "lane", lane, "name", name, "session_kind", actual, "requested_kind", want)
	_, _ = fmt.Fprintln(platform.Stdout, jsonjs.Stringify(obj))
	core.Warn(fmt.Sprintf("lane '%s' worker '%s' runs %s but lane.%s.kind is %s. Release the lane ('release %s --close'), then spawn again.", lane, name, actual, lane, want, name), filepath.Join(core.StateDirPath(ctx, env, cwd), "friction.log"), "spawn")
	platform.Die("", 13)
}

func startAgent(o spawnOptions, kind string, env platform.Env, created bool, args []string) bool {
	for tries := 0; ; tries++ {
		argv := []string{"agent", "start", o.name, "--kind", kind, "--pane", o.pane, "--timeout", o.timeout}
		if len(args) > 0 {
			argv = append(argv, "--")
			argv = append(argv, args...)
		}
		r := platform.RunCli("herdr", argv, platform.RunOptions{Env: env, TimeoutMs: int(startWrapperTimeoutMs(o.timeout)), MergeOutput: true})
		out := r.Stdout + r.Stderr
		if !r.NotFound && r.Status != nil && *r.Status == 0 {
			return false
		}
		if strings.Contains(out, "agent_not_ready") {
			return true
		}
		if strings.Contains(out, "agent_pane_busy") && tries < 15 {
			time.Sleep(busyRetryDelay)
			continue
		}
		if strings.TrimRight(out, "\n") != "" {
			_, _ = fmt.Fprintln(platform.Stderr, strings.TrimRight(out, "\n"))
		}
		if created {
			scr := herdr.AgentRead(env, o.pane, "visible", intPtr(40))
			tail := lastLines(scr, 5)
			closed := herdr.PaneClose(o.pane, env)
			if closed {
				core.DieFriction(fmt.Sprintf("agent start failed for %s (%s) in pane %s; closed the pane this spawn opened (last screen lines: %s)", o.name, kind, o.pane, tail), 4, "", "")
			}
			core.DieFriction(fmt.Sprintf("agent start failed for %s (%s) in pane %s; pane close failed and the pane is still open, check it for a running agent (last screen lines: %s)", o.name, kind, o.pane, tail), 4, "", "")
		}
		core.DieFriction(fmt.Sprintf("agent start failed for %s (%s) in pane %s; the pane was given (--pane) and stays open: check it for a running agent", o.name, kind, o.pane), 4, "", "")
	}
}
func startWrapperTimeoutMs(raw string) float64 {
	n, ok := text.ParseJSNumber(raw)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return 30_000
	}
	return n + 30_000
}

func updateGoneWaitMs(raw string) float64 {
	n, ok := text.ParseJSNumber(raw)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
		return 60_000
	}
	return n
}
func pollMillis(env platform.Env) time.Duration {
	n, err := strconv.Atoi(env.Get("HERDR_SOHO_WAIT_POLL_MS"))
	if err == nil && n >= 1 && n <= 3000 {
		return time.Duration(n) * time.Millisecond
	}
	return 500 * time.Millisecond
}
func checkStartWindow(o spawnOptions, kind string, env platform.Env, created bool, args []string, ctx *core.Config) (string, string, bool) {
	poll := time.Duration(PollIntervalMs(env)) * time.Millisecond
	canRelaunch := true
	blocked := false
	for {
		end := time.Now().Add(startPollWindow)
		lastScreen, lastState := "", ""
		marker := false
		for {
			st := herdr.AgentState(o.name, env, herdr.Timeout, nil)
			lastState = st.State
			screen := herdr.AgentRead(env, o.name, "visible", intPtr(40))
			if screen != "" {
				lastScreen = screen
			}
			if kind == "codex" && (strings.Contains(screen, "Updating Codex via") || strings.Contains(screen, "Please restart Codex")) {
				marker = true
			}
			if st.State == "gone" {
				if canRelaunch && marker {
					break
				}
				if len(strings.TrimSpace(lastScreen)) == 0 {
					lastScreen = herdr.AgentRead(env, o.name, "visible", intPtr(40))
				}
				providerErr := provider.ProviderDetect("gone", lastScreen)
				if providerErr != nil && providerErr.Status == "provider-error" && providerErr.Auth {
					core.DieFriction(fmt.Sprintf("agent '%s' (%s) hit a terminal provider authentication error right after start: %s; pane %s stays open for inspection", o.name, kind, providerErr.Cause, o.pane), 14, "", "")
				}
				core.DieFriction(fmt.Sprintf("agent '%s' (%s) exited right after start; last screen lines: %s", o.name, kind, lastLines(lastScreen, 5)), 4, "", "")
			}
			if marker && canRelaunch {
				break
			}
			if !marker || !time.Now().Before(end) {
				return lastState, lastScreen, blocked
			}
			time.Sleep(poll)
		}
		to := updateGoneWaitMs(o.timeout)
		until := time.Now().Add(time.Duration(to * float64(time.Millisecond)))
		st := herdr.AgentState(o.name, env, herdr.Timeout, nil)
		for st.State != "gone" && time.Now().Before(until) {
			time.Sleep(poll)
			st = herdr.AgentState(o.name, env, herdr.Timeout, nil)
		}
		if st.State != "gone" {
			return st.State, lastScreen, blocked
		}
		blocked = startAgent(o, kind, env, created, args)
		Warn(fmt.Sprintf("'%s' (%s) updated itself at start and exited; started it again", o.name, kind), ctx, env, "spawn")
		canRelaunch = false
	}
}
func lastLines(s string, n int) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if v := strings.TrimSpace(line); v != "" {
			lines = append(lines, v)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}
func staleRosterLines(sd, name, pane string) [][2]string {
	seen := map[string]bool{}
	var out [][2]string
	for _, line := range core.RosterRows(sd) {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		nm, pn := fieldAt(f, 0), fieldAt(f, 1)
		if nm == "" || seen[nm] {
			continue
		}
		if nm == name || pane != "" && pn == pane {
			seen[nm] = true
			out = append(out, [2]string{nm, pn})
		}
	}
	return out
}

func sameTreeEditors(thisName, role, workerCwd, sd string, env platform.Env, cwd string, ctx *core.Config) {
	if workerCwd == "" || !core.RoleIsEdit(role, env, cwd) {
		return
	}
	type candidate struct{ name, pane string }
	var candidates []candidate
	for _, line := range core.RosterRows(sd) {
		f := strings.Split(line, "\t")
		name := fieldAt(f, 0)
		if name == "" || name == thisName || !core.RoleIsEdit(fieldAt(f, 3), env, cwd) || fieldAt(f, 6) != workerCwd {
			continue
		}
		candidates = append(candidates, candidate{name, fieldAt(f, 1)})
	}
	if len(candidates) == 0 {
		return
	}
	var agents []any
	func() {
		defer func() { _ = recover() }()
		agents = herdr.LiveAgents(env, herdr.Timeout)
	}()
	for _, cand := range candidates {
		for _, agent := range agents {
			if fieldString(agent, "name") != cand.name || cand.pane != "" && fieldString(agent, "pane_id") != cand.pane {
				continue
			}
			Warn(fmt.Sprintf("'%s' and '%s' both edit %s: builds and test runs see each other's changes in progress; give each a git worktree (spawn --cwd <worktree>) to isolate them", thisName, cand.name, workerCwd), ctx, env, "spawn")
			break
		}
	}
}

// PollIntervalMs is the shared wait poll override used by spawn start checks.
func PollIntervalMs(env platform.Env) int {
	d := pollMillis(env)
	if d > 500*time.Millisecond {
		return 500
	}
	return int(d.Milliseconds())
}
