package plugin

import (
	"encoding/base64"
	"fmt"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

const ClipboardTimeoutMs = 10_000

type ClipboardResult struct {
	Path  string
	Bytes int
}

func ClipboardCandidates(platformName string) [][]string {
	switch platformName {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "win32":
		return [][]string{{"powershell", "-NoProfile", "-Command", "[Console]::InputEncoding=[Text.Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())"}}
	default:
		return [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	}
}

func OSC52Sequence(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}

// CopyText tries native clipboard tools in platform order, then emits OSC 52.
func CopyText(text string, env platform.Env, platformName string) ClipboardResult {
	for _, candidate := range ClipboardCandidates(platformName) {
		args := candidate[1:]
		run := platform.RunCli(candidate[0], args, platform.RunOptions{Env: env, Platform: platformName, Input: text, TimeoutMs: ClipboardTimeoutMs})
		if !run.NotFound && run.Error == "" && !run.TimedOut && run.Status != nil && *run.Status == 0 {
			return ClipboardResult{Path: candidate[0]}
		}
	}
	sequence := OSC52Sequence(text)
	_, _ = fmt.Fprint(platform.Stdout, sequence)
	return ClipboardResult{Path: "osc52", Bytes: len([]byte(sequence))}
}

func PluginCommand(args []string, env platform.Env, platformName, executable string, stdin string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(platform.Stderr, "herdr-soho plugin: expected 'bridge', 'clipboard' or 'picker'")
		return ExitInvalidTarget
	}
	switch args[0] {
	case "bridge":
		if len(args) < 2 {
			_, _ = fmt.Fprintln(platform.Stderr, "herdr-soho plugin: expected 'doctor', 'roster' or 'pick'")
			return ExitInvalidTarget
		}
		r, err := Bridge(args[1], env, platformName, executable)
		if err != nil {
			bridgeErr, ok := err.(*BridgeError)
			if !ok {
				panic(err)
			}
			_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho plugin: %s\n", bridgeErr.Message)
			return bridgeErr.Code
		}
		_, _ = fmt.Fprint(platform.Stdout, r.Out)
		_, _ = fmt.Fprint(platform.Stderr, r.Err)
		return r.Code
	case "clipboard":
		CopyText(stdin, env, platformName)
		return 0
	case "picker":
		return RunPicker(env, platformName, executable)
	default:
		_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho plugin: unknown command '%s'\n", args[0])
		return ExitInvalidTarget
	}
}
