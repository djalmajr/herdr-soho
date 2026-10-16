package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/djalmajr/soho/internal/platform"
)

const (
	ExitInvalidTarget = 2
	ExitHerdrFailure  = 4
	herdrTimeoutMs    = 30_000

	// SohoPluginID is the id of the soho plugin
	// (plugin/herdr-plugin.toml) and the default for pane opens.
	SohoPluginID = "djalmajr.soho"
	// LegacyShimPluginID is the id of the compatibility shim
	// (plugin-legacy/herdr-plugin.toml). It keeps the old shortcuts
	// working until the shim is removed after v0.4.0.
	LegacyShimPluginID = "djalmajr.herdr-soho"
)

func pickOpenArgs(pluginID string) []string {
	return []string{"plugin", "pane", "open", "--plugin", pluginID, "--entrypoint", "picker", "--focus"}
}

func teamOpenArgs(pluginID string) []string {
	return []string{"plugin", "pane", "open", "--plugin", pluginID, "--entrypoint", "team", "--placement", "overlay", "--focus"}
}

func teamDoctorOpenArgs(pluginID string) []string {
	return []string{"plugin", "pane", "open", "--plugin", pluginID, "--entrypoint", "team-doctor", "--placement", "overlay", "--focus"}
}

// PickerArguments returns the default picker open argv (the soho id).
func PickerArguments() []string { return pickOpenArgs(SohoPluginID) }

// BoardArguments remains a compatibility alias for the unified picker.
func BoardArguments() []string { return pickOpenArgs(SohoPluginID) }

// TeamArguments returns the default team open argv (the soho id).
func TeamArguments() []string { return teamOpenArgs(SohoPluginID) }

// TeamDoctorArguments returns the default team-doctor open argv (the soho id).
func TeamDoctorArguments() []string { return teamDoctorOpenArgs(SohoPluginID) }

// invokingPluginID resolves the id of the plugin that invoked the action
// from HERDR_PLUGIN_ID, which Herdr injects for action commands (the
// context JSON itself does not carry the id). Only the soho id and the
// legacy shim id are accepted; anything else, including an empty value,
// falls back to the soho id.
func invokingPluginID(env platform.Env) string {
	switch id := env.Get("HERDR_PLUGIN_ID"); id {
	case SohoPluginID, LegacyShimPluginID:
		return id
	default:
		return SohoPluginID
	}
}

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

// Bridge runs one plugin action using the Herdr-focused pane as its
// target. The unified picker uses the native popup placement from its
// manifest; team views use overlays. Output belongs to the interactive
// surface rather than the Herdr log.
func Bridge(action string, env platform.Env, platformName, executable string) (Result, error) {
	pluginID := invokingPluginID(env)
	if action == "pick" || action == "board" {
		return bridgePaneOpen(env, platformName, pickOpenArgs(pluginID))
	}
	if action == "team" || action == "roster" {
		return bridgePaneOpen(env, platformName, teamOpenArgs(pluginID))
	}
	if action == "doctor" {
		return bridgePaneOpen(env, platformName, teamDoctorOpenArgs(pluginID))
	}
	return Result{}, &BridgeError{Code: ExitInvalidTarget, Message: "unknown subcommand '" + action + "' (use 'doctor', 'roster', 'board' or 'pick'); the 'team' action opens the team panel"}
}

// bridgePaneOpen validates the focused target like the read-only actions
// and then opens the requested interactive surface over it.
func bridgePaneOpen(env platform.Env, platformName string, openArgs []string) (Result, error) {
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
	run := platform.RunExecutable(herdrBin, openArgs, platform.RunOptions{Env: env, Platform: platformName, TimeoutMs: herdrTimeoutMs})
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
	return Result{Code: code, Out: fmt.Sprintf("soho plugin: target workspace=%s pane=%s cwd=%s\n", ctx.WorkspaceID, ctx.PaneID, focused.Cwd) + run.Stdout, Err: run.Stderr}, nil
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
