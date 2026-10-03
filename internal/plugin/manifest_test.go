package plugin_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestManifestCommandsUseTheGoCLI checks every `command` line of
// plugin/herdr-plugin.toml with a plain line parser (no external TOML
// dependency). Herdr runs the argv without a shell, so the commands must
// call the Go CLI by name:
//   - every command starts with "herdr-soho", "plugin";
//   - the next subcommand is one PluginCommand dispatches
//     (internal/plugin/clipboard.go): bridge, clipboard or picker;
//   - a bridge action is one Bridge accepts (internal/plugin/bridge.go):
//     doctor, roster or pick.
//
// A manifest pointed at a removed Node file or a subcommand the CLI does
// not have is rejected here.
func TestManifestCommandsUseTheGoCLI(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "plugin", "herdr-plugin.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pluginSubcommands := map[string]bool{"bridge": true, "clipboard": true, "picker": true}
	bridgeActions := map[string]bool{"doctor": true, "roster": true, "pick": true}
	commands := 0
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		inner, ok := strings.CutPrefix(line, "command = [")
		if !ok {
			continue
		}
		inner, ok = strings.CutSuffix(strings.TrimSpace(inner), "]")
		if !ok {
			t.Fatalf("unparsed command line (missing ']'): %q", line)
		}
		var cmd []string
		for _, part := range strings.Split(inner, ",") {
			arg := strings.Trim(strings.TrimSpace(part), `"`)
			if arg == "" {
				t.Fatalf("empty argument in command line: %q", line)
			}
			cmd = append(cmd, arg)
		}
		commands++
		if len(cmd) < 3 || cmd[0] != "herdr-soho" || cmd[1] != "plugin" {
			t.Fatalf("command does not start with [\"herdr-soho\", \"plugin\"]: %q", line)
		}
		if !pluginSubcommands[cmd[2]] {
			t.Fatalf("subcommand %q is not dispatched by PluginCommand: %q", cmd[2], line)
		}
		if cmd[2] == "bridge" {
			if len(cmd) != 4 {
				t.Fatalf("bridge command must be [\"herdr-soho\", \"plugin\", \"bridge\", <action>]: %q", line)
			}
			if !bridgeActions[cmd[3]] {
				t.Fatalf("bridge action %q is not accepted by Bridge: %q", cmd[3], line)
			}
		}
	}
	if commands == 0 {
		t.Fatal("no `command` lines found in plugin/herdr-plugin.toml")
	}
}
