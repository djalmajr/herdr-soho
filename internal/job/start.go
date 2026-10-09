package job

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// start.go is the non-dry-run `job start`: it probes the Herdr server,
// prepares the checkout and worktree (slice 5), records the job, creates
// the job workspace, writes the team session file, launches the supervisor
// in the workspace's first pane, and moves the job to running. It stops at
// the first failure; a failure after the job is recorded ends in
// Store.Fail, which writes the failed state, the events and the report.
// This slice only launches `job supervise`; the supervisor itself is the
// next slice.

// afterWorkspaceCreate is a test hook: it runs after Herdr.Create succeeds
// and before the workspace id is recorded. Tests point it at a panic to
// simulate a crash in that window and restore it with t.Cleanup.
var afterWorkspaceCreate func()

// StartRequest is the validated input for one non-dry-run job start. The
// CLI already validated the id, the repo and the base regexes and the
// machine label; Prepare and Store re-validate what they own.
type StartRequest struct {
	ID         string
	Org        string
	Repo       string
	Base       string
	Mode       string
	Brief      []byte
	TimeoutMin int
	Team       Team
}

// Starter composes the non-dry-run job start. Every step runs against the
// injected Preparer, herdrWorkspaces and herdrPanes, so tests substitute
// fakes for the herdr CLI.
type Starter struct {
	Env         platform.Env
	Machine     Machine
	Preparer    Preparer
	Herdr       herdrWorkspaces
	Panes       herdrPanes
	SelfExe     string
	StateRoot   func(checkout string) string
	Friction    func(string)
	RawFriction func(string)
}

// NewStarter wires the production starter: the machine's Preparer with the
// default production clone, and one herdr CLI for both the workspace and
// the pane calls. The raw friction sink carries raw subprocess output
// (the unknown create envelope) to the friction log only; a nil sink drops
// it.
func NewStarter(env platform.Env, machine Machine, selfExe string, stateRoot func(string) string, friction func(string), rawFriction func(string)) Starter {
	herdr := newHerdrCLI(env, friction)
	herdr.RawFriction = rawFriction
	return Starter{
		Env:         env,
		Machine:     machine,
		Preparer:    Preparer{Machine: machine, Env: env},
		Herdr:       herdr,
		Panes:       herdr,
		SelfExe:     selfExe,
		StateRoot:   stateRoot,
		Friction:    friction,
		RawFriction: rawFriction,
	}
}

// Start runs the non-dry-run job start and stops at the first failure. The
// returned store is nil when nothing was written; from store.Start on it
// is always returned so the caller can print the job's state line.
func (s Starter) Start(req StartRequest) (State, *Store, error) {
	// 1. The server must be reachable before anything is written or run;
	// the reachability query is the only subprocess of this step.
	if !s.Herdr.ServerReachable() {
		return State{}, nil, herdrExit("job: no reachable Herdr server")
	}

	// 2. A missing checkout is prepared (and cloned) now, before the job
	// directory is created: the job directory lives in the checkout.
	checkout := filepath.Join(s.Machine.ReposRoot, req.Org, req.Repo)
	preparedIt := false
	var prepared Prepared
	if _, err := os.Stat(checkout); os.IsNotExist(err) {
		var err error
		prepared, err = s.Preparer.Prepare(prepareRequest(req))
		if err != nil {
			// Nothing is written: the job directory cannot exist yet.
			return State{}, nil, err
		}
		preparedIt = true
	}

	// 3. Record the job. A duplicate is returned as is; no other step
	// runs.
	store := Open(s.StateRoot(checkout))
	st, err := store.Start(req.ID, req.Brief)
	if err != nil {
		return st, store, err
	}
	if st.DuplicateOf != "" {
		return st, store, nil
	}

	// 4. The job is being prepared.
	if _, err = store.Transition(req.ID, StatusPreparing); err != nil {
		return State{}, store, s.failStored(store, req.ID, "", err)
	}
	if _, err = store.Append(req.ID, EventIn{Tipo: "preparing", Resumo: "preparing checkout and worktree"}); err != nil {
		return State{}, store, s.failStored(store, req.ID, "", err)
	}

	// 5. Prepare the checkout and worktree (already done in step 2).
	if !preparedIt {
		prepared, err = s.Preparer.Prepare(prepareRequest(req))
		if err != nil {
			_, _ = store.Fail(req.ID, err.Error(), prepareExitCode(err))
			return State{}, store, err
		}
	}

	// 6. Record the run facts; StartedAt comes from the store clock. This
	// slice computes no deadline: the timeout budget is enforced by the
	// supervisor slice.
	startedAt := formatTS(store.clock().Now())
	if _, err = store.Record(req.ID, func(st *State) {
		st.Checkout = prepared.Checkout
		st.Dir = prepared.Dir
		st.Branch = prepared.Branch
		st.Base = prepared.Base
		st.BaseSHA = prepared.BaseSHA
		st.Modo = runMode(req.Mode)
		st.TimeoutMin = req.TimeoutMin
		st.StartedAt = startedAt
	}); err != nil {
		return State{}, store, s.failStored(store, req.ID, "", err)
	}

	// 7. Create the job workspace in the worktree.
	workspaceID, rootPaneID, err := s.Herdr.Create(prepared.Dir, "job-"+req.ID, nil)
	if err != nil {
		_, _ = store.Fail(req.ID, err.Error(), herdrExitCode(err))
		return State{}, store, err
	}
	// The id is about to be joined into the session file path and passed
	// to `herdr workspace close`: it must be one safe path segment.
	// Otherwise nothing is recorded or written, the workspace is left
	// open (friction says so), and the job fails with the fixed message.
	// The raw id goes only to the raw friction sink, sanitized and cut.
	if !validWorkspaceID(workspaceID) {
		if s.RawFriction != nil {
			s.RawFriction("job: herdr workspace create returned an invalid workspace id: " + cutFriction(core.FrictionSafe(workspaceID)))
		}
		if s.Friction != nil {
			s.Friction("job: the Herdr workspace was left open: its id is invalid")
		}
		_, _ = store.Fail(req.ID, "job: herdr workspace create returned an invalid workspace id", ExitHerdr)
		return State{}, store, herdrExit("job: herdr workspace create returned an invalid workspace id")
	}
	// TODO(DJA-194): a crash in the window between Herdr.Create and the
	// workspace id record below leaves the job preparing, with the run
	// facts and an unrecorded Herdr workspace; supervisor recovery lands
	// with the supervisor slice.
	if afterWorkspaceCreate != nil {
		afterWorkspaceCreate()
	}
	if _, err = store.Record(req.ID, func(st *State) {
		st.WorkspaceID = workspaceID
		st.RootPane = rootPaneID
	}); err != nil {
		return State{}, store, s.failStored(store, req.ID, workspaceID, err)
	}

	// 8. Write the team into the workspace session file.
	if err = writeSessionConf(filepath.Join(s.StateRoot(checkout), workspaceID, "session.conf"), req.Team.Pairs); err != nil {
		s.closeToFriction(workspaceID)
		_, _ = store.Fail(req.ID, "job: cannot write the team session", ExitUsage)
		return State{}, store, errUsage("job: cannot write the team session")
	}

	// 9. Launch the supervisor in the workspace's first pane.
	if err = s.Panes.Run(rootPaneID, []string{s.SelfExe, "job", "supervise", "--id", req.ID}); err != nil {
		s.closeToFriction(workspaceID)
		_, _ = store.Fail(req.ID, err.Error(), herdrExitCode(err))
		return State{}, store, err
	}

	// 10. The job is running.
	st, err = store.Transition(req.ID, StatusRunning)
	if err != nil {
		return State{}, store, s.failStored(store, req.ID, workspaceID, err)
	}
	return st, store, nil
}

// prepareRequest maps the start request to the preparation input. The CLI
// already validated the machine label; Prepare re-validates with an absent
// maquina, which it accepts.
func prepareRequest(req StartRequest) PrepareRequest {
	return PrepareRequest{ID: req.ID, Org: req.Org, Repo: req.Repo, Base: req.Base, Mode: req.Mode}
}

// runMode records the mode as the contract spells it: worktree or
// workspace (an absent mode is worktree).
func runMode(mode string) string {
	if mode == "workspace" {
		return "workspace"
	}
	return "worktree"
}

// prepareExitCode is the exit code a preparation failure ends the job
// with: the code the error itself carries, else 22.
func prepareExitCode(err error) int {
	return exitCodeOr(err, ExitPrepare)
}

// herdrExitCode is the exit code a Herdr failure ends the job with: the
// code the error itself carries, else 4.
func herdrExitCode(err error) int {
	return exitCodeOr(err, ExitHerdr)
}

func exitCodeOr(err error, def int) int {
	var exit *ExitError
	if errors.As(err, &exit) && exit != nil {
		return exit.Code
	}
	return def
}

// failStored ends a recorded job after a storage error (a state, event or
// run-fact write failed): it closes the workspace when one was created and
// fails the job with exit 2, best effort, so every failure after the job is
// recorded still aims at a terminal state with report.json. The original
// error is returned.
func (s Starter) failStored(store *Store, id, workspaceID string, err error) error {
	if workspaceID != "" {
		s.closeToFriction(workspaceID)
	}
	if _, failErr := store.Fail(id, "job: cannot record the job state", ExitUsage); failErr != nil && s.Friction != nil {
		s.Friction("job: cannot record the job failure: " + failErr.Error())
	}
	return err
}

// closeToFriction closes the job workspace after a post-create failure;
// its error only goes to friction.
func (s Starter) closeToFriction(workspaceID string) {
	if err := s.Herdr.Close(workspaceID); err != nil && s.Friction != nil {
		s.Friction("job: cannot close the job workspace: " + err.Error())
	}
}

// writeSessionConf writes the team pairs to path atomically (temp file in
// the same directory + rename, mode 0600), creating the parent directory
// 0700: one key=value line per pair, in order, and nothing else.
func writeSessionConf(path string, pairs []TeamPair) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lines := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		lines = append(lines, pair.Key+"="+pair.Value)
	}
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	return writeAtomic0600(path, []byte(body))
}
