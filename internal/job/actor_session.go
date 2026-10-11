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

// pinActorSession writes the workspace environment's session file when it
// is not the start's one; a privately launched supervisor only, after the
// job lane and before the orchestrator spawn. A failure goes to friction
// and leaves the job running: the agent then misses the job truthfully,
// never into another store.
func (s *Supervisor) pinActorSession() {
	if !s.PrivateLaunch || s.actorEnv == nil {
		return
	}
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil || snap.State.Dir == "" || !validWorkspaceID(snap.State.WorkspaceID) {
		s.frictionf("job: the job workspace session cannot be pinned")
		return
	}
	workspace := snap.State.WorkspaceID
	actorRoot := core.SessionStateRootPath(s.actorEnv, snap.State.Dir)
	if storeIdentityPath(actorRoot) == storeIdentityPath(s.Store.Root) {
		return
	}
	if !sessionStateDirValue(s.Store.Root) {
		s.frictionf("job: the job state root cannot be named in a session file")
		return
	}
	// Inside the checkout the pin may only live in the default state
	// directory the job prepare excludes from git (/.herdr-soho/): anywhere
	// else in the checkout it could be committed with the job's work.
	checkout := storeIdentityPath(platform.StateProjectRoot(s.actorEnv, snap.State.Dir))
	if pinned := storeIdentityPath(actorRoot); withinOrSame(checkout, pinned) && pinned != filepath.Join(checkout, ".herdr-soho") {
		s.frictionf("job: the job workspace session would be inside the checkout")
		return
	}
	if core.StatePathInSkill(s.actorEnv, actorRoot) != "" {
		s.frictionf("job: the job workspace session would be inside the skill")
		return
	}
	data, ok, err := readPrivateFile(filepath.Join(s.Store.Root, workspace, "session.conf"), maxSessionBytes)
	if err != nil || !ok {
		s.frictionf("job: the job session file is unreadable")
		return
	}
	body := pinnedSessionBody(string(data), s.Store.Root)
	dir := filepath.Join(actorRoot, workspace)
	if err := os.MkdirAll(dir, 0o700); err != nil || !exactDirectory(dir) {
		s.frictionf("job: the job workspace session cannot be pinned")
		return
	}
	if err := writeAtomic0600(filepath.Join(dir, "session.conf"), []byte(body)); err != nil {
		s.frictionf("job: the job workspace session cannot be pinned")
	}
}

// pinnedSessionBody is the start's session text without any state_dir
// line, plus state_dir=root as its last line.
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
	lines = append(lines, "state_dir="+root)
	return strings.Join(lines, "\n") + "\n"
}

// sessionStateDirValue reports whether root survives a session file line
// as the same absolute state_dir: the config parser cuts at '#', splits at
// newlines, trims whitespace and strips one pair of quotes, and session
// set refuses tabs.
func sessionStateDirValue(root string) bool {
	if !filepath.IsAbs(root) || root != strings.TrimSpace(root) || strings.HasPrefix(root, `"`) || strings.HasSuffix(root, `"`) {
		return false
	}
	for i := 0; i < len(root); i++ {
		if root[i] < 0x20 || root[i] == 0x7f || root[i] == '#' {
			return false
		}
	}
	return core.ConfigValueOk("state_dir", root, platform.Env{}, "")
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
