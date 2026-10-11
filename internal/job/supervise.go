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
	}); err != nil {
		return err
	}
	s.intervalBase = before
	s.intervalBaseSet = true
	return nil
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
			s.fail("job: cannot ensure the job lane", ExitHerdr)
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
// differences, so a moving clock cannot end the wait early.
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
				return 0, false, nil
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
	s.fail("job: supervisor started before the job was running", ExitFailed)
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
	s.fail("job: cannot establish the job orchestrator", ExitHerdr)
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
		s.fail("job: cannot spawn the job orchestrator", ExitHerdr)
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
		s.fail("job: the job brief is missing", ExitFailed)
		return ExitFailed, true, nil
	}
	brief = append(brief, orchestratorBriefSection(s.ID)...)
	path := filepath.Join(jobDir, "orchestrator-brief.md")
	if err := writeAtomic0600(path, brief); err != nil {
		return 0, false, err
	}
	if err := s.Ops.Dispatch(name, path, false); err != nil {
		s.fail("job: cannot dispatch the job orchestrator", ExitHerdr)
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
			s.fail("job: the job orchestrator ended without a report", ExitFailed)
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
	s.fail("job: the job orchestrator ended without a report", ExitFailed)
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
	if st.Status == StatusBlocked && st.Motivo != nil && *st.Motivo == "decisions pending" {
		// The outcome was already derived: the loop keeps waiting for the
		// decision to be resolved, and derives nothing again.
		return 0, false, nil
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		// A done state without a readable report counts like a gone one.
		s.goneStreak++
		if s.goneStreak >= 3 {
			s.fail("job: the job orchestrator ended without a report", ExitFailed)
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
	if _, err := s.Store.Transition(s.ID, StatusFinishing); err != nil {
		return 0, true, err
	}
	if partials == 0 {
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
		if err := s.settleAndTransition(st, StatusBlocked, false); err != nil {
			return 0, true, err
		}
		motivo := "decisions pending"
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
	motivo := "itens parciais"
	if _, err := s.Store.Record(s.ID, func(st *State) { st.Motivo = &motivo }); err != nil {
		return 0, true, err
	}
	return s.finishOutcome(StatusFailed, motivo, ExitFailed)
}

// finishOutcome appends the terminal event (refs.motivo when set,
// refs.exit), moves the job to the outcome — which publishes report.json —
// and finalizes: the final push and draft pull request, the complete
// report facts, the release, the cleanup event, and the workspace close.
func (s *Supervisor) finishOutcome(status, motivo string, exit int) (int, bool, error) {
	// The final push happens while the job is still finishing, so its events
	// precede the terminal event.
	s.finalSync(status)
	refs := map[string]string{"exit": strconv.Itoa(exit)}
	if motivo != "" {
		refs["motivo"] = motivo
	}
	if _, err := s.Store.Append(s.ID, EventIn{Tipo: "terminal", Refs: refs}); err != nil {
		return 0, true, err
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

// fail records the failure (event, stored motivo, walk to failed, terminal
// event, report) and never returns its error: the state is the truth, and
// a failure to record it goes to friction only. The failure finalizes like
// every terminal outcome: final push, report, release, cleanup, close.
func (s *Supervisor) fail(motivo string, exit int) {
	s.finalSync(StatusFailed)
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
}
