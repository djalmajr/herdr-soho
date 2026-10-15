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
//     head equals the remote head (else not_pushed); then the live tree
//     is read again, because the facts were read before the job's agents
//     were released: a head that moved since the facts is not_pushed (the
//     new head was never proven pushed), a tree no longer clean is dirty,
//     and a failed read is check_failed;
//  5. the removal: `git -C <checkout> worktree remove <dir>` without
//     --force under the local timeout; a git refusal is remove_refused,
//     except when git removed the work and its registration but could
//     not delete the directory itself (on Windows, the job's root pane
//     still has it as its current directory): an empty directory the
//     checkout no longer lists as a worktree is the removal done
//     (emptyLeftover), and its deletion is tried once more.
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
	if emptyLeftover(checkout, dir, env) && acceptLeftover(dir) {
		// What an earlier removal left behind: the directory alone.
		return true, ""
	}
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
	head, err := runStep("git", []string{"-C", dir, "rev-parse", "HEAD"}, defaultLocalSyncTimeout, env)
	if err != nil {
		return false, WorktreeRemoveCheckFailed
	}
	if strings.TrimSpace(head) != facts.Head {
		return false, WorktreeRemoveNotPushed
	}
	status, err := runStep("git", []string{"-C", dir, "status", "--porcelain"}, defaultLocalSyncTimeout, env)
	if err != nil {
		return false, WorktreeRemoveCheckFailed
	}
	if strings.TrimSpace(status) != "" {
		return false, WorktreeRemoveDirty
	}
	if _, err := runStep("git", []string{"-C", checkout, "worktree", "remove", dir}, defaultLocalSyncTimeout, env); err != nil {
		if emptyLeftover(checkout, dir, env) && acceptLeftover(dir) {
			return true, ""
		}
		return false, WorktreeRemoveRefused
	}
	return true, ""
}

// removeLeftoverDir deletes the leftover directory; tests replace it to
// put content in the directory between the check and the deletion.
var removeLeftoverDir = os.Remove

// acceptLeftover deletes the empty leftover directory and reports whether
// it counts as removed: deleted, already gone, or — when the deletion
// fails, as for a directory still held on Windows — still an empty real
// directory on a fresh read. Content that appeared in the meantime, or a
// read that cannot tell, refuses the convergence.
func acceptLeftover(dir string) bool {
	err := removeLeftoverDir(dir)
	if err == nil || os.IsNotExist(err) {
		return true
	}
	if !exactDirectory(dir) {
		return false
	}
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) == 0
}

// emptyLeftover reports whether dir is an empty real directory that the
// checkout no longer lists as a worktree (`git worktree list
// --porcelain`): git removes a worktree's files and its registration
// even when it cannot delete the directory itself, and nothing of the
// job's work is left in such a directory. A read failure, a non-empty
// directory, a listed path, or a failed list reports false.
func emptyLeftover(checkout, dir string, env platform.Env) bool {
	if !exactDirectory(dir) {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		return false
	}
	out, err := runStep("git", []string{"-C", checkout, "worktree", "list", "--porcelain"}, defaultLocalSyncTimeout, env)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		path, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "worktree ")
		if ok && samePath(filepath.FromSlash(path), dir) {
			return false
		}
	}
	return true
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
