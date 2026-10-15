package job

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	// ownCheckoutGitTimeout bounds the one git query of the anchor.
	ownCheckoutGitTimeout = 10 * time.Second
	// ownCheckoutGitOutputMax bounds what that query's stdout may hold
	// (three paths); more is never stored and refuses the anchor.
	ownCheckoutGitOutputMax = 3 * 4097
	// ownCheckoutLinkMax bounds a worktree's .git and gitdir files.
	ownCheckoutLinkMax = 4096 + 16
	// ownCheckoutStateMax bounds the state.json the anchor reads.
	ownCheckoutStateMax = 1 << 20
)

// ownCheckoutGitQuery runs the anchor's git query from cwd under env
// (boundedGit).
func ownCheckoutGitQuery(env platform.Env, cwd string) (string, bool) {
	return boundedGit(env, cwd, "rev-parse", "--path-format=absolute", "--show-toplevel", "--absolute-git-dir", "--git-common-dir")
}

// boundedGit runs one read-only git command from dir under env: the git on
// env's PATH, a 10 s deadline, stderr discarded and stdout kept up to
// ownCheckoutGitOutputMax bytes (the rest is drained, not stored, and
// refuses the answer); a descendant holding stdout is waited for at most
// one second after the deadline or the exit.
func boundedGit(env platform.Env, dir string, args ...string) (string, bool) {
	git, ok := platform.FindExecutable("git", env, platform.Current())
	if !ok {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), ownCheckoutGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Dir = dir
	cmd.Env = env.List()
	out := &cappedOutput{max: ownCheckoutGitOutputMax}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil || out.over {
		return "", false
	}
	return out.buf.String(), true
}

// cappedOutput keeps at most max bytes and records that more arrived.
type cappedOutput struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (c *cappedOutput) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); len(p) > room {
		c.over = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

// parseOwnCheckoutGit reads the query's answer: exactly three absolute
// paths, one per line (the top level, the git dir and the common dir).
func parseOwnCheckoutGit(out string) (top, gitDir, common string, ok bool) {
	if len(out) > ownCheckoutGitOutputMax {
		return "", "", "", false
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(out, "\r\n", "\n"), "\n"), "\n")
	if len(lines) != 3 {
		return "", "", "", false
	}
	for i, line := range lines {
		lines[i] = filepath.FromSlash(line)
		if line == "" || !filepath.IsAbs(lines[i]) {
			return "", "", "", false
		}
	}
	return lines[0], lines[1], lines[2], true
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
//  1. one bounded git query from cwd (ownCheckoutGitQuery: its top level,
//     git dir and common dir; git's own discovery, nothing above the
//     nearest repository), three absolute paths (parseOwnCheckoutGit);
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
	out, ok := ownCheckoutGitQuery(env, cwd)
	if !ok {
		return nil
	}
	rawTop, rawGitDir, rawCommon, ok := parseOwnCheckoutGit(out)
	if !ok {
		return nil
	}
	top, gitDir, common := realAbs(rawTop), realAbs(rawGitDir), realAbs(rawCommon)
	realCwd := realAbs(cwd)
	if top == "" || gitDir == "" || common == "" || realCwd == "" || !insideOrSame(top, realCwd) {
		return nil
	}
	if filepath.Base(common) != ".git" || !exactDirectory(common) {
		return nil
	}
	checkout := filepath.Dir(common)
	mode := "workspace"
	if !sameEntryPath(gitDir, common) {
		mode = "worktree"
		if !sameEntryPath(filepath.Dir(gitDir), filepath.Join(common, "worktrees")) {
			return nil
		}
		if !sameEntryPath(top, filepath.Join(checkout, ".worktrees", "job-"+id)) {
			return nil
		}
		if !worktreeRegistered(top, gitDir) {
			return nil
		}
	} else if !sameEntryPath(top, checkout) || !sameEntryPath(filepath.Join(top, ".git"), common) {
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
	if !sameEntryPath(st.Checkout, checkout) || !sameEntryPath(st.Dir, top) {
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
	if !sameEntryPath(named, gitDir) {
		return false
	}
	back, ok, err := readPrivateFile(filepath.Join(gitDir, "gitdir"), ownCheckoutLinkMax)
	if err != nil || !ok {
		return false
	}
	return sameEntryPath(filepath.FromSlash(strings.TrimSpace(string(back))), dotGit)
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

// withinOrSame reports whether path is dir or lies inside it, by their
// clean path strings (both already real).
func withinOrSame(dir, path string) bool {
	if path == dir {
		return true
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// sameEntryPath reports whether a and b name the same existing entry
// (os.SameFile of their Stat): an alias, or a case variant on a
// case-insensitive file system, is the same entry, which a comparison of
// path strings would not see.
func sameEntryPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	return err == nil && os.SameFile(ia, ib)
}

// insideOrSame reports whether path is dir or lies below it by identity:
// path or one of its ancestors is the same entry as dir.
func insideOrSame(dir, path string) bool {
	di, err := os.Stat(dir)
	if err != nil {
		return false
	}
	for p := filepath.Clean(path); ; {
		if pi, err := os.Stat(p); err == nil && os.SameFile(di, pi) {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}

// exactDirectory reports whether path is a directory at its own path: not
// a symlink and, on Windows, not a junction or other reparse point (their
// Lstat type is not a plain directory).
func exactDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().Type() == os.ModeDir
}
