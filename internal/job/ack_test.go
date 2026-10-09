package job

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAckClose(t *testing.T) {
	t.Run("pending decisions and monotonic ack", func(t *testing.T) {
		s, _ := startedJob(t)
		if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "one", Escopo: "global", Motivo: "a"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "two", Escopo: "projeto", Motivo: "b"}); err != nil {
			t.Fatal(err)
		}
		skip, err := s.OlderThanCleanSkips("job-1")
		if err != nil || !skip {
			t.Fatalf("skip=%v err=%v", skip, err)
		}
		if _, err := s.Ack("job-1", 0); exitCode(t, err) != ExitUsage {
			t.Fatalf("ack 0: %v", err)
		}
		st, err := s.Ack("job-1", 2)
		if err != nil || st.DecisionsAckedSeq != 2 {
			t.Fatalf("ack 2: %+v err=%v", st, err)
		}
		st, err = s.Ack("job-1", 1)
		if err != nil || st.DecisionsAckedSeq != 2 {
			t.Fatalf("lower ack: %+v err=%v", st, err)
		}
		st, err = s.Ack("job-1", 2)
		if err != nil || st.DecisionsAckedSeq != 2 {
			t.Fatalf("retry ack: %+v err=%v", st, err)
		}
		if n := countTipo(t, s, "decision_acked"); n != 1 {
			t.Fatalf("decision_acked events = %d", n)
		}
		skip, err = s.OlderThanCleanSkips("job-1")
		if err != nil || !skip {
			t.Fatalf("still pending: skip=%v err=%v", skip, err)
		}
		if _, err := s.Ack("job-1", 3); err != nil {
			t.Fatal(err)
		}
		skip, err = s.OlderThanCleanSkips("job-1")
		if err != nil || skip {
			t.Fatalf("acked job still skipped: %v err=%v", skip, err)
		}
	})

	t.Run("close lists pending seqs", func(t *testing.T) {
		s, _ := startedJob(t)
		noteDecision(t, s, "global")
		noteDecision(t, s, "projeto")
		toDone(t, s)
		_, err := s.Close("job-1", false)
		exit, ok := err.(*ExitError)
		if !ok || exit.Code != ExitUnacked || len(exit.Seqs) != 2 || exit.Seqs[0] != 2 || exit.Seqs[1] != 3 {
			t.Fatalf("close err = %v", err)
		}
		if !strings.Contains(err.Error(), "2") || !strings.Contains(err.Error(), "3") {
			t.Fatalf("error does not list seqs: %v", err)
		}
		st, err := readState(filepath.Join(s.Root, "jobs", "job-1"))
		if err != nil || st.Status != StatusDone {
			t.Fatalf("status after refused close: %+v err=%v", st, err)
		}
		if n := countTipo(t, s, "cleanup"); n != 0 {
			t.Fatalf("cleanup events = %d", n)
		}
		if _, err := s.Close("job-1", false); exitCode(t, err) != ExitUnacked {
			t.Fatalf("retry close: %v", err)
		}
	})

	t.Run("force close records cleanup and retry converges", func(t *testing.T) {
		s, _ := startedJob(t)
		noteDecision(t, s, "global")
		toDone(t, s)
		st, err := s.Close("job-1", true)
		if err != nil || st.Status != StatusClosed {
			t.Fatalf("force close: %+v err=%v", st, err)
		}
		page := mustPage(t, s, 0, 0)
		var cleanup int
		for _, event := range page.Events {
			if event.Tipo != "cleanup" {
				continue
			}
			cleanup++
			if event.Refs["motivo"] != "force_sem_ack" {
				t.Fatalf("cleanup = %+v", event)
			}
		}
		if cleanup != 1 {
			t.Fatalf("cleanup events = %d", cleanup)
		}
		st, err = s.Close("job-1", true)
		if err != nil || st.Status != StatusClosed {
			t.Fatalf("retry close: %+v err=%v", st, err)
		}
		if n := countTipo(t, s, "cleanup"); n != 1 {
			t.Fatalf("retry cleanup events = %d", n)
		}
	})

	t.Run("close without pending decisions", func(t *testing.T) {
		s, _ := startedJob(t)
		toDone(t, s)
		st, err := s.Close("job-1", false)
		if err != nil || st.Status != StatusClosed {
			t.Fatalf("close: %+v err=%v", st, err)
		}
		page := mustPage(t, s, 0, 0)
		for _, event := range page.Events {
			if event.Tipo == "cleanup" && event.Refs["motivo"] != "" {
				t.Fatalf("cleanup motivo = %q", event.Refs["motivo"])
			}
		}
	})

	t.Run("crash after the cleanup event closes once", func(t *testing.T) {
		s, root := startedJob(t)
		toDone(t, s)
		previous := renameAtomic
		failed := false
		renameAtomic = func(from, to string) error {
			if !failed && strings.HasSuffix(to, "state.json") {
				failed = true
				return errors.New("crash")
			}
			return previous(from, to)
		}
		t.Cleanup(func() { renameAtomic = previous })
		if _, err := s.Close("job-1", false); err == nil {
			t.Fatal("expected crash")
		}
		st, err := readState(filepath.Join(root, "jobs", "job-1"))
		if err != nil || st.Status != StatusDone {
			t.Fatalf("state after crash: %+v err=%v", st, err)
		}
		if n := countTipo(t, s, "cleanup"); n != 1 {
			t.Fatalf("cleanup after crash = %d", n)
		}
		renameAtomic = previous
		st, err = s.Close("job-1", false)
		if err != nil || st.Status != StatusClosed {
			t.Fatalf("retry: %+v err=%v", st, err)
		}
		if n := countTipo(t, s, "cleanup"); n != 1 {
			t.Fatalf("cleanup after retry = %d", n)
		}
	})

	t.Run("close from running is refused", func(t *testing.T) {
		s, _ := startedJob(t)
		_, err := s.Close("job-1", true)
		if exitCode(t, err) != ExitUsage {
			t.Fatalf("close running: %v", err)
		}
	})

	t.Run("report keeps the worktree and sets removivel", func(t *testing.T) {
		s, root := startedJob(t)
		if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "share", Escopo: "global", Motivo: "wider", Refs: map[string]string{"sha": "abc"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "local", Escopo: "projeto", Motivo: "here"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Ack("job-1", 2); err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			facts ReportFacts
			keep  bool
			sync  bool
		}{
			{ReportFacts{Clean: true, Head: "abc", HeadRemoto: "abc"}, true, true},
			{ReportFacts{Clean: false, Head: "abc", HeadRemoto: "abc"}, false, true},
			{ReportFacts{Clean: true, Head: "abc", HeadRemoto: "def"}, false, false},
			{ReportFacts{Clean: true}, false, false},
		}
		for _, tc := range cases {
			rep, err := s.WriteReport("job-1", tc.facts)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Limpeza.Worktree != "kept" || rep.Limpeza.Removivel != tc.keep || rep.Sincronizado != tc.sync {
				t.Fatalf("facts %+v report limpeza=%+v sync=%v", tc.facts, rep.Limpeza, rep.Sincronizado)
			}
			if rep.Branch != "job/job-1" || rep.PR != nil {
				t.Fatalf("branch/pr = %q %#v", rep.Branch, rep.PR)
			}
		}
		raw, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("retencao_ate")) {
			t.Fatalf("report contains retencao_ate: %s", raw)
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(filepath.Join(root, "jobs", "job-1", "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("report mode %o", info.Mode().Perm())
			}
		}
		rep, err := s.WriteReport("job-1", ReportFacts{Clean: true, Head: "abc", HeadRemoto: "abc", Resumo: "done"})
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Memoria.Global) != 1 || rep.Memoria.Global[0].Seq != 2 || rep.Memoria.Global[0].Motivo != "wider" || rep.Memoria.Global[0].Refs["sha"] != "abc" {
			t.Fatalf("global = %+v", rep.Memoria.Global)
		}
		if len(rep.Memoria.Projeto) != 1 || rep.Memoria.Projeto[0].Seq != 3 || rep.Memoria.Projeto[0].Resumo != "local" {
			t.Fatalf("projeto = %+v", rep.Memoria.Projeto)
		}
		if rep.Memoria.DecisionsTotal != 2 || rep.Memoria.DecisionsAckedSeq != 2 {
			t.Fatalf("memoria counts = %+v", rep.Memoria)
		}
		if len(rep.Publico.IDs) != 1 || rep.Publico.IDs[0] != "CARD-1" {
			t.Fatalf("publico = %+v", rep.Publico)
		}
	})
}

func TestAckCloseCrashAfterAppend(t *testing.T) {
	s, root := startedJob(t)
	if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "one", Escopo: "global", Motivo: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "two", Escopo: "projeto", Motivo: "b"}); err != nil {
		t.Fatal(err)
	}
	// Crash after the decision_acked append and before the state save: the
	// event append is a plain O_APPEND write, so only the state rename fails.
	previous := renameAtomic
	renameAtomic = func(from, to string) error {
		if strings.HasSuffix(to, "state.json") {
			return errors.New("crash")
		}
		return previous(from, to)
	}
	t.Cleanup(func() { renameAtomic = previous })
	if _, err := s.Ack("job-1", 3); err == nil {
		t.Fatal("expected the state save crash")
	}
	page := mustPage(t, s, 0, 0)
	acked := 0
	for _, event := range page.Events {
		if event.Tipo != "decision_acked" {
			continue
		}
		acked++
		if event.Refs["seq_ref"] != "3" {
			t.Fatalf("acked refs = %+v, want seq_ref 3", event)
		}
	}
	if acked != 1 {
		t.Fatalf("decision_acked events = %d, want 1", acked)
	}
	st, err := readState(filepath.Join(root, "jobs", "job-1"))
	if err != nil || st.DecisionsAckedSeq != 0 {
		t.Fatalf("state after crash: %+v err=%v, want watermark 0", st, err)
	}
	renameAtomic = previous
	st, err = s.Ack("job-1", 3)
	if err != nil || st.DecisionsAckedSeq != 3 {
		t.Fatalf("retry ack: %+v err=%v", st, err)
	}
	if n := countTipo(t, s, "decision_acked"); n != 1 {
		t.Fatalf("retry appended another event: %d", n)
	}
	page = mustPage(t, s, 0, 0)
	if len(page.Events) != 4 {
		t.Fatalf("events = %+v, want 4 consecutive", page.Events)
	}
	for i, event := range page.Events {
		if event.Seq != i+1 {
			t.Fatalf("seqs not consecutive: %+v", page.Events)
		}
	}
	skip, err := s.OlderThanCleanSkips("job-1")
	if err != nil || skip {
		t.Fatalf("skip=%v err=%v, want fully acked", skip, err)
	}
	if _, err := s.Ack("job-1", 2); err != nil {
		t.Fatal(err)
	}
	if n := countTipo(t, s, "decision_acked"); n != 1 {
		t.Fatalf("lower ack appended: %d", n)
	}
}

func TestAckCloseReportRenameCrash(t *testing.T) {
	s, root := startedJob(t)
	toDone(t, s)
	if _, err := s.Transition("job-1", StatusCollected); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "jobs", "job-1")
	// Crash after the state is saved closed and before report.json is
	// rewritten. Close from collected is the remaining window: a close from
	// a terminal outcome publishes the report while the state still carries
	// the outcome, before the state save.
	previous := renameAtomic
	renameAtomic = func(from, to string) error {
		if strings.HasSuffix(to, "report.json") {
			return errors.New("crash")
		}
		return previous(from, to)
	}
	t.Cleanup(func() { renameAtomic = previous })
	if _, err := s.Close("job-1", false); err == nil {
		t.Fatal("expected the report save crash")
	}
	st, err := readState(dir)
	if err != nil || st.Status != StatusClosed {
		t.Fatalf("state after crash: %+v err=%v, want closed", st, err)
	}
	if n := countTipo(t, s, "cleanup"); n != 1 {
		t.Fatalf("cleanup events = %d, want 1", n)
	}
	renameAtomic = previous
	// The retry must repair the missing or stale report without a second
	// cleanup event.
	st, err = s.Close("job-1", false)
	if err != nil || st.Status != StatusClosed {
		t.Fatalf("retry close: %+v err=%v", st, err)
	}
	if n := countTipo(t, s, "cleanup"); n != 1 {
		t.Fatalf("retry appended a second cleanup: %d", n)
	}
	page := mustPage(t, s, 0, 0)
	rep, err := readReport(dir)
	if err != nil {
		t.Fatalf("report after retry: %v", err)
	}
	if rep.Status != StatusDone {
		t.Fatalf("report status = %q, want the terminal outcome done", rep.Status)
	}
	if rep.Eventos.Total != len(page.Events) || rep.Eventos.UltimoSeq != page.UltimoSeq {
		t.Fatalf("eventos = %+v, want total=%d ultimo=%d", rep.Eventos, len(page.Events), page.UltimoSeq)
	}
	if rep.Limpeza.Worktree != "kept" {
		t.Fatalf("limpeza = %+v", rep.Limpeza)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("retencao_ate")) {
		t.Fatalf("report contains retencao_ate: %s", raw)
	}
	// A third close is still a no-op with a current report.
	st, err = s.Close("job-1", false)
	if err != nil || st.Status != StatusClosed {
		t.Fatalf("third close: %+v err=%v", st, err)
	}
	if n := countTipo(t, s, "cleanup"); n != 1 {
		t.Fatalf("third close appended a cleanup: %d", n)
	}
}

func TestAckCloseForcedAfterOrdinaryCleanup(t *testing.T) {
	s, _ := startedJob(t)
	toDone(t, s)
	noteDecision(t, s, "global")
	// The terminal job already holds a trailing ordinary cleanup from the
	// release; the pending decision sits below it.
	if _, err := s.Append("job-1", EventIn{Tipo: "cleanup", Resumo: "release"}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Close("job-1", true)
	if err != nil || st.Status != StatusClosed {
		t.Fatalf("force close: %+v err=%v", st, err)
	}
	page := mustPage(t, s, 0, 0)
	last := page.Events[len(page.Events)-1]
	if last.Tipo != "cleanup" || last.Refs["motivo"] != "force_sem_ack" {
		t.Fatalf("last event = %+v, want cleanup motivo force_sem_ack", last)
	}
	cleanups := 0
	for _, event := range page.Events {
		if event.Tipo == "cleanup" {
			cleanups++
		}
	}
	if cleanups != 2 {
		t.Fatalf("cleanup events = %d, want the ordinary one plus the forced audit", cleanups)
	}
	// The retry appends nothing: the forced audit already exists.
	st, err = s.Close("job-1", true)
	if err != nil || st.Status != StatusClosed {
		t.Fatalf("retry force close: %+v err=%v", st, err)
	}
	page = mustPage(t, s, 0, 0)
	cleanups = 0
	for _, event := range page.Events {
		if event.Tipo == "cleanup" {
			cleanups++
		}
	}
	if cleanups != 2 {
		t.Fatalf("retry appended a cleanup: %d", cleanups)
	}
}

func TestAckCloseCrashBeforeAppend(t *testing.T) {
	s, root := startedJob(t)
	if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "one", Escopo: "global", Motivo: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "two", Escopo: "projeto", Motivo: "b"}); err != nil {
		t.Fatal(err)
	}
	// Crash before the acknowledgement event is published: nothing is durable.
	previous := appendEventFn
	appendEventFn = func(dir string, now time.Time, in EventIn) (Event, error) {
		return Event{}, errors.New("crash")
	}
	t.Cleanup(func() { appendEventFn = previous })
	if _, err := s.Ack("job-1", 3); err == nil {
		t.Fatal("expected the append crash")
	}
	if n := countTipo(t, s, "decision_acked"); n != 0 {
		t.Fatalf("crash published an event: %d", n)
	}
	st, err := readState(filepath.Join(root, "jobs", "job-1"))
	if err != nil || st.DecisionsAckedSeq != 0 {
		t.Fatalf("state after crash: %+v err=%v, want watermark 0", st, err)
	}
	appendEventFn = previous
	// The retry appends exactly once and converges.
	st, err = s.Ack("job-1", 3)
	if err != nil || st.DecisionsAckedSeq != 3 {
		t.Fatalf("retry ack: %+v err=%v", st, err)
	}
	if n := countTipo(t, s, "decision_acked"); n != 1 {
		t.Fatalf("decision_acked events = %d, want 1", n)
	}
	page := mustPage(t, s, 0, 0)
	if len(page.Events) != 4 {
		t.Fatalf("events = %+v, want 4 consecutive", page.Events)
	}
	for i, event := range page.Events {
		if event.Seq != i+1 {
			t.Fatalf("seqs not consecutive: %+v", page.Events)
		}
	}
	last := page.Events[len(page.Events)-1]
	if last.Tipo != "decision_acked" || last.Refs["seq_ref"] != "3" {
		t.Fatalf("last event = %+v, want decision_acked seq_ref 3", last)
	}
}

func TestAckCloseSnapshot(t *testing.T) {
	s, root := startedJob(t)
	dir := filepath.Join(root, "jobs", "job-1")
	if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "one", Escopo: "global", Motivo: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "two", Escopo: "projeto", Motivo: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ack("job-1", 2); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, dir)
	snap, err := s.Snapshot("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.Status != StatusAccepted || snap.State.DecisionsAckedSeq != 2 {
		t.Fatalf("state = %+v", snap.State)
	}
	if snap.EventTotal != 4 || snap.LastSeq != 4 {
		t.Fatalf("totals = %d/%d, want 4/4", snap.EventTotal, snap.LastSeq)
	}
	if len(snap.Pending) != 1 || snap.Pending[0] != 3 {
		t.Fatalf("pending = %v, want [3]", snap.Pending)
	}
	if snap.Report != nil {
		t.Fatalf("report = %+v, want nil before a terminal state", snap.Report)
	}
	assertTreeSame(t, dir, before)
	_, err = s.Snapshot("missing")
	if code := exitCode(t, err); code != ExitNotFound {
		t.Fatalf("unknown id: code %d err %v", code, err)
	}
	if _, err := s.Ack("job-1", 3); err != nil {
		t.Fatal(err)
	}
	toDone(t, s)
	snap, err = s.Snapshot("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Report == nil || snap.Report.Status != StatusDone {
		t.Fatalf("report = %+v, want status done", snap.Report)
	}
	if snap.Pending == nil || len(snap.Pending) != 0 {
		t.Fatalf("pending = %v, want empty not nil", snap.Pending)
	}
}

func TestAckCloseReportRefresh(t *testing.T) {
	s, root := startedJob(t)
	toDone(t, s)
	// The stored report carries worktree facts the refresh must keep.
	if _, err := s.WriteReport("job-1", ReportFacts{Clean: true, Head: "abc", HeadRemoto: "abc", Resumo: "what changed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "share", Escopo: "global", Motivo: "wider"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ack("job-1", 2); err != nil {
		t.Fatal(err)
	}
	st, err := s.Close("job-1", false)
	if err != nil || st.Status != StatusClosed {
		t.Fatalf("close: %+v err=%v", st, err)
	}
	dir := filepath.Join(root, "jobs", "job-1")
	page := mustPage(t, s, 0, 0)
	rep, err := readReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusDone {
		t.Fatalf("report status = %q, want the terminal outcome done", rep.Status)
	}
	if rep.Eventos.Total != len(page.Events) || rep.Eventos.UltimoSeq != page.UltimoSeq {
		t.Fatalf("eventos = %+v, want total=%d ultimo=%d", rep.Eventos, len(page.Events), page.UltimoSeq)
	}
	if len(rep.Memoria.Global) != 1 || rep.Memoria.Global[0].Seq != 2 {
		t.Fatalf("global = %+v, want the decision listed with its seq", rep.Memoria.Global)
	}
	if rep.Memoria.DecisionsTotal != 1 || rep.Memoria.DecisionsAckedSeq != 2 {
		t.Fatalf("memoria = %+v", rep.Memoria)
	}
	if rep.Head != "abc" || rep.HeadRemoto != "abc" || rep.Resumo != "what changed" || !rep.Sincronizado || !rep.Limpeza.Removivel {
		t.Fatalf("stored facts lost: head=%q resumo=%q sync=%v removivel=%v", rep.Head, rep.Resumo, rep.Sincronizado, rep.Limpeza.Removivel)
	}
	if rep.Limpeza.Worktree != "kept" || rep.PR != nil {
		t.Fatalf("limpeza=%+v pr=%+v", rep.Limpeza, rep.PR)
	}
}

func TestAckCloseReportKeepsOutcome(t *testing.T) {
	for _, terminal := range []string{StatusDone, StatusFailed, StatusTimeout, StatusCanceled} {
		terminal := terminal
		t.Run(terminal, func(t *testing.T) {
			s, root := startedJob(t)
			for _, status := range []string{StatusPreparing, StatusRunning, StatusFinishing} {
				if _, err := s.Transition("job-1", status); err != nil {
					t.Fatalf("to %s: %v", status, err)
				}
			}
			dir := filepath.Join(root, "jobs", "job-1")
			// Crash the report rename after the terminal state is saved, then
			// close the job while the report is still missing.
			previous := renameAtomic
			renameAtomic = func(from, to string) error {
				if strings.HasSuffix(to, "report.json") {
					return errors.New("crash")
				}
				return previous(from, to)
			}
			if _, err := s.Transition("job-1", terminal); err == nil {
				t.Fatal("expected the report rename crash")
			}
			renameAtomic = previous
			st, err := s.Close("job-1", false)
			if err != nil || st.Status != StatusClosed {
				t.Fatalf("close: %+v err=%v", st, err)
			}
			rep, err := readReport(dir)
			if err != nil {
				t.Fatalf("report after close: %v", err)
			}
			if rep.Status != terminal {
				t.Fatalf("report status = %q, want the outcome %s", rep.Status, terminal)
			}
			// The second close keeps the outcome on the report it already holds.
			st, err = s.Close("job-1", false)
			if err != nil || st.Status != StatusClosed {
				t.Fatalf("second close: %+v err=%v", st, err)
			}
			rep, err = readReport(dir)
			if err != nil {
				t.Fatalf("report after the second close: %v", err)
			}
			if rep.Status != terminal {
				t.Fatalf("second close report status = %q, want the outcome %s", rep.Status, terminal)
			}
			if n := countTipo(t, s, "cleanup"); n != 1 {
				t.Fatalf("cleanup events = %d, want 1", n)
			}
		})
	}
}

func TestAckCloseRetryRefreshesReport(t *testing.T) {
	crashReportRename := func() (restore func()) {
		previous := renameAtomic
		renameAtomic = func(from, to string) error {
			if strings.HasSuffix(to, "report.json") {
				return errors.New("crash")
			}
			return previous(from, to)
		}
		t.Cleanup(func() { renameAtomic = previous })
		return func() { renameAtomic = previous }
	}

	t.Run("stale report after the rename crash", func(t *testing.T) {
		s, root := startedJob(t)
		toDone(t, s)
		if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "one", Escopo: "global", Motivo: "a"}); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, "jobs", "job-1")
		// The state and the log converge before the report rename: the first
		// ack leaves acked=2 and one decision_acked event, the report stale.
		restore := crashReportRename()
		if _, err := s.Ack("job-1", 2); err == nil {
			t.Fatal("expected the report rename crash")
		}
		st, err := readState(dir)
		if err != nil || st.DecisionsAckedSeq != 2 {
			t.Fatalf("state after crash: %+v err=%v, want acked 2", st, err)
		}
		stale, err := readReport(dir)
		if err != nil {
			t.Fatalf("stale report after crash: %v", err)
		}
		if stale.Memoria.DecisionsAckedSeq != 0 || stale.Eventos.Total != 2 {
			t.Fatalf("stale report = acked %d total %d, want 0/2", stale.Memoria.DecisionsAckedSeq, stale.Eventos.Total)
		}
		restore()
		st, err = s.Ack("job-1", 2)
		if err != nil || st.DecisionsAckedSeq != 2 {
			t.Fatalf("retry ack: %+v err=%v", st, err)
		}
		page := mustPage(t, s, 0, 0)
		if len(page.Events) != 3 {
			t.Fatalf("events = %+v, want 3", page.Events)
		}
		if n := countTipo(t, s, "decision_acked"); n != 1 {
			t.Fatalf("decision_acked events = %d, want 1", n)
		}
		rep, err := readReport(dir)
		if err != nil {
			t.Fatalf("report after the retry: %v", err)
		}
		if rep.Status != StatusDone {
			t.Fatalf("report status = %q, want done", rep.Status)
		}
		if rep.Memoria.DecisionsAckedSeq != 2 {
			t.Fatalf("report acked = %d, want 2", rep.Memoria.DecisionsAckedSeq)
		}
		if rep.Eventos.Total != 3 || rep.Eventos.UltimoSeq != 3 {
			t.Fatalf("eventos = %+v, want total=3 ultimo=3", rep.Eventos)
		}
	})

	t.Run("missing report after the terminal publish crash", func(t *testing.T) {
		s, root := startedJob(t)
		for _, status := range []string{StatusPreparing, StatusRunning, StatusFinishing} {
			if _, err := s.Transition("job-1", status); err != nil {
				t.Fatalf("to %s: %v", status, err)
			}
		}
		dir := filepath.Join(root, "jobs", "job-1")
		// The terminal publish and the ack refresh both crash: the state is
		// done and the log holds the ack, but no report.json was ever saved.
		restore := crashReportRename()
		if _, err := s.Transition("job-1", StatusDone); err == nil {
			t.Fatal("expected the report rename crash")
		}
		if _, err := s.Ack("job-1", 1); err == nil {
			t.Fatal("expected the report rename crash")
		}
		restore()
		st, err := readState(dir)
		if err != nil || st.Status != StatusDone || st.DecisionsAckedSeq != 1 {
			t.Fatalf("state after crash: %+v err=%v, want done acked 1", st, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "report.json")); !os.IsNotExist(err) {
			t.Fatalf("report after crash: %v", err)
		}
		st, err = s.Ack("job-1", 1)
		if err != nil || st.DecisionsAckedSeq != 1 {
			t.Fatalf("retry ack: %+v err=%v", st, err)
		}
		page := mustPage(t, s, 0, 0)
		if len(page.Events) != 2 {
			t.Fatalf("events = %+v, want 2", page.Events)
		}
		rep, err := readReport(dir)
		if err != nil {
			t.Fatalf("report after the retry: %v", err)
		}
		if rep.Status != StatusDone {
			t.Fatalf("report status = %q, want done", rep.Status)
		}
		if rep.Memoria.DecisionsAckedSeq != 1 {
			t.Fatalf("report acked = %d, want 1", rep.Memoria.DecisionsAckedSeq)
		}
		if rep.Eventos.Total != 2 || rep.Eventos.UltimoSeq != 2 {
			t.Fatalf("eventos = %+v, want total=2 ultimo=2", rep.Eventos)
		}
	})
}

func noteDecision(t *testing.T, s *Store, escopo string) {
	t.Helper()
	if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: escopo, Escopo: escopo, Motivo: "because"}); err != nil {
		t.Fatal(err)
	}
}

func toDone(t *testing.T, s *Store) {
	t.Helper()
	for _, status := range []string{StatusPreparing, StatusRunning, StatusFinishing, StatusDone} {
		if _, err := s.Transition("job-1", status); err != nil {
			t.Fatalf("to %s: %v", status, err)
		}
	}
}

func countTipo(t *testing.T, s *Store, tipo string) int {
	t.Helper()
	page := mustPage(t, s, 0, 0)
	n := 0
	for _, event := range page.Events {
		if event.Tipo == tipo {
			n++
		}
	}
	return n
}
