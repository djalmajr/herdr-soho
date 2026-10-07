package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

const procsUsage = "usage: herdr-soho procs | procs add <pid> [--agent <name>]"

// cmdProcs lists the process registry, or registers a worker process. The
// command line of a process is never read or stored: it may carry
// credentials.
func cmdProcs(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	if len(argv) == 0 {
		return procsList(ctx, env, cwd)
	}
	if argv[0] != "add" {
		return procsUsageError()
	}
	return procsAdd(argv[1:], ctx, env, cwd)
}

// procsList is read-only: <pid>  <name>  <owner>  <age>  <state>, the
// state re-checked against the system at read time (running = the pid
// exists with the same start time; gone = the pid is not there; reused =
// the pid exists with another start time).
func procsList(ctx *core.Config, env platform.Env, cwd string) int {
	sd := core.StateDir(ctx, env, cwd)
	rows, err := core.ReadProcs(sd)
	if err != nil {
		core.DieFriction(fmt.Sprintf("procs: cannot read the process registry (%v)", err), 4, frictionLogPath, "procs")
	}
	if len(rows) == 0 {
		// An empty registry says so, apart from a silent failure (G3).
		_, _ = fmt.Fprintln(platform.Stdout, "no registered processes")
		return 0
	}
	now := platform.Now()
	for _, row := range rows {
		_, _ = fmt.Fprintf(platform.Stdout, "%d  %s  %s  %s  %s\n", row.Pid, row.Name, row.Owner, humanAge(procAge(row, now)), core.ProcRowState(row, env))
	}
	return 0
}

// procsAdd registers one pid. The owner is the roster agent of
// HERDR_PANE_ID (or "-"); --agent takes the name, and a name outside the
// roster exits 3. Refusals exit 2 and write nothing: a pid that is not a
// positive integer, pid 1, this process, the invoking process, or a
// proven-gone pid (the absence is kernel-confirmed). A pid whose identity
// the single snapshot read could not resolve (the ps query failed, or its
// line did not parse, and the kernel did not report the absence) is not
// refused as a dead process: it exits 4 with a clear diagnostic and is
// not registered — a failed read is not an absence, and no second read
// races the first to name its failure.
func procsAdd(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	var pidArg, agent string
	agentSeen := false
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--agent" {
			if agentSeen || i+1 >= len(argv) || argv[i+1] == "" {
				return procsUsageError()
			}
			agent = argv[i+1]
			agentSeen = true
			i++
			continue
		}
		if strings.HasPrefix(a, "--") {
			return procsUsageError()
		}
		if pidArg == "" {
			pidArg = a
			continue
		}
		return procsUsageError()
	}
	if pidArg == "" {
		return procsUsageError()
	}
	refuse := func(cause string) int {
		_, _ = fmt.Fprintf(platform.Stderr, "procs add: refusing %s: %s\n", pidArg, cause)
		return 2
	}
	pid, err := strconv.Atoi(pidArg)
	if err != nil || pid <= 0 {
		return refuse("not a positive integer pid")
	}
	if pid == 1 {
		return refuse("pid 1 is the system init process")
	}
	if pid == os.Getpid() {
		return refuse("that is this herdr-soho process")
	}
	if parent := platform.ParentPID(); parent > 0 && pid == parent {
		return refuse("that is the process that invoked herdr-soho")
	}
	sd := core.StateDir(ctx, env, cwd)
	if agentSeen && core.RosterLine(sd, agent) == "" {
		core.DieFriction(fmt.Sprintf("procs add: agent '%s' is not in the roster (state dir: %s)", agent, sd), 3, frictionLogPath, "procs")
	}
	started, name, live := platform.ReadProcFull(pid, env)
	switch live {
	case platform.ProcGone:
		return refuse("no process with that pid is running")
	case platform.ProcUnknown:
		// The one snapshot did not resolve the pid: the ps query failed
		// (or its line did not parse) and the kernel did not report the
		// absence. It is a friction, not the dead-process refusal.
		core.DieFriction(fmt.Sprintf("procs add: the identity of pid %s is unreadable (the system state read failed and the kernel did not report it gone); not registered", pidArg), 4, frictionLogPath, "procs")
	}
	pane := env.Get("HERDR_PANE_ID")
	owner := "-"
	if agentSeen {
		owner = agent
		if fields := strings.Split(core.RosterLine(sd, agent), "\t"); len(fields) > 1 && fields[1] != "" {
			pane = fields[1]
		}
	} else if pane != "" {
		owner = rosterAgentForPane(sd, pane)
		if owner == "" {
			owner = "-"
		}
	}
	if pane == "" {
		pane = "-"
	}
	row := core.ProcRow{Pid: pid, Started: started, Name: name, Owner: owner, Pane: pane, Created: core.FrictionISO(platform.Now())}
	if err := core.UpsertProc(sd, row); err != nil {
		var locked *core.RegistryLockedError
		if errors.As(err, &locked) {
			core.DieFriction(err.Error(), 4, frictionLogPath, "procs")
		}
		core.DieFriction(fmt.Sprintf("procs add: could not register pid %s (%v)", pidArg, err), 4, frictionLogPath, "procs")
	}
	return 0
}

func procsUsageError() int {
	_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: %s\n", procsUsage)
	return 2
}

// procAge is how long the line has been registered; an unparseable or
// future created reads as 0 (same rule as the copies age).
func procAge(row core.ProcRow, now time.Time) time.Duration {
	created, err := time.Parse(copiesTimeLayout, row.Created)
	if err != nil {
		return 0
	}
	age := now.Sub(created)
	if age < 0 {
		return 0
	}
	return age
}
