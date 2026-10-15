package job

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// supervise_cancel_latency_test.go pins, with the test clock, the hard
// bound of the cancel: from the acceptance of job cancel to the durable
// terminal record (state.json in canceled, via finishOutcome), at most the
// grace plus one supervisor poll interval (defaultSupervisorPoll) plus the
// finishing budget (job_finishing_timeout). The grace starts at the
// acceptance and ends on the persisted deadline (a restart never restarts
// it); the stop ends on the first tick at or past the deadline, at most one
// poll after it; the final push and pull request step run inside the
// finishing budget and are cut when the budget is spent, so they add at
// most the budget after the grace. The worst-case subtests run every
// allowed slow step: the stop amendment consumes the grace limit and the
// queue delivery consumes the grace limit (one per subtest), and the final
// push step is cut by the spent finishing budget.
//
// T0 is the acceptance instant (the persisted cancel_requested_at); the
// supervisor's default poll applies (Poll zero), so the bound uses
// defaultSupervisorPoll. The fake clock advances only on Sleep and on the
// scripted cuts, so the wall time when Run returns is the wall time of the
// durable terminal record: nothing after the transition advances the clock.

// cancelLatencyGrace and cancelLatencyBudget are the test values of the
// grace and of the finishing budget (the seam standing in for the
// job_finishing_timeout recorded at job start).
var (
	cancelLatencyGrace  = 60 * time.Second
	cancelLatencyBudget = 2 * time.Minute
)

// workingStatuses returns n scripted working observations.
func workingStatuses(n int) []fakeStatus {
	out := make([]fakeStatus, n)
	for i := range out {
		out[i] = fakeStatus{State: "working"}
	}
	return out
}

// assertTerminalWithinHardBound asserts the hard bound of the cancel: the
// durable terminal record (the clock's wall time when Run returned) is no
// more than the grace, one default supervisor poll interval and the
// finishing budget past the acceptance.
func assertTerminalWithinHardBound(t *testing.T, clock *nsClock, t0 time.Time, grace, budget time.Duration) {
	t.Helper()
	bound := t0.Add(grace + defaultSupervisorPoll + budget)
	if clock.now.After(bound) {
		t.Fatalf("terminal at %s is past the hard bound %s (acceptance %s + grace %s + poll %s + budget %s)", clock.now, bound, t0, grace, defaultSupervisorPoll, budget)
	}
}

// assertCanceledTerminal is the common terminal facts of the latency
// scenarios: the state canceled with its motivo, the single terminal event
// with exit 21, and the single retired cancel under the given name. The
// pending control directory and the friction are asserted by each
// subtest: they differ (a still-pending queue request, the cut line).
func assertCanceledTerminal(t *testing.T, store *Store, jobDir string, retired string) {
	t.Helper()
	if st := supState(t, store); st.Status != StatusCanceled || st.Motivo == nil || *st.Motivo != "cancelado" {
		t.Fatalf("state = %+v, want canceled with the cancel motivo", st)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "terminal"); n != 1 {
		t.Fatalf("terminal events = %d, want 1", n)
	}
	if terminal := eventsLast(t, jobDir); terminal.Refs["exit"] != "21" || terminal.Refs["motivo"] != "cancelado" {
		t.Fatalf("terminal refs = %+v", terminal)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{retired}) {
		t.Fatalf("retired control = %v, want the single retired cancel %s", got, retired)
	}
}

func TestCancelTerminalWithinTheHardBound(t *testing.T) {
	t.Run("the stop amendment consumes the grace limit", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 0, &friction)
		sup.FinishingLimits = finishingLimits{Budget: cancelLatencyBudget}
		// The acceptance at T0 anchors the grace (cancel_requested_at,
		// cancel_deadline) before the supervisor's first tick.
		t0 := clock.now
		monoT0 := clock.mono
		if queued, err := store.RequestCancel("job-1", int(cancelLatencyGrace.Seconds())); !queued || err != nil {
			t.Fatalf("cancel: queued=%v err=%v", queued, err)
		}
		// The stop amendment (the second dispatch; the brief is the first)
		// runs to the grace limit: the scripted cut lands the shared clock
		// exactly on the persisted deadline before it returns.
		team.dispatchCut = func() error {
			if len(team.dispatches) < 2 {
				return nil
			}
			clock.mono = monoT0 + int64(cancelLatencyGrace)
			clock.now = t0.Add(cancelLatencyGrace)
			return errors.New("the stop amendment dispatch was cut")
		}
		team.statusScript = workingStatuses(3)
		exit, err := sup.Run()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if exit != ExitCanceled {
			t.Fatalf("exit = %d, want %d", exit, ExitCanceled)
		}
		// The cut landed the clock on the deadline and the stop ended on
		// that same tick: the durable terminal record is exactly at
		// T0 + grace, within the hard bound.
		if !clock.now.Equal(t0.Add(cancelLatencyGrace)) {
			t.Fatalf("terminal at %s, want %s (the cut landed on the deadline and the stop ended there)", clock.now, t0.Add(cancelLatencyGrace))
		}
		assertTerminalWithinHardBound(t, clock, t0, cancelLatencyGrace, cancelLatencyBudget)
		if clock.sleeps != 0 {
			t.Fatalf("sleeps = %d, want 0 (the spent deadline ends the stop on the discovery tick)", clock.sleeps)
		}
		// The stop amendment was the only bounded delivery, limited to the
		// full grace at its start, and its cut wrote exactly one friction
		// line with no delivery_uncertain mark (it is not a queue request).
		if len(team.bounds) != 1 || team.bounds[0] != cancelLatencyGrace {
			t.Fatalf("bounds = %v, want the full grace on the stop amendment", team.bounds)
		}
		if len(team.dispatches) != 2 {
			t.Fatalf("dispatches = %+v, want the brief and the stop amendment", team.dispatches)
		}
		assertStopAmend(t, jobDir)
		if len(friction) != 1 || friction[0] != "job: the supervisor cannot dispatch the stop amendment" {
			t.Fatalf("friction = %v, want exactly the cut line", friction)
		}
		events, err := readEvents(jobDir)
		if err != nil {
			t.Fatal(err)
		}
		if n := countTipe(events, "delivery_uncertain"); n != 0 {
			t.Fatalf("delivery_uncertain = %d, want none (no queue request ran)", n)
		}
		if n := countTipe(events, "push"); n != 0 {
			t.Fatalf("push events = %d, want none (no git on this fixture)", n)
		}
		if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
			t.Fatalf("pending control = %v, want only seq (the cancel retired)", got)
		}
		assertCanceledTerminal(t, store, jobDir, "cancel.json-000001")
	})

	t.Run("the queue delivery consumes the grace limit", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 0, &friction)
		sup.FinishingLimits = finishingLimits{Budget: cancelLatencyBudget}
		// A queue request pending before the acceptance.
		if _, err := store.RequestAmend("job-1", []byte("tighten the scope\n")); err != nil {
			t.Fatalf("amend: %v", err)
		}
		t0 := clock.now
		monoT0 := clock.mono
		if queued, err := store.RequestCancel("job-1", int(cancelLatencyGrace.Seconds())); !queued || err != nil {
			t.Fatalf("cancel: queued=%v err=%v", queued, err)
		}
		// The stop amendment (the second dispatch) runs fast and bounded to
		// the full grace; the queue delivery (the third dispatch) runs to
		// the grace limit: the scripted cut lands the shared clock exactly
		// on the persisted deadline before it returns.
		team.dispatchCut = func() error {
			if len(team.dispatches) < 3 {
				return nil
			}
			clock.mono = monoT0 + int64(cancelLatencyGrace)
			clock.now = t0.Add(cancelLatencyGrace)
			return errors.New("the delivery was cut by the grace")
		}
		team.statusScript = workingStatuses(3)
		exit, err := sup.Run()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if exit != ExitCanceled {
			t.Fatalf("exit = %d, want %d", exit, ExitCanceled)
		}
		// The cut delivery failed after the deadline passed: the single
		// delivery_uncertain mark, no amend_received, no retry, and the
		// stop ended on that same tick.
		if !clock.now.Equal(t0.Add(cancelLatencyGrace)) {
			t.Fatalf("terminal at %s, want %s (the cut landed on the deadline and the stop ended there)", clock.now, t0.Add(cancelLatencyGrace))
		}
		assertTerminalWithinHardBound(t, clock, t0, cancelLatencyGrace, cancelLatencyBudget)
		events, err := readEvents(jobDir)
		if err != nil {
			t.Fatal(err)
		}
		if n := countTipe(events, "delivery_uncertain"); n != 1 {
			t.Fatalf("delivery_uncertain = %d, want 1 (the cut marks, it does not retry)", n)
		}
		var marked *Event
		for i := range events {
			if events[i].Tipo == "delivery_uncertain" {
				marked = &events[i]
			}
		}
		if marked.Refs["seq_ref"] != "1" {
			t.Fatalf("delivery_uncertain refs = %+v, want seq_ref 1", marked.Refs)
		}
		if n := countTipe(events, "amend_received"); n != 0 {
			t.Fatalf("amend_received = %d, want none (the cut delivery records, it does not deliver)", n)
		}
		// Both deliveries ran bounded to the grace remaining at their
		// start (the full grace), and the request stays pending on the
		// terminal job.
		if len(team.bounds) != 2 || team.bounds[0] != cancelLatencyGrace || team.bounds[1] != cancelLatencyGrace {
			t.Fatalf("bounds = %v, want the full grace on the stop amendment and on the delivery", team.bounds)
		}
		if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"amend-000001.md", "seq"}) {
			t.Fatalf("pending control = %v, want the still-pending amend", got)
		}
		assertStopAmend(t, jobDir)
		if len(friction) != 0 {
			t.Fatalf("friction = %v, want none (the cut marks, it does not retry)", friction)
		}
		assertCanceledTerminal(t, store, jobDir, "cancel.json-000002")
	})

	t.Run("the final push is cut by the spent finishing budget", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 0, &friction)
		sup.FinishingLimits = finishingLimits{Budget: cancelLatencyBudget}
		t0 := clock.now
		// The finishing anchor sits one budget before the acceptance, so
		// the terminal procedure's budget is exactly spent when the stop
		// ends: the worst case in which the final push finds no time left.
		if _, err := store.Record("job-1", func(st *State) {
			st.FinishingStartedAt = t0.Add(-cancelLatencyBudget).Format(time.RFC3339Nano)
		}); err != nil {
			t.Fatal(err)
		}
		// A non-nil git sync so the final push step runs: it is cut by the
		// spent budget before any subprocess, so its Dir is never used.
		sup.Git = &GitSync{Dir: t.TempDir(), Branch: "job/job-1", Base: "main", Repo: "example-org/example-repo"}
		if queued, err := store.RequestCancel("job-1", int(cancelLatencyGrace.Seconds())); !queued || err != nil {
			t.Fatalf("cancel: queued=%v err=%v", queued, err)
		}
		team.statusScript = workingStatuses(15)
		exit, err := sup.Run()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if exit != ExitCanceled {
			t.Fatalf("exit = %d, want %d", exit, ExitCanceled)
		}
		// The stop ended on the persisted deadline and the terminal record
		// followed within the same tick: no time past the deadline, no time
		// past the budget.
		if !clock.now.Equal(t0.Add(cancelLatencyGrace)) {
			t.Fatalf("terminal at %s, want %s (the deadline ended the stop and the record followed at once)", clock.now, t0.Add(cancelLatencyGrace))
		}
		assertTerminalWithinHardBound(t, clock, t0, cancelLatencyGrace, cancelLatencyBudget)
		// The cut names the step and the budget: the push never ran past
		// the finishing budget, and a cut of the canceled outcome keeps
		// the canceled outcome (only a done job would be rewritten failed).
		found := false
		for _, line := range friction {
			if line == "job: finishing budget exhausted before the final push" {
				found = true
			}
		}
		if !found {
			t.Fatalf("friction = %v, want the cut line naming the final push", friction)
		}
		if st := supState(t, store); st.Status != StatusCanceled || st.FinishingSynced {
			t.Fatalf("state = %+v, want canceled without a completed final sync", st)
		}
		events, err := readEvents(jobDir)
		if err != nil {
			t.Fatal(err)
		}
		if n := countTipe(events, "push"); n != 0 {
			t.Fatalf("push events = %d, want none (the step was cut before the push)", n)
		}
		if n := countTipe(events, "commit"); n != 0 {
			t.Fatalf("commit events = %d, want none (no step ran)", n)
		}
		if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
			t.Fatalf("pending control = %v, want only seq (the cancel retired)", got)
		}
		assertCanceledTerminal(t, store, jobDir, "cancel.json-000001")
	})

	t.Run("a scenario with no slow step ends one poll at most past the deadline", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 0, &friction)
		sup.FinishingLimits = finishingLimits{Budget: cancelLatencyBudget}
		// The acceptance lands one second after the first tick boundary:
		// the supervisor's last tick saw no cancel, so the discovery runs
		// on the next tick (a lag of one second, at most one poll
		// interval), and nothing else is slow (no queue request, no git,
		// a fast stop amendment). T0 is the acceptance.
		var t0 time.Time
		clock.onSleep = func() {
			if clock.sleeps == 1 {
				if queued, err := store.RequestCancel("job-1", int(cancelLatencyGrace.Seconds())); !queued || err != nil {
					t.Fatalf("cancel: queued=%v err=%v", queued, err)
				}
				t0 = clock.now
				// One second after the tick boundary: the next tick is the
				// discovery, one second late.
				clock.mono += int64(time.Second)
				clock.now = clock.now.Add(time.Second)
			}
		}
		team.statusScript = workingStatuses(15)
		exit, err := sup.Run()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if exit != ExitCanceled {
			t.Fatalf("exit = %d, want %d", exit, ExitCanceled)
		}
		// The stop ends on the first tick at or past the persisted
		// deadline (T0 + grace, one second off the tick grid): the
		// durable terminal record is exactly T0 + grace + one second —
		// no more than T0 + grace + defaultSupervisorPoll.
		if !clock.now.Equal(t0.Add(cancelLatencyGrace + time.Second)) {
			t.Fatalf("terminal at %s, want %s (the first tick past the deadline)", clock.now, t0.Add(cancelLatencyGrace+time.Second))
		}
		if clock.now.After(t0.Add(cancelLatencyGrace + defaultSupervisorPoll)) {
			t.Fatalf("terminal at %s is past %s (no slow step: at most one poll past the deadline)", clock.now, t0.Add(cancelLatencyGrace+defaultSupervisorPoll))
		}
		// Thirteen 5 s polls from the discovery to the deadline tick.
		if clock.sleeps != 13 {
			t.Fatalf("sleeps = %d, want 13", clock.sleeps)
		}
		// The stop amendment dispatched once, bounded to the grace
		// remaining from the discovery (one second after the acceptance).
		if len(team.bounds) != 1 || team.bounds[0] != cancelLatencyGrace-time.Second {
			t.Fatalf("bounds = %v, want the grace minus the discovery lag on the stop amendment", team.bounds)
		}
		assertStopAmend(t, jobDir)
		events, err := readEvents(jobDir)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"accepted", "worker_spawned", "terminal", "cleanup"}; !reflect.DeepEqual(eventTipes(events), want) {
			t.Fatalf("event types = %v, want %v", eventTipes(events), want)
		}
		if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
			t.Fatalf("pending control = %v, want only seq (the cancel retired)", got)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v, want none", friction)
		}
		assertCanceledTerminal(t, store, jobDir, "cancel.json-000001")
	})
}
