// supervise_git.go is the supervisor's git sync and the terminal path: it
// keeps the job branch pushed (announcing the new commits, pushing them
// without ever forcing, and opening at most one draft pull request on a
// 30 s tick cadence), turns a rejected push into a blocked job that a
// later successful push unblocks, and finishes every terminal outcome the
// same way — checkpoint the leftovers of a canceled or timed-out job, run
// the final push and pull request, publish the complete report facts,
// release the job orchestrator, the team and the registered copies and
// processes, record the cleanup event, and close the Herdr workspace
// last. It never removes a worktree, a directory, a branch, or a pull
// request.
package job

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// gitSyncCadence is the git watch cadence: 30 s of Poll ticks (6 ticks at
// the 5 s default poll). The cadence counts ticks, not wall-clock
// differences, so a moving clock cannot end or stretch it.
const gitSyncCadence = 30 * time.Second

// syncGit keeps the job branch in sync on every tick: on pushNow or once
// every 30 s of ticks it runs a sync pass. A nil Git (workspace mode)
// runs no git at all. GitSync failures go to friction without a state
// change; store write failures propagate.
func (s *Supervisor) syncGit() (int, bool, error) {
	s.lastPushTick++
	due := s.pushNow || s.lastPushTick >= s.syncTicks()
	s.pushNow = false
	if !due {
		return 0, false, nil
	}
	s.lastPushTick = 0
	return s.syncGitOnce()
}

// syncTicks is the ticks per cadence, clamped to at least one.
func (s *Supervisor) syncTicks() int {
	every := int(gitSyncCadence / s.poll())
	if every < 1 {
		every = 1
	}
	return every
}

// syncGitOnce runs one sync pass: the new commit events, the push, the
// unblock of a block this path set, and the draft pull request when the
// remote carries the local head and no pr_opened event exists yet. It is
// called from the watch loop (syncGit) and from finalize (the final push
// and pull request, where the cadence does not apply).
func (s *Supervisor) syncGitOnce() (int, bool, error) {
	return s.syncGitPass(nil)
}

// finSync is what one bounded final sync learned: cut is true when a step
// of the terminal procedure ran out of its bound (a subprocess killed by
// the clamped timeout, or a step skipped for lack of time) and failed is
// true when any step failed for another reason (a git or gh error, a
// refused push, a store error): the sync is recorded as complete only when
// it neither failed nor was cut.
type finSync struct{ cut, failed bool }

// fail marks the final sync as not clean; a nil fin (the watch loop's pass)
// records nothing.
func (f *finSync) fail() {
	if f != nil {
		f.failed = true
	}
}

// cutIfStepEnded marks the final sync cut when the failure just seen is the
// step's bound being reached; a nil fin (the watch loop's pass) is never cut.
func (s *Supervisor) cutIfStepEnded(fin *finSync) {
	if fin != nil && s.stepCut() {
		fin.cut = true
	}
}

// syncGitPass is syncGitOnce with an optional bounded final sync (fin, set
// by finalSync): the git steps run under the step deadline finalSync began,
// and the draft pull request is its own step with its own bound.
func (s *Supervisor) syncGitPass(fin *finSync) (int, bool, error) {
	if s.Git == nil {
		return 0, false, nil
	}
	head, err := s.Git.Head()
	if err != nil {
		s.frictionf(err.Error())
		fin.fail()
		s.cutIfStepEnded(fin)
		return 0, false, nil
	}
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		s.frictionf("job: git sync: " + err.Error())
		fin.fail()
		return 0, false, nil
	}
	events, err := readEvents(jobDir)
	if err != nil {
		s.frictionf("job: git sync: " + err.Error())
		fin.fail()
		return 0, false, nil
	}
	// The announced shas seed from the existing commit events, so a
	// restarted supervisor never re-announces a commit.
	if s.announced == nil {
		s.announced = map[string]bool{}
	}
	prKnown := false
	for _, event := range events {
		switch event.Tipo {
		case "commit":
			if sha := event.Refs["sha"]; sha != "" {
				s.announced[sha] = true
			}
		case "pr_opened":
			prKnown = true
		}
	}
	if head != s.lastHead {
		// lastHead is remembered only after the commits are announced: a
		// failed listing leaves it unchanged so the next sync walks again.
		commits, err := s.Git.Commits()
		if err != nil {
			s.frictionf(err.Error())
			fin.fail()
			s.cutIfStepEnded(fin)
			return 0, false, nil
		}
		for _, commit := range commits {
			if s.announced[commit.SHA] {
				continue
			}
			resumo, err := cutResumo(commit.Titulo)
			if err != nil {
				return 0, false, err
			}
			if _, err := s.Store.Append(s.ID, EventIn{
				Tipo:   "commit",
				Resumo: resumo,
				Refs:   map[string]string{"sha": commit.SHA},
			}); err != nil {
				return 0, false, err
			}
			s.announced[commit.SHA] = true
		}
		if len(commits) > 0 {
			// The draft pull request's title is the first (oldest)
			// commit of the job branch.
			s.prTitle = commits[0].Titulo
		}
		s.lastHead = head
	}
	result, err := s.Git.Push()
	if err != nil {
		s.frictionf(err.Error())
		fin.fail()
		s.cutIfStepEnded(fin)
		return 0, false, nil
	}
	switch {
	case result.Rejected:
		fin.fail()
		if _, err := s.Store.Append(s.ID, EventIn{
			Tipo: "push",
			Refs: map[string]string{"motivo": "push_rejected"},
		}); err != nil {
			return 0, false, err
		}
		if err := s.blockPushRejection(); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	case result.Pushed:
		if _, err := s.Store.Append(s.ID, EventIn{
			Tipo:   "push",
			Resumo: "pushed",
			Refs:   map[string]string{"sha": result.Head},
		}); err != nil {
			return 0, false, err
		}
		s.unblockPushRejection()
	}
	// The remote carries the local head (this push or an earlier one):
	// open the draft pull request when one is not known yet. No commits,
	// no pull request.
	if !prKnown && s.prTitle != "" {
		if fin != nil && !s.beginFinStep("the draft pull request", s.FinishingLimits.gh()) {
			fin.cut = true
			return 0, false, nil
		}
		pr, _, err := s.Git.EnsureDraftPR(s.prTitle, s.prBody())
		if err != nil {
			s.frictionf(err.Error())
			fin.fail()
			s.cutIfStepEnded(fin)
			return 0, false, nil
		}
		s.lastPR = &pr
		if _, err := s.Store.Append(s.ID, EventIn{
			Tipo:   "pr_opened",
			Resumo: "draft pull request opened",
			Refs:   map[string]string{"pr": strconv.Itoa(pr.Numero)},
		}); err != nil {
			return 0, false, err
		}
	}
	return 0, false, nil
}

// blockPushRejection blocks a running job for a rejected push: the
// transition, the stored motivo, and the blocked event. A job that is not
// running is left to the next state. The crossing is one locked write:
// the transition and the settle (the counters and the re-anchor) land
// together, so the blocked interval starts at the crossing and a crash at
// the crossing never leaves the transition without the settle.
func (s *Supervisor) blockPushRejection() error {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return err
	}
	if snap.State.Status != StatusRunning {
		return nil
	}
	if err := s.settleAndTransition(snap.State, StatusBlocked, false); err != nil {
		return err
	}
	motivo := "push_rejected"
	if _, err := s.Store.Record(s.ID, func(st *State) { st.Motivo = &motivo }); err != nil {
		return err
	}
	_, err = s.Store.Append(s.ID, EventIn{
		Tipo: "blocked",
		Refs: map[string]string{"motivo": "push_rejected"},
	})
	return err
}

// unblockPushRejection lifts a block this path set: a successful push that
// leaves the job blocked for push_rejected moves it back to running,
// clears the stored motivo, and lands the unblocked event. A block with
// any other motivo is never lifted here. The crossing is one locked
// write: the transition and the settle (the counters and the re-anchor)
// land together, so the active interval starts at the unblock and a crash
// at the crossing never leaves the transition without the settle.
func (s *Supervisor) unblockPushRejection() {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		s.frictionf("job: git sync: " + err.Error())
		return
	}
	st := snap.State
	if st.Status != StatusBlocked || st.Motivo == nil || *st.Motivo != "push_rejected" {
		return
	}
	if err := s.settleAndTransition(st, StatusRunning, true); err != nil {
		s.frictionf("job: git sync: " + err.Error())
		return
	}
	if _, err := s.Store.Record(s.ID, func(st *State) { st.Motivo = nil }); err != nil {
		s.frictionf("job: git sync: " + err.Error())
		return
	}
	if _, err := s.Store.Append(s.ID, EventIn{Tipo: "unblocked"}); err != nil {
		s.frictionf("job: git sync: " + err.Error())
	}
}

// prBody is the draft pull request body: Job: <id> and, when the brief has
// an origem.ref, Origin: <ref> — nothing else: no hosts, paths, machine,
// model, or event text.
func (s *Supervisor) prBody() string {
	lines := []string{"Job: " + s.ID}
	dir, err := s.Store.readDir(s.ID)
	if err == nil {
		if fields, err := readBriefFields(dir); err == nil {
			if origem, ok := fields["origem"].(map[string]any); ok {
				if ref, ok := origem["ref"].(string); ok && ref != "" {
					lines = append(lines, "Origin: "+ref)
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// frictionf reports one line to the friction sink when one is wired.
func (s *Supervisor) frictionf(message string) {
	if s.Friction != nil {
		s.Friction(message)
	}
}

// finalSync is the final push, run while the job is still finishing and
// before the terminal event, so the final commit, push and pr_opened
// events precede the terminal event and wake the dispatcher with it. On
// cancel or timeout the leftovers are committed first as
// wip(job): checkpoint.
//
// The final sync runs inside the terminal procedure's budget
// (finishing_started_at): the git steps (the checkpoint, the commit
// announcements and the push) are one bounded step and the draft pull
// request another, each ending at its own bound or at the budget's end,
// whichever comes first, with every subprocess timeout clamped to what is
// left of the step. It reports whether a step was cut (a subprocess killed
// by the clamp, or a step skipped for lack of time); the caller turns a cut
// into the failed outcome of a done job. Only a fully clean sync is recorded
// (finishing_synced), so a restart does not run it again: a step that was cut
// or failed for another reason (a git or gh error, a refused push, a store
// error) leaves the field false, and the restart runs the sync again.
func (s *Supervisor) finalSync(status string) (cut bool) {
	if s.Git == nil {
		return false
	}
	s.ensureFinishingBase()
	if snap, err := s.Store.Snapshot(s.ID); err == nil && snap.State.FinishingSynced {
		return false
	}
	s.Git.Limit = s.finCap
	defer func() {
		s.Git.Limit = nil
		s.stepDeadline, s.stepName = 0, ""
	}()
	if !s.beginFinStep("the final push", s.FinishingLimits.git()) {
		return true
	}
	fin := &finSync{}
	if status == StatusCanceled || status == StatusTimeout {
		if _, err := s.Git.Checkpoint(); err != nil {
			s.frictionf(err.Error())
			fin.fail()
			s.cutIfStepEnded(fin)
		}
	}
	if fin.cut {
		return true
	}
	// The final sync ignores the watch cadence.
	if _, _, err := s.syncGitPass(fin); err != nil {
		// A store error returned by the pass: the sync is not clean.
		fin.fail()
	}
	if fin.cut {
		return true
	}
	if fin.failed {
		return false
	}
	if _, err := s.Store.Record(s.ID, func(st *State) { st.FinishingSynced = true }); err != nil {
		s.frictionf("job: the supervisor could not record the completed final sync: " + err.Error())
	}
	return false
}

// finalize finishes every terminal outcome the same way, after the
// outcome is recorded and finalSync has pushed: the collaboration
// settlement of the persisted outcomes (a fresh process over a terminal
// job captures them first), the release of the job orchestrator, the
// team, and the registered copies and processes, the cleanup event (the
// release gate and the log check below), and the Herdr workspace close
// last. It never removes a worktree, a directory, a branch, or a pull
// request; its failures go to friction and the path continues, because
// everything durable is written before the process may end with the
// workspace.
func (s *Supervisor) finalize(status string) {
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		s.frictionf("job: finalize: " + err.Error())
		return
	}
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		s.frictionf("job: finalize: " + err.Error())
		return
	}
	events, err := readEvents(jobDir)
	if err != nil {
		s.frictionf("job: finalize: " + err.Error())
		return
	}
	facts := s.reportFacts(snap.State, events, jobDir)
	// The collaboration terminal step (supervise_collab.go): the outcomes
	// of this process's terminal path were persisted into the state
	// before the terminal transition, so they are settled here only; a
	// fresh process over a terminal job (a restart) has not captured and
	// runs the capture first — the state-first enumeration, the ask of the
	// still-active ones (once in its own process) and the wait on the
	// shared deadline. Either way the confirmed facts (the final phase,
	// finalized_by_supervisor and the cause) are recorded back into the
	// state, which is the single source of the report's colaboracoes and
	// partial fields. When the enumeration could not be restored by the
	// deadline the persisted ids stand and the release, the cleanup and
	// the workspace close are refused: the close is what guarantees no
	// orphan pane, and it runs only when the enumeration succeeded.
	var settleSrc []CollaborationOutcome
	if s.collabUnenumerated {
		// This process's capture could not enumerate the collaborations:
		// the known ids are persisted already; the settlement, the
		// release, the cleanup and the workspace close are refused, as on
		// the fresh-process path below.
		return
	}
	if s.collabEnded {
		settleSrc = snap.State.Colaboracoes
	} else {
		outcomes, capErr := s.endCollaborations(s.collabDeadline())
		if capErr != nil {
			s.frictionf("job: the supervisor cannot enumerate the collaborations: " + capErr.Error())
			if err := s.persistCollabOutcomes(outcomes); err != nil {
				s.frictionf("job: finalize: " + err.Error())
			}
			return
		}
		s.collabEnded = true
		if err := s.persistCollabOutcomes(outcomes); err != nil {
			s.frictionf("job: finalize: " + err.Error())
			return
		}
		settleSrc = outcomes
	}
	settled := s.settleCollaborations(settleSrc)
	if len(settled) > 0 {
		if err := s.persistCollabOutcomes(settled); err != nil {
			s.frictionf("job: finalize: " + err.Error())
			return
		}
	}
	if snap.State.WorkspaceID != "" {
		// The release, the cleanup, and the workspace close apply to jobs
		// with a recorded Herdr workspace; a legacy job without one ends
		// as before.
		facts.Workspace = "closed"
		facts.PanesFechados = 0
	}
	if _, err := s.Store.WriteReport(s.ID, facts); err != nil {
		s.frictionf("job: finalize: " + err.Error())
		return
	}

	// Release: the job orchestrator, then the remaining workers, then
	// the job's registered copies and processes. Errors are friction; the
	// path continues, but a failed release in this pass keeps the cleanup
	// event out of this pass (the gate below): the event claims the
	// processes are released, so it lands only on a pass whose release
	// ended clean; a later pass re-evaluates the release and appends it
	// then.
	released := true
	if snap.State.WorkspaceID != "" {
		if snap.State.Orchestrator != "" {
			if err := s.Ops.Release(snap.State.Orchestrator); err != nil {
				s.frictionf(err.Error())
				released = false
			}
		}
		if err := s.Ops.ReleaseTeam(); err != nil {
			s.frictionf(err.Error())
			released = false
		}
		if err := s.Ops.GC(); err != nil {
			s.frictionf(err.Error())
			released = false
		}
	}

	// The cleanup event is checked against the log first: a crashed first
	// attempt may have recorded it already, and the retry converges. The
	// release gate: it is appended only when the release of this pass
	// ended clean, so a failed release keeps the event pending for a
	// later pass (and a pass whose release fails after the event is
	// recorded adds nothing, the log already carries it).
	cleanupDone := false
	for _, event := range events {
		if event.Tipo == "cleanup" {
			cleanupDone = true
			break
		}
	}
	if !cleanupDone && released {
		if _, err := s.Store.Append(s.ID, EventIn{
			Tipo:   "cleanup",
			Resumo: "processes released; worktree kept",
		}); err != nil {
			s.frictionf("job: finalize: " + err.Error())
			return
		}
	}

	// The workspace close is last: the supervisor process may end with
	// the workspace; everything durable is already written.
	if snap.State.WorkspaceID == "" || s.Herdr == nil || snap.State.WorkspaceClosed {
		return
	}
	if !validWorkspaceID(snap.State.WorkspaceID) {
		// The recorded id is not one safe path segment: a close would
		// hand a hostile argument to `herdr workspace close`, so the
		// workspace is left open exactly as a failed close is — the
		// fixed friction line, the report rewritten open, and no
		// WorkspaceClosed record.
		s.frictionf("job: the recorded workspace id is invalid; the workspace was not closed")
		facts.Workspace = "open"
		if _, err := s.Store.WriteReport(s.ID, facts); err != nil {
			s.frictionf("job: finalize: " + err.Error())
		}
		return
	}
	if err := s.Herdr.Close(snap.State.WorkspaceID); err == nil {
		// Recorded so a later supervisor never closes the workspace twice;
		// the process may end with the workspace right after this.
		if _, err := s.Store.Record(s.ID, func(st *State) { st.WorkspaceClosed = true }); err != nil {
			s.frictionf("job: finalize: " + err.Error())
		}
	} else {
		// The close did not happen: the report says the workspace stayed
		// open, and the retry closes it.
		s.frictionf(err.Error())
		facts.Workspace = "open"
		if _, err := s.Store.WriteReport(s.ID, facts); err != nil {
			s.frictionf("job: finalize: " + err.Error())
		}
	}
}

// reportFacts is the finalize report facts: the git facts and commits with
// their push state, the draft pull request, the cost, and the logs.
func (s *Supervisor) reportFacts(st State, events []Event, jobDir string) ReportFacts {
	facts := ReportFacts{}
	if s.Git != nil {
		if gitFacts, err := s.Git.Facts(); err != nil {
			s.frictionf(err.Error())
		} else {
			facts.Clean = gitFacts.Clean
			facts.Head = gitFacts.Head
			facts.HeadRemoto = gitFacts.HeadRemoto
		}
		if commits, err := s.Git.Commits(); err != nil {
			s.frictionf(err.Error())
		} else {
			// The remote head contains the commits when it is the local
			// head; with the head unknown every commit is pending.
			push := "pendente"
			if facts.Head != "" && facts.Head == facts.HeadRemoto {
				push = "ok"
			}
			for i := range commits {
				commits[i].Push = push
			}
			facts.Commits = commits
		}
	}
	// The draft pull request: the number from the pr_opened event and the
	// URL of this process's EnsureDraftPR when it is the same pull
	// request; without the URL the report carries the number only.
	for _, event := range events {
		if event.Tipo != "pr_opened" {
			continue
		}
		if numero, err := strconv.Atoi(event.Refs["pr"]); err == nil {
			if s.lastPR != nil && s.lastPR.Numero == numero {
				facts.PR = s.lastPR
			} else {
				estado := "draft"
				facts.PR = &PullRequest{Numero: numero, Estado: estado}
			}
		}
		break
	}
	workers := 0
	for _, event := range events {
		if event.Tipo == "worker_spawned" {
			workers++
		}
	}
	fim := s.Store.clock().Now()
	var duracao int
	if started, err := time.Parse(time.RFC3339, st.StartedAt); err == nil {
		duracao = int(fim.Sub(started) / time.Second)
		if duracao < 0 {
			duracao = 0
		}
	}
	facts.Custo = &Custo{
		Inicio:   st.StartedAt,
		Fim:      formatTS(fim),
		DuracaoS: duracao,
		Workers:  workers,
	}
	facts.Logs = &Logs{
		ReportMD: filepath.Join(jobDir, "report.md"),
		StateDir: jobDir,
		Friction: filepath.Join(jobDir, "friction.log"),
	}
	return facts
}
