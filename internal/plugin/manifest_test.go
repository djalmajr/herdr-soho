package plugin_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/plugin"
)

const (
	sohoManifestPath = "plugin/herdr-plugin.toml"
	shimManifestPath = "plugin-legacy/herdr-plugin.toml"
)

type manifestAction struct {
	ID       string
	Contexts []string
	Command  []string
}

type manifestPane struct {
	ID        string
	Placement string
	Width     string
	Height    string
	Command   []string
}

type parsedManifest struct {
	ID      string
	Name    string
	Version string
	Actions []manifestAction
	Panes   []manifestPane
}

func manifestRoot(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join("..", "..", rel)
}

// splitList parses a TOML inline array of quoted strings such as
// ["workspace"] with a plain line parser (no external TOML dependency).
func splitList(t *testing.T, value, line string) []string {
	t.Helper()
	inner, ok := strings.CutPrefix(strings.TrimSpace(value), "[")
	if !ok {
		t.Fatalf("list without '[': %q", line)
	}
	inner, ok = strings.CutSuffix(strings.TrimSpace(inner), "]")
	if !ok {
		t.Fatalf("list without ']': %q", line)
	}
	var items []string
	for _, part := range strings.Split(inner, ",") {
		item := strings.Trim(strings.TrimSpace(part), `"`)
		if item == "" {
			t.Fatalf("empty item in list: %q", line)
		}
		items = append(items, item)
	}
	return items
}

// parseCommand parses a `command = [...]` line into its argv.
func parseCommand(t *testing.T, line string) []string {
	t.Helper()
	inner, ok := strings.CutPrefix(strings.TrimSpace(line), "command = [")
	if !ok {
		t.Fatalf("unparsed command line (missing 'command = ['): %q", line)
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
	return cmd
}

// parseManifest reads one herdr-plugin.toml with a plain line parser:
// the top-level id/name/version plus every [[actions]] and [[panes]]
// entry with the fields the parity check compares (id, contexts,
// placement, width, height, command).
func parseManifest(t *testing.T, path string) parsedManifest {
	t.Helper()
	data, err := os.ReadFile(manifestRoot(t, path))
	if err != nil {
		t.Fatal(err)
	}
	var m parsedManifest
	section := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "[[actions]]" {
			section = "action"
			m.Actions = append(m.Actions, manifestAction{})
			continue
		}
		if line == "[[panes]]" {
			section = "pane"
			m.Panes = append(m.Panes, manifestPane{})
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "command" {
			cmd := parseCommand(t, line)
			switch section {
			case "action":
				m.Actions[len(m.Actions)-1].Command = cmd
			case "pane":
				m.Panes[len(m.Panes)-1].Command = cmd
			}
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "id":
			switch section {
			case "action":
				m.Actions[len(m.Actions)-1].ID = value
			case "pane":
				m.Panes[len(m.Panes)-1].ID = value
			default:
				m.ID = value
			}
		case "name":
			m.Name = value
		case "version":
			m.Version = value
		case "contexts":
			if section != "action" {
				t.Fatalf("contexts outside [[actions]]: %q", line)
			}
			m.Actions[len(m.Actions)-1].Contexts = splitList(t, strings.TrimSpace(value), line)
		case "placement":
			if section != "pane" {
				t.Fatalf("placement outside [[panes]]: %q", line)
			}
			m.Panes[len(m.Panes)-1].Placement = value
		case "width":
			if section != "pane" {
				t.Fatalf("width outside [[panes]]: %q", line)
			}
			m.Panes[len(m.Panes)-1].Width = value
		case "height":
			if section != "pane" {
				t.Fatalf("height outside [[panes]]: %q", line)
			}
			m.Panes[len(m.Panes)-1].Height = value
		}
	}
	if m.ID == "" || m.Name == "" || m.Version == "" {
		t.Fatalf("manifest %s: missing top-level id, name or version", path)
	}
	return m
}

func allCommands(m parsedManifest) [][]string {
	var cmds [][]string
	for _, a := range m.Actions {
		cmds = append(cmds, a.Command)
	}
	for _, p := range m.Panes {
		cmds = append(cmds, p.Command)
	}
	return cmds
}

// TestManifestCommandsUseTheGoCLI checks every `command` line of both
// plugin/herdr-plugin.toml and plugin-legacy/herdr-plugin.toml with a
// plain line parser (no external TOML dependency). Herdr runs the argv
// without a shell, so the commands must call the Go CLI by name:
//   - every command starts with "soho", "plugin";
//   - the next subcommand is one PluginCommand dispatches
//     (internal/plugin/clipboard.go): board, bridge, clipboard or picker;
//   - a bridge action is one Bridge accepts (internal/plugin/bridge.go):
//     board, doctor, roster or pick.
//
// A manifest pointed at a removed binary or a subcommand the CLI does
// not have is rejected here.
func TestManifestCommandsUseTheGoCLI(t *testing.T) {
	pluginSubcommands := map[string]bool{"board": true, "bridge": true, "clipboard": true, "picker": true, "team": true}
	bridgeActions := map[string]bool{"board": true, "doctor": true, "roster": true, "team": true, "pick": true}
	for _, rel := range []string{sohoManifestPath, shimManifestPath} {
		t.Run(rel, func(t *testing.T) {
			m := parseManifest(t, rel)
			commands := 0
			for _, cmd := range allCommands(m) {
				commands++
				if len(cmd) < 3 || cmd[0] != "soho" || cmd[1] != "plugin" {
					t.Fatalf("%s: command does not start with [\"soho\", \"plugin\"]: %q", rel, cmd)
				}
				if !pluginSubcommands[cmd[2]] {
					t.Fatalf("%s: subcommand %q is not dispatched by PluginCommand: %q", rel, cmd[2], cmd)
				}
				if cmd[2] == "bridge" {
					if len(cmd) != 4 {
						t.Fatalf("%s: bridge command must be [\"soho\", \"plugin\", \"bridge\", <action>]: %q", rel, cmd)
					}
					if !bridgeActions[cmd[3]] {
						t.Fatalf("%s: bridge action %q is not accepted by Bridge: %q", rel, cmd[3], cmd)
					}
				}
			}
			if commands == 0 {
				t.Fatalf("%s: no `command` lines found", rel)
			}
		})
	}
}

// TestBothManifestsShareTheSameActionsAndPanes: the legacy shim
// (plugin-legacy/herdr-plugin.toml) is a compatibility copy of the soho
// manifest (plugin/herdr-plugin.toml). Both must expose the same actions
// and panes — ids, contexts and commands — run the same soho binary,
// carry the same version, and use the decided ids and names.
func TestBothManifestsShareTheSameActionsAndPanes(t *testing.T) {
	soho := parseManifest(t, sohoManifestPath)
	shim := parseManifest(t, shimManifestPath)

	if soho.ID != plugin.SohoPluginID || soho.Name != "soho" {
		t.Fatalf("soho manifest identity: id=%q name=%q, want %q / %q", soho.ID, soho.Name, plugin.SohoPluginID, "soho")
	}
	if shim.ID != plugin.LegacyShimPluginID || shim.Name != "herdr-soho (legacy shim)" {
		t.Fatalf("shim manifest identity: id=%q name=%q, want %q / %q", shim.ID, shim.Name, plugin.LegacyShimPluginID, "herdr-soho (legacy shim)")
	}
	if soho.Version == "" || soho.Version != shim.Version {
		t.Fatalf("manifest versions differ: soho=%q shim=%q", soho.Version, shim.Version)
	}

	sort.Slice(soho.Actions, func(i, j int) bool { return soho.Actions[i].ID < soho.Actions[j].ID })
	sort.Slice(shim.Actions, func(i, j int) bool { return shim.Actions[i].ID < shim.Actions[j].ID })
	if !reflect.DeepEqual(soho.Actions, shim.Actions) {
		t.Fatalf("actions differ:\nsoho=%#v\nshim=%#v", soho.Actions, shim.Actions)
	}
	sort.Slice(soho.Panes, func(i, j int) bool { return soho.Panes[i].ID < soho.Panes[j].ID })
	sort.Slice(shim.Panes, func(i, j int) bool { return shim.Panes[i].ID < shim.Panes[j].ID })
	if !reflect.DeepEqual(soho.Panes, shim.Panes) {
		t.Fatalf("panes differ:\nsoho=%#v\nshim=%#v", soho.Panes, shim.Panes)
	}

	wantActions := []string{"pick", "doctor", "roster", "team"}
	sort.Strings(wantActions)
	gotActions := make([]string, len(soho.Actions))
	for i, a := range soho.Actions {
		gotActions[i] = a.ID
	}
	sort.Strings(gotActions)
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("action ids=%v, want %v", gotActions, wantActions)
	}
	wantPanes := []string{"picker", "team", "team-doctor"}
	sort.Strings(wantPanes)
	gotPanes := make([]string, len(soho.Panes))
	for i, p := range soho.Panes {
		gotPanes[i] = p.ID
	}
	sort.Strings(gotPanes)
	if !reflect.DeepEqual(gotPanes, wantPanes) {
		t.Fatalf("pane ids=%v, want %v", gotPanes, wantPanes)
	}

	// Every command runs the soho binary (command[0] == "soho").
	for _, m := range []parsedManifest{soho, shim} {
		for _, cmd := range allCommands(m) {
			if cmd[0] != "soho" {
				t.Fatalf("command[0]=%q, want \"soho\": %q", cmd[0], cmd)
			}
		}
	}
}
