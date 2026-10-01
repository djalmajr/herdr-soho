package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/layout"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func cmdRelease(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	if len(argv) == 0 {
		platform.Die("agent: Parameter not set", 1)
	}
	agent := argv[0]
	closePane, force := false, false
	for _, arg := range argv[1:] {
		switch arg {
		case "--close":
			closePane = true
		case "--force":
			force = true
		default:
			core.DieFriction("release: unknown option "+arg, 2, frictionLogPath, "release")
		}
	}

	sd := core.StateDir(ctx, env, cwd)
	line := core.RosterLine(sd, agent)
	if line == "" {
		if closePane && releaseOrphan(agent, force, ctx, env, cwd) {
			return 0
		}
		core.DieFriction(fmt.Sprintf("agent '%s' is not in the roster (state dir: %s)", agent, sd), 3, frictionLogPath, "release")
	}
	fields := strings.Split(line, "\t")
	field := func(index int) string {
		if index >= len(fields) {
			return ""
		}
		return fields[index]
	}
	pane, created, wdir := field(1), field(5), field(6)
	burst := field(12) == "burst"
	closes := closePane || burst && created == "1"
	report := core.LastReport(sd, agent)
	if !force && reportEmpty(report) {
		state := herdr.AgentState(agent, env, herdr.Timeout, nil)
		if state.State == "unavailable" {
			core.DieFriction(fmt.Sprintf("agent '%s': herdr agent get failed (%s). Refusing to release; the worker may still be live. Retry when herdr answers, or pass --force.", agent, state.Cause), 4, frictionLogPath, "release")
		}
		if closes && report != "" && state.State == "working" {
			core.DieFriction(fmt.Sprintf("agent '%s' is still working and has not written %s; closing now discards its work. Run 'wait %s' first, or release --close --force", agent, report, agent), 3, frictionLogPath, "release")
		}
	}

	if closes {
		if created == "1" {
			if herdr.PaneClose(pane, env) {
				suffix := ""
				if burst {
					suffix = " (temporary)"
				}
				_, _ = fmt.Fprintf(platform.Stdout, "closed pane %s%s\n", pane, suffix)
			}
		} else {
			core.Warn(fmt.Sprintf("pane %s was not created by this skill; not closing it", pane), frictionLogPath, "release")
		}
	} else {
		core.PaneTaskTitle(sd, agent, nil, env)
	}

	core.RosterRemove(sd, agent)
	_ = os.Remove(core.LastReportPath(sd, agent))
	_ = os.Remove(filepath.Join(sd, "task-"+agent))
	waitDir := filepath.Join(sd, "wait")
	if entries, err := os.ReadDir(waitDir); err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), agent+".") {
				_ = os.Remove(filepath.Join(waitDir, entry.Name()))
			}
		}
	}

	if closes && core.Cfg(ctx, "regrid", "on", env) == "on" {
		releaseAutoRegrid(ctx, env, cwd)
	} else {
		func() {
			defer func() {
				if recover() != nil {
					core.Warn("relabel of the herd tabs failed (see friction)", frictionLogPath, "release")
				}
			}()
			layout.HerdTabsRelabel(ctx, env, cwd)
		}()
	}
	if !closes && pane != "" {
		_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: release: the pane %s stays open; close it later with: herdr-soho release %s --close\n", pane, agent)
	}

	worktrees := exec.Command("git", "-C", wdir, "worktree", "list")
	worktrees.Env = env.List()
	if output, err := worktrees.Output(); err == nil {
		leftovers := []string{}
		for _, row := range strings.Split(string(output), "\n") {
			if strings.Contains(row, "/.worktrees/") {
				leftovers = append(leftovers, row)
			}
		}
		if len(leftovers) > 0 {
			_, _ = fmt.Fprintln(platform.Stdout, "leftover worktrees (not removed):")
			for _, row := range leftovers {
				_, _ = fmt.Fprintln(platform.Stdout, row)
			}
		}
	}
	_, _ = fmt.Fprintf(platform.Stdout, "released %s\n", agent)
	return 0
}

// releaseOrphan closes the pane of an agent that has already left the roster
// (a release without --close leaves it alive and idle). Only a lane-worker
// name live in the current workspace counts as an orphan; the orchestrator's
// own pane and other names fall through to the plain not-in-roster error.
// Only a confirmed-idle orphan is closed; a working, blocked or unqueryable
// one is refused with 10 unless --force is passed.
func releaseOrphan(name string, force bool, ctx *core.Config, env platform.Env, cwd string) bool {
	orphan, ok := core.FindOrphan(name, ctx, env, cwd)
	if !ok {
		return false
	}
	state := ""
	if !force {
		state = herdr.AgentState(name, env, herdr.Timeout, nil).State
	}
	if !force && state != "idle" && state != "done" {
		core.DieFriction(fmt.Sprintf("agent '%s' is not in the roster and is %s; pass --force to close its pane", name, state), 10, frictionLogPath, "release")
	}
	if herdr.PaneClose(orphan.Pane, env) {
		_, _ = fmt.Fprintf(platform.Stdout, "released %s (pane %s closed; it was no longer in the roster)\n", name, orphan.Pane)
	} else {
		// A failed close keeps the roster release's contract: the release
		// stands, the pane stays open, and no "closed" claim is printed.
		_, _ = fmt.Fprintf(platform.Stdout, "released %s\n", name)
	}
	return true
}

func releaseAutoRegrid(ctx *core.Config, env platform.Env, cwd string) {
	message := "regrid after release failed; panes left as they are (see friction)"
	oldOut, oldErr := platform.Stdout, platform.Stderr
	failed := (*platform.ExitError)(nil)
	func() {
		defer func() {
			platform.Stdout, platform.Stderr = oldOut, oldErr
			if value := recover(); value != nil {
				exitErr, ok := value.(*platform.ExitError)
				if !ok {
					panic(value)
				}
				failed = exitErr
			}
		}()
		platform.Stdout, platform.Stderr = releaseDiscardWriter{}, releaseDiscardWriter{}
		layout.CmdRegrid(nil, ctx, env, cwd)
	}()
	if failed != nil {
		core.RecordFrictionError(failed.Msg, failed.Code, frictionLogPath, "release")
		core.Warn(message, frictionLogPath, "release")
	}
}

type releaseDiscardWriter struct{}

func (releaseDiscardWriter) Write(value []byte) (int, error) { return len(value), nil }

func reportEmpty(report string) bool {
	if report == "" {
		return true
	}
	info, err := os.Stat(report)
	return err != nil || info.Size() == 0
}
