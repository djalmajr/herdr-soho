package job

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests drive the real store and the real Supervisor.waitRunning
// against the failure walk's transient running: the rename seam holds the
// walk at its running save so the test observes the wait's polls while the
// job is transiently running with the failure event already in the log.

func TestWaitRunningIgnoresFailWalkRunning(t *testing.T) {
	root := t.TempDir()
	store := crashedStartState(t, root)
	// The store (the failure walk) and the supervisor (its polls) run in
	// two goroutines, so each has its own fake clock: the store's only
	// reads the time, the supervisor's only counts and reports sleeps.
	store.Clock = &fakeClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))}
	clock := &fakeClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))}
	// The private launch context this start issued: the guard runs for
	// this supervisor only.
	if _, err := issueLaunch(store, store.clock(), "job-1", "w9", "w9:p1"); err != nil {
		t.Fatal(err)
	}

	// The rename seam: right after the failure walk renames the transient
	// running state.json it signals and blocks until released, holding the
	// walk (and the job lock) while the job is transiently running with
	// the failure event already appended.
	previous := renameAtomic
	runningSeen := make(chan struct{})
	release := make(chan struct{})
	polledAfterRunning := make(chan struct{}, 1)
	var runningSaveDone atomic.Bool
	var releaseOnce sync.Once
	releaseWalk := func() { releaseOnce.Do(func() { close(release) }) }
	renameAtomic = func(from, to string) error {
		err := previous(from, to)
		if err == nil && filepath.Base(to) == "state.json" {
			if data, readErr := os.ReadFile(to); readErr == nil && bytes.Contains(data, []byte(`"status":"running"`)) {
				runningSaveDone.Store(true)
				close(runningSeen)
				<-release
			}
		}
		return err
	}
	t.Cleanup(func() {
		renameAtomic = previous
		releaseWalk()
	})

	// The supervisor's Sleep hands control back through the channel: it
	// reports every poll taken after the transient running save, with no
	// wall-clock pause anywhere. A poll taken before that save waits for
	// it, so the wait cannot spend its bounded polls on the preparing
	// state while the failure walk has not started yet (a scheduling race,
	// not the behavior under test).
	clock.onSleep = func() {
		if !runningSaveDone.Load() {
			select {
			case <-runningSeen:
			case <-time.After(10 * time.Second):
			}
		}
		if runningSaveDone.Load() {
			select {
			case polledAfterRunning <- struct{}{}:
			default:
			}
		}
	}

	failDone := make(chan error, 1)
	go func() {
		_, err := store.Fail("job-1", "the unconfirmed start failed", ExitFailed)
		failDone <- err
	}()

	type outcome struct {
		code int
		done bool
		err  error
	}
	result := make(chan outcome, 1)
	sup := &Supervisor{Store: store, ID: "job-1", Clock: clock, Poll: time.Second, PrivateLaunch: true}
	go func() {
		code, done, err := sup.waitRunning()
		result <- outcome{code: code, done: done, err: err}
	}()

	select {
	case <-runningSeen:
	case <-time.After(10 * time.Second):
		t.Fatal("the failure walk never saved the transient running")
	}

	// While the walk is paused at the transient running, the supervisor
	// polled at least once after that save: it called Sleep again instead
	// of returning.
	select {
	case <-polledAfterRunning:
	case <-time.After(10 * time.Second):
		t.Fatal("waitRunning stopped polling at the transient running")
	}

	// It did not return while the walk is paused: any return would have
	// taken the result channel.
	select {
	case res := <-result:
		t.Fatalf("waitRunning returned %d %v %v at the transient running, want a continued poll", res.code, res.done, res.err)
	default:
	}

	// Release the walk and let it reach failed.
	releaseWalk()
	select {
	case err := <-failDone:
		if err != nil {
			t.Fatalf("store.Fail: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the failure walk did not reach failed after the release")
	}

	// The wait ends at the terminal state with the failed outcome, never
	// (0, false, nil): the failure event stays in the log, so the running
	// snapshot was never acceptable to this supervisor.
	select {
	case res := <-result:
		if res.code != supervisorExit(StatusFailed) || !res.done || res.err != nil {
			t.Fatalf("waitRunning = %d %v %v, want %d true nil", res.code, res.done, res.err, supervisorExit(StatusFailed))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("waitRunning did not return after the job failed")
	}
}

// TestWaitRunningManualSuperviseUnchanged: a supervisor without the
// private launch context keeps the base behavior even with a launch
// record present: a running job whose log already holds a failure event
// is accepted at once.
func TestWaitRunningManualSuperviseUnchanged(t *testing.T) {
	root := t.TempDir()
	store := crashedStartState(t, root)
	clock := &fakeClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))}
	store.Clock = clock
	if _, err := issueLaunch(store, store.clock(), "job-1", "w9", "w9:p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition("job-1", StatusRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append("job-1", EventIn{Tipo: "failure", Resumo: "an earlier failure"}); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{Store: store, ID: "job-1", Clock: clock, Poll: time.Second}
	code, done, err := sup.waitRunning()
	if code != 0 || done || err != nil {
		t.Fatalf("waitRunning = %d %v %v, want 0 false nil (the base behavior)", code, done, err)
	}
	if clock.sleeps != 0 {
		t.Fatalf("sleeps = %d, want 0: the running snapshot is accepted at once", clock.sleeps)
	}
}

// TestWaitRunningAcceptsConfirmedRunning: with the private launch context,
// a job transitioned to running normally (no failure event) is accepted at
// once.
func TestWaitRunningAcceptsConfirmedRunning(t *testing.T) {
	root := t.TempDir()
	store := crashedStartState(t, root)
	clock := &fakeClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))}
	store.Clock = clock
	if _, err := issueLaunch(store, store.clock(), "job-1", "w9", "w9:p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition("job-1", StatusRunning); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{Store: store, ID: "job-1", Clock: clock, Poll: time.Second, PrivateLaunch: true}
	code, done, err := sup.waitRunning()
	if code != 0 || done || err != nil {
		t.Fatalf("waitRunning = %d %v %v, want 0 false nil", code, done, err)
	}
	if clock.sleeps != 0 {
		t.Fatalf("sleeps = %d, want 0: the running snapshot is accepted at once", clock.sleeps)
	}
}
