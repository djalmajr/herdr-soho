package doctor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/codexenv"
	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/dispatch"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/kinds"
	"github.com/djalmajr/herdr-soho/internal/layout"
	"github.com/djalmajr/herdr-soho/internal/platform"
	setupcmd "github.com/djalmajr/herdr-soho/internal/setup"
	"github.com/djalmajr/herdr-soho/internal/setuptext"
	"github.com/djalmajr/herdr-soho/internal/spawn"
	textutil "github.com/djalmajr/herdr-soho/internal/text"
)

type Say struct {
	Out      io.Writer
	OK, Warn int
}

func (s *Say) Ok(v string)      { fmt.Fprintf(s.Out, "ok     %s\n", v); s.OK++ }
func (s *Say) Warning(v string) { fmt.Fprintf(s.Out, "warn   %s\n", v); s.Warn++ }

func doctorLaneRoles(csv string) []string {
	parts := strings.Split(csv, ",")
	roles := make([]string, 0, len(parts))
	for _, role := range parts {
		role = strings.TrimFunc(role, isDoctorJSWhitespace)
		if role != "" {
			roles = append(roles, role)
		}
	}
	return roles
}

func isDoctorJSWhitespace(r rune) bool {
	return r >= 0x9 && r <= 0xd || r == 0x20 || r == 0xa0 || r == 0x1680 ||
		r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
}

var decimal = regexp.MustCompile(`^[0-9]+$`)
var teamChoice = regexp.MustCompile(`^(multi_role|role\.[A-Za-z0-9_-]+\.kind|lane\.[A-Za-z0-9_-]+\.kind)=`)

func ProjectHasRoster(ctx *core.Config, env platform.Env, cwd string) bool {
	root := core.StateRootPath(ctx, env, cwd)
	found := false
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != "agents.tsv" {
			return nil
		}
		raw, err := platform.ReadTextFile(p)
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(raw, "\n") {
			if strings.HasPrefix(line, "#") {
				continue
			}
			if strings.TrimSpace(line) != "" {
				found = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}

func setupUnifiedDiff(before, after, label string) string {
	return setupcmd.UnifiedDiff(before, after, label)
}

func CmdInit(ctx *core.Config, env platform.Env, cwd string) {
	herdr.RequireEnv(env, platform.Current(), os.Getpid(), nil)
	oldOut := platform.Stdout
	platform.Stdout = platform.Stderr
	DoctorCheck(ctx, env, cwd, platform.Stderr)
	platform.Stdout = oldOut
	state := core.StateDir(ctx, env, cwd)
	friction := filepath.Join(state, "friction.log")
	name := spawn.EnsureOrchestratorName(ctx, env)
	pane := env.Get("HERDR_PANE_ID")
	paneID := ""
	title := ""
	if pane != "" {
		r := platform.RunCli("herdr", []string{"pane", "get", pane}, platform.RunOptions{Env: env, TimeoutMs: 30000})
		var payload any
		if v, e := jsonjs.Parse([]byte(r.Stdout)); e == nil {
			payload = v
		}
		existing := ""
		if root, ok := payload.(*jsonjs.Object); ok {
			if result, ok := root.Get("result"); ok {
				if po, ok := result.(*jsonjs.Object); ok {
					if pv, ok := po.Get("pane"); ok {
						if paneObj, ok := pv.(*jsonjs.Object); ok {
							if v, ok := paneObj.Get("title"); ok && v != nil && v != false {
								existing, _ = v.(string)
							}
						}
					}
				}
			}
		}
		if r.NotFound || r.Status == nil || *r.Status != 0 {
			core.Warn(fmt.Sprintf("init: HERDR_PANE_ID '%s' is not a live pane; pane title left as is and pane_id left empty", pane), friction)
		} else {
			paneID = pane
			if existing != "" {
				title = existing
			} else {
				fresh := "orchestrator: " + filepath.Base(platform.ProjectRoot(env, cwd))
				if herdr.PaneTitle(pane, &fresh, env) {
					title = fresh
				} else {
					core.Warn("init: herdr pane report-metadata failed; pane title left as is", friction)
				}
			}
		}
	}
	first := ProjectIsFirstRun(ctx, env, cwd)
	layoutName := core.Cfg(ctx, "layout", "split", env)
	obj := jsonjs.O("orchestrator", name, "pane_id", paneID, "tab_id", env.Get("HERDR_TAB_ID"), "workspace_id", core.WorkspaceID(ctx, env, cwd), "layout", layoutName, "state_dir", state, "first_run", first, "title", title)
	fmt.Fprintln(platform.Stdout, jsonjs.StringifyIndent(obj, 2))
}

func ProjectIsFirstRun(ctx *core.Config, env platform.Env, cwd string) bool {
	root := platform.ProjectRoot(env, cwd)
	file := filepath.Join(root, ".agents", "herdr-soho.conf")
	if !isFile(file) {
		file = core.LegacyProjectConfigPath(root)
	}
	if raw, e := platform.ReadTextFile(file); e == nil {
		for _, line := range strings.Split(raw, "\n") {
			line = inlineComment.ReplaceAllString(line, "")
			line = strings.TrimSpace(line)
			if teamChoice.MatchString(line) {
				return false
			}
		}
	}
	return !ProjectHasRoster(ctx, env, cwd)
}

var inlineComment = regexp.MustCompile(`[ \t]#.*$`)

func isFile(p string) bool { st, e := os.Stat(p); return e == nil && st.Mode().IsRegular() }

func entryScript(env platform.Env) string {
	name := "herdr-soho"
	if platform.Current() == "win32" {
		name += ".cmd"
	}
	return filepath.Join(platform.SkillDir(env), "scripts", name)
}

func CmdDoctor(args []string, ctx *core.Config, env platform.Env, cwd string) {
	fix := false
	panes := ""
	where := "project"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--fix":
			fix = true
		case "--panes":
			i++
			if i < len(args) {
				panes = args[i]
			}
		case "--user":
			where = "user"
		case "--session":
			where = "session"
		default:
			platform.DieFriction(fmt.Sprintf("doctor: unknown option '%s'", args[i]), 2)
		}
	}
	if fix {
		doctorFix(where, panes, ctx, env, cwd)
		if env.Get("HERDR_SOHO_LIB") == "1" {
			return
		}
		fresh := core.LoadConfig(env, cwd)
		DoctorCheck(&fresh, env, cwd, platform.Stdout)
		return
	}
	DoctorCheck(ctx, env, cwd, platform.Stdout)
}

func doctorFix(where, panes string, ctx *core.Config, env platform.Env, cwd string) {
	dest := core.ConfigFileFor(where, env, cwd)
	if where == "session" {
		dest = core.SessionConfPath(ctx, env, cwd)
		if dest == "" {
			platform.DieFriction("doctor: --session needs a Herdr workspace", 2)
		}
	}
	if panes != "" && panes != "2" && panes != "3" && panes != "4" {
		platform.DieFriction("doctor --fix: --panes must be 2, 3 or 4", 2)
	}
	if panes == "" {
		legacy := dest
		if where == "project" {
			legacy = core.LegacyProjectConfigPath(platform.ProjectRoot(env, cwd))
		} else if where == "user" {
			legacy = core.LegacyUserConfigPath(platform.Current(), env)
		}
		panes = core.FileKeyValue(core.EffectiveConfigFile(dest, legacy), "panes")
	}
	if panes != "2" && panes != "3" && panes != "4" {
		if panes == "" {
			platform.DieFriction(fmt.Sprintf("doctor --fix: panes is not set in %s. Orchestrator: ask the user whether to run 2, 3 or 4 panes, then re-run 'doctor --fix --panes <n>'.", dest), 2)
		}
		platform.DieFriction(fmt.Sprintf("doctor --fix: panes=%s in %s is not 2, 3 or 4", panes, dest), 2)
	}
	before, _ := platform.ReadTextFile(dest)
	before = strings.TrimRight(before, "\n")
	for _, line := range core.ApplyLaneFile(dest, panes, env, cwd) {
		fmt.Fprintln(platform.Stdout, line)
	}
	after, _ := platform.ReadTextFile(dest)
	if before == strings.TrimRight(after, "\n") {
		fmt.Fprintf(platform.Stdout, "doctor --fix: no changes in %s\n", dest)
	} else {
		fmt.Fprintf(platform.Stdout, "doctor --fix: updated %s\n", dest)
		fmt.Fprint(platform.Stdout, setupUnifiedDiff(before, after, dest))
	}
}
func DoctorCheck(ctx *core.Config, env platform.Env, cwd string, out io.Writer) {
	s := &Say{Out: out}
	if env.Get("HERDR_ENV") == "1" {
		herdrContextCheck(env, s)
	} else {
		s.Warning(codexenv.DiagnoseOutsideHerdr("HERDR_ENV != 1: not inside a Herdr pane", env, platform.Current(), 0, nil))
	}
	_, herdrFound := platform.FindExecutable("herdr", env, platform.Current())
	cliVersion := ""
	if herdrFound {
		v := platform.RunCli("herdr", []string{"--version"}, platform.RunOptions{Env: env})
		for _, line := range strings.Split(v.Stdout, "\n") {
			f := strings.Fields(line)
			if len(f) > 1 {
				cliVersion += f[1] + "\n"
			}
		}
		srv := platform.RunCli("herdr", []string{"status", "server"}, platform.RunOptions{Env: env})
		server := regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`).FindString(srv.Stdout)
		cliVersion = strings.TrimSuffix(cliVersion, "\n")
		if server != "" && server != cliVersion {
			s.Warning(fmt.Sprintf("herdr client %s vs server %s: restart the server (herdr update --handoff) so CLI and server agree", cliVersion, server))
		} else {
			s.Ok("herdr " + cliVersion)
		}
	} else {
		s.Warning("herdr CLI not in PATH")
	}
	home := platform.HomeDir(platform.Current(), env)
	skill := ""
	for _, p := range []string{filepath.Join(home, ".agents", "skills", "herdr", "SKILL.md"), filepath.Join(home, ".claude", "skills", "herdr", "SKILL.md")} {
		if isFile(p) {
			skill = p
			break
		}
	}
	if skill == "" {
		s.Warning("official herdr skill not installed: bunx skills add herdrdev/herdr --skill herdr -g -y")
	} else {
		same := false
		if herdrFound {
			r := platform.RunCli("herdr", []string{"--skill"}, platform.RunOptions{Env: env})
			b, e := platform.ReadTextFile(skill)
			same = e == nil && r.Stdout == b
		}
		if herdrFound && !same {
			s.Warning(fmt.Sprintf("official herdr skill at %s differs from 'herdr --skill' (stale after herdr update?): bunx skills update herdr -g", skill))
		} else {
			s.Ok("official herdr skill matches the binary (" + skill + ")")
		}
	}
	used := usedKinds(ctx, env, cwd)
	var missing []string
	for _, k := range used {
		if _, ok := platform.FindExecutable(kindExe(k), env, platform.Current()); !ok {
			missing = append(missing, k)
		}
	}
	if len(used) == 0 {
		s.Ok("kinds: none configured (spawn passes --kind)")
	} else if len(missing) == 0 {
		s.Ok("kinds installed: " + strings.Join(used, " "))
	} else {
		s.Warning("kinds in use but not in PATH: " + strings.Join(missing, " ") + " (roles or lanes using them will fail to start)")
	}
	declared, warnings := kinds.OwnProviderDoctorLines(ctx, env, cwd)
	for _, w := range warnings {
		s.Warning(w)
	}
	if declared && len(warnings) == 0 {
		s.Ok("own providers: no known trap")
	}
	d := core.StateRoot(ctx, env, cwd)
	if core.Nowrite(env) {
		if st, e := os.Stat(d); e != nil {
			if os.IsNotExist(e) {
				s.Ok("state dir absent (no-write mode, not created): " + d)
			} else {
				s.Warning("state dir not writable: " + d)
			}
		} else if st.IsDir() && isWritable(d) {
			s.Ok("state dir writable: " + d)
		} else {
			s.Warning("state dir not writable: " + d)
		}
	} else {
		if os.MkdirAll(d, 0777) == nil && isWritable(d) {
			s.Ok("state dir writable: " + d)
		} else {
			s.Warning("state dir not writable: " + d)
		}
	}
	for _, w := range core.LegacyDoctorWarnings(env, cwd, platform.Current()) {
		s.Warning(w)
	}
	layoutName := core.Cfg(ctx, "layout", "split", env)
	if layoutName == "split" || layoutName == "tab" {
		s.Ok(fmt.Sprintf("config: layout=%s approvals=%s auto_approve=%s reuse_workers=%s multi_role=%s worker_context=%s", layoutName, core.Cfg(ctx, "approvals", "", env), core.Cfg(ctx, "auto_approve", "", env), core.Cfg(ctx, "reuse_workers", "", env), core.Cfg(ctx, "multi_role", "on", env), core.Cfg(ctx, "worker_context", "", env)))
	} else {
		s.Warning(fmt.Sprintf("config: invalid layout '%s' (split|tab)", layoutName))
	}
	mr := core.Cfg(ctx, "multi_role", "on", env)
	if mr != "on" && mr != "off" {
		s.Warning(fmt.Sprintf("config: multi_role='%s' is not on|off (cross-role reuse stays off until it is)", mr))
	}
	capRaw := core.Cfg(ctx, "split_max_panes", "4", env)
	cap := layout.SplitCap(ctx, env)
	if !decimal.MatchString(capRaw) {
		s.Warning(fmt.Sprintf("config: split_max_panes='%s' is not a number (using 4)", capRaw))
	} else if cap < 2 {
		s.Warning(fmt.Sprintf("config: split_max_panes=%d leaves no room next to the caller; every worker will overflow into herd tabs (set 2 or more)", cap))
	} else {
		s.Ok(fmt.Sprintf("config: split_max_panes=%d split_min_pane=%g", cap, layout.SplitMin(ctx, env)))
	}
	mwRaw := core.Cfg(ctx, "max_workers", "3", env)
	mw := core.MaxWorkers(ctx, env)
	if !decimal.MatchString(mwRaw) {
		s.Warning(fmt.Sprintf("config: max_workers='%s' is not a number (using 3)", mwRaw))
	} else if mw == "0" {
		s.Ok("config: max_workers=0 (no cap on live workers)")
	} else {
		s.Ok(fmt.Sprintf("config: max_workers=%s (orchestrator + %s workers)", mw, mw))
	}
	if core.LanesEnabled(ctx, env) {
		doctorLaneWarnings(ctx, env, cwd, s)
	} else {
		s.Ok("config: lanes=off (per-role reuse unchanged)")
	}
	doctorDiscardedModels(ctx, env, cwd, s)
	doctorModelPairs(ctx, env, cwd, s)
	doctorLaneArgs(ctx, env, s)
	doctorCodexNetwork(ctx, env, cwd, s)
	codexenv.DoctorCodexPolicyWarnings(env, platform.Current(), s.Warning)
	doctorFeedback(ctx, env, s)
	minRaw := core.Cfg(ctx, "split_min_pane", "0.18", env)
	if !regexp.MustCompile(`^0?\.[0-9]+$`).MatchString(minRaw) {
		s.Warning(fmt.Sprintf("config: split_min_pane='%s' must be a fraction like 0.18 (using 0.18)", minRaw))
	}
	hlm := core.Cfg(ctx, "herd_label_max", "16", env)
	if !decimal.MatchString(hlm) {
		s.Warning(fmt.Sprintf("config: herd_label_max='%s' is not a number (using 16)", hlm))
	} else {
		s.Ok(fmt.Sprintf("config: herd_label='%s' herd_label_max=%d", core.Cfg(ctx, "herd_label", "{roles}", env), layout.HerdLabelMax(ctx, env)))
	}
	checkSetup(ctx, env, cwd, s)
	fmt.Fprintf(out, "first_run: %t\n%d ok, %d warning(s)\n", ProjectIsFirstRun(ctx, env, cwd), s.OK, s.Warn)
}

// herdrContextCheck verifies the context HERDR_ENV=1 claims: the inherited
// pane id must be a live pane of the caller's workspace, or the current pane
// must resolve with the same argv core.WorkspaceID uses.
func herdrContextCheck(env platform.Env, s *Say) {
	if pane := env.Get("HERDR_PANE_ID"); pane != "" {
		r := platform.RunCli("herdr", []string{"pane", "get", pane}, platform.RunOptions{Env: env, TimeoutMs: 30000})
		if r.NotFound || r.Status == nil || *r.Status != 0 {
			s.Warning(fmt.Sprintf("Herdr context invalid: HERDR_PANE_ID=%s is not a live pane (%s)", pane, herdrCause("pane get", r)))
			return
		}
		ws := env.Get("HERDR_WORKSPACE_ID")
		if ws == "" {
			s.Ok("inside Herdr (HERDR_ENV=1)")
			return
		}
		paneWs := paneWorkspaceID(r.Stdout)
		if paneWs == ws {
			s.Ok("inside Herdr (HERDR_ENV=1)")
			return
		}
		s.Warning(fmt.Sprintf("Herdr context invalid: pane %s belongs to workspace '%s', not HERDR_WORKSPACE_ID '%s'", pane, paneWs, ws))
		return
	}
	r := platform.RunCli("herdr", []string{"pane", "current", "--current"}, platform.RunOptions{Env: env, TimeoutMs: 30000})
	if !r.NotFound && r.Status != nil && *r.Status == 0 {
		s.Ok("inside Herdr (HERDR_ENV=1)")
		return
	}
	s.Warning(fmt.Sprintf("Herdr context invalid: HERDR_PANE_ID is unset and herdr pane current failed (%s)", herdrCause("pane current", r)))
}

// paneWorkspaceID reads result.pane.workspace_id from a herdr pane payload,
// the same navigation core.WorkspaceID applies to the pane current result.
func paneWorkspaceID(stdout string) string {
	v, err := jsonjs.Parse([]byte(stdout))
	if err != nil {
		return ""
	}
	root, ok := v.(*jsonjs.Object)
	if !ok {
		return ""
	}
	result, _ := root.Get("result")
	resultObject, ok := result.(*jsonjs.Object)
	if !ok {
		return ""
	}
	pane, _ := resultObject.Get("pane")
	paneObject, ok := pane.(*jsonjs.Object)
	if !ok {
		return ""
	}
	id, ok := paneObject.Get("workspace_id")
	if !ok || id == nil || id == false {
		return ""
	}
	if s, ok := id.(string); ok {
		return s
	}
	return fmt.Sprint(id)
}

// herdrCause extracts the same short failure cause internal/herdr derives
// from a failed herdr call: the structured error code and message when the
// output carries one, the sanitized raw output otherwise, and a fallback
// with the exit code when there is nothing to read.
func herdrCause(what string, r platform.RunResult) string {
	if r.NotFound {
		return "herdr CLI not found in PATH"
	}
	if r.TimedOut || r.Error == "ETIMEDOUT" {
		return fmt.Sprintf("herdr %s timed out after 30s", what)
	}
	fallback := fmt.Sprintf("herdr %s failed (exit %d)", what, herdrExitCode(r))
	raw := r.Stderr
	if raw == "" {
		raw = r.Stdout
	}
	if code, message := herdrErrorInfo(raw); code != "" {
		if cause := textutil.SanitizeCause(code + ": " + message); cause != "" {
			return cause
		}
	}
	if raw == "" {
		return fallback
	}
	if cause := textutil.SanitizeCause(raw); cause != "" {
		return cause
	}
	return fallback
}

func herdrExitCode(r platform.RunResult) int {
	if r.Status != nil {
		return *r.Status
	}
	if r.Signal != "" {
		return 128
	}
	return 1
}

func herdrErrorInfo(raw string) (string, string) {
	v, err := jsonjs.Parse([]byte(raw))
	if err != nil {
		return "", ""
	}
	root, ok := v.(*jsonjs.Object)
	if !ok {
		return "", ""
	}
	errValue, _ := root.Get("error")
	errObject, ok := errValue.(*jsonjs.Object)
	if !ok {
		return "", ""
	}
	code, _ := errObject.Get("code")
	if code == nil || code == false {
		return "", ""
	}
	message, _ := errObject.Get("message")
	return herdrCauseString(code), herdrCauseString(message)
}

func herdrCauseString(value any) string {
	if value == false {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	case bool:
		return "true"
	default:
		return jsonjs.Stringify(v)
	}
}

func kindExe(k string) string {
	switch k {
	case "codex":
		return "codex"
	case "claude":
		return "claude"
	case "grok":
		return "grok"
	case "agy":
		return "agy"
	case "cursor":
		return "cursor-agent"
	case "pi":
		return "pi"
	case "opencode":
		return "opencode"
	case "gemini":
		return "gemini"
	}
	return k
}
func usedKinds(ctx *core.Config, env platform.Env, cwd string) []string {
	set := map[string]bool{}
	if core.LanesEnabled(ctx, env) {
		for _, lane := range core.LaneNames(ctx, env) {
			k := core.LaneAttr(ctx, lane, "kind", nil, env)
			if k != "" {
				set[k] = true
				continue
			}
			for _, r := range doctorLaneRoles(core.LaneRolesCSV(ctx, lane, env)) {
				if r != "documenter" && r != "planner" {
					v := core.ResolvedRoleKind(r, ctx, env, cwd)
					if v != "" {
						set[v] = true
					}
				}
			}
		}
	} else {
		seen := map[string]bool{}
		for _, d := range core.RoleDirs(env, cwd) {
			entries, _ := os.ReadDir(d)
			for _, entry := range entries {
				n := strings.TrimSuffix(entry.Name(), ".md")
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || seen[n] || n == "documenter" || n == "planner" {
					continue
				}
				seen[n] = true
				if v := core.ResolvedRoleKind(n, ctx, env, cwd); v != "" {
					set[v] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	textutil.SortUTF16(out)
	return out
}

func doctorLaneWarnings(ctx *core.Config, env platform.Env, cwd string, s *Say) {
	src := core.CfgSource(ctx, "panes", env)
	p := core.Cfg(ctx, "panes", "4", env)
	if v := core.Cfg(ctx, "lanes", "on", env); v != "on" && v != "off" {
		s.Warning(fmt.Sprintf("config: lanes='%s' is not on|off", v))
	}
	entry := entryScript(env)
	if p == "2" || p == "3" || p == "4" {
		if src == "defaults" || src == "builtin" {
			s.Warning(fmt.Sprintf("config: panes is not set in the project or user file (default %s). Ask the user for 2, 3 or 4 panes, then run '%s doctor --fix --panes <n>' with their answer.", p, entry))
		} else {
			s.Ok(fmt.Sprintf("config: panes=%s (%s)", p, src))
		}
	} else {
		s.Warning(fmt.Sprintf("config: panes='%s' is not 2, 3 or 4 (doctor --fix --panes 2|3|4 writes a preset)", p))
	}
	mode := core.Cfg(ctx, "pane_mode", "strict", env)
	if mode == "strict" {
		s.Ok(fmt.Sprintf("config: pane_mode=strict (never more than %s panels)", core.PanesValue(ctx, env)))
	} else if mode == "flex" {
		s.Ok(fmt.Sprintf("config: pane_mode=flex (+%d temporary panel for %s)", core.FlexExtra(ctx, env), core.Cfg(ctx, "flex_roles", "reviewer,documenter", env)))
	} else {
		s.Warning(fmt.Sprintf("config: pane_mode='%s' is not strict|flex", mode))
	}
	unknown, dups, mixed := "", "", ""
	seen := map[string]bool{}
	for _, lane := range core.LaneNames(ctx, env) {
		roles := core.LaneRolesCSV(ctx, lane, env)
		edit, review := false, false
		for _, r := range doctorLaneRoles(roles) {
			if core.RoleFile(r, env, cwd) == "" {
				unknown += " " + lane + ":" + r
			}
			if seen[r] {
				dups += " " + r
			}
			seen[r] = true
			edit = edit || core.RoleIsEdit(r, env, cwd)
			review = review || core.IsReviewRole(r)
			if r == "planner" {
				s.Warning(fmt.Sprintf("lanes: '%s' includes planner. The orchestrator is the planner and opens no pane; remove it from the lane.", lane))
			}
		}
		if edit && review {
			mixed += " " + lane
		}
		laneKind := core.Cfg(ctx, core.LaneKey(lane, "kind"), "", env)
		if laneKind != "" {
			effortKey := core.LaneKey(lane, "effort")
			if effort := core.Cfg(ctx, effortKey, "", env); effort != "" && core.CfgLayerRank(ctx, effortKey, env) < core.CfgLayerRank(ctx, core.LaneKey(lane, "kind"), env) {
				s.Ok(fmt.Sprintf("lanes: lane '%s' kind %s (%s); ignored lane effort %s from %s (another kind)", lane, laneKind, core.CfgSource(ctx, core.LaneKey(lane, "kind"), env), effort, core.CfgSource(ctx, effortKey, env)))
			}
			for _, role := range doctorLaneRoles(roles) {
				if role == "documenter" {
					continue
				}
				for _, attr := range []string{"kind", "model"} {
					key := "role_" + strings.ReplaceAll(role, "-", "_") + "_" + attr
					if core.ConfigExplicit(ctx, key, env) {
						if attr == "kind" {
							s.Warning(fmt.Sprintf("config: role.%s.kind is set and lane '%s' has kind=%s. Remove role.%s.kind (doctor --fix); the lane shares one kind.", role, lane, laneKind, role))
						} else {
							s.Warning(fmt.Sprintf("config: role.%s.model is set and lane '%s' has its own kind. Remove role.%s.model (doctor --fix).", role, lane, role))
						}
					}
				}
			}
		} else {
			values := []string{}
			first := ""
			different := false
			for _, role := range doctorLaneRoles(roles) {
				if role == "planner" || role == "documenter" {
					continue
				}
				kind := core.ResolvedRoleKind(role, ctx, env, cwd)
				values = append(values, role+"="+kind)
				if first == "" {
					first = kind
				} else if kind != first {
					different = true
				}
			}
			if different {
				s.Warning(fmt.Sprintf("lanes: lane '%s' has no lane.%s.kind and its roles disagree (%s). Orchestrator: ask the user, then run 'setup --lane %s=<kind>[:<model>[:<effort>]]'.", lane, lane, strings.Join(values, " "), lane))
			}
		}
	}
	if unknown != "" {
		s.Warning("lanes: unknown roles:" + unknown + ". Use a role from 'roles', or remove it.")
	} else {
		s.Ok("lanes: every role is known")
	}
	if dups != "" {
		s.Warning("lanes: roles in more than one lane:" + dups + ". Keep each role in one lane.")
	} else {
		s.Ok("lanes: no role is in two lanes")
	}
	if mixed != "" {
		s.Warning("lanes:" + mixed + " mix an edit role with a review role (a session must not review code it wrote). Split them the way panes=4 separates build from review.")
	} else {
		s.Ok("lanes: edit and review roles are separated")
	}
	eff := core.EffectiveLaneSignature(ctx, env)
	for _, preset := range []string{"4", "3"} {
		signature := core.LegacyPresetSignature(preset)
		if eff == signature {
			names := make([]string, 0, strings.Count(signature, "\n")+1)
			for _, line := range strings.Split(signature, "\n") {
				name, _, _ := strings.Cut(line, "=")
				names = append(names, name)
			}
			s.Warning(fmt.Sprintf("lanes: the lanes come from an old preset (%s); run '%s doctor --fix --panes %s' to move to the new ones (research joins the build lane).", strings.Join(names, ", "), entry, core.PanesValue(ctx, env)))
			break
		}
	}
	knownLanes := map[string]bool{}
	lanesForOrphans := core.LaneNames(ctx, env)
	for _, lane := range lanesForOrphans {
		knownLanes[lane] = true
	}
	orphanKeys := append([]string(nil), ctx.Order...)
	textutil.SortUTF16(orphanKeys)
	for _, key := range orphanKeys {
		if !strings.HasPrefix(key, "lane_") {
			continue
		}
		rest := strings.TrimPrefix(key, "lane_")
		attr := ""
		for _, candidate := range []string{"roles", "kind", "model", "effort", "approvals", "panes", "args"} {
			if strings.HasSuffix(rest, "_"+candidate) {
				attr = candidate
				break
			}
		}
		if attr == "" {
			continue
		}
		lane := strings.TrimSuffix(rest, "_"+attr)
		value := core.Cfg(ctx, key, "", env)
		if value == "" || knownLanes[lane] {
			continue
		}
		s.Warning(fmt.Sprintf("config: lane.%s.%s=%s (%s) sets a lane that does not exist (lanes: %s). doctor --fix removes it.", lane, attr, value, core.CfgSource(ctx, key, env), strings.Join(lanesForOrphans, " ")))
	}
	lanes := core.LaneNames(ctx, env)
	caps := []string{}
	sum := 0
	for _, l := range lanes {
		c := core.LaneCapacity(ctx, l, env)
		sum += c
		caps = append(caps, fmt.Sprintf("%s=%d", l, c))
	}
	if mode == "flex" {
		sum += core.FlexExtra(ctx, env)
	}
	mw := core.Cfg(ctx, "max_workers", "3", env)
	if core.ConfigExplicit(ctx, "max_workers", env) && mw != strconv.Itoa(sum) {
		s.Warning(fmt.Sprintf("config: max_workers=%s but the lanes hold %d workers (%s). Set max_workers=%d (doctor --fix aligns it).", mw, sum, strings.Join(caps, " "), sum))
	} else {
		s.Ok(fmt.Sprintf("config: max_workers=%s matches the lanes (%s)", core.MaxWorkers(ctx, env), strings.Join(caps, " ")))
	}
	if core.ConfigExplicit(ctx, "split_max_panes", env) {
		sp := core.Cfg(ctx, "split_max_panes", "", env)
		n, e := strconv.Atoi(sp)
		mw, _ := strconv.Atoi(core.MaxWorkers(ctx, env))
		ref := 1 + mw
		if mw == 0 {
			ref, _ = strconv.Atoi(core.PanesValue(ctx, env))
			if mode == "flex" {
				ref += core.FlexExtra(ctx, env)
			}
		}
		if e == nil && n > ref {
			s.Warning(fmt.Sprintf("config: split_max_panes=%s is greater than 1 + max_workers=%d. Set split_max_panes=%d (doctor --fix aligns it).", sp, mw, ref))
		}
		if core.LanesEnabled(ctx, env) && e == nil && n < ref {
			s.Warning(fmt.Sprintf("config: split_max_panes=%s leaves no room for the whole team (1 + max_workers=%d); the last workers will open in a herd tab. Remove split_max_panes or set %d.", sp, mw, ref))
		}
	}
	plannerKeys := make([]string, 0, len(ctx.Entries))
	for key := range ctx.Entries {
		if strings.HasPrefix(key, "role_planner_") {
			plannerKeys = append(plannerKeys, key)
		}
	}
	textutil.SortUTF16(plannerKeys)
	for _, key := range plannerKeys {
		source := core.CfgSource(ctx, key, env)
		if source == "user" || source == "project" || source == "env" || source == "session" {
			s.Warning(fmt.Sprintf("config: %s is set (%s) but the planner is the orchestrator and opens no pane. Remove it (doctor --fix).", key, source))
		}
	}
}

func doctorDiscardedModels(ctx *core.Config, env platform.Env, cwd string, s *Say) {
	if core.LanesEnabled(ctx, env) {
		for _, lane := range core.LaneNames(ctx, env) {
			kk, mk := core.LaneKey(lane, "kind"), core.LaneKey(lane, "model")
			k, m := core.Cfg(ctx, kk, "", env), core.Cfg(ctx, mk, "", env)
			if k != "" && m != "" && core.CfgLayerRank(ctx, mk, env) < core.CfgLayerRank(ctx, kk, env) {
				s.Warning(fmt.Sprintf("config: lane.%s.model=%s (%s) is ignored: lane.%s.kind=%s comes from a higher layer (%s) without a model", lane, m, core.CfgSource(ctx, mk, env), lane, k, core.CfgSource(ctx, kk, env)))
			}
		}
	}
	seen := map[string]bool{}
	for _, dir := range core.RoleDirs(env, cwd) {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			role := strings.TrimSuffix(entry.Name(), ".md")
			if seen[role] {
				continue
			}
			seen[role] = true
			if role == "planner" {
				continue
			}
			prefix := "role_" + strings.ReplaceAll(role, "-", "_") + "_"
			kk, mk := prefix+"kind", prefix+"model"
			kind, model := core.Cfg(ctx, kk, "", env), core.Cfg(ctx, mk, "", env)
			lane := ""
			if core.LanesEnabled(ctx, env) {
				lane = core.LaneOfRole(ctx, role, env)
			}
			laneKind := lane != "" && core.Cfg(ctx, core.LaneKey(lane, "kind"), "", env) != ""
			if kind != "" && model != "" && !laneKind && core.CfgLayerRank(ctx, mk, env) < core.CfgLayerRank(ctx, kk, env) {
				s.Warning(fmt.Sprintf("config: role.%s.model=%s (%s) is ignored: role.%s.kind=%s comes from a higher layer (%s) without a model", role, model, core.CfgSource(ctx, mk, env), role, kind, core.CfgSource(ctx, kk, env)))
			}
			modelWins := model != "" && core.CfgLayerRank(ctx, mk, env) >= core.CfgLayerRank(ctx, kk, env)
			if kind != "" && !laneKind && !modelWins {
				front := core.FmGet(core.RoleFile(role, env, cwd), "model")
				if front != "" {
					s.Warning(fmt.Sprintf("config: role file model '%s' of '%s' is ignored: role.%s.kind=%s comes from a higher layer (%s)", front, role, role, kind, core.CfgSource(ctx, kk, env)))
				}
			}
			if laneKind {
				front := core.FmGet(core.RoleFile(role, env, cwd), "model")
				lk := core.LaneKey(lane, "kind")
				if front != "" && core.Cfg(ctx, core.LaneKey(lane, "model"), "", env) == "" {
					s.Warning(fmt.Sprintf("config: role file model '%s' of '%s' is ignored: lane.%s.kind=%s comes from a higher layer (%s)", front, role, lane, core.Cfg(ctx, lk, "", env), core.CfgSource(ctx, lk, env)))
				}
			}
		}
	}
}

func sourceLayer(from string) string {
	if i := strings.LastIndex(from, "("); i >= 0 && strings.HasSuffix(from, ")") {
		return from[i+1 : len(from)-1]
	}
	return from
}
func unresolvedModel(kind, spec string, env platform.Env) bool {
	unresolved := false
	_, err := kinds.ResolveModel(kind, spec, "", env, func(message string) {
		if message == fmt.Sprintf("no %s model matches '%s'; passing it through unchanged", kind, spec) {
			unresolved = true
		}
	})
	return unresolved || err != nil
}

func doctorModelPairs(ctx *core.Config, env platform.Env, cwd string, s *Say) {
	type pair struct{ unit, name, kind, spec, specLayer, kindLayer string }
	pairs := []pair{}
	if core.LanesEnabled(ctx, env) {
		for _, lane := range core.LaneNames(ctx, env) {
			kind := core.LaneAttr(ctx, lane, "kind", nil, env)
			spec := core.LaneAttr(ctx, lane, "model", nil, env)
			if kind == "" && spec != "" {
				layer := core.CfgSource(ctx, core.LaneKey(lane, "model"), env)
				seen := map[string]bool{}
				for _, role := range doctorLaneRoles(core.LaneRolesCSV(ctx, lane, env)) {
					if seen[role] {
						continue
					}
					seen[role] = true
					res := core.ResolveRoleSettings(role, ctx, env, cwd, core.RoleFlags{})
					if res.Lane != lane || res.Kind == "" || res.ModelSpec != spec || !strings.HasPrefix(res.ModelFrom, "lane "+lane+" (") {
						continue
					}
					if unresolvedModel(res.Kind, spec, env) {
						s.Warning(fmt.Sprintf("config: lane '%s' model '%s' (%s) does not resolve for kind '%s' of role '%s' (%s)", lane, spec, layer, res.Kind, role, sourceLayer(res.KindFrom)))
					}
				}
				continue
			}
			if kind == "" || spec == "" {
				continue
			}
			pairs = append(pairs, pair{"lane", lane, kind, spec, core.CfgSource(ctx, core.LaneKey(lane, "model"), env), core.CfgSource(ctx, core.LaneKey(lane, "kind"), env)})
		}
	} else {
		seen := map[string]bool{}
		for _, dir := range core.RoleDirs(env, cwd) {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				role := strings.TrimSuffix(e.Name(), ".md")
				if seen[role] {
					continue
				}
				seen[role] = true
				if role == "planner" {
					continue
				}
				res := core.ResolveRoleSettings(role, ctx, env, cwd, core.RoleFlags{})
				if res.Kind == "" || res.ModelSpec == "" {
					continue
				}
				pairs = append(pairs, pair{"role", role, res.Kind, res.ModelSpec, sourceLayer(res.ModelFrom), sourceLayer(res.KindFrom)})
			}
		}
	}
	for _, p := range pairs {
		if unresolvedModel(p.kind, p.spec, env) {
			s.Warning(fmt.Sprintf("config: %s '%s' model '%s' (%s) does not resolve for kind '%s' (%s)", p.unit, p.name, p.spec, p.specLayer, p.kind, p.kindLayer))
		}
	}
}

func doctorFeedback(ctx *core.Config, env platform.Env, s *Say) {
	if core.Cfg(ctx, "feedback", "ask", env) != "local" {
		return
	}
	dir := core.Cfg(ctx, "feedback_dir", "", env)
	info, err := os.Stat(dir)
	if dir == "" || !filepath.IsAbs(dir) || err != nil || !info.IsDir() {
		s.Warning("config: feedback=local but feedback_dir is empty, relative or not a directory; set feedback_dir to the maintainer's directory, as an absolute path (feedback send dies 2 until then)")
	}
}

func doctorLaneArgs(ctx *core.Config, env platform.Env, s *Say) {
	if core.LanesEnabled(ctx, env) {
		for _, lane := range core.LaneNames(ctx, env) {
			for _, role := range doctorLaneRoles(core.LaneRolesCSV(ctx, lane, env)) {
				if core.Cfg(ctx, "role_"+strings.ReplaceAll(role, "-", "_")+"_args", "", env) != "" {
					s.Warning(fmt.Sprintf("config: role.%s.args is ignored: '%s' runs in lane '%s' (lanes=on); set lane.%s.args instead", role, role, lane, lane))
				}
			}
		}
		return
	}
	keys := []string{}
	for key := range ctx.Entries {
		if strings.HasPrefix(key, "lane_") && strings.HasSuffix(key, "_args") && core.Cfg(ctx, key, "", env) != "" {
			keys = append(keys, key)
		}
	}
	textutil.SortUTF16(keys)
	for _, key := range keys {
		lane := strings.TrimSuffix(strings.TrimPrefix(key, "lane_"), "_args")
		roles := doctorLaneRoles(core.LaneRolesCSV(ctx, lane, env))
		suffix := ""
		if len(roles) > 0 {
			suffix = " (" + strings.Join(roles, ", ") + ")"
		}
		s.Warning(fmt.Sprintf("config: lane.%s.args is ignored (lanes=off); set role.<role>.args for its roles instead%s", lane, suffix))
	}
}

func doctorCodexNetwork(ctx *core.Config, env platform.Env, cwd string, s *Say) {
	for _, role := range []string{"designer", "inspector"} {
		res := core.ResolveRoleSettings(role, ctx, env, cwd, core.RoleFlags{})
		if res.Kind != "codex" {
			continue
		}
		args := spawn.ConfigNativeArgs("codex", res.Lane, role, ctx, env, cwd, nil)
		if len(dispatch.SandboxNotes("codex", args)) > 1 {
			key := "role." + role + ".args"
			if res.Lane != "" {
				key = "lane." + res.Lane + ".args"
			}
			s.Warning(fmt.Sprintf("config: %s runs on codex without network: it cannot open a local port, so the UI and e2e tests do not run (listen EPERM); set %s=-c sandbox_workspace_write.network_access=true, or run the e2e yourself", role, key))
		}
	}
}

func checkSetup(ctx *core.Config, env platform.Env, cwd string, s *Say) {
	root := platform.ProjectRoot(env, cwd)
	target := ""
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		p := filepath.Join(root, name)
		if isFile(p) {
			raw, _ := platform.ReadTextFile(p)
			if strings.Contains(raw, setuptext.SetupStart) || strings.Contains(raw, setuptext.LegacySetupStart) {
				target = p
				break
			}
		}
	}
	tn, tl := false, false
	if target != "" {
		raw, _ := platform.ReadTextFile(target)
		tn = strings.Contains(raw, setuptext.SetupStart)
		tl = strings.Contains(raw, setuptext.LegacySetupStart)
	}
	local := filepath.Join(root, "CLAUDE.local.md")
	localRaw, _ := platform.ReadTextFile(local)
	ln, ll := strings.Contains(localRaw, setuptext.SetupStart), strings.Contains(localRaw, setuptext.LegacySetupStart)
	setupLocal := core.Cfg(ctx, "setup_target", "canonical", env) == "local"
	entry := entryScript(env)
	if tn {
		s.Ok("instruction block present in " + filepath.Base(target))
	} else if ln {
		s.Ok("instruction block present in CLAUDE.local.md")
	} else if tl || ll {
		if ll || setupLocal {
			s.Warning(fmt.Sprintf("legacy herdr-agents instruction block in CLAUDE.local.md: run '%s setup --local' to rename it in place (its text is kept)", entry))
		} else {
			s.Warning(fmt.Sprintf("legacy herdr-agents instruction block in %s: run '%s setup' to rename it in place (its text is kept)", filepath.Base(target), entry))
		}
	} else if setupLocal {
		s.Warning(fmt.Sprintf("no herdr-soho block in CLAUDE.local.md: run '%s setup --local' (writes the delegation rules between <!-- herdr-soho:start/end --> markers, kept unversioned)", entry))
	} else {
		s.Warning(fmt.Sprintf("no herdr-soho block in AGENTS.md/CLAUDE.md: run '%s setup' (writes the delegation rules between <!-- herdr-soho:start/end --> markers)", entry))
	}
	// A separate CLAUDE.md (not a symlink, not the file setup targets) that
	// still carries only the pre-rename block: Claude Code keeps reading it.
	claudeMd := filepath.Join(root, "CLAUDE.md")
	if info, err := os.Lstat(claudeMd); err == nil && info.Mode().IsRegular() && claudeMd != target {
		claudeRaw, _ := platform.ReadTextFile(claudeMd)
		if strings.Contains(claudeRaw, setuptext.LegacySetupStart) && !strings.Contains(claudeRaw, setuptext.SetupStart) {
			s.Warning(fmt.Sprintf("legacy herdr-agents instruction block in CLAUDE.md: run '%s setup --target CLAUDE.md' to rename it in place (its text is kept)", entry))
		}
	}
	settings := filepath.Join(root, ".claude", "settings.json")
	hook := settingsDoctorHook(settings)
	if hook == "new" {
		s.Ok("Claude hooks present in .claude/settings.json")
	} else if hook == "legacy" {
		if ll || setupLocal {
			s.Warning(fmt.Sprintf("legacy herdr-agents hooks in .claude/settings.json: run '%s setup --local' to replace them", entry))
		} else {
			s.Warning(fmt.Sprintf("legacy herdr-agents hooks in .claude/settings.json: run '%s setup' to replace them", entry))
		}
	} else if ln || setupLocal {
		s.Warning(fmt.Sprintf("no herdr-soho hooks in .claude/settings.json: run '%s setup --local' (UserPromptSubmit reminder + SessionStart doctor)", entry))
	} else {
		s.Warning(fmt.Sprintf("no herdr-soho hooks in .claude/settings.json: run '%s setup' (UserPromptSubmit reminder + SessionStart doctor)", entry))
	}
}
func settingsDoctorHook(file string) string {
	raw, e := platform.ReadTextFile(file)
	if e != nil {
		return ""
	}
	v, e := jsonjs.Parse([]byte(raw))
	if e != nil {
		return ""
	}
	o, ok := v.(*jsonjs.Object)
	if !ok {
		return ""
	}
	hooks, _ := o.Get("hooks")
	ho, ok := hooks.(*jsonjs.Object)
	if !ok {
		return ""
	}
	ev, _ := ho.Get("SessionStart")
	arr, ok := ev.([]any)
	if !ok {
		return ""
	}
	legacy := false
	for _, row := range arr {
		ro, ok := row.(*jsonjs.Object)
		if !ok {
			continue
		}
		hv, _ := ro.Get("hooks")
		ha, ok := hv.([]any)
		if !ok {
			continue
		}
		for _, h := range ha {
			obj, ok := h.(*jsonjs.Object)
			if !ok {
				continue
			}
			c, _ := obj.Get("command")
			if c == setuptext.SetupHookDoctor() {
				return "new"
			}
			if c == setuptext.LegacyHookDoctor() {
				legacy = true
			}
		}
	}
	if legacy {
		return "legacy"
	}
	return ""
}
