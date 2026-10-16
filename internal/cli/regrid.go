package cli

import (
	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/layout"
	"github.com/djalmajr/soho/internal/platform"
)

func cmdRegrid(args []string, ctx *core.Config, env platform.Env, cwd string) int {
	return layout.CmdRegrid(args, ctx, env, cwd)
}
