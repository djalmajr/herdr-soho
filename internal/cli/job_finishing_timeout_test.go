package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

// setFinishingTimeout appends job_finishing_timeout to the user file the
// job fixture wrote.
func setFinishingTimeout(t *testing.T, ee *jobStartEE, value string) {
	t.Helper()
	userFile := platform.UserConfigPath(platform.Current(), ee.f.env)
	body, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, append(body, []byte("job_finishing_timeout="+value+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
}

// An invalid job_finishing_timeout makes `job start` exit 2 with the exact
// message before any pane or actor is spawned: no herdr call, no gh call,
// no worktree, no job state.
func TestJobStartInvalidFinishingTimeoutExitsTwoBeforeAnySpawn(t *testing.T) {
	for _, raw := range []string{"0", "1441", "10m", "1.5"} {
		t.Run(raw, func(t *testing.T) {
			ee := newJobStartEE(t)
			setFinishingTimeout(t, ee, raw)
			code, out, errOut := ee.start(t, ee.f.briefFile(t, jobStartBrief))
			want := "soho: job: invalid job_finishing_timeout '" + raw + "'"
			if code != 2 || out != "" || strings.TrimSpace(errOut) != want {
				t.Fatalf("code=%d out=%q err=%q, want exit 2 with exactly %q", code, out, errOut, want)
			}
			if calls := ee.herdrCalls(t); len(calls) != 0 {
				t.Errorf("herdr calls = %+v, want none before the refusal", calls)
			}
			if _, err := os.Stat(ee.checkout + "/.worktrees/job-job-1"); err == nil {
				t.Errorf("a worktree was created before the refusal")
			}
			if _, err := os.Stat(ee.checkout + "/.soho/jobs/job-1"); err == nil {
				t.Errorf("job state was created before the refusal")
			}
		})
	}
}

// A valid value is recorded in state.json by the real start, next to the
// blocked budget.
func TestJobStartRecordsFinishingTimeout(t *testing.T) {
	ee := newJobStartEE(t)
	setFinishingTimeout(t, ee, "25")
	code, out, errOut := ee.start(t, ee.f.briefFile(t, jobStartBrief))
	if code != 0 || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	snap, err := ee.f.store(t, ee.checkout).Snapshot("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.FinishingTimeoutMin != 25 || snap.State.BlockedTimeoutMin != 1440 {
		t.Fatalf("recorded finishing=%d blocked=%d, want 25 and 1440", snap.State.FinishingTimeoutMin, snap.State.BlockedTimeoutMin)
	}
}
