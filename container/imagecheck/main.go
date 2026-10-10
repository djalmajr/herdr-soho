// Command imagecheck verifies the base container image and its build
// context: it scans the curated context and the saved image for secrets,
// credential files and denied tokens, compares two saved builds layer by
// layer, and smoke-tests the installed tool inventory. Standard library
// only; docker is an external CLI invoked by argv, never through a shell,
// and a matched secret or denied token is never printed.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches one subcommand. Exit codes: 0 pass, 1 check failed
// (findings, differences or inventory mismatch), 2 usage, 4 I/O or
// external command failure.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "context":
		return runContext(args[1:], stdout, stderr)
	case "image":
		return runImage(args[1:], stdout, stderr)
	case "compare":
		return runCompare(args[1:], stdout, stderr)
	case "smoke":
		return runSmoke(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "imagecheck: unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage:
  imagecheck context <dir> --allow <prefix>... [--deny-file <path>] [--accept-file <path>] [--accept <path>=<rule>]...
  imagecheck image <docker-save.tar> [--deny-file <path>] [--accept-file <path>] [--accept <path>=<rule>]...
  imagecheck compare <a.tar> <b.tar>
  imagecheck smoke --image <ref> --spec <path> [--dockerfile <path>] [--build-arg NAME=VALUE]... [--docker <exe>] [--timeout <duration>]
`)
}
