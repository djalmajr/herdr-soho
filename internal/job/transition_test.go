package job

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTransitions(t *testing.T) {
	allowed := map[string]map[string]bool{
		StatusAccepted:  {StatusPreparing: true},
		StatusPreparing: {StatusRunning: true},
		StatusRunning:   {StatusBlocked: true, StatusFinishing: true},
		StatusBlocked:   {StatusRunning: true, StatusFinishing: true},
		StatusFinishing: {
			StatusDone: true, StatusFailed: true, StatusBlocked: true,
			StatusTimeout: true, StatusCanceled: true,
		},
		// A terminal self-edge is the idempotent retry of a transition
		// whose report publish crashed: it republishes the report and
		// leaves the state unchanged.
		StatusDone:      {StatusCollected: true, StatusDone: true},
		StatusFailed:    {StatusCollected: true, StatusFailed: true},
		StatusTimeout:   {StatusCollected: true, StatusTimeout: true},
		StatusCanceled:  {StatusCollected: true, StatusCanceled: true},
		StatusCollected: {StatusClosed: true},
	}
	statuses := []string{
		StatusAccepted, StatusPreparing, StatusRunning, StatusBlocked, StatusFinishing,
		StatusDone, StatusFailed, StatusTimeout, StatusCanceled, StatusCollected, StatusClosed,
	}

	t.Run("allowed edges and no others", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		if _, err := s.Start("job-1", []byte(briefA)); err != nil {
			t.Fatal(err)
		}
		for _, from := range statuses {
			for _, to := range statuses {
				if err := writeStatus(root, "job-1", from); err != nil {
					t.Fatal(err)
				}
				_, err := s.Transition("job-1", to)
				want := allowed[from][to]
				if want && err != nil {
					t.Fatalf("%s -> %s: %v", from, to, err)
				}
				if !want && err == nil {
					t.Fatalf("%s -> %s was accepted", from, to)
				}
				if !want {
					if code := exitCode(t, err); code != ExitUsage {
						t.Fatalf("%s -> %s code %d", from, to, code)
					}
					st, loadErr := readState(filepath.Join(root, "jobs", "job-1"))
					if loadErr != nil || st.Status != from {
						t.Fatalf("refused edge changed %s -> %s (%v)", from, st.Status, loadErr)
					}
				}
			}
		}
	})

	t.Run("happy path reaches closed", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		if _, err := s.Start("job-1", []byte(briefA)); err != nil {
			t.Fatal(err)
		}
		path := []string{StatusPreparing, StatusRunning, StatusBlocked, StatusRunning, StatusFinishing, StatusDone, StatusCollected, StatusClosed}
		for _, status := range path {
			st, err := s.Transition("job-1", status)
			if err != nil || st.Status != status {
				t.Fatalf("to %s: %+v err=%v", status, st, err)
			}
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(filepath.Join(root, "jobs", "job-1", "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("mode %o", info.Mode().Perm())
			}
		}
	})

	t.Run("crash before rename leaves the previous state", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		if _, err := s.Start("job-1", []byte(briefA)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Transition("job-1", StatusPreparing); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, "jobs", "job-1")
		path := filepath.Join(dir, "state.json")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		previous := renameAtomic
		renameAtomic = func(from, to string) error {
			if filepath.Dir(from) != filepath.Dir(to) {
				t.Errorf("temp %s is not beside %s", from, to)
			}
			if !strings.HasSuffix(to, "state.json") {
				return previous(from, to)
			}
			return errors.New("crash")
		}
		t.Cleanup(func() { renameAtomic = previous })
		_, err = s.Transition("job-1", StatusRunning)
		if err == nil {
			t.Fatal("expected rename crash")
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("state changed across the crash\nbefore %s\nafter %s", before, after)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.Contains(entry.Name(), ".tmp") {
				t.Fatalf("temp file left behind: %s", entry.Name())
			}
		}
		renameAtomic = previous
		st, err := s.Transition("job-1", StatusRunning)
		if err != nil || st.Status != StatusRunning {
			t.Fatalf("retry after crash: %+v err=%v", st, err)
		}
	})

	t.Run("unknown job", func(t *testing.T) {
		s := Open(t.TempDir())
		_, err := s.Transition("missing", StatusPreparing)
		if code := exitCode(t, err); code != ExitNotFound {
			t.Fatalf("code %d err %v", code, err)
		}
	})
}

func TestTransitionsClosedPendingDecision(t *testing.T) {
	s, root := startedJob(t)
	noteDecision(t, s, "global")
	toDone(t, s)
	if _, err := s.Transition("job-1", StatusCollected); err != nil {
		t.Fatal(err)
	}
	_, err := s.Transition("job-1", StatusClosed)
	exit, ok := err.(*ExitError)
	if !ok || exit.Code != ExitUnacked || len(exit.Seqs) != 1 || exit.Seqs[0] != 2 {
		t.Fatalf("transition closed err = %v, want 24 with seq 2", err)
	}
	st, err := readState(filepath.Join(root, "jobs", "job-1"))
	if err != nil || st.Status != StatusCollected {
		t.Fatalf("status after refused close: %+v err=%v, want collected", st, err)
	}
	if n := countTipo(t, s, "cleanup"); n != 0 {
		t.Fatalf("cleanup events = %d, want 0", n)
	}
	if _, err := s.Ack("job-1", 2); err != nil {
		t.Fatal(err)
	}
	st, err = s.Transition("job-1", StatusClosed)
	if err != nil || st.Status != StatusClosed {
		t.Fatalf("transition closed after ack: %+v err=%v", st, err)
	}
	page := mustPage(t, s, 0, 0)
	cleanups := 0
	for _, event := range page.Events {
		if event.Tipo != "cleanup" {
			continue
		}
		cleanups++
		if event.Refs["motivo"] != "" {
			t.Fatalf("cleanup motivo = %q, want none", event.Refs["motivo"])
		}
	}
	if cleanups != 1 {
		t.Fatalf("cleanup events = %d, want 1", cleanups)
	}
}

func TestTransitionsTerminalReport(t *testing.T) {
	for _, terminal := range []string{StatusDone, StatusFailed, StatusTimeout, StatusCanceled} {
		terminal := terminal
		t.Run(terminal, func(t *testing.T) {
			s, root := startedJob(t)
			for _, status := range []string{StatusPreparing, StatusRunning, StatusFinishing} {
				if _, err := s.Transition("job-1", status); err != nil {
					t.Fatalf("to %s: %v", status, err)
				}
			}
			st, err := s.Transition("job-1", terminal)
			if err != nil || st.Status != terminal {
				t.Fatalf("to %s: %+v err=%v", terminal, st, err)
			}
			dir := filepath.Join(root, "jobs", "job-1")
			rep, err := readReport(dir)
			if err != nil {
				t.Fatalf("report.json after %s: %v", terminal, err)
			}
			if rep.Status != terminal {
				t.Fatalf("report status = %q, want %s", rep.Status, terminal)
			}
			if rep.PR != nil {
				t.Fatalf("pr = %+v, want null", rep.PR)
			}
			if rep.Limpeza.Worktree != "kept" {
				t.Fatalf("limpeza = %+v", rep.Limpeza)
			}
			if rep.Motivo == nil || *rep.Motivo != "sem commits" {
				t.Fatalf("motivo = %v, want sem commits", rep.Motivo)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"pr":null`) {
				t.Fatalf("report does not carry pr null: %s", raw)
			}
			if !strings.Contains(string(raw), `"removivel":false`) {
				t.Fatalf("report does not carry removivel: %s", raw)
			}
			if strings.Contains(string(raw), "retencao_ate") {
				t.Fatalf("report contains retencao_ate: %s", raw)
			}
			if rep.Eventos.Total != 1 || rep.Eventos.UltimoSeq != 1 {
				t.Fatalf("eventos = %+v, want 1/1", rep.Eventos)
			}
		})
	}
}

func TestTransitionsTerminalReportRetry(t *testing.T) {
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
			// Crash the report rename after the terminal state is saved.
			previous := renameAtomic
			renameAtomic = func(from, to string) error {
				if strings.HasSuffix(to, "report.json") {
					return errors.New("crash")
				}
				return previous(from, to)
			}
			t.Cleanup(func() { renameAtomic = previous })
			if _, err := s.Transition("job-1", terminal); err == nil {
				t.Fatal("expected the report rename crash")
			}
			stateBefore, err := os.ReadFile(filepath.Join(dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			st, err := readState(dir)
			if err != nil || st.Status != terminal {
				t.Fatalf("state after crash: %+v err=%v, want %s", st, err, terminal)
			}
			if _, err := os.Stat(filepath.Join(dir, "report.json")); !os.IsNotExist(err) {
				t.Fatalf("report after crash: %v", err)
			}
			renameAtomic = previous
			st, err = s.Transition("job-1", terminal)
			if err != nil || st.Status != terminal {
				t.Fatalf("retry: %+v err=%v", st, err)
			}
			stateAfter, err := os.ReadFile(filepath.Join(dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stateBefore, stateAfter) {
				t.Fatalf("retry changed state.json:\nbefore %s\nafter %s", stateBefore, stateAfter)
			}
			rep, err := readReport(dir)
			if err != nil {
				t.Fatalf("report after retry: %v", err)
			}
			if rep.Status != terminal {
				t.Fatalf("report status = %q, want %s", rep.Status, terminal)
			}
			if rep.PR != nil || rep.Limpeza.Worktree != "kept" {
				t.Fatalf("pr=%+v limpeza=%+v", rep.PR, rep.Limpeza)
			}
			if rep.Eventos.Total != 1 || rep.Eventos.UltimoSeq != 1 {
				t.Fatalf("eventos = %+v, want 1/1", rep.Eventos)
			}
		})
	}
}

func TestTransitionsCollectedReportKeepsOutcome(t *testing.T) {
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
			// leave the outcome to collected while the report is still missing.
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
			st, err := s.Transition("job-1", StatusCollected)
			if err != nil || st.Status != StatusCollected {
				t.Fatalf("collected: %+v err=%v", st, err)
			}
			rep, err := readReport(dir)
			if err != nil {
				t.Fatalf("report after collected: %v", err)
			}
			if rep.Status != terminal {
				t.Fatalf("report status = %q, want the outcome %s", rep.Status, terminal)
			}
			if rep.Limpeza.Worktree != "kept" {
				t.Fatalf("limpeza = %+v", rep.Limpeza)
			}
			if rep.Eventos.Total != 1 || rep.Eventos.UltimoSeq != 1 {
				t.Fatalf("eventos = %+v, want 1/1", rep.Eventos)
			}
		})
	}
}

func writeStatus(root, id, status string) error {
	dir := filepath.Join(root, "jobs", id)
	st, err := readState(dir)
	if err != nil {
		return err
	}
	st.Status = status
	return saveState(dir, st)
}
