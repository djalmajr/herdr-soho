package metrics

import "github.com/djalmajr/herdr-soho/internal/core"

// CmdExport writes the anonymized summary of the recorded lines (issue #39,
// part 3).
func CmdExport(args []string, c CommandContext) int {
	core.DieFriction("metrics export: not implemented yet", 2, c.FrictionLog, "metrics")
	return 2
}
