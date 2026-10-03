package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

const (
	copiesUsage = "usage: herdr-soho copies [add <path> [--agent <name>]]"
	gcUsage     = "usage: herdr-soho gc [--yes] [--older-than HOURS] [--include-unregistered]"

	// copiesTimeLayout is the ISO-8601 UTC format the registry stores in the
	// created column (core.FrictionISO).
	copiesTimeLayout = "2006-01-02T15:04:05Z"

	// gcUnregisteredHeader is the separate output block for mutation copies
	// that the registry does not know.
	gcUnregisteredHeader = "unregistered mutation copies (another project or an older version may own them):"
)

// copyRefusal is the single safety check (decision 7) that release, gc and
// copies add run before removing or registering a copy path. It returns the
// refusal cause, or "" when the path passes.
func copyRefusal(path string, env platform.Env, cwd string) string {
	if !filepath.IsAbs(path) {
		return "not an absolute path"
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "does not exist"
	}
	if info, lstatErr := os.Lstat(path); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "is a symlink"
	}
	if isFilesystemRoot(resolved) {
		return "is the filesystem root"
	}
	if home := mutationCopyHome(env); home != "" && (resolved == home || inside(resolved, home)) {
		return "is HOME or above it"
	}
	if root := platform.StateProjectRoot(env, cwd); root != "" && (inside(root, resolved) || inside(resolved, root)) {
		return "is the project root or inside it"
	}
	if _, gitErr := os.Lstat(filepath.Join(resolved, ".git")); gitErr == nil {
		return "contains a .git (another repository or a worktree)"
	}
	return ""
}

// rosterAgentForPane returns the roster name whose pane is pane, or "".
func rosterAgentForPane(stateDir, pane string) string {
	if pane == "" {
		return ""
	}
	for _, line := range core.RosterRows(stateDir) {
		fields := strings.Split(line, "\t")
		if len(fields) > 1 && fields[1] == pane && fields[0] != "" {
			return fields[0]
		}
	}
	return ""
}

// cmdCopies lists the copy registry, or registers a copy made by hand.
func cmdCopies(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	if len(argv) == 0 {
		return copiesList(ctx, env, cwd)
	}
	if argv[0] != "add" {
		_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: %s\n", copiesUsage)
		return 2
	}
	return copiesAdd(argv[1:], ctx, env, cwd)
}

// copiesList prints one line per registered copy, "<path>  <owner>  <age>
// <state>". It is read-only: it removes nothing and writes nothing, and it
// does not show sizes.
func copiesList(ctx *core.Config, env platform.Env, cwd string) int {
	sd := core.StateDir(ctx, env, cwd)
	rows, err := core.ReadCopies(sd)
	if err != nil {
		core.DieFriction(fmt.Sprintf("copies: cannot read the copy registry (%v)", err), 4, frictionLogPath, "copies")
	}
	now := platform.Now()
	for _, row := range rows {
		_, _ = fmt.Fprintf(platform.Stdout, "%s  %s  %s  %s\n", row.Path, row.Owner, humanAge(copyAge(row, now)), copyRowState(row, sd, env))
	}
	return 0
}

// copyRowState is the state of a registry line: missing when the path is
// gone, owned when the owner is a live roster agent, orphan otherwise (the
// owner is "-", is not in the roster, or is gone). An owner Herdr cannot
// answer for stays owned: gc must not drop a copy it cannot prove orphaned.
func copyRowState(row core.CopyRow, sd string, env platform.Env) string {
	if _, err := os.Stat(row.Path); err != nil {
		return "missing"
	}
	if row.Owner != "-" && core.RosterLine(sd, row.Owner) != "" && herdr.AgentState(row.Owner, env, herdr.Timeout, nil).State != "gone" {
		return "owned"
	}
	return "orphan"
}

// copyAge is how long the copy has existed; an unparseable or future created
// reads as 0, so gc never treats an unknown age as old.
func copyAge(row core.CopyRow, now time.Time) time.Duration {
	created, err := time.Parse(copiesTimeLayout, row.Created)
	if err != nil {
		return 0
	}
	age := now.Sub(created)
	if age < 0 {
		return 0
	}
	return age
}

// copiesAdd registers a copy made by hand (a reviewer's copy or the
// orchestrator's own) with origin add.
func copiesAdd(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	var path, agent string
	agentSeen := false
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--agent" {
			if agentSeen || i+1 >= len(argv) || argv[i+1] == "" {
				return copiesAddUsageError()
			}
			agent = argv[i+1]
			agentSeen = true
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return copiesAddUsageError()
		}
		if path != "" {
			return copiesAddUsageError()
		}
		path = arg
	}
	if path == "" {
		return copiesAddUsageError()
	}
	if cause := copyRefusal(path, env, cwd); cause != "" {
		_, _ = fmt.Fprintf(platform.Stderr, "copies add: refusing '%s': %s\n", path, cause)
		return 2
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !isExistingDir(resolved) {
		_, _ = fmt.Fprintf(platform.Stderr, "copies add: refusing '%s': not an existing directory\n", path)
		return 2
	}
	sd := core.StateDir(ctx, env, cwd)
	pane := env.Get("HERDR_PANE_ID")
	owner := "-"
	if agentSeen {
		line := core.RosterLine(sd, agent)
		if line == "" {
			core.DieFriction(fmt.Sprintf("copies add: agent '%s' is not in the roster (state dir: %s)", agent, sd), 3, frictionLogPath, "copies")
		}
		owner = agent
		if fields := strings.Split(line, "\t"); len(fields) > 1 && fields[1] != "" {
			pane = fields[1]
		}
	} else if pane != "" {
		owner = rosterAgentForPane(sd, pane)
		if owner == "" {
			owner = "-"
		}
	}
	if pane == "" {
		pane = "-"
	}
	row := core.CopyRow{Path: resolved, Owner: owner, Pane: pane, Created: core.FrictionISO(platform.Now()), Source: "-", Origin: "add"}
	if err := core.UpsertCopies(sd, row); err != nil {
		core.DieFriction(fmt.Sprintf("copies add: could not register '%s' (%v)", path, err), 4, frictionLogPath, "copies")
	}
	return 0
}

func copiesAddUsageError() int {
	_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: %s\n", copiesUsage)
	return 2
}

func isExistingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// gcCandidate is one registry line gc may act on: an orphan old enough, or a
// missing line that only leaves the registry.
type gcCandidate struct {
	path    string
	owner   string
	age     time.Duration
	size    int64
	missing bool
}

// gcUnregisteredCopy is a herdr-soho-mutation-* directory below TMPDIR that
// the registry does not know.
type gcUnregisteredCopy struct {
	path string
	size int64
	age  time.Duration
}

// cmdGc lists the orphan copies the registry holds (dry run), or removes the
// ones at least --older-than hours old. Unregistered mutation copies are
// always listed in their own block and are only removed with
// --include-unregistered --yes.
func cmdGc(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	yes, includeUnregistered := false, false
	hours := "2"
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch arg {
		case "--yes":
			yes = true
		case "--include-unregistered":
			includeUnregistered = true
		case "--older-than":
			if i+1 >= len(argv) || argv[i+1] == "" {
				return gcUsageError()
			}
			hours = argv[i+1]
			i++
		default:
			return gcUsageError()
		}
	}
	thresholdHours, err := parseGcHours(hours)
	if err != nil {
		_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: gc: --older-than expects a non-negative integer (%s)\n", hours)
		return 2
	}
	sd := core.StateDir(ctx, env, cwd)
	rows, err := core.ReadCopies(sd)
	if err != nil {
		core.DieFriction(fmt.Sprintf("gc: cannot read the copy registry (%v)", err), 4, frictionLogPath, "gc")
	}
	now := platform.Now()
	threshold := time.Duration(thresholdHours) * time.Hour

	candidates := []gcCandidate{}
	for _, row := range rows {
		switch copyRowState(row, sd, env) {
		case "missing":
			candidates = append(candidates, gcCandidate{path: row.Path, owner: row.Owner, age: copyAge(row, now), missing: true})
		case "orphan":
			age := copyAge(row, now)
			if age >= threshold {
				candidates = append(candidates, gcCandidate{path: row.Path, owner: row.Owner, age: age, size: copyDirSize(row.Path)})
			}
		}
		// owned lines are never touched
	}
	unregistered := gcUnregistered(env, rows, now, threshold)

	total := int64(0)
	for _, cand := range candidates {
		total += cand.size
	}
	if !yes {
		for _, cand := range candidates {
			_, _ = fmt.Fprintf(platform.Stdout, "would remove %s  %s  %s  %s\n", cand.path, humanSize(cand.size), cand.owner, humanAge(cand.age))
		}
		_, _ = fmt.Fprintf(platform.Stdout, "total: %s\n", humanSize(total))
		_, _ = fmt.Fprintln(platform.Stdout, "run 'herdr-soho gc --yes' to remove them")
		if len(unregistered) > 0 {
			gcPrintUnregistered(unregistered, false)
		}
		return 0
	}

	removedLines := map[string]bool{}
	freed := int64(0)
	for _, cand := range candidates {
		if cand.missing {
			// Nothing is left on disk: the line only leaves the registry.
			removedLines[cand.path] = true
			_, _ = fmt.Fprintf(platform.Stdout, "removed %s  %s\n", cand.path, humanSize(0))
			continue
		}
		if cause := copyRefusal(cand.path, env, cwd); cause != "" {
			_, _ = fmt.Fprintf(platform.Stdout, "kept %s (%s)\n", cand.path, cause)
			continue
		}
		if err := os.RemoveAll(cand.path); err != nil {
			_, _ = fmt.Fprintf(platform.Stdout, "kept %s (%v)\n", cand.path, err)
			continue
		}
		removedLines[cand.path] = true
		freed += cand.size
		_, _ = fmt.Fprintf(platform.Stdout, "removed %s  %s\n", cand.path, humanSize(cand.size))
	}
	if len(removedLines) > 0 {
		out := make([]core.CopyRow, 0, len(rows))
		for _, row := range rows {
			if !removedLines[row.Path] {
				out = append(out, row)
			}
		}
		if err := core.WriteCopies(sd, out); err != nil {
			core.DieFriction(fmt.Sprintf("gc: cannot update the copy registry (%v)", err), 4, frictionLogPath, "gc")
		}
	}
	if len(unregistered) > 0 {
		_, _ = fmt.Fprintln(platform.Stdout, gcUnregisteredHeader)
		if includeUnregistered {
			for _, entry := range unregistered {
				if cause := copyRefusal(entry.path, env, cwd); cause != "" {
					_, _ = fmt.Fprintf(platform.Stdout, "kept %s (%s)\n", entry.path, cause)
					continue
				}
				if err := os.RemoveAll(entry.path); err != nil {
					_, _ = fmt.Fprintf(platform.Stdout, "kept %s (%v)\n", entry.path, err)
					continue
				}
				freed += entry.size
				_, _ = fmt.Fprintf(platform.Stdout, "removed %s  %s\n", entry.path, humanSize(entry.size))
			}
		} else {
			for _, entry := range unregistered {
				_, _ = fmt.Fprintf(platform.Stdout, "%s  %s  %s\n", entry.path, humanSize(entry.size), humanAge(entry.age))
			}
		}
	}
	_, _ = fmt.Fprintf(platform.Stdout, "freed: %s\n", humanSize(freed))
	return 0
}

// gcPrintUnregistered lists unregistered copies in their own block without
// acting on them.
func gcPrintUnregistered(entries []gcUnregisteredCopy, _ bool) {
	_, _ = fmt.Fprintln(platform.Stdout, gcUnregisteredHeader)
	for _, entry := range entries {
		_, _ = fmt.Fprintf(platform.Stdout, "%s  %s  %s\n", entry.path, humanSize(entry.size), humanAge(entry.age))
	}
}

// gcUnregistered lists the herdr-soho-mutation-* directories directly below
// TMPDIR (or os.TempDir() without TMPDIR) that are not in the registry and
// whose mtime is older than the threshold, sorted by path.
func gcUnregistered(env platform.Env, rows []core.CopyRow, now time.Time, threshold time.Duration) []gcUnregisteredCopy {
	tmpDir := env.Get("TMPDIR")
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	registered := map[string]bool{}
	for _, row := range rows {
		registered[row.Path] = true
		if resolved, err := filepath.EvalSymlinks(row.Path); err == nil {
			registered[resolved] = true
		}
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil
	}
	out := []gcUnregisteredCopy{}
	for _, entry := range entries {
		path := filepath.Join(tmpDir, entry.Name())
		if !strings.HasPrefix(entry.Name(), "herdr-soho-mutation-") {
			continue
		}
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			continue
		}
		resolved, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil || registered[resolved] {
			continue
		}
		age := now.Sub(info.ModTime())
		if age <= threshold {
			continue
		}
		out = append(out, gcUnregisteredCopy{path: path, size: copyDirSize(path), age: age})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func gcUsageError() int {
	_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: %s\n", gcUsage)
	return 2
}

func parseGcHours(value string) (int64, error) {
	if !cleanDaysPattern.MatchString(value) {
		return 0, fmt.Errorf("invalid hour count")
	}
	var n int64
	for _, r := range value {
		n = n*10 + int64(r-'0')
		if n > 1<<60 {
			return 0, fmt.Errorf("hour count too large")
		}
	}
	return n, nil
}

// humanAge renders an age for the copies and gc output: 0m, 3h, 2d.
func humanAge(d time.Duration) string {
	if d < time.Minute {
		return "0m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int64(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int64(d.Hours()))
	}
	return fmt.Sprintf("%dd", int64(d.Hours())/24)
}

// humanSize renders a byte count in base-1024 human units (1.2 GB).
func humanSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, unit := range []struct {
		size float64
		name string
	}{
		{1 << 40, "TB"},
		{1 << 30, "GB"},
		{1 << 20, "MB"},
	} {
		if value >= unit.size {
			return fmt.Sprintf("%.1f %s", value/unit.size, unit.name)
		}
	}
	return fmt.Sprintf("%.1f KB", value/1024)
}

// copyDirSize sums the sizes of the regular files under root without
// following symlinks.
func copyDirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
