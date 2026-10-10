package job

import (
	"testing"
)

// report_nocommits_test.go pins criterion 9 on a real local bare remote and
// a real job worktree: a worktree job that ends with no commit ahead of the
// base reports pr null, commits [] and the motivo "sem commits" with the
// head equal to the remote head; a nil commit list (a failed or unrun
// listing) is never labeled sem commits; a state motivo that is already
// set is never overwritten.

func TestReportWorktreeNoCommits(t *testing.T) {
	t.Run("a worktree job with no commit ahead of the base reports sem commits", func(t *testing.T) {
		f := newSupGitFixture(t, nil)
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		exit, done, err := f.sup.finishOutcome(StatusDone, "", 0)
		if err != nil || !done || exit != 0 {
			t.Fatalf("finishOutcome = %d done=%v err=%v, want done with exit 0", exit, done, err)
		}
		rep := f.report(t)
		t.Logf("pr=%v motivo=%v head=%v remote=%v", rep["pr"], rep["motivo"], rep["head"], rep["head_remoto"])
		if rep["pr"] != nil {
			t.Fatalf("pr = %v, want null", rep["pr"])
		}
		if rep["motivo"] != "sem commits" {
			t.Fatalf("motivo = %v, want sem commits", rep["motivo"])
		}
		commits := reportSlice(t, rep, "commits")
		if len(commits) != 0 {
			t.Fatalf("commits = %v, want none", commits)
		}
		head, _ := rep["head"].(string)
		if head == "" {
			t.Fatalf("head = %v, want the worktree head", rep["head"])
		}
		if rep["head_remoto"] != head {
			t.Fatalf("head_remoto = %v, want the head %q (the final sync pushed it)", rep["head_remoto"], head)
		}
		if len(*f.friction) != 0 {
			t.Fatalf("friction = %v, want none", *f.friction)
		}
	})

	t.Run("a failed commit listing is never labeled sem commits", func(t *testing.T) {
		f := newSupGitFixture(t, nil)
		f.sup.Git.Base = "no-such-base"
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		exit, done, err := f.sup.finishOutcome(StatusDone, "", 0)
		if err != nil || !done || exit != 0 {
			t.Fatalf("finishOutcome = %d done=%v err=%v, want done with exit 0", exit, done, err)
		}
		rep := f.report(t)
		if rep["motivo"] != nil {
			t.Fatalf("motivo = %v, want null (the commit listing failed, it is not known empty)", rep["motivo"])
		}
		if head, _ := rep["head"].(string); head == "" {
			t.Fatalf("head = %v, want the worktree head (the listing, not the head, failed)", rep["head"])
		}
		if len(*f.friction) == 0 {
			t.Fatal("friction is empty, want the commit listing failure")
		}
	})

	t.Run("a state motivo that is already set is never overwritten", func(t *testing.T) {
		f := newSupGitFixture(t, nil)
		// The zero-commit conditions all hold (head set, the commit list
		// known and empty, no pull request); the stored terminal motivo
		// must still win.
		motivo := "cancelado"
		if _, err := f.store.Record(supGitID, func(st *State) { st.Motivo = &motivo }); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		exit, done, err := f.sup.finishOutcome(StatusCanceled, "cancelado", ExitCanceled)
		if err != nil || !done || exit != ExitCanceled {
			t.Fatalf("finishOutcome = %d done=%v err=%v, want canceled with exit 21", exit, done, err)
		}
		rep := f.report(t)
		if rep["status"] != "canceled" {
			t.Fatalf("status = %v, want canceled", rep["status"])
		}
		if rep["motivo"] != "cancelado" {
			t.Fatalf("motivo = %v, want the stored cancelado (zero commits never overwrite it)", rep["motivo"])
		}
	})
}
