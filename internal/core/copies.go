package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// copiesHeader is the first line of the per-workspace copy registry,
// copies.tsv.
const copiesHeader = "path\towner\tpane\tcreated\tsource\torigin"

const (
	// copiesLockName sits next to the registry: the exclusive creation of
	// this file is the inter-process lock of the registry transactions.
	copiesLockName = "copies.tsv.lock"
	// New lock attempts every 50 ms, for at most CopiesLockTimeout.
	copiesLockPoll = 50 * time.Millisecond
	// A lock older than 60 s by mtime is taken as abandoned and removed.
	copiesLockStale = 60 * time.Second
)

// CopiesLockTimeout is how long a caller waits for the registry lock before
// giving up. Tests shorten it so they never wait the real 10 s.
var CopiesLockTimeout = 10 * time.Second

// CopiesLockedError is the registry lock timeout error; its message is the
// exact text the commands print with exit 4.
type CopiesLockedError struct {
	Path string
}

func (e *CopiesLockedError) Error() string {
	return fmt.Sprintf("copies: registry is locked by another herdr-soho (%s)", e.Path)
}

// LockCopies takes the inter-process copies.tsv lock: the exclusive
// creation (O_CREATE|O_EXCL) of copies.tsv.lock next to the registry, with
// the PID and the time inside. It retries every 50 ms until the timeout; a
// lock older than 60 s by mtime is abandoned and taken. It returns the
// unlock function (defer it, it removes the lock) or a *CopiesLockedError.
func LockCopies(stateDir string) (func(), error) {
	lockPath := filepath.Join(stateDir, copiesLockName)
	deadline := time.Now().Add(CopiesLockTimeout)
	for {
		err := takeCopiesLock(lockPath)
		if err == nil {
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if held, statErr := os.Stat(lockPath); statErr == nil && time.Since(held.ModTime()) > copiesLockStale {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, &CopiesLockedError{Path: lockPath}
		}
		time.Sleep(copiesLockPoll)
	}
}

func takeCopiesLock(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(f, "%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	return f.Close()
}

// CopyRow is one line of copies.tsv: a copy directory and who made it.
type CopyRow struct {
	Path    string // the canonical path
	Owner   string // the roster name, or "-"
	Pane    string // the pane id, or "-"
	Created string // ISO-8601 UTC
	Source  string // the copied origin, or "-"
	Origin  string // "mutation-copy" or "add"
}

func (r CopyRow) fields() []string {
	return []string{r.Path, r.Owner, r.Pane, r.Created, r.Source, r.Origin}
}

// CopiesFile returns the copy registry path inside the state dir.
func CopiesFile(stateDir string) string { return filepath.Join(stateDir, "copies.tsv") }

// ReadCopies reads the copy registry without the header, in file order. A
// registry that does not exist yet reads as no rows.
func ReadCopies(stateDir string) ([]CopyRow, error) {
	raw, err := platform.ReadTextFile(CopiesFile(stateDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []CopyRow{}, nil
		}
		return nil, err
	}
	out := []CopyRow{}
	for _, line := range strings.Split(raw, "\n") {
		if line == "" || line == copiesHeader {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 6 {
			return nil, fmt.Errorf("copies.tsv: malformed line %q", line)
		}
		out = append(out, CopyRow{Path: fields[0], Owner: fields[1], Pane: fields[2], Created: fields[3], Source: fields[4], Origin: fields[5]})
	}
	return out, nil
}

// WriteCopies rewrites the copy registry atomically (temporary file plus
// rename, like the other state files).
func WriteCopies(stateDir string, rows []CopyRow) error {
	lines := []string{copiesHeader}
	for _, row := range rows {
		lines = append(lines, strings.Join(row.fields(), "\t"))
	}
	return platform.AtomicWrite(CopiesFile(stateDir), strings.Join(lines, "\n")+"\n")
}

// UpsertCopies records row, replacing the line with the same path when it
// exists, so re-registering a path never duplicates it. The whole
// read-modify-write runs under the inter-process registry lock, so two
// concurrent registrations never lose a line.
func UpsertCopies(stateDir string, row CopyRow) error {
	unlock, err := LockCopies(stateDir)
	if err != nil {
		return err
	}
	defer unlock()
	rows, err := ReadCopies(stateDir)
	if err != nil {
		return err
	}
	for i, existing := range rows {
		if existing.Path == row.Path {
			rows[i] = row
			return WriteCopies(stateDir, rows)
		}
	}
	return WriteCopies(stateDir, append(rows, row))
}

// DropCopiesLines re-reads the registry under the inter-process lock and
// rewrites it without the lines whose path is in removed, keeping every
// other line, including lines added meanwhile. The slow removals happen
// outside the lock, by the caller.
func DropCopiesLines(stateDir string, removed map[string]bool) error {
	unlock, err := LockCopies(stateDir)
	if err != nil {
		return err
	}
	defer unlock()
	rows, err := ReadCopies(stateDir)
	if err != nil {
		return err
	}
	out := make([]CopyRow, 0, len(rows))
	for _, row := range rows {
		if !removed[row.Path] {
			out = append(out, row)
		}
	}
	return WriteCopies(stateDir, out)
}

// WorkspaceIDOrError is the non-dying variant of WorkspaceID for callers
// that must keep running when the workspace cannot be resolved: a missing
// herdr CLI or a failing pane current comes back as an error.
func WorkspaceIDOrError(env platform.Env) (string, error) {
	if id := env.Get("HERDR_WORKSPACE_ID"); id != "" {
		return id, nil
	}
	r := platform.RunCli("herdr", []string{"pane", "current", "--current"}, platform.RunOptions{Env: env, TimeoutMs: 30_000})
	if r.NotFound {
		return "", errors.New("herdr CLI not found in PATH")
	}
	if r.Status == nil || *r.Status != 0 {
		code := 0
		if r.Status != nil {
			code = *r.Status
		}
		return "", fmt.Errorf("herdr pane current failed (exit %d)", code)
	}
	v, err := jsonjs.Parse([]byte(r.Stdout))
	if err != nil {
		return "", errors.New("herdr pane current returned invalid JSON")
	}
	root, ok := v.(*jsonjs.Object)
	if !ok {
		return "", errors.New("herdr pane current returned a non-object result")
	}
	result, _ := root.Get("result")
	resultObject, ok := result.(*jsonjs.Object)
	if !ok {
		return "", errors.New("herdr pane current returned a non-object result")
	}
	pane, _ := resultObject.Get("pane")
	paneObject, ok := pane.(*jsonjs.Object)
	if !ok {
		return "", errors.New("herdr pane current returned a non-object pane")
	}
	id, ok := paneObject.Get("workspace_id")
	if !ok || id == nil {
		return "null", nil
	}
	if s, ok := id.(string); ok {
		return s, nil
	}
	switch id.(type) {
	case *jsonjs.Object, []any:
		return "", errors.New("herdr pane current returned an unsupported workspace_id type")
	}
	return jsString(id), nil
}

// StateDirResolved is the non-dying variant of StateDir for callers that
// must keep running when the state dir cannot be resolved or created (the
// mutation-copy registration). The state-inside-skill refusal and every I/O
// failure come back as an error, decided before anything is written.
func StateDirResolved(ctx *Config, env platform.Env, cwd string) (string, error) {
	if !Nowrite(env) {
		if skill := StateInSkill(ctx, env, cwd); skill != "" {
			return "", fmt.Errorf("the state dir '%s' would be inside the herdr-soho skill ('%s'); run herdr-soho from the project's directory (nothing was written)", StateRootPath(ctx, env, cwd), skill)
		}
	}
	workspace, err := WorkspaceIDOrError(env)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(StateRootPath(ctx, env, cwd), workspace)
	if Nowrite(env) {
		return dir, nil
	}
	for _, name := range []string{"briefs", "reports", "wait"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o777); err != nil {
			return "", err
		}
	}
	roster := filepath.Join(dir, "agents.tsv")
	if _, err := os.Stat(roster); os.IsNotExist(err) {
		if err := os.WriteFile(roster, []byte(rosterHeader), 0o666); err != nil {
			return "", err
		}
	}
	return dir, nil
}
