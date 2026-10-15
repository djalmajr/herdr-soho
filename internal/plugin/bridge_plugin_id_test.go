package plugin_test

import (
	"path/filepath"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/plugin"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// TestBridgePaneOpenUsesTheInvokingPluginID: `plugin pane open` must open
// the pane under the id of the plugin that invoked the action. Herdr
// injects that id as HERDR_PLUGIN_ID for action commands (it is not part
// of HERDR_PLUGIN_CONTEXT_JSON). The legacy shim
// (djalmajr.herdr-soho) must open its own panes during the compatibility
// window; the soho plugin (djalmajr.soho) is the default, and any other
// value falls back to it.
func TestBridgePaneOpenUsesTheInvokingPluginID(t *testing.T) {
	actions := []string{"pick", "team", "roster", "doctor"}
	cases := []struct {
		name     string
		pluginID string // HERDR_PLUGIN_ID value; "" leaves the variable unset
		wantID   string
	}{
		{"shim invocation", "djalmajr.herdr-soho", "djalmajr.herdr-soho"},
		{"soho invocation", "djalmajr.soho", "djalmajr.soho"},
		{"unknown id falls back to soho", "djalmajr.someone-else", "djalmajr.soho"},
		{"missing id falls back to soho", "", "djalmajr.soho"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, _, _, _ := installBridgeFakes(t, "__default__", 0, 0, "", "")
			if tc.pluginID != "" {
				env["HERDR_PLUGIN_ID"] = tc.pluginID
			}
			for _, action := range actions {
				r, err := plugin.Bridge(action, env, platform.Current(), "")
				if err != nil || r.Code != 0 {
					t.Fatalf("bridge %s: result=%+v err=%v", action, r, err)
				}
			}
			calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(env.Get("HERDR_BIN_PATH")), "herdr.json"))
			if err != nil || len(calls) != 2*len(actions) {
				t.Fatalf("Herdr calls=%#v err=%v (want %d)", calls, err, 2*len(actions))
			}
			for i, action := range actions {
				argv := calls[2*i+1].Argv
				pluginID := ""
				for j := 0; j+1 < len(argv); j++ {
					if argv[j] == "--plugin" {
						pluginID = argv[j+1]
					}
				}
				if pluginID != tc.wantID {
					t.Fatalf("%s pane open argv=%#v want --plugin %s", action, argv, tc.wantID)
				}
			}
		})
	}
}
