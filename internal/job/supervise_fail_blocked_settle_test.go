package job

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// The failure exit from blocked is a blocked-boundary crossing like any
// other: the settle of the blocked interval, the re-anchor and the failed
// terminal intent must land in the same state write as the
// blocked -> finishing crossing, so the failure path never leaves the
// blocked tail on neither clock.
//
// Three distinct claims are kept apart in the tests below. A panic at
// the rename is an in-test hard stop at a commit cut point, not a power
// loss or OS crash:
//   - hard stop before the crossing commit: the process stops before the
//     crossing write lands (the rename override panics before the real
//     rename);
//   - hard stop after the crossing commit: the write landed and the
//     process stops right after it (the real rename runs, then the
//     override panics);
//   - write error: the write returns an error and the process keeps
//     running (the override returns an error once).

// failBlockedCrossingHardStop is the sentinel panic of the hard-stop-at-
// the-fail-crossing tests; the harness recovers only this sentinel and
// re-panics anything else, as the established clock.stop harness does.
var failBlockedCrossingHardStop = errors.New("hard stop at the fail's blocked crossing commit")

// failBlockedCrossing installs a renameAtomic override that acts (through
// act) on the first state.json rename that is the fail's blocked ->
// finishing crossing: the on-disk preimage (to, read before renaming)
// decodes to a blocked state and the new content (from) decodes to a
// finishing state. act receives the paths and the real rename; it may
// panic (a hard stop) or return an error (a write failure). Only the
// first match acts; renameAtomic is restored on test cleanup.
func failBlockedCrossing(t *testing.T, act func(from, to string, real func(from, to string) error) error) *bool {
	t.Helper()
	fired := false
	previous := renameAtomic
	renameAtomic = func(from, to string) error {
		if !fired && filepath.Base(to) == "state.json" {
			var pending, prev State
			match := false
			if raw, err := os.ReadFile(from); err == nil {
				if json.Unmarshal(raw, &pending) == nil && pending.Status == StatusFinishing {
					if raw, err := os.ReadFile(to); err == nil {
						if json.Unmarshal(raw, &prev) == nil && prev.Status == StatusBlocked {
							match = true
						}
					}
				}
			}
			if match {
				fired = true
				return act(from, to, previous)
			}
		}
		return previous(from, to)
	}
	t.Cleanup(func() { renameAtomic = previous })
	return &fired
}

// stopAtFailCrossingCommit runs the supervisor under a panic harness that
// recovers only the failBlockedCrossingHardStop sentinel and re-panics
// anything else, like the established clock.stop tests.
func stopAtFailCrossingCommit(t *testing.T, sup *Supervisor) {
	t.Helper()
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("the process did not stop at the injected hard stop")
				return
			}
			if r != failBlockedCrossingHardStop {
				panic(r)
			}
		}()
		if _, err := sup.Run(); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()
}

// failBlockedSetup is the nsSup fixture wired for the failure-exit tests:
// the job's workspace and the Herdr adapter are recorded so the terminal
// release and close are assertable, and the timeout is far from the test's
// wall so no budget fires.
func failBlockedSetup(t *testing.T, friction *[]string) (*Store, string, *nsClock, *fakeTeam, *fakeHerdr, *Supervisor) {
	t.Helper()
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, friction)
	herdr := &fakeHerdr{}
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 60
		st.WorkspaceID = "w1"
	}); err != nil {
		t.Fatal(err)
	}
	sup.Herdr = herdr
	return store, jobDir, clock, team, herdr, sup
}

// failBlockedRestart is the fresh-supervisor restart over the crashed job:
// the clock continues at the crash time (10:01:00), the reads the pre-watch
// steps need are scripted (none: the orchestrator and the dispatch are
// recorded), and the watch loop re-derives the failure.
func failBlockedRestart(t *testing.T, store *Store, friction *[]string) (*fakeTeam, *fakeHerdr) {
	t.Helper()
	team := &fakeTeam{spawnName: "orch-1"}
	team.statusScript = []fakeStatus{{State: "gone"}, {State: "gone"}, {State: "gone"}}
	herdr := &fakeHerdr{}
	// The clock continues at the crash time (10:01:00) with a fresh
	// monotonic origin, like the established crossing-crash restarts.
	sup := &Supervisor{
		Store:    store,
		ID:       "job-1",
		Ops:      team,
		Clock:    &nsClock{now: time.Date(2026, 10, 9, 10, 1, 0, 0, time.FixedZone("-03:00", -3*3600))},
		Herdr:    herdr,
		Poll:     10 * time.Second,
		Friction: func(message string) { *friction = append(*friction, message) },
	}
	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != ExitFailed {
		t.Fatalf("restart exit = %d, want %d", exit, ExitFailed)
	}
	return team, herdr
}

// TestSuperviseFailBlockedGoneSettlesTheBlockedClock: a blocked job whose
// orchestrator ends without a report fails through the real supervisor
// path (the gone streak), and the failure exit settles the blocked tail
// into the blocked clock in the same write as the blocked -> finishing
// crossing. Catches M1 (the base fail leaves the blocked tail unsettled)
// and M2 (the tail settles into the active clock).
func TestSuperviseFailBlockedGoneSettlesTheBlockedClock(t *testing.T) {
	var friction []string
	store, jobDir, _, team, herdr, sup := failBlockedSetup(t, &friction)
	// working t0..t10; question t20 blocks (20 s of active settled, the
	// anchor moves to 10:00:20); question t30 stays blocked; the
	// gone-family reads t40..t60 never unblock and fail the job at t60.
	team.statusScript = []fakeStatus{
		{State: "working"}, {State: "working"},
		{State: "question"}, {State: "question"},
		{State: "gone"}, {State: "settled-no-report"}, {State: "unavailable"},
	}
	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitFailed {
		t.Fatalf("exit = %d, want %d", exit, ExitFailed)
	}
	st := supState(t, store)
	if st.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", st.Status)
	}
	// The blocked tail (10:00:20..10:01:00) settles into the blocked
	// clock only; the active clock keeps the 20 s of pre-block time.
	if st.BudgetBlockedMs != 40000 {
		t.Fatalf("blocked = %d ms, want 40000 (the blocked tail settles out of blocked)", st.BudgetBlockedMs)
	}
	if st.BudgetActiveMs != 20000 {
		t.Fatalf("active = %d ms, want 20000 (the blocked tail never enters the active clock)", st.BudgetActiveMs)
	}
	if st.IntervalStart != "2026-10-09T10:01:00-03:00" {
		t.Fatalf("anchor = %q, want 10:01:00 (the crossing instant)", st.IntervalStart)
	}
	if st.TerminalIntent == nil || st.TerminalIntent.Status != StatusFailed ||
		st.TerminalIntent.Motivo != "job: the job orchestrator ended without a report" ||
		st.TerminalIntent.Exit != ExitFailed {
		t.Fatalf("intent = %+v, want the failed intent with motivo and exit", st.TerminalIntent)
	}
	if st.Motivo == nil || *st.Motivo != "job: the job orchestrator ended without a report" {
		t.Fatalf("motivo = %v, want the failure motivo", st.Motivo)
	}
	events := mustEvents(t, jobDir)
	if got := eventTipes(events); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned", "blocked", "failure", "terminal", "cleanup"}) {
		t.Fatalf("event types = %v, want accepted, worker_spawned, blocked, failure, terminal, cleanup", got)
	}
	if terminal := events[4]; terminal.Refs["exit"] != "19" {
		t.Fatalf("terminal refs = %+v, want exit 19", terminal.Refs)
	}
	// The terminal path releases the orchestrator and closes the
	// workspace.
	if team.releaseCalls != 1 || team.releaseTeamCalls != 1 || team.gcCalls != 1 {
		t.Fatalf("release = %d team %d gc %d, want 1/1/1", team.releaseCalls, team.releaseTeamCalls, team.gcCalls)
	}
	if !reflect.DeepEqual(herdr.CloseIDs, []string{"w1"}) || !st.WorkspaceClosed {
		t.Fatalf("close ids = %v workspaceClosed = %v, want [w1] and true", herdr.CloseIDs, st.WorkspaceClosed)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseFailBlockedStatusErrorSettlesTheBlockedClock: the same
// failure exit reached through accumulate (three consecutive failed
// status reads while the job is blocked). Catches M1 and M2.
func TestSuperviseFailBlockedStatusErrorSettlesTheBlockedClock(t *testing.T) {
	var friction []string
	store, jobDir, _, team, herdr, sup := failBlockedSetup(t, &friction)
	statusErr := errors.New("status read failed")
	// working t0..t10; question t20 blocks; question t30 stays blocked
	// (the error streak drops); the three failed reads t40..t60 fail the
	// job at t60 through accumulate.
	team.statusScript = []fakeStatus{
		{State: "working"}, {State: "working"},
		{State: "question"}, {State: "question"},
		{Err: statusErr}, {Err: statusErr}, {Err: statusErr},
	}
	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitFailed {
		t.Fatalf("exit = %d, want %d", exit, ExitFailed)
	}
	st := supState(t, store)
	if st.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", st.Status)
	}
	if st.BudgetBlockedMs != 40000 {
		t.Fatalf("blocked = %d ms, want 40000 (the blocked tail settles out of blocked)", st.BudgetBlockedMs)
	}
	if st.BudgetActiveMs != 20000 {
		t.Fatalf("active = %d ms, want 20000 (the blocked tail never enters the active clock)", st.BudgetActiveMs)
	}
	if st.IntervalStart != "2026-10-09T10:01:00-03:00" {
		t.Fatalf("anchor = %q, want 10:01:00 (the crossing instant)", st.IntervalStart)
	}
	if st.TerminalIntent == nil || st.TerminalIntent.Status != StatusFailed || st.TerminalIntent.Exit != ExitFailed {
		t.Fatalf("intent = %+v, want the failed intent", st.TerminalIntent)
	}
	if got := eventTipes(mustEvents(t, jobDir)); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned", "blocked", "failure", "terminal", "cleanup"}) {
		t.Fatalf("event types = %v, want accepted, worker_spawned, blocked, failure, terminal, cleanup", got)
	}
	if team.releaseCalls != 1 || team.releaseTeamCalls != 1 || team.gcCalls != 1 {
		t.Fatalf("release = %d team %d gc %d, want 1/1/1", team.releaseCalls, team.releaseTeamCalls, team.gcCalls)
	}
	if !reflect.DeepEqual(herdr.CloseIDs, []string{"w1"}) || !st.WorkspaceClosed {
		t.Fatalf("close ids = %v workspaceClosed = %v, want [w1] and true", herdr.CloseIDs, st.WorkspaceClosed)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseFailBlockedHardStopBeforeCrossingCommit: a hard stop
// before the crossing commit (the process stops before the rename)
// leaves the pre-crossing blocked state — the unsettled tail, no intent —
// and the restart settles the blocked tail exactly once, all in the
// blocked clock. Catches M1 (the restart must converge from the
// pre-crossing state with exact totals) and M3a (the failed intent lands
// in the blocked preimage, so the preimage carries it at the stop).
func TestSuperviseFailBlockedHardStopBeforeCrossingCommit(t *testing.T) {
	var friction []string
	store, jobDir, _, team, herdr, sup := failBlockedSetup(t, &friction)
	team.statusScript = []fakeStatus{
		{State: "working"}, {State: "working"},
		{State: "question"}, {State: "question"},
		{State: "gone"}, {State: "gone"}, {State: "gone"},
	}
	// The hard stop before the commit: the crossing write's rename never
	// runs; the process stops as if it died before the write landed.
	fired := failBlockedCrossing(t, func(from, to string, real func(from, to string) error) error {
		panic(failBlockedCrossingHardStop)
	})
	stopAtFailCrossingCommit(t, sup)
	if !*fired {
		t.Fatal("the hard-stop injection never matched the blocked -> finishing crossing rename")
	}
	// The preimage at the stop: entirely pre-crossing — the blocked tail
	// still in flight, no intent from the failure path, no failure or
	// terminal event, no release and no workspace close.
	st := supState(t, store)
	if st.Status != StatusBlocked {
		t.Fatalf("status = %s, want the pre-crossing blocked (the preimage at the stop)", st.Status)
	}
	if st.BudgetBlockedMs != 0 || st.BudgetActiveMs != 20000 {
		t.Fatalf("settled = active %d / blocked %d ms, want 20000 / 0 (the tail is still in flight)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if st.IntervalStart != "2026-10-09T10:00:20-03:00" {
		t.Fatalf("anchor = %q, want the block anchor (the tail unsettled)", st.IntervalStart)
	}
	if st.TerminalIntent != nil {
		t.Fatalf("intent = %+v, want no intent written before the crossing", st.TerminalIntent)
	}
	if got := eventTipes(mustEvents(t, jobDir)); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned", "blocked"}) {
		t.Fatalf("event types = %v, want no failure and no terminal event", got)
	}
	if team.releaseCalls != 0 || len(herdr.CloseIDs) != 0 || st.WorkspaceClosed {
		t.Fatalf("release = %d close = %v closed = %v, want none (the process stopped before the terminal path)", team.releaseCalls, herdr.CloseIDs, st.WorkspaceClosed)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	// The restart: a fresh supervisor, the clock continuing at the crash
	// time (10:01:00), through Run(): waitRunning proceeds on the blocked
	// job and the watch loop re-derives the failure; the crossing settles
	// the whole blocked interval (10:00:20..10:01:20) exactly once.
	team2, herdr2 := failBlockedRestart(t, store, &friction)
	st = supState(t, store)
	if st.Status != StatusFailed {
		t.Fatalf("status = %s, want failed after the restart", st.Status)
	}
	// Exact: active is the 20 s of pre-block time; blocked is the whole
	// 10:00:20..10:01:20 (60 s, the crash gap included) — the tail
	// counted once, all in the blocked clock, none in the active one.
	if st.BudgetActiveMs != 20000 || st.BudgetBlockedMs != 60000 {
		t.Fatalf("settled = active %d / blocked %d ms, want 20000 / 60000 (nothing lost, nothing counted twice)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if st.IntervalStart != "2026-10-09T10:01:20-03:00" {
		t.Fatalf("anchor = %q, want 10:01:20 (the restart's crossing instant)", st.IntervalStart)
	}
	if st.TerminalIntent == nil || st.TerminalIntent.Status != StatusFailed || st.TerminalIntent.Exit != ExitFailed {
		t.Fatalf("intent = %+v, want the failed intent", st.TerminalIntent)
	}
	events := mustEvents(t, jobDir)
	if got := eventTipes(events); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned", "blocked", "failure", "terminal", "cleanup"}) {
		t.Fatalf("event types = %v, want the failed outcome exactly once", got)
	}
	if n := countTipe(events, "terminal"); n != 1 {
		t.Fatalf("terminal events = %d, want 1", n)
	}
	if team2.releaseCalls != 1 || !reflect.DeepEqual(herdr2.CloseIDs, []string{"w1"}) || !st.WorkspaceClosed {
		t.Fatalf("restart release = %d close = %v closed = %v, want the terminal release and close", team2.releaseCalls, herdr2.CloseIDs, st.WorkspaceClosed)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseFailBlockedHardStopAfterCrossingCommit: a hard stop right
// after the crossing commit (the rename runs, then the process stops)
// leaves finishing with the settled clock AND the failed intent from that
// one committed write; the restart takes the stored-intent route and ends
// failed with identical totals — nothing settles again. Catches M1 and
// M3b (the committed crossing lacks the failed intent).
func TestSuperviseFailBlockedHardStopAfterCrossingCommit(t *testing.T) {
	var friction []string
	store, jobDir, _, team, herdr, sup := failBlockedSetup(t, &friction)
	team.statusScript = []fakeStatus{
		{State: "working"}, {State: "working"},
		{State: "question"}, {State: "question"},
		{State: "gone"}, {State: "gone"}, {State: "gone"},
	}
	// The hard stop after the commit: the crossing write's rename runs
	// (the write commits), then the process stops right after it.
	fired := failBlockedCrossing(t, func(from, to string, real func(from, to string) error) error {
		if err := real(from, to); err != nil {
			return err
		}
		panic(failBlockedCrossingHardStop)
	})
	stopAtFailCrossingCommit(t, sup)
	if !*fired {
		t.Fatal("the hard-stop injection never matched the blocked -> finishing crossing rename")
	}
	// The committed file, everything from that one write: the
	// transition, the settled blocked clock and the failed intent landed
	// together.
	st := supState(t, store)
	if st.Status != StatusFinishing {
		t.Fatalf("status = %s, want finishing (the failed-state write never ran)", st.Status)
	}
	if st.BudgetBlockedMs != 40000 {
		t.Fatalf("blocked = %d ms, want 40000 (the tail settled in the crossing write)", st.BudgetBlockedMs)
	}
	if st.BudgetActiveMs != 20000 {
		t.Fatalf("active = %d ms, want 20000 (unchanged by the failure exit)", st.BudgetActiveMs)
	}
	if st.IntervalStart != "2026-10-09T10:01:00-03:00" {
		t.Fatalf("anchor = %q, want 10:01:00 (the crossing instant)", st.IntervalStart)
	}
	if st.TerminalIntent == nil || st.TerminalIntent.Status != StatusFailed ||
		st.TerminalIntent.Motivo != "job: the job orchestrator ended without a report" ||
		st.TerminalIntent.Exit != ExitFailed {
		t.Fatalf("intent = %+v, want the failed intent in the same write as the settled clock", st.TerminalIntent)
	}
	if got := eventTipes(mustEvents(t, jobDir)); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned", "blocked"}) {
		t.Fatalf("event types = %v, want no failure and no terminal event (the store walk never ran)", got)
	}
	if team.releaseCalls != 0 || len(herdr.CloseIDs) != 0 || st.WorkspaceClosed {
		t.Fatalf("release = %d close = %v closed = %v, want none (the process stopped before the terminal path)", team.releaseCalls, herdr.CloseIDs, st.WorkspaceClosed)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	// The restart: the stored failed intent routes the fresh supervisor
	// straight to the failure walk from finishing — nothing settles
	// again, and the release and the workspace close land now.
	team2, herdr2 := failBlockedRestart(t, store, &friction)
	st = supState(t, store)
	if st.Status != StatusFailed {
		t.Fatalf("status = %s, want failed after the restart", st.Status)
	}
	if st.BudgetActiveMs != 20000 || st.BudgetBlockedMs != 40000 {
		t.Fatalf("settled = active %d / blocked %d ms, want 20000 / 40000 (identical totals: nothing settled twice, nothing in the active clock)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if st.IntervalStart != "2026-10-09T10:01:00-03:00" {
		t.Fatalf("anchor = %q, want 10:01:00 (the crossing instant, unchanged by the restart)", st.IntervalStart)
	}
	events := mustEvents(t, jobDir)
	if n := countTipe(events, "terminal"); n != 1 {
		t.Fatalf("terminal events = %d, want 1 (at most once across the hard stop)", n)
	}
	if team2.releaseCalls != 1 || !reflect.DeepEqual(herdr2.CloseIDs, []string{"w1"}) || !st.WorkspaceClosed {
		t.Fatalf("restart release = %d close = %v closed = %v, want the terminal release and close", team2.releaseCalls, herdr2.CloseIDs, st.WorkspaceClosed)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseFailBlockedWriteErrorAtCrossing: a write error at the
// combined write is not a hard stop: the write fails, the process keeps
// running and exits with the conservative failed code and the error; the
// job stays blocked and unsettled (the plain Store.Fail walk never ran —
// no failure or terminal event, no release, no close), and a fresh
// supervisor restart through Run() converges to failed with exact totals.
// Catches M4 (fall through to the rest of fail on the settle error) and
// M5 (the settle error runs finishWakes/finalize before returning).
func TestSuperviseFailBlockedWriteErrorAtCrossing(t *testing.T) {
	var friction []string
	store, jobDir, _, team, herdr, sup := failBlockedSetup(t, &friction)
	team.statusScript = []fakeStatus{
		{State: "working"}, {State: "working"},
		{State: "question"}, {State: "question"},
		{State: "gone"}, {State: "gone"}, {State: "gone"},
	}
	// The write error: the crossing write's rename returns an error
	// once; the process keeps running.
	fired := failBlockedCrossing(t, func(from, to string, real func(from, to string) error) error {
		return errors.New("injected write error at the crossing write")
	})
	exit, err := sup.Run()
	if err == nil {
		t.Fatalf("Run: exit = %d, want a non-nil error (the write failure is a store failure)", exit)
	}
	if exit != ExitFailed {
		t.Fatalf("exit = %d, want %d (the conservative failed code)", exit, ExitFailed)
	}
	if !*fired {
		t.Fatal("the write error was never injected at the combined crossing write")
	}
	// Still blocked and unsettled: the failure walk refused to run from
	// a state it could not cross, so the status never became failed.
	st := supState(t, store)
	if st.Status != StatusBlocked {
		t.Fatalf("status = %s, want still blocked (never failed)", st.Status)
	}
	if st.BudgetBlockedMs != 0 || st.BudgetActiveMs != 20000 {
		t.Fatalf("settled = active %d / blocked %d ms, want 20000 / 0 (the tail is still in flight)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if st.IntervalStart != "2026-10-09T10:00:20-03:00" {
		t.Fatalf("anchor = %q, want the block anchor (the tail unsettled)", st.IntervalStart)
	}
	if st.TerminalIntent != nil {
		t.Fatalf("intent = %+v, want no failed intent from this path", st.TerminalIntent)
	}
	if got := eventTipes(mustEvents(t, jobDir)); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned", "blocked"}) {
		t.Fatalf("event types = %v, want no failure and no terminal event", got)
	}
	if team.releaseCalls != 0 || len(herdr.CloseIDs) != 0 || st.WorkspaceClosed {
		t.Fatalf("release = %d close = %v closed = %v, want none (the failure walk never ran)", team.releaseCalls, herdr.CloseIDs, st.WorkspaceClosed)
	}
	if len(friction) != 1 || friction[0] != "job: the supervisor could not record the failure: injected write error at the crossing write" {
		t.Fatalf("friction = %v, want exactly the one failure-record line", friction)
	}
	// The restart through Run() converges to failed with exact totals.
	team2, herdr2 := failBlockedRestart(t, store, &friction)
	st = supState(t, store)
	if st.Status != StatusFailed {
		t.Fatalf("status = %s, want failed after the restart", st.Status)
	}
	if st.BudgetActiveMs != 20000 || st.BudgetBlockedMs != 60000 {
		t.Fatalf("settled = active %d / blocked %d ms, want 20000 / 60000 (the tail counted once, the crash gap blocked)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if st.IntervalStart != "2026-10-09T10:01:20-03:00" {
		t.Fatalf("anchor = %q, want 10:01:20 (the restart's crossing instant)", st.IntervalStart)
	}
	if n := countTipe(mustEvents(t, jobDir), "terminal"); n != 1 {
		t.Fatalf("terminal events = %d, want 1", n)
	}
	if team2.releaseCalls != 1 || !reflect.DeepEqual(herdr2.CloseIDs, []string{"w1"}) || !st.WorkspaceClosed {
		t.Fatalf("restart release = %d close = %v closed = %v, want the terminal release and close", team2.releaseCalls, herdr2.CloseIDs, st.WorkspaceClosed)
	}
	if len(friction) != 1 {
		t.Fatalf("friction = %v, want the one failure-record line and nothing more", friction)
	}
}

// TestSuperviseFailRunningKeepsTheBaseAccounting: control — a running job
// that fails through the gone streak keeps today's exact accounting and
// behavior (pinned on the base tree and identical after the fix): no
// block ever happened, so nothing settles and the failure walk is
// unchanged.
func TestSuperviseFailRunningKeepsTheBaseAccounting(t *testing.T) {
	var friction []string
	store, jobDir, _, team, herdr, sup := failBlockedSetup(t, &friction)
	// working t0..t10, the gone family t20..t40 fails the still-running
	// job at t40.
	team.statusScript = []fakeStatus{
		{State: "working"}, {State: "working"},
		{State: "gone"}, {State: "gone"}, {State: "gone"},
	}
	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitFailed {
		t.Fatalf("exit = %d, want %d", exit, ExitFailed)
	}
	st := supState(t, store)
	if st.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", st.Status)
	}
	// Pinned on the base tree: the failure path settles nothing for a
	// job that never blocked; the counters and the anchor are exactly as
	// the base leaves them.
	if st.BudgetActiveMs != 0 || st.BudgetBlockedMs != 0 {
		t.Fatalf("settled = active %d / blocked %d ms, want 0 / 0 (a running failure settles nothing)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if st.IntervalStart != "" {
		t.Fatalf("anchor = %q, want the untouched fixture anchor (empty)", st.IntervalStart)
	}
	if st.Motivo == nil || *st.Motivo != "job: the job orchestrator ended without a report" {
		t.Fatalf("motivo = %v, want the failure motivo", st.Motivo)
	}
	events := mustEvents(t, jobDir)
	if got := eventTipes(events); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned", "failure", "terminal", "cleanup"}) {
		t.Fatalf("event types = %v, want accepted, worker_spawned, failure, terminal, cleanup", got)
	}
	if terminal := events[3]; terminal.Refs["exit"] != "19" {
		t.Fatalf("terminal refs = %+v, want exit 19", terminal.Refs)
	}
	if team.releaseCalls != 1 || team.releaseTeamCalls != 1 || team.gcCalls != 1 {
		t.Fatalf("release = %d team %d gc %d, want 1/1/1", team.releaseCalls, team.releaseTeamCalls, team.gcCalls)
	}
	if !reflect.DeepEqual(herdr.CloseIDs, []string{"w1"}) || !st.WorkspaceClosed {
		t.Fatalf("close ids = %v workspaceClosed = %v, want [w1] and true", herdr.CloseIDs, st.WorkspaceClosed)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}
