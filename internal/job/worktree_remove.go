// worktree_remove.go is the end-of-job removal of the job's own worktree:
// it removes <checkout>/.worktrees/job-<id> only when it is the job's
// worktree and the report facts say the work is clean and pushed, and it
// converges to removed when the directory is already gone. It never
// deletes the branch job/<id>, never runs git with --force, never runs
// git stash, and touches nothing outside the worktree directory. Every
// subprocess is the real git on the current environment's PATH, through
// runStep with the same bounded local timeout as gitsync.go's git steps.

package job

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// The removeOwnedWorktree motivos, one per refusal reason; a successful
// removal (and the already-gone convergence) return the empty motivo.
const (
	WorktreeRemoveNotOwned    = "not_owned"
	WorktreeRemoveDirty       = "dirty"
	WorktreeRemoveNotPushed   = "not_pushed"
	WorktreeRemoveCheckFailed = "check_failed"
	WorktreeRemoveRefused     = "remove_refused"
)

// removeOwnedWorktree removes the job worktree when it is the job's own
// and the report facts say the work is clean and pushed:
//
//  1. the shape: checkout and dir are absolute and dir is exactly
//     <checkout>/.worktrees/job-<id>, which also excludes workspace mode,
//     whose dir is the checkout itself;
//  2. the convergence: a dir that is already absent — with the worktree
//     registration still there or already cleaned — returns removed=true
//     without error, whatever the facts say;
//  3. the ownership, the same checks prepare.go runs to reuse a worktree:
//     dir is its own toplevel, it is on the branch job/<id>, and it shares
//     the checkout's .git common dir, which is the checkout's own toplevel;
//  4. the facts gate: the head fact is present (a missing head is a read
//     error and check_failed), the tree is clean (else dirty), and the
//     head equals the remote head (else not_pushed);
//  5. the removal: `git -C <checkout> worktree remove <dir>` without
//     --force under the local timeout; a git refusal is remove_refused.
//
// Any doubt — a failed shape, a non-directory entry, a subprocess
// failure, a wrong branch — returns removed=false with not_owned. The
// branch job/<id> is never deleted and nothing outside dir is touched.
func removeOwnedWorktree(checkout, dir, id string, facts ReportFacts) (removed bool, motivo string) {
	if !filepath.IsAbs(checkout) || !filepath.IsAbs(dir) || filepath.Clean(dir) != filepath.Join(checkout, ".worktrees", "job-"+id) {
		return false, WorktreeRemoveNotOwned
	}
	if !exactDirectory(dir) {
		if _, err := os.Lstat(dir); os.IsNotExist(err) {
			// The directory is already gone, with the registration still
			// there or already cleaned: the removal has converged.
			return true, ""
		}
		// A file, a symlink, or a stat failure: not the job's worktree.
		return false, WorktreeRemoveNotOwned
	}
	env := platform.EnvFromOS()
	env["GIT_TERMINAL_PROMPT"] = "0"
	if !jobWorktreeOwned(checkout, dir, "job/"+id, env) {
		return false, WorktreeRemoveNotOwned
	}
	if facts.Head == "" {
		return false, WorktreeRemoveCheckFailed
	}
	if !facts.Clean {
		return false, WorktreeRemoveDirty
	}
	if facts.Head != facts.HeadRemoto {
		return false, WorktreeRemoveNotPushed
	}
	if _, err := runStep("git", []string{"-C", checkout, "worktree", "remove", dir}, defaultLocalSyncTimeout, env); err != nil {
		return false, WorktreeRemoveRefused
	}
	return true, ""
}

// jobWorktreeOwned applies the prepare.go worktree-reuse checks to dir: it
// is its own toplevel (the isGitCheckout query), it is on branch, and it
// shares the checkout's .git common dir; the checkout itself must be a
// checkout whose toplevel is exactly it. Every query is the real git with
// the 60 s local timeout, and any failure — including a subprocess
// failure — reports false.
func jobWorktreeOwned(checkout, dir, branch string, env platform.Env) bool {
	top, err := runStep("git", []string{"-C", dir, "rev-parse", "--show-toplevel"}, defaultLocalSyncTimeout, env)
	if err != nil || !samePath(strings.TrimSpace(top), dir) {
		return false
	}
	name, err := runStep("git", []string{"-C", dir, "rev-parse", "--abbrev-ref", "HEAD"}, defaultLocalSyncTimeout, env)
	if err != nil || strings.TrimSpace(name) != branch {
		return false
	}
	common, err := runStep("git", []string{"-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir"}, defaultLocalSyncTimeout, env)
	if err != nil || !samePath(strings.TrimSpace(common), filepath.Join(checkout, ".git")) {
		return false
	}
	checkoutTop, err := runStep("git", []string{"-C", checkout, "rev-parse", "--show-toplevel"}, defaultLocalSyncTimeout, env)
	if err != nil || !samePath(strings.TrimSpace(checkoutTop), checkout) {
		return false
	}
	return true
}
