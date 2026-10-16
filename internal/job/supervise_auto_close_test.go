// supervise_auto_close_test.go exercises the finalize automatic cleanup
// (DJA-231) over the git sync fixture: the worktree removal on a clean
// and pushed tree at every terminal outcome, the opt-out that keeps the
// worktree and the workspace open with only the folder_retained event,
// the removal refusals (a dirty tree, an unpushed head) that keep the
// worktree and append the needs_attention with their motivo, the close
// watch over a scripted pane list (a changing foreign pane waits, a
// quiet pane closes after the human input wait, the cap asks for human
// input), a failed release that removes nothing, the restart after the
// workspace_closed that repeats no event, the restart in the middle of a
// wait that starts the observation again, the workspace mode that
// removes nothing, and the job folder that is never deleted. The removal
// runs the real git over the fixture worktree.

package job

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// noPanesHerdr is a minimal Herdr client without the Panes method: it
// records the workspace close and implements herdrWorkspaces, but not
// the pane list, so the finalize close decision type-asserts for the
// method and fails the assertion — the close runs as today with the
// friction line.
type noPanesHerdr struct {
	CloseIDs []string
}

func (h *noPanesHerdr) Create(cwd, label string, env map[string]string) (string, string, error) {
	return "ws-1", "pane-root", nil
}

func (h *noPanesHerdr) Close(workspaceID string) error {
	h.CloseIDs = append(h.CloseIDs, workspaceID)
	return nil
}

func (h *noPanesHerdr) ServerReachable() bool { return true }

func (h *noPanesHerdr) List() ([]herdrWorkspace, error) { return nil, nil }

// orderingHerdr records, at the moment of the workspace close, the
// folder_retained and workspace_closed events already in the job log:
// both must be there — the supervisor runs in the workspace's root pane
// and ends with the close, so workspace_closed lands right before it.
// crash, when set, panics with it after the close is recorded: the
// supervisor process killed by its own workspace close.
type orderingHerdr struct {
	*fakeHerdr
	jobDir        string
	folderAtClose int
	closedAtClose int
	crash         error
}

func (h *orderingHerdr) Close(workspaceID string) error {
	if h.jobDir != "" {
		if events, err := readEvents(h.jobDir); err == nil {
			for _, event := range events {
				switch event.Tipo {
				case "folder_retained":
					h.folderAtClose++
				case "workspace_closed":
					h.closedAtClose++
				}
			}
		}
	}
	err := h.fakeHerdr.Close(workspaceID)
	if h.crash != nil {
		panic(h.crash)
	}
	return err
}

// closeKill is the sentinel panic of a supervisor killed by its own
// workspace close.
var closeKill = errors.New("the supervisor ended with its workspace")

// midWaitCrash is the sentinel panic of a crash in the middle of the
// close watch wait.
var midWaitCrash = errors.New("crash in the middle of the close watch wait")

// acFixture wires the auto-close facts over the git sync fixture: the
// state carries the checkout, the worktree mode, the root pane record,
// and the one-minute human input wait; the roster carries the job's own
// workers (and a foreign-workspace row whose pane must still count as
// foreign); and the supervisor gets the package's fake Herdr (the default
// Panes list is empty: no foreign pane, the watch closes at the first
// observation) and the one-minute poll.
func acFixture(t *testing.T, f *supGitFixture) *fakeHerdr {
	t.Helper()
	if _, err := f.store.Record(supGitID, func(st *State) {
		st.Checkout = f.job.base.checkout()
		st.Modo = "worktree"
		st.RootPane = "pane-root"
		st.HumanInputMin = 1
	}); err != nil {
		t.Fatal(err)
	}
	f.team.rosterRows = []rosterRow{
		{Name: "orch-1", Role: "job-orchestrator", Pane: "ws-1:pane-root"},
		{Name: "w-1", Role: "worker", Pane: "ws-1:pane-worker"},
		{Name: "w-x", Role: "worker", Pane: "ws-9:pane-x"},
	}
	f.sup.Herdr = f.herdr
	f.sup.Poll = time.Minute
	return f.herdr
}

// assertWorktreeRemoved proves the removal ran and the contract held:
// the worktree dir is gone, the checkout's job branch still exists (the
// removal never deletes it), and the job folder is alive.
func assertWorktreeRemoved(t *testing.T, f *supGitFixture) {
	t.Helper()
	if _, err := os.Stat(f.job.dir); !os.IsNotExist(err) {
		t.Fatalf("the worktree dir %s still exists (err %v)", f.job.dir, err)
	}
	checkout := f.job.base.checkout()
	if out, err := f.job.base.git(checkout, "rev-parse", "--verify", "refs/heads/"+gitSyncBranch); err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("the job branch is gone: %q %v", out, err)
	}
	assertJobFolderAlive(t, f)
}

// assertWorktreeKept proves the worktree dir and the local job branch
// survived the pass.
func assertWorktreeKept(t *testing.T, f *supGitFixture) {
	t.Helper()
	if _, err := os.Stat(f.job.dir); err != nil {
		t.Fatalf("the worktree dir is gone: %v", err)
	}
	checkout := f.job.base.checkout()
	if out, err := f.job.base.git(checkout, "rev-parse", "--verify", "refs/heads/"+gitSyncBranch); err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("the job branch is gone: %q %v", out, err)
	}
}

// assertJobFolderAlive proves the job folder was never deleted: its
// state, events, and report files are there, and the job is readable.
func assertJobFolderAlive(t *testing.T, f *supGitFixture) {
	t.Helper()
	for _, name := range []string{"state.json", "events.jsonl", "report.json"} {
		if _, err := os.Stat(filepath.Join(f.jobDir, name)); err != nil {
			t.Fatalf("the job folder lost %s: %v", name, err)
		}
	}
	if _, err := f.store.Snapshot(supGitID); err != nil {
		t.Fatalf("the job snapshot is unreadable: %v", err)
	}
}

func indexOfType(events []Event, tipo string) int {
	for i, event := range events {
		if event.Tipo == tipo {
			return i
		}
	}
	return -1
}

// frictionLine finds a friction line containing the fragment and fails
// the test when none does.
func frictionLine(t *testing.T, f *supGitFixture, fragment string) {
	t.Helper()
	for _, line := range *f.friction {
		if strings.Contains(line, fragment) {
			return
		}
	}
	t.Fatalf("friction = %v, want a line containing %q", *f.friction, fragment)
}

func TestSuperviseAutoClose(t *testing.T) {
	t.Run("a clean pushed tree is removed at every terminal outcome and the workspace closes", func(t *testing.T) {
		cases := []struct {
			name   string
			status string
			motivo string
			exit   int
			// bytes cross-checks the folder_retained refs.bytes against
			// the size measured at the append moment.
			bytes bool
		}{
			{name: "done", status: StatusDone, motivo: "", exit: 0, bytes: true},
			{name: "failed", status: StatusFailed, motivo: "falhou", exit: ExitFailed},
			{name: "canceled", status: StatusCanceled, motivo: "cancelado", exit: ExitCanceled},
			{name: "timeout as failure", status: StatusTimeout, motivo: "tempo esgotado", exit: ExitTimeout},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				f := newSupGitFixture(t, supGitGhRules)
				herdr := acFixture(t, f)
				f.sup.Herdr = &orderingHerdr{fakeHerdr: herdr, jobDir: f.jobDir}
				var folderBytes int64
				var folderDirAtAppend string
				var measureErr error
				if tc.bytes {
					oldAppend := appendEventFn
					appendEventFn = func(dir string, now time.Time, in EventIn) (Event, error) {
						if in.Tipo == "folder_retained" {
							folderBytes, measureErr = jobFolderBytes(dir)
							folderDirAtAppend = dir
						}
						return oldAppend(dir, now, in)
					}
					t.Cleanup(func() { appendEventFn = oldAppend })
				}
				f.job.commitIn(t, "feat: first")
				if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
					t.Fatal(err)
				}
				exit, done, err := f.sup.finishOutcome(tc.status, tc.motivo, tc.exit)
				if err != nil || !done || exit != tc.exit {
					t.Fatalf("finishOutcome = %d %v %v, want %v", exit, done, err, tc.exit)
				}
				// The worktree is gone, the branch stays, the job folder
				// is alive.
				assertWorktreeRemoved(t, f)
				st := f.state(t)
				if !st.WorktreeRemoved || !st.WorkspaceClosed {
					t.Fatalf("state = %+v, want the removed worktree and the closed workspace", st)
				}
				// The events land once each, in the contract order, with
				// the folder_retained and the workspace_closed both before
				// the close (the ordering Herdr saw the log at the close
				// moment).
				events := f.events(t)
				if n := countTipes(events, "needs_attention"); n != 0 {
					t.Fatalf("needs_attention events = %d, want none (%v)", n, eventTipes(events))
				}
				wantTail := []string{"terminal", "cleanup", "folder_retained", "workspace_closed"}
				if got := eventTipes(events)[len(events)-len(wantTail):]; !reflect.DeepEqual(got, wantTail) {
					t.Fatalf("event tail = %v, want %v", got, wantTail)
				}
				if oh, ok := f.sup.Herdr.(*orderingHerdr); !ok {
					t.Fatal("the supervisor ran without the ordering Herdr: the close-moment proof is missing")
				} else if oh.folderAtClose != 1 || oh.closedAtClose != 1 {
					t.Fatalf("at the close moment folder_retained = %d workspace_closed = %d, want 1 and 1", oh.folderAtClose, oh.closedAtClose)
				}
				folder := events[indexOfType(events, "folder_retained")]
				if folder.Refs["path"] != f.jobDir {
					t.Fatalf("folder_retained refs.path = %q, want the job dir %q", folder.Refs["path"], f.jobDir)
				}
				if bytes, err := strconv.ParseInt(folder.Refs["bytes"], 10, 64); err != nil || bytes <= 0 {
					t.Fatalf("folder_retained refs.bytes = %q, want a positive byte count", folder.Refs["bytes"])
				}
				if tc.bytes {
					if measureErr != nil {
						t.Fatalf("measure the job folder at the append: %v", measureErr)
					}
					if folder.Refs["bytes"] != strconv.FormatInt(folderBytes, 10) {
						t.Fatalf("folder_retained refs.bytes = %q, want %d (the size measured at the append)", folder.Refs["bytes"], folderBytes)
					}
					if folder.Refs["path"] != folderDirAtAppend {
						t.Fatalf("folder_retained refs.path = %q, want the append dir %q", folder.Refs["path"], folderDirAtAppend)
					}
				}
				// The report: the removed worktree and the closed
				// workspace.
				rep := f.report(t)
				limpeza, ok := rep["limpeza"].(map[string]any)
				if !ok || limpeza["worktree"] != "removed" || limpeza["workspace"] != "closed" {
					t.Fatalf("report limpeza = %v, want the removed worktree and the closed workspace", rep["limpeza"])
				}
				// One workspace close, with the recorded id, at the
				// first (and only) observation.
				if len(f.herdr.CloseIDs) != 1 || f.herdr.CloseIDs[0] != "ws-1" {
					t.Fatalf("herdr close = %v, want ws-1 exactly once", f.herdr.CloseIDs)
				}
				if herdr.PanesCalls != 1 {
					t.Fatalf("panes calls = %d, want one observation (no foreign pane)", herdr.PanesCalls)
				}
				if herdr.PanesCallIDs[0] != "ws-1" {
					t.Fatalf("panes workspace id = %q, want the recorded id ws-1", herdr.PanesCallIDs[0])
				}
				if len(*f.friction) != 0 {
					t.Fatalf("friction = %v, want none", *f.friction)
				}
			})
		}
	})

	t.Run("a restart after the workspace_closed repeats no event", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		herdr := acFixture(t, f)
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		assertWorktreeRemoved(t, f)
		before := f.events(t)
		// A fresh process over the terminal job: the close is already
		// recorded and the log already carries every event, so the pass
		// adds nothing.
		sup2 := f.supRestarted(t)
		sup2.Herdr = herdr
		sup2.Poll = f.sup.Poll
		if exit, err := sup2.Run(); err != nil || exit != 0 {
			t.Fatalf("restarted Run = %d %v, want 0", exit, err)
		}
		after := f.events(t)
		if len(after) != len(before) {
			t.Fatalf("the restart added events: %v", eventTipes(after[len(before):]))
		}
		if len(f.herdr.CloseIDs) != 1 {
			t.Fatalf("herdr close = %v, want still exactly one", f.herdr.CloseIDs)
		}
		assertWorktreeRemoved(t, f)
	})

	t.Run("a supervisor that ends with its workspace converges on the next pass", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			listed    bool
			wantClose int
		}{
			{name: "the workspace is gone: recorded closed, no second close", listed: false, wantClose: 1},
			{name: "the workspace is still listed: closed again, no second event", listed: true, wantClose: 2},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newSupGitFixture(t, supGitGhRules)
				herdr := acFixture(t, f)
				f.sup.Herdr = &orderingHerdr{fakeHerdr: herdr, jobDir: f.jobDir, crash: closeKill}
				f.job.commitIn(t, "feat: first")
				if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
					t.Fatal(err)
				}
				func() {
					defer func() {
						r := recover()
						if r == nil {
							t.Fatal("the close did not end the supervisor: the test proves nothing")
							return
						}
						if !errors.Is(r.(error), closeKill) {
							t.Fatalf("crash = %v, want the scripted end with the workspace", r)
						}
					}()
					_, _, _ = f.sup.finishOutcome(StatusDone, "", 0)
				}()
				// The event landed before the close; the record did not.
				if n := countTipes(f.events(t), "workspace_closed"); n != 1 {
					t.Fatalf("workspace_closed events = %d, want 1 (written before the close)", n)
				}
				if st := f.state(t); st.WorkspaceClosed {
					t.Fatalf("state = %+v, want no WorkspaceClosed record (the process ended first)", st)
				}
				if tc.listed {
					herdr.Workspaces = []herdrWorkspace{{ID: "ws-1", Label: "job-" + supGitID, PaneCount: 1}}
				}
				panesBefore := herdr.PanesCalls
				sup2 := f.supRestarted(t)
				sup2.Herdr = herdr
				sup2.Poll = f.sup.Poll
				if exit, err := sup2.Run(); err != nil || exit != 0 {
					t.Fatalf("restarted Run = %d %v, want 0", exit, err)
				}
				events := f.events(t)
				if n := countTipes(events, "workspace_closed"); n != 1 {
					t.Fatalf("workspace_closed events = %d, want still 1", n)
				}
				if n := countTipes(events, "needs_attention"); n != 0 {
					t.Fatalf("needs_attention events = %d, want none (%v)", n, eventTipes(events))
				}
				if len(herdr.CloseIDs) != tc.wantClose {
					t.Fatalf("herdr close = %v, want %d", herdr.CloseIDs, tc.wantClose)
				}
				if herdr.PanesCalls != panesBefore {
					t.Fatalf("panes calls = %d, want %d (the close was decided before: no second watch)", herdr.PanesCalls, panesBefore)
				}
				if st := f.state(t); !st.WorkspaceClosed {
					t.Fatalf("state = %+v, want the WorkspaceClosed record after the converging pass", st)
				}
				assertWorktreeRemoved(t, f)
			})
		}
	})

	t.Run("a failed close with the event written appends close_failed and an unreadable list waits", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		herdr := acFixture(t, f)
		herdr.CloseErr = errors.New("the close failed")
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		events := f.events(t)
		if got := needsAttentionMotivos(events); !reflect.DeepEqual(got, []string{"close_failed"}) {
			t.Fatalf("needs_attention motivos = %v, want [close_failed]", got)
		}
		if i, j := indexOfType(events, "workspace_closed"), indexOfType(events, "needs_attention"); i < 0 || j < i {
			t.Fatalf("events = %v, want workspace_closed before the close_failed needs_attention", eventTipes(events))
		}
		if limpeza := f.report(t)["limpeza"].(map[string]any); limpeza["workspace"] != "open" {
			t.Fatalf("report limpeza = %v, want the open workspace", limpeza)
		}
		if st := f.state(t); st.WorkspaceClosed {
			t.Fatalf("state = %+v, want the workspace left open", st)
		}
		// A later pass that cannot read the workspace list closes
		// nothing and records nothing: a later pass decides.
		herdr.ListErr = errors.New("list down")
		sup2 := f.supRestarted(t)
		sup2.Herdr = herdr
		sup2.Poll = f.sup.Poll
		sup2.finalize(StatusDone)
		if len(herdr.CloseIDs) != 1 {
			t.Fatalf("herdr close = %v, want still one (the list was unreadable)", herdr.CloseIDs)
		}
		if st := f.state(t); st.WorkspaceClosed {
			t.Fatalf("state = %+v, want the workspace left open", st)
		}
		if limpeza := f.report(t)["limpeza"].(map[string]any); limpeza["workspace"] != "open" {
			t.Fatalf("report limpeza after the unreadable list = %v, want the open workspace (the close is not confirmed)", limpeza)
		}
	})

	t.Run("the opt-out removes nothing and closes nothing, and only the folder_retained lands", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		herdr := acFixture(t, f)
		if _, err := f.store.Record(supGitID, func(st *State) { st.AutoCleanup = "off" }); err != nil {
			t.Fatal(err)
		}
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		exit, done, err := f.sup.finishOutcome(StatusDone, "", 0)
		if err != nil || !done || exit != 0 {
			t.Fatalf("finishOutcome = %d %v %v, want done with exit 0", exit, done, err)
		}
		// Nothing is removed and the workspace stays open.
		assertWorktreeKept(t, f)
		if len(f.herdr.CloseIDs) != 0 {
			t.Fatalf("herdr close = %v, want none (the opt-out closes nothing)", f.herdr.CloseIDs)
		}
		if herdr.PanesCalls != 0 {
			t.Fatalf("panes calls = %d, want none (the opt-out runs no watch)", herdr.PanesCalls)
		}
		events := f.events(t)
		if n := countTipes(events, "folder_retained"); n != 1 {
			t.Fatalf("folder_retained events = %d, want 1 (%v)", n, eventTipes(events))
		}
		for _, tipo := range []string{"workspace_closed", "needs_attention"} {
			if n := countTipes(events, tipo); n != 0 {
				t.Fatalf("%s events = %d, want none (the opt-out closes nothing)", tipo, n)
			}
		}
		// The existing cleanup gate is unchanged: it landed, before the
		// folder_retained.
		if n := countTipes(events, "cleanup"); n != 1 {
			t.Fatalf("cleanup events = %d, want 1", n)
		}
		cleanup := indexOfType(events, "cleanup")
		folder := indexOfType(events, "folder_retained")
		if !(cleanup >= 0 && folder > cleanup) {
			t.Fatalf("event order = %v, want cleanup before folder_retained", eventTipes(events))
		}
		st := f.state(t)
		if st.WorktreeRemoved || st.WorkspaceClosed {
			t.Fatalf("state = %+v, want the kept worktree and the open workspace", st)
		}
		rep := f.report(t)
		limpeza, ok := rep["limpeza"].(map[string]any)
		if !ok || limpeza["worktree"] != "kept" || limpeza["workspace"] != "open" {
			t.Fatalf("report limpeza = %v, want the kept worktree and the open workspace", rep["limpeza"])
		}
		if len(*f.friction) != 0 {
			t.Fatalf("friction = %v, want none", *f.friction)
		}
	})

	t.Run("a dirty tree keeps the worktree and appends the needs_attention with its motivo", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		acFixture(t, f)
		f.job.commitIn(t, "feat: first")
		if err := os.WriteFile(filepath.Join(f.job.dir, "leftover.txt"), []byte("wip\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.Transition(supGitID, StatusDone); err != nil {
			t.Fatal(err)
		}
		f.sup.finalize(StatusDone)
		// The worktree stays, with the branch, and the close follows.
		assertWorktreeKept(t, f)
		if len(f.herdr.CloseIDs) != 1 || f.herdr.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want ws-1 (the close follows a kept worktree)", f.herdr.CloseIDs)
		}
		events := f.events(t)
		if n := countTipes(events, "needs_attention"); n != 1 {
			t.Fatalf("needs_attention events = %d, want 1 (%v)", n, eventTipes(events))
		}
		na := events[indexOfType(events, "needs_attention")]
		if na.Refs["motivo"] != "dirty" {
			t.Fatalf("needs_attention refs.motivo = %q, want dirty", na.Refs["motivo"])
		}
		wantTail := []string{"cleanup", "needs_attention", "folder_retained", "workspace_closed"}
		if got := eventTipes(events)[len(events)-len(wantTail):]; !reflect.DeepEqual(got, wantTail) {
			t.Fatalf("event tail = %v, want %v", got, wantTail)
		}
		st := f.state(t)
		if st.WorktreeRemoved || !st.WorkspaceClosed {
			t.Fatalf("state = %+v, want the kept worktree and the closed workspace", st)
		}
		rep := f.report(t)
		limpeza, ok := rep["limpeza"].(map[string]any)
		if !ok || limpeza["worktree"] != "kept" || limpeza["workspace"] != "closed" {
			t.Fatalf("report limpeza = %v, want the kept worktree and the closed workspace", rep["limpeza"])
		}
		// The retry converges: no second needs_attention, no second
		// folder_retained or workspace_closed, the worktree stays.
		f.sup.finalize(StatusDone)
		events = f.events(t)
		for _, tipo := range []string{"needs_attention", "folder_retained", "workspace_closed", "cleanup"} {
			if n := countTipes(events, tipo); n != 1 {
				t.Fatalf("%s events after the retry = %d, want still 1", tipo, n)
			}
		}
		assertWorktreeKept(t, f)
	})

	t.Run("a kept worktree over a failed close appends at most one needs_attention per motivo", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		acFixture(t, f)
		f.job.commitIn(t, "feat: first")
		if err := os.WriteFile(filepath.Join(f.job.dir, "leftover.txt"), []byte("wip\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		f.herdr.CloseErr = errors.New("the close failed")
		// The failed close leaves the workspace listed.
		f.herdr.Workspaces = []herdrWorkspace{{ID: "ws-1", Label: "job-" + supGitID, PaneCount: 1}}
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.Transition(supGitID, StatusDone); err != nil {
			t.Fatal(err)
		}
		// Pass one: the worktree is kept (the dirty tree), and the close
		// fails (the workspace stays open): one needs_attention per
		// motivo, dirty and close_failed.
		f.sup.finalize(StatusDone)
		if got := needsAttentionMotivos(f.events(t)); !reflect.DeepEqual(got, []string{"dirty", "close_failed"}) {
			t.Fatalf("needs_attention motivos = %v, want [dirty close_failed]", got)
		}
		// Pass two, a restarted supervisor over the same terminal job:
		// the removal is refused again with the same motivo — the log
		// already carries the needs_attention, so nothing is repeated; the
		// close is re-attempted and still fails.
		sup2 := f.supRestarted(t)
		sup2.Herdr = f.herdr
		sup2.Poll = f.sup.Poll
		sup2.finalize(StatusDone)
		events := f.events(t)
		if got := needsAttentionMotivos(events); !reflect.DeepEqual(got, []string{"dirty", "close_failed"}) {
			t.Fatalf("needs_attention motivos after pass two = %v, want still [dirty close_failed] (the log deduplicates them)", got)
		}
		if n := countTipes(events, "workspace_closed"); n != 1 {
			t.Fatalf("workspace_closed events after pass two = %d, want 1 (written once, before the first close)", n)
		}
		if n := countTipes(events, "folder_retained"); n != 1 {
			t.Fatalf("folder_retained events after pass two = %d, want still 1", n)
		}
		if len(f.herdr.CloseIDs) != 2 {
			t.Fatalf("herdr close = %v, want one attempt per pass", f.herdr.CloseIDs)
		}
		if st := f.state(t); st.WorkspaceClosed {
			t.Fatalf("state = %+v, want the workspace left open (the close failed)", st)
		}
		assertWorktreeKept(t, f)
	})

	t.Run("a needs_attention wakes the dispatcher right away after the wake worker stopped", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		acFixture(t, f)
		fakeDir := t.TempDir()
		if _, err := fakecli.InstallWithOptions(t, fakeDir, "wakehook", []fakecli.Rule{{AnyArgs: true}},
			fakecli.InstallOptions{CaptureStdin: true, CaptureEnv: wakeEnvKeys}); err != nil {
			t.Fatalf("install the fake wake hook: %v", err)
		}
		env := platform.Env{}
		for _, item := range fakecli.Env(nil, fakeDir) {
			if key, value, ok := strings.Cut(item, "="); ok && key != "" {
				env[key] = value
			}
		}
		f.sup.Wake = &WakeHook{Cmd: "wakehook", JobID: supGitID, Env: env}
		f.job.commitIn(t, "feat: first")
		if err := os.WriteFile(filepath.Join(f.job.dir, "leftover.txt"), []byte("wip\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		events := f.events(t)
		na := indexOfType(events, "needs_attention")
		if na < 0 {
			t.Fatalf("events = %v, want a needs_attention for the dirty tree", eventTipes(events))
		}
		var woke []fakecli.Call
		for _, call := range wakeHookCalls(t, fakeDir) {
			if call.Env["SOHO_JOB_EVENT"] == "needs_attention" {
				woke = append(woke, call)
			}
		}
		if len(woke) != 1 {
			t.Fatalf("needs_attention hook runs = %d, want exactly one", len(woke))
		}
		if woke[0].Env["SOHO_JOB_SEQ"] != strconv.Itoa(events[na].Seq) {
			t.Fatalf("hook seq = %q, want %d", woke[0].Env["SOHO_JOB_SEQ"], events[na].Seq)
		}
		if !strings.Contains(woke[0].Stdin, `"motivo":"dirty"`) {
			t.Fatalf("hook stdin = %q, want the dirty motivo", woke[0].Stdin)
		}
	})

	t.Run("an unpushed head keeps the worktree (not_pushed)", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		acFixture(t, f)
		// The commit is local only: nothing was ever pushed.
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.Transition(supGitID, StatusDone); err != nil {
			t.Fatal(err)
		}
		f.sup.finalize(StatusDone)
		assertWorktreeKept(t, f)
		events := f.events(t)
		if n := countTipes(events, "needs_attention"); n != 1 {
			t.Fatalf("needs_attention events = %d, want 1 (%v)", n, eventTipes(events))
		}
		na := events[indexOfType(events, "needs_attention")]
		if na.Refs["motivo"] != "not_pushed" {
			t.Fatalf("needs_attention refs.motivo = %q, want not_pushed", na.Refs["motivo"])
		}
		if len(f.herdr.CloseIDs) != 1 || f.herdr.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want ws-1 (the close follows a kept worktree)", f.herdr.CloseIDs)
		}
		if st := f.state(t); st.WorktreeRemoved {
			t.Fatalf("state = %+v, want the worktree kept", st)
		}
		if limpeza := f.report(t)["limpeza"].(map[string]any); limpeza["worktree"] != "kept" {
			t.Fatalf("report limpeza = %v, want the kept worktree", limpeza)
		}
	})

	t.Run("a failed release removes nothing", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		acFixture(t, f)
		f.team.releaseErr = errors.New("the release failed")
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		exit, done, err := f.sup.finishOutcome(StatusCanceled, "cancelado", ExitCanceled)
		if err != nil || !done || exit != ExitCanceled {
			t.Fatalf("finishOutcome = %d %v %v, want canceled with exit 21", exit, done, err)
		}
		// The release did not end clean in this pass: no cleanup event
		// (the existing gate) and no removal (the same gate).
		assertWorktreeKept(t, f)
		events := f.events(t)
		for _, tipo := range []string{"cleanup", "needs_attention"} {
			if n := countTipes(events, tipo); n != 0 {
				t.Fatalf("%s events = %d, want none after a failed release", tipo, n)
			}
		}
		if n := countTipes(events, "folder_retained"); n != 1 {
			t.Fatalf("folder_retained events = %d, want 1", n)
		}
		// The close follows today's path: it never depends on the
		// release.
		if n := countTipes(events, "workspace_closed"); n != 1 {
			t.Fatalf("workspace_closed events = %d, want 1", n)
		}
		if len(f.herdr.CloseIDs) != 1 || f.herdr.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want ws-1 as today", f.herdr.CloseIDs)
		}
		if st := f.state(t); st.WorktreeRemoved {
			t.Fatalf("state = %+v, want the worktree kept", st)
		}
		if limpeza := f.report(t)["limpeza"].(map[string]any); limpeza["worktree"] != "kept" || limpeza["workspace"] != "closed" {
			t.Fatalf("report limpeza = %v, want the kept worktree and the closed workspace", limpeza)
		}
		frictionLine(t, f, "the release failed")
	})

	t.Run("a changing foreign pane makes the close wait, and a quiet one closes", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		herdr := acFixture(t, f)
		// The foreign pane changes on the second observation and then
		// goes quiet: the watch waits through both changes and closes
		// once the pane has stayed unchanged for the full one-minute
		// wait. The job's own panes (the root pane and the rostered
		// worker of this workspace) are listed too, and the
		// foreign-workspace roster row keeps pane-x foreign.
		herdr.PanesFn = func(call int) []herdrPane {
			revision := 1
			if call >= 2 {
				revision = 2
			}
			return []herdrPane{{"pane-root", 1}, {"pane-worker", 1}, {"pane-x", revision}}
		}
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		// Three observations (wait, wait, close) and exactly two poll
		// sleeps through the clock.
		if herdr.PanesCalls != 3 {
			t.Fatalf("panes calls = %d, want 3 (wait, wait, close)", herdr.PanesCalls)
		}
		if f.clock.sleeps != 2 {
			t.Fatalf("clock sleeps = %d, want 2 (the two waits)", f.clock.sleeps)
		}
		if len(f.herdr.CloseIDs) != 1 || f.herdr.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want ws-1 after the quiet wait", f.herdr.CloseIDs)
		}
		events := f.events(t)
		if n := countTipes(events, "workspace_closed"); n != 1 {
			t.Fatalf("workspace_closed events = %d, want 1", n)
		}
		if n := countTipes(events, "needs_attention"); n != 0 {
			t.Fatalf("needs_attention events = %d, want none (%v)", n, eventTipes(events))
		}
		if len(*f.friction) != 0 {
			t.Fatalf("friction = %v, want none", *f.friction)
		}
	})

	t.Run("a foreign pane that changes to the cap asks for human input and the workspace stays open", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		herdr := acFixture(t, f)
		// The foreign pane changes at every observation: the watch
		// waits the whole six-wait cap and then asks for attention
		// instead of closing.
		herdr.PanesFn = func(call int) []herdrPane {
			return []herdrPane{{"pane-root", 1}, {"pane-x", call}}
		}
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		// Eight observations: seven waits plus the cap observation, and
		// seven poll sleeps.
		if herdr.PanesCalls != 8 {
			t.Fatalf("panes calls = %d, want 8 (seven waits plus the cap observation)", herdr.PanesCalls)
		}
		if f.clock.sleeps != 7 {
			t.Fatalf("clock sleeps = %d, want 7 (the waits to the cap)", f.clock.sleeps)
		}
		// The workspace stays open: no close, no workspace_closed, the
		// state and the report say so.
		if len(f.herdr.CloseIDs) != 0 {
			t.Fatalf("herdr close = %v, want none (the workspace stays open)", f.herdr.CloseIDs)
		}
		events := f.events(t)
		if n := countTipes(events, "needs_attention"); n != 1 {
			t.Fatalf("needs_attention events = %d, want 1 (%v)", n, eventTipes(events))
		}
		na := events[indexOfType(events, "needs_attention")]
		if na.Refs["motivo"] != "human_input" {
			t.Fatalf("needs_attention refs.motivo = %q, want human_input", na.Refs["motivo"])
		}
		if n := countTipes(events, "workspace_closed"); n != 0 {
			t.Fatalf("workspace_closed events = %d, want none", n)
		}
		if n := countTipes(events, "folder_retained"); n != 1 {
			t.Fatalf("folder_retained events = %d, want 1", n)
		}
		// The folder_retained lands before the close decision, so before
		// the human_input needs_attention.
		if idx := indexOfType(events, "folder_retained"); !(idx >= 0 && idx < indexOfType(events, "needs_attention")) {
			t.Fatalf("event order = %v, want folder_retained before the human_input needs_attention", eventTipes(events))
		}
		if st := f.state(t); st.WorkspaceClosed {
			t.Fatalf("state = %+v, want the workspace left open", st)
		}
		rep := f.report(t)
		limpeza, ok := rep["limpeza"].(map[string]any)
		if !ok || limpeza["workspace"] != "open" {
			t.Fatalf("report limpeza = %v, want the open workspace", rep["limpeza"])
		}
		// The removal ran before the close decision (the clean pushed
		// tree): the worktree is gone even though the workspace stays
		// open.
		assertWorktreeRemoved(t, f)
		if len(*f.friction) != 0 {
			t.Fatalf("friction = %v, want none", *f.friction)
		}
	})

	t.Run("a restart in the middle of the wait starts the observation again", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		herdr := acFixture(t, f)
		herdr.PanesFn = func(call int) []herdrPane {
			return []herdrPane{{"pane-root", 1}, {"pane-x", call}}
		}
		f.job.commitIn(t, "feat: first")
		// The process dies on the third wait sleep, well before the cap.
		f.clock.stopAfter = 3
		f.clock.stop = midWaitCrash
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("the watch did not crash at the scripted sleep: the test proves nothing")
					return
				}
				if !errors.Is(r.(error), midWaitCrash) {
					t.Fatalf("crash = %v, want the scripted mid-wait crash", r)
				}
			}()
			if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
				t.Fatalf("finishOutcome: %v", err)
			}
		}()
		// The crash left the release done (the cleanup in the log), the
		// worktree removed (the removal runs before the watch), no close,
		// and no needs_attention (the cap was not reached).
		if n := countTipes(f.events(t), "cleanup"); n != 1 {
			t.Fatalf("cleanup events = %d, want 1 after the crash", n)
		}
		if n := countTipes(f.events(t), "needs_attention"); n != 0 {
			t.Fatalf("needs_attention events = %d, want none before the cap", n)
		}
		if len(f.herdr.CloseIDs) != 0 {
			t.Fatalf("herdr close = %v, want none mid-wait", f.herdr.CloseIDs)
		}
		if _, err := os.Stat(f.job.dir); !os.IsNotExist(err) {
			t.Fatalf("the worktree dir still exists after the crash: the removal must run before the watch")
		}
		if st := f.state(t); st.WorkspaceClosed {
			t.Fatalf("state = %+v, want the workspace left open mid-wait", st)
		}
		// The restart is a fresh process with a fresh clock: the
		// observation starts again (conservative) and runs to the cap —
		// seven full waits — and the needs_attention lands exactly once.
		f.clock.stopAfter = 0
		f.clock.stop = nil
		sup2 := f.supRestarted(t)
		sup2.Clock = &fakeClock{now: f.clock.Now()}
		sup2.Herdr = herdr
		sup2.Poll = f.sup.Poll
		if exit, err := sup2.Run(); err != nil || exit != 0 {
			t.Fatalf("restarted Run = %d %v, want 0", exit, err)
		}
		if sleeps := sup2.Clock.(*fakeClock).sleeps; sleeps != 7 {
			t.Fatalf("restart clock sleeps = %d, want 7 (the observation restarted from scratch)", sleeps)
		}
		events := f.events(t)
		if n := countTipes(events, "needs_attention"); n != 1 {
			t.Fatalf("needs_attention events = %d, want exactly one after the restart", n)
		}
		na := events[indexOfType(events, "needs_attention")]
		if na.Refs["motivo"] != "human_input" {
			t.Fatalf("needs_attention refs.motivo = %q, want human_input", na.Refs["motivo"])
		}
		if len(f.herdr.CloseIDs) != 0 {
			t.Fatalf("herdr close = %v, want none (the workspace stays open)", f.herdr.CloseIDs)
		}
		if st := f.state(t); st.WorkspaceClosed {
			t.Fatalf("state = %+v, want the workspace left open", st)
		}
		if limpeza := f.report(t)["limpeza"].(map[string]any); limpeza["workspace"] != "open" {
			t.Fatalf("report limpeza = %v, want the open workspace", limpeza)
		}
	})

	t.Run("a client without the Panes method closes as today with a friction line", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		acFixture(t, f)
		np := &noPanesHerdr{}
		f.sup.Herdr = np
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		if len(np.CloseIDs) != 1 || np.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want ws-1 as today", np.CloseIDs)
		}
		if n := countTipes(f.events(t), "workspace_closed"); n != 1 {
			t.Fatalf("workspace_closed events = %d, want 1", n)
		}
		frictionLine(t, f, "no pane list")
		assertWorktreeRemoved(t, f)
	})

	t.Run("a failed pane read closes as today with a friction line", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		herdr := acFixture(t, f)
		herdr.PanesErr = errors.New("the pane list failed")
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		if len(f.herdr.CloseIDs) != 1 || f.herdr.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want ws-1 as today", f.herdr.CloseIDs)
		}
		if n := countTipes(f.events(t), "workspace_closed"); n != 1 {
			t.Fatalf("workspace_closed events = %d, want 1", n)
		}
		frictionLine(t, f, "pane list failed")
		assertWorktreeRemoved(t, f)
	})

	t.Run("a failed roster read closes as today with a friction line", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		acFixture(t, f)
		f.team.rosterErr = errors.New("the roster failed")
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		if len(f.herdr.CloseIDs) != 1 || f.herdr.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want ws-1 as today", f.herdr.CloseIDs)
		}
		if n := countTipes(f.events(t), "workspace_closed"); n != 1 {
			t.Fatalf("workspace_closed events = %d, want 1", n)
		}
		frictionLine(t, f, "roster could not be read")
		assertWorktreeRemoved(t, f)
	})

	t.Run("the workspace mode removes nothing", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		acFixture(t, f)
		checkout := f.job.base.checkout()
		if _, err := f.store.Record(supGitID, func(st *State) {
			st.Checkout = checkout
			st.Dir = checkout
			st.Modo = "workspace"
			st.RootPane = "pane-root"
		}); err != nil {
			t.Fatal(err)
		}
		f.sup.Git = nil // workspace mode runs no git at all
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		exit, done, err := f.sup.finishOutcome(StatusDone, "", 0)
		if err != nil || !done || exit != 0 {
			t.Fatalf("finishOutcome = %d %v %v, want done with exit 0", exit, done, err)
		}
		// The job's worktree dir and its branch are untouched.
		assertWorktreeKept(t, f)
		events := f.events(t)
		if n := countTipes(events, "needs_attention"); n != 0 {
			t.Fatalf("needs_attention events = %d, want none (the workspace mode never removes)", n)
		}
		if n := countTipes(events, "folder_retained"); n != 1 {
			t.Fatalf("folder_retained events = %d, want 1", n)
		}
		if n := countTipes(events, "workspace_closed"); n != 1 {
			t.Fatalf("workspace_closed events = %d, want 1", n)
		}
		if len(f.herdr.CloseIDs) != 1 || f.herdr.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want ws-1", f.herdr.CloseIDs)
		}
		if st := f.state(t); st.WorktreeRemoved {
			t.Fatalf("state = %+v, want no removal recorded", st)
		}
		rep := f.report(t)
		limpeza, ok := rep["limpeza"].(map[string]any)
		if !ok || limpeza["worktree"] != "kept" || limpeza["workspace"] != "closed" {
			t.Fatalf("report limpeza = %v, want the kept worktree and the closed workspace", rep["limpeza"])
		}
	})

	t.Run("the job folder is never deleted", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		acFixture(t, f)
		f.job.commitIn(t, "feat: first")
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		// The removal happened...
		assertWorktreeRemoved(t, f)
		// ...and the job folder survived it: the folder_retained path is
		// the folder that still exists and is readable.
		assertJobFolderAlive(t, f)
		events := f.events(t)
		if n := countTipes(events, "folder_retained"); n != 1 {
			t.Fatalf("folder_retained events = %d, want 1", n)
		}
		folder := events[indexOfType(events, "folder_retained")]
		if folder.Refs["path"] != f.jobDir {
			t.Fatalf("folder_retained refs.path = %q, want %q", folder.Refs["path"], f.jobDir)
		}
		if _, err := os.Stat(folder.Refs["path"]); err != nil {
			t.Fatalf("the folder_retained path %q is gone: %v", folder.Refs["path"], err)
		}
	})
}

// needsAttentionMotivos lists the refs.motivo of the needs_attention
// events, in log order.
func needsAttentionMotivos(events []Event) []string {
	var out []string
	for _, event := range events {
		if event.Tipo == "needs_attention" {
			out = append(out, event.Refs["motivo"])
		}
	}
	return out
}
