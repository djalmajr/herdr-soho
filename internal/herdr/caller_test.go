package herdr

import (
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestPaneCurrentParsesTheNativeResult(t *testing.T) {
	// Mutation captured: the wrapper is transport only — it exposes what
	// result.pane carries (and Ok for a usable read) and never applies
	// workspace or live-topology policy; that decision stays with the caller.
	t.Run(`pane current: a well-formed result exposes the identity fields verbatim`, func(t *testing.T) {
		env := newFake(t, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"id":"cli:pane:current","result":{"pane":{"agent":"pi","pane_id":"w14:p88","tab_id":"w14:t1","title":"implementer: x","workspace_id":"w14"}},"type":"pane_current"}`}})
		got := PaneCurrent(env)
		want := PaneCurrentResult{Ok: true, PaneID: "w14:p88", TabID: "w14:t1", WorkspaceID: "w14"}
		if got != want {
			t.Fatalf("PaneCurrent()=%#v want %#v", got, want)
		}
	})
	t.Run(`pane current: an absent field is exposed as empty, not invented`, func(t *testing.T) {
		env := newFake(t, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"p1","tab_id":"t1"}}}`}})
		got := PaneCurrent(env)
		want := PaneCurrentResult{Ok: true, PaneID: "p1", TabID: "t1", WorkspaceID: ""}
		if got != want {
			t.Fatalf("PaneCurrent()=%#v want %#v", got, want)
		}
	})
	t.Run(`pane current: a null identity field is exposed as empty`, func(t *testing.T) {
		env := newFake(t, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"pane_id":"p1","tab_id":"t1","workspace_id":null}}}`}})
		got := PaneCurrent(env)
		if !got.Ok || got.PaneID != "p1" || got.TabID != "t1" || got.WorkspaceID != "" {
			t.Fatalf("PaneCurrent()=%#v", got)
		}
	})
}

func TestPaneCurrentTransportFailuresAreReportedNotThrown(t *testing.T) {
	// The wrapper never dies: every transport failure returns Ok=false so
	// the caller can refuse the whole command with its own explicit error.
	cases := []struct {
		name string
		env  func(t *testing.T) platform.Env
	}{
		{"the CLI is missing from PATH", func(t *testing.T) platform.Env { return platform.Env{"PATH": t.TempDir()} }},
		{
			"the call exits nonzero with an error payload",
			func(t *testing.T) platform.Env {
				return newFake(t, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Code: 1, Stderr: "cannot resolve current pane: server is not running"}})
			},
		},
		{
			"the call succeeds but prints nothing",
			func(t *testing.T) platform.Env {
				return newFake(t, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}}})
			},
		},
		{
			"the result is not an object",
			func(t *testing.T) platform.Env {
				return newFake(t, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":"p1"}`}})
			},
		},
		{
			"the pane is not an object",
			func(t *testing.T) platform.Env {
				return newFake(t, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":"p1"}}`}})
			},
		},
		{
			"the stdout is not JSON",
			func(t *testing.T) platform.Env {
				return newFake(t, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: "not json"}})
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PaneCurrent(tc.env(t))
			if got != (PaneCurrentResult{}) {
				t.Fatalf("PaneCurrent()=%#v want the zero result", got)
			}
		})
	}
}
