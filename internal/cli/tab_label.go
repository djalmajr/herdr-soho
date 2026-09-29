package cli

import (
	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/layout"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func cmdTabLabel(args []string, ctx *core.Config, env platform.Env, cwd string) int {
	return layout.CmdTabLabel(args, ctx, env, cwd)
}
