package job

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// actor_session.go makes the job workspace's own session file carry the
// job's team and state root for the agents that run there. The job start
// writes the team to <job store root>/<workspace>/session.conf, and the
// supervisor's team commands read it through the starter's selection. A
// job orchestrator, or a worker it spawns, runs with the workspace's own
// environment (the supervisor's original one) and looks its session file
// up under the state root that environment resolves before any session is
// read. When that is another file (a custom state_dir on either side),
// the supervisor writes it as the start's session pairs plus
// state_dir=<job store root>: the existing key moves the agent's state
// root, and with it its roster, team commands and job lookup, to the
// job's store. No other key, file kind or environment value is involved.

// maxSessionBytes caps the start's session file the pin copies.
const maxSessionBytes = 64 << 10

// errActorSession is the fixed failure of a pin the job needs but cannot
// make; the reason goes to friction.
var errActorSession = errUsage("job: cannot pin the job workspace session")

// pinActorSession writes the workspace environment's session file when it
// is not the start's one; a privately launched supervisor only, after the
// job lane and before the orchestrator spawn. Nothing to pin is nil. A pin
// the job needs that cannot be encoded, read, written or read back is
// errActorSession (the reason in friction): the caller fails the job
// before any agent runs with a session that would miss its team and
// state.
func (s *Supervisor) pinActorSession() error {
	if !s.PrivateLaunch || s.actorEnv == nil {
		return nil
	}
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil || snap.State.Dir == "" || !validWorkspaceID(snap.State.WorkspaceID) {
		s.frictionf("job: the job workspace session cannot be located")
		return errActorSession
	}
	workspace := snap.State.WorkspaceID
	actorRoot := core.SessionStateRootPath(s.actorEnv, snap.State.Dir)
	if storeIdentityPath(actorRoot) == storeIdentityPath(s.Store.Root) {
		return nil
	}
	if !sessionStateDirValue(s.Store.Root) {
		s.frictionf("job: the job state root cannot be written in a session file (an absolute path without '#' or line breaks)")
		return errActorSession
	}
	// Inside the checkout the pin may only live in the default state
	// directory the job prepare excludes from git (/.herdr-soho/): anywhere
	// else in the checkout it could be committed with the job's work.
	checkout := storeIdentityPath(platform.StateProjectRoot(s.actorEnv, snap.State.Dir))
	if pinned := storeIdentityPath(actorRoot); withinOrSame(checkout, pinned) && pinned != filepath.Join(checkout, ".herdr-soho") {
		s.frictionf("job: the job workspace session would be inside the checkout")
		return errActorSession
	}
	if core.StatePathInSkill(s.actorEnv, actorRoot) != "" {
		s.frictionf("job: the job workspace session would be inside the skill")
		return errActorSession
	}
	data, ok, err := readPrivateFile(filepath.Join(s.Store.Root, workspace, "session.conf"), maxSessionBytes)
	if err != nil || !ok {
		s.frictionf("job: the job session file is unreadable")
		return errActorSession
	}
	body := pinnedSessionBody(string(data), s.Store.Root)
	dir := filepath.Join(actorRoot, workspace)
	if err := os.MkdirAll(dir, 0o700); err != nil || !exactDirectory(actorRoot) || !exactDirectory(dir) {
		s.frictionf("job: the job workspace session directory is not a plain directory")
		return errActorSession
	}
	file := filepath.Join(dir, "session.conf")
	if err := writeAtomic0600(file, []byte(body)); err != nil {
		s.frictionf("job: the job workspace session cannot be written")
		return errActorSession
	}
	// Read back what the agents will read: the same bytes, at its own
	// path, and the existing config parser resolving state_dir to the
	// store root.
	back, ok, err := readPrivateFile(file, maxSessionBytes)
	if err != nil || !ok || string(back) != body || !sessionNamesRoot(file, s.Store.Root) {
		s.frictionf("job: the job workspace session does not read back")
		return errActorSession
	}
	return nil
}

// pinnedSessionBody is the start's session text without any state_dir
// line, plus state_dir="<root>" as its last line: the config parser strips
// one pair of quotes, so spaces at either end and quotes inside the path
// survive.
func pinnedSessionBody(text, root string) string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if key, _, found := strings.Cut(line, "="); found && core.NormalizeKey(key) == "state_dir" {
			continue
		}
		lines = append(lines, line)
	}
	lines = append(lines, `state_dir="`+root+`"`)
	return strings.Join(lines, "\n") + "\n"
}

// sessionStateDirValue reports whether root can be one session file line:
// absolute, without '#' (the parser cuts a comment there, quoted or not)
// and without line breaks.
func sessionStateDirValue(root string) bool {
	return filepath.IsAbs(root) && !strings.ContainsAny(root, "#\n\r")
}

// sessionNamesRoot reports whether the config parser reads file's
// state_dir as exactly root.
func sessionNamesRoot(file, root string) bool {
	pairs, err := core.ReadConfigFilePairs(file)
	if err != nil {
		return false
	}
	value, found := "", false
	for _, pair := range pairs {
		if pair.Key == "state_dir" {
			value, found = pair.Value, true
		}
	}
	return found && value == root
}

// storeIdentityPath is the real path of a state root; a root that does
// not exist yet is its nearest existing ancestor's real path joined with
// the rest.
func storeIdentityPath(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	rest := ""
	for dir := abs; ; {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}
