package metrics

import "github.com/djalmajr/herdr-soho/internal/core"

// CmdMark labels a settled report (issue #39, part 2).
func CmdMark(args []string, c CommandContext) int {
	core.DieFriction("metrics mark: not implemented yet", 2, c.FrictionLog, "metrics")
	return 2
}
