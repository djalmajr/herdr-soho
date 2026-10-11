package job

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// The supervisor confirmations in these tests are implementation-generated
// (fakeSupervisorConfirms or a direct ConfirmLaunch call), never evidence
// of a real Herdr launch.

// readLaunchFile reads job-1's launch record.
func readLaunchFile(t *testing.T, stateRoot string) launchRecord {
	t.Helper()
	rec, ok, err := readLaunch(filepath.Join(stateRoot, "jobs", "job-1"))
	if err != nil || !ok {
		t.Fatalf("launch record: ok=%v err=%v", ok, err)
	}
	return rec
}

func readReportWorkspace(t *testing.T, stateRoot string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(stateRoot, "jobs", "job-1", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	return rep.Limpeza.Workspace
}

func hasFriction(friction []string, part string) bool {
	for _, line := range friction {
		if strings.Contains(line, part) {
			return true
		}
	}
	return false
}

// TestStartConfirmedLaunchBothModes: in worktree and workspace mode the
// start publishes running only with the supervisor's confirmation of this
// launch, and the confirmed record names this job, store, workspace and
// root pane.
func TestStartConfirmedLaunchBothModes(t *testing.T) {
	for _, mode := range []string{"worktree", "workspace"} {
		t.Run(mode, func(t *testing.T) {
			f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
			stateRoot := t.TempDir()
			fake := startFake()
			var friction []string
			s := startStarter(t, f, fake, stateRoot, &friction)
			req := startRequest("main", briefA)
			req.Mode = mode
			st, _, err := s.Start(req)
			if err != nil || st.Status != StatusRunning {
				t.Fatalf("Start: %+v err=%v", st, err)
			}
			rec := readLaunchFile(t, stateRoot)
			if rec.State != launchConfirmed || rec.ID != "job-1" || rec.Store != filepath.Clean(stateRoot) ||
				rec.Workspace != "w9" || rec.Pane != "w9:p1" || rec.PID != os.Getpid() || len(rec.Nonce) != 32 {
				t.Fatalf("launch record = %+v", rec)
			}
			wantEnv := map[string]string{"XDG_CONFIG_HOME": filepath.Clean(f.env.Get("XDG_CONFIG_HOME"))}
			if !reflect.DeepEqual(fake.CreateCalls[0].Env, wantEnv) {
				t.Fatalf("create env = %v, want %v", fake.CreateCalls[0].Env, wantEnv)
			}
		})
	}
}

// TestStartForwardsHomeOnlyForAHomeRelativeReposRoot: the home variable
// travels only when job_repos_root was expanded from the home.
func TestStartForwardsHomeOnlyForAHomeRelativeReposRoot(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	stateRoot := t.TempDir()
	fake := startFake()
	var friction []string
	s := startStarter(t, f, fake, stateRoot, &friction)
	s.Machine.reposRootHome = true
	if _, _, err := s.Start(startRequest("main", briefA)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	want := map[string]string{
		"XDG_CONFIG_HOME": filepath.Clean(f.env.Get("XDG_CONFIG_HOME")),
		"HOME":            filepath.Clean(f.env.Get("HOME")),
	}
	if !reflect.DeepEqual(fake.CreateCalls[0].Env, want) {
		t.Fatalf("create env = %v, want %v", fake.CreateCalls[0].Env, want)
	}
}

// TestStartRefusesAnUnusableLocator: a selector with a control byte fails
// the start with exit 2 before any workspace is created.
func TestStartRefusesAnUnusableLocator(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	stateRoot := t.TempDir()
	fake := startFake()
	var friction []string
	s := startStarter(t, f, fake, stateRoot, &friction)
	env := platform.Env{}
	for k, v := range f.env {
		env[k] = v
	}
	env["XDG_CONFIG_HOME"] = f.env.Get("XDG_CONFIG_HOME") + "\n"
	s.Env = env
	_, store, err := s.Start(startRequest("main", briefA))
	if code := exitCode(t, err); code != ExitUsage {
		t.Fatalf("exit = %d (%v)", code, err)
	}
	if len(fake.CreateCalls) != 0 || len(fake.RunCalls) != 0 {
		t.Fatalf("Herdr called: creates=%d runs=%d", len(fake.CreateCalls), len(fake.RunCalls))
	}
	snap, serr := store.Snapshot("job-1")
	if serr != nil || snap.State.Status != StatusFailed {
		t.Fatalf("state = %+v err=%v", snap.State, serr)
	}
}

// TestStartWithoutConfirmationFailsAndRetains: no supervisor confirms (the
// pane run is acknowledged, nothing confirms within the 30 s production
// wait): the launch is revoked, the job fails with exit 4, and the
// workspace is left open — nothing in the root pane is proven inert.
func TestStartWithoutConfirmationFailsAndRetains(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	stateRoot := t.TempDir()
	fake := startFake()
	fake.OnRun = func(string, []string) {}
	var friction []string
	s := startStarter(t, f, fake, stateRoot, &friction)
	st, store, err := s.Start(startRequest("main", briefA))
	if code := exitCode(t, err); code != ExitHerdr || st.Status != "" {
		t.Fatalf("Start: %+v exit=%d err=%v", st, code, err)
	}
	if len(fake.CloseIDs) != 0 {
		t.Fatalf("CloseIDs = %v, want none", fake.CloseIDs)
	}
	snap, err := store.Snapshot("job-1")
	if err != nil || snap.State.Status != StatusFailed || snap.State.Motivo == nil ||
		*snap.State.Motivo != "job: the supervisor did not confirm its start" {
		t.Fatalf("state = %+v err=%v", snap.State, err)
	}
	if rec := readLaunchFile(t, stateRoot); rec.State != launchRevoked {
		t.Fatalf("launch state = %q, want revoked", rec.State)
	}
	if got := readReportWorkspace(t, stateRoot); got != "open" {
		t.Fatalf("limpeza.workspace = %q, want open", got)
	}
	if !hasFriction(friction, "left open") {
		t.Fatalf("friction = %v", friction)
	}
	// A late supervisor cannot confirm the revoked launch.
	confirmed, _ := ConfirmLaunch(Open(stateRoot), "job-1", platform.Env{"HERDR_WORKSPACE_ID": "w9", "HERDR_PANE_ID": "w9:p1"})
	if confirmed {
		t.Fatal("a late supervisor confirmed a revoked launch")
	}
}

// TestStartWrongPaneConfirmationIsNotAccepted: a supervisor in another pane
// cannot confirm; the start fails closed like an absent confirmation.
func TestStartWrongPaneConfirmationIsNotAccepted(t *testing.T) {
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	stateRoot := t.TempDir()
	fake := startFake()
	fake.OnRun = func(_ string, argv []string) {
		_, _ = ConfirmLaunch(Open(stateRoot), argv[len(argv)-1], platform.Env{"HERDR_WORKSPACE_ID": "w9", "HERDR_PANE_ID": "w9:p2"})
	}
	var friction []string
	s := startStarter(t, f, fake, stateRoot, &friction)
	_, _, err := s.Start(startRequest("main", briefA))
	if code := exitCode(t, err); code != ExitHerdr {
		t.Fatalf("exit = %d (%v)", code, err)
	}
	if rec := readLaunchFile(t, stateRoot); rec.State != launchRevoked || rec.PID != 0 {
		t.Fatalf("launch record = %+v", rec)
	}
}

// TestStartRunFailureFencesTheLaunch: a failed pane run revokes the launch
// before the existing close; a supervisor that confirmed anyway keeps its
// workspace open.
func TestStartRunFailureFencesTheLaunch(t *testing.T) {
	t.Run("fenced: closed as before", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		stateRoot := t.TempDir()
		fake := startFake()
		fake.RunErr = herdrExit("job: herdr pane run failed")
		var friction []string
		s := startStarter(t, f, fake, stateRoot, &friction)
		if _, _, err := s.Start(startRequest("main", briefA)); err == nil {
			t.Fatal("Start succeeded")
		}
		if rec := readLaunchFile(t, stateRoot); rec.State != launchRevoked {
			t.Fatalf("launch state = %q", rec.State)
		}
		if !reflect.DeepEqual(fake.CloseIDs, []string{"w9"}) {
			t.Fatalf("CloseIDs = %v", fake.CloseIDs)
		}
	})
	t.Run("confirmed anyway: left open", func(t *testing.T) {
		f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
		stateRoot := t.TempDir()
		fake := startFake()
		fake.RunErr = herdrExit("job: herdr pane run failed")
		fake.OnRun = func(paneID string, argv []string) {
			_, _ = ConfirmLaunch(Open(stateRoot), argv[len(argv)-1], platform.Env{"HERDR_WORKSPACE_ID": "w9", "HERDR_PANE_ID": paneID})
		}
		var friction []string
		s := startStarter(t, f, fake, stateRoot, &friction)
		if _, _, err := s.Start(startRequest("main", briefA)); err == nil {
			t.Fatal("Start succeeded")
		}
		if len(fake.CloseIDs) != 0 {
			t.Fatalf("CloseIDs = %v, want none", fake.CloseIDs)
		}
		if got := readReportWorkspace(t, stateRoot); got != "open" {
			t.Fatalf("limpeza.workspace = %q", got)
		}
	})
}

// recoverWithLaunch leaves a crashed start that recorded its workspace and
// issued a launch in the given state, then recovers it.
func recoverWithLaunch(t *testing.T, state string) (*fakeHerdr, string, State, []string) {
	t.Helper()
	root := t.TempDir()
	store := crashedStartState(t, root)
	if _, err := store.Record("job-1", func(st *State) { st.WorkspaceID = "w9"; st.RootPane = "w9:p1" }); err != nil {
		t.Fatal(err)
	}
	if state != "" {
		rec, err := issueLaunch(store, store.clock(), "job-1", "w9", "w9:p1")
		if err != nil {
			t.Fatal(err)
		}
		if state != launchPending {
			rec.State = state
			if err := writeLaunch(filepath.Join(root, "jobs", "job-1"), rec); err != nil {
				t.Fatal(err)
			}
		}
	}
	fake := &fakeHerdr{Workspaces: []herdrWorkspace{{ID: "w9", Label: "job-job-1", PaneCount: 1}}}
	var friction []string
	st, recovered, err := RecoverStart(store, "job-1", fake, func(m string) { friction = append(friction, m) }, nil)
	if err != nil || !recovered {
		t.Fatalf("RecoverStart: recovered=%v err=%v", recovered, err)
	}
	return fake, root, st, friction
}

// TestRecoverStartFencesTheLaunch: a crashed start without a launch record
// never ran the supervisor command and is recovered as before (closed);
// with a pending record the launch is revoked first and, like a confirmed
// one, the workspace is left open — a supervisor may be in its root pane.
func TestRecoverStartFencesTheLaunch(t *testing.T) {
	t.Run("no record: closed", func(t *testing.T) {
		fake, root, st, _ := recoverWithLaunch(t, "")
		if st.Status != StatusFailed || !reflect.DeepEqual(fake.CloseIDs, []string{"w9"}) {
			t.Fatalf("state=%+v CloseIDs=%v", st, fake.CloseIDs)
		}
		if got := readReportWorkspace(t, root); got != "closed" {
			t.Fatalf("limpeza.workspace = %q", got)
		}
	})
	for _, state := range []string{launchPending, launchConfirmed, launchRevoked} {
		t.Run(state+": left open", func(t *testing.T) {
			fake, root, st, friction := recoverWithLaunch(t, state)
			if st.Status != StatusFailed || len(fake.CloseIDs) != 0 || fake.ListReads != 0 {
				t.Fatalf("state=%+v CloseIDs=%v ListReads=%d", st, fake.CloseIDs, fake.ListReads)
			}
			if got := readReportWorkspace(t, root); got != "open" {
				t.Fatalf("limpeza.workspace = %q", got)
			}
			if !hasFriction(friction, "left open") {
				t.Fatalf("friction = %v", friction)
			}
			rec := readLaunchFile(t, root)
			want := state
			if state == launchPending {
				want = launchRevoked
			}
			if rec.State != want {
				t.Fatalf("launch state = %q, want %q", rec.State, want)
			}
		})
	}
}

// TestConfirmLaunchStaleWithoutStarter: a pending record whose starter is
// gone (start.lock free) is not confirmed, and the record is unchanged.
func TestConfirmLaunchStaleWithoutStarter(t *testing.T) {
	root := t.TempDir()
	store := crashedStartState(t, root)
	if _, err := issueLaunch(store, store.clock(), "job-1", "w9", "w9:p1"); err != nil {
		t.Fatal(err)
	}
	confirmed, reason := ConfirmLaunch(store, "job-1", platform.Env{"HERDR_WORKSPACE_ID": "w9", "HERDR_PANE_ID": "w9:p1"})
	if confirmed || !strings.Contains(reason, "stale") {
		t.Fatalf("confirmed=%v reason=%q", confirmed, reason)
	}
	if rec := readLaunchFile(t, root); rec.State != launchPending {
		t.Fatalf("launch state = %q", rec.State)
	}
}
