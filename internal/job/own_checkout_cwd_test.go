package job

import (
	"path/filepath"
	"testing"
)

// TestOwnCheckoutRefusesACwdOutsideTheTop: with GIT_DIR naming the job
// worktree's registered git dir and GIT_WORK_TREE its top level, the real
// git answers that worktree's valid paths from any cwd. The anchor accepts
// them from inside the worktree (the control) and refuses them from a
// directory outside it: git's answer alone does not place the caller in
// the job's directory.
func TestOwnCheckoutRefusesACwdOutsideTheTop(t *testing.T) {
	f := newOwnWorktreeFixture(t, "j1")
	f.installStore(t, f.storeRoot())
	env := f.env.Clone()
	env["GIT_DIR"] = filepath.Join(f.checkout, ".git", "worktrees", "job-j1")
	env["GIT_WORK_TREE"] = f.top
	f.expectStore(t, env, f.top, f.storeRoot())
	f.expectNil(t, env, realTmp(t))
}
