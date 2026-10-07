// Command herdr-soho-release builds the six release binaries and publishes
// them with the native gh CLI, replacing the Node/Bash release workflow.
// Standard library only: go and gh are external Go CLIs invoked by argv,
// never through a shell, and no credential is ever printed or passed on
// the command line.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/djalmajr/herdr-soho/internal/release"
)

// main returns before os.Exit so the deferred signal-context stop and the
// Build staging defers actually run: an owned Ctrl-C/SIGTERM cancels the
// context, the exec.CommandContext go/gh children are killed, Build
// returns through its defer (removing only the owned staging), and run
// reports the error as the exit code.
func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		usage()
		return 2
	}
	// One process-wide signal context for the whole invocation, passed to
	// every build/publish/CI consumer. os.Interrupt covers the console
	// ^C/^BREAK on every platform; on Unix SIGTERM is delivered the usual
	// way, and on Windows the installed os/signal package documents that
	// Notify delivers syscall.SIGTERM for CTRL_CLOSE/LOGOFF/SHUTDOWN
	// events, so both are registered without a per-OS branch.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "build":
		err = runBuild(ctx, os.Args[2:])
	case "publish":
		err = runPublish(ctx, os.Args[2:])
	case "ci-build":
		err = runCIBuild(ctx, os.Args[2:])
	case "ci-publish":
		err = runCIPublish(ctx, os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprint(os.Stderr, `herdr-soho-release builds and publishes the herdr-soho release assets.

usage:
  herdr-soho-release build [--version V] [--dest DIR] [--repo DIR]
  herdr-soho-release publish --version V --repo OWNER/REPO [--dest DIR]
  herdr-soho-release ci-build [--dest DIR] [--repo DIR]
  herdr-soho-release ci-publish [--dest DIR]

build stages the six exact assets (darwin/linux/windows x amd64/arm64) plus
SHA256SUMS into a fresh sibling directory and moves them into --dest,
refusing to overwrite existing destination files. publish verifies the
assets and SHA256SUMS, then runs:
  gh release create TAG FILES --repo OWNER/REPO --generate-notes [--prerelease]
ci-build resolves the version from the event: dev for workflow_dispatch,
otherwise the validated tag from GITHUB_REF_NAME/GITHUB_REF. ci-publish
only runs on a push of an exact refs/tags/v... ref and uses
GITHUB_REPOSITORY explicitly.
`)
}

// rejectPositional returns an error when the parser saw trailing
// positional arguments instead of flags only.
func rejectPositional(fs *flag.FlagSet) error {
	if fs.NArg() > 0 {
		return fmt.Errorf("release: %s: unexpected arguments: %v", fs.Name(), fs.Args())
	}
	return nil
}

func runBuild(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	version := fs.String("version", release.DevVersion, "build version (dev or v-prefixed release tag)")
	dest := fs.String("dest", "dist", "output directory")
	repoDir := fs.String("repo", ".", "repository root")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := rejectPositional(fs); err != nil {
		return err
	}
	res, err := release.Build(ctx, release.BuildConfig{
		Version:      *version,
		RepoDir:      *repoDir,
		Dest:         *dest,
		GoExecutable: "",
	})
	if err != nil {
		return err
	}
	printBuild(res)
	return nil
}

func runPublish(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	version := fs.String("version", "", "v-prefixed release tag (required)")
	repo := fs.String("repo", "", "OWNER/REPO (required)")
	dest := fs.String("dest", "dist", "artifact directory")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := rejectPositional(fs); err != nil {
		return err
	}
	if *version == "" {
		return fmt.Errorf("release: publish requires --version")
	}
	if *repo == "" {
		return fmt.Errorf("release: publish requires --repo")
	}
	res, err := release.Publish(ctx, release.PublishConfig{
		Version: *version,
		Repo:    *repo,
		Dest:    *dest,
	})
	if err != nil {
		return err
	}
	printPublish(res)
	return nil
}

func runCIBuild(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ci-build", flag.ContinueOnError)
	dest := fs.String("dest", "dist", "output directory")
	repoDir := fs.String("repo", ".", "repository root")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := rejectPositional(fs); err != nil {
		return err
	}
	version, err := release.ResolveBuildVersion(
		os.Getenv("GITHUB_EVENT_NAME"),
		os.Getenv("GITHUB_REF_NAME"),
		os.Getenv("GITHUB_REF"),
	)
	if err != nil {
		return err
	}
	res, err := release.Build(ctx, release.BuildConfig{
		Version: version,
		RepoDir: *repoDir,
		Dest:    *dest,
	})
	if err != nil {
		return err
	}
	printBuild(res)
	return nil
}

func runCIPublish(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ci-publish", flag.ContinueOnError)
	dest := fs.String("dest", "dist", "artifact directory")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := rejectPositional(fs); err != nil {
		return err
	}
	tag, err := release.ResolvePublishRef(
		os.Getenv("GITHUB_EVENT_NAME"),
		os.Getenv("GITHUB_REF"),
	)
	if err != nil {
		return err
	}
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return fmt.Errorf("release: ci-publish requires GITHUB_REPOSITORY")
	}
	res, err := release.Publish(ctx, release.PublishConfig{
		Version: tag,
		Repo:    repo,
		Dest:    *dest,
	})
	if err != nil {
		return err
	}
	printPublish(res)
	return nil
}

// printBuild reports the written artifacts. No environment values are
// printed.
func printBuild(res release.BuildResult) {
	fmt.Printf("built %s into %s\n", res.Version, res.Dest)
	for _, name := range res.Artifacts {
		fmt.Printf("  %s\n", name)
	}
	fmt.Printf("  %s\n", res.Sums)
}

// printPublish reports what was passed to gh. No environment values or
// credentials are printed.
func printPublish(res release.PublishResult) {
	prefix := ""
	if res.Prerelease {
		prefix = " (prerelease)"
	}
	fmt.Printf("published %s to %s%s\n", res.Tag, res.Repo, prefix)
	for _, f := range res.Files {
		fmt.Printf("  %s\n", f)
	}
}
