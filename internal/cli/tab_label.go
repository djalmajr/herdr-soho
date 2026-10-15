package cli

import (
	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/layout"
	"github.com/djalmajr/soho/internal/platform"
)

func cmdTabLabel(args []string, ctx *core.Config, env platform.Env, cwd string) int {
	return layout.CmdTabLabel(args, ctx, env, cwd)
}
