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
	if s.Git == nil {
		return 0, false, nil
	}
	head, err := s.Git.Head()
	if err != nil {
		s.frictionf(err.Error())
		return 0, false, nil
	}
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		s.frictionf("job: git sync: " + err.Error())
		return 0, false, nil
	}
	events, err := readEvents(jobDir)
	if err != nil {
		s.frictionf("job: git sync: " + err.Error())
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
		s.lastHead = head
		commits, err := s.Git.Commits()
		if err != nil {
			s.frictionf(err.Error())
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
	}
	result, err := s.Git.Push()
	if err != nil {
		s.frictionf(err.Error())
		return 0, false, nil
	}
	switch {
	case result.Rejected:
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
		pr, _, err := s.Git.EnsureDraftPR(s.prTitle, s.prBody())
		if err != nil {
			s.frictionf(err.Error())
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
// running is left to the next state.
func (s *Supervisor) blockPushRejection() error {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return err
	}
	if snap.State.Status != StatusRunning {
		return nil
	}
	if _, err := s.Store.Transition(s.ID, StatusBlocked); err != nil {
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
// any other motivo is never lifted here.
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
	if _, err := s.Store.Transition(s.ID, StatusRunning); err != nil {
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
func (s *Supervisor) finalSync(status string) {
	if (status == StatusCanceled || status == StatusTimeout) && s.Git != nil {
		if _, err := s.Git.Checkpoint(); err != nil {
			s.frictionf(err.Error())
		}
	}
	// The final sync ignores the watch cadence.
	s.syncGitOnce()
}

// finalize finishes every terminal outcome the same way, after the
// outcome is recorded and finalSync has pushed: the complete report facts, the
// release of the job orchestrator, the team, and the registered copies and
// processes, the cleanup event (checked against the log first so a retry
// converges), and the Herdr workspace close last. It never removes a
// worktree, a directory, a branch, or a pull request; its failures go to
// friction and the path continues, because everything durable is written
// before the process may end with the workspace.
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
	// path continues.
	if snap.State.WorkspaceID != "" {
		if snap.State.Orchestrator != "" {
			if err := s.Ops.Release(snap.State.Orchestrator); err != nil {
				s.frictionf(err.Error())
			}
		}
		if err := s.Ops.ReleaseTeam(); err != nil {
			s.frictionf(err.Error())
		}
		if err := s.Ops.GC(); err != nil {
			s.frictionf(err.Error())
		}
	}

	// The cleanup event is checked against the log first: a crashed first
	// attempt may have recorded it already, and the retry converges.
	cleanupDone := false
	for _, event := range events {
		if event.Tipo == "cleanup" {
			cleanupDone = true
			break
		}
	}
	if !cleanupDone {
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
	if snap.State.WorkspaceID == "" || s.Herdr == nil {
		return
	}
	if err := s.Herdr.Close(snap.State.WorkspaceID); err != nil {
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
