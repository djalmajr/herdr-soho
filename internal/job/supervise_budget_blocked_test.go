package job

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// The active/blocked budget tests drive the supervisor's watch loop with the
// shared fakes at a known poll cadence so ticks land at exact monotonic
// times: the active budget (timeout_min) counts every second the job is not
// in status blocked and is paused while it is; the blocked budget
// (blocked_timeout_min, the machine default 1440 when absent) counts only
// the blocked seconds, warns once at 80 % (blocked_warning) and ends the
// job at 100 % through the existing timeout outcome (exit 9) with the
// distinct ceiling motivo. All tests use the injected clocks, no real
// sleeps and no timer changes.

// blockedCeilingMotivo is the fixed motivo the blocked ceiling ends the job
// with, distinct from the active-budget timeout's "tempo esgotado".
const blockedCeilingMotivo = "tempo de bloqueio esgotado"

func addStatuses(script []fakeStatus, states ...string) []fakeStatus {
	for _, state := range states {
		script = append(script, fakeStatus{State: state})
	}
	return script
}

// fillScript appends n entries of state.
func fillScript(script []fakeStatus, n int, state string) []fakeStatus {
	for i := 0; i < n; i++ {
		script = append(script, fakeStatus{State: state})
	}
	return script
}

// TestSuperviseBudgetActiveOnlyWarning: the 80 % warning counts active time
// only. Working 40 s, blocked 40 s (wall 50 s past 80 % of the 1 min
// budget at the first blocked tick — a total-wall budget would warn and
// stop while the job is blocked), then working: the warning lands on the
// first tick at or past 80 % of the cumulative ACTIVE time, after the
// unblock, and exactly once.
func TestSuperviseBudgetActiveOnlyWarning(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 1
		start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Format(time.RFC3339)
		st.IntervalStart = start.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	// Working 40 s (t0..t30), blocked t40..t70 (the wall passes 80 % and
	// 100 % of the total budget while blocked), working t80..t90.
	team.statusScript = fillScript(nil, 4, "working")
	team.statusScript = fillScript(team.statusScript, 4, "question")
	team.statusScript = fillScript(team.statusScript, 2, "working")
	clock.stopAfter = 10
	clock.stop = errors.New("stop the watch loop")
	runUntilStop(t, sup, clock.stop)

	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "worker_spawned", "blocked", "unblocked", "timeout_warning"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v (one warning, after the unblock)", got, want)
	}
	warning := events[4]
	if warning.Resumo != "80% of the active job budget used" {
		t.Fatalf("timeout_warning resumo = %q", warning.Resumo)
	}
	// Active crosses 80 % of the 1 min budget at the first tick at or
	// past 48 s of active time: 40 s settled plus 10 s after the unblock
	// (t90), not at t50 where the total wall passed 80 % while blocked.
	if want := time.Date(2026, 10, 9, 10, 1, 30, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339); warning.TS != want {
		t.Fatalf("warning ts = %q, want %q (the first tick at or past 80 %% of active)", warning.TS, want)
	}
	if n := countTipe(events, "blocked_warning"); n != 0 {
		t.Fatalf("blocked_warnings = %d, want 0 (far from the blocked ceiling)", n)
	}
	if _, err := os.Stat(filepath.Join(jobDir, "stop-amend.md")); !os.IsNotExist(err) {
		t.Fatalf("no stop amendment before 100 %% of active: %v", err)
	}
	if st := supState(t, store); st.Status != StatusRunning {
		t.Fatalf("state = %+v, want still running", st)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseBudgetBlockedDoesNotConsumeActiveBudget: blocked time does
// not count toward the active budget. Blocked 70 s of wall (past 100 % of
// the 1 min total budget — a total-wall budget would have stopped the job
// while it was blocked), then working: no warning or stop while blocked,
// the warning at the first tick at or past 80 % of active after the
// unblock, and the stop at 100 % of active, terminal timeout 9.
func TestSuperviseBudgetBlockedDoesNotConsumeActiveBudget(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) { st.TimeoutMin = 1 }); err != nil {
		t.Fatal(err)
	}
	// Blocked t0..t60 (70 s of wall), working t70..t120, done t130.
	team.statusScript = fillScript(nil, 7, "question")
	team.statusScript = fillScript(team.statusScript, 6, "working")
	team.statusScript = append(team.statusScript, fakeStatus{State: "done"})

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitTimeout {
		t.Fatalf("exit = %d, want %d (the active budget, not the wall, expired)", exit, ExitTimeout)
	}
	if st := supState(t, store); st.Status != StatusTimeout {
		t.Fatalf("state = %+v", st)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "worker_spawned", "blocked", "unblocked", "timeout_warning", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v (no warning or stop while blocked)", got, want)
	}
	warning := events[4]
	if warning.Resumo != "80% of the active job budget used" {
		t.Fatalf("timeout_warning resumo = %q", warning.Resumo)
	}
	// 48 s of active is first reached at t120 (10 s active after the t70
	// unblock settles the 70 s blocked interval).
	if want := time.Date(2026, 10, 9, 10, 2, 0, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339); warning.TS != want {
		t.Fatalf("warning ts = %q, want %q", warning.TS, want)
	}
	terminal := events[5]
	if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != "tempo esgotado" {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	assertStopAmend(t, jobDir)
	if len(team.dispatches) != 2 || team.dispatches[1] != (fakeDispatch{Name: "orch-1", Path: filepath.Join(jobDir, "stop-amend.md"), Amend: true}) {
		t.Fatalf("dispatches = %+v", team.dispatches)
	}
	// The 70 s blocked interval settled into the blocked counter at the
	// unblock; the active counter stayed at zero while blocked.
	st := supState(t, store)
	if st.BudgetBlockedMs != 70000 || st.BudgetActiveMs != 0 {
		t.Fatalf("settled = active %d ms / blocked %d ms, want 0 / 70000", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if n := countTipe(events, "blocked_warning"); n != 0 {
		t.Fatalf("blocked_warnings = %d, want 0", n)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	// Ticks at 0, 10, ..., 130 s: the stop starts at 60 s of active
	// (t130), the orchestrator's done ends it in the same tick.
	if clock.sleeps != 13 {
		t.Fatalf("sleeps = %d, want 13", clock.sleeps)
	}
}

// TestSuperviseBudgetMultipleBlockUnblock: several crossings settle into
// the counters at each boundary; the warning lands once, at the first tick
// at or past 80 % of the cumulative active time; the terminal mapping is
// unchanged (timeout 9, tempo esgotado).
func TestSuperviseBudgetMultipleBlockUnblock(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) { st.TimeoutMin = 10 }); err != nil {
		t.Fatal(err)
	}
	// Active 120 s (20 %), blocked 2880 s (48 m), active 180 s (30 %),
	// blocked 620 s, active to 480 s cumulative (80 % — warning at t3980),
	// blocked 100 s, active to 600 s cumulative (100 % — stop at t4200).
	team.statusScript = fillScript(nil, 12, "working")
	team.statusScript = fillScript(team.statusScript, 288, "question")
	team.statusScript = fillScript(team.statusScript, 18, "working")
	team.statusScript = fillScript(team.statusScript, 62, "question")
	team.statusScript = fillScript(team.statusScript, 20, "working")
	team.statusScript = fillScript(team.statusScript, 10, "question")
	team.statusScript = fillScript(team.statusScript, 10, "working")
	team.statusScript = append(team.statusScript, fakeStatus{State: "done"})

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitTimeout {
		t.Fatalf("exit = %d, want %d", exit, ExitTimeout)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "worker_spawned", "blocked", "unblocked", "blocked", "unblocked",
		"timeout_warning", "blocked", "unblocked", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v (paired crossings, one warning)", got, want)
	}
	// 80 % of the 10 min active budget (480 s) is first reached at t3980:
	// 300 s settled plus 180 s after the third unblock.
	if want := time.Date(2026, 10, 9, 11, 6, 20, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339); events[6].TS != want {
		t.Fatalf("warning ts = %q, want %q", events[6].TS, want)
	}
	terminal := events[9]
	if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != "tempo esgotado" {
		t.Fatalf("terminal refs = %+v (the ceiling is not involved: the active budget expired)", terminal.Refs)
	}
	// The crossings settled the exact intervals: active 120+180+200 s,
	// blocked 2880+620+100 s.
	st := supState(t, store)
	if st.BudgetActiveMs != 500000 || st.BudgetBlockedMs != 3600000 {
		t.Fatalf("settled = active %d ms / blocked %d ms, want 500000 / 3600000", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	if clock.sleeps != 420 {
		t.Fatalf("sleeps = %d, want 420", clock.sleeps)
	}
}

// TestSuperviseBlockedWarningOnce: the blocked alert fires once, at the
// first tick at or past 80 % of the blocked ceiling; further ticks in the
// same process append nothing (the in-process guard), and nothing else
// happens until the ceiling itself.
func TestSuperviseBlockedWarningOnce(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 10
		st.BlockedTimeoutMin = 2
		start := time.Date(2026, 10, 9, 9, 59, 50, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Format(time.RFC3339)
		st.IntervalStart = start.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	// Blocked through t100: the 2 min ceiling's 80 % (96 s) is first
	// reached at t100, and the ceiling (120 s) is not reached yet.
	team.statusScript = fillScript(nil, 11, "question")
	clock.stopAfter = 11
	clock.stop = errors.New("stop the watch loop")
	runUntilStop(t, sup, clock.stop)

	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "blocked_warning"); n != 1 {
		t.Fatalf("blocked_warnings = %d, want 1 (one per process, once total)", n)
	}
	var warning Event
	for _, event := range events {
		if event.Tipo == "blocked_warning" {
			warning = event
		}
	}
	if warning.Resumo != "80% of the blocked ceiling used" {
		t.Fatalf("blocked_warning resumo = %q", warning.Resumo)
	}
	if want := time.Date(2026, 10, 9, 10, 1, 40, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339); warning.TS != want {
		t.Fatalf("blocked_warning ts = %q, want %q (the first tick at or past 80 %%)", warning.TS, want)
	}
	if n := countTipe(events, "timeout_warning"); n != 0 {
		t.Fatalf("timeout_warnings = %d, want 0 (active time is far from its budget)", n)
	}
	if n := countTipe(events, "terminal"); n != 0 {
		t.Fatalf("terminals = %d, want 0 (the ceiling is not reached)", n)
	}
	if _, err := os.Stat(filepath.Join(jobDir, "stop-amend.md")); !os.IsNotExist(err) {
		t.Fatalf("no stop amendment before the ceiling: %v", err)
	}
	if st := supState(t, store); st.Status != StatusBlocked {
		t.Fatalf("state = %+v, want still blocked", st)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseBlockedWarningRestartNoDuplicate: a restarted supervisor
// (fresh monotonic origin, the blocked total re-derived once from the
// persisted counters and the interval anchor) does not warn again.
func TestSuperviseBlockedWarningRestartNoDuplicate(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 10
		st.BlockedTimeoutMin = 2
		start := time.Date(2026, 10, 9, 9, 59, 50, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Format(time.RFC3339)
		st.IntervalStart = start.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	// Process one: blocked past the 80 % alert, unblocked one tick later
	// (the blocked interval settles and persists), then a crash while
	// running.
	team.statusScript = fillScript(nil, 11, "question")
	team.statusScript = append(team.statusScript, fakeStatus{State: "working"})
	clock.stopAfter = 12
	clock.stop = errors.New("stop the watch loop")
	runUntilStop(t, sup, clock.stop)

	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "blocked_warning"); n != 1 {
		t.Fatalf("warnings after the first run = %d, want 1", n)
	}
	// The unblock settled the 110 s blocked interval, and the job is
	// running at the crash, so the restart passes the running wait.
	if st := supState(t, store); st.Status != StatusRunning || st.BudgetBlockedMs != 110000 {
		t.Fatalf("state = %+v, want running with the 110 s blocked interval settled", st)
	}

	// Process two: a fresh supervisor (its own monotonic clock) over the
	// same store; the blocked total (persisted, past the alert) is
	// re-derived without a second warning, and the counter is kept.
	clock2 := &nsClock{now: clock.now}
	store.Clock = clock2
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = []fakeStatus{{State: "working"}}
	clock2.stopAfter = 1
	clock2.stop = errors.New("stop the watch loop")
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second}
	runUntilStop(t, sup2, clock2.stop)

	events, err = readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "blocked_warning"); n != 1 {
		t.Fatalf("warnings after the restart = %d, want 1 (the log already has one)", n)
	}
	if n := countTipe(events, "terminal"); n != 0 {
		t.Fatalf("terminals = %d, want 0 (110 s is past the alert, below the 120 s ceiling)", n)
	}
	if st := supState(t, store); st.Status != StatusRunning || st.BudgetBlockedMs != 110000 {
		t.Fatalf("state = %+v, want running with the blocked counter kept (no reset)", st)
	}
	if team2.spawnCalls != 0 {
		t.Fatalf("restart spawn calls = %d, want 0", team2.spawnCalls)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseBlockedCeiling: the blocked ceiling runs the existing stop
// path — stop amendment, once-per-process dispatch, bounded grace — and
// ends the job through the existing timeout outcome (exit 9) with the
// distinct ceiling motivo: report.json published with status timeout and
// the motivo, job wait's exit mapping 9, the state timeout (never a live
// blocked state with a closed job), and the stop ending early on the
// orchestrator's done exactly like the active timeout stop (the done
// beats the 120 s deadline one tick after the ceiling).
func TestSuperviseBlockedCeiling(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 1
		st.BlockedTimeoutMin = 1
	}); err != nil {
		t.Fatal(err)
	}
	// Blocked through the ceiling at t60 (warning at t50); the
	// orchestrator stays blocked until the stop's done read at t70.
	team.statusScript = fillScript(nil, 7, "question")
	team.statusScript = append(team.statusScript, fakeStatus{State: "done"})

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitTimeout {
		t.Fatalf("exit = %d, want %d (the ceiling ends through the existing timeout outcome)", exit, ExitTimeout)
	}
	// The final state is the terminal timeout, not a live blocked state.
	st := supState(t, store)
	if st.Status != StatusTimeout {
		t.Fatalf("state = %+v, want the terminal timeout (never a live blocked job)", st)
	}
	if st.Motivo == nil || *st.Motivo != blockedCeilingMotivo {
		got := "<nil>"
		if st.Motivo != nil {
			got = *st.Motivo
		}
		t.Fatalf("state motivo = %q, want %q", got, blockedCeilingMotivo)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "worker_spawned", "blocked", "blocked_warning", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if n := countTipe(events, "blocked_warning"); n != 1 || countTipe(events, "terminal") != 1 || countTipe(events, "cleanup") != 1 {
		t.Fatalf("warning/terminal/cleanup = %d/%d/%d, want 1/1/1",
			countTipe(events, "blocked_warning"), countTipe(events, "terminal"), countTipe(events, "cleanup"))
	}
	warning := events[3]
	if warning.Resumo != "80% of the blocked ceiling used" {
		t.Fatalf("blocked_warning resumo = %q", warning.Resumo)
	}
	if want := time.Date(2026, 10, 9, 10, 0, 50, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339); warning.TS != want {
		t.Fatalf("blocked_warning ts = %q, want %q (the first tick at or past 80 %%)", warning.TS, want)
	}
	terminal := events[4]
	if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != blockedCeilingMotivo {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	// The terminal outcome published report.json with the status timeout
	// and the distinct ceiling motivo.
	rep, err := os.ReadFile(filepath.Join(jobDir, "report.json"))
	if err != nil {
		t.Fatalf("report.json: %v", err)
	}
	if !strings.Contains(string(rep), `"status":"timeout"`) || !strings.Contains(string(rep), `"motivo":"`+blockedCeilingMotivo+`"`) {
		t.Fatalf("report.json = %s", rep)
	}
	// job wait's exit mapping for the terminal state is the timeout code.
	if st, err := store.Wait("job-1", 5*time.Second); err != nil || st.Status != StatusTimeout {
		t.Fatalf("wait = %+v err %v, want the terminal state", st, err)
	}
	if code := supervisorExit(st.Status); code != 9 {
		t.Fatalf("wait exit = %d, want 9", code)
	}
	assertStopAmend(t, jobDir)
	if len(team.dispatches) != 2 || team.dispatches[1] != (fakeDispatch{Name: "orch-1", Path: filepath.Join(jobDir, "stop-amend.md"), Amend: true}) {
		t.Fatalf("dispatches = %+v (one stop amendment, dispatched once)", team.dispatches)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	// The ceiling started at t60 and the orchestrator's done ended the
	// stop at t70, one tick later, well inside the 120 s grace — the same
	// early end the active timeout stop has (the done beats the
	// deadline): ticks at 0, 10, ..., 70 s, the last one done.
	if clock.sleeps != 7 {
		t.Fatalf("sleeps = %d, want 7 (the done beat the ceiling grace)", clock.sleeps)
	}

	// A supervisor started on the already-terminal job returns the
	// outcome's code, finalizes nothing new, dispatches nothing, and
	// appends no second terminal or cleanup (idempotent finalize).
	team2 := &fakeTeam{spawnName: "orch-1"}
	clock2 := &nsClock{now: clock.now}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	exit2, err := sup2.Run()
	if err != nil {
		t.Fatalf("terminal restart Run: %v", err)
	}
	if exit2 != ExitTimeout {
		t.Fatalf("terminal restart exit = %d, want %d", exit2, ExitTimeout)
	}
	if team2.spawnCalls != 0 || len(team2.dispatches) != 0 {
		t.Fatalf("terminal restart dispatched something: spawn=%d dispatches=%+v", team2.spawnCalls, team2.dispatches)
	}
	events, err = readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "terminal"); n != 1 || countTipe(events, "cleanup") != 1 || countTipe(events, "blocked_warning") != 1 {
		t.Fatalf("terminal restart counts = %d/%d/%d, want 1/1/1 (no duplicates)",
			countTipe(events, "terminal"), countTipe(events, "cleanup"), countTipe(events, "blocked_warning"))
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseBlockedCeilingRestartOncePerProcess: a supervisor that dies
// inside the ceiling's grace is restarted (the job unblocked by an amend
// during the grace, so the restart passes the running wait); the restart
// re-derives the blocked total from the persisted settled counter,
// re-dispatches the stop amendment once, writes no duplicate
// blocked_warning, and ends timeout 9 with the distinct motivo.
func TestSuperviseBlockedCeilingRestartOncePerProcess(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 1
		st.BlockedTimeoutMin = 1
	}); err != nil {
		t.Fatal(err)
	}
	// The ceiling fires at t60 (stop amendment dispatched); one tick
	// later the dispatcher's amend unblocks the job (the blocked
	// interval settles and persists), and the process dies inside the
	// grace.
	var amendPath string
	clock.onSleep = func() {
		if clock.sleeps == 7 {
			item, err := store.RequestAmend("job-1", []byte("the answer to the question"))
			if err != nil {
				panic(err)
			}
			amendPath = item.Path
		}
	}
	// question at t0 (the block), t10..t50 (the no-ops) and t60 (the stop's
	// own read); at t70 the orchestrator has left the question (the hold
	// read before the delivery sees working, so the amend is delivered),
	// and the last entry stays for the stop's read after the unblock.
	team.statusScript = append(fillScript(nil, 7, "question"), fakeStatus{State: "working"})
	clock.stopAfter = 8
	clock.stop = errors.New("the first supervisor dies inside the ceiling grace")
	runUntilStop(t, sup, clock.stop)

	// The stop amendment dispatched once in process one; the job is
	// running (the amend unblocked it during the grace) and the blocked
	// interval settled at the unblock.
	if st := supState(t, store); st.Status != StatusRunning {
		t.Fatalf("status after the unblock = %s, want running", st.Status)
	}
	assertStopAmend(t, jobDir)
	if len(team.dispatches) != 3 || team.dispatches[1].Path != filepath.Join(jobDir, "stop-amend.md") || team.dispatches[2] != (fakeDispatch{Name: "orch-1", Path: amendPath, Amend: true}) {
		t.Fatalf("dispatches = %+v, want the brief, the stop amendment and the amend", team.dispatches)
	}
	if st := supState(t, store); st.BudgetBlockedMs != 70000 {
		t.Fatalf("settled blocked = %d ms, want 70000 (persisted for the restart)", st.BudgetBlockedMs)
	}

	// Process two: a fresh supervisor (its own monotonic clock) over the
	// same store. It re-derives the blocked total from the persisted
	// counter (already past the ceiling), starts the stop once more, and
	// runs it to the orchestrator's done.
	clock2 := &nsClock{now: clock.now}
	store.Clock = clock2
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done"}}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	exit, err := sup2.Run()
	st := supState(t, store)
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != ExitTimeout {
		t.Fatalf("exit = %d, want %d (the persisted blocked total is past the ceiling)", exit, ExitTimeout)
	}
	if st.Status != StatusTimeout {
		t.Fatalf("status = %s, want the terminal timeout", st.Status)
	}
	if st.Motivo == nil || *st.Motivo != blockedCeilingMotivo {
		got := "<nil>"
		if st.Motivo != nil {
			got = *st.Motivo
		}
		t.Fatalf("state motivo = %q, want %q", got, blockedCeilingMotivo)
	}
	// One stop amendment per supervisor process (two in total), nothing
	// else beyond the brief and the amend.
	stopPath := filepath.Join(jobDir, "stop-amend.md")
	stops := 0
	for _, d := range team.dispatches {
		if d.Path == stopPath && d.Amend {
			stops++
		}
	}
	if len(team.dispatches) != 4 || stops != 2 {
		t.Fatalf("dispatches = %+v, want the brief, one stop amendment per process and the amend", team.dispatches)
	}
	assertStopAmend(t, jobDir)
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "blocked_warning"); n != 1 {
		t.Fatalf("blocked_warnings = %d, want 1 (the log already has one)", n)
	}
	if n := countTipe(events, "terminal"); n != 1 {
		t.Fatalf("terminals = %d, want 1 (no duplicates)", n)
	}
	if n := countTipe(events, "cleanup"); n != 1 {
		t.Fatalf("cleanups = %d, want 1 (no duplicates)", n)
	}
	terminal := eventsLast(t, jobDir)
	if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != blockedCeilingMotivo {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	rep, err := os.ReadFile(filepath.Join(jobDir, "report.json"))
	if err != nil {
		t.Fatalf("report.json: %v", err)
	}
	if !strings.Contains(string(rep), `"status":"timeout"`) {
		t.Fatalf("report.json = %s", rep)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
	// The done beat the fresh 120 s grace at the second tick.
	if clock2.sleeps != 1 {
		t.Fatalf("restart sleeps = %d, want 1 (the done beat the fresh grace)", clock2.sleeps)
	}
}

// TestSuperviseRestartMidActive: a supervisor restart mid-active-interval
// re-derives the in-flight active interval from the persisted anchor: the
// warning fires at the absolute cumulative threshold (not 48 s into the
// new process), a second restart writes no duplicate warning, and the
// terminal is single.
func TestSuperviseRestartMidActive(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 1
		start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Format(time.RFC3339)
		st.IntervalStart = start.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	// Process one: 40 s of active time accumulated, then a crash before
	// the 80 % warning (48 s).
	team.statusScript = fillScript(nil, 4, "working")
	clock.stopAfter = 4
	clock.stop = errors.New("the first supervisor dies mid interval")
	runUntilStop(t, sup, clock.stop)
	if n := countTipe(mustEvents(t, jobDir), "timeout_warning"); n != 0 {
		t.Fatalf("warnings before the restart = %d, want 0", n)
	}

	// Process two: a fresh clock at the crash time. The in-flight active
	// interval is re-derived from the anchor (40 s already elapsed), so
	// the warning lands 8 s into this process (50 s cumulative), not 48
	// s. The store's clock continues with the process: the event stamps
	// follow the shared wall.
	clock2 := &nsClock{now: clock.now}
	store.Clock = clock2
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = []fakeStatus{{State: "working"}, {State: "working"}}
	clock2.stopAfter = 2
	clock2.stop = errors.New("the second supervisor dies after the warning")
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	runUntilStop(t, sup2, clock2.stop)
	events := mustEvents(t, jobDir)
	if n := countTipe(events, "timeout_warning"); n != 1 {
		t.Fatalf("warnings after the restart = %d, want 1", n)
	}
	warning := events[len(events)-1]
	if want := time.Date(2026, 10, 9, 10, 0, 50, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339); warning.TS != want {
		t.Fatalf("warning ts = %q, want %q (8 s into process two, 50 s cumulative)", warning.TS, want)
	}

	// Process three: a second restart must not double-count or re-warn:
	// the log guard keeps it to one warning, and the stop runs to the
	// terminal once (60 s cumulative, exactly the 100 % mark).
	clock3 := &nsClock{now: clock2.now}
	store.Clock = clock3
	team3 := &fakeTeam{spawnName: "orch-1"}
	team3.statusScript = []fakeStatus{{State: "working"}, {State: "done"}}
	sup3 := &Supervisor{Store: store, ID: "job-1", Ops: team3, Clock: clock3, Poll: 10 * time.Second, Friction: sup.Friction}
	exit, err := sup3.Run()
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != ExitTimeout {
		t.Fatalf("exit = %d, want %d", exit, ExitTimeout)
	}
	events = mustEvents(t, jobDir)
	if n := countTipe(events, "timeout_warning"); n != 1 {
		t.Fatalf("warnings after the second restart = %d, want 1 (no double count)", n)
	}
	if n := countTipe(events, "terminal"); n != 1 {
		t.Fatalf("terminals = %d, want 1", n)
	}
	terminal := eventsLast(t, jobDir)
	if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != "tempo esgotado" {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseBudgetBackwardsWallRestartClampsInFlight: a supervisor
// restart at a wall clock earlier than the persisted interval anchor
// (mid active interval) clamps the in-flight interval to zero — never
// negative — keeps the persisted cumulative totals, and the warning and
// the stop fire at the absolute cumulative thresholds, not at the
// process-local marks.
func TestSuperviseBudgetBackwardsWallRestartClampsInFlight(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 2
		start := time.Date(2026, 10, 9, 9, 59, 20, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Format(time.RFC3339)
		st.IntervalStart = start.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	// Process one: blocks at t0 (the 40 s active interval since
	// StartedAt settles), unblocks at t10 (the 10 s blocked interval
	// settles), running through t30, then a crash.
	team.statusScript = []fakeStatus{{State: "question"}, {State: "working"}, {State: "working"}, {State: "working"}}
	clock.stopAfter = 4
	clock.stop = errors.New("the first supervisor dies mid active interval")
	runUntilStop(t, sup, clock.stop)

	if st := supState(t, store); st.Status != StatusRunning || st.BudgetActiveMs != 40000 || st.BudgetBlockedMs != 10000 {
		t.Fatalf("state = %+v, want running with 40 s active and 10 s blocked settled", st)
	}

	// Process two: a fresh clock whose wall is 12 minutes BEHIND the
	// persisted anchor (10:00:10). The in-flight interval clamps to zero
	// and re-accumulates on the monotonic clock, the settled totals are
	// kept, and the cumulative marks decide: the 80 % alert of the 2 min
	// active budget (96 s) is first reached at 100 s cumulative (60 s
	// into this process, not 48 s), and the stop at 120 s.
	clock2 := &nsClock{now: time.Date(2026, 10, 9, 9, 58, 0, 0, time.FixedZone("-03:00", -3*3600))}
	store.Clock = clock2
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = fillScript(nil, 11, "working")
	team2.statusScript = append(team2.statusScript, fakeStatus{State: "done"})
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	exit, err := sup2.Run()
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != ExitTimeout {
		t.Fatalf("exit = %d, want %d (the cumulative active budget expired, not the wall)", exit, ExitTimeout)
	}
	if st := supState(t, store); st.Status != StatusTimeout {
		t.Fatalf("state = %+v, want the terminal timeout", st)
	}
	// The in-flight interval clamped to zero and re-accumulated on the
	// monotonic clock: the settled totals are untouched by the backwards
	// wall (no negative, no reset).
	if st := supState(t, store); st.BudgetActiveMs != 40000 || st.BudgetBlockedMs != 10000 {
		t.Fatalf("settled = active %d / blocked %d ms, want 40000 / 10000 (kept)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	events := mustEvents(t, jobDir)
	if n := countTipe(events, "timeout_warning"); n != 1 {
		t.Fatalf("warnings = %d, want 1 (at the cumulative 96 s, 60 s into process two)", n)
	}
	if n := countTipe(events, "blocked_warning"); n != 0 {
		t.Fatalf("blocked_warnings = %d, want 0 (10 s of blocked, far from the ceiling)", n)
	}
	assertStopAmend(t, jobDir)
	terminal := eventsLast(t, jobDir)
	if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != "tempo esgotado" {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseBudgetCrashWindowReanchor: the interval anchor the restart
// derives the in-flight interval from. With the atomic crossings
// (settleAndTransition) a crash at a crossing leaves the complete
// pre-crossing state — old status, old counters, old anchor — and the
// restart counts the in-flight interval in the old status's kind; the
// re-anchor rule no longer covers a crashed crossing. What it still covers
// is the legacy state: a blocked job started before the anchor field
// existed, whose blocked interval's start is unknown — the first budget
// check re-anchors to now and counts the in-flight interval as zero: no
// negative counter, no double count, and the alert and ceiling fire at
// the fresh 48/60 s marks. Control: a consistent blocked state (a
// recorded blocked re-anchor, not a missing one) keeps the anchor and
// counts exactly. All drive the watch loop ticks directly: a blocked job
// at supervisor start is the state under test, and the tick loop is the
// unit under test.
func TestSuperviseBudgetCrashWindowReanchor(t *testing.T) {
	// driveToEnd runs the watch loop ticks (no start wait, no spawn) until
	// one ends the job, failing the test on any error or unscripted
	// status; the exit code comes from the ending tick.
	driveToEnd := func(t *testing.T, sup *Supervisor) int {
		t.Helper()
		var code int
		for i := 0; ; i++ {
			var done bool
			var err error
			code, done, err = sup.tick()
			if err != nil {
				t.Fatalf("tick %d: %v", i+1, err)
			}
			if done {
				return code
			}
			sup.clockSleep(sup.poll())
		}
	}

	t.Run("legacy blocked state without an interval start re-anchors", func(t *testing.T) {
		var friction []string
		store, jobDir, _, team, sup := nsSup(t, 10*time.Second, &friction)
		if _, err := store.Record("job-1", func(st *State) {
			st.TimeoutMin = 1
			st.BlockedTimeoutMin = 1
			start := time.Date(2026, 10, 9, 9, 50, 0, 0, time.FixedZone("-03:00", -3*3600))
			st.StartedAt = start.Format(time.RFC3339)
		}); err != nil {
			t.Fatal(err)
		}
		// The legacy state: a job started before the interval anchor
		// existed is blocked — the anchor field is absent.
		if _, err := store.Transition("job-1", StatusBlocked); err != nil {
			t.Fatal(err)
		}
		motivo := "question"
		if _, err := store.Record("job-1", func(st *State) { st.Motivo = &motivo }); err != nil {
			t.Fatal(err)
		}
		// Blocked the whole run (the stop ends on its 120 s deadline at
		// t180, before any working read could unblock).
		team.statusScript = fillScript(nil, 19, "question")

		code := driveToEnd(t, sup)
		if code != ExitTimeout {
			t.Fatalf("code = %d, want %d", code, ExitTimeout)
		}
		st := supState(t, store)
		if st.Status != StatusTimeout {
			t.Fatalf("state = %+v, want the terminal timeout", st)
		}
		// The first budget check re-anchored the missing anchor to now and
		// counted the in-flight interval as zero: no negative counter, no
		// double count, and the alert and ceiling ran on the fresh marks.
		// The stop's crossing out of blocked into finishing (t180) settles
		// exactly the re-anchored blocked interval (10:00:00..10:03:00) into
		// the blocked clock and re-anchors to the crossing; the unknown
		// interval before the re-anchor stays lost, never double-counted.
		if st.IntervalStart != "2026-10-09T10:03:00-03:00" {
			t.Fatalf("interval_start = %q, want 10:03:00 (the crossing into finishing)", st.IntervalStart)
		}
		if st.BudgetActiveMs != 0 || st.BudgetBlockedMs != 180000 {
			t.Fatalf("counters = active %d / blocked %d ms, want 0 / 180000 (only the re-anchored blocked interval)", st.BudgetActiveMs, st.BudgetBlockedMs)
		}
		events := mustEvents(t, jobDir)
		if n := countTipe(events, "blocked_warning"); n != 1 {
			t.Fatalf("blocked_warnings = %d, want 1", n)
		}
		var warning Event
		for _, event := range events {
			if event.Tipo == "blocked_warning" {
				warning = event
			}
		}
		// The alert fires at 48 s of the re-anchored blocked interval
		// (t50), not at a stale anchor.
		if want := "2026-10-09T10:00:50-03:00"; warning.TS != want {
			t.Fatalf("warning ts = %q, want %q (the fresh 48 s mark)", warning.TS, want)
		}
		terminal := eventsLast(t, jobDir)
		if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != blockedCeilingMotivo {
			t.Fatalf("terminal refs = %+v", terminal.Refs)
		}
		assertStopAmend(t, jobDir)
		if len(friction) != 0 {
			t.Fatalf("friction = %v", friction)
		}
	})

	t.Run("a crash at the block crossing leaves the pre-crossing state", func(t *testing.T) {
		var friction []string
		store, _, clock, team, sup := nsSup(t, 10*time.Second, &friction)
		if _, err := store.Record("job-1", func(st *State) {
			st.TimeoutMin = 60 // far from the test's wall: no budget fires
			start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
			st.StartedAt = start.Format(time.RFC3339)
			st.IntervalStart = start.Format(time.RFC3339)
		}); err != nil {
			t.Fatal(err)
		}
		// Working t0..t30, the block crossing at t40 is the crash: the
		// single TransitionSettle write fails, so nothing of it lands.
		team.statusScript = fillScript(nil, 4, "working")
		team.statusScript = append(team.statusScript, fakeStatus{State: "question"})
		fired := crashAtStateWrite(t, StatusBlocked, "2026-10-09T10:00:40-03:00")
		if _, err := sup.Run(); err == nil {
			t.Fatal("the block crossing write did not crash the process")
		}
		if !*fired {
			t.Fatal("the crash injection never matched the block crossing write")
		}

		// The state is entirely pre-crossing: the old status, counters and
		// anchor — the crossing's write did not land.
		st := supState(t, store)
		if st.Status != StatusRunning {
			t.Fatalf("status = %s, want running (the pre-crossing state)", st.Status)
		}
		if st.BudgetActiveMs != 0 || st.BudgetBlockedMs != 0 {
			t.Fatalf("counters = active %d / blocked %d ms, want 0 / 0 (nothing of the crossing landed)", st.BudgetActiveMs, st.BudgetBlockedMs)
		}
		if st.IntervalStart != "2026-10-09T10:00:00-03:00" {
			t.Fatalf("anchor = %q, want the job-start anchor (nothing of the crossing landed)", st.IntervalStart)
		}

		// The restart at the crash time (10:00:40): working t0 keeps the
		// job running (the in-flight active interval counts from the
		// job-start anchor), question t10 blocks and settles — the totals
		// are the pre-crossing ones plus the in-flight interval of the old
		// (active) kind. Driven by tick (a Run() restart of a blocked job
		// would need the running wait).
		clock2 := &nsClock{now: clock.now}
		team2 := &fakeTeam{spawnName: "orch-1"}
		team2.statusScript = []fakeStatus{{State: "working"}, {State: "question"}}
		sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
		driveTicks(t, sup2, 2)

		st = supState(t, store)
		if st.Status != StatusBlocked {
			t.Fatalf("state = %+v, want blocked after the restart's block", st)
		}
		// Exact: 50 s of active (10:00:00..10:00:50, the old interval's
		// kind) settled at the restart's block — nothing lost, nothing
		// counted twice, nothing in the wrong counter.
		if st.BudgetActiveMs != 50000 || st.BudgetBlockedMs != 0 {
			t.Fatalf("settled = active %d ms / blocked %d ms, want 50000 / 0 (the old kind's in-flight interval)", st.BudgetActiveMs, st.BudgetBlockedMs)
		}
		if st.IntervalStart != "2026-10-09T10:00:50-03:00" {
			t.Fatalf("anchor = %q, want 10:00:50 (the restart's block re-anchor)", st.IntervalStart)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v", friction)
		}
	})

	t.Run("a consistent blocked state keeps the anchor and counts exactly", func(t *testing.T) {
		var friction []string
		store, jobDir, _, team, sup := nsSup(t, 10*time.Second, &friction)
		if _, err := store.Record("job-1", func(st *State) {
			st.TimeoutMin = 1
			st.BlockedTimeoutMin = 1
			start := time.Date(2026, 10, 9, 9, 59, 0, 0, time.FixedZone("-03:00", -3*3600))
			st.StartedAt = start.Format(time.RFC3339)
			// A recorded blocked re-anchor, 30 s before now: the
			// steady-state combination, not a legacy state.
			st.IntervalStart = time.Date(2026, 10, 9, 9, 59, 30, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339)
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Transition("job-1", StatusBlocked); err != nil {
			t.Fatal(err)
		}
		motivo := "question"
		if _, err := store.Record("job-1", func(st *State) { st.Motivo = &motivo }); err != nil {
			t.Fatal(err)
		}
		// Blocked through the ceiling at t30 (warning at t20); the stop's
		// own reads keep it blocked (working t40, done t50 ends the stop
		// before any unblock crossing).
		team.statusScript = fillScript(nil, 4, "question")
		team.statusScript = append(team.statusScript, fakeStatus{State: "working"}, fakeStatus{State: "done"})

		code := driveToEnd(t, sup)
		if code != ExitTimeout {
			t.Fatalf("code = %d, want %d", code, ExitTimeout)
		}
		// No re-anchor: the recorded anchor stands, and the in-flight
		// interval counts from it (30 s already elapsed). The only crossing
		// is the stop's exit from blocked into finishing (10:02:30), which
		// settles the whole blocked interval from the recorded anchor
		// (09:59:30..10:02:30) into the blocked clock and re-anchors there.
		st := supState(t, store)
		if st.Status != StatusTimeout {
			t.Fatalf("state = %+v, want the terminal timeout", st)
		}
		if st.IntervalStart != "2026-10-09T10:02:30-03:00" {
			t.Fatalf("interval_start = %q, want 10:02:30 (the crossing into finishing)", st.IntervalStart)
		}
		if st.BudgetActiveMs != 0 || st.BudgetBlockedMs != 180000 {
			t.Fatalf("counters = active %d / blocked %d ms, want 0 / 180000 (the blocked interval from the recorded anchor)", st.BudgetActiveMs, st.BudgetBlockedMs)
		}
		events := mustEvents(t, jobDir)
		if n := countTipe(events, "blocked_warning"); n != 1 {
			t.Fatalf("blocked_warnings = %d, want 1", n)
		}
		var warning Event
		for _, event := range events {
			if event.Tipo == "blocked_warning" {
				warning = event
			}
		}
		// 48 s cumulative (30 s before the restart + 20 s) is first
		// reached at t20: the exact count, not 48 s into the process.
		if want := "2026-10-09T10:00:20-03:00"; warning.TS != want {
			t.Fatalf("warning ts = %q, want %q (the exact cumulative count)", warning.TS, want)
		}
		terminal := eventsLast(t, jobDir)
		if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != blockedCeilingMotivo {
			t.Fatalf("terminal refs = %+v", terminal.Refs)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v", friction)
		}
	})
}

// TestSuperviseBlockedRestartTrustsRecordedJobStartAnchor: a block
// crossing at the job-start instant settles the 0 s interval and
// re-anchors to now — which equals StartedAt, with the anchor recorded
// (not absent). A restart of that blocked state must trust the recorded
// anchor (the in-flight blocked interval counts from it), never
// re-anchor it away: the re-anchor rule covers only the legacy state
// without an anchor. Catches the revert of the legacy-only narrowing of
// the crash-window rule (the old rule also re-anchored a blocked state
// whose anchor equalled StartedAt).
func TestSuperviseBlockedRestartTrustsRecordedJobStartAnchor(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 60 // far from the test's wall: no active budget fires
		st.BlockedTimeoutMin = 1
		start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Format(time.RFC3339)
		st.IntervalStart = start.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	// Process one: the block at t0 settles the 0 s active interval and
	// re-anchors to now — which is the job-start instant, so the recorded
	// anchor equals StartedAt; blocked t10, then a crash.
	team.statusScript = fillScript(nil, 2, "question")
	clock.stopAfter = 2
	clock.stop = errors.New("stop the watch loop")
	runUntilStop(t, sup, clock.stop)
	st := supState(t, store)
	if st.Status != StatusBlocked || st.IntervalStart != st.StartedAt || st.BudgetActiveMs != 0 {
		t.Fatalf("state = %+v, want blocked with the recorded job-start anchor", st)
	}

	// Process two: a fresh supervisor at 10:00:20 over the same store. The
	// recorded anchor is trusted (20 s of the blocked interval already
	// elapsed), so the 48 s alert lands at 10:00:50 and the 60 s ceiling
	// starts the stop at 10:01:00 — the re-anchored rule would have fired
	// them ten seconds later (10:01:00 / never).
	clock2 := &nsClock{now: clock.now}
	store.Clock = clock2
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = fillScript(nil, 6, "question")
	clock2.stopAfter = 6
	clock2.stop = errors.New("stop the watch loop")
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	runUntilStop(t, sup2, clock2.stop)

	if st := supState(t, store); st.IntervalStart != "2026-10-09T10:00:00-03:00" {
		t.Fatalf("anchor = %q, want the recorded job-start anchor (trusted, not re-anchored)", st.IntervalStart)
	}
	events := mustEvents(t, jobDir)
	if n := countTipe(events, "blocked_warning"); n != 1 {
		t.Fatalf("blocked_warnings = %d, want 1", n)
	}
	var warning Event
	for _, event := range events {
		if event.Tipo == "blocked_warning" {
			warning = event
		}
	}
	if want := "2026-10-09T10:00:50-03:00"; warning.TS != want {
		t.Fatalf("warning ts = %q, want %q (48 s from the trusted anchor, not the re-anchor)", warning.TS, want)
	}
	// The 60 s ceiling was reached (10:01:00): the stop amendment was
	// written, the re-anchored rule had not reached it by 10:01:10.
	assertStopAmend(t, jobDir)
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseBudgetBackwardsClockBlockedInterval: a backwards wall clock
// never moves the blocked budget — the interval's converted elapsed stays
// clamped at zero (captured once) and the alert and the ceiling are
// reached at the monotonic points, not earlier.
func TestSuperviseBudgetBackwardsClockBlockedInterval(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	clock.rewind = true
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 10
		st.BlockedTimeoutMin = 1
		start := time.Date(2026, 10, 9, 9, 59, 20, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Format(time.RFC3339)
		st.IntervalStart = start.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	// Blocked from t0; the wall clock jumps 10000 hours back after every
	// sleep from then on.
	team.statusScript = fillScript(nil, 6, "question")
	team.statusScript = append(team.statusScript, fakeStatus{State: "working"}, fakeStatus{State: "done"})

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitTimeout {
		t.Fatalf("exit = %d, want %d: a backwards wall clock must not change the blocked budget", exit, ExitTimeout)
	}
	if st := supState(t, store); st.Status != StatusTimeout {
		t.Fatalf("state = %+v", st)
	}
	events := mustEvents(t, jobDir)
	// The alert at 50 s of monotonic blocked elapsed (t50) and the
	// ceiling at 60 s (t60): the monotonic points, not earlier — a wall
	// comparison against the rewound clock would have fired at the first
	// tick.
	if n := countTipe(events, "blocked_warning"); n != 1 {
		t.Fatalf("blocked_warnings = %d, want 1", n)
	}
	terminal := eventsLast(t, jobDir)
	if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != blockedCeilingMotivo {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	// The 40 s active interval before the block settled exactly.
	if st := supState(t, store); st.BudgetActiveMs != 40000 {
		t.Fatalf("settled active = %d ms, want 40000", st.BudgetActiveMs)
	}
	assertStopAmend(t, jobDir)
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	// Ticks at 0, 10, ..., 70 s: the warning at t50, the ceiling at t60,
	// the done at t70 ends the stop — seven sleeps (the last tick ends
	// without one).
	if clock.sleeps != 7 {
		t.Fatalf("sleeps = %d, want 7 (t50 warned, t60 stopped, the done at t70 ends it)", clock.sleeps)
	}
}

// TestSuperviseBudgetMillisecondUnits: the clock's Monotonic readings are
// nanoseconds, and the persisted counters are whole milliseconds; the
// conversion happens exactly once (monoToMs), so an interval of exactly 1
// s adds exactly 1000 ms — not 1e9, which would expire every budget on
// the first tick — and a 59 s active job under a 1 min budget warns at
// the first tick at or past 48 000 ms and does not stop (59 s < 60 s).
func TestSuperviseBudgetMillisecondUnits(t *testing.T) {
	t.Run("one second of interval adds exactly 1000 ms", func(t *testing.T) {
		var friction []string
		store, _, clock, team, sup := nsSup(t, time.Second, &friction)
		if _, err := store.Record("job-1", func(st *State) {
			st.TimeoutMin = 10
			start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
			st.StartedAt = start.Format(time.RFC3339)
			st.IntervalStart = start.Format(time.RFC3339)
		}); err != nil {
			t.Fatal(err)
		}
		// working at t0, blocked at t1 (the 1 s active interval settles),
		// working at t2 (the 1 s blocked interval settles), then a crash.
		team.statusScript = []fakeStatus{{State: "working"}, {State: "question"}, {State: "working"}}
		clock.stopAfter = 3
		clock.stop = errors.New("stop the watch loop")
		runUntilStop(t, sup, clock.stop)

		st := supState(t, store)
		if st.BudgetActiveMs != 1000 || st.BudgetBlockedMs != 1000 {
			t.Fatalf("settled = active %d ms / blocked %d ms, want exactly 1000 / 1000 (a 1 s interval, not 1e9)", st.BudgetActiveMs, st.BudgetBlockedMs)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v", friction)
		}
	})

	t.Run("59 s active under a 1 min budget warns at 48000 ms and never stops", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, time.Second, &friction)
		if _, err := store.Record("job-1", func(st *State) {
			st.TimeoutMin = 1
			start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
			st.StartedAt = start.Format(time.RFC3339)
			st.IntervalStart = start.Format(time.RFC3339)
		}); err != nil {
			t.Fatal(err)
		}
		// 59 working ticks (t0..t58): the job stays 1 s under the budget.
		team.statusScript = fillScript(nil, 59, "working")
		clock.stopAfter = 59
		clock.stop = errors.New("stop the watch loop")
		runUntilStop(t, sup, clock.stop)

		events := mustEvents(t, jobDir)
		// The first tick at or past 48 000 ms of active time is t48 — the
		// warning is exactly there, and nothing stops (59 s < 60 s). With
		// the raw nanoseconds, the first tick would already be 1e9 "ms":
		// the warning would land at t1 and the stop with it.
		if n := countTipe(events, "timeout_warning"); n != 1 {
			t.Fatalf("warnings = %d, want 1 (at the first tick at or past 48 000 ms)", n)
		}
		var warning Event
		for _, event := range events {
			if event.Tipo == "timeout_warning" {
				warning = event
			}
		}
		if want := "2026-10-09T10:00:48-03:00"; warning.TS != want {
			t.Fatalf("warning ts = %q, want %q (not before 48 000 ms)", warning.TS, want)
		}
		if n := countTipe(events, "terminal"); n != 0 {
			t.Fatalf("terminals = %d, want 0 (59 s < 60 s)", n)
		}
		if _, err := os.Stat(filepath.Join(jobDir, "stop-amend.md")); !os.IsNotExist(err) {
			t.Fatalf("no stop amendment before 100 %% of the budget: %v", err)
		}
		if st := supState(t, store); st.Status != StatusRunning {
			t.Fatalf("state = %+v, want still running", st)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v", friction)
		}
	})
}

// TestSuperviseAnchorSubSecondPrecisionAcrossRestart: the interval anchor
// is written with RFC3339Nano (sub-second resolution), so a restart at a
// sub-second point mid-interval keeps the cumulative totals exact to the
// millisecond — a whole-second anchor would shift the re-derived in-flight
// interval by up to 999 ms, and the anchor itself loses the fraction.
func TestSuperviseAnchorSubSecondPrecisionAcrossRestart(t *testing.T) {
	var friction []string
	store, _, clock, team, sup := nsSup(t, 300*time.Millisecond, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 10
		start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Format(time.RFC3339)
		st.IntervalStart = start.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	// Process one: blocks at t0, unblocks at t1 (the blocked interval
	// settles at the sub-second wall 10:00:00.3 — the anchor keeps the
	// fraction), then a crash 300 ms later, mid active interval.
	team.statusScript = []fakeStatus{{State: "question"}, {State: "working"}}
	clock.stopAfter = 2
	clock.stop = errors.New("the first supervisor dies mid interval")
	runUntilStop(t, sup, clock.stop)

	st := supState(t, store)
	if st.BudgetBlockedMs != 300 {
		t.Fatalf("settled blocked = %d ms, want 300 (the 300 ms blocked interval)", st.BudgetBlockedMs)
	}
	if want := "2026-10-09T10:00:00.3-03:00"; st.IntervalStart != want {
		t.Fatalf("anchor = %q, want %q (the sub-second fraction kept)", st.IntervalStart, want)
	}

	// Process two: a fresh clock at the crash time (10:00:00.6), 300 ms
	// after the anchor. The in-flight active interval re-derives to
	// exactly 300 ms — with a whole-second anchor it would be 600 ms —
	// and settles at the block; the unblock settles another 300 ms. The
	// store's clock continues with the process so the settle stamps keep
	// the sub-second fraction of the shared wall.
	clock2 := &nsClock{now: clock.now}
	store.Clock = clock2
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = []fakeStatus{{State: "question"}, {State: "working"}, {State: "done", Report: writeOrchestratorReport(t, "# Report\n")}}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 300 * time.Millisecond, Friction: sup.Friction}
	exit, err := sup2.Run()
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (the done ends the job)", exit)
	}
	st = supState(t, store)
	if st.BudgetActiveMs != 300 || st.BudgetBlockedMs != 600 {
		t.Fatalf("settled = active %d ms / blocked %d ms, want 300 / 600 (exact to the millisecond across the restart)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if want := "2026-10-09T10:00:00.9-03:00"; st.IntervalStart != want {
		t.Fatalf("anchor = %q, want %q (the unblock re-anchor, fraction kept)", st.IntervalStart, want)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseBudgetPushRejectionSettlesInterval: the rejected-push block
// and its unblock go through the shared settle helper, like every other
// blocked-boundary path: the exact active interval settles at the
// rejection, the exact blocked interval settles at the lifting push, and
// neither crossing warns or double-settles (the retry rejection while
// blocked settles nothing).
func TestSuperviseBudgetPushRejectionSettlesInterval(t *testing.T) {
	// The gh create rule matches on the argv prefix (title and body
	// agnostic): the test's merge integration of the remote head makes
	// another commit the oldest of the job branch (the draft pull
	// request's title), so the create call the supervisor makes does not
	// carry the "feat: first" title the shared supGitGhRules pin.
	job := newJobSyncFixture(t, []fakecli.Rule{
		{Argv: ghListArgv(), Stdout: "[]\n"},
		{ArgvPrefix: true, Argv: ghCreateArgv("feat: first", supGitPRBody)[:9], Stdout: "Creating pull request\n" + gitSyncPRURL + "\n"},
	})
	root := t.TempDir()
	clock := &nsClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))}
	store := Open(root)
	store.Clock = clock
	if _, err := store.Start(prepID, supGitBrief); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(prepID, StatusPreparing); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(prepID, func(st *State) {
		st.Dir = job.dir
		st.Branch = supGitBranch
		st.Base = "main"
		st.TimeoutMin = 10
		st.StartedAt = clock.Now().Format(time.RFC3339)
		st.IntervalStart = clock.Now().Format(time.RFC3339)
		st.Orchestrator = "orch-1"
		st.WorkspaceID = "ws-1"
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(prepID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(prepID, EventIn{
		Tipo:   "worker_spawned",
		Resumo: "job orchestrator spawned",
		Refs:   map[string]string{"agente": "orch-1", "papel": "job-orchestrator"},
	}); err != nil {
		t.Fatal(err)
	}
	var friction []string
	team := &fakeTeam{spawnName: "orch-1"}
	sup := &Supervisor{
		Store:    store,
		ID:       prepID,
		Ops:      team,
		Clock:    clock,
		Git:      &job.sync,
		Herdr:    &fakeHerdr{},
		Friction: func(message string) { friction = append(friction, message) },
	}

	// Another clone advances the remote job branch past the local head,
	// so the local push is a non-fast-forward.
	other := filepath.Join(job.base.root, "other")
	if out, err := job.base.git(job.base.root, "clone", "-q", job.base.bare, other); err != nil {
		t.Fatalf("setup git clone: %s: %v", out, err)
	}
	for _, args := range [][]string{
		{"checkout", "-q", "-b", supGitBranch, "refs/remotes/origin/main"},
		{"commit", "-q", "--allow-empty", "-m", "other"},
		{"push", "-q", "origin", supGitBranch},
	} {
		if out, err := job.base.git(other, args...); err != nil {
			t.Fatalf("setup git %v in the second clone: %s: %v", args, out, err)
		}
	}
	job.commitIn(t, "feat: first")
	remoteSHA := job.remoteRef(t)
	if remoteSHA == "" {
		t.Fatal("the second clone did not advance the remote job branch")
	}

	// tickN runs n watch loop ticks (5 s default poll: a sync pass every
	// 6 ticks, the 30 s cadence).
	tickN := func(n int) {
		for i := 0; i < n; i++ {
			team.statusScript = append(team.statusScript, fakeStatus{State: "working"})
			code, done, err := sup.tick()
			if err != nil || done {
				t.Fatalf("tick %d: code=%d done=%v err=%v", i+1, code, done, err)
			}
			sup.clockSleep(sup.poll())
		}
	}

	// The first sync (tick 6, 25 s in): the push is rejected and the
	// running job blocks; the exact 25 s active interval settles at the
	// crossing.
	tickN(6)
	if got := job.remoteRef(t); got != remoteSHA {
		t.Fatalf("remote ref = %q, want the second clone's head (a force push)", got)
	}
	st, err := store.Snapshot(prepID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State.Status != StatusBlocked || st.State.Motivo == nil || *st.State.Motivo != "push_rejected" {
		t.Fatalf("state = %+v, want blocked for push_rejected", st.State)
	}
	if st.State.BudgetActiveMs != 25000 {
		t.Fatalf("settled active = %d ms, want 25000 (the exact interval at the rejected-push crossing)", st.State.BudgetActiveMs)
	}

	// The retry sync (tick 12) rejects again: one more push event, no
	// second blocked event, and no second settle while blocked.
	tickN(6)
	events, err := readEvents(filepath.Join(root, "jobs", prepID))
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "blocked"); n != 1 {
		t.Fatalf("blocked events = %d, want 1", n)
	}
	if n := countTipe(events, "push"); n != 2 {
		t.Fatalf("push events = %d, want 2", n)
	}
	if st, err := store.Snapshot(prepID); err != nil || st.State.BudgetBlockedMs != 0 {
		t.Fatalf("blocked counter after the retry = %+v, want 0 (a crossing while blocked settles nothing)", st)
	}

	// The orchestrator integrates the remote head and commits, the way
	// the job would; the next sync (tick 18) pushes fast-forward and
	// lifts the block: the exact 60 s blocked interval settles.
	for _, args := range [][]string{
		{"fetch", "-q", "origin"},
		{"merge", "-q", "--no-edit", "refs/remotes/origin/" + supGitBranch},
	} {
		if out, err := job.base.git(job.dir, args...); err != nil {
			t.Fatalf("setup git %v: %s: %v", args, out, err)
		}
	}
	mergeSHA := job.head(t)
	tickN(6)
	if got := job.remoteRef(t); got != mergeSHA {
		t.Fatalf("remote ref = %q, want the merged head %q", got, mergeSHA)
	}
	st, err = store.Snapshot(prepID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State.Status != StatusRunning || st.State.Motivo != nil {
		t.Fatalf("state = %+v, want running with the motivo cleared", st.State)
	}
	if st.State.BudgetBlockedMs != 60000 {
		t.Fatalf("settled blocked = %d ms, want 60000 (the exact interval at the lifting push)", st.State.BudgetBlockedMs)
	}
	if want := "2026-10-09T10:01:25-03:00"; st.State.IntervalStart != want {
		t.Fatalf("anchor = %q, want %q (the unblock re-anchor)", st.State.IntervalStart, want)
	}
	events, err = readEvents(filepath.Join(root, "jobs", prepID))
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "unblocked"); n != 1 {
		t.Fatalf("unblocked events = %d, want 1", n)
	}
	if n := countTipe(events, "timeout_warning"); n != 0 {
		t.Fatalf("timeout_warnings = %d, want 0 (25 s is far from the active budget)", n)
	}
	if n := countTipe(events, "blocked_warning"); n != 0 {
		t.Fatalf("blocked_warnings = %d, want 0 (60 s is far from the blocked ceiling)", n)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// mustEvents reads the job events, failing the test on error or absence.
// The while-blocked enforcement tests: blocking pauses the ACCRUAL of the
// active budget, not its enforcement — the settled active total is checked
// on every tick, in every status, so a job that crossed the active 80 %
// or 100 % mark before (or exactly at) a block entry still warns and
// stops while blocked. The crossing crash tests prove the same boundary
// from the other side: the status transition and the interval settle land
// in one locked write, so a crash at any crossing leaves a state the
// restart accounts exactly.

// crashAtStateWrite installs a renameAtomic wrapper that fails the first
// state.json write whose content is the given status with the given
// interval anchor — the crash at the crossing write (the combined
// transition-and-settle write) — and reports through the returned pointer
// whether the crash fired. The real rename is restored on test cleanup, the
// established crash-injection seam (the variable's own comment).
func crashAtStateWrite(t *testing.T, status, anchor string) *bool {
	t.Helper()
	fired := false
	previous := renameAtomic
	renameAtomic = func(from, to string) error {
		if !fired && filepath.Base(to) == "state.json" {
			var st State
			if raw, err := os.ReadFile(from); err == nil {
				if json.Unmarshal(raw, &st) == nil && st.Status == status && st.IntervalStart == anchor {
					fired = true
					return errors.New("crash at the crossing write")
				}
			}
		}
		return previous(from, to)
	}
	t.Cleanup(func() { renameAtomic = previous })
	return &fired
}

// driveTicks runs exactly n watch-loop ticks (no start wait, no spawn) and
// fails on any error or a done tick: the direct tick driving the restart
// accounting uses, mirroring driveToEnd for a restart that does not end the
// job (a Run() restart of a blocked job would sit in the running wait,
// which never passes while the job stays blocked).
func driveTicks(t *testing.T, sup *Supervisor, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		code, done, err := sup.tick()
		if err != nil {
			t.Fatalf("tick %d: %v", i+1, err)
		}
		if done {
			t.Fatalf("tick %d ended the job with code %d", i+1, code)
		}
		sup.clockSleep(sup.poll())
	}
}

// TestSuperviseBlockedActiveWarningWhileBlocked: active time charged past
// 80 % of the active budget (50 s of a 60 s budget) settles at the block
// entry, so the check of that tick — the first tick of the blocked
// interval, the job blocked before the warning could have landed — warns
// while blocked, exactly once, and does not stop (50 s is under 100 %).
func TestSuperviseBlockedActiveWarningWhileBlocked(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) { st.TimeoutMin = 1 }); err != nil {
		t.Fatal(err)
	}
	// Working t0..t40 (40 s, under the 48 s = 80 % mark), the block at
	// t50 settles the 50 s active interval, blocked t60..t70.
	team.statusScript = fillScript(nil, 5, "working")
	team.statusScript = fillScript(team.statusScript, 3, "question")
	clock.stopAfter = 8
	clock.stop = errors.New("stop the watch loop")
	runUntilStop(t, sup, clock.stop)

	events := mustEvents(t, jobDir)
	want := []string{"accepted", "worker_spawned", "blocked", "timeout_warning"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v (the warning lands after the block, on the first tick of the blocked interval)", got, want)
	}
	if n := countTipe(events, "timeout_warning"); n != 1 {
		t.Fatalf("timeout_warnings = %d, want 1", n)
	}
	if warning := events[3]; warning.Resumo != "80% of the active job budget used" {
		t.Fatalf("timeout_warning resumo = %q", warning.Resumo)
	}
	// The first tick of the blocked interval is t50 — the tick the block
	// entry settled the 50 s active interval.
	if want := time.Date(2026, 10, 9, 10, 0, 50, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339); events[3].TS != want {
		t.Fatalf("warning ts = %q, want %q (the first tick of the blocked interval)", events[3].TS, want)
	}
	if n := countTipe(events, "blocked_warning"); n != 0 {
		t.Fatalf("blocked_warnings = %d, want 0 (far from the blocked ceiling)", n)
	}
	if _, err := os.Stat(filepath.Join(jobDir, "stop-amend.md")); !os.IsNotExist(err) {
		t.Fatalf("no stop amendment under 100 %% of active: %v", err)
	}
	if st := supState(t, store); st.Status != StatusBlocked {
		t.Fatalf("state = %+v, want still blocked", st)
	}
	if st := supState(t, store); st.BudgetActiveMs != 50000 {
		t.Fatalf("settled active = %d ms, want 50000 (the block entry settled it)", st.BudgetActiveMs)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseBlockedActiveTimeoutWhileBlocked: the active budget is
// exhausted at the block entry (60 s of a 60 s budget settled at the
// crossing): the check of that tick — while blocked — starts the existing
// timeout stop (terminal timeout 9, motivo tempo esgotado, NOT the
// blocked ceiling) and the job ends through the stop's bounded 120 s
// grace at t180: no 24 h of blocked-ceiling immunity.
func TestSuperviseBlockedActiveTimeoutWhileBlocked(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) { st.TimeoutMin = 1 }); err != nil {
		t.Fatal(err)
	}
	// Working t0..t50 (the 48 s mark warns at t50, while active), the
	// block at t60 settles the exhausted 60 s active interval, the
	// orchestrator stays blocked through the stop's deadline at t180.
	team.statusScript = fillScript(nil, 6, "working")
	team.statusScript = fillScript(team.statusScript, 14, "question")

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitTimeout {
		t.Fatalf("exit = %d, want %d", exit, ExitTimeout)
	}
	st := supState(t, store)
	if st.Status != StatusTimeout {
		t.Fatalf("state = %+v, want the terminal timeout", st)
	}
	if st.Motivo == nil || *st.Motivo != "tempo esgotado" {
		got := "<nil>"
		if st.Motivo != nil {
			got = *st.Motivo
		}
		t.Fatalf("state motivo = %q, want %q (the active timeout, not the blocked ceiling)", got, "tempo esgotado")
	}
	events := mustEvents(t, jobDir)
	want := []string{"accepted", "worker_spawned", "timeout_warning", "blocked", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v (the block lands before the stop: the stop ran while blocked)", got, want)
	}
	if n := countTipe(events, "timeout_warning"); n != 1 {
		t.Fatalf("timeout_warnings = %d, want 1", n)
	}
	if n := countTipe(events, "blocked_warning"); n != 0 {
		t.Fatalf("blocked_warnings = %d, want 0 (far from the blocked ceiling)", n)
	}
	terminal := events[4]
	if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != "tempo esgotado" {
		t.Fatalf("terminal refs = %+v, want exit 9 / tempo esgotado", terminal.Refs)
	}
	// The block entry settled the exhausted active interval; the stop
	// ended the job at the 120 s grace deadline (t180), not at the
	// 1440 min blocked ceiling.
	if st.BudgetActiveMs != 60000 {
		t.Fatalf("settled active = %d ms, want 60000", st.BudgetActiveMs)
	}
	if len(team.dispatches) != 2 || team.dispatches[1] != (fakeDispatch{Name: "orch-1", Path: filepath.Join(jobDir, "stop-amend.md"), Amend: true}) {
		t.Fatalf("dispatches = %+v, want the brief and one stop amendment", team.dispatches)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	// Ticks at 0, 10, ..., 180 s: the stop starts at t60 (the block
	// entry) and the 120 s deadline ends it at t180 — 18 sleeps.
	if clock.sleeps != 18 {
		t.Fatalf("sleeps = %d, want 18 (the stop's bounded grace, not the 24 h blocked ceiling)", clock.sleeps)
	}
}

// TestSuperviseBlockedBelowActiveThresholdsNoStop: blocked below the
// active thresholds for a long wall time (30 s of a 60 s active budget,
// then blocked for 2 h — past 100 % of the total wall budget, far under
// both thresholds): no timeout_warning, no stop, and the blocked wall
// time never adds to the active total (the regression guard for the
// original behavior: a total-wall budget would warn and stop while
// blocked).
func TestSuperviseBlockedBelowActiveThresholdsNoStop(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) { st.TimeoutMin = 1 }); err != nil {
		t.Fatal(err)
	}
	// Working t0..t20 (30 s at the t30 block, under the 48 s mark), then
	// blocked for 2 h of wall (t40..t7230).
	team.statusScript = fillScript(nil, 3, "working")
	team.statusScript = fillScript(team.statusScript, 721, "question")
	clock.stopAfter = 724
	clock.stop = errors.New("stop the watch loop")
	runUntilStop(t, sup, clock.stop)

	events := mustEvents(t, jobDir)
	if n := countTipe(events, "timeout_warning"); n != 0 {
		t.Fatalf("timeout_warnings = %d, want 0 (30 s active, under the 48 s mark — the wall time never adds to it)", n)
	}
	if n := countTipe(events, "blocked_warning"); n != 0 {
		t.Fatalf("blocked_warnings = %d, want 0 (2 h of the 1440 min ceiling)", n)
	}
	if n := countTipe(events, "terminal"); n != 0 {
		t.Fatalf("terminals = %d, want 0 (no stop: the active budget is under 80 %% and the blocked ceiling far away)", n)
	}
	if _, err := os.Stat(filepath.Join(jobDir, "stop-amend.md")); !os.IsNotExist(err) {
		t.Fatalf("no stop amendment: %v", err)
	}
	st := supState(t, store)
	if st.Status != StatusBlocked {
		t.Fatalf("state = %+v, want still blocked", st)
	}
	// The 30 s active interval settled at the block entry; the 2 h
	// blocked interval is in flight (never settled) and never touched
	// the active total.
	if st.BudgetActiveMs != 30000 || st.BudgetBlockedMs != 0 {
		t.Fatalf("settled = active %d ms / blocked %d ms, want 30000 / 0", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseBlockedRestartActiveWarningOnce: a restart while blocked
// with the settled active total past 80 % keeps the warning to exactly
// one — the event log deduplicates the one already emitted (subtest
// one), and the new process emits it when the crossing crash left it
// unemitted (subtest two).
func TestSuperviseBlockedRestartActiveWarningOnce(t *testing.T) {
	t.Run("an emitted warning is not repeated by the restart", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
		if _, err := store.Record("job-1", func(st *State) { st.TimeoutMin = 1 }); err != nil {
			t.Fatal(err)
		}
		// Process one: working t0..t40, the block at t50 settles the 50 s
		// active interval (past 80 % of the 60 s budget) and warns,
		// blocked t60..t70, then a crash.
		team.statusScript = fillScript(nil, 5, "working")
		team.statusScript = fillScript(team.statusScript, 3, "question")
		clock.stopAfter = 8
		clock.stop = errors.New("stop the watch loop")
		runUntilStop(t, sup, clock.stop)
		if n := countTipe(mustEvents(t, jobDir), "timeout_warning"); n != 1 {
			t.Fatalf("warnings after the first run = %d, want 1", n)
		}

		// Process two: a fresh supervisor at the crash time over the same
		// store: the settled 50 s is still past 80 %, but the log already
		// carries the warning — nothing is repeated, and 50 s is under
		// 100 %, so no stop.
		clock2 := &nsClock{now: clock.now}
		team2 := &fakeTeam{spawnName: "orch-1"}
		team2.statusScript = fillScript(nil, 2, "question")
		clock2.stopAfter = 2
		clock2.stop = errors.New("stop the watch loop")
		sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
		runUntilStop(t, sup2, clock2.stop)

		if n := countTipe(mustEvents(t, jobDir), "timeout_warning"); n != 1 {
			t.Fatalf("warnings after the restart = %d, want 1 (the log already carries it)", n)
		}
		if _, err := os.Stat(filepath.Join(jobDir, "stop-amend.md")); !os.IsNotExist(err) {
			t.Fatalf("no stop amendment under 100 %% of active: %v", err)
		}
		if st := supState(t, store); st.Status != StatusBlocked {
			t.Fatalf("state = %+v, want still blocked", st)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v", friction)
		}
	})

	t.Run("an unemitted warning fires in the new process", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
		// The state the crossing crash leaves: the block transition and
		// its settle (50 s of active, settled at 10:00:00, the crossing)
		// landed, the process died before the warning tick, and the log
		// carries no timeout_warning.
		if _, err := store.Record("job-1", func(st *State) {
			st.TimeoutMin = 1
			st.StartedAt = time.Date(2026, 10, 9, 9, 59, 10, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339)
			st.IntervalStart = time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339)
			st.BudgetActiveMs = 50000
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Transition("job-1", StatusBlocked); err != nil {
			t.Fatal(err)
		}
		motivo := "question"
		if _, err := store.Record("job-1", func(st *State) { st.Motivo = &motivo }); err != nil {
			t.Fatal(err)
		}
		team.statusScript = fillScript(nil, 2, "question")
		clock.stopAfter = 2
		clock.stop = errors.New("stop the watch loop")
		runUntilStop(t, sup, clock.stop)

		events := mustEvents(t, jobDir)
		if n := countTipe(events, "timeout_warning"); n != 1 {
			t.Fatalf("warnings = %d, want 1 (the new process emits the unemitted one)", n)
		}
		// The settled 50 s is past 80 % but under 100 %: warn, do not
		// stop.
		if _, err := os.Stat(filepath.Join(jobDir, "stop-amend.md")); !os.IsNotExist(err) {
			t.Fatalf("no stop amendment under 100 %% of active: %v", err)
		}
		if st := supState(t, store); st.Status != StatusBlocked {
			t.Fatalf("state = %+v, want still blocked", st)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v", friction)
		}
	})
}

// TestSuperviseCrossingCrashSecondBlock: a crash at the second block
// crossing — after one full active→blocked→active cycle already settled —
// leaves only what the crossing's single write leaves. The transition and
// the settle land together (settleAndTransition), so the crash lands
// before it (or after it, which is the completed crossing): the state is
// entirely pre-crossing (old status, old counters, old anchor), and the
// restart accounts exactly — the settled totals are kept (no reset), the
// un-settled tail counts in the old status's kind (no double count, no
// misattribution).
func TestSuperviseCrossingCrashSecondBlock(t *testing.T) {
	var friction []string
	store, _, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 60 // far from the test's wall: no budget fires
	}); err != nil {
		t.Fatal(err)
	}
	// Working t0..t10, the block at t20 settles 20 s of active, working
	// t30 unblocks (the 10 s blocked interval settles), the second block
	// at t40 is the crash.
	team.statusScript = []fakeStatus{
		{State: "working"}, {State: "working"},
		{State: "question"}, {State: "working"},
		{State: "question"},
	}
	fired := crashAtStateWrite(t, StatusBlocked, "2026-10-09T10:00:40-03:00")
	if _, err := sup.Run(); err == nil {
		t.Fatal("the second block crossing write did not crash the first process")
	}
	if !*fired {
		t.Fatal("the crash injection never matched the second block crossing write")
	}

	// The restart: a fresh monotonic origin at the crash time (10:00:40).
	// Working t0 keeps the job running; question t10 blocks again and
	// settles — the accounting must be exact. Driven by tick (a Run()
	// restart of a blocked job sits in the running wait).
	clock2 := &nsClock{now: clock.now}
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = []fakeStatus{{State: "working"}, {State: "question"}}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	driveTicks(t, sup2, 2)

	st := supState(t, store)
	if st.Status != StatusBlocked {
		t.Fatalf("state = %+v, want blocked after the restart's block", st)
	}
	// Exact: t0..t40 is 40 s of active (20 s settled at the first block,
	// 20 s from the unblock to the second block) and t20..t30 is the 10 s
	// of blocked — nothing lost, nothing counted twice, nothing in the
	// wrong counter.
	if st.BudgetActiveMs != 40000 || st.BudgetBlockedMs != 10000 {
		t.Fatalf("settled = active %d ms / blocked %d ms, want 40000 / 10000 (exact: no reset, no double count, no misattribution)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if st.IntervalStart != "2026-10-09T10:00:50-03:00" {
		t.Fatalf("anchor = %q, want 10:00:50 (the restart's block re-anchor)", st.IntervalStart)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseCrossingCrashSecondUnblock: the same crash at the second
// unblock (resume): the combined write lands or does not land at all — a
// crash leaves the pre-crossing blocked state (settled at the block), and
// the restart accounts exactly: the settled totals are kept, and the
// un-settled blocked tail counts as blocked (the old status's kind), never
// as active.
func TestSuperviseCrossingCrashSecondUnblock(t *testing.T) {
	var friction []string
	store, _, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 60
	}); err != nil {
		t.Fatal(err)
	}
	// Working t0..t10, block t20 (settle 20 s active), working t30
	// (unblock, settle 10 s blocked), block t40 (settle 30 s active),
	// the second unblock at t50 is the crash.
	team.statusScript = []fakeStatus{
		{State: "working"}, {State: "working"},
		{State: "question"}, {State: "working"},
		{State: "question"}, {State: "working"},
	}
	fired := crashAtStateWrite(t, StatusRunning, "2026-10-09T10:00:50-03:00")
	if _, err := sup.Run(); err == nil {
		t.Fatal("the second unblock crossing write did not crash the first process")
	}
	if !*fired {
		t.Fatal("the crash injection never matched the second unblock crossing write")
	}

	// The restart at the crash time (10:00:50): working t0 resumes the
	// job, question t10 blocks and settles — the accounting must be
	// exact. Driven by tick (a Run() restart of a blocked job sits in
	// the running wait).
	clock2 := &nsClock{now: clock.now}
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = []fakeStatus{{State: "working"}, {State: "question"}}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	driveTicks(t, sup2, 2)

	st := supState(t, store)
	if st.Status != StatusBlocked {
		t.Fatalf("state = %+v, want blocked after the restart's block", st)
	}
	// Exact: active is t0..t20 (20 s) + t30..t40 (10 s) + t50..t60
	// (10 s) = 40 s, blocked is t20..t30 (10 s) + t40..t50 (10 s) = 20 s
	// — nothing lost, nothing counted twice, nothing in the wrong
	// counter.
	if st.BudgetActiveMs != 40000 || st.BudgetBlockedMs != 20000 {
		t.Fatalf("settled = active %d ms / blocked %d ms, want 40000 / 20000 (exact: no reset, no double count, no misattribution)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if st.IntervalStart != "2026-10-09T10:01:00-03:00" {
		t.Fatalf("anchor = %q, want 10:01:00 (the restart's block re-anchor)", st.IntervalStart)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseCrossingCrashSecondBlockBackwardsWall: the second-block
// crash with a backwards wall clock at the restart: the restart's
// in-flight interval clamps to zero (the established base-capture bound),
// so the settled totals stay exact and the un-settled tail is lost, not
// misattributed or double counted. The restart also performs no crossing
// the crash did not leave: the crash left the pre-crossing running state,
// which the first working read never crosses.
func TestSuperviseCrossingCrashSecondBlockBackwardsWall(t *testing.T) {
	var friction []string
	store, jobDir, _, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 60
	}); err != nil {
		t.Fatal(err)
	}
	// Same crash as TestSuperviseCrossingCrashSecondBlock: working
	// t0..t10, block t20 (settle 20 s active), unblock t30 (settle 10 s
	// blocked), the second block at t40 crashes.
	team.statusScript = []fakeStatus{
		{State: "working"}, {State: "working"},
		{State: "question"}, {State: "working"},
		{State: "question"},
	}
	fired := crashAtStateWrite(t, StatusBlocked, "2026-10-09T10:00:40-03:00")
	if _, err := sup.Run(); err == nil {
		t.Fatal("the second block crossing write did not crash the first process")
	}
	if !*fired {
		t.Fatal("the crash injection never matched the second block crossing write")
	}

	// The restart: the wall clock starts BEFORE the persisted anchor
	// (10:00:30), so the in-flight interval clamps to zero at the base
	// capture — the established bound. Working t0, question t10. Driven
	// by tick (a Run() restart of a blocked job sits in the running wait).
	clock2 := &nsClock{now: time.Date(2026, 10, 9, 9, 59, 0, 0, time.FixedZone("-03:00", -3*3600))}
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = []fakeStatus{{State: "working"}, {State: "question"}}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	driveTicks(t, sup2, 2)

	st := supState(t, store)
	if st.Status != StatusBlocked {
		t.Fatalf("state = %+v, want blocked after the restart's block", st)
	}
	// The settled totals are kept (no reset, no double count): the 20 s
	// of active and 10 s of blocked settled before the crash stay; the
	// un-settled tail (10 s of active, 10:00:30..10:00:40) clamps to
	// zero at the backwards-wall restart (the bound) and is never counted
	// in the blocked counter. The 10 s re-accumulated on the restart's
	// own monotonic (t0..t10) settles at the block.
	if st.BudgetActiveMs != 30000 || st.BudgetBlockedMs != 10000 {
		t.Fatalf("settled = active %d ms / blocked %d ms, want 30000 / 10000 (the kept totals, the clamped tail)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	events := mustEvents(t, jobDir)
	// No spurious crossing at the restart: one unblock (t30) and two
	// blocks (t20 and the restart's t10) — the crash left the running
	// state, which unblocks nothing.
	if n := countTipe(events, "unblocked"); n != 1 {
		t.Fatalf("unblocked = %d, want 1 (no spurious unblock at the restart)", n)
	}
	if n := countTipe(events, "blocked"); n != 2 {
		t.Fatalf("blocked = %d, want 2 (t20 and the restart's block — the crash left no third)", n)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSuperviseAmendUnblockCrashLeavesPreCrossingState: a crash at the
// amend unblock crossing (the dispatcher's amend delivered while the job
// is blocked) leaves the complete pre-crossing blocked state — the
// combined write did not land — and the restart settles the un-settled
// blocked tail in its own kind (the old status's kind, never the
// post-transition kind). Catches the revert of the amend unblock's
// separate Transition and settle writes.
func TestSuperviseAmendUnblockCrashLeavesPreCrossingState(t *testing.T) {
	var friction []string
	store, _, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 60 // far from the test's wall: no budget fires
		start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Format(time.RFC3339)
		st.IntervalStart = start.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	// question at t0 (the block crossing, the 0 s active interval settles
	// at the job-start anchor), then the dispatcher's amend (requested
	// between t0 and t10) is delivered at t10, once the hold read sees the
	// orchestrator out of the question, and unblocks the job — the crash.
	team.statusScript = []fakeStatus{{State: "question"}, {State: "working"}}
	clock.onSleep = func() {
		if clock.sleeps == 1 {
			if _, err := store.RequestAmend("job-1", []byte("the answer")); err != nil {
				panic(err)
			}
		}
	}
	fired := crashAtStateWrite(t, StatusRunning, "2026-10-09T10:00:10-03:00")
	if _, err := sup.Run(); err == nil {
		t.Fatal("the amend unblock crossing write did not crash the process")
	}
	if !*fired {
		t.Fatal("the crash injection never matched the amend unblock crossing write")
	}

	// The state is entirely pre-crossing: still blocked, the job-start
	// anchor, zero counters — nothing of the unblock landed.
	st := supState(t, store)
	if st.Status != StatusBlocked || st.BudgetActiveMs != 0 || st.BudgetBlockedMs != 0 || st.IntervalStart != "2026-10-09T10:00:00-03:00" {
		t.Fatalf("state = %+v, want the pre-crossing blocked state (anchor 10:00:00, counters 0 / 0)", st)
	}

	// The restart at the crash time (10:00:10): the pending amend
	// redelivers and its unblock crossing settles the 10 s blocked tail
	// (the old status's kind). Driven by tick (a Run() restart of a
	// blocked job needs the running wait).
	clock2 := &nsClock{now: clock.now}
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = []fakeStatus{{State: "working"}}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	driveTicks(t, sup2, 1)

	st = supState(t, store)
	if st.Status != StatusRunning {
		t.Fatalf("state = %+v, want running after the restart's unblock", st)
	}
	if st.BudgetActiveMs != 0 || st.BudgetBlockedMs != 10000 {
		t.Fatalf("settled = active %d ms / blocked %d ms, want 0 / 10000 (the blocked tail, never active)", st.BudgetActiveMs, st.BudgetBlockedMs)
	}
	if st.IntervalStart != "2026-10-09T10:00:10-03:00" {
		t.Fatalf("anchor = %q, want 10:00:10 (the restart's unblock re-anchor)", st.IntervalStart)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSupervisePushRejectionBlockCrashLeavesPreCrossingState: a crash at
// the rejected-push block crossing leaves the complete pre-crossing
// running state (the combined write did not land) and the restart settles
// the active tail at its own rejected-push block crossing — the old
// status's kind. Catches the revert of blockPushRejection's separate
// Transition and settle writes.
func TestSupervisePushRejectionBlockCrashLeavesPreCrossingState(t *testing.T) {
	job := newJobSyncFixture(t, supGitGhRules)
	root := t.TempDir()
	clock := &nsClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))}
	store := Open(root)
	store.Clock = clock
	if _, err := store.Start(prepID, supGitBrief); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(prepID, StatusPreparing); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(prepID, func(st *State) {
		st.Dir = job.dir
		st.Branch = supGitBranch
		st.Base = "main"
		st.TimeoutMin = 60 // far from the test's wall: no budget fires
		st.StartedAt = clock.Now().Format(time.RFC3339)
		st.IntervalStart = clock.Now().Format(time.RFC3339)
		st.Orchestrator = "orch-1"
		st.WorkspaceID = "ws-1"
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(prepID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(prepID, EventIn{
		Tipo:   "worker_spawned",
		Resumo: "job orchestrator spawned",
		Refs:   map[string]string{"agente": "orch-1", "papel": "job-orchestrator"},
	}); err != nil {
		t.Fatal(err)
	}
	// Another clone advances the remote job branch past the local head,
	// so the local push is a non-fast-forward.
	other := filepath.Join(job.base.root, "other")
	if out, err := job.base.git(job.base.root, "clone", "-q", job.base.bare, other); err != nil {
		t.Fatalf("setup git clone: %s: %v", out, err)
	}
	for _, args := range [][]string{
		{"checkout", "-q", "-b", supGitBranch, "refs/remotes/origin/main"},
		{"commit", "-q", "--allow-empty", "-m", "other"},
		{"push", "-q", "origin", supGitBranch},
	} {
		if out, err := job.base.git(other, args...); err != nil {
			t.Fatalf("setup git %v in the second clone: %s: %v", args, out, err)
		}
	}
	job.commitIn(t, "feat: first")
	var friction []string
	team := &fakeTeam{spawnName: "orch-1"}
	sup := &Supervisor{
		Store:    store,
		ID:       prepID,
		Ops:      team,
		Clock:    clock,
		Git:      &job.sync,
		Herdr:    &fakeHerdr{},
		Friction: func(message string) { friction = append(friction, message) },
	}

	// Working t0..t20 (the status reads); the first sync (the 6th tick,
	// 25 s in) rejects the push, and the block crossing of that tick is
	// the crash.
	team.statusScript = fillScript(nil, 5, "working")
	fired := crashAtStateWrite(t, StatusBlocked, "2026-10-09T10:00:25-03:00")
	if _, err := sup.Run(); err == nil {
		t.Fatal("the rejected-push block crossing write did not crash the process")
	}
	if !*fired {
		t.Fatal("the crash injection never matched the block crossing write")
	}

	// The state is entirely pre-crossing: still running, the job-start
	// anchor, zero counters — nothing of the block landed.
	st, err := store.Snapshot(prepID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State.Status != StatusRunning || st.State.BudgetActiveMs != 0 || st.State.BudgetBlockedMs != 0 || st.State.IntervalStart != "2026-10-09T10:00:00-03:00" {
		t.Fatalf("state = %+v, want the pre-crossing running state (anchor 10:00:00, counters 0 / 0)", st.State)
	}

	// The restart at the crash time (10:00:25): working t0..t20 keeps the
	// job running; the retry sync (the 6th tick, 10:00:50) rejects again
	// and its block crossing settles the 50 s active tail (the old
	// status's kind). Driven by tick (a Run() restart of a blocked job
	// would sit in the running wait).
	clock2 := &nsClock{now: clock.now}
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = fillScript(nil, 5, "working")
	sup2 := &Supervisor{Store: store, ID: prepID, Ops: team2, Clock: clock2, Git: &job.sync, Herdr: &fakeHerdr{}, Friction: sup.Friction}
	driveTicks(t, sup2, 6)

	st, err = store.Snapshot(prepID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State.Status != StatusBlocked || st.State.Motivo == nil || *st.State.Motivo != "push_rejected" {
		t.Fatalf("state = %+v, want blocked for push_rejected after the restart's block", st.State)
	}
	if st.State.BudgetActiveMs != 50000 || st.State.BudgetBlockedMs != 0 {
		t.Fatalf("settled = active %d ms / blocked %d ms, want 50000 / 0 (the old kind's in-flight interval)", st.State.BudgetActiveMs, st.State.BudgetBlockedMs)
	}
	if st.State.IntervalStart != "2026-10-09T10:00:50-03:00" {
		t.Fatalf("anchor = %q, want 10:00:50 (the restart's block re-anchor)", st.State.IntervalStart)
	}
	events, err := readEvents(filepath.Join(root, "jobs", prepID))
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "blocked"); n != 1 {
		t.Fatalf("blocked events = %d, want 1 (the crash left none; the restart's block)", n)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestSupervisePushRejectionUnblockCrashLeavesPreCrossingState: a crash at
// the successful-push unblock crossing leaves the complete pre-crossing
// blocked state (settled at the block, the remote already carrying the
// merged head) and the restart performs no spurious unblock (a
// push_rejected block is only lifted by a successful push, and the remote
// already carries the head) and keeps the settled totals. The unblock
// path is friction-only (a failed crossing write never ends the loop), so
// the crash is proven by the seam's fired flag and the entirely
// pre-crossing state; the watch loop is stopped by the clock right after
// the crash. Catches the revert of unblockPushRejection's separate
// Transition and settle writes.
func TestSupervisePushRejectionUnblockCrashLeavesPreCrossingState(t *testing.T) {
	// The gh create rule matches on the argv prefix (title and body
	// agnostic): the test's merge integration makes another commit the
	// oldest of the job branch (the draft pull request's title).
	job := newJobSyncFixture(t, []fakecli.Rule{
		{Argv: ghListArgv(), Stdout: "[]\n"},
		{ArgvPrefix: true, Argv: ghCreateArgv("feat: first", supGitPRBody)[:9], Stdout: "Creating pull request\n" + gitSyncPRURL + "\n"},
	})
	root := t.TempDir()
	clock := &nsClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))}
	store := Open(root)
	store.Clock = clock
	if _, err := store.Start(prepID, supGitBrief); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(prepID, StatusPreparing); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(prepID, func(st *State) {
		st.Dir = job.dir
		st.Branch = supGitBranch
		st.Base = "main"
		st.TimeoutMin = 60 // far from the test's wall: no budget fires
		st.StartedAt = clock.Now().Format(time.RFC3339)
		st.IntervalStart = clock.Now().Format(time.RFC3339)
		st.Orchestrator = "orch-1"
		st.WorkspaceID = "ws-1"
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(prepID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(prepID, EventIn{
		Tipo:   "worker_spawned",
		Resumo: "job orchestrator spawned",
		Refs:   map[string]string{"agente": "orch-1", "papel": "job-orchestrator"},
	}); err != nil {
		t.Fatal(err)
	}
	// Another clone advances the remote job branch past the local head,
	// so the local push is a non-fast-forward.
	other := filepath.Join(job.base.root, "other")
	if out, err := job.base.git(job.base.root, "clone", "-q", job.base.bare, other); err != nil {
		t.Fatalf("setup git clone: %s: %v", out, err)
	}
	for _, args := range [][]string{
		{"checkout", "-q", "-b", supGitBranch, "refs/remotes/origin/main"},
		{"commit", "-q", "--allow-empty", "-m", "other"},
		{"push", "-q", "origin", supGitBranch},
	} {
		if out, err := job.base.git(other, args...); err != nil {
			t.Fatalf("setup git %v in the second clone: %s: %v", args, out, err)
		}
	}
	job.commitIn(t, "feat: first")
	var friction []string
	team := &fakeTeam{spawnName: "orch-1"}
	sup := &Supervisor{
		Store:    store,
		ID:       prepID,
		Ops:      team,
		Clock:    clock,
		Git:      &job.sync,
		Herdr:    &fakeHerdr{},
		Friction: func(message string) { friction = append(friction, message) },
	}

	// Working t0..t80 (the status reads). The syncs at t25 and t55 reject
	// (the block crossing lands at t25); between t80 and t85 the
	// orchestrator integrates the remote head; the sync at t85 pushes
	// fast-forward, and the unblock crossing of that tick is the crash.
	// The loop stops at the 18th sleep (t85), right after the crash.
	team.statusScript = fillScript(nil, 17, "working")
	clock.onSleep = func() {
		if clock.sleeps == 16 {
			for _, args := range [][]string{
				{"fetch", "-q", "origin"},
				{"merge", "-q", "--no-edit", "refs/remotes/origin/" + supGitBranch},
			} {
				if out, err := job.base.git(job.dir, args...); err != nil {
					panic(fmt.Sprintf("setup git %v: %s: %v", args, out, err))
				}
			}
		}
	}
	clock.stopAfter = 18
	clock.stop = errors.New("stop the watch loop after the crash")
	fired := crashAtStateWrite(t, StatusRunning, "2026-10-09T10:01:25-03:00")
	runUntilStop(t, sup, clock.stop)
	if !*fired {
		t.Fatal("the crash injection never matched the unblock crossing write")
	}

	// The state is entirely pre-crossing: still blocked for push_rejected,
	// the block's anchor, the 25 s active tail settled at the block —
	// nothing of the unblock landed. The friction carries exactly the
	// crashed crossing's own line (the unblock path is friction-only).
	st, err := store.Snapshot(prepID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State.Status != StatusBlocked || st.State.Motivo == nil || *st.State.Motivo != "push_rejected" || st.State.BudgetActiveMs != 25000 || st.State.BudgetBlockedMs != 0 || st.State.IntervalStart != "2026-10-09T10:00:25-03:00" {
		t.Fatalf("state = %+v, want the pre-crossing blocked state (anchor 10:00:25, active 25000)", st.State)
	}
	if len(friction) != 1 || friction[0] != "job: git sync: crash at the crossing write" {
		t.Fatalf("friction = %v, want exactly the crashed crossing's line", friction)
	}
	friction = friction[:0] // the restart section must add no new lines

	// The restart at the crash time (10:01:30): the working reads keep
	// the job blocked (a push_rejected block is only lifted by a
	// successful push, and the remote already carries the head) — no
	// spurious unblock, the settled totals and the anchor stay.
	clock2 := &nsClock{now: clock.now}
	team2 := &fakeTeam{spawnName: "orch-1"}
	team2.statusScript = fillScript(nil, 3, "working")
	sup2 := &Supervisor{Store: store, ID: prepID, Ops: team2, Clock: clock2, Git: &job.sync, Herdr: &fakeHerdr{}, Friction: sup.Friction}
	driveTicks(t, sup2, 3)

	st, err = store.Snapshot(prepID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State.Status != StatusBlocked || st.State.BudgetActiveMs != 25000 || st.State.BudgetBlockedMs != 0 || st.State.IntervalStart != "2026-10-09T10:00:25-03:00" {
		t.Fatalf("state = %+v, want the pre-crossing blocked state kept (no spurious unblock)", st.State)
	}
	events, err := readEvents(filepath.Join(root, "jobs", prepID))
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "unblocked"); n != 0 {
		t.Fatalf("unblocked events = %d, want 0 (the unblock crossing did not land)", n)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

func mustEvents(t *testing.T, jobDir string) []Event {
	t.Helper()
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	return events
}
