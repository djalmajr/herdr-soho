// Package job stores ephemeral job state, events and reports at
// <stateRoot>/jobs/<id>, outside any workspace directory.
package job

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	StatusAccepted  = "accepted"
	StatusPreparing = "preparing"
	StatusRunning   = "running"
	StatusBlocked   = "blocked"
	StatusFinishing = "finishing"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusTimeout   = "timeout"
	StatusCanceled  = "canceled"
	StatusCollected = "collected"
	StatusClosed    = "closed"
)

// State is the jobs/<id>/state.json document. The fields from Checkout on
// are run facts recorded by the start slice; they are optional so a job
// without them serializes byte for byte as before.
type State struct {
	Schema            int     `json:"schema"`
	ID                string  `json:"id"`
	Status            string  `json:"status"`
	BriefSHA256       string  `json:"brief_sha256"`
	DecisionsAckedSeq int     `json:"decisions_acked_seq"`
	Motivo            *string `json:"motivo"`
	DuplicateOf       string  `json:"duplicate_of,omitempty"`
	Checkout          string  `json:"checkout,omitempty"`
	Dir               string  `json:"dir,omitempty"`
	Branch            string  `json:"branch,omitempty"`
	Base              string  `json:"base,omitempty"`
	BaseSHA           string  `json:"base_sha,omitempty"`
	Modo              string  `json:"modo,omitempty"`
	WorkspaceID       string  `json:"workspace_id,omitempty"`
	RootPane          string  `json:"root_pane,omitempty"`
	WorkspaceClosed   bool    `json:"workspace_closed,omitempty"`
	TimeoutMin        int     `json:"timeout_min,omitempty"`
	StartedAt         string  `json:"started_at,omitempty"`
	Orchestrator      string  `json:"orchestrator,omitempty"`
	DispatchedAt      string  `json:"dispatched_at,omitempty"`
}

// Store is the job state root (core.StateRootPath, not a workspace directory).
type Store struct {
	Root  string
	Clock Clock
}

func Open(stateRoot string) *Store {
	return &Store{Root: stateRoot}
}

// Dir is <stateRoot>/jobs/<id>. The id is the contract regex, and "." / ".."
// are refused so the directory cannot leave jobs/.
func Dir(stateRoot, id string) (string, error) {
	if !idPattern.MatchString(id) || id == "." || id == ".." {
		return "", errUsage("job: invalid id")
	}
	jobs := filepath.Join(stateRoot, "jobs")
	dir := filepath.Join(jobs, id)
	if filepath.Base(dir) != id || filepath.Clean(filepath.Dir(dir)) != filepath.Clean(jobs) {
		return "", errUsage("job: invalid id")
	}
	return dir, nil
}

// Start validates the brief, records the job, and returns the current state.
// The same canonical brief hash returns that state with DuplicateOf set.
// A different hash returns exit 20 and does not change the job.
func (s *Store) Start(id string, brief []byte) (State, error) {
	dir, err := Dir(s.Root, id)
	if err != nil {
		return State{}, err
	}
	doc, err := parseBrief(brief, id)
	if err != nil {
		return State{}, err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return State{}, err
	}
	var st State
	err = withLock(dir, func() error {
		existing, readErr := readState(dir)
		if readErr == nil {
			if existing.BriefSHA256 != doc.Hash {
				return &ExitError{Code: ExitBriefConflict, Msg: "job: id reused with a different brief"}
			}
			existing.DuplicateOf = existing.ID
			st = existing
			return nil
		}
		if !isCode(readErr, ExitNotFound) {
			return readErr
		}
		// Interrupted start: a published brief.json survives without
		// state.json. A different hash is a conflict that changes nothing;
		// the same hash resumes the publication instead of overwriting it.
		saved, err := os.ReadFile(filepath.Join(dir, "brief.json"))
		switch {
		case err == nil:
			sum := sha256.Sum256(saved)
			if hex.EncodeToString(sum[:]) != doc.Hash {
				return &ExitError{Code: ExitBriefConflict, Msg: "job: id reused with a different brief"}
			}
		case os.IsNotExist(err):
			if err = writeAtomic0600(filepath.Join(dir, "brief.json"), doc.Canonical); err != nil {
				return err
			}
		default:
			return fmt.Errorf("job: brief unreadable: %w", err)
		}
		if err := writeAtomic0600(filepath.Join(dir, "brief.md"), []byte(doc.Markdown)); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(dir, "control"), 0o700); err != nil {
			return err
		}
		events, err := readEvents(dir)
		if err != nil {
			return err
		}
		if !hasTipo(events, "accepted") {
			if _, err = appendEventFn(dir, s.clock().Now(), EventIn{Tipo: "accepted", Resumo: doc.Objetivo}); err != nil {
				return err
			}
		}
		st = State{Schema: 1, ID: id, Status: StatusAccepted, BriefSHA256: doc.Hash}
		return saveState(dir, st)
	})
	return st, err
}

var transitions = map[string]map[string]bool{
	StatusAccepted:  {StatusPreparing: true},
	StatusPreparing: {StatusRunning: true},
	StatusRunning:   {StatusBlocked: true, StatusFinishing: true},
	StatusBlocked:   {StatusRunning: true, StatusFinishing: true},
	StatusFinishing: {
		StatusDone: true, StatusFailed: true, StatusBlocked: true,
		StatusTimeout: true, StatusCanceled: true,
	},
	StatusDone:      {StatusCollected: true},
	StatusFailed:    {StatusCollected: true},
	StatusTimeout:   {StatusCollected: true},
	StatusCanceled:  {StatusCollected: true},
	StatusCollected: {StatusClosed: true},
}

// Transition moves a job along one allowed lifecycle edge. The closed edge
// runs the shared close checks (pending decisions, cleanup audit, report
// refresh); a transition into a terminal outcome publishes report.json in
// the same locked operation.
func (s *Store) Transition(id, to string) (State, error) {
	var st State
	err := s.withExisting(id, func(dir string) error {
		var err error
		st, err = readState(dir)
		if err != nil {
			return err
		}
		if st.Status == to && isTerminalOutcome(to) {
			// Idempotent retry of a transition whose report publish crashed:
			// the state is already the terminal outcome, so republish the
			// report and leave the state unchanged.
			return refreshReport(dir, st)
		}
		if !transitions[st.Status][to] {
			return errUsage("job: transition " + st.Status + " -> " + to + " is not allowed")
		}
		if to == StatusClosed {
			closed, closeErr := s.closeLocked(dir, st, false)
			st = closed
			return closeErr
		}
		// Leaving a terminal outcome republishes the report while the state
		// still carries it, so a missing report is built with the outcome as
		// its status instead of collected.
		if to == StatusCollected && isTerminalOutcome(st.Status) {
			if err = refreshReport(dir, st); err != nil {
				return err
			}
		}
		st.Status = to
		if err = saveState(dir, st); err != nil {
			return err
		}
		return s.refreshIfWrote(dir, st, true)
	})
	return st, err
}

// readDir resolves the job directory for a lock-free read. Reads open only
// existing files: they never create the lock, never open it for writing,
// and work when the job directory and its files are read-only. Writers keep
// withLock exactly for serialization.
func (s *Store) readDir(id string) (string, error) {
	dir, err := Dir(s.Root, id)
	if err != nil {
		return "", err
	}
	if _, err = os.Stat(filepath.Join(dir, "state.json")); err != nil {
		if os.IsNotExist(err) {
			return "", &ExitError{Code: ExitNotFound, Msg: "job: not found"}
		}
		return "", fmt.Errorf("job: state unreadable: %w", err)
	}
	return dir, nil
}

func (s *Store) withExisting(id string, fn func(dir string) error) error {
	dir, err := Dir(s.Root, id)
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(dir, "state.json")); err != nil {
		if os.IsNotExist(err) {
			return &ExitError{Code: ExitNotFound, Msg: "job: not found"}
		}
		return fmt.Errorf("job: state unreadable: %w", err)
	}
	return withLock(dir, func() error { return fn(dir) })
}

// Snapshot is the lock-free, write-free read projection for the CLI slice:
// the state, the event totals, the pending decision seqs (effective
// watermark, empty not nil) and the stored report (nil when absent).
// Unknown ids are ExitNotFound.
type Snapshot struct {
	State      State
	EventTotal int
	LastSeq    int
	Pending    []int
	Report     *Report
}

func (s *Store) Snapshot(id string) (Snapshot, error) {
	var snap Snapshot
	dir, err := s.readDir(id)
	if err != nil {
		return snap, err
	}
	st, err := readState(dir)
	if err != nil {
		return snap, err
	}
	events, err := readEvents(dir)
	if err != nil {
		return snap, err
	}
	snap.State = st
	snap.EventTotal = len(events)
	snap.LastSeq = lastSeq(events)
	snap.Pending = pendingDecisions(events, effectiveWatermark(st.DecisionsAckedSeq, events))
	rep, err := readReport(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return snap, err
		}
	} else {
		snap.Report = &rep
	}
	return snap, nil
}

func readState(dir string) (State, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, &ExitError{Code: ExitNotFound, Msg: "job: not found"}
		}
		return State{}, fmt.Errorf("job: state unreadable: %w", err)
	}
	var st State
	if err = json.Unmarshal(raw, &st); err != nil {
		return State{}, fmt.Errorf("job: state unreadable: %w", err)
	}
	st.DuplicateOf = ""
	return st, nil
}

func saveState(dir string, st State) error {
	st.Schema = 1
	st.DuplicateOf = ""
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic0600(filepath.Join(dir, "state.json"), raw)
}

// renameAtomic is the last step of an atomic replace. Tests point it at a
// failing function to simulate a crash after the temp file is written.
var renameAtomic = os.Rename

func writeAtomic0600(dest string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+"-*.tmp")
	if err != nil {
		return err
	}
	tmp := file.Name()
	cleanup := func() { _ = os.Remove(tmp) }
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		cleanup()
		return err
	}
	if err = file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return err
	}
	if err = file.Close(); err != nil {
		cleanup()
		return err
	}
	if err = renameAtomic(tmp, dest); err != nil {
		cleanup()
		return err
	}
	return os.Chmod(dest, 0o600)
}
