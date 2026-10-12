package job

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// actor_session.go makes the job's agents resolve the job's team and state
// root. The job start writes the team to <job store root>/<workspace>/
// session.conf, and the supervisor's team commands read it through the
// starter's selection. A job orchestrator, or a worker it spawns, runs with
// the workspace's own environment (the supervisor's original one): its user
// layer is that environment's, and it looks its session file up under the
// state root that environment resolves before any session is read.
//
// So a privately launched supervisor, after the job lane and before the
// orchestrator spawn:
//
//  1. snapshots into the start's session file the team settings the
//     starter's selection resolves for every role of the team's lanes
//     (kind, model, effort and approvals, on the lane or role keys that
//     already exist) and max_workers, where the session does not already
//     decide them: resolved values, so the layer rules (a lower layer's
//     model is discarded under a higher layer's kind) apply before the
//     snapshot, not after;
//  2. when the workspace environment's session file is another file (a
//     custom state_dir on either side), writes it as that session plus
//     state_dir="<job store root>": the existing key moves the agents'
//     state root, and with it their roster, team commands and job lookup;
//  3. resolves both views again, the starter's and the workspace
//     environment's, and requires the same team and state root.
//
// Any failure is errActorSession, before an agent is spawned. No other key
// (no args, credentials, machine policy or unrelated setting), file kind or
// environment value is involved.

// maxSessionBytes caps the session files the pin reads.
const maxSessionBytes = 64 << 10

// maxExcludeBytes caps the info/exclude file the pin reads.
const maxExcludeBytes = 1 << 20

// errActorSession is the fixed failure of a pin the job needs but cannot
// make; the reason goes to friction.
var errActorSession = errUsage("job: cannot pin the job workspace session")

// teamLaneRolesRE is a team lane's roles key (the job lane is the
// supervisor's own and is not part of the team).
var teamLaneRolesRE = regexp.MustCompile(`^lane\.([a-z][a-z0-9_-]*)\.roles$`)

// teamView is what an agent's spawn resolves for the team: per role its
// lane, kind, model, effort and approvals, and max_workers.
type teamView struct {
	roles      map[string]core.RoleSettings
	maxWorkers string
}

func (v teamView) equal(o teamView) bool { return v.diff(o) == "" }

// diff names the first setting v and o resolve differently ("" when none):
// the role and attribute, never a value.
func (v teamView) diff(o teamView) string {
	if v.maxWorkers != o.maxWorkers {
		return "max_workers"
	}
	roles := make([]string, 0, len(v.roles))
	for role := range v.roles {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	if len(v.roles) != len(o.roles) {
		return "roles"
	}
	for _, role := range roles {
		a := v.roles[role]
		b, ok := o.roles[role]
		switch {
		case !ok:
			return "role " + role
		case a.Lane != b.Lane:
			return "role " + role + " lane"
		case a.Kind != b.Kind:
			return "role " + role + " kind"
		case a.ModelSpec != b.ModelSpec:
			return "role " + role + " model"
		case a.Effort != b.Effort:
			return "role " + role + " effort"
		case a.Approvals != b.Approvals:
			return "role " + role + " approvals"
		}
	}
	return ""
}

func resolveTeamView(roles []string, ctx *core.Config, env platform.Env, dir string) teamView {
	view := teamView{roles: map[string]core.RoleSettings{}, maxWorkers: core.Cfg(ctx, "max_workers", "", env)}
	for _, role := range roles {
		view.roles[role] = core.ResolveRoleSettings(role, ctx, env, dir, core.RoleFlags{})
	}
	return view
}

// pinActorSession runs steps 1 to 3 above; a supervisor without the
// private launch has nothing to pin and returns nil.
func (s *Supervisor) pinActorSession() error {
	if !s.PrivateLaunch || s.actorEnv == nil {
		return nil
	}
	snap, err := s.Store.Snapshot(s.ID)
	if err != nil || snap.State.Dir == "" || !validWorkspaceID(snap.State.WorkspaceID) {
		s.frictionf("job: the job workspace session cannot be located")
		return errActorSession
	}
	workspace, dir := snap.State.WorkspaceID, snap.State.Dir
	data, ok, err := readPrivateFile(filepath.Join(s.Store.Root, workspace, "session.conf"), maxSessionBytes)
	if err != nil || !ok {
		s.frictionf("job: the job session file is unreadable")
		return errActorSession
	}
	pairs := core.ConfigTextPairs(data)
	roles := teamRoles(pairs)
	// The environment of an agent in the job workspace: the supervisor's
	// own, with the job workspace's id (the session layer's key).
	agentEnv := s.actorEnv.Clone()
	agentEnv["HERDR_WORKSPACE_ID"] = workspace

	// 1. The starter's team, resolved, into the start's session file.
	starter := core.LoadConfigFor(agentEnv, dir, s.launchSel)
	if storeIdentityPath(core.StateRootPath(&starter, agentEnv, dir)) != storeIdentityPath(s.Store.Root) {
		s.frictionf("job: the starter's selection does not resolve the job state root")
		return errActorSession
	}
	want := resolveTeamView(roles, &starter, agentEnv, dir)
	body := appendSessionLines(string(data), teamPins(pairs, want))
	if body != string(data) {
		if !s.writeSessionFile(s.Store.Root, workspace, body, excludeLine{}, false) {
			return errActorSession
		}
		again := core.LoadConfigFor(agentEnv, dir, s.launchSel)
		if d := resolveTeamView(roles, &again, agentEnv, dir).diff(want); d != "" {
			s.frictionf("job: the team snapshot would change the supervisor's team (" + d + ")")
			return errActorSession
		}
	}

	// 2. The workspace environment's own session file.
	actorRoot := core.SessionStateRootPath(s.actorEnv, dir)
	if storeIdentityPath(actorRoot) != storeIdentityPath(s.Store.Root) {
		if !sessionStateDirValue(s.Store.Root) {
			s.frictionf("job: the job state root cannot be written in a session file (an absolute path without '#' or line breaks)")
			return errActorSession
		}
		if core.StatePathInSkill(s.actorEnv, actorRoot) != "" {
			s.frictionf("job: the job workspace session would be inside the skill")
			return errActorSession
		}
		exclude, ok := s.pinExclude(actorRoot, workspace, dir)
		if !ok {
			return errActorSession
		}
		if !s.writeSessionFile(actorRoot, workspace, pinnedSessionBody(body, s.Store.Root), exclude, true) {
			return errActorSession
		}
	}

	// 3. What the agents will resolve.
	actor := core.LoadConfig(agentEnv, dir)
	if storeIdentityPath(core.StateRootPath(&actor, agentEnv, dir)) != storeIdentityPath(s.Store.Root) {
		s.frictionf("job: the job's agents would not resolve the job state root")
		return errActorSession
	}
	if d := resolveTeamView(roles, &actor, agentEnv, dir).diff(want); d != "" {
		s.frictionf("job: the job's agents would not resolve the job team (" + d + ")")
		return errActorSession
	}
	return nil
}

// teamRoles are the roles of the session's team lanes (every lane but the
// job lane), in order, once each.
func teamRoles(pairs []core.ConfigPair) []string {
	var roles []string
	seen := map[string]bool{}
	for _, pair := range pairs {
		m := teamLaneRolesRE.FindStringSubmatch(pair.Original)
		if m == nil || m[1] == "job" {
			continue
		}
		for _, role := range strings.Split(pair.Value, ",") {
			role = strings.TrimSpace(role)
			if role != "" && !seen[role] {
				seen[role] = true
				roles = append(roles, role)
			}
		}
	}
	return roles
}

// teamPins are the key=value lines that make the session decide each
// resolved team setting it does not decide yet: a value the session
// already gives (from its lane or role key) is left alone; a lane value
// from a lower layer is pinned on that lane key, any other source on the
// role key; an empty value (nothing resolved it) is not pinned. max_workers
// is pinned when the session does not set it.
func teamPins(pairs []core.ConfigPair, view teamView) []string {
	inSession := map[string]bool{}
	for _, pair := range pairs {
		inSession[pair.Original] = true
	}
	var lines []string
	add := func(key, value string) {
		if value == "" || inSession[key] {
			return
		}
		inSession[key] = true
		lines = append(lines, key+"="+value)
	}
	roles := make([]string, 0, len(view.roles))
	for role := range view.roles {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		rs := view.roles[role]
		for _, attr := range []struct{ name, value, from string }{
			{"kind", rs.Kind, rs.KindFrom},
			{"model", rs.ModelSpec, rs.ModelFrom},
			{"effort", rs.Effort, rs.EffortFrom},
			{"approvals", rs.Approvals, rs.ApprovalsFrom},
		} {
			if strings.HasSuffix(attr.from, "(session)") || attr.from == "flag" {
				continue
			}
			if rs.Lane != "" && strings.HasPrefix(attr.from, "lane ") {
				add("lane."+rs.Lane+"."+attr.name, attr.value)
				continue
			}
			add("role."+role+"."+attr.name, attr.value)
		}
	}
	add("max_workers", view.maxWorkers)
	return lines
}

// appendSessionLines is text with lines appended, one per line.
func appendSessionLines(text string, lines []string) string {
	if len(lines) == 0 {
		return text
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + strings.Join(lines, "\n") + "\n"
}

// pinnedSessionBody is the session text without any state_dir line, plus
// state_dir="<root>" as its last line: the config parser strips one pair
// of quotes, so spaces at either end and quotes inside the path survive.
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

// pinExclude decides the git-local exclude line the workspace session at
// <actorRoot>/<ws> needs. Outside the checkout, or under the checkout's
// .herdr-soho (the job prepare excludes it), none; under the checkout's
// .worktrees it is refused (a worktree has its own root, which a checkout
// exclude line does not reach); elsewhere in the checkout the line is
// /<path from the checkout>/<ws>/, refused when git already tracks a file
// there or the path holds a gitignore pattern character.
func (s *Supervisor) pinExclude(actorRoot, workspace, dir string) (excludeLine, bool) {
	checkout := storeIdentityPath(platform.StateProjectRoot(s.actorEnv, dir))
	pinned := filepath.Join(storeIdentityPath(actorRoot), workspace)
	if !withinOrSame(checkout, pinned) || withinOrSame(filepath.Join(checkout, ".herdr-soho"), pinned) {
		return excludeLine{}, true
	}
	if withinOrSame(filepath.Join(checkout, ".worktrees"), pinned) {
		s.frictionf("job: the job workspace session would be inside a job worktree")
		return excludeLine{}, false
	}
	rel, err := filepath.Rel(checkout, pinned)
	if err != nil || rel == "." || strings.ContainsAny(filepath.ToSlash(rel), "*?[]\\#!\n\r") || strings.TrimSpace(rel) != rel {
		s.frictionf("job: the job workspace session path cannot be excluded from git")
		return excludeLine{}, false
	}
	out, ok := boundedGit(s.actorEnv, checkout, "ls-files", "-z", "--", filepath.ToSlash(rel))
	if !ok || out != "" {
		s.frictionf("job: the job workspace session path is tracked by git or cannot be checked")
		return excludeLine{}, false
	}
	return excludeLine{checkout: checkout, line: "/" + filepath.ToSlash(rel) + "/"}, true
}

// excludeLine is one line for a checkout's local info/exclude; the zero
// value adds none.
type excludeLine struct {
	checkout string
	line     string
}

// writeSessionFile writes body to <root>/<workspace>/session.conf. Every
// refusal comes before anything is created: an existing root or workspace
// directory must be a plain directory at its own path and an existing
// session file a regular file at its own path (never a link, a FIFO or a
// directory). Then the exclude line, when one is given, is added to the
// checkout's local info/exclude; missing directories are created one
// level at a time and checked; the file is written atomically (0600) and
// read back at its own path, bounded and without following a link, as the
// same bytes (for the workspace pin, pinsRoot, bytes whose state_dir the
// config parser reads as the job store root); the directories are checked
// again.
func (s *Supervisor) writeSessionFile(root, workspace, body string, exclude excludeLine, pinsRoot bool) bool {
	dir := filepath.Join(root, workspace)
	file := filepath.Join(dir, "session.conf")
	if !plainOrMissing(root, true) || !plainOrMissing(dir, true) || !plainOrMissing(file, false) {
		s.frictionf("job: the job workspace session path holds a link or another kind of file")
		return false
	}
	if exclude.line != "" && !s.appendExclude(exclude.checkout, exclude.line) {
		return false
	}
	rootInfo, ok := ensurePlainDirectory(root)
	if !ok {
		s.frictionf("job: the job workspace session directory cannot be created")
		return false
	}
	dirInfo, ok := ensurePlainDirectory(dir)
	if !ok {
		s.frictionf("job: the job workspace session directory cannot be created")
		return false
	}
	if err := writeAtomic0600(file, []byte(body)); err != nil {
		s.frictionf("job: the job workspace session cannot be written")
		return false
	}
	back, ok, err := readPrivateFile(file, maxSessionBytes)
	if err != nil || !ok || string(back) != body || !sameEntry(root, rootInfo) || !sameEntry(dir, dirInfo) {
		s.frictionf("job: the job workspace session does not read back")
		return false
	}
	if value, set := sessionStateDir(back); pinsRoot && (!set || value != s.Store.Root) {
		s.frictionf("job: the job workspace session does not read back")
		return false
	}
	return true
}

// appendExclude adds line to the checkout's <checkout>/.git/info/exclude
// unless it is there: .git and info are plain directories at their own
// paths and exclude a regular file at its own path (read bounded, without
// following a link); the line is appended and read back.
func (s *Supervisor) appendExclude(checkout, line string) bool {
	info := filepath.Join(checkout, ".git", "info")
	file := filepath.Join(info, "exclude")
	fail := func() bool {
		s.frictionf("job: the checkout's local git exclude file cannot be updated")
		return false
	}
	if !exactDirectory(filepath.Join(checkout, ".git")) || !plainOrMissing(info, true) || !plainOrMissing(file, false) {
		return fail()
	}
	existing, _, err := readPrivateFile(file, maxExcludeBytes)
	if err != nil {
		return fail()
	}
	if excludeHas(existing, line) {
		return true
	}
	if _, ok := ensurePlainDirectory(info); !ok || !plainOrMissing(file, false) {
		return fail()
	}
	add := line + "\n"
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		add = "\n" + add
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fail()
	}
	_, werr := f.WriteString(add)
	if cerr := f.Close(); werr != nil || cerr != nil {
		return fail()
	}
	back, ok, err := readPrivateFile(file, maxExcludeBytes)
	if err != nil || !ok || !excludeHas(back, line) {
		return fail()
	}
	return true
}

// excludeHas reports whether data holds line as one of its lines.
func excludeHas(data []byte, line string) bool {
	for _, have := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(have) == line {
			return true
		}
	}
	return false
}

// sessionStateDir is the state_dir the config parser reads from session
// bytes (core.ConfigTextPairs: the parser every layer is read with, over
// the bytes already read; no second open).
func sessionStateDir(data []byte) (string, bool) {
	value, found := "", false
	for _, pair := range core.ConfigTextPairs(data) {
		if pair.Key == "state_dir" {
			value, found = pair.Value, true
		}
	}
	return value, found
}

// plainOrMissing reports whether path is absent, or present at its own
// path as a plain directory (dir) or a regular file (!dir).
func plainOrMissing(path string, dir bool) bool {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil {
		return false
	}
	if dir {
		return info.Mode().Type() == os.ModeDir
	}
	return info.Mode().IsRegular()
}

// ensurePlainDirectory returns the Lstat of path as a plain directory: an
// existing one must be a directory at its own path (no link, no reparse
// point); a missing one is created with Mkdir, one level at a time from
// its nearest existing ancestor (an existing ancestor is resolved as the
// path names it), and each created level is checked at its own path.
// Mkdir never follows a link at the path it creates.
func ensurePlainDirectory(path string) (os.FileInfo, bool) {
	info, err := os.Lstat(path)
	if err == nil {
		return info, info.Mode().Type() == os.ModeDir
	}
	if !os.IsNotExist(err) {
		return nil, false
	}
	parent := filepath.Dir(path)
	if parent == path {
		return nil, false
	}
	if parentInfo, err := os.Stat(parent); err != nil {
		if !os.IsNotExist(err) {
			return nil, false
		}
		if _, ok := ensurePlainDirectory(parent); !ok {
			return nil, false
		}
	} else if !parentInfo.IsDir() {
		return nil, false
	}
	if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
		return nil, false
	}
	info, err = os.Lstat(path)
	if err != nil || info.Mode().Type() != os.ModeDir {
		return nil, false
	}
	return info, true
}

// sameEntry reports whether path still names, at its own path, the entry
// before describes.
func sameEntry(path string, before os.FileInfo) bool {
	now, err := os.Lstat(path)
	return err == nil && os.SameFile(before, now) && now.Mode().Type() == before.Mode().Type()
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
