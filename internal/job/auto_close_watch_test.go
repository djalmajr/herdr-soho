package job

// The automatic close watch is pure over the monotonic clock: these tests
// drive closeWatchStep with a fixed 5-minute wait (the built-in default)
// and fake monotonic nows, so no real time elapses.

import (
	"testing"
	"time"
)

const (
	closeWatchTestWait  = 5 * time.Minute
	closeWatchTestEpoch = "2026-10-01T00:00:00Z"
)

func closeWatchTestBase() time.Time {
	base, err := time.Parse(time.RFC3339, closeWatchTestEpoch)
	if err != nil {
		panic(err)
	}
	return base
}

func wantCloseOutcome(t *testing.T, out closeOutcome, decision, motivo string) {
	t.Helper()
	if out.Decision != decision || out.Motivo != motivo {
		t.Fatalf("outcome = %+v, want decision %q motivo %q", out, decision, motivo)
	}
}

func assertZeroCloseWatch(t *testing.T, watch closeWatch) {
	t.Helper()
	if watch.Panes != nil || !watch.Start.IsZero() || !watch.Changed.IsZero() {
		t.Fatalf("watch = %+v, want the zero watch (Panes nil, zero times)", watch)
	}
}

// TestCloseWatchNoForeignPane pins the first rule: with no foreign pane —
// no pane at all, or only the job's own panes, even changing panes — the
// decision is close and the watch resets.
func TestCloseWatchNoForeignPane(t *testing.T) {
	base := closeWatchTestBase()
	job := map[string]bool{"w9:p1": true, "w9:p2": true}
	t.Run("no pane at all closes and resets the watch", func(t *testing.T) {
		out, next := closeWatchStep(nil, job, base, closeWatchTestWait, closeWatch{})
		wantCloseOutcome(t, out, CloseWatchClose, "")
		assertZeroCloseWatch(t, next)
	})
	t.Run("only job panes close", func(t *testing.T) {
		panes := []herdrPane{{PaneID: "w9:p1", Revision: 3}, {PaneID: "w9:p2", Revision: 4}}
		out, _ := closeWatchStep(panes, job, base, closeWatchTestWait, closeWatch{})
		wantCloseOutcome(t, out, CloseWatchClose, "")
	})
	t.Run("job panes changing close on every observation", func(t *testing.T) {
		watch := closeWatch{}
		var out closeOutcome
		for i, rev := range []int{1, 9, 40} {
			panes := []herdrPane{{PaneID: "w9:p1", Revision: rev}, {PaneID: "w9:p2", Revision: rev + 1}}
			out, watch = closeWatchStep(panes, job, base.Add(time.Duration(i)*time.Minute), closeWatchTestWait, watch)
			wantCloseOutcome(t, out, CloseWatchClose, "")
		}
	})
}

// TestCloseWatchChangingPaneWaitsUntilTheCap pins the wait rule: a
// foreign pane that changes revision at every observation keeps the
// decision wait while the episode's total wait is within 6 × wait —
// including the exact cap — and turns to needs_attention (motivo
// human_input) once it passes, staying there while the episode lasts.
func TestCloseWatchChangingPaneWaitsUntilTheCap(t *testing.T) {
	base := closeWatchTestBase()
	job := map[string]bool{"w9:p1": true}
	panes := func(revision int) []herdrPane { return []herdrPane{{PaneID: "w9:p9", Revision: revision}} }
	watch := closeWatch{}
	var out closeOutcome
	for i := 0; i <= 30; i++ {
		now := base.Add(time.Duration(i) * time.Minute)
		out, watch = closeWatchStep(panes(i+1), job, now, closeWatchTestWait, watch)
		if out.Decision != CloseWatchWait || out.Motivo != "" {
			t.Fatalf("minute %d: outcome = %+v, want wait", i, out)
		}
	}
	for i := 31; i <= 33; i++ {
		now := base.Add(time.Duration(i) * time.Minute)
		out, watch = closeWatchStep(panes(i+1), job, now, closeWatchTestWait, watch)
		wantCloseOutcome(t, out, CloseWatchNeedsAttention, closeWatchMotivoHumanInput)
	}
}

// TestCloseWatchQuietPaneCloses pins the close rule: the first
// observation counts as a change now, the pane changing nothing keeps the
// decision wait until a full wait has passed, and then it is close,
// stable on a re-observation while the episode lasts.
func TestCloseWatchQuietPaneCloses(t *testing.T) {
	base := closeWatchTestBase()
	job := map[string]bool{}
	panes := []herdrPane{{PaneID: "w9:p9", Revision: 7}}
	watch := closeWatch{}
	out, watch := closeWatchStep(panes, job, base, closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchWait, "")
	out, watch = closeWatchStep(panes, job, base.Add(4*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchWait, "")
	out, watch = closeWatchStep(panes, job, base.Add(5*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchClose, "")
	out, watch = closeWatchStep(panes, job, base.Add(6*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchClose, "")
}

// TestCloseWatchCapPrecedesQuiet pins the cap's precedence: the pane has
// been quiet since the episode's first observation, but the total wait
// passes 6 × wait, so the decision is needs_attention, not close.
func TestCloseWatchCapPrecedesQuiet(t *testing.T) {
	base := closeWatchTestBase()
	job := map[string]bool{}
	panes := []herdrPane{{PaneID: "w9:p9", Revision: 7}}
	_, watch := closeWatchStep(panes, job, base, closeWatchTestWait, closeWatch{})
	out, _ := closeWatchStep(panes, job, base.Add(31*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchNeedsAttention, closeWatchMotivoHumanInput)
}

// TestCloseWatchAppearanceAndDisappearanceRestartTheCount pins the
// change rule for set changes: a foreign pane that appears mid-episode
// and one that disappears both count as a change, restarting the quiet
// count of the surviving panes.
func TestCloseWatchAppearanceAndDisappearanceRestartTheCount(t *testing.T) {
	base := closeWatchTestBase()
	job := map[string]bool{}
	a := func(revision int) herdrPane { return herdrPane{PaneID: "w9:pA", Revision: revision} }
	b := herdrPane{PaneID: "w9:pB", Revision: 9}
	watch := closeWatch{}
	out, watch := closeWatchStep([]herdrPane{a(1)}, job, base, closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchWait, "")
	// A stays unchanged at 3 minutes: still within the wait.
	out, watch = closeWatchStep([]herdrPane{a(1)}, job, base.Add(3*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchWait, "")
	// B appears at 6 minutes: a change now, so wait — without it A alone
	// would have been quiet for 6 minutes, past the 5-minute wait, and
	// the decision would be close.
	out, watch = closeWatchStep([]herdrPane{a(1), b}, job, base.Add(6*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchWait, "")
	out, watch = closeWatchStep([]herdrPane{a(1), b}, job, base.Add(9*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchWait, "")
	out, watch = closeWatchStep([]herdrPane{a(1), b}, job, base.Add(11*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchClose, "")
	// B disappears at 14 minutes: a change now, so wait — without it A
	// alone would have been quiet for 14 minutes and the decision would
	// be close.
	out, watch = closeWatchStep([]herdrPane{a(1)}, job, base.Add(14*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchWait, "")
	// Quiet since 14 minutes for a full wait: close.
	out, watch = closeWatchStep([]herdrPane{a(1)}, job, base.Add(19*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchClose, "")
}

// TestCloseWatchMultipleForeignPanes pins the "some pane changed" rule
// over several foreign panes: while one pane changed within the last
// wait the decision is wait; when every pane has been unchanged for a
// full wait it is close.
func TestCloseWatchMultipleForeignPanes(t *testing.T) {
	base := closeWatchTestBase()
	job := map[string]bool{}
	quiet := herdrPane{PaneID: "w9:pQ", Revision: 5}
	working1 := herdrPane{PaneID: "w9:pW", Revision: 1}
	working2 := herdrPane{PaneID: "w9:pW", Revision: 2}
	watch := closeWatch{}
	out, watch := closeWatchStep([]herdrPane{quiet, working1}, job, base, closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchWait, "")
	// At 4 minutes W changes revision: a change now, still wait.
	out, watch = closeWatchStep([]herdrPane{quiet, working2}, job, base.Add(4*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchWait, "")
	// Both panes have now been unchanged for a full wait: close.
	out, watch = closeWatchStep([]herdrPane{quiet, working2}, job, base.Add(9*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchClose, "")
}

// TestCloseWatchResetsWhenThePanesLeave pins the episode reset: a watch
// that lasted far past the cap resets fully when the foreign panes leave,
// and a new episode starts its own total wait instead of inheriting the
// old one.
func TestCloseWatchResetsWhenThePanesLeave(t *testing.T) {
	base := closeWatchTestBase()
	job := map[string]bool{}
	panes := []herdrPane{{PaneID: "w9:p9", Revision: 1}}
	_, watch := closeWatchStep(panes, job, base, closeWatchTestWait, closeWatch{})
	for i := 1; i <= 40; i++ {
		_, watch = closeWatchStep(panes, job, base.Add(time.Duration(i)*time.Minute), closeWatchTestWait, watch)
	}
	out, next := closeWatchStep(nil, job, base.Add(41*time.Minute), closeWatchTestWait, watch)
	wantCloseOutcome(t, out, CloseWatchClose, "")
	assertZeroCloseWatch(t, next)
	// A new episode at 100 minutes starts fresh: wait, not
	// needs_attention (41 minutes of the old episode cannot leak in).
	out, _ = closeWatchStep(panes, job, base.Add(100*time.Minute), closeWatchTestWait, next)
	wantCloseOutcome(t, out, CloseWatchWait, "")
}
