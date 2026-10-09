package job

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// team_ops.go composes the CLI's own team commands for the job supervisor:
// the lane it guarantees, the job orchestrator it spawns, dispatches,
// watches and releases. The production adapter runs this executable's
// subcommands as argv subprocesses behind runStepIn with the job worktree
// as working directory, so the CLI's rules stay the shared implementation.
// Its errors are fixed strings that name the step; subprocess output never
// reaches them.

const defaultTeamOpsTimeout = 120 * time.Second

// teamOps is the team surface the supervisor drives from the job worktree.
type teamOps interface {
	EnsureJobLane() error
	SpawnOrchestrator() (name string, err error)
	Dispatch(name, briefPath string, amend bool) error
	Status(name string) (state, reportPath string, err error)
	Release(name string) error
}

// selfCLI is the production teamOps: one herdr-soho executable, the job
// worktree as working directory, one bounded timeout per command (a zero
// Timeout is 120 s).
type selfCLI struct {
	Exe     string
	Dir     string
	Env     platform.Env
	Timeout time.Duration
}

func (c selfCLI) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return defaultTeamOpsTimeout
}

// step runs one self subprocess and maps any failure to the fixed error of
// the operation.
func (c selfCLI) step(op string, args ...string) (string, error) {
	out, err := runStepIn(c.Dir, c.Exe, args, c.timeout(), c.Env)
	if err != nil {
		return "", teamOpsError(op)
	}
	return out, nil
}

// teamOpsError is the fixed error of one failed operation; the subprocess
// output stays out of it.
func teamOpsError(op string) error {
	return &ExitError{Code: ExitHerdr, Msg: "job: cannot " + op}
}

// EnsureJobLane guarantees the lane the job orchestrator runs in: the lane
// role and its single pane, and — only when the config carries an explicit
// max_workers value (a source other than defaults) — a bump of the worker
// cap by one. It is idempotent: session set rewrites the same keys.
func (c selfCLI) EnsureJobLane() error {
	if _, err := c.step("ensure the job lane", "session", "set", "lane.job.roles", "job-orchestrator"); err != nil {
		return err
	}
	if _, err := c.step("ensure the job lane", "session", "set", "lane.job.panes", "1"); err != nil {
		return err
	}
	out, err := c.step("ensure the job lane", "config")
	if err != nil {
		return err
	}
	value, explicit, err := parseMaxWorkers(out)
	if err != nil {
		return teamOpsError("ensure the job lane")
	}
	if explicit {
		if _, err := c.step("ensure the job lane", "session", "set", "max_workers", strconv.Itoa(value+1)); err != nil {
			return err
		}
	}
	return nil
}

// parseMaxWorkers reads the max_workers row of the config output: the key,
// the value and the source are whitespace-separated, and the source names
// the layer the value came from. An absent row or a non-integer value is an
// error; explicit is true when the source is not defaults.
func parseMaxWorkers(output string) (value int, explicit bool, err error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "max_workers" {
			continue
		}
		n, parseErr := strconv.Atoi(fields[1])
		if parseErr != nil {
			return 0, false, errors.New("max_workers value is not an integer")
		}
		return n, fields[2] != "defaults", nil
	}
	return 0, false, errors.New("no max_workers row")
}

// SpawnOrchestrator spawns the job orchestrator in the job worktree and
// returns its agent name: the name field of the last JSON object the spawn
// prints. The real spawn indents the object over several lines, so the
// candidate is the last line that opens an object, joined to the end.
func (c selfCLI) SpawnOrchestrator() (string, error) {
	out, err := c.step("spawn the job orchestrator", "spawn", "job-orchestrator", "--cwd", c.Dir, "--approvals", "full", "--fresh")
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "{") {
			continue
		}
		object, err := parseJSONObject(strings.Join(lines[i:], "\n"))
		if err != nil {
			continue
		}
		name, _ := object["name"].(string)
		if name == "" {
			return "", teamOpsError("spawn the job orchestrator")
		}
		return name, nil
	}
	return "", teamOpsError("spawn the job orchestrator")
}

// Dispatch sends the brief to the agent without waiting; amend re-sends it
// as an amendment.
func (c selfCLI) Dispatch(name, briefPath string, amend bool) error {
	args := []string{"dispatch", name, briefPath, "--no-wait"}
	if amend {
		args = append(args, "--amend")
	}
	_, err := c.step("dispatch the job orchestrator", args...)
	return err
}

// Status reads the agent's state: the state is the second tab-separated
// field of the first stdout line whose first field is the agent name, and
// the report path the third field. A non-zero exit still returns the
// parsed state when one is present (status exits non-zero for blocked,
// gone, quota and the like); no line is an error. The question, quota and
// provider states print a JSON object instead of a tab line, so a line that
// carries the agent in its agent field is read the same way.
func (c selfCLI) Status(name string) (string, string, error) {
	opts := platform.RunOptions{Env: c.Env, Cwd: c.Dir, TimeoutMs: int(c.timeout() / time.Millisecond)}
	var result platform.RunResult
	if filepath.IsAbs(c.Exe) {
		result = platform.RunExecutable(c.Exe, []string{"status", name}, opts)
	} else {
		result = platform.RunCli(c.Exe, []string{"status", name}, opts)
	}
	if result.NotFound || result.TimedOut || result.Status == nil {
		return "", "", teamOpsError("read the job orchestrator status")
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		if state, report, ok := parseStatusLine(line, name); ok {
			return state, report, nil
		}
	}
	return "", "", teamOpsError("read the job orchestrator status")
}

// parseStatusLine parses one status line for the agent: the tab form first,
// then the JSON form the question, quota and provider states print.
func parseStatusLine(line, name string) (state, report string, ok bool) {
	fields := strings.Split(line, "\t")
	if len(fields) >= 2 && fields[0] == name && fields[1] != "" {
		if len(fields) >= 3 {
			report = fields[2]
		}
		return fields[1], report, true
	}
	object, err := parseJSONObject(line)
	if err != nil {
		return "", "", false
	}
	agent, _ := object["agent"].(string)
	if agent != name {
		return "", "", false
	}
	state, _ = object["status"].(string)
	if state == "" {
		return "", "", false
	}
	report, _ = object["report"].(string)
	return state, report, true
}

// Release closes the agent's pane and force-releases it. This slice does
// not release (the release slice owns it); the adapter is complete so that
// slice has nothing to add.
func (c selfCLI) Release(name string) error {
	_, err := c.step("release the job orchestrator", "release", name, "--close", "--force")
	return err
}

// parseJSONObject decodes one JSON object; a non-object or an undecodable
// input is an error.
func parseJSONObject(raw string) (map[string]any, error) {
	var object map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &object); err != nil || object == nil {
		return nil, errors.New("not a JSON object")
	}
	return object, nil
}
