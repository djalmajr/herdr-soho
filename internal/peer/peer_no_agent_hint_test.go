package peer_test

// F7: when agent get resolves agent_not_found for a
// [machine/]<workspace>:<pane> reference (the workspace's orchestrator moved
// to another pane), the 4 message suggests the live agents of the same
// workspace on the same machine, listed the way find does (the same snapshot
// agent list, with --machine for a remote machine): at most 5, preferring
// the names that begin with "orchestrator". A failed or empty listing — and
// a target that is not such a reference — keeps today's message. The exit
// code stays 4 and nothing is sent.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

const noAgentNotRef = `{"error":{"code":"agent_not_found","message":"no such pane"}}`

// The snapshot of workspace w12: pane p1 holds no agent (the failed
// reference), p10 holds the orchestrator (claude, working) and p3 a codex
// (idle, unnamed). Workspace w13 holds one more agent, which must not be
// suggested for a w12 reference.
const w12Snapshot = `{"result":{"snapshot":{"workspaces":[{"workspace_id":"ws-12","label":"w12"},{"workspace_id":"ws-13","label":"w13"}],"tabs":[],"agents":[{"pane_id":"w12:p10","name":"orchestrator-12","agent":"claude"},{"pane_id":"w12:p3","agent":"codex"},{"pane_id":"w13:p1","name":"other-workspace","agent":"pi"}],"panes":[{"pane_id":"w12:p1","workspace_id":"ws-12","tab_id":"t1"},{"pane_id":"w12:p10","workspace_id":"ws-12","tab_id":"t1","agent_status":"working","focused":true},{"pane_id":"w12:p3","workspace_id":"ws-12","tab_id":"t2","agent_status":"idle"},{"pane_id":"w13:p1","workspace_id":"ws-13","tab_id":"t3","agent_status":"idle"}]}}}`

// Seven live agents in w12, in snapshot order: the two orchestrator names
// must come first, then the rest in snapshot order, cut at five.
const w12CrowdedSnapshot = `{"result":{"snapshot":{"workspaces":[{"workspace_id":"ws-12","label":"w12"}],"tabs":[],"agents":[{"pane_id":"w12:p11","name":"planner-12","agent":"pi"},{"pane_id":"w12:p12","name":"orchestrator-12","agent":"claude"},{"pane_id":"w12:p13","name":"reviewer-12","agent":"codex"},{"pane_id":"w12:p14","name":"implementer-12","agent":"pi"},{"pane_id":"w12:p15","name":"orchestrator-3","agent":"claude"},{"pane_id":"w12:p16","name":"scouter-12","agent":"codex"},{"pane_id":"w12:p17","name":"tasker-12","agent":"pi"}],"panes":[{"pane_id":"w12:p9","workspace_id":"ws-12","tab_id":"t1"},{"pane_id":"w12:p11","workspace_id":"ws-12","tab_id":"t1","agent_status":"idle"},{"pane_id":"w12:p12","workspace_id":"ws-12","tab_id":"t2","agent_status":"working"},{"pane_id":"w12:p13","workspace_id":"ws-12","tab_id":"t2","agent_status":"idle"},{"pane_id":"w12:p14","workspace_id":"ws-12","tab_id":"t3","agent_status":"blocked"},{"pane_id":"w12:p15","workspace_id":"ws-12","tab_id":"t3","agent_status":"working"},{"pane_id":"w12:p16","workspace_id":"ws-12","tab_id":"t4","agent_status":"idle"},{"pane_id":"w12:p17","workspace_id":"ws-12","tab_id":"t4","agent_status":"idle"}]}}}`

func TestSendNoAgentHintListsSameWorkspaceAgents(t *testing.T) {
	// A local reference with no agent: the 4 message lists the live agents
	// of the same workspace, the orchestrator name first, the unnamed agent
	// with "-".
	f := newFixture(t, []fakecli.Rule{
		{Argv: []string{"agent", "get", "w12:p1"}, Stderr: noAgentNotRef, Code: 1},
		{Argv: []string{"api", "snapshot"}, Stdout: w12Snapshot},
	})
	code, out, stderr := f.run([]string{"send", "w12:p1", "hello"})
	if code != 4 || out != "" {
		t.Fatalf("the hint keeps today's exit 4 and sends nothing: code=%d out=%q stderr=%q", code, out, stderr)
	}
	want := "herdr-soho: send: no agent in local/w12:p1; agents in w12: local/w12:p10 (orchestrator-12, claude), local/w12:p3 (-, codex)\n"
	if stderr != want {
		t.Fatalf("the message suggests the same-workspace agents: %q", stderr)
	}
	assertNoAgentHintCalls(t, f, 2, "api snapshot")
}

func TestSendNoAgentHintRemoteListsWithMachine(t *testing.T) {
	// A remote reference: the listing is find's snapshot with --machine, and
	// the suggested references carry the machine.
	f := newFixture(t, []fakecli.Rule{
		{Argv: []string{"--machine", "mac", "agent", "get", "w12:p1"}, Stderr: noAgentNotRef, Code: 1},
		{Argv: []string{"--machine", "mac", "api", "snapshot"}, Stdout: w12Snapshot},
	})
	code, out, stderr := f.run([]string{"send", "mac/w12:p1", "hello"})
	if code != 4 || out != "" {
		t.Fatalf("the remote hint keeps today's exit 4 and sends nothing: code=%d out=%q stderr=%q", code, out, stderr)
	}
	want := "herdr-soho: send: no agent in mac/w12:p1; agents in w12: mac/w12:p10 (orchestrator-12, claude), mac/w12:p3 (-, codex)\n"
	if stderr != want {
		t.Fatalf("the remote message suggests the same-workspace agents on that machine: %q", stderr)
	}
	assertNoAgentHintCalls(t, f, 2, "--machine mac api snapshot")
}

func TestSendNoAgentHintKeepsMessageWhenListingFails(t *testing.T) {
	// The listing fails: today's message stands, untouched.
	f := newFixture(t, []fakecli.Rule{
		{Argv: []string{"agent", "get", "w12:p1"}, Stderr: noAgentNotRef, Code: 1},
		{Argv: []string{"api", "snapshot"}, Stderr: "herdr api snapshot: connection refused", Code: 1},
	})
	code, out, stderr := f.run([]string{"send", "w12:p1", "hello"})
	if code != 4 || out != "" {
		t.Fatalf("a failed listing keeps today's exit 4: code=%d out=%q stderr=%q", code, out, stderr)
	}
	if want := "herdr-soho: send: no agent in local/w12:p1\n"; stderr != want {
		t.Fatalf("a failed listing keeps today's message: %q", stderr)
	}
	assertNoAgentHintCalls(t, f, 2, "api snapshot")
}

func TestSendNoAgentHintKeepsMessageWhenListingEmpty(t *testing.T) {
	// The listing comes back without any live agent in the workspace: today's
	// message stands.
	f := newFixture(t, []fakecli.Rule{
		{Argv: []string{"agent", "get", "w12:p1"}, Stderr: noAgentNotRef, Code: 1},
		{Argv: []string{"api", "snapshot"}, Stdout: `{"result":{"snapshot":{"workspaces":[{"workspace_id":"ws-12","label":"w12"}],"tabs":[],"agents":[],"panes":[{"pane_id":"w12:p1","workspace_id":"ws-12","tab_id":"t1"}]}}}`},
	})
	code, out, stderr := f.run([]string{"send", "w12:p1", "hello"})
	if code != 4 || out != "" {
		t.Fatalf("an empty listing keeps today's exit 4: code=%d out=%q stderr=%q", code, out, stderr)
	}
	if want := "herdr-soho: send: no agent in local/w12:p1\n"; stderr != want {
		t.Fatalf("an empty listing keeps today's message: %q", stderr)
	}
	assertNoAgentHintCalls(t, f, 2, "api snapshot")
}

func TestSendNoAgentHintCutsAtFivePreferringOrchestrator(t *testing.T) {
	// Seven live agents: the message holds at most 5, the orchestrator names
	// first, then the rest in snapshot order.
	f := newFixture(t, []fakecli.Rule{
		{Argv: []string{"agent", "get", "w12:p9"}, Stderr: noAgentNotRef, Code: 1},
		{Argv: []string{"api", "snapshot"}, Stdout: w12CrowdedSnapshot},
	})
	code, out, stderr := f.run([]string{"send", "w12:p9", "hello"})
	if code != 4 || out != "" {
		t.Fatalf("the crowded hint keeps today's exit 4: code=%d out=%q stderr=%q", code, out, stderr)
	}
	want := "herdr-soho: send: no agent in local/w12:p9; agents in w12: local/w12:p12 (orchestrator-12, claude), local/w12:p15 (orchestrator-3, claude), local/w12:p11 (planner-12, pi), local/w12:p13 (reviewer-12, codex), local/w12:p14 (implementer-12, pi)\n"
	if stderr != want {
		t.Fatalf("the crowded message prefers orchestrator names and cuts at 5: %q", stderr)
	}
	assertNoAgentHintCalls(t, f, 2, "api snapshot")
}

func TestSendNoAgentHintNameTargetKeepsMessage(t *testing.T) {
	// A target that is a name, not a [machine/]<workspace>:<pane> reference:
	// no listing, today's message, no snapshot call.
	f := newFixture(t, []fakecli.Rule{
		{Argv: []string{"agent", "get", "build"}, Stderr: noAgentNotRef, Code: 1},
	})
	code, out, stderr := f.run([]string{"send", "build", "hello"})
	if code != 4 || out != "" {
		t.Fatalf("a name target keeps today's exit 4: code=%d out=%q stderr=%q", code, out, stderr)
	}
	if want := "herdr-soho: send: no agent in build\n"; stderr != want {
		t.Fatalf("a name target keeps today's message: %q", stderr)
	}
	assertNoAgentHintCalls(t, f, 1, "")
}

// assertNoAgentHintCalls asserts the herdr calls: resolve get, the snapshot
// (when wantSecond is nonempty) and nothing else — no prompt, no key.
func assertNoAgentHintCalls(t *testing.T, f *fixture, wantCalls int, wantSecond string) {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != wantCalls {
		t.Fatalf("the hint only reads the roster, it sends nothing: got %d calls, want %d: %+v", len(calls), wantCalls, calls)
	}
	if wantSecond != "" && strings.Join(calls[1].Argv, " ") != wantSecond {
		t.Fatalf("the listing is find's snapshot call: %+v", calls[1].Argv)
	}
	if enters := countSendKeyEnters(t, f); enters != 0 {
		t.Fatalf("no key may go to a pane without an agent: %d enters", enters)
	}
	if prompts := countPromptCalls(t, f); prompts != 0 {
		t.Fatalf("nothing was sent: %d prompts", prompts)
	}
}
