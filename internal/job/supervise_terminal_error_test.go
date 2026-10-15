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

// supervise_terminal_error_test.go pins the two guarantees of a store error
// in the terminal procedure: the friction log keeps the actual error text and
// the step that failed (the returned error is never wrapped: callers match
// its code with isCode), and a job whose terminal state was not saved never
// runs the terminal finalize (cleanup event, release, workspace close) nor
// retires its pending cancel. The failures are injected through the
// established renameAtomic seam, on the state.json write of one status.

// failStateWrite fails the first state.json write whose pending content
// satisfies match with err, and reports through the returned pointer whether
// it fired. The real rename is restored on test cleanup.
func failStateWrite(t *testing.T, err error, match func(pending State) bool) *bool {
	t.Helper()
	fired := false
	previous := renameAtomic
	renameAtomic = func(from, to string) error {
		if !fired && filepath.Base(to) == "state.json" {
			var pending State
			if raw, readErr := os.ReadFile(from); readErr == nil && json.Unmarshal(raw, &pending) == nil && match(pending) {
				fired = true
				return err
			}
		}
		return previous(from, to)
	}
	t.Cleanup(func() { renameAtomic = previous })
	return &fired
}

// requireNoTerminalFinalize asserts that finalize never ran: no cleanup
// event, no release, no gc, no workspace close, and the workspace is not
// recorded as closed.
func requireNoTerminalFinalize(t *testing.T, store *Store, jobDir string, team *fakeTeam, herdr *fakeHerdr) {
	t.Helper()
	if n := countTipe(mustEvents(t, jobDir), "cleanup"); n != 0 {
		t.Errorf("cleanup events = %d, want 0 (the terminal state was not saved)", n)
	}
	if team.releaseCalls != 0 || team.releaseTeamCalls != 0 || team.gcCalls != 0 {
		t.Errorf("release = %d team %d gc %d, want none", team.releaseCalls, team.releaseTeamCalls, team.gcCalls)
	}
	if len(herdr.CloseIDs) != 0 {
		t.Errorf("workspace close calls = %v, want none", herdr.CloseIDs)
	}
	if st := supState(t, store); st.WorkspaceClosed {
		t.Errorf("workspace_closed = true, want false")
	}
}

// A done outcome whose terminal state.json write fails: the actual error and
// the failing step reach friction, the job stays finishing with its terminal
// intent, and nothing past the terminal write (finalize) runs.
func TestSuperviseTerminalWriteErrorKeepsTextAndSkipsFinalize(t *testing.T) {
	var friction []string
	store, jobDir, _, team, herdr, sup := failBlockedSetup(t, &friction)
	report := writeOrchestratorReport(t, "# Report — job\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}
	// No budget left to retry the failed write.
	sup.FinishingLimits = finishingLimits{Budget: time.Second}
	injected := errors.New("injected terminal write error")
	fired := failStateWrite(t, injected, func(p State) bool { return p.Status == StatusDone })
	exit, err := sup.Run()
	if err == nil {
		t.Errorf("Run: exit = %d err = nil, want a non-nil error", exit)
	}
	if exit != ExitFailed {
		t.Errorf("exit = %d, want %d", exit, ExitFailed)
	}
	if !*fired {
		t.Fatal("the terminal write error was never injected")
	}
	st := supState(t, store)
	if st.Status != StatusFinishing || st.TerminalIntent == nil || st.TerminalIntent.Status != StatusDone {
		t.Errorf("state = %s intent=%+v, want finishing with the done intent", st.Status, st.TerminalIntent)
	}
	want := []string{"job: the supervisor could not record the terminal state done: injected terminal write error"}
	if !reflect.DeepEqual(friction, want) {
		t.Errorf("friction = %q, want %q", friction, want)
	}
	requireNoTerminalFinalize(t, store, jobDir, team, herdr)
}

// The entry into finishing fails (the write that carries the terminal
// intent): the error text and the step reach friction and the job stays
// running with no intent.
func TestSuperviseEnterFinishingErrorKeepsText(t *testing.T) {
	var friction []string
	store, jobDir, _, team, herdr, sup := failBlockedSetup(t, &friction)
	report := writeOrchestratorReport(t, "# Report — job\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}
	injected := errors.New("injected finishing write error")
	fired := failStateWrite(t, injected, func(p State) bool { return p.Status == StatusFinishing })
	exit, err := sup.Run()
	if err == nil {
		t.Errorf("Run: exit = %d err = nil, want a non-nil error", exit)
	}
	if exit != ExitFailed {
		t.Errorf("exit = %d, want %d", exit, ExitFailed)
	}
	if !*fired {
		t.Fatal("the finishing write error was never injected")
	}
	st := supState(t, store)
	if st.Status != StatusRunning || st.TerminalIntent != nil {
		t.Errorf("state = %s intent=%+v, want running with no intent", st.Status, st.TerminalIntent)
	}
	want := []string{"job: the supervisor could not enter finishing: injected finishing write error"}
	if !reflect.DeepEqual(friction, want) {
		t.Errorf("friction = %q, want %q", friction, want)
	}
	requireNoTerminalFinalize(t, store, jobDir, team, herdr)
}

// Store.Fail fails inside fail() (a running job, so not the blocked
// crossing): the actual error reaches friction, Run returns it with the
// conservative failed code, finalize never runs and the pending cancel is
// not retired, because the failed state was not saved.
func TestSuperviseFailStoreErrorKeepsTextAndSkipsFinalize(t *testing.T) {
	store, jobDir, clock, team := supFixture(t)
	team.ensureErr = errors.New("lane failed")
	if queued, err := store.RequestCancel("job-1", 60); !queued || err != nil {
		t.Fatalf("cancel: queued=%v err=%v", queued, err)
	}
	var friction []string
	sup := supSupervisor(store, clock, team, &friction)
	herdr := &fakeHerdr{}
	sup.Herdr = herdr
	injected := errors.New("injected failed write error")
	fired := failStateWrite(t, injected, func(p State) bool { return p.Status == StatusFailed })
	exit, err := sup.Run()
	if err == nil {
		t.Errorf("Run: exit = %d err = nil, want the Store.Fail error", exit)
	} else if !errors.Is(err, injected) {
		t.Errorf("Run error = %v, want the injected error unwrapped", err)
	}
	if exit != ExitFailed {
		t.Errorf("exit = %d, want %d", exit, ExitFailed)
	}
	if !*fired {
		t.Fatal("the failed write error was never injected")
	}
	st := supState(t, store)
	if st.Status != StatusFinishing || st.TerminalIntent == nil || st.TerminalIntent.Status != StatusFailed || st.TerminalIntent.Exit != ExitHerdr {
		t.Errorf("state = %s intent=%+v, want finishing with the failed intent (exit %d)", st.Status, st.TerminalIntent, ExitHerdr)
	}
	want := []string{"job: the supervisor could not record the failure: injected failed write error"}
	if !reflect.DeepEqual(friction, want) {
		t.Errorf("friction = %q, want %q", friction, want)
	}
	requireNoTerminalFinalize(t, store, jobDir, team, herdr)
	items, err := store.PendingControl("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != "cancel" {
		t.Errorf("pending control = %+v, want the one cancel still pending", items)
	}
}

// A restart over a running job that carries a terminal intent: the
// transition to finishing fails and the actual error reaches friction; the
// job stays running with its intent and finalize never runs.
func TestSuperviseRestartFinishingTransitionErrorKeepsText(t *testing.T) {
	var friction []string
	store, jobDir, _, team, herdr, sup := failBlockedSetup(t, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TerminalIntent = &TerminalIntent{Status: StatusDone}
	}); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected restart finishing error")
	fired := failStateWrite(t, injected, func(p State) bool { return p.Status == StatusFinishing })
	exit, err := sup.Run()
	if err == nil {
		t.Errorf("Run: exit = %d err = nil, want a non-nil error", exit)
	}
	if exit != ExitFailed {
		t.Errorf("exit = %d, want %d", exit, ExitFailed)
	}
	if !*fired {
		t.Fatal("the restart finishing write error was never injected")
	}
	st := supState(t, store)
	if st.Status != StatusRunning || st.TerminalIntent == nil || st.TerminalIntent.Status != StatusDone {
		t.Errorf("state = %s intent=%+v, want running with the done intent", st.Status, st.TerminalIntent)
	}
	want := []string{"job: the supervisor could not enter finishing: injected restart finishing error"}
	if !reflect.DeepEqual(friction, want) {
		t.Errorf("friction = %q, want %q", friction, want)
	}
	requireNoTerminalFinalize(t, store, jobDir, team, herdr)
}

// Control: with no injected failure the same done run reaches the terminal
// state and finalize, with no friction.
func TestSuperviseTerminalWriteControlRunsFinalize(t *testing.T) {
	var friction []string
	store, jobDir, _, team, herdr, sup := failBlockedSetup(t, &friction)
	report := writeOrchestratorReport(t, "# Report — job\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}
	exit, err := sup.Run()
	if err != nil || exit != 0 {
		t.Fatalf("Run: exit = %d err = %v, want 0 and nil", exit, err)
	}
	st := supState(t, store)
	if st.Status != StatusDone || !st.WorkspaceClosed {
		t.Fatalf("state = %s closed = %v, want done and the workspace closed", st.Status, st.WorkspaceClosed)
	}
	if n := countTipe(mustEvents(t, jobDir), "cleanup"); n != 1 {
		t.Fatalf("cleanup events = %d, want 1", n)
	}
	if team.releaseCalls != 1 || !reflect.DeepEqual(herdr.CloseIDs, []string{"w1"}) {
		t.Fatalf("release = %d close = %v, want the terminal release and close", team.releaseCalls, herdr.CloseIDs)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}
