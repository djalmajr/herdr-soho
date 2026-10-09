package job

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// The control delivery and budget tests drive the supervisor's watch loop
// with the shared fakes at a known poll cadence so ticks land at exact
// monotonic times.

// stopAmendWant is the exact stop amendment body (contract "Cancel and
// timeout"), named here so the tests pin the bytes independently.
const stopAmendWant = "Stop now and report what you have: commit what is integrated, release your workers and write your report.\n"

// ctlSup is supFixture with an explicit poll: control and budget tests
// drive ticks at a known cadence.
func ctlSup(t *testing.T, poll time.Duration, friction *[]string) (*Store, string, *fakeClock, *fakeTeam, *Supervisor) {
	t.Helper()
	store, jobDir, clock, team := supFixture(t)
	sup := supSupervisor(store, clock, team, friction)
	sup.Poll = poll
	return store, jobDir, clock, team, sup
}

// runUntilStop runs the supervisor until the fake clock panics its stop; a
// clean early return or any other panic fails the test.
func runUntilStop(t *testing.T, sup *Supervisor, stop error) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("the watch loop ended before the stop")
			return
		}
		if !errors.Is(r.(error), stop) {
			panic(r)
		}
	}()
	if _, err := sup.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// nsClock is the test clock for deadline-driven tests: unlike the shared
// fakeClock (whose Monotonic counts sleeps), it advances real nanoseconds
// per Sleep, so the supervisor's time.Duration grace deadlines and budget
// are reached at the expected tick count.
type nsClock struct {
	now       time.Time
	mono      int64
	sleeps    int
	rewind    bool
	stopAfter int
	stop      error
	onSleep   func()
}

func (c *nsClock) Now() time.Time   { return c.now }
func (c *nsClock) Monotonic() int64 { return c.mono }
func (c *nsClock) Sleep(d time.Duration) {
	c.sleeps++
	c.mono += int64(d)
	c.now = c.now.Add(d)
	if c.rewind {
		c.now = c.now.Add(-10000 * time.Hour)
	}
	if c.onSleep != nil {
		c.onSleep()
	}
	if c.stopAfter > 0 && c.sleeps >= c.stopAfter {
		panic(c.stop)
	}
}

// nsSup is the fixture over an nsClock: the store and the supervisor share
// the same nanosecond clock, so event timestamps and deadlines agree.
func nsSup(t *testing.T, poll time.Duration, friction *[]string) (*Store, string, *nsClock, *fakeTeam, *Supervisor) {
	t.Helper()
	root := t.TempDir()
	clock := &nsClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))}
	store := Open(root)
	store.Clock = clock
	if _, err := store.Start("job-1", []byte(supTestBrief)); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := store.Transition("job-1", StatusPreparing); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record("job-1", func(st *State) { st.Dir = dir }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition("job-1", StatusRunning); err != nil {
		t.Fatal(err)
	}
	team := &fakeTeam{spawnName: "orch-1"}
	sup := &Supervisor{
		Store:    store,
		ID:       "job-1",
		Ops:      team,
		Clock:    clock,
		Friction: func(message string) { *friction = append(*friction, message) },
	}
	sup.Poll = poll
	return store, filepath.Join(root, "jobs", "job-1"), clock, team, sup
}

// flakyDispatch wraps the fake team: the n-th dispatch (1-based, counting
// the initial brief dispatch) fails once; every other call passes through.
type flakyDispatch struct {
	*fakeTeam
	failCall int
}

func (f *flakyDispatch) Dispatch(name, briefPath string, amend bool) error {
	f.dispatches = append(f.dispatches, fakeDispatch{Name: name, Path: briefPath, Amend: amend})
	if len(f.dispatches) == f.failCall {
		return errors.New("dispatch failed")
	}
	return nil
}

func countTipe(events []Event, tipo string) int {
	n := 0
	for _, event := range events {
		if event.Tipo == tipo {
			n++
		}
	}
	return n
}

func dirNames(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func assertStopAmend(t *testing.T, jobDir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(jobDir, "stop-amend.md"))
	if err != nil {
		t.Fatalf("stop-amend.md: %v", err)
	}
	if !bytes.Equal(raw, []byte(stopAmendWant)) {
		t.Fatalf("stop-amend.md = %q, want the exact contract body", raw)
	}
	mode, err := os.Stat(filepath.Join(jobDir, "stop-amend.md"))
	if err != nil {
		t.Fatalf("stop-amend.md: %v", err)
	}
	// Windows reports 0666 for a writable file; the 0600 contract is the
	// POSIX one.
	if runtime.GOOS != "windows" && mode.Mode().Perm() != 0o600 {
		t.Fatalf("stop-amend.md mode = %o, want 0600", mode.Mode().Perm())
	}
}

func TestSuperviseControlDeliveryOrder(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := ctlSup(t, 10*time.Second, &friction)
	amend, err := store.RequestAmend("job-1", []byte("tighten the scope"))
	if err != nil {
		t.Fatal(err)
	}
	send, err := store.RequestSend("job-1", []byte("a note from the dispatcher"))
	if err != nil {
		t.Fatal(err)
	}
	if queued, err := store.RequestCheckpoint("job-1"); !queued || err != nil {
		t.Fatalf("checkpoint: queued=%v err=%v", queued, err)
	}
	report := writeOrchestratorReport(t, "# Report — job\n\n- Item 1 [done] did the thing\n")
	// Tick one delivers everything, then the orchestrator works to the
	// report.
	team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}, {State: "working"}, {State: "done", Report: report}}

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	// Checkpoint asks for an immediate push: the git sync of the same tick
	// consumes pushNow (the checkpoint event is asserted below).
	if sup.pushNow {
		t.Fatal("pushNow was not consumed by the git sync")
	}
	// Amend: exactly one amendment dispatch with the request path.
	if len(team.dispatches) != 2 || team.dispatches[1] != (fakeDispatch{Name: "orch-1", Path: amend.Path, Amend: true}) {
		t.Fatalf("dispatches = %+v", team.dispatches)
	}
	// Send: exactly one Send with the request path.
	if len(team.sends) != 1 || team.sends[0] != (fakeSend{Name: "orch-1", Path: send.Path}) {
		t.Fatalf("sends = %+v", team.sends)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "worker_spawned", "checkpoint", "amend_received", "amend_received", "worker_done", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	checkpoint := events[2]
	if checkpoint.Resumo != "push requested" || len(checkpoint.Refs) != 0 {
		t.Fatalf("checkpoint event = %+v, want the resumo and no refs", checkpoint)
	}
	amendEv := events[3]
	if amendEv.Resumo != "amendment delivered" || amendEv.Refs["seq_ref"] != "1" {
		t.Fatalf("amend_received = %+v", amendEv)
	}
	sendEv := events[4]
	if sendEv.Resumo != "note delivered" || sendEv.Refs["seq_ref"] != "2" {
		t.Fatalf("amend_received = %+v", sendEv)
	}
	// Every request retired after delivery, in order.
	if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
		t.Fatalf("pending control = %v, want only seq", got)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"amend-000001.md", "checkpoint-000003", "send-000002.md"}) {
		t.Fatalf("retired control = %v", got)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	if clock.sleeps != 3 {
		t.Fatalf("sleeps = %d, want 3", clock.sleeps)
	}
	if st := supState(t, store); st.Status != StatusDone {
		t.Fatalf("state = %+v", st)
	}
}

func TestSuperviseControlAmendUnblocksBlockedJob(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := ctlSup(t, 10*time.Second, &friction)
	report := writeOrchestratorReport(t, "# Report — job\n\n- Item 1 [done] did the thing\n")
	// The job blocks during the loop (the orchestrator's question), and
	// the dispatcher's amend lands one tick later.
	var amendPath string
	clock.onSleep = func() {
		if clock.sleeps == 1 {
			item, err := store.RequestAmend("job-1", []byte("the answer to the question"))
			if err != nil {
				panic(err)
			}
			amendPath = item.Path
		}
	}
	team.statusScript = []fakeStatus{{State: "question"}, {State: "working"}, {State: "done", Report: report}}

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	// The amendment unblocked the job: running, motivo cleared.
	st := supState(t, store)
	if st.Status != StatusDone || st.Motivo != nil {
		t.Fatalf("state = %+v", st)
	}
	// The amendment was dispatched once, with the request path.
	if len(team.dispatches) != 2 || team.dispatches[1] != (fakeDispatch{Name: "orch-1", Path: amendPath, Amend: true}) {
		t.Fatalf("dispatches = %+v", team.dispatches)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "worker_spawned", "blocked", "amend_received", "unblocked", "worker_done", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	amendEv := events[3]
	if amendEv.Resumo != "amendment delivered" || amendEv.Refs["seq_ref"] != "1" {
		t.Fatalf("amend_received = %+v", amendEv)
	}
	// The request retired after the unblock.
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"amend-000001.md"}) {
		t.Fatalf("retired control = %v", got)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

func TestSuperviseControlSendDoesNotUnblock(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := ctlSup(t, 10*time.Second, &friction)
	// The job blocks during the loop (the orchestrator's question); the
	// send lands while it is blocked, and the orchestrator keeps asking.
	var sendPath string
	clock.onSleep = func() {
		if clock.sleeps == 1 {
			item, err := store.RequestSend("job-1", []byte("a non-blocking note"))
			if err != nil {
				panic(err)
			}
			sendPath = item.Path
		}
	}
	team.statusScript = []fakeStatus{{State: "question"}, {State: "question"}}
	clock.stopAfter = 2
	clock.stop = errors.New("stop the watch loop")

	runUntilStop(t, sup, clock.stop)

	st := supState(t, store)
	if st.Status != StatusBlocked || st.Motivo == nil || *st.Motivo != "question" {
		t.Fatalf("a send never unblocks: state = %+v", st)
	}
	if len(team.sends) != 1 || team.sends[0] != (fakeSend{Name: "orch-1", Path: sendPath}) {
		t.Fatalf("sends = %+v", team.sends)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "worker_spawned", "blocked", "amend_received"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v (no unblocked)", got, want)
	}
	amendEv := events[3]
	if amendEv.Resumo != "note delivered" || amendEv.Refs["seq_ref"] != "1" {
		t.Fatalf("amend_received = %+v", amendEv)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"send-000001.md"}) {
		t.Fatalf("retired control = %v", got)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	if clock.sleeps != 2 {
		t.Fatalf("sleeps = %d, want 2", clock.sleeps)
	}
}

func TestSuperviseControlDispatchFailureRetries(t *testing.T) {
	store, jobDir, clock, team := supFixture(t)
	var friction []string
	sup := supSupervisor(store, clock, team, &friction)
	sup.Poll = 10 * time.Second
	// The first dispatch (the brief) succeeds; the first control delivery
	// fails once, the retry succeeds.
	flaky := &flakyDispatch{fakeTeam: team, failCall: 2}
	sup.Ops = flaky

	amend, err := store.RequestAmend("job-1", []byte("tighten the scope"))
	if err != nil {
		t.Fatal(err)
	}
	report := writeOrchestratorReport(t, "# Report — job\n\n- Item 1 [done] did the thing\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}, {State: "done", Report: report}}

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	// The failed delivery wrote one friction line naming the request and
	// kept it for the next tick; the retry delivered it once.
	if len(friction) != 1 || !strings.Contains(friction[0], "amend-000001.md") {
		t.Fatalf("friction = %v, want one line naming the request", friction)
	}
	if len(team.dispatches) != 3 {
		t.Fatalf("dispatches = %+v, want the brief, the failed delivery and the retry", team.dispatches)
	}
	want := fakeDispatch{Name: "orch-1", Path: amend.Path, Amend: true}
	if team.dispatches[1] != want || team.dispatches[2] != want {
		t.Fatalf("control dispatches = %+v, %+v", team.dispatches[1], team.dispatches[2])
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	wantTypes := []string{"accepted", "worker_spawned", "amend_received", "worker_done", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, wantTypes) {
		t.Fatalf("event types = %v, want %v (one amend_received)", got, wantTypes)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"amend-000001.md"}) {
		t.Fatalf("retired control = %v, want the request retired by the retry", got)
	}
	// The failed delivery cost exactly one tick; the retry landed on the
	// next one.
	if clock.sleeps != 2 {
		t.Fatalf("sleeps = %d, want 2 (fail, retry, done)", clock.sleeps)
	}
}

func TestSuperviseControlRestartRedeliversPending(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := ctlSup(t, 10*time.Second, &friction)
	// The state after a crash: the orchestrator is recorded and dispatched,
	// and the amend is still pending, not retired.
	if _, err := store.Record("job-1", func(st *State) {
		st.Orchestrator = "orch-1"
		st.DispatchedAt = "2026-10-09T09:00:00-03:00"
	}); err != nil {
		t.Fatal(err)
	}
	amend, err := store.RequestAmend("job-1", []byte("tighten the scope"))
	if err != nil {
		t.Fatal(err)
	}
	report := writeOrchestratorReport(t, "# Report — job\n\n- Item 1 [done] did the thing\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	// The restart reuses the recorded orchestrator and redelivers the
	// pending request (at-least-once delivery).
	if team.spawnCalls != 0 {
		t.Fatalf("spawn calls = %d, want 0 on a restart", team.spawnCalls)
	}
	if len(team.dispatches) != 1 || team.dispatches[0] != (fakeDispatch{Name: "orch-1", Path: amend.Path, Amend: true}) {
		t.Fatalf("dispatches = %+v, want the pending amend delivered", team.dispatches)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "amend_received", "worker_done", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"amend-000001.md"}) {
		t.Fatalf("retired control = %v", got)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	// One tick to deliver, one to finish.
	if clock.sleeps != 1 {
		t.Fatalf("sleeps = %d, want 1", clock.sleeps)
	}
}

func TestSuperviseControlRetireOnlyAfterEvent(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := ctlSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.Orchestrator = "orch-1"
		st.DispatchedAt = "2026-10-09T09:00:00-03:00"
	}); err != nil {
		t.Fatal(err)
	}
	amend, err := store.RequestAmend("job-1", []byte("tighten the scope"))
	if err != nil {
		t.Fatal(err)
	}
	// Crash the event append: the delivery landed, the event did not.
	original := appendEventFn
	appendEventFn = func(dir string, ts time.Time, in EventIn) (Event, error) {
		if in.Tipo == "amend_received" {
			return Event{}, errors.New("crash before the amend event lands")
		}
		return original(dir, ts, in)
	}
	t.Cleanup(func() { appendEventFn = original })
	report := writeOrchestratorReport(t, "# Report — job\n\n- Item 1 [done] did the thing\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}

	// First run: the delivery succeeds, the event fails, and the request
	// must stay pending (no retire before the event).
	exit, err := sup.Run()
	if err == nil {
		t.Fatalf("Run: the failing event must surface as an error, exit %d", exit)
	}
	if exit != ExitFailed {
		t.Fatalf("exit = %d, want %d", exit, ExitFailed)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"amend-000001.md", "seq"}) {
		t.Fatalf("pending control = %v, want the request unretired", got)
	}
	if entries, err := os.ReadDir(filepath.Join(jobDir, "control", "done")); err == nil && len(entries) != 0 {
		t.Fatalf("retired control = %v, want nothing", entries)
	}
	if events, err := readEvents(jobDir); err != nil {
		t.Fatal(err)
	} else if countTipe(events, "amend_received") != 0 {
		t.Fatal("a failed event append must not count as delivered")
	}

	// Second run: the same request is delivered again and retires.
	appendEventFn = original
	exit, err = sup.Run()
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("second exit = %d, want 0", exit)
	}
	if len(team.dispatches) != 2 || team.dispatches[1] != (fakeDispatch{Name: "orch-1", Path: amend.Path, Amend: true}) {
		t.Fatalf("dispatches = %+v, want the pending amend delivered again", team.dispatches)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"amend-000001.md"}) {
		t.Fatalf("retired control = %v", got)
	}
	if events, err := readEvents(jobDir); err != nil {
		t.Fatal(err)
	} else if countTipe(events, "amend_received") != 1 {
		t.Fatal("the redelivered request lands its amend_received once")
	}
	if clock.sleeps != 1 {
		t.Fatalf("sleeps = %d, want 1 (deliver, done)", clock.sleeps)
	}
}

func TestSuperviseControlCancelByDeadline(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if queued, err := store.RequestCancel("job-1", 60); !queued || err != nil {
		t.Fatalf("cancel: queued=%v err=%v", queued, err)
	}
	working := func() []fakeStatus {
		script := make([]fakeStatus, 7)
		for i := range script {
			script[i] = fakeStatus{State: "working"}
		}
		return script
	}
	team.statusScript = working()

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitCanceled {
		t.Fatalf("exit = %d, want %d", exit, ExitCanceled)
	}
	st := supState(t, store)
	if st.Status != StatusCanceled {
		t.Fatalf("state = %+v", st)
	}
	assertStopAmend(t, jobDir)
	// One stop dispatch with the stop amendment, nothing else.
	if len(team.dispatches) != 2 || team.dispatches[1] != (fakeDispatch{Name: "orch-1", Path: filepath.Join(jobDir, "stop-amend.md"), Amend: true}) {
		t.Fatalf("dispatches = %+v", team.dispatches)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "worker_spawned", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	terminal := events[2]
	if terminal.Refs["exit"] != "21" || terminal.Refs["motivo"] != "cancelado" {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"cancel.json-000001"}) {
		t.Fatalf("retired control = %v", got)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
		t.Fatalf("pending control = %v, want only seq", got)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	// The stop ended exactly at the 60 s grace: six 10 s sleeps after the
	// tick that started the stop.
	if clock.sleeps != 6 {
		t.Fatalf("sleeps = %d, want 6 (the grace deadline)", clock.sleeps)
	}
	rep, err := os.ReadFile(filepath.Join(jobDir, "report.json"))
	if err != nil {
		t.Fatalf("report.json: %v", err)
	}
	if !strings.Contains(string(rep), `"status":"canceled"`) {
		t.Fatalf("report.json = %s", rep)
	}
}

func TestSuperviseControlCancelByOrchestratorDone(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if queued, err := store.RequestCancel("job-1", 300); !queued || err != nil {
		t.Fatalf("cancel: queued=%v err=%v", queued, err)
	}
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done"}}

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitCanceled {
		t.Fatalf("exit = %d, want %d", exit, ExitCanceled)
	}
	st := supState(t, store)
	if st.Status != StatusCanceled {
		t.Fatalf("state = %+v", st)
	}
	assertStopAmend(t, jobDir)
	// The orchestrator's done ends the stop before the 300 s deadline.
	if clock.sleeps != 1 {
		t.Fatalf("sleeps = %d, want 1 (the done beat the deadline)", clock.sleeps)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	if terminal := eventsLast(t, jobDir); terminal.Refs["exit"] != "21" || terminal.Refs["motivo"] != "cancelado" {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
}

func TestSuperviseControlSecondCancelNoEffect(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if queued, err := store.RequestCancel("job-1", 60); !queued || err != nil {
		t.Fatalf("cancel: queued=%v err=%v", queued, err)
	}
	// A second cancel lands after the stop started; it retires with no
	// effect and never extends the deadline.
	clock.onSleep = func() {
		if clock.sleeps == 1 {
			if queued, err := store.RequestCancel("job-1", 300); !queued || err != nil {
				panic(err)
			}
		}
	}
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
	// Both cancels retired, the stop ran once, and the deadline stayed at
	// the first cancel's 60 s grace.
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"cancel.json-000001", "cancel.json-000002"}) {
		t.Fatalf("retired control = %v", got)
	}
	if len(team.dispatches) != 2 {
		t.Fatalf("dispatches = %+v, want the brief and the single stop amendment", team.dispatches)
	}
	assertStopAmend(t, jobDir)
	if clock.sleeps != 6 {
		t.Fatalf("sleeps = %d, want 6 (the second cancel must not extend the grace)", clock.sleeps)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
}

func TestSuperviseBudgetWarningThenTimeout(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) { st.TimeoutMin = 1 }); err != nil {
		t.Fatal(err)
	}
	working := make([]fakeStatus, 7)
	for i := range working {
		working[i] = fakeStatus{State: "working"}
	}
	team.statusScript = append(working, fakeStatus{State: "done"})

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != ExitTimeout {
		t.Fatalf("exit = %d, want %d", exit, ExitTimeout)
	}
	st := supState(t, store)
	if st.Status != StatusTimeout {
		t.Fatalf("state = %+v", st)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "worker_spawned", "timeout_warning", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v (one warning)", got, want)
	}
	warning := events[2]
	if warning.Resumo != "80% of the job budget used" {
		t.Fatalf("timeout_warning resumo = %q", warning.Resumo)
	}
	// The warning fired on the first tick at or past 80 % of the 1 min
	// budget: the 50 s tick (10 s polls), not earlier.
	if want := time.Date(2026, 10, 9, 10, 0, 50, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339); warning.TS != want {
		t.Fatalf("warning ts = %q, want %q (the first tick at or past 80 %%)", warning.TS, want)
	}
	terminal := events[3]
	if terminal.Refs["exit"] != "9" || terminal.Refs["motivo"] != "tempo esgotado" {
		t.Fatalf("terminal refs = %+v", terminal.Refs)
	}
	// The timeout runs the same stop path: the stop amendment lands and is
	// dispatched once.
	assertStopAmend(t, jobDir)
	if len(team.dispatches) != 2 || team.dispatches[1] != (fakeDispatch{Name: "orch-1", Path: filepath.Join(jobDir, "stop-amend.md"), Amend: true}) {
		t.Fatalf("dispatches = %+v", team.dispatches)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	// Ticks at 0, 10, ..., 70 s: the stop starts at 60 s, the
	// orchestrator's done ends it at 70 s.
	if clock.sleeps != 7 {
		t.Fatalf("sleeps = %d, want 7", clock.sleeps)
	}
	rep, err := os.ReadFile(filepath.Join(jobDir, "report.json"))
	if err != nil {
		t.Fatalf("report.json: %v", err)
	}
	if !strings.Contains(string(rep), `"status":"timeout"`) {
		t.Fatalf("report.json = %s", rep)
	}
}

func TestSuperviseBudgetRestartNoDuplicateWarning(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Record("job-1", func(st *State) {
		st.TimeoutMin = 1
		start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
		st.StartedAt = start.Add(-90 * time.Second).Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}}
	clock.stopAfter = 2
	clock.stop = errors.New("stop the watch loop")
	runUntilStop(t, sup, clock.stop)

	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := countTipe(events, "timeout_warning"); n != 1 {
		t.Fatalf("warnings after the first run = %d, want 1", n)
	}

	// A restarted supervisor (new process: a new monotonic origin, the
	// elapsed time re-derived once from StartedAt) must not warn again.
	clock2 := &nsClock{now: clock.now}
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
	if n := countTipe(events, "timeout_warning"); n != 1 {
		t.Fatalf("warnings after the restart = %d, want 1 (the log already has one)", n)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	if st := supState(t, store); st.Status != StatusRunning {
		t.Fatalf("state = %+v, want still running (the stop is pending)", st)
	}
	if team2.spawnCalls != 0 {
		t.Fatalf("restart spawn calls = %d, want 0", team2.spawnCalls)
	}
}

func TestSuperviseBudgetBackwardsClock(t *testing.T) {
	t.Run("a backwards Now never shortens or extends the budget", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
		clock.rewind = true
		if _, err := store.Record("job-1", func(st *State) {
			st.TimeoutMin = 1
			start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
			st.StartedAt = start.Add(-50 * time.Second).Format(time.RFC3339)
		}); err != nil {
			t.Fatal(err)
		}
		team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}, {State: "done"}}

		exit, err := sup.Run()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if exit != ExitTimeout {
			t.Fatalf("exit = %d, want %d: a backwards wall clock must not change the budget", exit, ExitTimeout)
		}
		if st := supState(t, store); st.Status != StatusTimeout {
			t.Fatalf("state = %+v", st)
		}
		// The budget advanced exactly on the monotonic reading: the 50 s
		// already recorded warned, and the stop started at 60 s of
		// monotonic elapsed — the wall clock sat 10000 hours behind by
		// then.
		if events, err := readEvents(jobDir); err != nil {
			t.Fatal(err)
		} else if n := countTipe(events, "timeout_warning"); n != 1 {
			t.Fatalf("warnings = %d, want 1", n)
		}
		if clock.sleeps != 2 {
			t.Fatalf("sleeps = %d, want 2 (50 s warned, 60 s stopped)", clock.sleeps)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v", friction)
		}
	})

	t.Run("a started_at in the future starts the budget at zero", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
		if _, err := store.Record("job-1", func(st *State) {
			st.TimeoutMin = 1
			// Five minutes after the clock's start: the already-recorded
			// elapsed is negative and clamps to zero.
			st.StartedAt = time.Date(2026, 10, 9, 10, 5, 0, 0, time.FixedZone("-03:00", -3*3600)).Format(time.RFC3339)
		}); err != nil {
			t.Fatal(err)
		}
		team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}}
		clock.stopAfter = 2
		clock.stop = errors.New("stop the watch loop")
		runUntilStop(t, sup, clock.stop)

		// Twenty seconds of monotonic elapsed is far below 80 % of 1 min:
		// no warning, no stop, no stop amendment.
		if events, err := readEvents(jobDir); err != nil {
			t.Fatal(err)
		} else if n := countTipe(events, "timeout_warning"); n != 0 {
			t.Fatalf("warnings = %d, want 0", n)
		}
		if st := supState(t, store); st.Status != StatusRunning {
			t.Fatalf("state = %+v, want still running", st)
		}
		if _, err := os.Stat(filepath.Join(jobDir, "stop-amend.md")); !os.IsNotExist(err) {
			t.Fatalf("no stop amendment before 80 %%: %v", err)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v", friction)
		}
	})
}

func eventsLast(t *testing.T, jobDir string) Event {
	t.Helper()
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("no events")
	}
	// The terminal path ends with the cleanup event; the terminal event is
	// the last one of its type.
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Tipo == "terminal" {
			return events[i]
		}
	}
	t.Fatal("no terminal event")
	return Event{}
}

// Repeated checkpoints converge ([retry]): while one is pending the queue
// stays a single marker (a repeat request is a no-op), the supervisor
// handles that one exactly once, and the queue is reusable after.
func TestSuperviseControlRepeatedCheckpointConverges(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := ctlSup(t, 10*time.Second, &friction)
	if queued, err := store.RequestCheckpoint("job-1"); !queued || err != nil {
		t.Fatalf("checkpoint: queued=%v err=%v", queued, err)
	}
	// A repeat while the first is pending must not accumulate: the no-op
	// reports not queued.
	if queued, err := store.RequestCheckpoint("job-1"); queued || err != nil {
		t.Fatalf("repeat checkpoint: queued=%v err=%v, want a no-op", queued, err)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"checkpoint"}) {
		t.Fatalf("pending control = %v, want the single marker", got)
	}
	report := writeOrchestratorReport(t, "# Report — job\n\n- Item 1 [done] did the thing\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}

	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	// Checkpoint asks for an immediate push: the git sync of the same tick
	// consumes pushNow (the checkpoint event is asserted below).
	if sup.pushNow {
		t.Fatal("pushNow was not consumed by the git sync")
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "worker_spawned", "checkpoint", "worker_done", "terminal", "cleanup"}
	if got := eventTipes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if e := events[2]; e.Resumo != "push requested" || len(e.Refs) != 0 {
		t.Fatalf("checkpoint event = %+v, want the resumo and no refs", e)
	}
	// The request retired with its counter.
	if got := dirNames(t, filepath.Join(jobDir, "control")); !reflect.DeepEqual(got, []string{"seq"}) {
		t.Fatalf("pending control = %v, want only seq", got)
	}
	if got := dirNames(t, filepath.Join(jobDir, "control", "done")); !reflect.DeepEqual(got, []string{"checkpoint-000001"}) {
		t.Fatalf("retired control = %v", got)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v", friction)
	}
	if clock.sleeps != 1 {
		t.Fatalf("sleeps = %d, want 1", clock.sleeps)
	}
	if st := supState(t, store); st.Status != StatusDone {
		t.Fatalf("state = %+v", st)
	}
}
