package job

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// TestStartHostileWorkspaceID pins the workspace id as one safe path
// segment: a create result whose id is not one segment fails the start with
// the fixed exit 4 message, records no workspace id, writes no session.conf
// anywhere, calls no Close and no Run, fails the job, and sends one fixed
// line — carrying no id — to the normal friction sink.
func TestStartHostileWorkspaceID(t *testing.T) {
	hostile := []string{
		"../escape", "..", ".", "a/b", `a\b`, "/abs", `\rooted`,
		`C:\x`, "C:x", `\\server\share`, "-x", "--force",
		"a\x00b", "w\n2", " w2", "w2.",
	}
	valid := []string{"w2A", "w12", "ws-1"}
	// NUL is reserved only where the OS treats it as such;
	// filepath.IsLocal is the platform rule, so the hostile list grows
	// on Windows and the valid list grows everywhere else.
	if runtime.GOOS == "windows" {
		hostile = append(hostile, "NUL")
	} else {
		valid = append(valid, "NUL")
	}

	for _, id := range hostile {
		t.Run("hostile "+strconv.Quote(id), func(t *testing.T) {
			f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
			// A private parent: an escape lands inside the walk below,
			// and an absolute id lands outside the test root entirely.
			parent := t.TempDir()
			stateRoot := filepath.Join(parent, "state")
			if err := os.MkdirAll(stateRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			fake := startFake()
			fake.CreateWorkspaceID = id
			fake.CreateRootPaneID = "w9:p1"
			var friction []string
			s := startStarter(t, f, fake, stateRoot, &friction)

			const fixed = "job: herdr workspace create returned an invalid workspace id"
			_, store, err := s.Start(startRequest("main", briefA))
			wantExit(t, err, ExitHerdr, fixed)
			if store == nil {
				t.Fatal("no store returned")
			}
			snap, err := store.Snapshot("job-1")
			if err != nil {
				t.Fatal(err)
			}
			if snap.State.Status != StatusFailed || snap.State.Motivo == nil || *snap.State.Motivo != fixed {
				t.Fatalf("state = %q motivo %v", snap.State.Status, snap.State.Motivo)
			}
			if snap.State.WorkspaceID != "" || snap.State.RootPane != "" {
				t.Fatalf("workspace facts were recorded: %+v", snap.State)
			}
			// No session.conf anywhere under the private parent: an
			// escape would land right there, a valid write under the
			// state root.
			var sessions []string
			walkErr := filepath.WalkDir(parent, func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && d.Name() == "session.conf" {
					sessions = append(sessions, path)
				}
				return nil
			})
			if walkErr != nil {
				t.Fatal(walkErr)
			}
			if len(sessions) != 0 {
				t.Fatalf("session.conf created: %v", sessions)
			}
			if len(fake.CloseIDs) != 0 || len(fake.RunCalls) != 0 {
				t.Fatalf("CloseIDs = %v RunCalls = %+v", fake.CloseIDs, fake.RunCalls)
			}
			if len(friction) != 1 || friction[0] != "job: the Herdr workspace was left open: its id is invalid" ||
				strings.Contains(friction[0], id) {
				t.Fatalf("friction = %v", friction)
			}
			// The workspace is left open on purpose: the failure and the
			// terminal record come first, then the cleanup event, and the
			// report says the workspace stayed open.
			events, err := readEvents(filepath.Join(stateRoot, "jobs", "job-1"))
			if err != nil {
				t.Fatal(err)
			}
			if len(events) < 3 {
				t.Fatalf("events = %+v, want the failure, the terminal and the cleanup", events)
			}
			failure, terminal, cleanup := events[len(events)-3], events[len(events)-2], events[len(events)-1]
			if failure.Tipo != "failure" || failure.Refs["motivo"] != fixed || terminal.Tipo != "terminal" || terminal.Refs["exit"] != "4" || cleanup.Tipo != "cleanup" {
				t.Fatalf("failure = %+v terminal = %+v cleanup = %+v", failure, terminal, cleanup)
			}
			assertFailedReport(t, stateRoot, "job-1", fixed)
			raw, err := os.ReadFile(filepath.Join(stateRoot, "jobs", "job-1", "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var rep Report
			if err := json.Unmarshal(raw, &rep); err != nil {
				t.Fatal(err)
			}
			if rep.Limpeza.Workspace != "open" {
				t.Fatalf("limpeza.workspace = %q, want open", rep.Limpeza.Workspace)
			}
		})
	}

	for _, id := range valid {
		t.Run("valid "+strconv.Quote(id), func(t *testing.T) {
			f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
			stateRoot := t.TempDir()
			fake := startFake()
			fake.CreateWorkspaceID = id
			fake.CreateRootPaneID = "w9:p1"
			var friction []string
			s := startStarter(t, f, fake, stateRoot, &friction)

			st, store, err := s.Start(startRequest("main", briefA))
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			if st.Status != StatusRunning {
				t.Fatalf("status = %q", st.Status)
			}
			snap, err := store.Snapshot("job-1")
			if err != nil {
				t.Fatal(err)
			}
			if snap.State.WorkspaceID != id || snap.State.RootPane != "w9:p1" {
				t.Fatalf("run facts = %+v", snap.State)
			}
			session := filepath.Join(stateRoot, id, "session.conf")
			raw, err := os.ReadFile(session)
			if err != nil {
				t.Fatalf("session.conf: %v", err)
			}
			if string(raw) != "panes=3\nlane.build.roles=implementer\n" {
				t.Fatalf("session.conf = %q", raw)
			}
			if len(friction) != 0 {
				t.Fatalf("friction = %v", friction)
			}
		})
	}
}

// TestStartHostileWorkspaceIDRawSink pins the raw id as raw-friction-only:
// for a hostile workspace id the raw sink gets the sanitized id exactly
// once, and the id reaches neither the normal friction line, the error,
// nor the job state or report.
func TestStartHostileWorkspaceIDRawSink(t *testing.T) {
	for _, id := range []string{"../escape", `a\b`, "C:x", "a\x00b", " w2"} {
		t.Run(strconv.Quote(id), func(t *testing.T) {
			f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
			stateRoot := t.TempDir()
			fake := startFake()
			fake.CreateWorkspaceID = id
			fake.CreateRootPaneID = "w9:p1"
			var friction, raw []string
			s := startStarter(t, f, fake, stateRoot, &friction)
			s.RawFriction = func(message string) { raw = append(raw, message) }

			const fixed = "job: herdr workspace create returned an invalid workspace id"
			_, store, err := s.Start(startRequest("main", briefA))
			wantExit(t, err, ExitHerdr, fixed)
			if len(raw) != 1 || !strings.Contains(raw[0], core.FrictionSafe(id)) {
				t.Fatalf("raw sink = %v, want the sanitized id exactly once", raw)
			}
			for _, line := range friction {
				if strings.Contains(line, id) {
					t.Fatalf("normal friction carries the raw id: %v", friction)
				}
			}
			if strings.Contains(err.Error(), id) {
				t.Fatalf("error carries the raw id: %v", err)
			}
			snap, err := store.Snapshot("job-1")
			if err != nil {
				t.Fatal(err)
			}
			if snap.State.WorkspaceID != "" || (snap.State.Motivo != nil && strings.Contains(*snap.State.Motivo, id)) {
				t.Fatalf("state carries the raw id: %+v", snap.State)
			}
		})
	}
}
