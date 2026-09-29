package cli

import (
	"fmt"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/kinds"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func cmdModels(argv []string, env platform.Env) int {
	if len(argv) == 0 || argv[0] == "" {
		_, _ = fmt.Fprintln(platform.Stderr, "herdr-soho.mjs: 1: kind")
		return 1
	}
	ids := kinds.ModelIDs(argv[0], env)
	sorted := kinds.VersionSortDesc(ids)
	if len(sorted) == 0 {
		_, _ = fmt.Fprintln(platform.Stdout)
		return 0
	}
	for _, id := range sorted {
		_, _ = fmt.Fprintln(platform.Stdout, id)
	}
	return 0
}

func cmdModel(argv []string, env platform.Env) int {
	kind, spec, effort := "", "", ""
	if len(argv) > 0 {
		kind = argv[0]
	}
	if len(argv) > 1 {
		spec = argv[1]
	}
	if len(argv) > 2 {
		effort = argv[2]
	}
	if kind == "" {
		_, _ = fmt.Fprintln(platform.Stderr, "herdr-soho.mjs: 1: kind")
		return 1
	}
	if spec == "" {
		_, _ = fmt.Fprintln(platform.Stderr, "herdr-soho.mjs: 2: spec")
		return 1
	}
	model, err := kinds.ResolveModel(kind, spec, effort, env, func(string) {})
	if err != nil {
		if exit, ok := err.(*platform.ExitError); ok {
			if exit.Friction {
				platform.DieFriction(exit.Msg, exit.Code)
			}
			platform.Die(exit.Msg, exit.Code)
		}
		panic(err)
	}
	args := append(kinds.KindModelArgs(kind, model, effort, func(string) {}), kinds.KindEffortArgs(kind, effort, model, env, func(string) {})...)
	ceiling := kinds.KindEffortCeiling(kind)
	if kind == "codex" {
		ceiling = kinds.CodexEffortCeiling(model, env)
	}
	output := jsonjs.O(
		"kind", kind,
		"spec", spec,
		"effort", effort,
		"model", model,
		"family", kinds.AgentFamily(kind, model),
		"effort_ceiling", ceiling,
		"agent_args", strings.Join(args, " "),
	)
	_, _ = platform.Stdout.Write([]byte(jsonjs.StringifyIndent(output, 2) + "\n"))
	return 0
}
