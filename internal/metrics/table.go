package metrics

import "github.com/djalmajr/herdr-soho/internal/core"

// CmdTable turns exported summaries into the generated table of
// references/agent-profiles.md (issue #39, part 4).
func CmdTable(args []string, c CommandContext) int {
	core.DieFriction("metrics table: not implemented yet", 2, c.FrictionLog, "metrics")
	return 2
}
