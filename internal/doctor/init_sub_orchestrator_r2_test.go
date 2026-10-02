package doctor

import (
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitSubOrchestratorR2RosterOrchestratorPrefix(t *testing.T) {
	// D12: the caller pane is already a roster row of this project (a
	// sub-orchestrator the upper orchestrator opened), and its pane has no
	// title: the init must not rename its agent (no agent rename) and must
	// not set the 'orchestrator: …' title (no report-metadata), must name
	// the worker on stderr, and must report the agent's own name.
	env, root := paneContextFixture(t, []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Stdout: `{"result":{"agent":{"name":"orchestrator-child","agent_status":"working"}}}`},
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`},
		{Argv: []string{"pane", "get", "w0test:p0a"}, Stdout: `{"result":{"pane":{"workspace_id":"ws"}}}`},
	})
	env["HERDR_PANE_ID"], env["HERDR_WORKSPACE_ID"] = "w0test:p0a", "ws"
	if err := os.MkdirAll(filepath.Join(root, "state", "ws"), 0o700); err != nil {
		t.Fatal(err)
	}
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n" +
		"orchestrator-child\tw0test:p0a\tclaude\tsub-orchestrator\tanthropic\t0\t" + root + "\tnow\t\t\t\t\n"
	if err := os.WriteFile(filepath.Join(root, "state", "ws", "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr := runCmdInit(t, env, root)
	if !strings.Contains(out, `"orchestrator": "orchestrator-child"`) || !strings.Contains(out, `"pane_id": "w0test:p0a"`) || !strings.Contains(out, `"title": ""`) {
		t.Fatalf("the init did not report the roster worker as is (title left untouched, not set to 'orchestrator: …'):\n%s", out)
	}
	want := "herdr-soho: init: this pane is 'orchestrator-child' in the roster (a sub-orchestrator opened by another orchestrator); keeping its name\n"
	if !strings.Contains(stderr, want) {
		t.Fatalf("missing the roster line:\n%s", stderr)
	}
	calls, err := fakecli.ReadCalls(filepath.Join(root, "bin", "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if len(c.Argv) >= 2 && c.Argv[0] == "agent" && c.Argv[1] == "rename" {
			t.Fatalf("the roster worker was renamed: %#v", calls)
		}
		if len(c.Argv) >= 2 && c.Argv[0] == "pane" && c.Argv[1] == "report-metadata" {
			t.Fatalf("the roster pane title was touched: %#v", calls)
		}
	}
}

func TestInitSubOrchestratorR2RosterTransientNameRead(t *testing.T) {
	// D12: the caller pane is already a roster row of this project (a
	// sub-orchestrator the upper orchestrator opened), and its pane has no
	// title: the init must not rename its agent (no agent rename) and must
	// not set the 'orchestrator: …' title (no report-metadata), must name
	// the worker on stderr, and must report the agent's own name.
	env, root := paneContextFixture(t, []fakecli.Rule{
		{Argv: []string{"--version"}, Stdout: "herdr 1.2.3\n"},
		{Argv: []string{"status", "server"}, Stdout: "server 1.2.3\n"},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: `{"result":{"agent":{"name":"cinzel-opus","agent_status":"working"}}}`},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Code: 1, Stderr: "temporary get failure"},
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`},
		{Argv: []string{"pane", "get", "w0test:p0a"}, Stdout: `{"result":{"pane":{"workspace_id":"ws"}}}`},
	})
	env["HERDR_PANE_ID"], env["HERDR_WORKSPACE_ID"] = "w0test:p0a", "ws"
	if err := os.MkdirAll(filepath.Join(root, "state", "ws"), 0o700); err != nil {
		t.Fatal(err)
	}
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n" +
		"cinzel-opus\tw0test:p0a\tclaude\tsub-orchestrator\tanthropic\t0\t" + root + "\tnow\t\t\t\t\n"
	if err := os.WriteFile(filepath.Join(root, "state", "ws", "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr := runCmdInit(t, env, root)
	if !strings.Contains(out, `"orchestrator": "cinzel-opus"`) || !strings.Contains(out, `"pane_id": "w0test:p0a"`) || !strings.Contains(out, `"title": ""`) {
		t.Fatalf("the init did not report the roster worker as is (title left untouched, not set to 'orchestrator: …'):\n%s", out)
	}
	want := "herdr-soho: init: this pane is 'cinzel-opus' in the roster (a sub-orchestrator opened by another orchestrator); keeping its name\n"
	if !strings.Contains(stderr, want) {
		t.Fatalf("missing the roster line:\n%s", stderr)
	}
	calls, err := fakecli.ReadCalls(filepath.Join(root, "bin", "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if len(c.Argv) >= 2 && c.Argv[0] == "agent" && c.Argv[1] == "rename" {
			t.Fatalf("the roster worker was renamed: %#v", calls)
		}
		if len(c.Argv) >= 2 && c.Argv[0] == "pane" && c.Argv[1] == "report-metadata" {
			t.Fatalf("the roster pane title was touched: %#v", calls)
		}
	}
}
