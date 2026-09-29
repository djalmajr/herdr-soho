package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

const (
	ExitInvalidTarget = 2
	ExitHerdrFailure  = 4
	herdrTimeoutMs    = 30_000
	cliTimeoutMs      = 300_000
)

var actions = map[string]bool{"doctor": true, "roster": true}

var pickOpenArgs = []string{"plugin", "pane", "open", "--plugin", "djalmajr.herdr-soho", "--entrypoint", "picker", "--placement", "overlay", "--focus"}

func PickerArguments() []string { return append([]string(nil), pickOpenArgs...) }

type BridgeError struct {
	Code    int
	Message string
}

func (e *BridgeError) Error() string { return e.Message }

type context struct {
	WorkspaceID string
	TabID       string
	PaneID      string
}

type pane struct {
	WorkspaceID string `json:"workspace_id"`
	Cwd         string `json:"cwd"`
}

type Result struct {
	Code int
	Out  string
	Err  string
}

// Bridge runs one plugin action using the Herdr-focused pane as its target.
func Bridge(action string, env platform.Env, platformName, executable string) (Result, error) {
	if action == "pick" {
		return bridgePick(env, platformName)
	}
	if !actions[action] {
		return Result{}, &BridgeError{Code: ExitInvalidTarget, Message: "unknown subcommand '" + action + "' (use 'doctor', 'roster' or 'pick')"}
	}
	ctx, err := parseContext(env)
	if err != nil {
		return Result{}, err
	}
	herdrBin := env.Get("HERDR_BIN_PATH")
	if herdrBin == "" {
		return Result{}, &BridgeError{Code: ExitInvalidTarget, Message: "HERDR_BIN_PATH missing; the action must run inside Herdr"}
	}
	focused, err := getPane(herdrBin, ctx.PaneID, env, platformName)
	if err != nil {
		return Result{}, err
	}
	if focused.WorkspaceID != ctx.WorkspaceID {
		return Result{}, &BridgeError{Code: ExitInvalidTarget, Message: fmt.Sprintf("workspace divergence: the context points to '%s' and pane %s belongs to '%s'; target rejected", ctx.WorkspaceID, ctx.PaneID, focused.WorkspaceID)}
	}
	info, statErr := os.Stat(focused.Cwd)
	if statErr != nil {
		return Result{}, &BridgeError{Code: ExitInvalidTarget, Message: "pane cwd does not exist: " + focused.Cwd}
	}
	if !info.IsDir() {
		return Result{}, &BridgeError{Code: ExitInvalidTarget, Message: "pane cwd is not a directory: " + focused.Cwd}
	}
	childEnv := env.Clone()
	childEnv["HERDR_WORKSPACE_ID"] = ctx.WorkspaceID
	childEnv["HERDR_PANE_ID"] = ctx.PaneID
	childEnv["HERDR_SOHO_NOWRITE"] = "1"
	if ctx.TabID != "" {
		childEnv["HERDR_TAB_ID"] = ctx.TabID
	} else {
		delete(childEnv, "HERDR_TAB_ID")
	}
	pathKey := "PATH"
	if _, ok := childEnv["Path"]; ok && platformName == "win32" {
		pathKey = "Path"
	}
	separator := string(os.PathListSeparator)
	if platformName == "win32" {
		separator = ";"
	}
	childEnv[pathKey] = filepath.Dir(herdrBin) + separator + childEnv[pathKey]
	intro := fmt.Sprintf("herdr-soho plugin: target workspace=%s pane=%s cwd=%s\n", ctx.WorkspaceID, ctx.PaneID, focused.Cwd)
	childEnv = withPathTail(childEnv, filepath.Dir(executable), platformName)
	run := platform.RunExecutable(executable, []string{action}, platform.RunOptions{Env: childEnv, Platform: platformName, Cwd: focused.Cwd, TimeoutMs: cliTimeoutMs})
	if run.Error == "ETIMEDOUT" || run.TimedOut {
		return Result{Code: ExitHerdrFailure, Out: intro, Err: fmt.Sprintf("herdr-soho plugin: CLI %s timed out after 300s\n", action)}, nil
	}
	if run.Error != "" || run.NotFound {
		cause := run.Error
		if run.NotFound {
			cause = "ENOENT"
		}
		return Result{}, &BridgeError{Code: ExitHerdrFailure, Message: fmt.Sprintf("CLI not executable (%s): %s", executable, cause)}
	}
	code := 1
	if run.Status != nil {
		code = *run.Status
	}
	return Result{Code: code, Out: intro + run.Stdout, Err: run.Stderr}, nil
}

func bridgePick(env platform.Env, platformName string) (Result, error) {
	ctx, err := parseContext(env)
	if err != nil {
		return Result{}, err
	}
	herdrBin := env.Get("HERDR_BIN_PATH")
	if herdrBin == "" {
		return Result{}, &BridgeError{Code: ExitInvalidTarget, Message: "HERDR_BIN_PATH missing; the action must run inside Herdr"}
	}
	focused, err := getPane(herdrBin, ctx.PaneID, env, platformName)
	if err != nil {
		return Result{}, err
	}
	if focused.WorkspaceID != ctx.WorkspaceID {
		return Result{}, &BridgeError{Code: ExitInvalidTarget, Message: fmt.Sprintf("workspace divergence: the context points to '%s' and pane %s belongs to '%s'; target rejected", ctx.WorkspaceID, ctx.PaneID, focused.WorkspaceID)}
	}
	run := platform.RunExecutable(herdrBin, pickOpenArgs, platform.RunOptions{Env: env, Platform: platformName, TimeoutMs: herdrTimeoutMs})
	if run.Error == "ETIMEDOUT" || run.TimedOut {
		return Result{}, &BridgeError{Code: ExitHerdrFailure, Message: "herdr plugin pane open timed out after 30s"}
	}
	if run.Error != "" || run.NotFound {
		cause := run.Error
		if run.NotFound {
			cause = "ENOENT"
		}
		return Result{}, &BridgeError{Code: ExitHerdrFailure, Message: fmt.Sprintf("herdr binary not executable (%s): %s", herdrBin, cause)}
	}
	code := 1
	if run.Status != nil {
		code = *run.Status
	}
	return Result{Code: code, Out: fmt.Sprintf("herdr-soho plugin: target workspace=%s pane=%s cwd=%s\n", ctx.WorkspaceID, ctx.PaneID, focused.Cwd) + run.Stdout, Err: run.Stderr}, nil
}

func parseContext(env platform.Env) (context, error) {
	raw := env.Get("HERDR_PLUGIN_CONTEXT_JSON")
	if raw == "" {
		return context{}, &BridgeError{Code: ExitInvalidTarget, Message: "missing context (empty HERDR_PLUGIN_CONTEXT_JSON); the action must run inside Herdr"}
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return context{}, &BridgeError{Code: ExitInvalidTarget, Message: "invalid context (malformed JSON in HERDR_PLUGIN_CONTEXT_JSON)"}
	}
	value, ok := decoded.(map[string]any)
	if !ok {
		return context{}, &BridgeError{Code: ExitInvalidTarget, Message: "invalid context (expected a JSON object in HERDR_PLUGIN_CONTEXT_JSON)"}
	}
	workspaceID, ok := value["workspace_id"].(string)
	if !ok || workspaceID == "" {
		return context{}, &BridgeError{Code: ExitInvalidTarget, Message: "context has no workspace_id; unknown target"}
	}
	paneID, ok := value["focused_pane_id"].(string)
	if !ok || paneID == "" {
		return context{}, &BridgeError{Code: ExitInvalidTarget, Message: "context has no focused_pane_id; unknown target"}
	}
	tabID, _ := value["tab_id"].(string)
	return context{WorkspaceID: workspaceID, TabID: tabID, PaneID: paneID}, nil
}

func getPane(herdrBin, paneID string, env platform.Env, platformName string) (pane, error) {
	run := platform.RunExecutable(herdrBin, []string{"pane", "get", paneID}, platform.RunOptions{Env: env, Platform: platformName, TimeoutMs: herdrTimeoutMs})
	if run.Error == "ETIMEDOUT" || run.TimedOut {
		return pane{}, &BridgeError{Code: ExitHerdrFailure, Message: "herdr pane get timed out after 30s"}
	}
	if run.Error != "" || run.NotFound {
		cause := run.Error
		if run.NotFound {
			cause = "ENOENT"
		}
		return pane{}, &BridgeError{Code: ExitHerdrFailure, Message: fmt.Sprintf("herdr binary not executable (%s): %s", herdrBin, cause)}
	}
	if run.Status == nil || *run.Status != 0 {
		out := strings.TrimSpace(run.Stdout + " " + run.Stderr)
		message := fmt.Sprintf("herdr pane get failed (exit %v)", run.Status)
		if run.Status != nil {
			message = fmt.Sprintf("herdr pane get failed (exit %d)", *run.Status)
		}
		if out != "" {
			message += ": " + out
		}
		return pane{}, &BridgeError{Code: ExitHerdrFailure, Message: message}
	}
	var response any
	if err := json.Unmarshal([]byte(run.Stdout), &response); err != nil {
		return pane{}, &BridgeError{Code: ExitHerdrFailure, Message: "malformed herdr output (invalid JSON from pane get)"}
	}
	responseObject, ok := response.(map[string]any)
	if !ok {
		return pane{}, &BridgeError{Code: ExitHerdrFailure, Message: "malformed herdr output (pane get without .result.pane)"}
	}
	resultObject, ok := responseObject["result"].(map[string]any)
	if !ok {
		return pane{}, &BridgeError{Code: ExitHerdrFailure, Message: "malformed herdr output (pane get without .result.pane)"}
	}
	paneObject, ok := resultObject["pane"].(map[string]any)
	if !ok {
		return pane{}, &BridgeError{Code: ExitHerdrFailure, Message: "malformed herdr output (pane get without .result.pane)"}
	}
	workspaceID, ok := paneObject["workspace_id"].(string)
	if !ok || workspaceID == "" {
		return pane{}, &BridgeError{Code: ExitHerdrFailure, Message: "malformed herdr output (pane without workspace_id)"}
	}
	cwd, ok := paneObject["cwd"].(string)
	if !ok || cwd == "" {
		return pane{}, &BridgeError{Code: ExitHerdrFailure, Message: "malformed herdr output (pane without cwd)"}
	}
	return pane{WorkspaceID: workspaceID, Cwd: cwd}, nil
}

func withPathTail(env platform.Env, dir, platformName string) platform.Env {
	key := "PATH"
	if platformName == "win32" {
		if _, ok := env["Path"]; ok && env["PATH"] == "" {
			key = "Path"
		}
	}
	separator := string(os.PathListSeparator)
	if platformName == "win32" {
		separator = ";"
	}
	if env.Get(key) == "" {
		env[key] = dir
	} else {
		env[key] += separator + dir
	}
	return env
}

func withPath(env platform.Env, dir, platformName string) platform.Env {
	key := "PATH"
	if platformName == "win32" {
		if _, ok := env["Path"]; ok && env["PATH"] == "" {
			key = "Path"
		}
	}
	separator := string(os.PathListSeparator)
	if platformName == "win32" {
		separator = ";"
	}
	if env.Get(key) == "" {
		env[key] = dir
	} else {
		env[key] = dir + separator + env.Get(key)
	}
	return env
}
