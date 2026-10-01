package core

import (
	"os"
	"sort"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/platform"
	textutil "github.com/djalmajr/herdr-soho/internal/text"
)

// Orphan is a live Herdr agent of the current workspace that this project no
// longer owns: it is not a roster pane, it is not the orchestrator's own
// pane, and it carries this project's worker name. A release without --close
// leaves such an agent alive and idle outside the roster.
type Orphan struct {
	Name  string
	Pane  string
	State string
}

// WorkerNameMatches reports whether name is base alone or base followed by
// "-<n>", the shape spawn.UniqueName gives a lane worker's name (review,
// review-2, review-3...).
func WorkerNameMatches(name, base string) bool {
	if base == "" || name == "" {
		return false
	}
	if name == base {
		return true
	}
	if !strings.HasPrefix(name, base) {
		return false
	}
	suffix := name[len(base):]
	if !strings.HasPrefix(suffix, "-") {
		return false
	}
	suffix = suffix[1:]
	if suffix == "" {
		return false
	}
	for i := 0; i < len(suffix); i++ {
		if suffix[i] < '0' || suffix[i] > '9' {
			return false
		}
	}
	return true
}

// WorkerNameBases lists the base names that identify this project's lane
// workers: the configured lanes with lanes=on, the known role names (the
// role files) with lanes=off.
func WorkerNameBases(ctx *Config, env platform.Env, cwd string) []string {
	if LanesEnabled(ctx, env) {
		return LaneNames(ctx, env)
	}
	names := map[string]bool{}
	for _, dir := range RoleDirs(env, cwd) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			names[strings.TrimSuffix(entry.Name(), ".md")] = true
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	textutil.SortUTF16(out)
	return out
}

// WorkerNameOfProject reports whether name is one of this project's worker
// names: a configured lane (lanes=on) or a known role (lanes=off), alone or
// followed by "-<n>".
func WorkerNameOfProject(name string, ctx *Config, env platform.Env, cwd string) bool {
	return workerNameOfBases(name, WorkerNameBases(ctx, env, cwd))
}

func workerNameOfBases(name string, bases []string) bool {
	for _, base := range bases {
		if WorkerNameMatches(name, base) {
			return true
		}
	}
	return false
}

// OrphansOf classifies a herdr agent list into the orphans of the current
// workspace, using the live list the way the roster reads it for the "other
// live agents" section: not a roster pane, not the orchestrator's own pane
// (HERDR_PANE_ID), a pane of this workspace (herdr pane list --workspace),
// and named like one of this project's workers. A failed pane list reads as
// no workspace pane, so nothing is classified (no false orphan).
func OrphansOf(ctx *Config, env platform.Env, cwd string, live []any) []Orphan {
	rosterPanes := map[string]bool{}
	for _, line := range RosterRows(StateDirPath(ctx, env, cwd)) {
		fields := strings.Split(line, "\t")
		if len(fields) > 1 && fields[1] != "" {
			rosterPanes[fields[1]] = true
		}
	}
	workspacePanes := map[string]bool{}
	for _, pane := range herdr.PaneList(env, WorkspaceID(ctx, env, cwd)) {
		if id := agentField(pane, "pane_id"); id != "" {
			workspacePanes[id] = true
		}
	}
	orchestrator := env.Get("HERDR_PANE_ID")
	bases := WorkerNameBases(ctx, env, cwd)
	out := []Orphan{}
	for _, agent := range live {
		name := agentField(agent, "name")
		pane := agentField(agent, "pane_id")
		if name == "" || pane == "" {
			continue
		}
		if rosterPanes[pane] || pane == orchestrator || !workspacePanes[pane] {
			continue
		}
		if !workerNameOfBases(name, bases) {
			continue
		}
		out = append(out, Orphan{Name: name, Pane: pane, State: agentField(agent, "agent_status")})
	}
	sort.SliceStable(out, func(i, j int) bool { return textutil.CompareUTF16(out[i].Name, out[j].Name) < 0 })
	return out
}

// Orphans is OrphansOf over herdr.LiveAgents: a failed agent list dies with
// herdr.LiveAgents' own error, the same way the roster's "other live agents"
// section fails.
func Orphans(ctx *Config, env platform.Env, cwd string) []Orphan {
	return OrphansOf(ctx, env, cwd, herdr.LiveAgents(env, herdr.Timeout))
}

// FindOrphan reports the orphan named exactly name, when it has one.
func FindOrphan(name string, ctx *Config, env platform.Env, cwd string) (Orphan, bool) {
	for _, orphan := range Orphans(ctx, env, cwd) {
		if orphan.Name == name {
			return orphan, true
		}
	}
	return Orphan{}, false
}
