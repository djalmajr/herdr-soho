package job

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// own_checkout.go is the own-checkout anchor of the job --id lookup: the
// store of the job whose recorded directory the caller stands in, found
// from the caller's cwd alone, so a command run inside a job's worktree or
// workspace checkout finds that job even when the caller's user
// configuration names another checkout root. It is structural ownership,
// not authentication: a same-user process can build a checkout, a worktree
// registration and a state.json that pass every check, as it can write any
// job store directly. Any check that fails yields no anchor; it never
// widens a lookup and never fails the command by itself.

const (
	// ownCheckoutGitTimeoutMs bounds the one git query of the anchor.
	ownCheckoutGitTimeoutMs = 10_000
	// ownCheckoutGitOutputMax bounds that query's output: three paths.
	ownCheckoutGitOutputMax = 3 * 4097
	// ownCheckoutLinkMax bounds a worktree's .git and gitdir files.
	ownCheckoutLinkMax = 4096 + 16
	// ownCheckoutStateMax bounds the state.json the anchor reads.
	ownCheckoutStateMax = 1 << 20
)

// ownCheckoutGit runs the anchor's git query; tests swap it.
var ownCheckoutGit = func(env platform.Env, cwd string) (string, bool) {
	result := platform.RunCli("git", []string{"rev-parse", "--path-format=absolute", "--show-toplevel", "--absolute-git-dir", "--git-common-dir"}, platform.RunOptions{Env: env, Cwd: cwd, TimeoutMs: ownCheckoutGitTimeoutMs})
	if result.NotFound || result.TimedOut || result.Status == nil || *result.Status != 0 {
		return "", false
	}
	return result.Stdout, true
}

// ownCheckoutState is the part of state.json the anchor checks.
type ownCheckoutState struct {
	Schema   int    `json:"schema"`
	ID       string `json:"id"`
	Checkout string `json:"checkout"`
	Dir      string `json:"dir"`
	Modo     string `json:"modo"`
}

// OwnCheckoutStore returns the store of job id when cwd lies in that job's
// own directory, else nil:
//
//  1. one bounded git query from cwd (its top level, git dir and common
//     dir; git's own discovery, nothing above the nearest repository);
//  2. cwd, by real path, is the top level or inside it;
//  3. the registration, read from the files themselves: workspace mode,
//     the git dir is the common dir <top>/.git, a real directory; worktree
//     mode, the common dir is <checkout>/.git, a real directory, the top
//     level is <checkout>/.worktrees/job-<id>, its .git file names the git
//     dir <common>/worktrees/<name>, and that git dir's gitdir file names
//     the top level's .git back;
//  4. the store root is the checkout's state root under the caller's own
//     configuration (core.StateRootPath, as the root scan resolves it),
//     and the root, jobs and jobs/<id> are real directories at their own
//     paths;
//  5. jobs/<id>/state.json (bounded, regular, not followed) is schema 1,
//     names id and the measured mode, and records the measured checkout and
//     job directory by real path.
func OwnCheckoutStore(env platform.Env, cwd, id string) *Store {
	if cwd == "" {
		return nil
	}
	if _, err := Dir("", id); err != nil {
		return nil
	}
	out, ok := ownCheckoutGit(env, cwd)
	if !ok || len(out) > ownCheckoutGitOutputMax {
		return nil
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(out, "\r\n", "\n"), "\n"), "\n")
	if len(lines) != 3 {
		return nil
	}
	top, gitDir, common := realAbs(lines[0]), realAbs(lines[1]), realAbs(lines[2])
	realCwd := realAbs(cwd)
	if top == "" || gitDir == "" || common == "" || realCwd == "" || !withinOrSame(top, realCwd) {
		return nil
	}
	if filepath.Base(common) != ".git" || !exactDirectory(common) {
		return nil
	}
	checkout := filepath.Dir(common)
	mode := "workspace"
	if gitDir != common {
		mode = "worktree"
		if filepath.Dir(gitDir) != filepath.Join(common, "worktrees") {
			return nil
		}
		if top != filepath.Join(checkout, ".worktrees", "job-"+id) {
			return nil
		}
		if !worktreeRegistered(top, gitDir) {
			return nil
		}
	} else if top != checkout || filepath.Join(top, ".git") != common {
		return nil
	}
	cfg := core.LoadConfig(env, checkout)
	root := core.StateRootPath(&cfg, env, checkout)
	jobDir := filepath.Join(root, "jobs", id)
	if !exactDirectory(root) || !exactDirectory(filepath.Join(root, "jobs")) || !exactDirectory(jobDir) {
		return nil
	}
	data, ok, err := readPrivateFile(filepath.Join(jobDir, "state.json"), ownCheckoutStateMax)
	if err != nil || !ok {
		return nil
	}
	var st ownCheckoutState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil
	}
	if st.Schema != 1 || st.ID != id || st.Modo != mode || !filepath.IsAbs(st.Checkout) || !filepath.IsAbs(st.Dir) {
		return nil
	}
	if realAbs(st.Checkout) != checkout || realAbs(st.Dir) != top {
		return nil
	}
	return Open(root)
}

// worktreeRegistered reports whether the linked worktree top is registered
// at gitDir both ways: top/.git is a regular file naming gitDir, and
// gitDir/gitdir is a regular file naming top/.git (real paths).
func worktreeRegistered(top, gitDir string) bool {
	dotGit := filepath.Join(top, ".git")
	link, ok, err := readPrivateFile(dotGit, ownCheckoutLinkMax)
	if err != nil || !ok {
		return false
	}
	named, found := strings.CutPrefix(strings.TrimSpace(string(link)), "gitdir:")
	if !found {
		return false
	}
	named = strings.TrimSpace(named)
	if !filepath.IsAbs(filepath.FromSlash(named)) {
		named = filepath.Join(top, named)
	}
	if realAbs(named) != gitDir {
		return false
	}
	back, ok, err := readPrivateFile(filepath.Join(gitDir, "gitdir"), ownCheckoutLinkMax)
	if err != nil || !ok {
		return false
	}
	return realAbs(strings.TrimSpace(string(back))) == realAbs(dotGit)
}

// realAbs is the real absolute path of an existing path, "" otherwise.
func realAbs(path string) string {
	path = filepath.FromSlash(path)
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(real)
}

// withinOrSame reports whether path is dir or lies inside it (both real).
func withinOrSame(dir, path string) bool {
	if path == dir {
		return true
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// exactDirectory reports whether path is a directory at its own path: not
// a symlink and, on Windows, not a junction or other reparse point (their
// Lstat type is not a plain directory).
func exactDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().Type() == os.ModeDir
}
