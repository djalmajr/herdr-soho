// supervise_spawn_recover_test.go covers the job orchestrator spawn
// recovery: the spawn intent recorded before the spawn, the roster
// adoption on a restart, the fail-closed decisions (no second spawn, no
// release of an unproven pane), the single EnsureJobLane across the
// crash, and the worker_spawned backfill of a crash between the
// Orchestrator record and the event append.
package job

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

const recoverReport = "# Report — job\n\n- Item 1 [done] did the thing\n"

// supSupervisorWithHerdr is supSupervisor with the Herdr workspace list
// adapter installed for the recovery reads.
func supSupervisorWithHerdr(store *Store, clock *fakeClock, team *fakeTeam, friction *[]string, herdr *fakeHerdr) *Supervisor {
	sup := supSupervisor(store, clock, team, friction)
	sup.Herdr = herdr
	return sup
}

// crashSpawn runs the supervisor over the fixture and recovers the crash
// panic the fake team throws on the spawn; it returns the store state the
// crash left, and the caller asserts the intent is recorded without the
// name.
func crashSpawn(t *testing.T, store *Store, clock *fakeClock, team *fakeTeam, friction *[]string) {
	t.Helper()
	team.spawnPanic = true
	sup := supSupervisor(store, clock, team, friction)
	func() {
		defer func() {
			if r := recover(); !errors.Is(r.(error), crashBetweenSpawnAndRecord) {
				panic(r)
			}
		}()
		if _, err := sup.Run(); err != nil {
			t.Errorf("first Run: %v", err)
		}
	}()
}

// assertRecoveryFailed checks the fail-closed outcome: the job is failed
// with the fixed motivo and the Herdr exit code, no new spawn ran and
// nothing was dispatched.
func assertRecoveryFailed(t *testing.T, store *Store, jobDir string, team *fakeTeam, herdr *fakeHerdr) {
	t.Helper()
	st := supState(t, store)
	if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "job: cannot establish the job orchestrator" {
		t.Fatalf("state = %+v, want failed with the recovery motivo", st)
	}
	if st.Orchestrator != "" {
		t.Fatalf("the fail-closed records no orchestrator: %q", st.Orchestrator)
	}
	if team.spawnCalls != 1 {
		t.Fatalf("spawn calls = %d, want 1 (the crashed spawn only, no new spawn)", team.spawnCalls)
	}
	if len(team.dispatches) != 0 {
		t.Fatalf("dispatches = %v, want none", team.dispatches)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := eventTipes(events); !reflect.DeepEqual(got, []string{"accepted", "failure", "terminal", "cleanup"}) {
		t.Fatalf("event types = %v", got)
	}
	if terminal := events[len(events)-2]; terminal.Refs["exit"] != "4" {
		t.Fatalf("terminal refs = %+v, want exit 4", terminal.Refs)
	}
	// herdr is non-nil only when the subtest asserts the list was never
	// consulted (the fail-closed decided before the list read).
	if herdr != nil && herdr.ListReads != 0 {
		t.Fatalf("list reads = %d, want 0 (the recovery never reaches the list)", herdr.ListReads)
	}
}

func TestSuperviseSpawnRecoverZeroCandidatesSinglePaneSpawns(t *testing.T) {
	store, jobDir, clock, team := supFixture(t)
	// The roster has no job-orchestrator row and the workspace holds only
	// the supervisor's own root pane: no orchestrator pane exists, so the
	// restart spawns as normal — exactly one new spawn.
	herdr := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w1", Label: "job", PaneCount: 1}}}
	report := writeOrchestratorReport(t, recoverReport)
	team.statusScript = []fakeStatus{{State: "done", Report: report}}
	var friction []string
	crashSpawn(t, store, clock, team, &friction)
	if st := supState(t, store); st.OrchestratorSpawnAt == "" || st.Orchestrator != "" || st.Status != StatusRunning {
		t.Fatalf("after the crash the state = %+v", st)
	}
	sup := supSupervisorWithHerdr(store, clock, team, &friction, herdr)
	exit, err := sup.Run()
	if err != nil || exit != 0 {
		t.Fatalf("second Run: %d %v, want 0", exit, err)
	}
	if team.rosterCalls != 1 || herdr.ListReads != 1 {
		t.Fatalf("roster calls = %d list reads = %d, want 1/1", team.rosterCalls, herdr.ListReads)
	}
	if team.spawnCalls != 2 {
		t.Fatalf("spawn calls = %d, want 2 (one per run)", team.spawnCalls)
	}
	if len(team.dispatches) != 1 {
		t.Fatalf("dispatches = %v, want exactly one", team.dispatches)
	}
	if st := supState(t, store); st.Status != StatusDone || st.Orchestrator != "orch-1" {
		t.Fatalf("state = %+v", st)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := eventTipes(events); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned", "worker_done", "terminal", "cleanup"}) {
		t.Fatalf("event types = %v", got)
	}
	if n := countTipe(events, "worker_spawned"); n != 1 {
		t.Fatalf("worker_spawned count = %d, want 1", n)
	}
}

func TestSuperviseSpawnRecoverFailsClosed(t *testing.T) {
	t.Run("zero candidates with two panes fails closed without spawning", func(t *testing.T) {
		store, jobDir, clock, team := supFixture(t)
		// The done status is scripted so a mutant that fails open (spawns or
		// adopts the foreign orchestrator) ends the job and fails the state
		// assertion below instead of running into the unscripted watch.
		herdr := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w1", Label: "job", PaneCount: 2}}}
		report := writeOrchestratorReport(t, recoverReport)
		team.statusScript = []fakeStatus{{State: "done", Report: report}}
		var friction []string
		crashSpawn(t, store, clock, team, &friction)
		sup := supSupervisorWithHerdr(store, clock, team, &friction, herdr)
		exit, err := sup.Run()
		if err != nil || exit != ExitHerdr {
			t.Fatalf("second Run: %d %v, want %d", exit, err, ExitHerdr)
		}
		if herdr.ListReads != 1 {
			t.Fatalf("list reads = %d, want 1 (the single-pane proof was consulted)", herdr.ListReads)
		}
		assertRecoveryFailed(t, store, jobDir, team, nil)
	})

	t.Run("a workspace missing from the list fails closed without spawning", func(t *testing.T) {
		store, jobDir, clock, team := supFixture(t)
		herdr := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w2", Label: "other", PaneCount: 1}}}
		var friction []string
		crashSpawn(t, store, clock, team, &friction)
		sup := supSupervisorWithHerdr(store, clock, team, &friction, herdr)
		exit, err := sup.Run()
		if err != nil || exit != ExitHerdr {
			t.Fatalf("second Run: %d %v, want %d", exit, err, ExitHerdr)
		}
		if herdr.ListReads != 1 {
			t.Fatalf("list reads = %d, want 1 (the list was consulted)", herdr.ListReads)
		}
		assertRecoveryFailed(t, store, jobDir, team, nil)
	})

	t.Run("a list error fails closed without spawning", func(t *testing.T) {
		store, jobDir, clock, team := supFixture(t)
		herdr := &fakeHerdr{ListErr: errors.New("herdr workspace list failed")}
		var friction []string
		crashSpawn(t, store, clock, team, &friction)
		sup := supSupervisorWithHerdr(store, clock, team, &friction, herdr)
		exit, err := sup.Run()
		if err != nil || exit != ExitHerdr {
			t.Fatalf("second Run: %d %v, want %d", exit, err, ExitHerdr)
		}
		if herdr.ListReads != 1 {
			t.Fatalf("list reads = %d, want 1 (the failing list read was consulted)", herdr.ListReads)
		}
		assertRecoveryFailed(t, store, jobDir, team, nil)
	})

	t.Run("a roster error fails closed without consulting the list or spawning", func(t *testing.T) {
		store, jobDir, clock, team := supFixture(t)
		team.rosterErr = errors.New("roster failed")
		herdr := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w1", Label: "job", PaneCount: 1}}}
		var friction []string
		crashSpawn(t, store, clock, team, &friction)
		sup := supSupervisorWithHerdr(store, clock, team, &friction, herdr)
		exit, err := sup.Run()
		if err != nil || exit != ExitHerdr {
			t.Fatalf("second Run: %d %v, want %d", exit, err, ExitHerdr)
		}
		assertRecoveryFailed(t, store, jobDir, team, herdr)
	})

	t.Run("no Herdr adapter fails closed without spawning", func(t *testing.T) {
		store, jobDir, clock, team := supFixture(t)
		var friction []string
		crashSpawn(t, store, clock, team, &friction)
		sup := supSupervisor(store, clock, team, &friction)
		exit, err := sup.Run()
		if err != nil || exit != ExitHerdr {
			t.Fatalf("second Run: %d %v, want %d", exit, err, ExitHerdr)
		}
		assertRecoveryFailed(t, store, jobDir, team, nil)
	})

	t.Run("two candidates fail closed without spawning", func(t *testing.T) {
		store, jobDir, clock, team := supFixture(t)
		team.rosterRows = []rosterRow{
			{Name: "orch-a", Role: "job-orchestrator", Pane: "w1:p2", State: "working"},
			{Name: "orch-b", Role: "job-orchestrator", Pane: "w1:p3", State: "idle"},
		}
		herdr := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w1", Label: "job", PaneCount: 2}}}
		var friction []string
		crashSpawn(t, store, clock, team, &friction)
		sup := supSupervisorWithHerdr(store, clock, team, &friction, herdr)
		exit, err := sup.Run()
		if err != nil || exit != ExitHerdr {
			t.Fatalf("second Run: %d %v, want %d", exit, err, ExitHerdr)
		}
		assertRecoveryFailed(t, store, jobDir, team, herdr)
	})

	t.Run("a candidate in another workspace is not adopted", func(t *testing.T) {
		store, jobDir, clock, team := supFixture(t)
		team.rosterRows = []rosterRow{{Name: "orch-x", Role: "job-orchestrator", Pane: "w9:p2", State: "working"}}
		// Two panes: even though the roster read succeeds, the recovery
		// cannot prove the job workspace's pane state and fails closed
		// instead of adopting the foreign orchestrator. The done status is
		// scripted so a mutant that adopts it fails the state assertion
		// instead of running into the unscripted watch.
		herdr := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w1", Label: "job", PaneCount: 2}}}
		report := writeOrchestratorReport(t, recoverReport)
		team.statusScript = []fakeStatus{{State: "done", Report: report}}
		var friction []string
		crashSpawn(t, store, clock, team, &friction)
		sup := supSupervisorWithHerdr(store, clock, team, &friction, herdr)
		exit, err := sup.Run()
		if err != nil || exit != ExitHerdr {
			t.Fatalf("second Run: %d %v, want %d", exit, err, ExitHerdr)
		}
		if herdr.ListReads != 1 {
			t.Fatalf("list reads = %d, want 1 (the foreign row is no candidate; the list decides)", herdr.ListReads)
		}
		assertRecoveryFailed(t, store, jobDir, team, nil)
	})

	t.Run("a gone candidate is not adopted", func(t *testing.T) {
		store, jobDir, clock, team := supFixture(t)
		team.rosterRows = []rosterRow{{Name: "orch-x", Role: "job-orchestrator", Pane: "w1:p2", State: "gone"}}
		// As above: the done status keeps a mutant that ignores the gone
		// state on the state assertion instead of the unscripted watch.
		herdr := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w1", Label: "job", PaneCount: 2}}}
		report := writeOrchestratorReport(t, recoverReport)
		team.statusScript = []fakeStatus{{State: "done", Report: report}}
		var friction []string
		crashSpawn(t, store, clock, team, &friction)
		sup := supSupervisorWithHerdr(store, clock, team, &friction, herdr)
		exit, err := sup.Run()
		if err != nil || exit != ExitHerdr {
			t.Fatalf("second Run: %d %v, want %d", exit, err, ExitHerdr)
		}
		if herdr.ListReads != 1 {
			t.Fatalf("list reads = %d, want 1 (the gone row is no candidate; the list decides)", herdr.ListReads)
		}
		assertRecoveryFailed(t, store, jobDir, team, nil)
	})
}

// TestSuperviseSpawnRecoverEnsureJobLaneOnce: the job lane (and its
// max_workers bump) is set once across the crash and the restart; the
// spawn intent marks the lane as already ensured.
func TestSuperviseSpawnRecoverEnsureJobLaneOnce(t *testing.T) {
	store, _, clock, team := supFixture(t)
	team.rosterRows = []rosterRow{{Name: "orch-1", Role: "job-orchestrator", Pane: "w1:p2", State: "working"}}
	report := writeOrchestratorReport(t, recoverReport)
	team.statusScript = []fakeStatus{{State: "done", Report: report}}
	var friction []string
	crashSpawn(t, store, clock, team, &friction)
	if st := supState(t, store); st.OrchestratorSpawnAt == "" || st.Orchestrator != "" {
		t.Fatalf("after the crash the state = %+v", st)
	}
	if team.ensureCalls != 1 {
		t.Fatalf("ensure calls after the first run = %d, want 1", team.ensureCalls)
	}
	sup := supSupervisor(store, clock, team, &friction)
	exit, err := sup.Run()
	if err != nil || exit != 0 {
		t.Fatalf("second Run: %d %v, want 0", exit, err)
	}
	if team.ensureCalls != 1 {
		t.Fatalf("ensure calls across the crash and the restart = %d, want 1", team.ensureCalls)
	}
	if team.spawnCalls != 1 {
		t.Fatalf("spawn calls = %d, want 1 (the adoption spawns nothing)", team.spawnCalls)
	}
}

var crashBetweenRecordAndAppend = errors.New("crash between the Orchestrator record and the event append")

// TestSuperviseCrashBetweenOrchestratorRecordAndEvent: the crash leaves the
// name recorded with no worker_spawned; the restart appends exactly one,
// and a third run appends none.
func TestSuperviseCrashBetweenOrchestratorRecordAndEvent(t *testing.T) {
	store, jobDir, clock, team := supFixture(t)
	report := writeOrchestratorReport(t, recoverReport)
	// Crash between the Orchestrator record and the event append: the first
	// worker_spawned append panics before it publishes anything; the
	// restart's backfill append runs afterwards and is not crashed.
	previous := appendEventFn
	crashed := false
	appendEventFn = func(dir string, now time.Time, in EventIn) (Event, error) {
		if in.Tipo == "worker_spawned" && !crashed {
			crashed = true
			panic(crashBetweenRecordAndAppend)
		}
		return previous(dir, now, in)
	}
	t.Cleanup(func() { appendEventFn = previous })
	// Three working ticks keep the job running through the second run's
	// watch loop (stopped by the clock), then the third run reports done.
	team.statusScript = []fakeStatus{
		{State: "working"}, {State: "working"}, {State: "working"},
		{State: "done", Report: report},
	}
	clock.stopAfter = 3
	clock.stop = errors.New("stop the watch loop")
	var friction []string

	// First run: the crash leaves the name recorded with no event.
	sup := supSupervisor(store, clock, team, &friction)
	func() {
		defer func() {
			if r := recover(); !errors.Is(r.(error), crashBetweenRecordAndAppend) {
				panic(r)
			}
		}()
		if _, err := sup.Run(); err != nil {
			t.Errorf("first Run: %v", err)
		}
	}()
	st := supState(t, store)
	if st.OrchestratorSpawnAt == "" || st.Orchestrator != "orch-1" || st.Status != StatusRunning {
		t.Fatalf("after the crash the state = %+v", st)
	}
	if events, err := readEvents(jobDir); err != nil || countTipe(events, "worker_spawned") != 0 {
		t.Fatalf("events after the crash = %v err=%v, want no worker_spawned", events, err)
	}

	// Second run: the backfill appends exactly one worker_spawned and the
	// job keeps running until the clock stops it.
	clock.stopAfter = 3
	clock.stop = errors.New("stop the watch loop")
	sup = supSupervisor(store, clock, team, &friction)
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("the watch loop ended before the stop: it stopped looping")
				return
			}
			if !errors.Is(r.(error), clock.stop) {
				panic(r)
			}
		}()
		if _, err := sup.Run(); err != nil {
			t.Errorf("second Run: %v", err)
		}
	}()
	if st := supState(t, store); st.Status != StatusRunning {
		t.Fatalf("after the second run the state = %+v, want still running", st)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := eventTipes(events); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned"}) {
		t.Fatalf("event types = %v, want the backfilled worker_spawned", got)
	}
	if n := countTipe(events, "worker_spawned"); n != 1 {
		t.Fatalf("worker_spawned count = %d, want exactly one", n)
	}
	if spawned := events[1]; spawned.Refs["agente"] != "orch-1" || spawned.Refs["papel"] != "job-orchestrator" {
		t.Fatalf("worker_spawned = %+v, want the recorded name and role", spawned)
	}
	if len(team.dispatches) != 1 {
		t.Fatalf("dispatches = %v, want exactly one (the crash was before the dispatch)", team.dispatches)
	}

	// Third run: the event exists, so no backfill and no new dispatch; the
	// job ends done with still exactly one worker_spawned.
	clock.stopAfter = 0
	sup = supSupervisor(store, clock, team, &friction)
	exit, err := sup.Run()
	if err != nil || exit != 0 {
		t.Fatalf("third Run: %d %v, want 0", exit, err)
	}
	if st := supState(t, store); st.Status != StatusDone {
		t.Fatalf("state = %+v", st)
	}
	if events, err := readEvents(jobDir); err != nil {
		t.Fatal(err)
	} else if got := eventTipes(events); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned", "worker_done", "terminal", "cleanup"}) {
		t.Fatalf("event types = %v", got)
	}
	if n := countTipe(events, "worker_spawned"); n != 1 {
		t.Fatalf("worker_spawned count = %d, want exactly one after the third run", n)
	}
	if len(team.dispatches) != 1 {
		t.Fatalf("dispatches = %v, want no new dispatch on the third run", team.dispatches)
	}
}
