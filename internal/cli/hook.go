package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/doctor"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

const hookReminder = "herdr-soho: this project routes non-trivial work through /herdr-soho — surveys go to a scouter, slices to workers; the orchestrator keeps only one-or-two-file changes."

// cmdHook is native, advisory hook handling. A hook outside Herdr is silent;
// doctor warnings never block the host agent's session startup.
func cmdHook(args []string, env platform.Env, cwd string) int {
	if len(args) != 1 || (args[0] != "reminder" && args[0] != "doctor") {
		platform.Die("usage: hook reminder|doctor", 2)
	}
	if env.Get("HERDR_ENV") != "1" {
		return 0
	}
	if args[0] == "reminder" {
		fmt.Fprintln(platform.Stdout, hookReminder)
		return 0
	}
	if project := env.Get("CLAUDE_PROJECT_DIR"); project != "" {
		cwd = project
	}
	readEnv := env.Clone()
	readEnv["HERDR_SOHO_NOWRITE"] = "1"
	out := platform.Stdout
	var captured bytes.Buffer
	platform.Stdout = &captured
	defer func() { platform.Stdout = out }()
	func() {
		defer func() {
			if failure := recover(); failure != nil {
				if e, ok := failure.(*platform.ExitError); ok {
					fmt.Fprintf(&captured, "warn   %s\n", e.Msg)
				} else {
					panic(failure)
				}
			}
		}()
		ctx := core.LoadConfig(readEnv, cwd)
		doctor.CmdDoctor(nil, &ctx, readEnv, cwd)
	}()
	for _, line := range strings.Split(captured.String(), "\n") {
		if rest, ok := strings.CutPrefix(line, "warn"); ok && strings.HasPrefix(rest, " ") {
			fmt.Fprintf(out, "herdr-soho doctor: %s\n", strings.TrimLeft(rest, " \t"))
		}
	}
	return 0
}
