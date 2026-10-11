package job

// The colaboracoes field of report.json: omitted when empty, the exact
// shape and key order when present, and kept as stored by every later
// rewrite (the collected and closed transitions).

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// The field is omitted when the job has no collaborations: the report
// carries no colaboracoes key and the top-level key order is the contract
// order unchanged.
func TestReportCollabOmittedWhenEmpty(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if _, err := s.Start("job-1", []byte(supTestBrief)); err != nil {
		t.Fatal(err)
	}
	writeReportMD(t, root, "# report\n\n- [done] item\n")
	if _, err := s.WriteReport("job-1", ReportFacts{}); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	raw := storedReport(t, root)
	var shape struct {
		Colaboracoes json.RawMessage `json:"colaboracoes"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatalf("unmarshal the stored report: %v", err)
	}
	if shape.Colaboracoes != nil {
		t.Fatalf("report carries colaboracoes with none to record:\n%s", raw)
	}
	// The stored keys are a subsequence of the contract order: a zero-
	// facts report omits the always-empty contract fields, but it never
	// reorders what it carries.
	keys := topKeys(t, raw)
	pos := -1
	for _, key := range keys {
		idx := -1
		for i, want := range reportFieldOrder {
			if want == key {
				idx = i
				break
			}
		}
		if idx <= pos {
			t.Fatalf("top-level key %q is out of the contract order:\n%v", key, keys)
		}
		pos = idx
	}
}

// The shape of one outcome: the key order, the empty-field omissions and
// the placement between memoria and equipe, byte for byte.
func TestReportCollabShapeAndKeyOrder(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if _, err := s.Start("job-1", []byte(supTestBrief)); err != nil {
		t.Fatal(err)
	}
	writeReportMD(t, root, "# report\n\n- [done] item\n")
	facts := fullReportFacts()
	facts.Colaboracoes = []CollaborationOutcome{
		{
			ID: "c-0000000000000001", Phase: "finished", Round: 1, Revision: "rev",
			LastEvent: "reviewed by rev-1, delivery submitted",
			Report:    "/abs/review.md", ReportHash: "hash-a", ReportSHA256: "sha-a",
			ParticipantReports: []ParticipantReport{
				{Name: "impl-1", Report: "/abs/stop.md", ReportSHA256: "sha-b"},
			},
			Verdict: "reject", Outcome: "partial", Cause: "the record was lost",
			FinalizedBySupervisor: true,
		},
		// The unavailable outcome stands with its cause and no optional
		// field: the empties are omitted, not nulled.
		{
			ID: "c-0000000000000002", Phase: "", LastEvent: "none",
			Outcome: "unavailable", Cause: "unreadable record",
		},
	}
	if _, err := s.WriteReport("job-1", facts); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	raw := storedReport(t, root)
	// The field is placed between memoria and equipe.
	idxMemoria := bytes.Index(raw, []byte(`"memoria"`))
	idxColab := bytes.Index(raw, []byte(`"colaboracoes"`))
	idxEquipe := bytes.Index(raw, []byte(`"equipe"`))
	if idxMemoria < 0 || idxColab < 0 || idxEquipe < 0 || !(idxMemoria < idxColab && idxColab < idxEquipe) {
		t.Fatalf("colaboracoes placement = memoria@%d colaboracoes@%d equipe@%d, want in between", idxMemoria, idxColab, idxEquipe)
	}
	// The stored array is exactly the compact marshal of the outcomes:
	// the key order of the outcome and of the participant report, and the
	// omission of every empty optional field.
	var shape struct {
		Colaboracoes json.RawMessage `json:"colaboracoes"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatalf("unmarshal the stored report: %v", err)
	}
	if shape.Colaboracoes == nil {
		t.Fatalf("report has no colaboracoes:\n%s", raw)
	}
	marshaled, err := json.Marshal(facts.Colaboracoes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(shape.Colaboracoes, marshaled) {
		t.Fatalf("stored colaboracoes = %s, want the compact marshal\n%s", shape.Colaboracoes, marshaled)
	}
	// The order spelled out: the byte equality above pins it, and this
	// per-key walk fails loudly if a key ever moves or drops.
	pos := -1
	for _, key := range []string{"id", "phase", "round", "revision", "last_event", "report", "report_hash", "report_sha256", "participant_reports", "verdict", "outcome", "cause", "finalized_by_supervisor"} {
		idx := bytes.Index(marshaled, []byte(`"`+key+`":`))
		if idx <= pos {
			t.Fatalf("outcome key %q is missing or out of order:\n%s", key, marshaled)
		}
		pos = idx
	}
}

// Every later rewrite keeps the stored outcomes: the collected and closed
// transitions rewrite the report (the event counters move) without touching
// colaboracoes, and the terminal status stands.
func TestReportCollabKeptAcrossCollectAndClose(t *testing.T) {
	var friction []string
	store, jobDir, _, team, sup := nsSup(t, 10*time.Second, &friction)
	herdr := collabSup(t, store, sup)
	ws := collabWorkspace(t, jobDir)
	_, id := makeCollab(t, ws, collabJobDir(t, store), "w1", "impl-1", "rev-1", "w1:p2", "w1:p3")
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
	first, err := readReport(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	before := findColab(t, first.Colaboracoes, id)
	if _, err := store.Transition("job-1", StatusCollected); err != nil {
		t.Fatal(err)
	}
	mid, err := readReport(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition("job-1", StatusClosed); err != nil {
		t.Fatal(err)
	}
	last, err := readReport(jobDir)
	if err != nil {
		t.Fatal(err)
	}
	// The rewrites keep the stored outcomes and the terminal status;
	// only the state moves on.
	if got := findColab(t, mid.Colaboracoes, id); !reflect.DeepEqual(got, before) {
		t.Fatalf("outcome changed across the collected transition:\n%+v\n---\n%+v", before, got)
	}
	if got := findColab(t, last.Colaboracoes, id); !reflect.DeepEqual(got, before) {
		t.Fatalf("outcome changed across the closed transition:\n%+v\n---\n%+v", before, got)
	}
	// The terminal outcome stands in the report across both transitions.
	if mid.Status != StatusDone || last.Status != StatusDone {
		t.Fatalf("report status = %s / %s, want the terminal outcome standing", mid.Status, last.Status)
	}
	if mid.Eventos != first.Eventos || last.Eventos != first.Eventos {
		t.Fatalf("event counters moved across the rewrites: %+v -> %+v -> %+v", first.Eventos, mid.Eventos, last.Eventos)
	}
	if st := supState(t, store); st.Status != StatusClosed || !st.WorkspaceClosed {
		t.Fatalf("state = %s/closed=%v, want closed", st.Status, st.WorkspaceClosed)
	}
	if len(herdr.CloseIDs) != 1 {
		t.Fatalf("close calls = %v, want the job workspace once (the supervisor's)", herdr.CloseIDs)
	}
	if len(friction) != 0 {
		t.Fatalf("friction = %v, want none", friction)
	}
}
