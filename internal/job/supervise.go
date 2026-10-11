package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/reportscan"
)

// supervise.go is the job supervisor: the process job start launches in the
// first pane of the job's Herdr workspace. It makes sure the job
// orchestrator has a lane, spawns it in the job worktree, dispatches the
// job's brief to it once, and then watches it until its report lands or it
// stops, moving the job through running, blocked and finishing to a
// terminal outcome with the contract's events and report.json. It composes
// the CLI's own session, spawn, dispatch, status and release commands
// behind teamOps. Git sync, control delivery, the job budget, the wake hook
// and the final release are later slices; their extension points are the
// tick comment and the terminal-return TODO.

// startWait is how long the supervisor waits for the job to become running.
const startWait = 60 * time.Second

// defaultSupervisorPoll is the watch poll when Supervisor.Poll is zero.
const defaultSupervisorPoll = 5 * time.Second

// supervisorExit maps a terminal outcome to the contract exit code, as
// jobOutcomeExit in internal/cli does: the CLI imports the job package, so
// the mapping lives here and the CLI keeps its own for display.
func supervisorExit(status string) int {
	switch status {
	case StatusDone:
		return 0
	case StatusBlocked:
		return ExitBlocked
	case StatusTimeout:
		return ExitTimeout
	case StatusFailed:
		return ExitFailed
	case StatusCanceled:
		return ExitCanceled
	default:
		return 0
	}
}

// Supervisor watches one job. The loop sleeps through Clock.Sleep, never
// time.Sleep directly; a zero Poll is 5 s.
type Supervisor struct {
	Store    *Store
	ID       string
	Ops      teamOps
	Clock    Clock
	Poll     time.Duration
	Friction func(string)

	// Wake: the machine's job wake hook; nil or an empty Cmd disables it
	// (no worker, no cursor).
	Wake *WakeHook

	// PrivateLaunch: this supervisor was started by job start with the
	// private launch context (set by the CLI); only then does waitRunning
	// refuse a running or blocked state written inside a failure
	// (Store.Fail's walk).
	PrivateLaunch bool

	// Git sync and release: the git sync of the job branch — nil in
	// workspace mode, which runs no git at all — and the Herdr adapter
	// that closes the job workspace last.
	Git   *GitSync
	Herdr herdrWorkspaces

	// ProjectRoot is the trusted project root the supervisor binds the
	// collaborations it owns on (supervise_collab.go): the same root
	// resolution the CLI uses for collaborations, never derived from an
	// assignment record; an empty root owns nothing.
	ProjectRoot string

	// Git sync bookkeeping (pushNow, the immediate push ask of job
	// checkpoint, is declared with the control fields): the ticks since the
	// last sync, the last synced head, the announced
	// commit shas, the first commit's title, and this process's draft
	// pull request.
	lastPushTick int
	lastHead     string
	announced    map[string]bool
	prTitle      string
	lastPR       *PullRequest

	// Tick bookkeeping: consecutive bad observations (the gone-family
	// states, status errors) and the unknown states already reported to
	// friction.
	goneStreak int
	errStreak  int
	frictioned map[string]bool

	// Wake bookkeeping: the queue and its worker (nil when the hook is
	// disabled) and the highest event seq queued in this process
	// (initialized from the cursor).
	wakeQueue  chan Event
	wakeDone   chan struct{}
	wakeQueued int

	// Control delivery and the job budget (supervise_control.go): pushNow is
	// set here and read and cleared by the git sync slice; stopping is "" or
	// the kind the stop path ends with (canceled, timeout, or the blocked
	// ceiling — which ends through the existing timeout outcome) and
	// stopDeadline its Clock.Monotonic deadline; warnedActive and
	// warnedBlocked guard the single timeout_warning and blocked_warning;
	// intervalBase is the virtual Monotonic base of the current budget
	// interval it counts from, captured once on the first budget check of
	// this process (ensureIntervalBase) and re-anchored at every settled
	// blocked-boundary crossing (settleAndTransition).
	pushNow         bool
	stopping        string
	stopDeadline    int64
	warnedActive    bool
	warnedBlocked   bool
	intervalBase    int64
	intervalBaseSet bool

	// collabAsked guards the single ask of the job's active collaborations
	// in this process (supervise_collab.go): a restarted supervisor
	// re-asks once in its own process, the same accepted bound as the stop
	// amendment.
	collabAsked bool

	// collabEnded guards the single collaboration capture of this
	// process's terminal path: finishOutcome and fail capture and
	// persist the outcomes before the terminal transition, and finalize
	// then settles the persisted outcomes only; a fresh process over a
	// terminal job (a restart) has not captured and runs the capture in
	// finalize instead.
	collabEnded bool

	// collabUnenumerated records that this process's capture could not
	// enumerate the job's collaborations by the deadline: finalize then
	// refuses the settlement, the release, the cleanup and the workspace
	// close exactly as a fresh process's failed capture does, and a later
	// supervisor pass retries the capture.
	collabUnenumerated bool
}

// NewSupervisor wires the production supervisor over a recorded job: the
// job worktree from state.json is the working directory of every team
// command, the clock is the store's, the git sync watches the job branch
// (workspace mode has none), and the Herdr adapter owns the workspace
// close.
func NewSupervisor(store *Store, id string, env platform.Env, selfExe string, friction func(string)) (*Supervisor, error) {
	snap, err := store.Snapshot(id)
	if err != nil {
		return nil, err
	}
	if snap.State.Dir == "" {
		return nil, errUsage("job: the job has no worktree directory")
	}
	s := &Supervisor{
		Store:       store,
		ID:          id,
		Ops:         selfCLI{Exe: selfExe, Dir: snap.State.Dir, Env: env},
		Clock:       store.clock(),
		Friction:    friction,
		Herdr:       newHerdrCLI(env, friction),
		ProjectRoot: platform.StateProjectRoot(env, snap.State.Dir),
	}
	if snap.State.Modo != "workspace" && snap.State.Branch != "" {
		// A branch job without a recorded branch has no push refspec, so
		// it is not synced; workspace mode runs no git at all. The draft
		// pull request's repo comes from brief.json; a missing or
		// unreadable brief leaves it empty and the pr step errors on use.
		var repo string
		if dir, err := store.readDir(id); err == nil {
			if fields, err := readBriefFields(dir); err == nil {
				repo = jsonString(fields, "repo")
			}
		}
		s.Git = &GitSync{Env: env, Dir: snap.State.Dir, Branch: snap.State.Branch, Base: snap.State.Base, Repo: repo}
	}
	return s, nil
}

func (s *Supervisor) poll() time.Duration {
	if s.Poll > 0 {
		return s.Poll
	}
	return defaultSupervisorPoll
}

func (s *Supervisor) clockSleep(d time.Duration) {
	if s.Clock != nil {
		s.Clock.Sleep(d)
		return
	}
	systemClock{}.Sleep(d)
}

// monoToMs converts a monotonic nanosecond difference into the whole
// milliseconds the budget counters persist: the single conversion point
// between the clock's nanosecond Monotonic readings and the persisted
// millisecond counts, so nothing compares or stores the raw nanoseconds.
func monoToMs(nano int64) int64 {
	return nano / int64(time.Millisecond)
}

// ensureIntervalBase captures the process's virtual Monotonic base for the
// current budget interval, once per process: the first reading minus the
// wall elapsed since the persisted anchor (IntervalStart, or StartedAt on
// a state without it), clamped at zero so a future anchor starts the
// interval at zero. The wall clock is compared only here; from then on the
// in-flight interval is counted on the monotonic clock only, so a backwards
// Now never shortens or extends it.
//
// Legacy rule: a blocked state without an IntervalStart is a job started
// before the anchor field existed, whose blocked interval's start is
// unknown — the in-flight interval is counted as zero and the anchor is
// re-anchored to now (persisted), losing at most the interval's elapsed
// tail and never double-counting. A blocked state with a recorded anchor
// always trusts it: every blocked-boundary crossing settles and re-anchors
// in the same state write (settleAndTransition), so a crash at a crossing
// leaves the complete pre-crossing state (old status, old counters, old
// anchor) and never a blocked status with a stale job-start anchor.
//
// The anchor is written with RFC3339Nano (sub-second resolution) and read
// back with the same layout; whole-second anchors written before the
// nanosecond precision (or by formatTS callers) parse unchanged.
func (s *Supervisor) ensureIntervalBase(st State) error {
	if s.intervalBaseSet {
		return nil
	}
	if st.Status == StatusBlocked && st.IntervalStart == "" {
		// Legacy state: the blocked interval's start is unrecorded: re-anchor
		// to now and count the in-flight interval as zero.
		s.intervalBase = s.Clock.Monotonic()
		s.intervalBaseSet = true
		return s.recordIntervalAnchor(st)
	}
	anchor := st.IntervalStart
	if anchor == "" {
		anchor = st.StartedAt
	}
	s.intervalBase = s.Clock.Monotonic()
	if anchor != "" {
		if start, err := time.Parse(time.RFC3339Nano, anchor); err == nil {
			if elapsed := s.Clock.Now().Sub(start); elapsed > 0 {
				s.intervalBase -= int64(elapsed)
			}
		}
	}
	s.intervalBaseSet = true
	return nil
}

// recordIntervalAnchor persists the interval anchor at the current wall
// time: the re-anchor record the legacy rule leaves behind, so the next
// process re-derives its virtual base from the same anchor. A no-op when
// the anchor already carries it. The anchor is written with RFC3339Nano
// (sub-second resolution): a whole-second anchor would shift a restarted
// in-flight interval by up to 999 ms.
func (s *Supervisor) recordIntervalAnchor(st State) error {
	now := s.Clock.Now().Format(time.RFC3339Nano)
	if st.IntervalStart == now {
		return nil
	}
	_, err := s.Store.Record(s.ID, func(st *State) { st.IntervalStart = now })
	return err
}

// settleAndTransition settles the finished budget interval — the whole
// milliseconds of the virtual Monotonic elapsed since the process's
// interval base, clamped at zero, into the counter of the finished
// interval's kind (blocked when finishedBlocked, active otherwise) and the
// re-anchor at the crossing — and moves the job along the crossing edge in
// one locked state write (TransitionSettle): the status change and the
// settle land together, so a crash at the crossing leaves either the
// pre-crossing state (the finished interval still in flight, of its old
// kind) or the post-crossing state (settled and re-anchored) — never the
// window the separate Transition + settle writes left, which a restart
// would misread as an in-flight interval of the new status's kind. The
// process's virtual base moves to the crossing instant.
func (s *Supervisor) settleAndTransition(st State, to string, finishedBlocked bool) error {
	return s.settleAndTransitionWith(st, to, finishedBlocked, nil)
}

// settleAndTransitionWith is settleAndTransition with extra facts (update,
// may be nil) recorded in the same locked write as the crossing.
func (s *Supervisor) settleAndTransitionWith(st State, to string, finishedBlocked bool, update func(*State)) error {
	if err := s.ensureIntervalBase(st); err != nil {
		return err
	}
	before := s.Clock.Monotonic()
	elapsed := before - s.intervalBase
	if elapsed < 0 {
		elapsed = 0
	}
	ms := monoToMs(int64(elapsed))
	if _, err := s.Store.TransitionSettle(s.ID, to, func(st *State) {
		if finishedBlocked {
			st.BudgetBlockedMs += ms
		} else {
			st.BudgetActiveMs += ms
		}
		st.IntervalStart = s.Clock.Now().Format(time.RFC3339Nano)
		if update != nil {
			update(st)
		}
	}); err != nil {
		return err
	}
	s.intervalBase = before
	s.intervalBaseSet = true
	return nil
}

// collabDeadline is the shared deadline of the collaboration wait on a
// terminal path: the stop deadline when a stop is running in this process
// (cancel and timeout keep the stop's original grace, never restarted or
// extended), one defaultTimeoutGrace from now for done and failed and for
// a fresh process over a terminal job.
func (s *Supervisor) collabDeadline() int64 {
	if s.stopping != "" {
		return s.stopDeadline
	}
	return s.Clock.Monotonic() + int64(defaultTimeoutGrace.Nanoseconds())
}

// persistCollabOutcomes merges the captured outcomes into the persisted
// ones (the no-late-erasure rule) and records the merge into the state,
// the single source of the report's colaboracoes. An empty merge is a
// no-op that rewrites nothing.
func (s *Supervisor) persistCollabOutcomes(outcomes []CollaborationOutcome) error {
	if len(outcomes) == 0 {
		return nil
	}
	_, err := s.Store.Record(s.ID, func(st *State) {
		st.Colaboracoes = mergeCollabOutcomes(st.Colaboracoes, outcomes)
	})
	return err
}

// Run drives the job to a terminal outcome. The exit code follows the
// outcome (done 0, failed 19, canceled 21, timeout 9, blocked 7); err is
// non-nil only for unexpected store failures, which exit with the
// conservative failed code.
func (s *Supervisor) Run() (int, error) {
	// The wake worker runs for the whole process and drains on every
	// return, so waking events behind the cursor are run even when the
	// supervisor returns before the watch loop (an already-ended job, a
	// failed start wait). A panic is a crash: it does not drain, and the
	// cursor reruns the events on the next start.
	s.startWake()
	defer func() {
		if r := recover(); r != nil {
			panic(r)
		}
		s.finishWakes()
	}()
	// The terminal intent (persisted before the terminal transition on
	// every terminal path): a crash left the outcome behind without the
	// transition, and this process converges on it instead of re-deriving
	// the outcome. A job already terminal keeps today's path (finalize
	// again; settle the persisted outcomes; never re-ask or rewrite a
	// persisted outcome).
	if snap, err := s.Store.Snapshot(s.ID); err == nil {
		intent := snap.State.TerminalIntent
		if intent != nil && !isTerminalOutcome(snap.State.Status) &&
			snap.State.Status != StatusCollected && snap.State.Status != StatusClosed {
			if intent.Status == StatusFailed {
				if err := s.fail(intent.Motivo, intent.Exit); err != nil {
					return ExitFailed, err
				}
				return intent.Exit, nil
			}
			if snap.State.Status == StatusRunning || snap.State.Status == StatusBlocked {
				if _, err := s.Store.Transition(s.ID, StatusFinishing); err != nil {
					return ExitFailed, err
				}
			}
			code, _, err := s.finishOutcome(intent.Status, intent.Motivo, intent.Exit)
			if err != nil {
				return ExitFailed, err
			}
			return code, nil
		}
		// The pending-decision path entered finishing with its motivo and
		// no intent, and a crash came before the crossing to blocked:
		// complete it, then watch the blocked job as usual.
		if intent == nil && snap.State.Status == StatusFinishing &&
			snap.State.Motivo != nil && *snap.State.Motivo == decisionsPendingMotivo {
			if _, _, err := s.blockOnPendingDecisions(snap.State, snap.State.Orchestrator); err != nil {
				return ExitFailed, err
			}
		}
	}
	code, done, err := s.waitRunning()
	if err != nil {
		return ExitFailed, err
	}
	if done {
		// A job that already ended may not have finished its release (a
		// crash before the workspace close): finalize again; it skips the
		// steps already done. A job without a recorded workspace has no
		// release to finish.
		if snap, snapErr := s.Store.Snapshot(s.ID); snapErr == nil && isTerminalOutcome(snap.State.Status) {
			if snap.State.WorkspaceID != "" && !snap.State.WorkspaceClosed {
				s.finishWakes()
				s.finalize(snap.State.Status)
			}
			// A crash between the terminal record and the retirement of the
			// cancel leaves it pending on the terminal job: retire it
			// idempotently and send nothing.
			s.retirePendingCancel()
		}
		return code, nil
	}
	// The job lane (and its max_workers bump) is set once, before the spawn
	// intent is recorded; a restarted supervisor (the intent is recorded, or
	// the orchestrator is) skips it, so the bump is never applied twice.
	if snap, err := s.Store.Snapshot(s.ID); err != nil {
		return ExitFailed, err
	} else if snap.State.Orchestrator == "" && snap.State.OrchestratorSpawnAt == "" {
		if err := s.Ops.EnsureJobLane(); err != nil {
			if err := s.fail("job: cannot ensure the job lane", ExitHerdr); err != nil {
				return ExitFailed, err
			}
			return ExitHerdr, nil
		}
	}
	name, done, err := s.ensureOrchestrator()
	if err != nil {
		return ExitFailed, err
	}
	if done {
		return ExitHerdr, nil
	}
	code, done, err = s.ensureDispatched(name)
	if err != nil {
		return ExitFailed, err
	}
	if done {
		return code, nil
	}
	for {
		code, done, err := s.tick()
		if err != nil {
			return ExitFailed, err
		}
		if done {
			return code, nil
		}
		s.clockSleep(s.poll())
	}
}

// waitRunning waits for the running transition of job start. It reads the
// state every poll for at most 60 s of polls: a running or a blocked job
// proceeds at once (a blocked job is alive — the orchestrator is waiting
// on a question, and the watch loop keeps enforcing the budgets and the
// blocked ceiling while it stays blocked), a terminal state returns its
// outcome code at once, and a job still not running after the bound fails
// with exit 19. The bound counts sleep iterations, not wall-clock
// differences, so a moving clock cannot end the wait early. Only a
// privately scoped supervisor (PrivateLaunch) refuses a running or
// blocked snapshot written inside a failure: the store's failure walk
// appends the failure event and saves running and finishing before
// failed, so a snapshot read in that window is a failure in flight, not
// the start's running, and the wait keeps polling; every other
// supervisor accepts the snapshot at once, exactly as before.
func (s *Supervisor) waitRunning() (int, bool, error) {
	poll := s.poll()
	maxSleeps := int(startWait / poll)
	if maxSleeps < 1 {
		maxSleeps = 1
	}
	for i := 0; ; i++ {
		snap, err := s.Store.Snapshot(s.ID)
		if err == nil {
			if snap.State.Status == StatusRunning || snap.State.Status == StatusBlocked {
				// A failure in flight is not the start's running: the store's
				// failure walk appends the failure event and saves running and
				// finishing before failed, so a snapshot read in that window is
				// refused below and the wait keeps polling. The refusal applies
				// only to this supervisor's own private scoped execution
				// (PrivateLaunch): a supervisor without the private context
				// accepts exactly as before.
				if !s.PrivateLaunch {
					return 0, false, nil
				}
				dir, dirErr := s.Store.readDir(s.ID)
				accepted := dirErr == nil
				if dirErr == nil {
					// A failed directory or events read, or a log that already
					// holds a failure event, is refused: keep polling like a
					// not-yet-running state.
					events, eventsErr := readEvents(dir)
					accepted = eventsErr == nil && !hasTipo(events, "failure")
				}
				if accepted {
					return 0, false, nil
				}
				// Refused: fall through to the sleep and keep polling.
			}
			if isTerminalOutcome(snap.State.Status) {
				return supervisorExit(snap.State.Status), true, nil
			}
		}
		if i >= maxSleeps {
			break
		}
		s.clockSleep(poll)
	}
	if err := s.fail("job: supervisor started before the job was running", ExitFailed); err != nil {
		return ExitFailed, true, err
	}
	return ExitFailed, true, nil
}

// ensureOrchestrator records the job orchestrator and appends its
// worker_spawned event exactly once. A fresh job records the spawn intent
// (OrchestratorSpawnAt) before spawning: a crash between the spawn and the
// Orchestrator record leaves the intent without the name, and the
// restarted supervisor recovers the orchestrator it already spawned —
// adopting it when its identity is proven, spawning when it is proven that
// none exists, and failing the job closed without spawning otherwise. A
// crash between the Orchestrator record and the event append leaves the
// name recorded with no event: the event is appended once, keyed on the
// name. done is true when the spawn failed or the recovery failed closed
// (the job already failed); err is a store failure.
func (s *Supervisor) ensureOrchestrator() (string, bool, error) {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return "", false, err
	}
	if snap.State.Orchestrator != "" {
		// A crash between the record and the append left the name with no
		// worker_spawned: append it once when the spawn intent shows the
		// name came from this job's spawn. A name without the intent (an
		// older record or a recovery-free restart) is untouched.
		if snap.State.OrchestratorSpawnAt != "" {
			if err := s.appendWorkerSpawnedOnce(snap.State.Orchestrator); err != nil {
				return "", false, err
			}
		}
		return snap.State.Orchestrator, false, nil
	}
	if snap.State.OrchestratorSpawnAt != "" {
		// The spawn intent is recorded and the name is not: the spawn ran
		// and a crash lost the record. Recover the orchestrator it left.
		return s.recoverOrchestrator(snap.State)
	}
	// The spawn intent is recorded before the spawn, so the crash window
	// between the spawn and the record still leaves it behind for a
	// restart to recover from.
	spawnAt := formatTS(s.Store.clock().Now())
	if _, err := s.Store.Record(s.ID, func(st *State) { st.OrchestratorSpawnAt = spawnAt }); err != nil {
		return "", false, err
	}
	return s.spawnOrchestrator()
}

// recoverOrchestrator is the restart path of ensureOrchestrator when the
// spawn intent is recorded and the name is not: the crash happened after
// the spawn. The roster gives only the identity (name, role, pane); the
// liveness of each job-orchestrator candidate in the job's own workspace
// comes from the authoritative Status read the supervisor already uses.
// A single live candidate is adopted — recorded and evented exactly as the
// normal path does, without a spawn. With no candidate, a workspace whose
// only pane is the supervisor's own root pane proves the orchestrator
// pane is gone, and the orchestrator is spawned as normal; a gone
// candidate counts as no candidate. Anything else — a status read error,
// more than one candidate, a roster read error, an invalid or missing workspace, a multi-pane
// workspace, a list error, no Herdr adapter — fails the job closed without
// spawning: the supervisor never spawns blind and never releases or closes
// a pane it did not prove is the job's.
func (s *Supervisor) recoverOrchestrator(st State) (string, bool, error) {
	rows, err := s.Ops.Roster()
	if err != nil {
		return s.failOrchestratorRecovery()
	}
	var candidates []rosterRow
	for _, row := range rows {
		if row.Role != "job-orchestrator" {
			continue
		}
		// The candidate's pane must live in the job's own workspace: the
		// text before the first : of the pane, compared to a recorded id
		// that is one safe path segment. The roster STATE column is not
		// read: a spaced tab label before it shifts it.
		workspace, _, ok := strings.Cut(row.Pane, ":")
		if !ok || !validWorkspaceID(st.WorkspaceID) || workspace != st.WorkspaceID {
			continue
		}
		// Liveness is the candidate's own status read: gone is not a
		// candidate; a failed read fails the recovery closed; every other
		// state keeps the row a candidate.
		state, _, err := s.Ops.Status(row.Name)
		if err != nil {
			return s.failOrchestratorRecovery()
		}
		if state == "gone" {
			continue
		}
		candidates = append(candidates, row)
	}
	if len(candidates) == 1 {
		// Adopt the orchestrator the crash left alive: no spawn.
		name := candidates[0].Name
		if _, err := s.Store.Record(s.ID, func(st *State) { st.Orchestrator = name }); err != nil {
			return "", false, err
		}
		if _, err := s.Store.Append(s.ID, EventIn{
			Tipo:   "worker_spawned",
			Resumo: "job orchestrator spawned",
			Refs:   map[string]string{"agente": name, "papel": "job-orchestrator"},
		}); err != nil {
			return "", false, err
		}
		return name, false, nil
	}
	if len(candidates) > 1 {
		// Two live orchestrators in the job workspace: the identity is
		// unproven.
		return s.failOrchestratorRecovery()
	}
	if s.Herdr == nil || !validWorkspaceID(st.WorkspaceID) {
		// No adapter, or a recorded id that is not one safe path segment:
		// the job workspace cannot be identified in the list.
		return s.failOrchestratorRecovery()
	}
	workspaces, err := s.Herdr.List()
	if err != nil {
		return s.failOrchestratorRecovery()
	}
	for _, workspace := range workspaces {
		if workspace.ID != st.WorkspaceID {
			continue
		}
		if workspace.PaneCount != 1 {
			// A pane beyond the supervisor's own root pane may be the
			// crashed orchestrator's: the identity is unproven.
			return s.failOrchestratorRecovery()
		}
		// One pane, the supervisor's own root pane: no orchestrator pane
		// exists, so spawn as normal.
		return s.spawnOrchestrator()
	}
	// The job workspace is missing from the list.
	return s.failOrchestratorRecovery()
}

// failOrchestratorRecovery is the fail-closed of the recovery: the job
// fails with the fixed motivo and the Herdr exit code, without spawning
// and without releasing or closing a pane the supervisor did not prove is
// the job's.
func (s *Supervisor) failOrchestratorRecovery() (string, bool, error) {
	if err := s.fail("job: cannot establish the job orchestrator", ExitHerdr); err != nil {
		return "", true, err
	}
	return "", true, nil
}

// appendWorkerSpawnedOnce appends the worker_spawned event for name when no
// worker_spawned event with that refs.agente exists yet: the crash lost the
// append after the record, and exactly one such event ever lands.
func (s *Supervisor) appendWorkerSpawnedOnce(name string) error {
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		return err
	}
	events, err := readEvents(jobDir)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.Tipo == "worker_spawned" && event.Refs["agente"] == name {
			return nil
		}
	}
	_, err = s.Store.Append(s.ID, EventIn{
		Tipo:   "worker_spawned",
		Resumo: "job orchestrator spawned",
		Refs:   map[string]string{"agente": name, "papel": "job-orchestrator"},
	})
	return err
}

// spawnOrchestrator spawns the job orchestrator, records its name, and
// appends the worker_spawned event. A crash between the spawn and the
// record leaves the spawn intent for the recovery; a crash between the
// record and the append leaves the name for the worker_spawned backfill.
// done is true when the spawn failed (the job already failed); err is a
// store failure.
func (s *Supervisor) spawnOrchestrator() (string, bool, error) {
	name, err := s.Ops.SpawnOrchestrator()
	if err != nil {
		if err := s.fail("job: cannot spawn the job orchestrator", ExitHerdr); err != nil {
			return "", true, err
		}
		return "", true, nil
	}
	if _, err = s.Store.Record(s.ID, func(st *State) { st.Orchestrator = name }); err != nil {
		return "", false, err
	}
	if _, err = s.Store.Append(s.ID, EventIn{
		Tipo:   "worker_spawned",
		Resumo: "job orchestrator spawned",
		Refs:   map[string]string{"agente": name, "papel": "job-orchestrator"},
	}); err != nil {
		return "", false, err
	}
	return name, false, nil
}

// ensureDispatched writes the orchestrator brief (the job brief plus the job
// section) and dispatches it once; a restart with DispatchedAt set never
// dispatches again. The exit code is returned when the dispatch (or the
// missing brief) failed; err is a store failure.
func (s *Supervisor) ensureDispatched(name string) (int, bool, error) {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return 0, false, err
	}
	if snap.State.DispatchedAt != "" {
		return 0, false, nil
	}
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		return 0, false, err
	}
	brief, err := os.ReadFile(filepath.Join(jobDir, "brief.md"))
	if err != nil {
		if err := s.fail("job: the job brief is missing", ExitFailed); err != nil {
			return ExitFailed, true, err
		}
		return ExitFailed, true, nil
	}
	brief = append(brief, orchestratorBriefSection(s.ID)...)
	path := filepath.Join(jobDir, "orchestrator-brief.md")
	if err := writeAtomic0600(path, brief); err != nil {
		return 0, false, err
	}
	if err := s.Ops.Dispatch(name, path, false); err != nil {
		if err := s.fail("job: cannot dispatch the job orchestrator", ExitHerdr); err != nil {
			return ExitFailed, true, err
		}
		return ExitHerdr, true, nil
	}
	// DispatchedAt comes from the store clock: RFC 3339 with an explicit
	// offset.
	dispatchedAt := formatTS(s.Store.clock().Now())
	if _, err := s.Store.Record(s.ID, func(st *State) { st.DispatchedAt = dispatchedAt }); err != nil {
		return 0, false, err
	}
	return 0, false, nil
}

// orchestratorBriefSection is the section appended to the job brief for the
// job orchestrator, with the job id filled in.
func orchestratorBriefSection(id string) []byte {
	return []byte("\n## Job\n\n- Job id: " + id +
		"\n- Publish decisions, questions, checkpoints and notes with `herdr-soho job note --id " + id + " --tipo <type> ...`." +
		"\n- Ask for an immediate push after a commit with `herdr-soho job checkpoint --id " + id + "`.\n")
}

// tick runs one watch iteration: the wake queue first (it only enqueues),
// then the dispatcher's control requests (amend, send, checkpoint,
// cancel), then the git sync (a checkpoint request pushes in the same tick;
// a rejected push blocks the job before the orchestrator is read), then,
// while a stop is running, the job budget (the stop path's end); otherwise
// the job orchestrator — read once, the blocked-boundary crossing first (a
// block entry's settle lands before the tick's budget check, so the active
// time accrued up to the block is enforced while blocked), then the job
// budget against the post-crossing state, then the done and gone-family
// outcomes.
func (s *Supervisor) tick() (int, bool, error) {
	if code, done, err := s.dispatchWakes(); err != nil || done {
		return code, true, err
	}
	if code, done, err := s.deliverControl(); err != nil || done {
		return code, done, err
	}
	if code, done, err := s.syncGit(); err != nil || done {
		return code, done, err
	}
	if s.stopping != "" {
		// A stop is already running: the stop path owns the job (a fresh
		// orchestrator read for the done check, no crossing).
		return s.checkBudget("")
	}
	// No stop running: checkOrchestrator reads the orchestrator once,
	// runs the blocked-boundary crossing first, then the budget check
	// against the post-crossing state (reusing this read for the stop
	// path's done check), then the done and gone-family outcomes.
	return s.checkOrchestrator()
}

// checkOrchestrator maps one status read of the job orchestrator to the
// job's transitions, events and outcome. The blocked-boundary crossing
// (block or unblock) runs first, when the read shows one: its settle lands
// before this tick's budget check, so a block entry's accrued active time
// is counted by that check while the job is blocked. The budget check then
// runs against the post-crossing state, reusing this tick's read for the
// stop path's done check (no second read); a stop that starts this tick
// ends the job on a done read (endStop), so the done and gone-family
// outcomes below are reached only when no stop is running. The gone streak
// counts consecutive gone-family observations and drops on any observation
// that shows the orchestrator alive; the error streak counts consecutive
// failed reads and drops on any read that succeeds. A done state without a
// readable report counts like a gone observation.
func (s *Supervisor) checkOrchestrator() (int, bool, error) {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		s.goneStreak = 0
		return s.accumulate()
	}
	st := snap.State
	state, reportPath, err := s.Ops.Status(st.Orchestrator)
	if err != nil {
		s.goneStreak = 0
		return s.accumulate()
	}
	s.errStreak = 0
	if aliveOrchestratorState[state] {
		s.goneStreak = 0
	}
	switch state {
	case "working", "no-report-yet", "idle", "compacting":
		// Nothing; a block this function set is lifted by the working
		// state.
		if code, done, err := s.unblockOrchestrator(st); err != nil || done {
			return code, done, err
		}
	case "blocked", "question":
		motivo := "dialog"
		if state == "question" {
			motivo = "question"
		}
		if code, done, err := s.blockOrchestrator(st, st.Orchestrator, motivo); err != nil || done {
			return code, done, err
		}
	case "quota":
		if code, done, err := s.blockOrchestrator(st, st.Orchestrator, "quota"); err != nil || done {
			return code, done, err
		}
	case "provider-error", "capacity":
		if code, done, err := s.blockOrchestrator(st, st.Orchestrator, "provider_error"); err != nil || done {
			return code, done, err
		}
	}
	// The budget check against the post-crossing state; a stop that starts
	// this tick reuses this tick's read (state) for the done check.
	if code, done, err := s.checkBudget(state); err != nil || done {
		return code, done, err
	}
	// No stop is running: the done and gone-family outcomes. The crossing
	// states (handled above) are no-ops here: nothing more this tick.
	switch state {
	case "done":
		return s.onOrchestratorDone(st, st.Orchestrator, reportPath)
	case "gone", "settled-no-report", "not-received", "unavailable":
		s.goneStreak++
		if s.goneStreak >= 3 {
			if err := s.fail("job: the job orchestrator ended without a report", ExitFailed); err != nil {
				return ExitFailed, true, err
			}
			return ExitFailed, true, nil
		}
		return 0, false, nil
	case "working", "no-report-yet", "idle", "compacting", "blocked", "question", "quota", "provider-error", "capacity":
		return 0, false, nil
	default:
		s.frictionUnknown(state)
		return 0, false, nil
	}
}

// accumulate counts a failed observation (a status error or an unreadable
// state): three in a row ends the job as failed.
func (s *Supervisor) accumulate() (int, bool, error) {
	s.errStreak++
	if s.errStreak < 3 {
		return 0, false, nil
	}
	if err := s.fail("job: the job orchestrator ended without a report", ExitFailed); err != nil {
		return ExitFailed, true, err
	}
	return ExitFailed, true, nil
}

// blockOrchestrator blocks a running job for the orchestrator's motivo and
// appends the blocked event; a job that is not running is left to the next
// state. The crossing is one locked write: the transition and the settle
// (the counters and the re-anchor) land together, so the blocked interval
// starts at the crossing and a crash at the crossing never leaves the
// transition without the settle.
func (s *Supervisor) blockOrchestrator(st State, name, motivo string) (int, bool, error) {
	if st.Status != StatusRunning {
		return 0, false, nil
	}
	if err := s.settleAndTransition(st, StatusBlocked, false); err != nil {
		return 0, true, err
	}
	if _, err := s.Store.Record(s.ID, func(st *State) { st.Motivo = &motivo }); err != nil {
		return 0, true, err
	}
	if _, err := s.Store.Append(s.ID, EventIn{
		Tipo: "blocked",
		Refs: map[string]string{"agente": name, "motivo": motivo},
	}); err != nil {
		return 0, true, err
	}
	return 0, false, nil
}

// aliveOrchestratorState are the states that show the orchestrator alive;
// they drop the gone streak. The gone-family states, the done state (a done
// without a readable report must keep accumulating) and the unknown ones
// leave the streak alone.
var aliveOrchestratorState = map[string]bool{
	"working": true, "no-report-yet": true, "idle": true, "compacting": true,
	"blocked": true, "question": true, "quota": true,
	"provider-error": true, "capacity": true,
}

// orchestratorMotivos are the stored motivos this function sets on a block;
// only those are lifted by a working state.
var orchestratorMotivos = map[string]bool{
	"dialog": true, "question": true, "quota": true, "provider_error": true,
}

// unblockOrchestrator lifts a block this function set: the job moves back
// to running, the stored motivo clears, and the unblocked event lands. The
// crossing is one locked write: the transition and the settle (the counters
// and the re-anchor) land together, so the active interval starts at the
// unblock and a crash at the crossing never leaves the transition without
// the settle.
func (s *Supervisor) unblockOrchestrator(st State) (int, bool, error) {
	if st.Status != StatusBlocked || st.Motivo == nil {
		return 0, false, nil
	}
	if !orchestratorMotivos[*st.Motivo] {
		return 0, false, nil
	}
	if err := s.settleAndTransition(st, StatusRunning, true); err != nil {
		return 0, true, err
	}
	if _, err := s.Store.Record(s.ID, func(st *State) { st.Motivo = nil }); err != nil {
		return 0, true, err
	}
	if _, err := s.Store.Append(s.ID, EventIn{Tipo: "unblocked"}); err != nil {
		return 0, true, err
	}
	return 0, false, nil
}

// onOrchestratorDone handles the report landing: the report is copied into
// the job dir, the worker_done event lands, the job moves to finishing, and
// the outcome follows the partial count and the pending decisions.
func (s *Supervisor) onOrchestratorDone(st State, name, reportPath string) (int, bool, error) {
	if st.Status == StatusBlocked && st.Motivo != nil && *st.Motivo == decisionsPendingMotivo {
		// The outcome was already derived: the loop keeps waiting for the
		// decision to be resolved, and derives nothing again.
		return 0, false, nil
	}
	// A regular file only, opened without blocking: a FIFO at the report
	// path reads as no report instead of holding the supervisor.
	raw, err := platform.ReadRegularFile(reportPath)
	if err != nil {
		// A done state without a readable report counts like a gone one.
		s.goneStreak++
		if s.goneStreak >= 3 {
			if err := s.fail("job: the job orchestrator ended without a report", ExitFailed); err != nil {
				return ExitFailed, true, err
			}
			return ExitFailed, true, nil
		}
		return 0, false, nil
	}
	partials := reportscan.PartialCount(string(raw))
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		return 0, true, err
	}
	reportMD := filepath.Join(jobDir, "report.md")
	if err := writeAtomic0600(reportMD, raw); err != nil {
		return 0, true, err
	}
	if _, err := s.Store.Append(s.ID, EventIn{
		Tipo:   "worker_done",
		Resumo: fmt.Sprintf("job orchestrator reported, %d partial", partials),
		Refs:   map[string]string{"agente": name, "report": reportMD},
	}); err != nil {
		return 0, true, err
	}
	// The outcome is derived before the job enters finishing, and the
	// entry records it in the same locked write: a crash can never leave
	// the job finishing without the terminal intent (or, for the pending
	// path, without the motivo the restart completes the crossing from).
	if partials == 0 {
		if err := s.enterFinishing(&TerminalIntent{Status: StatusDone}, nil); err != nil {
			return 0, true, err
		}
		return s.finishOutcome(StatusDone, "", 0)
	}
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return 0, true, err
	}
	if len(snap.Pending) > 0 {
		// Partial items with an open decision: the job stays alive,
		// blocked, until the decision lands; the loop keeps running. The
		// active interval (finishing counts as active) settles and
		// re-anchors with the crossing, in one locked write.
		motivo := decisionsPendingMotivo
		if err := s.enterFinishing(nil, &motivo); err != nil {
			return 0, true, err
		}
		return s.blockOnPendingDecisions(st, name)
	}
	motivo := "itens parciais"
	if err := s.enterFinishing(&TerminalIntent{Status: StatusFailed, Motivo: motivo, Exit: ExitFailed}, &motivo); err != nil {
		return 0, true, err
	}
	return s.finishOutcome(StatusFailed, motivo, ExitFailed)
}

// decisionsPendingMotivo is the motivo of a job whose orchestrator
// finished with partial items that still have an open decision.
const decisionsPendingMotivo = "decisions pending"

// enterFinishing moves the job to finishing and records the terminal
// intent and the motivo (either may be nil: left unchanged) in the same
// locked state write; a job already finishing records them with one
// Record. Either way the job is never durably finishing without what the
// restart converges on. Entering finishing from blocked is a
// blocked-boundary crossing like any other: the blocked interval settles
// into the blocked clock and the anchor moves to the crossing in the same
// write, so time spent blocked is never charged to the active budget (from
// running, both sides are active and nothing settles, as before).
func (s *Supervisor) enterFinishing(intent *TerminalIntent, motivo *string) error {
	update := func(st *State) {
		if intent != nil {
			st.TerminalIntent = intent
		}
		if motivo != nil {
			st.Motivo = motivo
		}
	}
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return err
	}
	if snap.State.Status == StatusFinishing {
		_, err = s.Store.Record(s.ID, update)
		return err
	}
	if snap.State.Status == StatusBlocked {
		return s.settleAndTransitionWith(snap.State, StatusFinishing, true, update)
	}
	_, err = s.Store.TransitionSettle(s.ID, StatusFinishing, update)
	return err
}

// blockOnPendingDecisions completes the pending-decision path from
// finishing: the crossing to blocked (the active interval settles and
// re-anchors in the same write) and the blocked event. A restart over a
// job left finishing with the pending motivo and no terminal intent (a
// crash between the two writes) completes it the same way.
func (s *Supervisor) blockOnPendingDecisions(st State, name string) (int, bool, error) {
	if err := s.settleAndTransition(st, StatusBlocked, false); err != nil {
		return 0, true, err
	}
	if _, err := s.Store.Append(s.ID, EventIn{
		Tipo: "blocked",
		Refs: map[string]string{"agente": name, "motivo": decisionsPendingMotivo},
	}); err != nil {
		return 0, true, err
	}
	return 0, false, nil
}

// finishOutcome commits the terminal outcome in the contract order: the
// terminal intent is persisted first (a crash leaves it for a restart to
// converge on), the final push and draft pull request run while the job is
// still finishing, the collaboration outcomes are captured and persisted
// before the terminal transition (so the partial facts exist before the
// outcome is visible), the terminal event lands at most once, and the
// transition publishes report.json — built from the state's persisted
// outcomes — before the retire, the wake drain and the finalize (the
// settlement, the release, the cleanup event, the workspace close).
func (s *Supervisor) finishOutcome(status, motivo string, exit int) (int, bool, error) {
	// The terminal intent is durable before anything else: a crash between
	// the intent and the terminal transition leaves the outcome for the
	// restart instead of re-deriving it.
	intent := &TerminalIntent{Status: status, Motivo: motivo, Exit: exit}
	if _, err := s.Store.Record(s.ID, func(st *State) { st.TerminalIntent = intent }); err != nil {
		return 0, true, err
	}
	// The final push happens while the job is still finishing, so its events
	// precede the terminal event.
	s.finalSync(status)
	// The collaboration capture: the outcomes are merged into the state
	// (the persisted ones stand forever) and persisted before the
	// terminal transition; an enumeration that could not be restored by
	// the deadline still persists every known id, and the friction names
	// it.
	outcomes, capErr := s.endCollaborations(s.collabDeadline())
	if capErr != nil {
		s.frictionf("job: the supervisor cannot enumerate the collaborations: " + capErr.Error())
		s.collabUnenumerated = true
	}
	s.collabEnded = true
	if err := s.persistCollabOutcomes(outcomes); err != nil {
		return 0, true, err
	}
	// The terminal event lands at most once: a crashed earlier pass may
	// have appended it already, and the restart converges without a
	// duplicate.
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		return 0, true, err
	}
	events, err := readEvents(jobDir)
	if err != nil {
		return 0, true, err
	}
	if !hasTipo(events, "terminal") {
		refs := map[string]string{"exit": strconv.Itoa(exit)}
		if motivo != "" {
			refs["motivo"] = motivo
		}
		if _, err := s.Store.Append(s.ID, EventIn{Tipo: "terminal", Refs: refs}); err != nil {
			return 0, true, err
		}
	}
	if _, err := s.Store.Transition(s.ID, status); err != nil {
		return 0, true, err
	}
	// The job is terminal: a cancel still pending (it stays pending through
	// the stop so a restart finds it) has nothing left to deliver.
	s.retirePendingCancel()
	// The terminal events wake the dispatcher too; the worker drains
	// before the release and the workspace close.
	s.finishWakes()
	s.finalize(status)
	return exit, true, nil
}

// frictionUnknown reports an unknown orchestrator state to friction, once
// per state.
func (s *Supervisor) frictionUnknown(state string) {
	if s.frictioned == nil {
		s.frictioned = map[string]bool{}
	}
	if s.frictioned[state] || s.Friction == nil {
		return
	}
	s.frictioned[state] = true
	s.Friction("job: the job orchestrator reports an unknown state '" + state + "'")
}

// fail records the failure and returns its store error: an unexpected
// store failure (the state read, or the crossing write of a blocked exit)
// exits through Run with the conservative failed code instead of being
// retried, and the next supervisor start converges. From blocked the
// failure exit settles the blocked interval and lands the failed terminal
// intent in the same locked write as the blocked -> finishing crossing
// (the contract's atomic crossing): the blocked tail goes to the blocked
// clock and the anchor moves to the crossing, so a hard stop at the write
// leaves either the pre-crossing blocked state (the tail still in flight,
// no intent) or the committed finishing state (settled, re-anchored, with
// the intent) — never the separate Record + Fail writes, which left the
// blocked tail on neither clock. Any other status
// records the intent first, best effort, and the Store.Fail walk runs
// from that status as before.
func (s *Supervisor) fail(motivo string, exit int) error {
	intent := &TerminalIntent{Status: StatusFailed, Motivo: motivo, Exit: exit}
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		// The status is unknown, so the unsettled walk must not run.
		s.frictionf("job: the supervisor could not record the failure")
		return err
	}
	if snap.State.Status == StatusBlocked {
		// The failure exit from blocked: the settle, the re-anchor and the
		// intent land with the crossing in one write; the Store.Fail walk
		// then runs from finishing.
		if err := s.settleAndTransitionWith(snap.State, StatusFinishing, true, func(st *State) { st.TerminalIntent = intent }); err != nil {
			// The job stays blocked and unsettled: the failure walk refused
			// to run from a state it could not cross, and the restart
			// converges on the still-blocked job.
			s.frictionf("job: the supervisor could not record the failure")
			return err
		}
	} else {
		// The terminal intent, best effort: on error, friction and continue
		// — the failure still has to be recorded.
		if _, err := s.Store.Record(s.ID, func(st *State) { st.TerminalIntent = intent }); err != nil {
			s.frictionf("job: the supervisor could not record the terminal intent")
		}
	}
	s.finalSync(StatusFailed)
	// The collaboration capture, persisted before the failure walk; an
	// enumeration that could not be restored by the deadline still
	// persists every known id, and the friction names it.
	outcomes, capErr := s.endCollaborations(s.collabDeadline())
	if capErr != nil {
		s.frictionf("job: the supervisor cannot enumerate the collaborations: " + capErr.Error())
		s.collabUnenumerated = true
	}
	s.collabEnded = true
	if err := s.persistCollabOutcomes(outcomes); err != nil {
		s.frictionf("job: the supervisor could not persist the collaboration outcomes")
	}
	if _, err := s.Store.Fail(s.ID, motivo, exit); err != nil {
		if s.Friction != nil {
			s.Friction("job: the supervisor could not record the failure")
		}
	} else {
		// The job is terminal: a pending cancel has nothing left to deliver.
		s.retirePendingCancel()
	}
	// The terminal events wake the dispatcher too; the worker drains
	// before the release and the workspace close.
	s.finishWakes()
	s.finalize(StatusFailed)
	return nil
}
