package job

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/collaboration"
)

// The persisted partial facts of the job's own collaborations: the state
// fields (the outcomes, the asked ids, the terminal intent), the helper,
// and the report's partial fields (always present, derived from the state
// — never from the facts — by every rewrite). Each test names the
// mutation it guards: the change it would catch.

// Mutation guard: persisting the capture after the terminal transition
// (the old order) — the outcomes, the asked ids and the terminal intent
// must stand in the state while the job is still finishing, and the
// report (refreshed by every state write) must carry the partial fields
// from before the transition.
func TestSupervisePartialDonePersistsStateAndReportBeforeTerminal(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	herdr := collabSup(t, store, sup)
	ws := collabWorkspace(t, jobDir)
	cstore, id := makeCollab(t, ws, collabJobDir(t, store), "w1", "impl-1", "rev-1", "w1:p2", "w1:p3")
	team.rosterRows = collabRoster("impl-1", "rev-1")
	report := writeOrchestratorReport(t, "# report\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}, {State: "done", Report: report}}
	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	st := supState(t, store)
	// The state carries the three persisted facts: the outcomes, the
	// asked ids, and the terminal intent.
	if len(st.Colaboracoes) != 1 {
		t.Fatalf("state colaboracoes = %+v, want the single persisted outcome", st.Colaboracoes)
	}
	if st.Colaboracoes[0].ID != id || st.Colaboracoes[0].Outcome != collabOutcomePartial || st.Colaboracoes[0].Verdict != "reject" {
		t.Fatalf("persisted outcome = %+v, want partial/reject for %s", st.Colaboracoes[0], id)
	}
	if !reflect.DeepEqual(st.CollabIDs, []string{id}) {
		t.Fatalf("state collab_ids = %v, want %v", st.CollabIDs, []string{id})
	}
	if st.TerminalIntent == nil || st.TerminalIntent.Status != StatusDone || st.TerminalIntent.Exit != 0 {
		t.Fatalf("terminal intent = %+v, want done/0", st.TerminalIntent)
	}
	// The helper: the sorted deduplicated ids of the partial-family
	// outcomes.
	if got := PartialCollaborations(st); !reflect.DeepEqual(got, []string{id}) {
		t.Fatalf("PartialCollaborations = %v, want %v", got, []string{id})
	}
	// The report the supervisor wrote carries the fields with the exact
	// values, right after parciais and always present.
	raw, err := os.ReadFile(filepath.Join(jobDir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"partial":true,"partial_collaborations":["`+id+`"]`)) {
		t.Fatalf("report.json lacks the exact partial fields:\n%s", raw)
	}
	out := findColab(t, readColabs(t, jobDir), id)
	if out.Outcome != collabOutcomePartial || out.Verdict != "reject" || out.Phase != collaboration.Finished || !out.FinalizedBySupervisor {
		t.Fatalf("settled outcome = %+v, want partial/reject finished by the supervisor", out)
	}
	_ = clock
	if a := collabDisk(t, cstore, id); a.Phase != collaboration.Finished || a.Verdict != "reject" {
		t.Fatalf("collaboration on disk = %s/%s, want finished/reject", a.Phase, a.Verdict)
	}
	requireNoActiveCollabs(t, ws)
	requireWorkspaceClosed(t, herdr, team)
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
}

// Mutation guard: the field lists the ids raw (in ask order), counts a
// collaboration the orchestrator finished inside the grace, or duplicates
// an id the state already carries — partial_collaborations must be the
// sorted deduplicated ids of the partial-family outcomes only.
func TestSupervisePartialSortedUniqueAndFinishedInGrace(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	herdr := collabSup(t, store, sup)
	ws := collabWorkspace(t, jobDir)
	root := collabJobDir(t, store)
	ca, idA := makeCollab(t, ws, root, "w1", "impl-1", "rev-1", "w1:p2", "w1:p3")
	cb, idB := makeCollab(t, ws, root, "w1", "impl-2", "rev-2", "w1:p4", "w1:p5")
	_, idC := makeCollab(t, ws, root, "w1", "impl-3", "rev-3", "w1:p6", "w1:p7")
	armCollabForAccept(t, cb, idB, "impl-2", "rev-2")
	team.rosterRows = collabRoster("impl-1", "rev-1", "impl-2", "rev-2", "impl-3", "rev-3")
	// The state already carries A as partial (a crashed earlier capture):
	// the new capture must keep it, never duplicate it.
	if _, err := store.Record("job-1", func(st *State) {
		st.Colaboracoes = []CollaborationOutcome{{ID: idA, Outcome: collabOutcomePartial, Verdict: "reject"}}
	}); err != nil {
		t.Fatal(err)
	}
	// B is asked with the rest and reaches finished/accept inside the
	// grace (the first wait sleep): it never counts partial.
	bDone := false
	clock.onSleep = func() {
		if bDone || clock.sleeps != 3 {
			return
		}
		if _, err := cb.Finalize(idB, "accept"); err != nil {
			t.Fatalf("harness finalize in the grace: %v", err)
		}
		bDone = true
	}
	report := writeOrchestratorReport(t, "# report\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}, {State: "done", Report: report}}
	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	st := supState(t, store)
	// A and C stand partial (created in an arbitrary order): the field is
	// their sorted deduplicated ids; B is finished, never listed.
	want := []string{idA, idC}
	sort.Strings(want)
	if got := PartialCollaborations(st); !reflect.DeepEqual(got, want) {
		t.Fatalf("partial_collaborations = %v, want the sorted %v (B finished in the grace)", got, want)
	}
	if len(st.Colaboracoes) != 3 {
		t.Fatalf("persisted outcomes = %d, want exactly one per id", len(st.Colaboracoes))
	}
	for _, o := range st.Colaboracoes {
		switch o.ID {
		case idB:
			if o.Outcome != collabOutcomeFinished || o.Verdict != "accept" {
				t.Fatalf("B outcome = %+v, want finished/accept", o)
			}
		case idA, idC:
			if o.Outcome != collabOutcomePartial || o.Verdict != "reject" {
				t.Fatalf("%s outcome = %+v, want partial/reject", o.ID, o)
			}
		default:
			t.Fatalf("unexpected persisted id %s", o.ID)
		}
	}
	// The report agrees with the state, byte for byte.
	raw, err := os.ReadFile(filepath.Join(jobDir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"partial":true,"partial_collaborations":[`+jsonArray(want)+`]`)) {
		t.Fatalf("report.json lacks the exact partial fields:\n%s", raw)
	}
	if out := findColab(t, readColabs(t, jobDir), idA); len(out.ParticipantReports) != 0 || out.Outcome != collabOutcomePartial {
		t.Fatalf("A outcome = %+v, want the persisted partial standing", out)
	}
	_ = ca
	requireNoActiveCollabs(t, ws)
	requireWorkspaceClosed(t, herdr, team)
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
}

// Mutation guard: the fields omit themselves (omitempty) or stand nil
// when there is no partial collaboration — the report must always carry
// them, false and the empty array included.
func TestSupervisePartialControlCarriesEmptyValues(t *testing.T) {
	var friction []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
	herdr := collabSup(t, store, sup)
	ws := collabWorkspace(t, jobDir)
	_ = ws
	team.rosterRows = collabRoster("impl-1", "rev-1")
	report := writeOrchestratorReport(t, "# report\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}
	exit, err := sup.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	st := supState(t, store)
	// The helper never returns nil: the empty array is the value.
	if got := PartialCollaborations(st); got == nil || len(got) != 0 {
		t.Fatalf("PartialCollaborations = %v, want the non-nil empty slice", got)
	}
	raw, err := os.ReadFile(filepath.Join(jobDir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"partial":false,"partial_collaborations":[]`)) {
		t.Fatalf("report.json lacks the empty partial fields:\n%s", raw)
	}
	_ = clock
	requireWorkspaceClosed(t, herdr, team)
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
}

// Mutation guard: settling or persisting the outcomes after the terminal
// transition (the old order) — a crash in the transition's own state
// write must leave the capture persisted (the job still finishing, the
// intent recorded, the terminal event in the log), and the restart must
// converge on the intent without a second terminal event.
func TestSupervisePartialOutcomesPersistBeforeTheTerminalTransition(t *testing.T) {
	var friction1, friction2 []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction1)
	herdr := collabSup(t, store, sup)
	ws := collabWorkspace(t, jobDir)
	_, id := makeCollab(t, ws, collabJobDir(t, store), "w1", "impl-1", "rev-1", "w1:p2", "w1:p3")
	team.rosterRows = collabRoster("impl-1", "rev-1")
	report := writeOrchestratorReport(t, "# report\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}, {State: "done", Report: report}}
	// Crash the terminal transition's state write: the first state.json
	// whose top-level outcome is the done one is the transition itself —
	// the intent record (which carries the done status inside the intent,
	// not at the top level) and the capture persist before it.
	previous := renameAtomic
	renameAtomic = func(from, to string) error {
		if strings.HasSuffix(to, "state.json") {
			if raw, err := os.ReadFile(from); err == nil {
				var tmp State
				if json.Unmarshal(raw, &tmp) == nil && tmp.Status == StatusDone {
					return errors.New("crash")
				}
			}
		}
		return previous(from, to)
	}
	t.Cleanup(func() { renameAtomic = previous })
	if _, err := sup.Run(); err == nil {
		t.Fatal("the run did not fail at the scripted transition crash")
	}
	// The capture stands persisted while the job is still finishing: the
	// partial facts exist before the outcome is visible.
	st := supState(t, store)
	if st.Status != StatusFinishing {
		t.Fatalf("status = %s, want finishing at the crash", st.Status)
	}
	if st.TerminalIntent == nil || st.TerminalIntent.Status != StatusDone {
		t.Fatalf("terminal intent = %+v, want done", st.TerminalIntent)
	}
	if len(st.Colaboracoes) != 1 || st.Colaboracoes[0].ID != id || st.Colaboracoes[0].Outcome != collabOutcomePartial {
		t.Fatalf("persisted outcomes at the crash = %+v, want the partial capture", st.Colaboracoes)
	}
	events, err := readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if countTipe(events, "terminal") != 1 {
		t.Fatalf("terminal events at the crash = %d, want one (it lands before the transition)", countTipe(events, "terminal"))
	}
	if len(friction1) != 0 {
		t.Fatalf("friction at the crash = %v, want none", friction1)
	}
	// The crash was scripted once: the restart transitions without it.
	renameAtomic = previous
	// Process two: the restart converges on the intent — one re-ask in
	// its own process, the capture's own grace, no second terminal event.
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
		t.Fatalf("restart exit = %d, want 0", exit)
	}
	if clock2.sleeps != 12 {
		t.Fatalf("restart sleeps = %d, want 12 (the capture's own grace)", clock2.sleeps)
	}
	stopPath := filepath.Join(jobDir, "stop-amend.md")
	requireDispatches(t, team2, []fakeDispatch{
		{Name: "impl-1", Path: stopPath, Amend: true},
		{Name: "rev-1", Path: stopPath, Amend: true},
	})
	if st := supState(t, store); st.Status != StatusDone || !st.WorkspaceClosed {
		t.Fatalf("state after the restart = %s/closed=%v, want done and closed", st.Status, st.WorkspaceClosed)
	}
	if out := findColab(t, readColabs(t, jobDir), id); out.Outcome != collabOutcomePartial || out.Verdict != "reject" || !out.FinalizedBySupervisor {
		t.Fatalf("restart outcome = %+v, want the persisted partial settled", out)
	}
	events, err = readEvents(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if countTipe(events, "terminal") != 1 || countTipe(events, "cleanup") != 1 {
		t.Fatalf("terminal=%d cleanup=%d after the restart, want exactly one of each", countTipe(events, "terminal"), countTipe(events, "cleanup"))
	}
	requireNoActiveCollabs(t, ws)
	requireWorkspaceClosed(t, herdr2, team2)
	if len(friction2) != 0 {
		t.Fatalf("restart friction = %v, want none", friction2)
	}
	_ = herdr
}

// Mutation guard: a later capture (the restart's) overwriting the
// persisted evidence — the participant report captured in the first pass
// must stand in the report; the fresh read of the same file after the
// participant rewrote it never replaces the persisted facts.
func TestSupervisePartialNoLateErasureOfPersistedEvidence(t *testing.T) {
	var friction1, friction2 []string
	store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction1)
	herdr := collabSup(t, store, sup)
	ws := collabWorkspace(t, jobDir)
	_, id := makeCollab(t, ws, collabJobDir(t, store), "w1", "impl-1", "rev-1", "w1:p2", "w1:p3")
	team.rosterRows = collabRoster("impl-1", "rev-1")
	stopPath, hashA := writeStopReportInWs(t, ws, "impl-1", "stop one")
	report := writeOrchestratorReport(t, "# report\n\n- [done] item\n")
	team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}, {State: "done", Report: report}}
	// The crash: inside the settlement, after the stop.
	settleCollaborationHook = func(string) { panic(errCollabTestCrash) }
	defer func() { settleCollaborationHook = nil }()
	crashed := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				if r == errCollabTestCrash {
					crashed = true
					return
				}
				panic(r)
			}
		}()
		if _, err := sup.Run(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}()
	if !crashed {
		t.Fatal("the settlement did not crash where scripted")
	}
	// The crash was scripted once for process one: process two settles
	// the finished record without it (it only confirms the phase).
	settleCollaborationHook = nil
	// The persisted evidence carries the first stop report.
	if out := findColab(t, readColabs(t, jobDir), id); len(out.ParticipantReports) != 1 || out.ParticipantReports[0].Report != stopPath || out.ParticipantReports[0].ReportSHA256 != hashA {
		t.Fatalf("persisted participant report = %+v, want the first stop report", out.ParticipantReports)
	}
	// The participant rewrites the report between the passes.
	if _, hashB := writeStopReportInWs(t, ws, "impl-1", "stop two"); hashB == hashA {
		t.Fatal("the rewritten report stands with the same hash")
	}
	// Process two: the restart re-captures from the persisted state; the
	// fresh read sees the new hash, the persisted facts stand.
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
		t.Fatalf("restart exit = %d, want 0", exit)
	}
	if clock2.sleeps != 0 || len(team2.dispatches) != 0 {
		t.Fatalf("restart sleeps=%d dispatches=%d, want none (the record stands finished)", clock2.sleeps, len(team2.dispatches))
	}
	out := findColab(t, readColabs(t, jobDir), id)
	if out.Outcome != collabOutcomePartial || out.Verdict != "reject" || out.Phase != collaboration.Finished || out.FinalizedBySupervisor {
		t.Fatalf("restart outcome = %+v, want the persisted partial, finished, without a new supervisor claim", out)
	}
	if len(out.ParticipantReports) != 1 || out.ParticipantReports[0].ReportSHA256 != hashA {
		t.Fatalf("participant report = %+v, want the persisted hash, not the later one", out.ParticipantReports)
	}
	requireNoActiveCollabs(t, ws)
	requireWorkspaceClosed(t, herdr2, team2)
	if len(friction2) != 0 {
		t.Fatalf("restart friction = %v, want none", friction2)
	}
	_ = herdr
}

// Mutation guard: a terminal path that drops the partial facts — every
// outcome (failed, canceled, timeout) must persist the capture and carry
// the partial fields with its own exit code. The done path's partial
// items rule is untouched: the items count (parciais) and the
// collaboration partial (partial) are different facts.
func TestSupervisePartialEveryTerminalPathCarriesTheFacts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cancel     bool
		timeoutMin int
		partialMD  bool
		status     string
		exit       int
	}{
		{name: "failed", partialMD: true, status: StatusFailed, exit: ExitFailed},
		{name: "canceled", cancel: true, status: StatusCanceled, exit: ExitCanceled},
		{name: "timeout", timeoutMin: 1, status: StatusTimeout, exit: ExitTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var friction []string
			store, jobDir, _, team, sup := nsSup(t, 10*time.Second, &friction)
			herdr := collabSup(t, store, sup)
			ws := collabWorkspace(t, jobDir)
			_, id := makeCollab(t, ws, collabJobDir(t, store), "w1", "impl-1", "rev-1", "w1:p2", "w1:p3")
			team.rosterRows = collabRoster("impl-1", "rev-1")
			if tc.timeoutMin > 0 {
				if _, err := store.Record("job-1", func(st *State) { st.TimeoutMin = tc.timeoutMin }); err != nil {
					t.Fatal(err)
				}
				team.statusScript = []fakeStatus{{State: "working"}}
			} else {
				body := "# report\n\n- [done] item\n"
				if tc.partialMD {
					body = "# report\n\n- [partial] item\n"
				}
				report := writeOrchestratorReport(t, body)
				team.statusScript = []fakeStatus{{State: "working"}, {State: "working"}, {State: "done", Report: report}}
			}
			if tc.cancel {
				if queued, err := store.RequestCancel("job-1", 60); !queued || err != nil {
					t.Fatal(err)
				}
			}
			exit, err := sup.Run()
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if exit != tc.exit {
				t.Fatalf("exit = %d, want %d", exit, tc.exit)
			}
			st := supState(t, store)
			if st.Status != tc.status {
				t.Fatalf("status = %s, want %s", st.Status, tc.status)
			}
			if st.TerminalIntent == nil || st.TerminalIntent.Status != tc.status {
				t.Fatalf("terminal intent = %+v, want %s", st.TerminalIntent, tc.status)
			}
			if got := PartialCollaborations(st); !reflect.DeepEqual(got, []string{id}) {
				t.Fatalf("partial_collaborations = %v, want %v", got, []string{id})
			}
			out := findColab(t, readColabs(t, jobDir), id)
			if out.Outcome != collabOutcomePartial || out.Verdict != "reject" || out.Phase != collaboration.Finished || !out.FinalizedBySupervisor {
				t.Fatalf("outcome = %+v, want the settled partial/reject", out)
			}
			raw, err := os.ReadFile(filepath.Join(jobDir, "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(raw, []byte(`"partial":true,"partial_collaborations":["`+id+`"]`)) {
				t.Fatalf("report.json lacks the exact partial fields:\n%s", raw)
			}
			// The done path's items rule stands: the failed case's report
			// still counts the item partials, separate from the
			// collaboration partial.
			if tc.name == "failed" {
				rep, err := readReport(jobDir)
				if err != nil {
					t.Fatal(err)
				}
				if rep.Parciais != 1 {
					t.Fatalf("parciais = %d, want 1 (the items rule is untouched)", rep.Parciais)
				}
			}
			requireNoActiveCollabs(t, ws)
			requireWorkspaceClosed(t, herdr, team)
			if len(friction) != 0 {
				t.Fatalf("friction = %v, want none", friction)
			}
		})
	}
}

// jsonArray is the comma-joined quoted ids, for the byte-for-byte report
// assertion.
func jsonArray(ids []string) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = `"` + id + `"`
	}
	return strings.Join(out, ",")
}

// Mutation guard: an unreadable record joining the outcomes without
// ownership proof (the decision-3 revert in endCollaborations) — foreign
// or unknown unreadable records would then stand unavailable outcomes and
// partial ids, and their friction line would carry the old wording.
func TestSuperviseCollabUnreadableRecordsOwnedOnly(t *testing.T) {
	const foreign1 = "c-00000000000000ff" // unknown ownership: a torn record
	const foreign2 = "c-00000000000000aa" // a foreign claim: another workspace
	const ownedID = "c-00000000000001ee"  // the job's own claim, one invalid field

	t.Run("foreign unreadable records are friction only", func(t *testing.T) {
		var friction []string
		store, jobDir, _, team, sup := nsSup(t, 10*time.Second, &friction)
		_ = store
		herdr := collabSup(t, store, sup)
		ws := collabWorkspace(t, jobDir)
		writeCollabRecord(t, ws, foreign1, "{")
		writeCollabRecord(t, ws, foreign2, `{"version":2,"id":"`+foreign2+`","workspace":"w2","root":"/elsewhere"}`)
		team.rosterRows = collabRoster("impl-1", "rev-1")
		report := writeOrchestratorReport(t, "# report\n\n- [done] item\n")
		team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}
		exit, err := sup.Run()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if exit != 0 {
			t.Fatalf("exit = %d, want 0", exit)
		}
		// state.json: no outcome, no asked ids, and the partial fact is
		// false with the empty list — the foreign records leave it there.
		st := supState(t, store)
		if len(st.Colaboracoes) != 0 {
			t.Fatalf("state colaboracoes = %+v, want none", st.Colaboracoes)
		}
		if got := PartialCollaborations(st); len(got) != 0 {
			t.Fatalf("PartialCollaborations = %v, want none", got)
		}
		// report.json: the same empty fact, byte for byte.
		raw, err := os.ReadFile(filepath.Join(jobDir, "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte(`"partial":false,"partial_collaborations":[]`)) {
			t.Fatalf("report.json lacks the empty partial fields:\n%s", raw)
		}
		// Exactly one friction line per foreign record, each with the
		// provably-owned wording and the record's id — nothing else.
		if len(friction) != 2 {
			t.Fatalf("friction = %v, want one line per foreign record", friction)
		}
		byID := map[string]int{}
		for _, line := range friction {
			for _, id := range []string{foreign1, foreign2} {
				if strings.HasPrefix(line, "job: the collaboration "+id+" is unreadable and not provably the job's own: ") {
					byID[id]++
				}
			}
		}
		if byID[foreign1] != 1 || byID[foreign2] != 1 {
			t.Fatalf("friction = %v, want exactly one provably-owned line per foreign record", friction)
		}
		requireWorkspaceClosed(t, herdr, team)
	})

	t.Run("the owned unreadable record stands unavailable", func(t *testing.T) {
		var friction []string
		store, jobDir, clock, team, sup := nsSup(t, 10*time.Second, &friction)
		herdr := collabSup(t, store, sup)
		ws := collabWorkspace(t, jobDir)
		root := collabJobDir(t, store)
		writeCollabRecord(t, ws, ownedID, `{"version":2,"id":"`+ownedID+`","workspace":"w1","root":"`+root+`"}`)
		team.rosterRows = collabRoster("impl-1", "rev-1")
		report := writeOrchestratorReport(t, "# report\n\n- [done] item\n")
		team.statusScript = []fakeStatus{{State: "working"}, {State: "done", Report: report}}
		exit, err := sup.Run()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if exit != 0 {
			t.Fatalf("exit = %d, want 0", exit)
		}
		// The owned unreadable record stands unavailable with its cause,
		// and no verdict.
		outcomes := readColabs(t, jobDir)
		if len(outcomes) != 1 {
			t.Fatalf("colaboracoes = %+v, want the single owned outcome", outcomes)
		}
		out := findColab(t, outcomes, ownedID)
		if out.Outcome != collabOutcomeUnavailable || out.Cause == "" || out.Verdict != "" {
			t.Fatalf("owned outcome = %+v, want unavailable with a cause and no verdict", out)
		}
		// state.json: the partial fact carries the owned id only.
		st := supState(t, store)
		if got := PartialCollaborations(st); !reflect.DeepEqual(got, []string{ownedID}) {
			t.Fatalf("PartialCollaborations = %v, want %v", got, []string{ownedID})
		}
		// report.json: the same fact, byte for byte.
		raw, err := os.ReadFile(filepath.Join(jobDir, "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte(`"partial":true,"partial_collaborations":["`+ownedID+`"]`)) {
			t.Fatalf("report.json lacks the exact partial fields:\n%s", raw)
		}
		// The owned record is named twice (the snapshot and the settle)
		// with the unreadable wording and nothing else.
		if len(friction) != 2 {
			t.Fatalf("friction = %v, want the two lines naming the owned record", friction)
		}
		for _, line := range friction {
			if !strings.HasPrefix(line, "job: the collaboration "+ownedID+" is unreadable: ") {
				t.Fatalf("friction line %q, want the unreadable wording naming %s", line, ownedID)
			}
		}
		_ = clock
		requireWorkspaceClosed(t, herdr, team)
	})
}
