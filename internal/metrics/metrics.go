// Package metrics implements the metrics subcommands of issue #39 over the
// lines that the wait records in <state>/metrics.jsonl when metrics=on:
// `metrics mark` labels a settled report after the orchestrator checked it,
// `metrics export` writes an anonymized summary of the recorded lines, and
// `metrics table` turns exported summaries into the generated table of
// references/agent-profiles.md.
package metrics

import (
	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// CommandContext carries what every metrics subcommand needs.
type CommandContext struct {
	Config      *core.Config
	Env         platform.Env
	Cwd         string
	FrictionLog string
}

const usageLine = "metrics: usage: herdr-soho metrics mark <report> [--finding <n>=real|false]... [--missed P0|P1|P2|P3]... [--amendment implementer|brief] | metrics export [--since <date>] [--project-label <label>] | metrics table [--write <file>] <export.jsonl>..."

// CmdMetrics routes `metrics mark` and `metrics export`.
func CmdMetrics(args []string, c CommandContext) int {
	if len(args) == 0 {
		core.DieFriction(usageLine, 2, c.FrictionLog, "metrics")
	}
	switch args[0] {
	case "mark":
		return CmdMark(args[1:], c)
	case "export":
		return CmdExport(args[1:], c)
	case "table":
		return CmdTable(args[1:], c)
	}
	core.DieFriction("metrics: unknown subcommand '"+args[0]+"'; "+usageLine, 2, c.FrictionLog, "metrics")
	return 2
}
