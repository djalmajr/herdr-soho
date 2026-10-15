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

	"github.com/djalmajr/herdr-soho/internal/collaboration"
)

// Mutation guard: saving the terminal state.json before report.json (the
// old Transition and Fail order) — at the instant the terminal state lands,
// report.json must already carry the outcome and the partial facts, on the
// transition path (done, failed by partial items) and on the failure walk
// (failed by a gone orchestrator).
func TestSuperviseTerminalReportPublishedBeforeTheTerminalState(t *testing.T) {
	cases := []struct {
		name   string
		script func(report string) []fakeStatus
		body   string
		status string
		exit   int
	}{
		{
			name: "done",
			body: "# report\n\n- [done] item\n",
			script: func(report string) []fakeStatus {
				return []fakeStatus{{State: "working"}, {State: "done", Report: report}}
			},
			status: StatusDone,
			exit:   0,
		},
		{
			name: "failed by partial items",
			body: "# report\n\n- [partial] item — the gate was not run\n",
			script: func(report string) []fakeStatus {
				return []fakeStatus{{State: "working"}, {State: "done", Report: report}}
			},
			status: StatusFailed,
			exit:   ExitFailed,
		},
		{
			name: "failed by a gone orchestrator",
			body: "# report\n\n- [done] item\n",
			script: func(string) []fakeStatus {
				return []fakeStatus{{State: "working"}, {State: "gone"}, {State: "gone"}, {State: "gone"}}
			},
			status: StatusFailed,
			exit:   ExitFailed,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var friction []string
			store, jobDir, _, team, sup := nsSup(t, 10*time.Second, &friction)
			herdr := collabSup(t, store, sup)
			ws := collabWorkspace(t, jobDir)
			_, id := makeCollab(t, ws, collabJobDir(t, store), "w1", "impl-1", "rev-1", "w1:p2", "w1:p3")
			team.rosterRows = collabRoster("impl-1", "rev-1")
			team.statusScript = tc.script(writeOrchestratorReport(t, tc.body))
			// Observe the boundary: right before the first state.json whose
			// top-level status is the outcome is renamed into place, read
			// what report.json holds.
			var atBoundary []byte
			boundaryErr := errors.New("the terminal state was never written")
			observed := false
			previous := renameAtomic
			renameAtomic = func(from, to string) error {
				if !observed && strings.HasSuffix(to, "state.json") {
					if raw, err := os.ReadFile(from); err == nil {
						var tmp State
						if json.Unmarshal(raw, &tmp) == nil && tmp.Status == tc.status {
							observed = true
							atBoundary, boundaryErr = os.ReadFile(filepath.Join(jobDir, "report.json"))
						}
					}
				}
				return previous(from, to)
			}
			t.Cleanup(func() { renameAtomic = previous })
			exit, err := sup.Run()
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if exit != tc.exit {
				t.Fatalf("exit = %d, want %d", exit, tc.exit)
			}
			if boundaryErr != nil {
				t.Fatalf("report.json when the terminal state landed: %v", boundaryErr)
			}
			var rep Report
			if err := json.Unmarshal(atBoundary, &rep); err != nil {
				t.Fatalf("report.json at the boundary: %v\n%s", err, atBoundary)
			}
			if rep.Status != tc.status || !rep.Partial || !reflect.DeepEqual(rep.PartialCollaborations, []string{id}) {
				t.Fatalf("report.json at the boundary: status=%s partial=%v ids=%v, want %s true [%s]", rep.Status, rep.Partial, rep.PartialCollaborations, tc.status, id)
			}
			if st := supState(t, store); st.Status != tc.status || !reflect.DeepEqual(PartialCollaborations(st), []string{id}) {
				t.Fatalf("state after the run = %s %v, want %s [%s]", st.Status, PartialCollaborations(st), tc.status, id)
			}
			requireWorkspaceClosed(t, herdr, team)
		})
	}
}

// crashAfterFinishing makes every state write after the first durable
// finishing state fail, like a process that died right after that write:
// it returns the restore function.
func crashAfterFinishing(t *testing.T) func() {
	t.Helper()
	previous := renameAtomic
	crashed := false
	renameAtomic = func(from, to string) error {
		if crashed {
			return errors.New("crash")
		}
		if !strings.HasSuffix(to, "state.json") {
			return previous(from, to)
		}
		raw, readErr := os.ReadFile(from)
		if err := previous(from, to); err != nil {
			return err
		}
		var tmp State
		if readErr == nil && json.Unmarshal(raw, &tmp) == nil && tmp.Status == StatusFinishing {
			crashed = true
		}
		return nil
	}
	restore := func() { renameAtomic = previous }
	t.Cleanup(restore)
	return restore
}

// Mutation guard: entering finishing in its own write before the terminal
// intent (the old onOrchestratorDone order) — a crash right after finishing
// is durable must leave the intent, and the restart converges on done
// (with the partial facts) instead of failing the completed job.
func TestSuperviseCrashAfterFinishingKeepsTheTerminalIntent(t *testing.T) {
	var friction1, friction2 []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction1)
	collabSup(t, store, sup)
	ws := collabWorkspace(t, jobDir)
	_, id := makeCollab(t, ws, collabJobDir(t, store), "w1", "impl-1", "rev-1", "w1:p2", "w1:p3")
	team.rosterRows = collabRoster("impl-1", "rev-1")
	report := writeOrchestratorReport(t, "# report\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}
	restore := crashAfterFinishing(t)
	if _, err := sup.Run(); err == nil {
		t.Fatal("the run did not fail at the scripted crash")
	}
	st := supState(t, store)
	if st.Status != StatusFinishing || st.TerminalIntent == nil || st.TerminalIntent.Status != StatusDone {
		t.Fatalf("state at the crash = %s intent=%+v, want finishing with the done intent", st.Status, st.TerminalIntent)
	}
	restore()
	clock2 := &nsClock{now: clock.now}
	herdr2 := &fakeHerdr{}
	team2 := &fakeTeam{spawnName: "orch-1", rosterRows: team.rosterRows}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Herdr: herdr2, ProjectRoot: collabJobDir(t, store)}
	sup2.Friction = func(message string) { friction2 = append(friction2, message) }
	exit, err := sup2.Run()
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("restart exit = %d, want 0 (the completed job converges on done)", exit)
	}
	st = supState(t, store)
	if st.Status != StatusDone || !reflect.DeepEqual(PartialCollaborations(st), []string{id}) {
		t.Fatalf("state after the restart = %s %v, want done [%s]", st.Status, PartialCollaborations(st), id)
	}
	rep, err := readReport(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusDone || !rep.Partial || !reflect.DeepEqual(rep.PartialCollaborations, []string{id}) {
		t.Fatalf("report.json after the restart: status=%s partial=%v ids=%v", rep.Status, rep.Partial, rep.PartialCollaborations)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if countTipe(events, "failure") != 0 || countTipe(events, "terminal") != 1 {
		t.Fatalf("failure=%d terminal=%d, want no failure and one terminal event", countTipe(events, "failure"), countTipe(events, "terminal"))
	}
	requireWorkspaceClosed(t, herdr2, team2)
	if want := []string{"job: the supervisor could not record the terminal intent: crash"}; !reflect.DeepEqual(friction1, want) || len(friction2) != 0 {
		t.Fatalf("friction = %q / %q, want %q / none", friction1, friction2, want)
	}
}

// Mutation guard: entering finishing on the pending-decision path without
// the motivo in the same write (or a restart that does not complete the
// crossing) — a crash right after finishing is durable must leave the
// restart enough to block the job on its pending decision, never fail it.
func TestSuperviseCrashAfterFinishingPendingDecisionBlocks(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if _, err := store.Note("job-1", EventIn{Tipo: "decision", Escopo: "global", Resumo: "keep the queue"}); err != nil {
		t.Fatal(err)
	}
	report := writeOrchestratorReport(t, "# Report — job\n\n- [partial] Item 1 — the gate was not run\n")
	team.statusScript = []fakeStatus{{State: "done", Report: report}}
	restore := crashAfterFinishing(t)
	if _, err := sup.Run(); err == nil {
		t.Fatal("the run did not fail at the scripted crash")
	}
	st := supState(t, store)
	if st.Status != StatusFinishing || st.Motivo == nil || *st.Motivo != decisionsPendingMotivo || st.TerminalIntent != nil {
		t.Fatalf("state at the crash = %s motivo=%v intent=%+v, want finishing with the pending motivo and no intent", st.Status, st.Motivo, st.TerminalIntent)
	}
	restore()
	clock2 := &nsClock{now: clock.now, stopAfter: 3, stop: errors.New("stop the watch loop")}
	team2 := &fakeTeam{spawnName: "orch-1", statusScript: []fakeStatus{{State: "done", Report: report}}}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second}
	sup2.Friction = func(message string) { friction = append(friction, message) }
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("the restarted watch loop ended before the stop")
				return
			}
			if !errors.Is(r.(error), clock2.stop) {
				panic(r)
			}
		}()
		if _, err := sup2.Run(); err != nil {
			t.Errorf("restart Run: %v", err)
		}
	}()
	st = supState(t, store)
	if st.Status != StatusBlocked || st.Motivo == nil || *st.Motivo != decisionsPendingMotivo {
		t.Fatalf("state after the restart = %s motivo=%v, want blocked on the pending decision", st.Status, st.Motivo)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if countTipe(events, "failure") != 0 || countTipe(events, "terminal") != 0 || countTipe(events, "blocked") != 1 {
		t.Fatalf("event types = %v, want one blocked event and no failure or terminal", eventTipes(events))
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
}

// Mutation guard: a first-pass enumeration failure that still releases,
// cleans up and closes the workspace (the old collabEnded-only finalize) —
// the same process must refuse the settlement, the release, the cleanup
// and the close exactly as a fresh process does, and a later supervisor
// pass that can enumerate settles the collaboration and closes.
func TestSuperviseFirstPassEnumerationFailureRefusesCleanup(t *testing.T) {
	var friction1, friction2 []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction1)
	herdr := collabSup(t, store, sup)
	ws := collabWorkspace(t, jobDir)
	cstore, id := makeCollab(t, ws, collabJobDir(t, store), "w1", "impl-1", "rev-1", "w1:p2", "w1:p3")
	team.rosterRows = collabRoster("impl-1", "rev-1")
	report := writeOrchestratorReport(t, "# report\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}
	previous := collabListActive
	collabListActive = func(collaboration.Store) ([]collaboration.Assignment, []collaboration.UnreadableAssignment, error) {
		return nil, nil, errors.New("directory unreadable")
	}
	t.Cleanup(func() { collabListActive = previous })
	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	st := supState(t, store)
	if st.Status != StatusDone || st.WorkspaceClosed {
		t.Fatalf("state = %s closed=%v, want done with the workspace open", st.Status, st.WorkspaceClosed)
	}
	if team.releaseCalls != 0 || team.releaseTeamCalls != 0 || team.gcCalls != 0 || len(herdr.CloseIDs) != 0 {
		t.Fatalf("release=%d team=%d gc=%d close=%v, want none after the failed enumeration", team.releaseCalls, team.releaseTeamCalls, team.gcCalls, herdr.CloseIDs)
	}
	if a := collabDisk(t, cstore, id); a.Phase == collaboration.Finished {
		t.Fatalf("collaboration on disk = %s, want it left active (never settled without enumeration)", a.Phase)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if countTipe(events, "cleanup") != 0 {
		t.Fatalf("cleanup events = %d, want none", countTipe(events, "cleanup"))
	}
	if len(friction1) != 1 || !strings.Contains(friction1[0], "cannot enumerate the collaborations") {
		t.Fatalf("friction = %v, want the enumeration line", friction1)
	}
	// A later supervisor pass that can enumerate: the capture, the
	// settlement, the release and the close run.
	collabListActive = previous
	clock2 := &nsClock{now: clock.now}
	herdr2 := &fakeHerdr{}
	team2 := &fakeTeam{spawnName: "orch-1", rosterRows: team.rosterRows}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second, Herdr: herdr2, ProjectRoot: collabJobDir(t, store)}
	sup2.Friction = func(message string) { friction2 = append(friction2, message) }
	exit, err = sup2.Run()
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("restart exit = %d, want 0", exit)
	}
	if out := findColab(t, readColabs(t, jobDir), id); out.Outcome != collabOutcomePartial || out.Verdict != "reject" || !out.FinalizedBySupervisor {
		t.Fatalf("restart outcome = %+v, want partial/reject by the supervisor", out)
	}
	if st := supState(t, store); !st.WorkspaceClosed {
		t.Fatal("the workspace is still open after the restart")
	}
	requireNoActiveCollabs(t, ws)
	requireWorkspaceClosed(t, herdr2, team2)
	if len(friction2) != 0 {
		t.Fatalf("restart friction = %v, want none", friction2)
	}
}

// Mutation guard: entering finishing on the stop path in its own write
// before the terminal intent (the old endStop order) — a crash right after
// finishing is durable must leave the canceled intent, and the restart
// ends the job canceled, never failed.
func TestSuperviseCrashAfterFinishingKeepsTheStopOutcome(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	if queued, err := store.RequestCancel("job-1", 60); !queued || err != nil {
		t.Fatalf("cancel: queued=%v err=%v", queued, err)
	}
	script := make([]fakeStatus, 7)
	for i := range script {
		script[i] = fakeStatus{State: "working"}
	}
	team.statusScript = script
	restore := crashAfterFinishing(t)
	if _, err := sup.Run(); err == nil {
		t.Fatal("the run did not fail at the scripted crash")
	}
	st := supState(t, store)
	if st.Status != StatusFinishing || st.TerminalIntent == nil || st.TerminalIntent.Status != StatusCanceled || st.Motivo == nil || *st.Motivo != "cancelado" {
		t.Fatalf("state at the crash = %s motivo=%v intent=%+v, want finishing with the canceled intent", st.Status, st.Motivo, st.TerminalIntent)
	}
	restore()
	clock2 := &nsClock{now: clock.now}
	team2 := &fakeTeam{spawnName: "orch-1"}
	sup2 := &Supervisor{Store: store, ID: "job-1", Ops: team2, Clock: clock2, Poll: 10 * time.Second}
	sup2.Friction = func(message string) { friction = append(friction, message) }
	exit, err := sup2.Run()
	if err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if exit != ExitCanceled {
		t.Fatalf("restart exit = %d, want %d", exit, ExitCanceled)
	}
	if st := supState(t, store); st.Status != StatusCanceled {
		t.Fatalf("state after the restart = %s, want canceled", st.Status)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if countTipe(events, "failure") != 0 || countTipe(events, "terminal") != 1 {
		t.Fatalf("event types = %v, want one terminal event and no failure", eventTipes(events))
	}
	if terminal := events[len(events)-2]; terminal.Tipo != "terminal" || terminal.Refs["exit"] != "21" || terminal.Refs["motivo"] != "cancelado" {
		t.Fatalf("terminal event = %+v, want exit 21 cancelado", terminal)
	}
	if want := []string{"job: the supervisor could not record the terminal intent: crash"}; !reflect.DeepEqual(friction, want) {
		t.Fatalf("friction = %q, want exactly the crash line of the first process and nothing from the restart: %q", friction, want)
	}
}
