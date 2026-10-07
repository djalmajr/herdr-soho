package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestRosterWorkspaceScopeAndExplicitServer(t *testing.T) {
	for _, scope := range []string{"workspace", "server"} {
		t.Run(scope, func(t *testing.T) {
			f := newCopiesFixture(t, []fakecli.Rule{
				{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[{"name":"near","pane_id":"ws:p1","agent":"claude","agent_status":"done"},{"name":"far","pane_id":"other:p2","agent":"pi","agent_status":"idle"}]}}`},
				{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[{"pane_id":"ws:p1","workspace_id":"ws","tab_id":"ws:t1"}]}}`},
				{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[{"pane_id":"ws:p1","workspace_id":"ws"},{"pane_id":"other:p2","workspace_id":"other"}]}}`},
				{Argv: []string{"tab", "list", "--workspace", "ws"}, Stdout: `{"result":{"tabs":[{"tab_id":"ws:t1","label":"work"}]}}`},
			})
			env := f.env.Clone()
			env["HERDR_SOHO_NOWRITE"] = "1"
			before := nowriteTreeSnapshot(t, f.root)
			args := []string{"roster"}
			if scope == "server" {
				args = append(args, "--scope", scope)
			}
			code, out, stderr := f.run(t, env, args...)
			if code != 0 || stderr != "" || !strings.Contains(out, "near") || !strings.Contains(out, "done") {
				t.Fatalf("code=%d out=%q err=%q", code, out, stderr)
			}
			if scope == "workspace" && (strings.Contains(out, "far") || !strings.Contains(out, "in workspace ws")) {
				t.Fatalf("default scope leaks another workspace: %q", out)
			}
			if scope == "server" && (!strings.Contains(out, "far") || !strings.Contains(out, "# workspace other") || !strings.Contains(out, "idle")) {
				t.Fatalf("server scope loses grouping or open sessions: %q", out)
			}
			if after := nowriteTreeSnapshot(t, f.root); !reflect.DeepEqual(before, after) {
				t.Fatal("read-only roster changed the tree")
			}
		})
	}
}

func TestRosterScopeRejectsUnknownOptions(t *testing.T) {
	for _, args := range [][]string{{"--scope"}, {"--scope", "all"}, {"--scope", ""}, {"--close"}, {"--scope", "server", "extra"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f := newCopiesFixture(t, nil)
			env := f.env.Clone()
			env["HERDR_SOHO_NOWRITE"] = "1"
			before := nowriteTreeSnapshot(t, f.root)
			code, _, _ := f.run(t, env, append([]string{"roster"}, args...)...)
			if code != 2 || !reflect.DeepEqual(before, nowriteTreeSnapshot(t, f.root)) {
				t.Fatalf("invalid roster invocation code=%d or changed tree", code)
			}
		})
	}
}
