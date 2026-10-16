package plugin_test

import (
	"path/filepath"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/plugin"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// TestBridgePaneOpenAlwaysUsesTheSohoPluginID (DJA-318 N2, negative): the
// bridge opens every pane under the soho plugin id (djalmajr.soho),
// whatever HERDR_PLUGIN_ID the invoking plugin carried: a leftover shim id
// (djalmajr.herdr-soho), the soho id, an unknown id, or an unset variable
// all open with --plugin djalmajr.soho.
func TestBridgePaneOpenAlwaysUsesTheSohoPluginID(t *testing.T) {
	actions := []string{"pick", "team", "roster", "doctor"}
	cases := []struct {
		name     string
		pluginID string // HERDR_PLUGIN_ID value; "" leaves the variable unset
	}{
		{"leftover shim id", "djalmajr.herdr-soho"},
		{"soho id", "djalmajr.soho"},
		{"unknown id", "djalmajr.someone-else"},
		{"missing id", ""},
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
				if pluginID != plugin.SohoPluginID {
					t.Fatalf("%s pane open argv=%#v want --plugin %s", action, argv, plugin.SohoPluginID)
				}
			}
		})
	}
}
