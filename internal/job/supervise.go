package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	// the kind the stop path ends with (canceled, timeout) and stopDeadline
	// its Clock.Monotonic deadline; warned guards the single
	// timeout_warning; budgetBase is the virtual Monotonic base the budget
	// counts from, captured once on the first budget check.
	pushNow       bool
	stopping      string
	stopDeadline  int64
	warned        bool
	budgetBase    int64
	budgetBaseSet bool
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
		Store:    store,
		ID:       id,
		Ops:      selfCLI{Exe: selfExe, Dir: snap.State.Dir, Env: env},
		Clock:    store.clock(),
		Friction: friction,
		Herdr:    newHerdrCLI(env, friction),
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
		return code, nil
	}
	// The job lane (and its max_workers bump) is set once, before the
	// orchestrator is recorded; a restarted supervisor skips it, so the bump
	// is never applied twice.
	if snap, err := s.Store.Snapshot(s.ID); err != nil {
		return ExitFailed, err
	} else if snap.State.Orchestrator == "" {
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
// state every poll for at most 60 s of polls: a terminal state returns its
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
			if snap.State.Status == StatusRunning {
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

// ensureOrchestrator spawns the job orchestrator when it is not recorded yet
// (restarts reuse the recorded name) and appends the worker_spawned event.
// done is true when the spawn failed (the job already failed); err is a
// store failure.
func (s *Supervisor) ensureOrchestrator() (string, bool, error) {
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil {
		return "", false, err
	}
	if snap.State.Orchestrator != "" {
		return snap.State.Orchestrator, false, nil
	}
	name, err := s.Ops.SpawnOrchestrator()
	if err != nil {
		s.fail("job: cannot spawn the job orchestrator", ExitHerdr)
		return "", true, nil
	}
	// TODO(DJA-194): a crash between the spawn and this record leaves the
	// orchestrator unrecorded: the restarted supervisor spawns a second
	// one. Recording the spawn receipt before the dispatch, or recovering
	// the pane, closes the window; the design lands with the release slice.
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
// a rejected push blocks the job before the orchestrator is read), then the
// job budget, which decides the stop path's end while a stop is running,
// and the job orchestrator when no stop is running.
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
		return s.checkBudget()
	}
	if code, done, err := s.checkBudget(); err != nil || done {
		return code, done, err
	}
	if s.stopping != "" {
		// checkBudget started the stop at 100 % of the budget this tick;
		// the stop path decides from here on.
		return s.checkBudget()
	}
	return s.checkOrchestrator()
}

// checkOrchestrator maps one status read of the job orchestrator to the
// job's transitions, events and outcome. The gone streak counts consecutive
// gone-family observations and drops on any observation that shows the
// orchestrator alive; the error streak counts consecutive failed reads and
// drops on any read that succeeds. A done state without a readable report
// counts like a gone observation.
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
		return s.unblockOrchestrator(st)
	case "blocked", "question":
		motivo := "dialog"
		if state == "question" {
			motivo = "question"
		}
		return s.blockOrchestrator(st, st.Orchestrator, motivo)
	case "quota":
		return s.blockOrchestrator(st, st.Orchestrator, "quota")
	case "provider-error", "capacity":
		return s.blockOrchestrator(st, st.Orchestrator, "provider_error")
	case "done":
		return s.onOrchestratorDone(st, st.Orchestrator, reportPath)
	case "gone", "settled-no-report", "not-received", "unavailable":
		s.goneStreak++
		if s.goneStreak >= 3 {
			s.fail("job: the job orchestrator ended without a report", ExitFailed)
			return ExitFailed, true, nil
		}
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
// state.
func (s *Supervisor) blockOrchestrator(st State, name, motivo string) (int, bool, error) {
	if st.Status != StatusRunning {
		return 0, false, nil
	}
	if _, err := s.Store.Transition(s.ID, StatusBlocked); err != nil {
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
// to running, the stored motivo clears, and the unblocked event lands.
func (s *Supervisor) unblockOrchestrator(st State) (int, bool, error) {
	if st.Status != StatusBlocked || st.Motivo == nil {
		return 0, false, nil
	}
	if !orchestratorMotivos[*st.Motivo] {
		return 0, false, nil
	}
	if _, err := s.Store.Transition(s.ID, StatusRunning); err != nil {
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
		// blocked, until the decision lands; the loop keeps running.
		if _, err := s.Store.Transition(s.ID, StatusBlocked); err != nil {
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
	if _, err := s.Store.Fail(s.ID, motivo, exit); err != nil && s.Friction != nil {
		s.Friction("job: the supervisor could not record the failure")
	}
	// The terminal events wake the dispatcher too; the worker drains
	// before the release and the workspace close.
	s.finishWakes()
	s.finalize(StatusFailed)
}
