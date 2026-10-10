package job

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// crashedStartState leaves a job exactly where a crashed start left it:
// accepted, moved to preparing with the preparing event the starter
// appends, with the create intent recorded (the label and the preexisting
// ids) but no workspace id.
func crashedStartState(t *testing.T, root string) *Store {
	t.Helper()
	s := Open(root)
	if _, err := s.Start("job-1", []byte(briefA)); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := s.Transition("job-1", StatusPreparing); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if _, err := s.Append("job-1", EventIn{Tipo: "preparing", Resumo: "preparing checkout and worktree"}); err != nil {
		t.Fatalf("preparing event: %v", err)
	}
	if _, err := s.Record("job-1", func(st *State) {
		st.WorkspaceLabel = "job-job-1"
		st.WorkspacePreexisting = []string{"w0"}
	}); err != nil {
		t.Fatalf("record the intent: %v", err)
	}
	return s
}

func countFailureEvents(t *testing.T, dir string) int {
	t.Helper()
	events, err := readEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	failures := 0
	for _, event := range events {
		if event.Tipo == "failure" {
			failures++
		}
	}
	return failures
}

// failAndTerminalEvents returns the failure and the terminal events of the
// job log, looked up by tipo: the start failure paths append the cleanup
// event after the terminal, so the positional last two are no longer the
// pair.
func failAndTerminalEvents(t *testing.T, root, id string) (failure, terminal Event) {
	t.Helper()
	events, err := readEvents(filepath.Join(root, "jobs", id))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		switch event.Tipo {
		case "failure":
			failure = event
		case "terminal":
			terminal = event
		}
	}
	if failure.Seq == 0 || terminal.Seq == 0 || failure.Seq >= terminal.Seq {
		t.Fatalf("events = %+v, want the failure before the terminal", events)
	}
	return failure, terminal
}

// assertCleanupReport checks the start failure's workspace outcome: the
// cleanup event last in the log with the supervisor's finalize resumo, and
// the report's limpeza.workspace value ("" is the absent field: the failure
// left no Herdr workspace).
func assertCleanupReport(t *testing.T, root, id, workspace string) {
	t.Helper()
	events, err := readEvents(filepath.Join(root, "jobs", id))
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if workspace == "" {
		if last.Tipo == "cleanup" {
			t.Fatalf("cleanup event on a failure without a workspace: %+v", last)
		}
		if rep := reportJSON(t, root, id); rep.Limpeza.Workspace != "" {
			t.Fatalf("limpeza = %+v, want no workspace", rep.Limpeza)
		}
		return
	}
	if last.Tipo != "cleanup" || last.Resumo != "processes released; worktree kept" {
		t.Fatalf("last event = %+v, want the cleanup with the finalize resumo", last)
	}
	if rep := reportJSON(t, root, id); rep.Limpeza.Workspace != workspace {
		t.Fatalf("limpeza = %+v, want the workspace %q", rep.Limpeza, workspace)
	}
}

// TestRecoverStartCrashAfterCreate pins the old gap end to end: the crash
// in the create window is detectable (the job is preparing, the intent was
// recorded before the create, and the start.lock is free), and the recovery
// closes exactly the new labeled workspace, ends the job failed with the
// report, and records the id and the close.
func TestRecoverStartCrashAfterCreate(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	f.cloneCheckout()
	fake := startFake()
	// The Herdr already lists one job-job-1 workspace (w0) before the
	// start: the create intent records it as preexisting.
	fake.Workspaces = []herdrWorkspace{{ID: "w0", Label: "job-job-1", PaneCount: 1}}
	stateRoot := t.TempDir()
	var friction []string
	s := startStarter(t, f, fake, stateRoot, &friction)

	afterWorkspaceCreate = func() { panic("simulated crash after workspace create") }
	t.Cleanup(func() { afterWorkspaceCreate = nil })
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("the crash hook did not panic")
			}
		}()
		s.Start(startRequest("main", briefA))
	}()
	afterWorkspaceCreate = nil

	store := Open(stateRoot)
	snap, err := store.Snapshot("job-1")
	if err != nil {
		t.Fatal(err)
	}
	// The job is left preparing, with the intent recorded before the
	// create (the label and the preexisting ids are in the state at the
	// crash point, after Herdr.Create ran) and no workspace id.
	if snap.State.Status != StatusPreparing || snap.State.WorkspaceLabel != "job-job-1" ||
		!reflect.DeepEqual(snap.State.WorkspacePreexisting, []string{"w0"}) || snap.State.WorkspaceID != "" {
		t.Fatalf("crashed state = %+v", snap.State)
	}
	// The panic released the start lock: the free lock is the crash
	// signal, so a fresh try-lock succeeds.
	jobDir, err := Dir(stateRoot, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	lockFile, err := tryStartLock(jobDir)
	if err != nil {
		t.Fatalf("start.lock is not free after the panic: %v", err)
	}
	closeStartLock(lockFile)

	// The Herdr still lists the preexisting job-job-1 workspace (w0,
	// recorded in the intent) and the crashed start's new one (w9).
	fake.Workspaces = append(fake.Workspaces, herdrWorkspace{ID: "w9", Label: "job-job-1", PaneCount: 1})
	st, recovered, err := RecoverStart(store, "job-1", fake, func(m string) { friction = append(friction, m) }, nil)
	if err != nil || !recovered {
		t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, recovered)
	}
	if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "job: start interrupted" {
		t.Fatalf("state = %q motivo %v", st.Status, st.Motivo)
	}
	if st.WorkspaceID != "w9" || !st.WorkspaceClosed {
		t.Fatalf("workspace = %q closed %v, want w9 / true", st.WorkspaceID, st.WorkspaceClosed)
	}
	if !reflect.DeepEqual(fake.CloseIDs, []string{"w9"}) {
		t.Fatalf("CloseIDs = %v, want exactly [w9]", fake.CloseIDs)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "jobs", "job-1", "report.json")); err != nil {
		t.Fatalf("report.json: %v", err)
	}
	assertFailedReport(t, stateRoot, "job-1", "job: start interrupted")
	if countFailureEvents(t, jobDir) != 1 {
		t.Fatalf("failure events, want exactly one")
	}
	failEvent, terminal := failAndTerminalEvents(t, stateRoot, "job-1")
	if failEvent.Refs["motivo"] != "job: start interrupted" || terminal.Refs["exit"] != "19" {
		t.Fatalf("failure = %+v terminal = %+v", failEvent, terminal)
	}
	// The recovery closed the workspace: the log ends with the cleanup
	// event and the report records it.
	assertCleanupReport(t, stateRoot, "job-1", "closed")
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
}

// TestRecoverStartAmbiguousFailsClosed: two new labeled rows (an ambiguous
// identity) and an unreadable list both close nothing, say so to friction,
// and still end the job failed.
func TestRecoverStartAmbiguousFailsClosed(t *testing.T) {
	t.Run("two new labeled rows close nothing", func(t *testing.T) {
		root := t.TempDir()
		store := crashedStartState(t, root)
		var friction []string
		fake := &fakeHerdr{Workspaces: []herdrWorkspace{
			{ID: "w0", Label: "job-job-1", PaneCount: 1},
			{ID: "w1", Label: "job-job-1", PaneCount: 1},
			{ID: "w2", Label: "job-job-1", PaneCount: 1},
		}}
		st, recovered, err := RecoverStart(store, "job-1", fake, func(m string) { friction = append(friction, m) }, nil)
		if err != nil || !recovered {
			t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, recovered)
		}
		if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "job: start interrupted" ||
			st.WorkspaceID != "" || st.WorkspaceClosed {
			t.Fatalf("state = %+v", st)
		}
		if len(fake.CloseIDs) != 0 {
			t.Fatalf("CloseIDs = %v, want none", fake.CloseIDs)
		}
		if len(friction) != 1 || friction[0] != "job: the Herdr workspace may be left open: its identity could not be established" {
			t.Fatalf("friction = %v", friction)
		}
		assertFailedReport(t, root, "job-1", "job: start interrupted")
		assertCleanupReport(t, root, "job-1", "open")
	})
	t.Run("a list error closes nothing", func(t *testing.T) {
		root := t.TempDir()
		store := crashedStartState(t, root)
		var friction []string
		// The rows would decide a single candidate; the unreadable list
		// must refuse the decision, not decide from a partial read.
		fake := &fakeHerdr{
			ListErr:    herdrExit("job: herdr workspace list failed"),
			Workspaces: []herdrWorkspace{{ID: "w9", Label: "job-job-1", PaneCount: 1}},
		}
		st, recovered, err := RecoverStart(store, "job-1", fake, func(m string) { friction = append(friction, m) }, nil)
		if err != nil || !recovered {
			t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, recovered)
		}
		if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "job: start interrupted" ||
			st.WorkspaceID != "" || st.WorkspaceClosed {
			t.Fatalf("state = %+v", st)
		}
		if fake.ListReads != 1 || len(fake.CloseIDs) != 0 {
			t.Fatalf("ListReads = %d CloseIDs = %v, want 1 / none", fake.ListReads, fake.CloseIDs)
		}
		if len(friction) != 1 || friction[0] != "job: the Herdr workspace may be left open: its identity could not be established" {
			t.Fatalf("friction = %v", friction)
		}
		assertFailedReport(t, root, "job-1", "job: start interrupted")
		assertCleanupReport(t, root, "job-1", "open")
	})
}

// TestRecoverStartInvalidLabeledIDsFailClosed: a new labeled row whose id
// the job cannot name makes the identity unproven, like an ambiguous
// list: nothing closes and the friction line is written — even when a
// valid new row is present alongside the invalid one. Zero new labeled
// rows stay friction-free (TestRecoverStartZeroNewRows).
func TestRecoverStartInvalidLabeledIDsFailClosed(t *testing.T) {
	assertFailClosed := func(t *testing.T, root string, fake *fakeHerdr, friction []string) {
		t.Helper()
		store := Open(root)
		st, recovered, err := RecoverStart(store, "job-1", fake, func(m string) { friction = append(friction, m) }, nil)
		if err != nil || !recovered {
			t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, recovered)
		}
		if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "job: start interrupted" ||
			st.WorkspaceID != "" || st.WorkspaceClosed {
			t.Fatalf("state = %+v", st)
		}
		if len(fake.CloseIDs) != 0 {
			t.Fatalf("CloseIDs = %v, want none", fake.CloseIDs)
		}
		if len(friction) != 1 || friction[0] != "job: the Herdr workspace may be left open: its identity could not be established" {
			t.Fatalf("friction = %v", friction)
		}
		assertFailedReport(t, root, "job-1", "job: start interrupted")
		assertCleanupReport(t, root, "job-1", "open")
	}
	t.Run("one new labeled row with an invalid id closes nothing", func(t *testing.T) {
		root := t.TempDir()
		crashedStartState(t, root)
		var friction []string
		// The preexisting job-job-1 workspace (w0) plus one new row whose
		// id the job cannot name.
		fake := &fakeHerdr{Workspaces: []herdrWorkspace{
			{ID: "w0", Label: "job-job-1", PaneCount: 1},
			{ID: "../w9", Label: "job-job-1", PaneCount: 1},
		}}
		assertFailClosed(t, root, fake, friction)
	})
	t.Run("a valid and an invalid new row close nothing", func(t *testing.T) {
		root := t.TempDir()
		crashedStartState(t, root)
		var friction []string
		// The preexisting job-job-1 workspace (w0) plus one new row with a
		// valid id and one with an id the job cannot name: the valid one
		// does not make the set proven.
		fake := &fakeHerdr{Workspaces: []herdrWorkspace{
			{ID: "w0", Label: "job-job-1", PaneCount: 1},
			{ID: "w1", Label: "job-job-1", PaneCount: 1},
			{ID: "../w9", Label: "job-job-1", PaneCount: 1},
		}}
		assertFailClosed(t, root, fake, friction)
	})
}

// TestRecoverStartZeroNewRows: the crash left no new labeled workspace (a
// crash before the create): nothing to close, and the job still ends
// failed.
func TestRecoverStartZeroNewRows(t *testing.T) {
	root := t.TempDir()
	store := crashedStartState(t, root)
	var friction []string
	fake := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w0", Label: "job-job-1", PaneCount: 1}}}
	st, recovered, err := RecoverStart(store, "job-1", fake, func(m string) { friction = append(friction, m) }, nil)
	if err != nil || !recovered {
		t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, recovered)
	}
	if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "job: start interrupted" ||
		st.WorkspaceID != "" || st.WorkspaceClosed {
		t.Fatalf("state = %+v", st)
	}
	if len(fake.CloseIDs) != 0 || len(friction) != 0 {
		t.Fatalf("CloseIDs = %v friction = %v, want none", fake.CloseIDs, friction)
	}
	assertFailedReport(t, root, "job-1", "job: start interrupted")
	assertCleanupReport(t, root, "job-1", "")
}

// TestRecoverStartRecordedWorkspaceID: a crash after the workspace id
// record is proven by the record itself: the recovery closes the recorded
// id without asking the list, and a close error goes only to friction.
func TestRecoverStartRecordedWorkspaceID(t *testing.T) {
	crashedAfterRecord := func(t *testing.T, root string) *Store {
		t.Helper()
		s := Open(root)
		if _, err := s.Start("job-1", []byte(briefA)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Transition("job-1", StatusPreparing); err != nil {
			t.Fatal(err)
		}
		// The id was recorded before the crash (the other side of the
		// afterWorkspaceCreate window).
		if _, err := s.Record("job-1", func(st *State) {
			st.WorkspaceLabel = "job-job-1"
			st.WorkspaceID = "w9"
			st.RootPane = "w9:p1"
		}); err != nil {
			t.Fatal(err)
		}
		return s
	}
	t.Run("the recorded id closes without a list", func(t *testing.T) {
		root := t.TempDir()
		store := crashedAfterRecord(t, root)
		var friction []string
		fake := &fakeHerdr{}
		st, recovered, err := RecoverStart(store, "job-1", fake, func(m string) { friction = append(friction, m) }, nil)
		if err != nil || !recovered {
			t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, recovered)
		}
		if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "job: start interrupted" ||
			st.WorkspaceID != "w9" || !st.WorkspaceClosed {
			t.Fatalf("state = %+v", st)
		}
		if !reflect.DeepEqual(fake.CloseIDs, []string{"w9"}) {
			t.Fatalf("CloseIDs = %v, want exactly [w9]", fake.CloseIDs)
		}
		if fake.ListReads != 0 || len(friction) != 0 {
			t.Fatalf("ListReads = %d friction = %v, want 0 / none", fake.ListReads, friction)
		}
		assertFailedReport(t, root, "job-1", "job: start interrupted")
		assertCleanupReport(t, root, "job-1", "closed")
	})
	t.Run("a close error goes to friction", func(t *testing.T) {
		root := t.TempDir()
		store := crashedAfterRecord(t, root)
		var friction []string
		fake := &fakeHerdr{CloseErr: errors.New("close down")}
		st, recovered, err := RecoverStart(store, "job-1", fake, func(m string) { friction = append(friction, m) }, nil)
		if err != nil || !recovered {
			t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, recovered)
		}
		if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "job: start interrupted" ||
			st.WorkspaceID != "w9" || st.WorkspaceClosed {
			t.Fatalf("state = %+v", st)
		}
		if len(friction) != 1 || friction[0] != "job: cannot close the job workspace: close down" {
			t.Fatalf("friction = %v", friction)
		}
		assertFailedReport(t, root, "job-1", "job: start interrupted")
		assertCleanupReport(t, root, "job-1", "open")
	})
}

// TestRecoverStartLiveStarter: a held start.lock is a live starter: the
// recovery returns the current state unchanged with recovered=false and
// makes no Herdr call. The hold comes from this same process through a
// separate descriptor: the OS lock on a separately opened file conflicts
// within one process, which is what makes the file a live starter's marker
// for in-process callers as well.
func TestRecoverStartLiveStarter(t *testing.T) {
	root := t.TempDir()
	store := crashedStartState(t, root)
	jobDir, err := Dir(root, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	holder, err := tryStartLock(jobDir)
	if err != nil {
		t.Fatalf("hold the start lock: %v", err)
	}
	t.Cleanup(func() { closeStartLock(holder) })
	fake := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w9", Label: "job-job-1", PaneCount: 1}}}
	st, recovered, err := RecoverStart(store, "job-1", fake, nil, nil)
	if err != nil || recovered {
		t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, recovered)
	}
	if st.Status != StatusPreparing || st.WorkspaceID != "" || st.Motivo != nil {
		t.Fatalf("state changed: %+v", st)
	}
	after, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("state.json changed: %s", after)
	}
	if fake.ListReads != 0 || len(fake.CloseIDs) != 0 {
		t.Fatalf("Herdr called: ListReads=%d CloseIDs=%v", fake.ListReads, fake.CloseIDs)
	}
}

// TestRecoverStartRetryIsANoOp: a second recovery of the failed job changes
// nothing: no second close, no second list, one failure event, and
// report.json byte for byte unchanged.
func TestRecoverStartRetryIsANoOp(t *testing.T) {
	root := t.TempDir()
	store := crashedStartState(t, root)
	fake := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w9", Label: "job-job-1", PaneCount: 1}}}
	_, recovered, err := RecoverStart(store, "job-1", fake, nil, nil)
	if err != nil || !recovered {
		t.Fatalf("first: recovered=%v err=%v", recovered, err)
	}
	report, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	st, recovered, err := RecoverStart(store, "job-1", fake, nil, nil)
	if err != nil || recovered {
		t.Fatalf("second: %+v err=%v recovered=%v", st, err, recovered)
	}
	if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "job: start interrupted" {
		t.Fatalf("second state = %+v", st)
	}
	if !reflect.DeepEqual(fake.CloseIDs, []string{"w9"}) {
		t.Fatalf("CloseIDs = %v, want the single first close", fake.CloseIDs)
	}
	if fake.ListReads != 1 {
		t.Fatalf("ListReads = %d, want 1 (the second made no call)", fake.ListReads)
	}
	if countFailureEvents(t, filepath.Join(root, "jobs", "job-1")) != 1 {
		t.Fatalf("failure events, want exactly one")
	}
	if countTipo(t, Open(root), "cleanup") != 1 {
		t.Fatalf("cleanup events, want exactly one")
	}
	after, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "report.json"))
	if err != nil || !bytes.Equal(report, after) {
		t.Fatalf("report.json changed by the retry")
	}
}

// TestRecoverStartTerminalOrRunningIsUnchanged: a job past the crash window
// (running, or a terminal status) is untouched: no Herdr call, no event,
// no state change.
func TestRecoverStartTerminalOrRunningIsUnchanged(t *testing.T) {
	for _, status := range []string{StatusRunning, StatusDone, StatusFailed} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			s := Open(root)
			if status == StatusFailed {
				// failed is not a lifecycle walk target: fail it from
				// accepted.
				if _, err := s.Start("job-1", []byte(briefA)); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Fail("job-1", "a gate failed", ExitFailed); err != nil {
					t.Fatal(err)
				}
			} else {
				lifecycleState(t, s, "job-1", status)
			}
			jobDir := filepath.Join(root, "jobs", "job-1")
			before, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			var friction []string
			fake := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w9", Label: "job-job-1", PaneCount: 1}}}
			st, recovered, err := RecoverStart(s, "job-1", fake, func(m string) { friction = append(friction, m) }, nil)
			if err != nil || recovered {
				t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, recovered)
			}
			if st.Status != status {
				t.Fatalf("status = %q, want %q", st.Status, status)
			}
			after, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("state changed: %s", after)
			}
			if fake.ListReads != 0 || len(fake.CloseIDs) != 0 || len(friction) != 0 {
				t.Fatalf("Herdr called or friction written: ListReads=%d CloseIDs=%v friction=%v", fake.ListReads, fake.CloseIDs, friction)
			}
		})
	}
}

// TestRecoverStartRetiresThePendingCancel: a cancel queued while the job
// was accepted retires with the terminal record; nothing stays pending and
// the retired request lands in control/done.
func TestRecoverStartRetiresThePendingCancel(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if _, err := s.Start("job-1", []byte(briefA)); err != nil {
		t.Fatal(err)
	}
	// The cancel was queued while the job was still accepted.
	if _, err := s.RequestCancel("job-1", 120); err != nil {
		t.Fatal(err)
	}
	var friction []string
	fake := &fakeHerdr{}
	st, recovered, err := RecoverStart(s, "job-1", fake, func(m string) { friction = append(friction, m) }, nil)
	if err != nil || !recovered {
		t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, recovered)
	}
	if st.Status != StatusFailed || st.Motivo == nil || *st.Motivo != "job: start interrupted" {
		t.Fatalf("state = %+v", st)
	}
	if len(fake.CloseIDs) != 0 || len(friction) != 0 {
		t.Fatalf("CloseIDs = %v friction = %v, want none (nothing to close)", fake.CloseIDs, friction)
	}
	assertCleanupReport(t, root, "job-1", "")
	pending, err := s.PendingControl("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %+v, want none", pending)
	}
	done, err := os.ReadDir(filepath.Join(root, "jobs", "job-1", "control", "done"))
	if err != nil || len(done) != 1 || done[0].Name() != "cancel.json-000001" {
		t.Fatalf("done = %v err=%v", done, err)
	}
	assertFailedReport(t, root, "job-1", "job: start interrupted")
}

// TestStartListErrorBeforeCreate: an unreadable workspace list fails the
// job like a create failure (exit 4, the fixed motivo) and the create
// never runs; the create intent records only after the list succeeds.
func TestStartListErrorBeforeCreate(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	f.cloneCheckout()
	fake := startFake()
	fake.ListErr = &ExitError{Code: ExitHerdr, Msg: "job: herdr workspace list failed"}
	stateRoot := t.TempDir()
	var friction []string
	s := startStarter(t, f, fake, stateRoot, &friction)

	_, store, err := s.Start(startRequest("main", briefA))
	wantExit(t, err, ExitHerdr, "job: herdr workspace list failed")
	if store == nil {
		t.Fatal("no store returned")
	}
	snap, err := store.Snapshot("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.Status != StatusFailed || snap.State.Motivo == nil ||
		*snap.State.Motivo != "job: herdr workspace list failed" {
		t.Fatalf("state = %q motivo %v", snap.State.Status, snap.State.Motivo)
	}
	failEvent, terminal := failEvents(t, stateRoot, "job-1")
	if failEvent.Refs["motivo"] != "job: herdr workspace list failed" || terminal.Refs["exit"] != "4" {
		t.Fatalf("failure = %+v terminal = %+v", failEvent, terminal)
	}
	assertFailedReport(t, stateRoot, "job-1", "job: herdr workspace list failed")
	if len(fake.CreateCalls) != 0 || len(fake.RunCalls) != 0 {
		t.Fatalf("CreateCalls = %d RunCalls = %d, want 0 and 0", len(fake.CreateCalls), len(fake.RunCalls))
	}
	if snap.State.WorkspaceLabel != "" {
		t.Fatalf("the intent was recorded on a list error: %+v", snap.State)
	}
}

// TestStartHeldStartLockReturnsTheDuplicate: a start whose start.lock is
// held by another live starter does nothing else and returns the current
// state as a duplicate; it changes nothing and runs no step.
func TestStartHeldStartLockReturnsTheDuplicate(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	f.cloneCheckout()
	fake := startFake()
	stateRoot := t.TempDir()
	var friction []string
	s := startStarter(t, f, fake, stateRoot, &friction)

	// The job is recorded, as the live starter recorded it.
	store := Open(stateRoot)
	if _, err := store.Start("job-1", []byte(briefA)); err != nil {
		t.Fatal(err)
	}
	jobDir, err := Dir(stateRoot, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	holder, err := tryStartLock(jobDir)
	if err != nil {
		t.Fatalf("hold the start lock: %v", err)
	}
	t.Cleanup(func() { closeStartLock(holder) })

	st, store2, err := s.Start(startRequest("main", briefA))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store2 == nil || st.DuplicateOf != "job-1" || st.Status != StatusAccepted {
		t.Fatalf("held start = %+v store=%v", st, store2 != nil)
	}
	after, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("state changed by the held start: %s", after)
	}
	if len(fake.CreateCalls) != 0 || len(fake.RunCalls) != 0 {
		t.Fatalf("the held start ran steps: CreateCalls=%d RunCalls=%d", len(fake.CreateCalls), len(fake.RunCalls))
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestStartHeldStartLockRefusesAChangedBrief: a start whose start.lock is
// held by another live starter checks the supplied brief against the live
// job's recorded canonical hash, like Store.Start: a different canonical
// brief is the id-reused-with-a-different-brief conflict (exit 20) with no
// store, and nothing is written or run.
func TestStartHeldStartLockRefusesAChangedBrief(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	f.cloneCheckout()
	fake := startFake()
	stateRoot := t.TempDir()
	var friction []string
	s := startStarter(t, f, fake, stateRoot, &friction)

	// The job is recorded, as the live starter recorded it.
	store := Open(stateRoot)
	if _, err := store.Start("job-1", []byte(briefA)); err != nil {
		t.Fatal(err)
	}
	jobDir, err := Dir(stateRoot, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	beforeBrief, err := os.ReadFile(filepath.Join(jobDir, "brief.json"))
	if err != nil {
		t.Fatal(err)
	}
	holder, err := tryStartLock(jobDir)
	if err != nil {
		t.Fatalf("hold the start lock: %v", err)
	}
	t.Cleanup(func() { closeStartLock(holder) })

	_, store2, err := s.Start(startRequest("main", briefChanged))
	wantExit(t, err, ExitBriefConflict, "job: id reused with a different brief")
	if store2 != nil {
		t.Fatalf("store = %v, want nil", store2.Root)
	}
	afterState, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
	if err != nil || !bytes.Equal(beforeState, afterState) {
		t.Fatalf("state changed by the held start: %s", afterState)
	}
	afterBrief, err := os.ReadFile(filepath.Join(jobDir, "brief.json"))
	if err != nil || !bytes.Equal(beforeBrief, afterBrief) {
		t.Fatalf("brief changed by the held start: %s", afterBrief)
	}
	if len(fake.CreateCalls) != 0 || len(fake.RunCalls) != 0 {
		t.Fatalf("the held start ran steps: CreateCalls=%d RunCalls=%d", len(fake.CreateCalls), len(fake.RunCalls))
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

// TestTryStartLock: a held start.lock reports the distinct held result, and
// it is free after the holder releases it. The hold comes from this same
// process through a separate descriptor: the OS lock on a separately
// opened file conflicts within one process (flock is per open file,
// LockFileEx per file name), so the file is a live starter's marker for
// in-process callers as well. The lock file is separate from the job lock
// and mode 0600.
func TestTryStartLock(t *testing.T) {
	dir := t.TempDir()
	first, err := tryStartLock(dir)
	if err != nil {
		t.Fatalf("first tryStartLock: %v", err)
	}
	second, err := tryStartLock(dir)
	if !errors.Is(err, errStartLockHeld) {
		t.Fatalf("second tryStartLock = %v, want the held result", err)
	}
	if second != nil {
		t.Fatalf("the held try returned a descriptor: %v", second)
	}
	info, err := os.Stat(filepath.Join(dir, "start.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("start.lock mode = %v", info.Mode().Perm())
	}
	closeStartLock(first)
	third, err := tryStartLock(dir)
	if err != nil {
		t.Fatalf("after the holder released the lock: %v", err)
	}
	closeStartLock(third)
}
