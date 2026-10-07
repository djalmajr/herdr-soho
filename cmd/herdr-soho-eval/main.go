// Command herdr-soho-eval is the native Go port of the legacy evaluation kit
// entry points evals/prepare.mjs and evals/run-probes.mjs. It prints one JSON
// object per run on stdout and exits 0 for measured results (including
// failed probes and scope violations) or 2 for usage, manifest, path-safety
// or unmeasurable-probe errors, with diagnostics on stderr.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/eval"
)

const usage = "usage: herdr-soho-eval [--repo <path>] prepare <fixture> <destination>\n" +
	"       herdr-soho-eval [--repo <path>] probes <fixture> <destination>"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	repository := ""
	rest := argv
	for len(rest) > 0 {
		if rest[0] == "--repo" {
			if len(rest) < 2 {
				return failUsage()
			}
			repository = rest[1]
			rest = rest[2:]
			continue
		}
		if strings.HasPrefix(rest[0], "--repo=") {
			repository = strings.TrimPrefix(rest[0], "--repo=")
			rest = rest[1:]
			continue
		}
		break
	}
	if len(rest) != 3 {
		return failUsage()
	}
	ctx := context.Background()
	switch rest[0] {
	case "prepare":
		result, err := eval.Prepare(ctx, eval.Options{
			Fixture:     rest[1],
			Destination: rest[2],
			Repository:  repository,
		})
		if err != nil {
			return fail("prepare", err)
		}
		return printJSON(result)
	case "probes":
		result, err := eval.Probes(ctx, eval.Options{
			Fixture:     rest[1],
			Destination: rest[2],
			Repository:  repository,
		})
		if err != nil {
			return fail("probes", err)
		}
		return printJSON(result)
	default:
		return failUsage()
	}
}

// failUsage prints the usage lines and reports the usage error code 2.
func failUsage() int {
	fmt.Fprintln(os.Stderr, usage)
	return 2
}

func fail(subcommand string, err error) int {
	fmt.Fprintf(os.Stderr, "%s: %v\n", subcommand, err)
	return 2
}

func printJSON(value any) int {
	payload, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "herdr-soho-eval: %v\n", err)
		return 2
	}
	if _, err := fmt.Fprintf(os.Stdout, "%s\n", payload); err != nil {
		fmt.Fprintf(os.Stderr, "herdr-soho-eval: %v\n", err)
		return 2
	}
	return 0
}
