package job

import (
	"encoding/json"
	"regexp"
	"sort"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// ExitHerdr is a Herdr failure in the job family: a workspace create or
// close, an unreachable server, or a refused or failed pane command.
const ExitHerdr = 4

const defaultHerdrTimeout = 30 * time.Second

// herdrWorkspaces is the job's view of Herdr workspaces. The production
// adapter shells out to the herdr CLI; tests use an in-memory fake.
type herdrWorkspaces interface {
	Create(cwd, label string, env map[string]string) (workspaceID, rootPaneID string, err error)
	Close(workspaceID string) error
	ServerReachable() bool
}

// herdrPanes is the job's view of Herdr panes.
type herdrPanes interface {
	Run(paneID string, argv []string) error
}

// herdrCLI implements herdrWorkspaces and herdrPanes through the herdr CLI,
// every call via runStep (argv only, context deadline and WaitDelay, and an
// error that never carries subprocess output). A zero Timeout means 30 s. A
// nil Friction drops friction messages.
type herdrCLI struct {
	Env      platform.Env
	Timeout  time.Duration
	Friction func(message string)
}

func newHerdrCLI(env platform.Env, friction func(string)) *herdrCLI {
	return &herdrCLI{Env: env, Friction: friction}
}

var (
	_ herdrWorkspaces = (*herdrCLI)(nil)
	_ herdrPanes      = (*herdrCLI)(nil)
)

func (c *herdrCLI) timeout() time.Duration {
	if c.Timeout <= 0 {
		return defaultHerdrTimeout
	}
	return c.Timeout
}

func herdrExit(msg string) error {
	return &ExitError{Code: ExitHerdr, Msg: msg}
}

// Create opens the job's Herdr workspace:
// `herdr workspace create --cwd <cwd> --label <label> [--env KEY=VALUE ...]
// --no-focus`, with the --env pairs sorted by key. A non-zero exit, a
// timeout, invalid JSON, or a missing id or pane is exit 4.
func (c *herdrCLI) Create(cwd, label string, env map[string]string) (string, string, error) {
	args := []string{"workspace", "create", "--cwd", cwd, "--label", label}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--env", key+"="+env[key])
	}
	args = append(args, "--no-focus")
	out, err := runStep("herdr", args, c.timeout(), c.Env)
	if err != nil {
		return "", "", herdrExit("job: herdr workspace create failed")
	}
	workspaceID, rootPaneID, ok := parseWorkspaceCreateResult(out)
	if !ok {
		// The raw envelope only goes to friction, sanitized and cut; it
		// never reaches stdout, stderr, or the error message.
		message := "job: herdr workspace create returned an unknown result: " + cutFriction(core.FrictionSafe(out))
		if c.Friction != nil {
			c.Friction(message)
		}
		return "", "", herdrExit("job: herdr workspace create returned an unknown result")
	}
	return workspaceID, rootPaneID, nil
}

// parseWorkspaceCreateResult reads the workspace id from
// result.workspace.workspace_id, else result.workspace_id, and the root pane
// from result.root_pane.pane_id, else result.workspace.root_pane.pane_id,
// else result.tab.root_pane.pane_id (the same fallback idea as
// herdr.TabCreate).
//
// TODO(DJA-194): verify the herdr workspace create result shape on a real server (premise 1).
func parseWorkspaceCreateResult(out string) (string, string, bool) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return "", "", false
	}
	result, ok := doc["result"].(map[string]any)
	if !ok {
		return "", "", false
	}
	workspace, _ := result["workspace"].(map[string]any)
	tab, _ := result["tab"].(map[string]any)
	workspaceID := herdrMapString(workspace, "workspace_id")
	if workspaceID == "" {
		workspaceID = herdrString(result["workspace_id"])
	}
	rootPaneID := herdrRootPaneID(result)
	if rootPaneID == "" {
		rootPaneID = herdrRootPaneID(workspace)
	}
	if rootPaneID == "" {
		rootPaneID = herdrRootPaneID(tab)
	}
	if workspaceID == "" || rootPaneID == "" {
		return "", "", false
	}
	return workspaceID, rootPaneID, true
}

// Close ends the job's Herdr workspace. An empty id refuses with exit 2
// before anything runs.
func (c *herdrCLI) Close(workspaceID string) error {
	if workspaceID == "" {
		return errUsage("job: invalid workspace id")
	}
	if _, err := runStep("herdr", []string{"workspace", "close", workspaceID}, c.timeout(), c.Env); err != nil {
		return herdrExit("job: herdr workspace close failed")
	}
	return nil
}

// ServerReachable queries only `herdr workspace list` and reports true when
// it exits 0 and the stdout parses as a JSON object with a result key. It
// never runs `herdr server` or any other command that could start a
// server.
//
// TODO(DJA-194): verify on Windows that an SSH session reaches the desktop-session server and that workspace list never starts one (premise 1).
func (c *herdrCLI) ServerReachable() bool {
	out, err := runStep("herdr", []string{"workspace", "list"}, c.timeout(), c.Env)
	if err != nil {
		return false
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return false
	}
	_, ok := doc["result"]
	return ok
}

// safePaneRunArg matches one `herdr pane run` argument: letters, digits,
// and . _ / : \ = -; an empty argument matches nothing.
var safePaneRunArg = regexp.MustCompile(`^[A-Za-z0-9._/:\\=-]+$`)

// Run starts an argv in an existing pane:
// `herdr pane run <paneID> <argv...>`. It refuses an empty paneID or any
// empty or unsafe argument with exit 4 before anything runs, because
// whether pane run re-reads the command through the pane's shell is
// unverified.
//
// TODO(DJA-194): verify that herdr pane run passes argv to a pane at a shell prompt without shell re-interpretation (premise 3).
func (c *herdrCLI) Run(paneID string, argv []string) error {
	if !safePaneRunArg.MatchString(paneID) {
		return herdrExit("job: herdr pane run refused an unsafe argument")
	}
	args := make([]string, 0, len(argv)+3)
	args = append(args, "pane", "run", paneID)
	for _, arg := range argv {
		if !safePaneRunArg.MatchString(arg) {
			return herdrExit("job: herdr pane run refused an unsafe argument")
		}
		args = append(args, arg)
	}
	if _, err := runStep("herdr", args, c.timeout(), c.Env); err != nil {
		return herdrExit("job: herdr pane run failed")
	}
	return nil
}

// herdrString reports value as a string; anything else is the empty string.
func herdrString(value any) string {
	s, _ := value.(string)
	return s
}

func herdrMapString(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	return herdrString(obj[key])
}

func herdrRootPaneID(obj map[string]any) string {
	root, ok := obj["root_pane"].(map[string]any)
	if !ok {
		return ""
	}
	return herdrMapString(root, "pane_id")
}

// cutFriction caps a friction fragment at 2000 bytes.
func cutFriction(s string) string {
	if len(s) > 2000 {
		return s[:2000]
	}
	return s
}
