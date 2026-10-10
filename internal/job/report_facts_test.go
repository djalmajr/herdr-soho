// report_facts_test.go pins the report being built from recorded facts
// instead of hardcoded defaults: the team source recorded in the state
// (equipe) reaches report.json through the Fail path and WriteReport, the
// `## Resumo` section of report.md becomes resumo, the state's branch and
// base reach the report, and the start failure paths that created or may
// have created a Herdr workspace end with the cleanup event and a current
// limpeza.workspace. A repeated recovery converges.
package job

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// reportJSON is the job's stored report.json.
func reportJSON(t *testing.T, root, id string) Report {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "jobs", id, "report.json"))
	if err != nil {
		t.Fatalf("report.json: %v", err)
	}
	var rep Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatalf("report.json: %v", err)
	}
	return rep
}

// eventTipos is the event types of the job log, in order.
func eventTipos(t *testing.T, root, id string) []string {
	t.Helper()
	events, err := readEvents(filepath.Join(root, "jobs", id))
	if err != nil {
		t.Fatal(err)
	}
	tipos := []string{}
	for _, event := range events {
		tipos = append(tipos, event.Tipo)
	}
	return tipos
}

// TestReportFactsEquipe pins the team source from recorded facts: the
// State.Equipe recorded at start reaches report.json through the Fail path
// (the report is built with empty facts) and through WriteReport; a state
// without one stays null; and the supervisor's facts win over the state.
func TestReportFactsEquipe(t *testing.T) {
	recorded := func() *Equipe {
		return &Equipe{Fonte: "teams/example-org/example-repo.conf", OverrideBrief: []string{"lane.review.effort"}}
	}

	t.Run("the state equipe reaches the report through the Fail path", func(t *testing.T) {
		s, root := startedJob(t)
		if _, err := s.Record("job-1", func(st *State) { st.Equipe = recorded() }); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Fail("job-1", "a gate failed", 19); err != nil {
			t.Fatal(err)
		}
		if rep := reportJSON(t, root, "job-1"); !reflect.DeepEqual(rep.Equipe, recorded()) {
			t.Fatalf("equipe = %+v", rep.Equipe)
		}
	})

	t.Run("the state equipe reaches the report through WriteReport", func(t *testing.T) {
		s, _ := startedJob(t)
		if _, err := s.Record("job-1", func(st *State) { st.Equipe = recorded() }); err != nil {
			t.Fatal(err)
		}
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rep.Equipe, recorded()) {
			t.Fatalf("equipe = %+v", rep.Equipe)
		}
	})

	t.Run("a state without an equipe stays null", func(t *testing.T) {
		s, root := startedJob(t)
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Equipe != nil {
			t.Fatalf("equipe = %+v, want nil", rep.Equipe)
		}
		raw, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte(`"equipe":null`)) {
			t.Fatalf("report lacks equipe null: %s", raw)
		}
	})

	t.Run("the facts equipe wins over the state", func(t *testing.T) {
		s, _ := startedJob(t)
		if _, err := s.Record("job-1", func(st *State) { st.Equipe = recorded() }); err != nil {
			t.Fatal(err)
		}
		facts := &Equipe{Fonte: "teams/default.conf", OverrideBrief: []string{}}
		rep, err := s.WriteReport("job-1", ReportFacts{Equipe: facts})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rep.Equipe, facts) {
			t.Fatalf("equipe = %+v, want the facts", rep.Equipe)
		}
	})
}

// TestReportFactsResumoSection pins the resumo extraction: the `## Resumo`
// section of report.md (the line is exactly the heading, trailing spaces
// allowed) runs up to the next line starting with `#`; fenced code blocks
// are skipped with the reportItems fence rules; lines are trimmed, empty
// lines dropped, at most the first six kept, each cut to the resumo limit,
// joined with \n. No section is "". A non-empty facts resumo wins.
func TestReportFactsResumoSection(t *testing.T) {
	t.Run("the section lines become the resumo", func(t *testing.T) {
		s, root := startedJob(t)
		writeReportMD(t, root, strings.Join([]string{
			"# report",
			"",
			"## Resumo   ",
			"changed the loader.",
			"",
			"proved it with the gate.",
			"```",
			"a fenced line that must not appear",
			"```",
			"proved it twice.",
			"",
			"## Itens",
			"- [done] the item",
			"",
		}, "\n"))
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatal(err)
		}
		if want := "changed the loader.\nproved it with the gate.\nproved it twice."; rep.Resumo != want {
			t.Fatalf("resumo = %q, want %q", rep.Resumo, want)
		}
	})

	t.Run("more than six lines keeps the first six", func(t *testing.T) {
		s, root := startedJob(t)
		lines := []string{"## Resumo"}
		for i := 1; i <= 8; i++ {
			lines = append(lines, "line "+string(rune('a'+i-1)))
		}
		writeReportMD(t, root, strings.Join(lines, "\n"))
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatal(err)
		}
		if want := "line a\nline b\nline c\nline d\nline e\nline f"; rep.Resumo != want {
			t.Fatalf("resumo = %q, want %q", rep.Resumo, want)
		}
	})

	t.Run("a long section line is cut to the resumo limit", func(t *testing.T) {
		s, root := startedJob(t)
		writeReportMD(t, root, "## Resumo\n"+strings.Repeat("é", 300)+"\n")
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatal(err)
		}
		if got := len([]rune(rep.Resumo)); got != resumoLimit {
			t.Fatalf("resumo = %d code points, want %d", got, resumoLimit)
		}
	})

	t.Run("no section is empty", func(t *testing.T) {
		s, root := startedJob(t)
		writeReportMD(t, root, "# report\n\n- [done] x\n")
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Resumo != "" {
			t.Fatalf("resumo = %q, want empty", rep.Resumo)
		}
	})

	t.Run("a missing report.md is empty", func(t *testing.T) {
		s, _ := startedJob(t)
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Resumo != "" {
			t.Fatalf("resumo = %q, want empty", rep.Resumo)
		}
	})

	t.Run("the facts resumo wins over the section", func(t *testing.T) {
		s, root := startedJob(t)
		writeReportMD(t, root, "## Resumo\nsection line\n")
		rep, err := s.WriteReport("job-1", ReportFacts{Resumo: "facts line"})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Resumo != "facts line" {
			t.Fatalf("resumo = %q, want the facts", rep.Resumo)
		}
	})
}

// TestReportFactsBranchBase pins the branch and base from the recorded
// state (the workspace mode records the repository branch and base) with
// the report's historical job/<id> branch and the brief's base as the
// fallback for a state that recorded none.
func TestReportFactsBranchBase(t *testing.T) {
	t.Run("the workspace-mode state carries its branch and base", func(t *testing.T) {
		s, _ := startedJob(t)
		// briefA carries no base field: the recorded state facts must
		// reach the report instead of the brief.
		if _, err := s.Record("job-1", func(st *State) {
			st.Branch = "main"
			st.Base = "main"
		}); err != nil {
			t.Fatal(err)
		}
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Branch != "main" || rep.Base != "main" {
			t.Fatalf("branch/base = %q/%q, want main/main", rep.Branch, rep.Base)
		}
	})

	t.Run("an empty state falls back to the job branch and the brief base", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		brief := `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","base":"release/1","objetivo":"Ship the change.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/job"}]}`
		if _, err := s.Start("job-1", []byte(brief)); err != nil {
			t.Fatal(err)
		}
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Branch != "job/job-1" || rep.Base != "release/1" {
			t.Fatalf("branch/base = %q/%q, want job/job-1/release-1", rep.Branch, rep.Base)
		}
	})
}

// TestReportFactsStartCleanup pins the start failure paths that created or
// may have created a Herdr workspace: the recovery of a crashed start that
// closes the workspace ends with the cleanup event (the supervisor's
// finalize resumo) and limpeza.workspace closed; an unproven identity
// ends with the cleanup event and open; a failure without a workspace
// gets neither; a second recovery adds nothing; and a starter failure
// after the create records the close it made or the open it left.
func TestReportFactsStartCleanup(t *testing.T) {
	recovered := func(t *testing.T, root string, workspaces []herdrWorkspace) (State, *fakeHerdr) {
		t.Helper()
		store := crashedStartState(t, root)
		fake := &fakeHerdr{Workspaces: workspaces}
		st, rec, err := RecoverStart(store, "job-1", fake, nil, nil)
		if err != nil || !rec {
			t.Fatalf("RecoverStart: %+v err=%v recovered=%v", st, err, rec)
		}
		return st, fake
	}

	t.Run("a recovery that closes the workspace ends with the cleanup event", func(t *testing.T) {
		root := t.TempDir()
		st, fake := recovered(t, root, []herdrWorkspace{{ID: "w9", Label: "job-job-1", PaneCount: 1}})
		if st.WorkspaceID != "w9" || !st.WorkspaceClosed {
			t.Fatalf("state = %+v, want w9 closed", st)
		}
		if !reflect.DeepEqual(fake.CloseIDs, []string{"w9"}) {
			t.Fatalf("CloseIDs = %v, want exactly [w9]", fake.CloseIDs)
		}
		if tipos := eventTipos(t, root, "job-1"); !reflect.DeepEqual(tipos, []string{"accepted", "preparing", "failure", "terminal", "cleanup"}) {
			t.Fatalf("event tipos = %v", tipos)
		}
		events, err := readEvents(filepath.Join(root, "jobs", "job-1"))
		if err != nil {
			t.Fatal(err)
		}
		if last := events[len(events)-1]; last.Resumo != "processes released; worktree kept" {
			t.Fatalf("cleanup resumo = %q", last.Resumo)
		}
		if rep := reportJSON(t, root, "job-1"); rep.Limpeza.Workspace != "closed" {
			t.Fatalf("limpeza = %+v, want closed", rep.Limpeza)
		}
	})

	t.Run("an unproven identity ends with the cleanup event and open", func(t *testing.T) {
		root := t.TempDir()
		_, _ = recovered(t, root, []herdrWorkspace{
			{ID: "w1", Label: "job-job-1", PaneCount: 1},
			{ID: "w2", Label: "job-job-1", PaneCount: 1},
		})
		if tipos := eventTipos(t, root, "job-1"); !reflect.DeepEqual(tipos, []string{"accepted", "preparing", "failure", "terminal", "cleanup"}) {
			t.Fatalf("event tipos = %v", tipos)
		}
		if rep := reportJSON(t, root, "job-1"); rep.Limpeza.Workspace != "open" {
			t.Fatalf("limpeza = %+v, want open", rep.Limpeza)
		}
	})

	t.Run("a failure without a workspace gets neither", func(t *testing.T) {
		root := t.TempDir()
		_, _ = recovered(t, root, []herdrWorkspace{{ID: "w0", Label: "job-job-1", PaneCount: 1}})
		if tipos := eventTipos(t, root, "job-1"); !reflect.DeepEqual(tipos, []string{"accepted", "preparing", "failure", "terminal"}) {
			t.Fatalf("event tipos = %v", tipos)
		}
		if rep := reportJSON(t, root, "job-1"); rep.Limpeza.Workspace != "" {
			t.Fatalf("limpeza = %+v, want no workspace", rep.Limpeza)
		}
	})

	t.Run("a second recovery adds nothing", func(t *testing.T) {
		root := t.TempDir()
		store := crashedStartState(t, root)
		fake := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w9", Label: "job-job-1", PaneCount: 1}}}
		if _, rec, err := RecoverStart(store, "job-1", fake, nil, nil); err != nil || !rec {
			t.Fatalf("first: rec=%v err=%v", rec, err)
		}
		report, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		first := eventTipos(t, root, "job-1")
		if _, rec, err := RecoverStart(store, "job-1", fake, nil, nil); err != nil || rec {
			t.Fatalf("second: rec=%v err=%v", rec, err)
		}
		if tipos := eventTipos(t, root, "job-1"); !reflect.DeepEqual(tipos, first) {
			t.Fatalf("the second recovery appended events: %v", tipos)
		}
		after, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "report.json"))
		if err != nil || !bytes.Equal(report, after) {
			t.Fatalf("report.json changed by the second recovery")
		}
		if !reflect.DeepEqual(fake.CloseIDs, []string{"w9"}) {
			t.Fatalf("CloseIDs = %v, want the single first close", fake.CloseIDs)
		}
	})

	t.Run("a start pane-run failure closes the workspace and records it", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		f.cloneCheckout()
		fake := startFake()
		fake.RunErr = &ExitError{Code: ExitHerdr, Msg: "job: herdr pane run failed"}
		stateRoot := t.TempDir()
		var friction []string
		s := startStarter(t, f, fake, stateRoot, &friction)
		_, store, err := s.Start(startRequest("main", briefA))
		if err == nil {
			t.Fatal("Start succeeded despite the pane run failure")
		}
		snap, err := store.Snapshot("job-1")
		if err != nil {
			t.Fatal(err)
		}
		if !snap.State.WorkspaceClosed {
			t.Fatalf("state = %+v, want the close recorded", snap.State)
		}
		if !reflect.DeepEqual(fake.CloseIDs, []string{"w9"}) {
			t.Fatalf("CloseIDs = %v, want exactly [w9]", fake.CloseIDs)
		}
		if tipos := eventTipos(t, stateRoot, "job-1"); !reflect.DeepEqual(tipos, []string{"accepted", "preparing", "failure", "terminal", "cleanup"}) {
			t.Fatalf("event tipos = %v", tipos)
		}
		if rep := reportJSON(t, stateRoot, "job-1"); rep.Limpeza.Workspace != "closed" {
			t.Fatalf("limpeza = %+v, want closed", rep.Limpeza)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v, want none", friction)
		}
	})

	t.Run("a failed close leaves the workspace open", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		f.cloneCheckout()
		fake := startFake()
		fake.RunErr = &ExitError{Code: ExitHerdr, Msg: "job: herdr pane run failed"}
		fake.CloseErr = errors.New("close down")
		stateRoot := t.TempDir()
		var friction []string
		s := startStarter(t, f, fake, stateRoot, &friction)
		_, store, err := s.Start(startRequest("main", briefA))
		if err == nil {
			t.Fatal("Start succeeded despite the pane run failure")
		}
		snap, err := store.Snapshot("job-1")
		if err != nil {
			t.Fatal(err)
		}
		if snap.State.WorkspaceClosed {
			t.Fatalf("state = %+v, want no close record", snap.State)
		}
		if tipos := eventTipos(t, stateRoot, "job-1"); !reflect.DeepEqual(tipos, []string{"accepted", "preparing", "failure", "terminal", "cleanup"}) {
			t.Fatalf("event tipos = %v", tipos)
		}
		if rep := reportJSON(t, stateRoot, "job-1"); rep.Limpeza.Workspace != "open" {
			t.Fatalf("limpeza = %+v, want open", rep.Limpeza)
		}
		if len(friction) != 1 || !strings.Contains(friction[0], "close down") {
			t.Fatalf("friction = %v, want the close error only", friction)
		}
	})

	t.Run("an invalid create id leaves the workspace open and records it", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		fake := startFake()
		fake.CreateWorkspaceID = "../escape"
		fake.CreateRootPaneID = "w9:p1"
		stateRoot := t.TempDir()
		var friction []string
		s := startStarter(t, f, fake, stateRoot, &friction)
		_, store, err := s.Start(startRequest("main", briefA))
		wantExit(t, err, ExitHerdr, "job: herdr workspace create returned an invalid workspace id")
		snap, err := store.Snapshot("job-1")
		if err != nil {
			t.Fatal(err)
		}
		if snap.State.WorkspaceID != "" || snap.State.WorkspaceClosed {
			t.Fatalf("state = %+v, want no workspace record", snap.State)
		}
		if len(fake.CloseIDs) != 0 {
			t.Fatalf("CloseIDs = %v, want none (the workspace is left open on purpose)", fake.CloseIDs)
		}
		if tipos := eventTipos(t, stateRoot, "job-1"); !reflect.DeepEqual(tipos, []string{"accepted", "preparing", "failure", "terminal", "cleanup"}) {
			t.Fatalf("event tipos = %v", tipos)
		}
		if rep := reportJSON(t, stateRoot, "job-1"); rep.Limpeza.Workspace != "open" {
			t.Fatalf("limpeza = %+v, want open", rep.Limpeza)
		}
	})
}
