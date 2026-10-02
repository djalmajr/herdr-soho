package spawn

import (
	"fmt"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// ConfigNativeArgs returns configured args for the CLI the worker will run.
func ConfigNativeArgs(kind, lane, role string, ctx *core.Config, env platform.Env, cwd string, onDrop func(key, own string)) string {
	scopedKey := "role." + role + ".args"
	scoped := core.Cfg(ctx, "role_"+strings.ReplaceAll(role, "-", "_")+"_args", "", env)
	if lane != "" {
		scopedKey = "lane." + lane + ".args"
		scoped = core.Cfg(ctx, core.LaneKey(lane, "args"), "", env)
	}
	if scoped != "" {
		own := core.ResolveRoleSettings(role, ctx, env, cwd, core.RoleFlags{}).Kind
		if own != kind {
			if onDrop != nil {
				onDrop(scopedKey, own)
			}
			scoped = ""
		}
	}
	var tokens []string
	for _, value := range []string{core.Cfg(ctx, "args_"+kind, "", env), scoped} {
		if value != "" {
			tokens = append(tokens, strings.Fields(value)...)
		}
	}
	return strings.Join(tokens, " ")
}

// RosterCaller reports the row of this project's roster that holds pane, when
// one exists: the row's agent name and role. A pane row means the pane is a
// worker (or sub-orchestrator) this project opened for another orchestrator.
func RosterCaller(ctx *core.Config, env platform.Env, cwd, pane string) (name, role string, ok bool) {
	if pane == "" {
		return "", "", false
	}
	for _, line := range core.RosterRows(core.StateDirPath(ctx, env, cwd)) {
		f := strings.Split(line, "\t")
		if len(f) > 3 && f[0] != "" && f[1] == pane {
			name, role = f[0], f[3]
		}
	}
	return name, role, name != ""
}

// EnsureOrchestratorName names the caller's agent when the current pane hosts
// one. A caller pane that already holds a roster row of this project is a
// worker another orchestrator opened: its agent keeps its name, one line is
// written to stderr naming it (command is the calling command, "init" or
// "spawn"), and the agent's current name is returned.
func EnsureOrchestratorName(ctx *core.Config, env platform.Env, cwd, command string) string {
	pane := env.Get("HERDR_PANE_ID")
	if pane == "" {
		return ""
	}
	want := core.Cfg(ctx, "orchestrator_name", "orchestrator", env)
	r := platform.RunCli("herdr", []string{"agent", "get", pane}, platform.RunOptions{Env: env, TimeoutMs: int(herdr.Timeout.Milliseconds())})
	if r.NotFound || r.Status == nil || *r.Status != 0 {
		return ""
	}
	cur := herdr.CallerAgentName(env)
	if cur == want || strings.HasPrefix(cur, want+"-") {
		return cur
	}
	if name, role, inRoster := RosterCaller(ctx, env, cwd, pane); inRoster {
		who := "a " + role + " opened by another orchestrator"
		if role == "" {
			who = "opened by another orchestrator"
		}
		_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: %s: this pane is '%s' in the roster (%s); keeping its name\n", command, name, who)
		return cur
	}
	name := UniqueName(want, env)
	r = platform.RunCli("herdr", []string{"agent", "rename", pane, name}, platform.RunOptions{Env: env, TimeoutMs: int(herdr.Timeout.Milliseconds())})
	if r.NotFound || r.Status == nil || *r.Status != 0 {
		Warn("could not rename the caller agent to '"+name+"'", ctx, env, "spawn")
		return ""
	}
	return name
}
