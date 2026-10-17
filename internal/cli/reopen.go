package cli

import (
	"fmt"
	"strings"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/spawn"
)

// cmdReopen closes the worker like `release <name> --close` (the same
// refusals apply, and --force is passed through) and then reopens it with a
// `spawn <role> --fresh` that carries the role, kind, model, effort, cwd and
// native args recorded in the roster row. Both halves run as function calls;
// the release half's stdout is discarded, so the command's output is the
// spawn JSON.
func cmdReopen(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	if len(argv) == 0 {
		core.DieFriction("reopen: missing agent name", 2, frictionLogPath, "reopen")
	}
	agent := argv[0]
	force := false
	for _, arg := range argv[1:] {
		switch arg {
		case "--force":
			force = true
		default:
			core.DieFriction("reopen: unknown option "+arg, 2, frictionLogPath, "reopen")
		}
	}
	sd := core.StateDir(ctx, env, cwd)
	line := core.RosterLine(sd, agent)
	if line == "" {
		core.DieFriction(fmt.Sprintf("agent '%s' is not in the roster", agent), 3, frictionLogPath, "reopen")
	}
	// DJA-392: the spawn half of a reopen always opens a new pane (--fresh,
	// never reuse), so the pressure refusal runs here, before the release
	// half changes any state (roster line, pane, copies): a refused reopen
	// leaves the worker exactly as it was, and the spawn's warning line of
	// today still goes out. --force is the skip and is forwarded to the
	// spawn half, which would otherwise refuse there.
	if !force {
		if parts := core.PressureRefuseParts(ctx, env, cwd); len(parts) > 0 {
			core.PressureWarn(ctx, env, cwd)
			platform.Die(fmt.Sprintf("spawn: refusing to open a new pane under resource pressure: %s; close idle panes or run 'soho gc', or pass --force", strings.Join(parts, "; ")), 25)
		}
	}
	fields := strings.Split(line, "\t")
	field := func(index int) string {
		if index < len(fields) {
			return fields[index]
		}
		return ""
	}
	role, wkind, model, wdir, native := field(3), field(2), field(8), field(6), field(13)
	effort := field(14)

	releaseArgs := []string{agent, "--close"}
	if force {
		releaseArgs = append(releaseArgs, "--force")
	}
	oldOut := platform.Stdout
	platform.Stdout = releaseDiscardWriter{}
	defer func() { platform.Stdout = oldOut }()
	code := cmdRelease(releaseArgs, ctx, env, cwd)
	platform.Stdout = oldOut
	if code != 0 {
		return code
	}

	spawnArgs := []string{role, "--fresh", "--kind", wkind, "--name", agent, "--cwd", wdir}
	if force {
		spawnArgs = append(spawnArgs, "--force")
	}
	if model != "" {
		spawnArgs = append(spawnArgs, "--model", model)
	}
	if effort != "" {
		spawnArgs = append(spawnArgs, "--effort", effort)
	}
	if native != "" {
		spawnArgs = append(spawnArgs, "--")
		spawnArgs = append(spawnArgs, strings.Fields(native)...)
	}
	spawn.CmdSpawn(spawnArgs, ctx, env, cwd)
	return 0
}
