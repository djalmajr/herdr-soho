package job

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// supervise_cancel_restart_test.go pins the cancel that survives a
// supervisor restart: the request stays pending in control/ through the
// grace, a second request during the grace is a no-op on the pending file,
// a restarted supervisor starts the stop path once (one stop amendment per
// process) and ends the grace on the acceptance's persisted deadline — a
// restart never restarts the grace — and the cancel retires only after the
// terminal outcome is recorded. A supervisor started on a job that is
// already canceled while the cancel is still pending retires it once,
// idempotently, and dispatches nothing. The timeout stop path restarts the
// same way without a duplicate timeout_warning.

var errCancelRestartCrash = errors.New("the first supervisor dies inside the grace")

func TestSuperviseCancelSurvivesSupervisorRestart(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if queued, err := store.RequestCancel("job-1", 60); !queued || err != nil {
		t.Fatalf("cancel: queued=%v err=%v", queued, err)
	}
	// The orchestrator keeps working through the first process.
	team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}}
	// A second cancel during the grace is a no-op (the queue is a single
	// pending file) and must not change any count.
	var secondQueued bool
	var secondErr error
	clock.onSleep = func() {
		if clock.sleeps == 1 {
			secondQueued, secondErr = store.RequestCancel("job-1", 60)
		}
	}
	// Process one: the first tick starts the stop and dispatches the stop
	// amendment, then the process dies inside the grace; the cancel stays
	// pending, because it retires only after the terminal outcome.
	clock.stopAfter = 1
	clock.stop = errCancelRestartCrash
	runUntilStop(t, sup, errCancelRestartCrash)
	if secondErr != nil {
		t.Fatalf("second cancel during the grace: err=%v, want the no-op", secondErr)
	}
	if secondQueued {
		t.Fatal("second cancel during the grace queued a new file, want the no-op on the pending one")
	}
	if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"cancel.json"}) {
		t.Fatalf("pending control after the crash = %v, want the still-pending cancel (the counter file lands only on the retirement)", got)
	}
	if st := supState(t, store); st.Status != StatusRunning {
		t.Fatalf("status after the interrupted grace = %s, want running", st.Status)
	}

	// Process two: a fresh supervisor (its own monotonic clock) over the
	// same store, whose orchestrator reports done; it finds the pending
	// cancel and starts the stop once.
	clock2 := &nsClock{now: clock.now}
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done"}}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	exit, err := sup2.Run()
	st := supState(t, store)
	t.Logf("restart exit=%d err=%v status=%s", exit, err, st.Status)
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != ExitCanceled {
		t.Fatalf("exit = %d, want %d (the in-flight cancel must survive the restart)", exit, ExitCanceled)
	}
	if st.Status != StatusCanceled {
		t.Fatalf("status = %s, want canceled", st.Status)
	}

	// The stop amendment dispatched exactly once per supervisor process:
	// the brief and the stop amendment in process one, the stop amendment
	// (and nothing else) in process two.
	stopPath := filepath.Join(jobDir, "stop-amend.md")
	stops := 0
	for _, d := range team.dispatches {
		if d.Path == stopPath && d.Amend {
			stops++
		}
	}
	if len(team.dispatches) != 3 || stops != 2 {
		t.Fatalf("dispatches = %+v, want the brief and one stop amendment per process (2 in total)", team.dispatches)
	}
	assertStopAmend(t, jobDir)

	// The cancel retired exactly once, after the terminal outcome: done
	// holds the single retired entry and control/ holds only the counter.
	if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
		t.Fatalf("pending control = %v, want only seq", got)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"cancel.json-000001"}) {
		t.Fatalf("retired control = %v, want the single retired cancel", got)
	}

	// Exactly one terminal event with the canceled outcome, and nothing a
	// canceled job's final sync performs beyond its no-op (no git on this
	// fixture): no push or checkpoint event.
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "terminal"); n != 1 {
		t.Fatalf("terminal events = %d, want 1", n)
	}
	if n := countTipe(events, "push"); n != 0 {
		t.Fatalf("push events = %d, want none (the final sync has no git)", n)
	}
	if n := countTipe(events, "checkpoint"); n != 0 {
		t.Fatalf("checkpoint events = %d, want none (no checkpoint commit)", n)
	}
	want := []string{"accepted", "worker_spawned", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	terminal := eventsLast(t, jobDir)
	if terminal.Refs["exit"] != "21" || terminal.Refs["motivo"] != "cancelado" {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
	// The stop in process two ended on the orchestrator's done at the
	// second tick, inside the persisted deadline (the acceptance plus the
	// grace, not a fresh measure): one sleep between the two ticks.
	if clock2.sleeps != 1 {
		t.Fatalf("restart sleeps = %d, want 1 (the done beat the persisted deadline)", clock2.sleeps)
	}
}

// cancelAnchor reads the persisted grace anchor straight from state.json
// (the raw fields, not the State projection), so the proof holds on a
// source that predates the fields: absent fields read as empty strings.
type cancelAnchor struct {
	RequestedAt string `json:"cancel_requested_at"`
	Deadline    string `json:"cancel_deadline"`
}

func readCancelAnchor(t *testing.T, jobDir string) cancelAnchor {
	t.Helper()
	var a cancelAnchor
	raw, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
	if err != nil {
		t.Fatalf("state.json: %v", err)
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatalf("state.json: %v", err)
	}
	return a
}

// TestSuperviseCancelRestartKeepsPersistedDeadline: process one accepts the
// cancel with a 60 s grace at T0 and dies inside the grace; process two
// starts with the wall clock one second past the persisted deadline
// (T0+60 s). The stop ends on the first check — the persisted deadline, not
// a fresh grace: no tick boundary is crossed, one terminal event, the
// cancel retires, and no friction (the deadline was readable).
func TestSuperviseCancelRestartKeepsPersistedDeadline(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	// Process one accepts the cancel with a 60 s grace at T0; the
	// acceptance anchors the grace in the state.
	if queued, err := store.RequestCancel("job-1", 60); !queued || err != nil {
		t.Fatalf("cancel: queued=%v err=%v", queued, err)
	}
	t0 := clock.now
	anchor := readCancelAnchor(t, jobDir)
	if want := t0.Format(time.RFC3339Nano); anchor.RequestedAt != want {
		t.Fatalf("cancel_requested_at = %q, want %q (the acceptance instant)", anchor.RequestedAt, want)
	}
	if want := t0.Add(60 * time.Second).Format(time.RFC3339Nano); anchor.Deadline != want {
		t.Fatalf("cancel_deadline = %q, want %q (the acceptance plus the grace)", anchor.Deadline, want)
	}
	// The orchestrator keeps working through the first process, which dies
	// inside the grace; the cancel stays pending.
	team.statusScript = []fakeStatus{{State: "working"}}
	clock.stopAfter = 1
	clock.stop = errCancelRestartCrash
	runUntilStop(t, sup, errCancelRestartCrash)
	if st := supState(t, store); st.Status != StatusRunning {
		t.Fatalf("status after the interrupted grace = %s, want running", st.Status)
	}

	// Process two starts with the wall clock one second past the persisted
	// deadline (T0+61 s): the first check must end the stop without
	// crossing a tick boundary, no fresh grace measured, and no stop
	// amendment dispatched (no time left: no attempt).
	clock2 := &nsClock{now: t0.Add(61 * time.Second)}
	team.statusScript = []fakeStatus{{State: "working"}}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	exit, err := sup2.Run()
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != ExitCanceled {
		t.Fatalf("exit = %d, want %d (the in-flight cancel must survive the restart)", exit, ExitCanceled)
	}
	if st := supState(t, store); st.Status != StatusCanceled {
		t.Fatalf("status = %s, want canceled", st.Status)
	}
	// The first checkBudget check ended the stop: the persisted deadline had
	// already passed, so no tick boundary was crossed.
	if clock2.sleeps != 0 {
		t.Fatalf("restart sleeps = %d, want 0 (the persisted deadline beat the restart, no fresh grace)", clock2.sleeps)
	}
	// The stop amendment dispatched exactly once: process one started the
	// stop inside the grace. Process two starts after the persisted
	// deadline, so there is no time left and no stop amendment dispatch —
	// the first check ends the stop.
	stopPath := filepath.Join(jobDir, "stop-amend.md")
	stops := 0
	for _, d := range team.dispatches {
		if d.Path == stopPath && d.Amend {
			stops++
		}
	}
	if len(team.dispatches) != 2 || stops != 1 {
		t.Fatalf("dispatches = %+v, want the brief and the single stop amendment of process one", team.dispatches)
	}
	assertStopAmend(t, jobDir)
	// The cancel retired exactly once, after the terminal outcome.
	if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
		t.Fatalf("pending control = %v, want only seq", got)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"cancel.json-000001"}) {
		t.Fatalf("retired control = %v, want the single retired cancel", got)
	}
	// Exactly one terminal event with the canceled outcome.
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "terminal"); n != 1 {
		t.Fatalf("terminal events = %d, want 1", n)
	}
	if want := []string{"accepted", "worker_spawned", "terminal", "cleanup"}; !reflect.DeepEqual(eventTipes(events), want) {
		t.Fatalf("event types = %v, want %v", eventTipes(events), want)
	}
	if terminal := eventsLast(t, jobDir); terminal.Refs["exit"] != "21" || terminal.Refs["motivo"] != "cancelado" {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	// The persisted deadline was readable in the restarted process: no
	// friction.
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
}

// TestSuperviseCancelLegacyStateMeasuresFreshGrace: without the persisted
// anchor the stop keeps the bound the persisted deadline replaces — the
// request's full grace on this process's own monotonic clock — and writes
// one friction line; an unreadable deadline does the same.
func TestSuperviseCancelLegacyStateMeasuresFreshGrace(t *testing.T) {
	runLegacyCancel := func(t *testing.T, store *Store, jobDir string, clock *nsClock, team *fakeTeam, sup *Supervisor, friction *[]string) {
		working := make([]fakeStatus, 7)
		for i := range working {
			working[i] = fakeStatus{State: "working"}
		}
		team.statusScript = working
		exit, err := sup.Run()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if exit != ExitCanceled {
			t.Fatalf("exit = %d, want %d", exit, ExitCanceled)
		}
		if st := supState(t, store); st.Status != StatusCanceled {
			t.Fatalf("state = %+v", st)
		}
		// The full fresh grace on this process's clock: six 10 s ticks.
		if clock.sleeps != 6 {
			t.Fatalf("sleeps = %d, want 6 (the full grace from this process)", clock.sleeps)
		}
		if len(*friction) != 1 || !strings.Contains((*friction)[0], "the persisted cancel deadline is absent or unreadable") {
			t.Fatalf("friction = %v, want the one line naming the missing anchor", *friction)
		}
		if terminal := eventsLast(t, jobDir); terminal.Refs["exit"] != "21" || terminal.Refs["motivo"] != "cancelado" {
			t.Fatalf("terminal refs = %+v", terminal.Refs)
		}
		if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
			t.Fatalf("pending control = %v, want only seq", got)
		}
		if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"cancel.json-000001"}) {
			t.Fatalf("retired control = %v, want the single retired cancel", got)
		}
	}

	t.Run("a state without the fields", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
		// The legacy acceptance: the pending file without the state anchors.
		ctl := filepath.Join(jobDir, "control")
		if err := os.MkdirAll(ctl, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ctl, "cancel.json"), []byte(`{"grace":60}`), 0o600); err != nil {
			t.Fatal(err)
		}
		runLegacyCancel(t, store, jobDir, clock, team, sup, &friction)
	})

	t.Run("an unreadable deadline", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
		if queued, err := store.RequestCancel("job-1", 60); !queued || err != nil {
			t.Fatalf("cancel: queued=%v err=%v", queued, err)
		}
		// A hostile state: the deadline does not parse.
		if _, err := store.Record("job-1", func(st *State) { st.CancelDeadline = "not-a-timestamp" }); err != nil {
			t.Fatal(err)
		}
		runLegacyCancel(t, store, jobDir, clock, team, sup, &friction)
	})
}

// TestSuperviseTerminalRestartRetiresPendingCancel: a supervisor started on
// a job that is already canceled while the cancel request is still pending
// (a crash between the terminal record and the retirement) retires the
// request once and idempotently and dispatches nothing.
func TestSuperviseTerminalRestartRetiresPendingCancel(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if queued, err := store.RequestCancel("job-1", 60); !queued || err != nil {
		t.Fatalf("cancel: queued=%v err=%v", queued, err)
	}
	// The crash-window state: the job is already canceled and the cancel
	// request was never retired.
	for _, to := range []string{StatusFinishing, StatusCanceled} {
		if _, err := store.Transition("job-1", to); err != nil {
			t.Fatal(err)
		}
	}
	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitCanceled {
		t.Fatalf("exit = %d, want %d (the terminal outcome's code)", exit, ExitCanceled)
	}
	if team.spawnCalls != 0 || len(team.dispatches) != 0 || len(team.sends) != 0 {
		t.Fatalf("the restart dispatched something: spawn=%d dispatches=%+v sends=%+v", team.spawnCalls, team.dispatches, team.sends)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
		t.Fatalf("pending control = %v, want only seq", got)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"cancel.json-000001"}) {
		t.Fatalf("retired control = %v, want the single retired cancel", got)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"accepted"}; !reflect.DeepEqual(eventTipes(events), want) {
		t.Fatalf("event types = %v, want %v (the restart appends nothing)", eventTipes(events), want)
	}
	// The second restart retires nothing more: the retirement is
	// idempotent.
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team, Clock: clock, Poll: 10 * time.Second, Friction: sup.Friction}
	if exit, err := sup2.Run(); err != nil || exit != ExitCanceled {
		t.Fatalf("second Run = %d %v, want %d", exit, err, ExitCanceled)
	}
	if len(team.dispatches) != 0 {
		t.Fatalf("the second restart dispatched something: %+v", team.dispatches)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"cancel.json-000001"}) {
		t.Fatalf("retired control = %v, want still the single entry", got)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
}

// TestSuperviseTimeoutStopRestartsOncePerProcess: the timeout stop path
// after a restart recomputes the budget from StartedAt, starts its stop
// once per process with the full grace on the process's own monotonic
// clock, ends timeout (exit 9), and writes no duplicate timeout_warning.
func TestSuperviseTimeoutStopRestartsOncePerProcess(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 1
		start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Add(-90 * time.Second).Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	working := make([]fakeStatus, 30)
	for i := range working {
		working[i] = fakeStatus{State: "working"}
	}
	team.statusScript = working
	// Process one: the budget is already past 100 %; the first tick warns
	// (the log has no warning yet) and starts the stop, and the process
	// dies inside the grace.
	clock.stopAfter = 1
	clock.stop = errors.New("the first supervisor dies inside the stop grace")
	runUntilStop(t, sup, clock.stop)

	// Process two: a fresh supervisor (its own monotonic clock) recomputes
	// the budget from StartedAt, starts the stop once and runs it to the
	// 120 s grace.
	clock2 := &nsClock{now: clock.now}
	team.statusScript = working
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team, Clock: clock2, Poll: 10 * time.Second, Friction: sup.Friction}
	exit, err := sup2.Run()
	st := supState(t, store)
	t.Logf("restart exit=%d err=%v status=%s", exit, err, st.Status)
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != ExitTimeout {
		t.Fatalf("exit = %d, want %d", exit, ExitTimeout)
	}
	if st.Status != StatusTimeout {
		t.Fatalf("status = %s, want timeout", st.Status)
	}
	// One stop amendment per supervisor process (two in total), nothing
	// else.
	stopPath := filepath.Join(jobDir, "stop-amend.md")
	stops := 0
	for _, d := range team.dispatches {
		if d.Path == stopPath && d.Amend {
			stops++
		}
	}
	if len(team.dispatches) != 3 || stops != 2 {
		t.Fatalf("dispatches = %+v, want the brief and one stop amendment per process (2 in total)", team.dispatches)
	}
	assertStopAmend(t, jobDir)
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "timeout_warning"); n != 1 {
		t.Fatalf("timeout_warnings = %d, want 1 (the log already has one)", n)
	}
	if n := countTipe(events, "terminal"); n != 1 {
		t.Fatalf("terminal events = %d, want 1", n)
	}
	terminal := eventsLast(t, jobDir)
	if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != "tempo esgotado" {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
}

// A job that fails while a cancel is pending (the stop never ran) is
// terminal: the cancel retires once with the failure, nothing stays pending
// and nothing is dispatched for it.
func TestSuperviseFailureRetiresPendingCancel(t *testing.T) {
	store, jobDir, clock, team := supFixture(t)
	team.ensureErr = errors.New("lane failed")
	if queued, err := store.RequestCancel("job-1", 60); !queued || err != nil {
		t.Fatalf("cancel: queued=%v err=%v", queued, err)
	}
	var friction []string
	sup := supSupervisor(store, clock, team, &friction)
	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitHerdr {
		t.Fatalf("exit = %d, want %d", exit, ExitHerdr)
	}
	if st := supState(t, store); st.Status != StatusFailed {
		t.Fatalf("state = %+v", st)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
		t.Fatalf("pending control = %v, want only seq", got)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"cancel.json-000001"}) {
		t.Fatalf("retired control = %v", got)
	}
	if team.spawnCalls != 0 {
		t.Fatalf("spawn calls = %d, want 0", team.spawnCalls)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}
