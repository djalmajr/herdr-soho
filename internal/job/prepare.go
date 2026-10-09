package job

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// prepare.go is the preparation step of `herdr-soho job start`: it admits
// the organization, checks gh authentication, clones a missing checkout,
// fetches, resolves the base, and creates (or reuses) the job worktree —
// or validates the checkout for workspace mode — then dry-runs the push.
// It stops at the first failure and never writes job state, events, or
// reports. Its errors are fixed strings that name the step: subprocess
// output, remote URLs, and environment values never appear in them.

const (
	defaultLocalPrepareTimeout = 60 * time.Second  // local git commands
	defaultNetPrepareTimeout   = 300 * time.Second // fetch, ls-remote, push dry-run, clone
	defaultGhPrepareTimeout    = 30 * time.Second  // gh auth status
)

var (
	baseNamePattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
	errSubprocess   = errors.New("job: preparation subprocess failed")
)

// PrepareRequest is the preparation input for one job. Base empty means the
// remote default branch; Mode is "worktree" (also when empty) or
// "workspace". The CLI already validated the id, repo, and base regexes;
// Prepare re-validates the id with the same rule as Dir.
type PrepareRequest struct {
	ID           string
	Org          string
	Repo         string
	Base         string
	Mode         string
	BriefMachine string
}

// Prepared describes the prepared checkout. Dir is the worktree path in
// worktree mode and the checkout in workspace mode; Branch is job/<id> in
// worktree mode and the checkout's current branch name in workspace mode;
// BaseSHA is the full sha of refs/remotes/origin/<base>.
type Prepared struct {
	Checkout string
	Dir      string
	Branch   string
	Base     string
	BaseSHA  string
	Cloned   bool
}

// Preparer runs the preparation steps. Zero timeouts select the defaults:
// LocalTimeout 60 s, NetTimeout 300 s, GhTimeout 30 s. A nil Clone uses the
// production `gh repo clone`.
type Preparer struct {
	Machine      Machine
	Env          platform.Env
	Clone        func(org, repo, dest string) error
	LocalTimeout time.Duration
	NetTimeout   time.Duration
	GhTimeout    time.Duration
}

func prepareError(step string) error {
	return &ExitError{Code: ExitPrepare, Msg: "job: preparation failed: " + step}
}

// validPrepareID applies the Dir id rule: the contract regex, and "." / ".."
// refused so a worktree path cannot leave the checkout.
func validPrepareID(id string) bool {
	return idPattern.MatchString(id) && id != "." && id != ".."
}

// Prepare prepares the job checkout. It stops at the first failure; nothing
// after a failure runs. The step order is: admission, gh authentication,
// clone a missing checkout, fetch, resolve the base, prepare the worktree
// or validate the workspace, and the push dry-run.
func (p Preparer) Prepare(req PrepareRequest) (Prepared, error) {
	local := p.LocalTimeout
	if local <= 0 {
		local = defaultLocalPrepareTimeout
	}
	net := p.NetTimeout
	if net <= 0 {
		net = defaultNetPrepareTimeout
	}
	gh := p.GhTimeout
	if gh <= 0 {
		gh = defaultGhPrepareTimeout
	}
	var out Prepared
	if !validPrepareID(req.ID) {
		return out, errUsage("job: invalid id")
	}
	if req.Mode != "" && req.Mode != "worktree" && req.Mode != "workspace" {
		return out, errUsage("job: invalid mode")
	}
	if err := p.Machine.Admit(req.BriefMachine, req.Org, req.Repo); err != nil {
		return out, err
	}
	branch := "job/" + req.ID
	workspace := req.Mode == "workspace"
	gitEnv := p.Env.Clone()
	gitEnv["GIT_TERMINAL_PROMPT"] = "0"
	ghEnv := p.Env.Clone()
	ghEnv["GH_PROMPT_DISABLED"] = "1"

	// Gh authentication and push access are checked for every job, before
	// any pane opens and before any clone.
	if _, err := p.cli("gh", []string{"auth", "status"}, gh, ghEnv); err != nil {
		return out, prepareError("gh authentication")
	}
	checkout := filepath.Join(p.Machine.ReposRoot, req.Org, req.Repo)
	out.Checkout = checkout
	if _, statErr := os.Stat(checkout); statErr == nil {
		if !p.isGitCheckout(checkout, local, gitEnv) {
			return out, prepareError("not a git checkout")
		}
	} else if os.IsNotExist(statErr) {
		if err := os.MkdirAll(filepath.Join(p.Machine.ReposRoot, req.Org), 0o755); err != nil {
			return out, prepareError("clone")
		}
		var cloneErr error
		if p.Clone != nil {
			cloneErr = p.Clone(req.Org, req.Repo, checkout)
		} else {
			_, cloneErr = p.cli("gh", []string{"repo", "clone", req.Org + "/" + req.Repo, checkout}, net, ghEnv)
		}
		if cloneErr != nil || !p.isGitCheckout(checkout, local, gitEnv) {
			return out, prepareError("clone")
		}
		out.Cloned = true
	} else {
		return out, prepareError("not a git checkout")
	}

	if err := p.excludeJobDirs(checkout, local, gitEnv); err != nil {
		return out, err
	}
	if _, err := p.cli("git", []string{"-C", checkout, "fetch", "--prune", "origin"}, net, gitEnv); err != nil {
		return out, prepareError("fetch")
	}
	base := req.Base
	if base == "" {
		resolved, ok, err := p.remoteDefaultBase(checkout, net, gitEnv)
		if err != nil || !ok || !validBaseName(resolved) {
			return out, prepareError("unknown base")
		}
		base = resolved
	}
	baseSHA, err := p.cli("git", []string{"-C", checkout, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/" + base + "^{commit}"}, local, gitEnv)
	if err != nil {
		return out, prepareError("unknown base")
	}
	out.Base = base
	out.BaseSHA = strings.TrimSpace(baseSHA)

	if workspace {
		if err := p.prepareWorkspace(checkout, local, gitEnv); err != nil {
			return out, err
		}
		name, err := p.cli("git", []string{"-C", checkout, "rev-parse", "--abbrev-ref", "HEAD"}, local, gitEnv)
		if err != nil {
			return out, prepareError("workspace")
		}
		out.Dir = checkout
		out.Branch = strings.TrimSpace(name)
	} else {
		dir := filepath.Join(checkout, ".worktrees", "job-"+req.ID)
		if err := p.prepareWorktree(checkout, dir, branch, base, local, gitEnv); err != nil {
			return out, err
		}
		out.Dir = dir
		out.Branch = branch
	}

	if _, err := p.cli("git", []string{"-C", out.Dir, "push", "--dry-run", "--no-verify", "--porcelain", "origin", "HEAD:refs/heads/" + branch}, net, gitEnv); err != nil {
		return out, prepareError("push access")
	}
	return out, nil
}

// isGitCheckout reports whether path is a git checkout whose toplevel is
// exactly path (both sides canonicalized).
func (p Preparer) isGitCheckout(checkout string, timeout time.Duration, env platform.Env) bool {
	top, err := p.cli("git", []string{"-C", checkout, "rev-parse", "--show-toplevel"}, timeout, env)
	if err != nil {
		return false
	}
	return samePath(strings.TrimSpace(top), checkout)
}

// jobExcludeLines are the checkout paths the job family writes: the job
// state root and the job worktrees. They go to the checkout's local
// info/exclude (never a tracked .gitignore), so they never show as
// untracked changes, in particular to a later workspace-mode job.
var jobExcludeLines = []string{"/.herdr-soho/", "/.worktrees/"}

// excludeJobDirs appends each missing jobExcludeLines entry to
// <git-common-dir>/info/exclude; a retry adds nothing.
func (p Preparer) excludeJobDirs(checkout string, timeout time.Duration, env platform.Env) error {
	common, err := p.cli("git", []string{"-C", checkout, "rev-parse", "--path-format=absolute", "--git-common-dir"}, timeout, env)
	if err != nil {
		return prepareError("checkout")
	}
	file := filepath.Join(strings.TrimSpace(common), "info", "exclude")
	existing, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return prepareError("checkout")
	}
	present := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(string(existing), "\r\n", "\n"), "\n") {
		present[strings.TrimSpace(line)] = true
	}
	var add strings.Builder
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		add.WriteByte('\n')
	}
	missing := false
	for _, line := range jobExcludeLines {
		if !present[line] {
			add.WriteString(line + "\n")
			missing = true
		}
	}
	if !missing {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return prepareError("checkout")
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return prepareError("checkout")
	}
	if _, err = f.WriteString(add.String()); err != nil {
		_ = f.Close()
		return prepareError("checkout")
	}
	if err = f.Close(); err != nil {
		return prepareError("checkout")
	}
	return nil
}

// remoteDefaultBase parses `ref: refs/heads/<name>\tHEAD` from
// `git ls-remote --symref origin HEAD`.
func (p Preparer) remoteDefaultBase(checkout string, timeout time.Duration, env platform.Env) (string, bool, error) {
	output, err := p.cli("git", []string{"-C", checkout, "ls-remote", "--symref", "origin", "HEAD"}, timeout, env)
	if err != nil {
		return "", false, err
	}
	const prefix = "ref: refs/heads/"
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, "\tHEAD") {
			continue
		}
		return strings.TrimSuffix(strings.TrimPrefix(line, prefix), "\tHEAD"), true, nil
	}
	return "", false, nil
}

func validBaseName(base string) bool {
	return baseNamePattern.MatchString(base) && !strings.Contains(base, "..")
}

// prepareWorktree creates or reuses the job worktree. An existing path is
// reused only when it is its own toplevel (git -C walks up from a plain
// directory to the checkout), is on job/<id>, and shares the checkout's .git
// common dir; it is never deleted or reset. An existing local job branch is
// attached; otherwise the branch is created from origin/<base> without an
// upstream.
func (p Preparer) prepareWorktree(checkout, dir, branch, base string, timeout time.Duration, env platform.Env) error {
	if _, statErr := os.Stat(dir); statErr == nil {
		if !p.isGitCheckout(dir, timeout, env) {
			return prepareError("worktree")
		}
		name, err := p.cli("git", []string{"-C", dir, "rev-parse", "--abbrev-ref", "HEAD"}, timeout, env)
		if err != nil || strings.TrimSpace(name) != branch {
			return prepareError("worktree")
		}
		common, err := p.cli("git", []string{"-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir"}, timeout, env)
		if err != nil || !samePath(strings.TrimSpace(common), filepath.Join(checkout, ".git")) {
			return prepareError("worktree")
		}
		return nil
	} else if !os.IsNotExist(statErr) {
		return prepareError("worktree")
	}
	if _, err := p.cli("git", []string{"-C", checkout, "rev-parse", "--verify", "--quiet", "refs/heads/" + branch}, timeout, env); err == nil {
		if _, err := p.cli("git", []string{"-C", checkout, "worktree", "add", dir, branch}, timeout, env); err != nil {
			return prepareError("worktree")
		}
		return nil
	}
	if _, err := p.cli("git", []string{"-C", checkout, "worktree", "add", "--no-track", "-b", branch, dir, "refs/remotes/origin/" + base}, timeout, env); err != nil {
		return prepareError("worktree")
	}
	return nil
}

// prepareWorkspace validates the checkout for workspace mode: a locked
// index (index.lock in the git dir) and any dirty status (untracked
// non-ignored files included) are refused.
func (p Preparer) prepareWorkspace(checkout string, timeout time.Duration, env platform.Env) error {
	gitDir, err := p.cli("git", []string{"-C", checkout, "rev-parse", "--path-format=absolute", "--git-dir"}, timeout, env)
	if err != nil {
		return prepareError("workspace")
	}
	if _, statErr := os.Stat(filepath.Join(strings.TrimSpace(gitDir), "index.lock")); statErr == nil {
		return prepareError("checkout locked")
	}
	status, err := p.cli("git", []string{"-C", checkout, "status", "--porcelain"}, timeout, env)
	if err != nil {
		return prepareError("workspace")
	}
	if strings.TrimSpace(status) != "" {
		return prepareError("checkout dirty")
	}
	return nil
}

// samePath compares two paths after canonicalizing both sides with
// filepath.EvalSymlinks, falling back to a clean string comparison when a
// side cannot be canonicalized.
func samePath(a, b string) bool {
	canonicalA, aErr := filepath.EvalSymlinks(a)
	canonicalB, bErr := filepath.EvalSymlinks(b)
	if aErr != nil || bErr != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return canonicalA == canonicalB
}

func (p Preparer) cli(exe string, args []string, timeout time.Duration, env platform.Env) (string, error) {
	return runStep(exe, args, timeout, env)
}

// runStep runs one job subprocess through platform.RunCli (context deadline
// and WaitDelay), argv only. A not-found executable, a timeout, a nil status,
// or a non-zero status fails the step; the subprocess output never reaches
// the error.
func runStep(exe string, args []string, timeout time.Duration, env platform.Env) (string, error) {
	result := platform.RunCli(exe, args, platform.RunOptions{Env: env, TimeoutMs: int(timeout / time.Millisecond)})
	if result.NotFound || result.TimedOut || result.Status == nil || *result.Status != 0 {
		return "", errSubprocess
	}
	return result.Stdout, nil
}
