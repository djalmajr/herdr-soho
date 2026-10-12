package job

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// finishing_timeout_config_test.go pins the configurable, persisted
// finishing budget: `job start` records the machine's job_finishing_timeout
// as state.json finishing_timeout_min in the same write as
// blocked_timeout_min, and the supervisor (first process and every restart)
// counts the terminal procedure's budget from that recorded value only,
// with 10 minutes for a state that predates it, without rewriting it.

// finishingRetrySpan runs sup over a job already finishing with a done
// intent whose terminal write always fails, so the supervisor retries it
// until the finishing budget is spent, and returns the monotonic span of
// those retries: what the budget had left when the process started (plus or
// minus one poll).
func finishingRetrySpan(t *testing.T, clock *nsClock, sup *Supervisor) time.Duration {
	t.Helper()
	rec := failEveryStateWrite(t, errors.New("injected"), func() int64 { return clock.mono }, func(p State) bool { return p.Status == StatusDone })
	if _, err := sup.Run(); err == nil {
		t.Fatal("Run succeeded under a persistent terminal write failure")
	}
	if rec.n < 2 {
		t.Fatalf("the terminal write was attempted %d times, want a retried budget", rec.n)
	}
	return time.Duration(rec.last - rec.first)
}

// recordFinishing puts the job in finishing with a done intent anchored
// anchorAgo before the supervisor's now, and records the finishing budget
// (0 leaves the field absent, a state that predates it).
func recordFinishing(t *testing.T, store *Store, clock *nsClock, minutes int, anchorAgo time.Duration) {
	t.Helper()
	if _, err := store.Record("job-1", func(st *State) {
		st.TerminalIntent = &TerminalIntent{Status: StatusDone}
		st.FinishingStartedAt = clock.now.Add(-anchorAgo).Format(time.RFC3339Nano)
		st.FinishingTimeoutMin = minutes
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition("job-1", StatusFinishing); err != nil {
		t.Fatal(err)
	}
}

func requireSpan(t *testing.T, got, want time.Duration) {
	t.Helper()
	// One poll of the fixture (10 s) of slack, like the sibling finishing
	// budget tests.
	if got < want-10*time.Second || got > want+10*time.Second {
		t.Errorf("terminal write retried over %v, want the %v the recorded budget leaves (+- one poll)", got, want)
	}
}

// A job recorded with N minutes gets a budget of N minutes on its first
// run: shorter and longer than the built-in 10.
func TestFinishingTimeoutRecordedValueIsTheBudgetOfTheFirstRun(t *testing.T) {
	for _, minutes := range []int{3, 25} {
		t.Run(time.Duration(minutes*int(time.Minute)).String(), func(t *testing.T) {
			var friction []string
			store, _, clock, _, _, sup := failBlockedSetup(t, &friction)
			recordFinishing(t, store, clock, minutes, 0)
			requireSpan(t, finishingRetrySpan(t, clock, sup), time.Duration(minutes)*time.Minute)
		})
	}
}

// The recorded value is the only source: a restart after the machine key
// was edited to another valid value still counts the recorded N, from the
// persisted anchor.
func TestFinishingTimeoutRestartKeepsTheRecordedValueAfterTheMachineKeyChanges(t *testing.T) {
	var friction []string
	store, _, clock, _, _, _ := failBlockedSetup(t, &friction)
	// Recorded 3 minutes, 2 minutes already spent from the anchor: 1 minute
	// is left. The built-in 10 would leave 8, the edited key (20) 18.
	recordFinishing(t, store, clock, 3, 2*time.Minute)

	env := platform.Env{"HOME": t.TempDir(), "XDG_CONFIG_HOME": t.TempDir()}
	userFile := platform.UserConfigPath("linux", env)
	writeTeamFile(t, userFile, "job_finishing_timeout=20\n")
	machine, err := LoadMachine("linux", env)
	if err != nil || machine.FinishingTimeoutMin != 20 {
		t.Fatalf("the edited machine key reads %d (err %v), want 20: the fixture did not change it", machine.FinishingTimeoutMin, err)
	}

	// The restart is the production constructor over the recorded job; its
	// collaborators are swapped for fakes the way the sibling fixtures do.
	sup, err := NewSupervisor(store, "job-1", env, "/nonexistent/herdr-soho", func(message string) { friction = append(friction, message) })
	if err != nil {
		t.Fatal(err)
	}
	sup.Clock, sup.Ops, sup.Herdr, sup.Poll = clock, &fakeTeam{spawnName: "orch-1"}, &fakeHerdr{}, 10*time.Second
	sup.Git = nil
	requireSpan(t, finishingRetrySpan(t, clock, sup), time.Minute)
	if st := supState(t, store); st.FinishingTimeoutMin != 3 {
		t.Errorf("finishing_timeout_min = %d after the restart, want the recorded 3", st.FinishingTimeoutMin)
	}
}

// A state without the field (a job started before it existed) gets the
// built-in 10 minutes at read time and is not rewritten: state.json has no
// finishing_timeout_min afterwards.
func TestFinishingTimeoutLegacyStateUsesTenMinutesAndIsNotRewritten(t *testing.T) {
	var friction []string
	store, jobDir, clock, _, _, sup := failBlockedSetup(t, &friction)
	recordFinishing(t, store, clock, 0, 4*time.Minute)
	requireSpan(t, finishingRetrySpan(t, clock, sup), 6*time.Minute)
	raw, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "finishing_timeout_min") {
		t.Errorf("state.json = %s, want no finishing_timeout_min (the legacy state is not rewritten)", raw)
	}
	if st := supState(t, store); st.FinishingTimeoutMin != 0 {
		t.Errorf("FinishingTimeoutMin = %d, want 0", st.FinishingTimeoutMin)
	}
}

// The test seam keeps winning: a non-zero FinishingLimits.Budget over a
// recorded value.
func TestFinishingTimeoutLimitsBudgetWinsOverTheRecordedValue(t *testing.T) {
	var friction []string
	store, _, clock, _, _, sup := failBlockedSetup(t, &friction)
	recordFinishing(t, store, clock, 25, 0)
	sup.FinishingLimits = finishingLimits{Budget: 60 * time.Second}
	requireSpan(t, finishingRetrySpan(t, clock, sup), time.Minute)
}

// The per-step bounds stay internal constants capped by the selected
// budget: a 1 minute budget caps the 5 minute git step and the 2 minute gh
// step; a 12 minute budget leaves both at their own bound.
func TestFinishingTimeoutRecordedBudgetCapsTheStepBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		minutes int
		step    string
		bound   time.Duration
		want    time.Duration
	}{
		{"1 minute caps the git step", 1, "the final push", defaultFinishingGitStep, time.Minute},
		{"1 minute caps the gh step", 1, "the draft pull request", defaultFinishingGHStep, time.Minute},
		{"12 minutes leave the git step at 5", 12, "the final push", defaultFinishingGitStep, defaultFinishingGitStep},
		{"12 minutes leave the gh step at 2", 12, "the draft pull request", defaultFinishingGHStep, defaultFinishingGHStep},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var friction []string
			store, _, clock, _, _, sup := failBlockedSetup(t, &friction)
			recordFinishing(t, store, clock, tc.minutes, 0)
			sup.ensureFinishingBase()
			if !sup.beginFinStep(tc.step, tc.bound) {
				t.Fatalf("beginFinStep(%q) refused with the full budget left; friction %q", tc.step, friction)
			}
			if got := time.Duration(sup.stepDeadline - clock.mono); got != tc.want {
				t.Errorf("%s step deadline is %v from now, want %v", tc.step, got, tc.want)
			}
			if got := sup.finRemaining(); got != time.Duration(tc.minutes)*time.Minute {
				t.Errorf("finRemaining = %v, want the recorded %d minutes", got, tc.minutes)
			}
		})
	}
}

// startStateWrites records the pending state of every state.json write.
func startStateWrites(t *testing.T) *[]State {
	t.Helper()
	var writes []State
	previous := renameAtomic
	renameAtomic = func(from, to string) error {
		if filepath.Base(to) == "state.json" {
			if raw, err := os.ReadFile(from); err == nil {
				var pending State
				if json.Unmarshal(raw, &pending) == nil {
					writes = append(writes, pending)
				}
			}
		}
		return previous(from, to)
	}
	t.Cleanup(func() { renameAtomic = previous })
	return &writes
}

// `job start` records the machine's finishing budget with the run facts: in
// the same atomic write as blocked_timeout_min, so no state.json write holds
// one of the two without the other.
func TestStartRecordsFinishingTimeoutInTheSameWriteAsBlockedTimeout(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	f.machine.BlockedTimeoutMin = 720
	f.machine.FinishingTimeoutMin = 25
	fake := startFake()
	stateRoot := t.TempDir()
	var friction []string
	s := startStarter(t, f, fake, stateRoot, &friction)
	writes := startStateWrites(t)

	if _, store, err := s.Start(startRequest("main", briefA)); err != nil {
		t.Fatalf("Start: %v", err)
	} else {
		snap, err := store.Snapshot("job-1")
		if err != nil {
			t.Fatal(err)
		}
		if st := snap.State; st.FinishingTimeoutMin != 25 || st.BlockedTimeoutMin != 720 {
			t.Fatalf("recorded finishing=%d blocked=%d, want 25 and 720", st.FinishingTimeoutMin, st.BlockedTimeoutMin)
		}
		raw, err := os.ReadFile(filepath.Join(store.Root, "jobs", "job-1", "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"finishing_timeout_min": 25`) && !strings.Contains(string(raw), `"finishing_timeout_min":25`) {
			t.Errorf("state.json = %s, want the finishing_timeout_min field", raw)
		}
	}
	both := 0
	for i, w := range *writes {
		if (w.BlockedTimeoutMin > 0) != (w.FinishingTimeoutMin > 0) {
			t.Errorf("state.json write %d holds blocked=%d finishing=%d: the two values must be recorded in one write", i, w.BlockedTimeoutMin, w.FinishingTimeoutMin)
		}
		if w.BlockedTimeoutMin > 0 && w.FinishingTimeoutMin > 0 {
			both++
		}
	}
	if both == 0 {
		t.Errorf("no state.json write recorded both values (%d writes observed)", len(*writes))
	}
}

// The invalid machine value is refused before a Starter exists (`job start`
// loads the machine first): LoadMachine fails with exit 2 and the exact
// text, so the Starter, and with it every pane and actor call, never runs.
func TestStartInvalidFinishingTimeoutFailsTheMachineLoad(t *testing.T) {
	f := newMachineFix(t)
	writeTeamFile(t, platform.UserConfigPath("linux", f.env), "job_finishing_timeout=1441\n")
	_, err := LoadMachine("linux", f.env)
	var exit *platform.ExitError
	if !errors.As(err, &exit) || exit.Code != 2 || exit.Msg != "job: invalid job_finishing_timeout '1441'" {
		t.Fatalf("LoadMachine = %v, want exit 2 \"job: invalid job_finishing_timeout '1441'\"", err)
	}
}
