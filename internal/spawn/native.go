package spawn

import (
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

// EnsureOrchestratorName names the caller's agent when the current pane hosts one.
func EnsureOrchestratorName(ctx *core.Config, env platform.Env) string {
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
	name := UniqueName(want, env)
	r = platform.RunCli("herdr", []string{"agent", "rename", pane, name}, platform.RunOptions{Env: env, TimeoutMs: int(herdr.Timeout.Milliseconds())})
	if r.NotFound || r.Status == nil || *r.Status != 0 {
		Warn("could not rename the caller agent to '"+name+"'", ctx, env, "spawn")
		return ""
	}
	return name
}
