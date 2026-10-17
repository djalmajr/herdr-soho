package cli

import (
	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
)

// cmdRoles prints the roles table; exactly --json selects the plugin's JSON
// read. Any other argument keeps today's behavior: the text table, ignored
// arguments.
func cmdRoles(args []string, env platform.Env, cwd string) {
	if len(args) == 1 && args[0] == "--json" {
		core.CmdRolesJSON(env, cwd)
		return
	}
	core.CmdRoles(env, cwd)
}
func cmdRole(args []string, env platform.Env, cwd string) { core.CmdRole(args, env, cwd) }
