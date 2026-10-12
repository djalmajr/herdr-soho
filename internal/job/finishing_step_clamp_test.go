package job

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// finishing_step_clamp_test.go pins that the finishing clamp is applied to
// each real subprocess of a multi-command GitSync step, not once per step:
// a later command gets only what is left of the step when it starts.

// clampSlack is the slack over a step's bound the regressions allow: the
// platform WaitDelay is 1 s per killed command, plus 0.5 s for the process
// spawn, the git push and the store writes around it.
const clampSlack = 1500 * time.Millisecond

// stepDeadlineLimit models the supervisor's finCap for a GitSync used on its
// own: every timeout is the default or what is left of the step, never below
// one millisecond and rounded up to whole milliseconds.
func stepDeadlineLimit(deadline time.Time) func(time.Duration) time.Duration {
	return func(d time.Duration) time.Duration {
		remaining := time.Until(deadline)
		if remaining < time.Millisecond {
			remaining = time.Millisecond
		}
		remaining = (remaining + time.Millisecond - 1) / time.Millisecond * time.Millisecond
		if remaining < d {
			return remaining
		}
		return d
	}
}

// A slow first gh command followed by a hung second one stays inside the gh
// step: `gh pr create` receives the remainder at its own start, not the whole
// step again.
func TestFinishingGhStepBoundsTheSecondCommandByTheRemainder(t *testing.T) {
	for _, tc := range []struct {
		name  string
		step  time.Duration
		delay int
	}{
		{"step 2s, list 1.8s", 2 * time.Second, 1800},
		{"step 5s, list 4s", 5 * time.Second, 4000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := []fakecli.Rule{
				{Argv: ghListArgv(), Stdout: "[]\n", Delay: tc.delay},
				{Argv: ghCreateArgv("feat: first", supGitPRBody), Stdout: gitSyncPRURL + "\n", Delay: 60000},
			}
			f := newSupGitFixture(t, rules)
			f.job.commitIn(t, "feat: first")
			finishingRealClock(f, finishingLimits{Budget: 60 * time.Second, Git: 30 * time.Second, GH: tc.step})
			exit, err, elapsed := finishDone(t, f)
			t.Logf("gh step %v, list %dms, create hangs: terminal in %v", tc.step, tc.delay, elapsed)
			if elapsed > tc.step+clampSlack {
				t.Errorf("terminal path took %v, want at most the %v gh step + %v", elapsed, tc.step, clampSlack)
			}
			requireBudgetFailed(t, f, exit, err, "the draft pull request")
			if got := len(f.job.base.ghCalls()); got != 2 {
				t.Errorf("gh calls = %d, want list and create (the scenario never reached create otherwise)", got)
			}
		})
	}
}

// A slow early checkpoint command followed by a hung commit stays inside the
// step: `git commit` receives the remainder at its own start. The checkpoint
// runs on its own GitSync whose Limit models a step deadline, with a fake git
// that delays `add`, reports a staged diff and hangs on `commit`.
func TestCheckpointBoundsTheLaterCommandsByTheStepRemainder(t *testing.T) {
	for _, tc := range []struct {
		name  string
		step  time.Duration
		delay int
		// hangDiff hangs `diff --cached` (and lets `commit` return at once)
		// instead of hanging `commit`.
		hangDiff bool
	}{
		{"step 2s, add 1.8s, commit hangs", 2 * time.Second, 1800, false},
		{"step 5s, add 4s, commit hangs", 5 * time.Second, 4000, false},
		{"step 2s, add 1.8s, diff hangs", 2 * time.Second, 1800, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fakeDir := t.TempDir()
			diffRule := fakecli.Rule{Argv: []string{"-C", dir, "diff", "--cached", "--quiet"}, Code: 1}
			commitRule := fakecli.Rule{Argv: []string{"-C", dir, "commit", "--no-verify", "-m", "wip(job): checkpoint"}}
			if tc.hangDiff {
				diffRule.Delay = 60000
			} else {
				commitRule.Delay = 60000
			}
			rules := []fakecli.Rule{
				{Argv: []string{"-C", dir, "add", "-A"}, Delay: tc.delay},
				diffRule,
				commitRule,
			}
			if _, err := fakecli.Install(t, fakeDir, "git", rules); err != nil {
				t.Fatalf("install the fake git: %v", err)
			}
			g := &GitSync{Env: envFromPairs(t, fakecli.Env(nil, fakeDir)), Dir: dir, Branch: gitSyncBranch, Base: "main", Repo: gitSyncRepo}
			start := time.Now()
			g.Limit = stepDeadlineLimit(start.Add(tc.step))
			committed, err := g.Checkpoint()
			elapsed := time.Since(start)
			t.Logf("checkpoint step %v, add %dms, hang on diff=%v: returned in %v (committed=%v err=%v)", tc.step, tc.delay, tc.hangDiff, elapsed, committed, err)
			// A killed diff counts as a staged diff and the commit then runs on
			// the 1 ms the step has left, so only the hung commit has a fixed
			// result.
			if !tc.hangDiff && (err == nil || committed) {
				t.Errorf("Checkpoint = %v, %v, want the checkpoint error", committed, err)
			}
			if elapsed < time.Duration(tc.delay)*time.Millisecond {
				t.Errorf("returned in %v, before the %dms add: the scenario did not run", elapsed, tc.delay)
			}
			if elapsed > tc.step+clampSlack {
				t.Errorf("Checkpoint took %v, want at most the %v step + %v", elapsed, tc.step, clampSlack)
			}
			calls, err := fakecli.ReadCalls(filepath.Join(fakeDir, "git.calls.jsonl"))
			if want := 3; err != nil || len(calls) < want-1 || !tc.hangDiff && len(calls) != want {
				t.Errorf("fake git calls = %d (%v), want add, diff and (unless diff hung) commit", len(calls), err)
			}
		})
	}
}
