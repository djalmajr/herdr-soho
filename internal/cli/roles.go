package cli

import (
	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func cmdRoles(env platform.Env, cwd string)               { core.CmdRoles(env, cwd) }
func cmdRole(args []string, env platform.Env, cwd string) { core.CmdRole(args, env, cwd) }
