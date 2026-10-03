package wait

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// notInRosterFixture is the D11 status fixture: a temp tree with the
// workspace state dir (roster + wait dir), a fake herdr on PATH, and a
// run method that captures CmdStatus's stdout and stderr.
type notInRosterFixture struct {
	cwd   string
	state string
	ctx   *core.Config
	env   platform.Env
}

// run swaps the platform writers around the CmdStatus call and returns the
// exit code plus the captured stdout and stderr.
func (f notInRosterFixture) run(t *testing.T, agents []string) (int, string, string) {
	t.Helper()
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, err bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &err
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	code := CmdStatus(agents, f.ctx, f.env, f.cwd)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	return code, out.String(), err.String()
}

func newNotInRosterFixture(t *testing.T, rosterRows string, rules []fakecli.Rule) notInRosterFixture {
	t.Helper()
	base := t.TempDir()
	cwd := filepath.Join(base, "cwd")
	state := filepath.Join(base, "state")
	bin := filepath.Join(base, "bin")
	for _, dir := range []string{cwd, filepath.Join(state, "ws", "wait"), bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n" + rosterRows
	if err := os.WriteFile(filepath.Join(state, "ws", "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	return notInRosterFixture{
		cwd:   cwd,
		state: state,
		ctx:   &core.Config{Entries: map[string]core.ConfigEntry{}},
		env: platform.Env{
			"PATH":                      bin,
			"HERDR_ENV":                 "1",
			"HERDR_SOHO_DIR":            state,
			"HERDR_WORKSPACE_ID":        "ws",
			"HERDR_SOCKET_PATH":         filepath.Join(base, "none.sock"),
			"HERDR_SOHO_FAKECLI_CONFIG": bin,
		},
	}
}

// callLog returns the ordered argv of every fake herdr call, joined with
// NUL, in the order the calls happened.
func (f notInRosterFixture) callLog(t *testing.T) []string {
	t.Helper()
	calls, err := fakecli.ReadCalls(filepath.Join(f.env["PATH"], "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, c := range calls {
		out = append(out, strings.Join(c.Argv, "\x00"))
	}
	return out
}

func wantLog(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("herdr calls=%#v, want exactly %#v", got, want)
	}
}

// D11: an agent outside the roster that Herdr knows is `not-in-roster` with
// the stderr hint, not `unknown-agent`. The hint cites the command form
// that worked: a bare name and a local reference take the local query, a
// remote reference goes to its machine with --machine, and a value that
// does not parse as a reference keeps today's local name behavior.
func TestStatusNotInRosterWhenHerdrKnowsTheAgent(t *testing.T) {
	f := newNotInRosterFixture(t, "worker\tp1\tclaude\timplementer\tanthropic\t\t\t\tmodel-x\t\t\t\n", []fakecli.Rule{
		{Argv: []string{"agent", "get", "outsider"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
		{Argv: []string{"--machine", "box1", "agent", "get", "w2:p42"}, Stdout: `{"result":{"agent":{"agent_status":"done"}}}`},
		{Argv: []string{"agent", "get", "w3:p7"}, Stdout: `{"result":{"agent":{"agent_status":"idle"}}}`},
		{Argv: []string{"agent", "get", "w4:p9"}, Stdout: `{"result":{"agent":{"agent_status":"blocked"}}}`},
		{Argv: []string{"agent", "get", "[box1/p42]"}, Stdout: `{"result":{"agent":{"agent_status":"idle"}}}`},
	})
	code, out, err := f.run(t, []string{"outsider", "box1/w2:p42", "local/w3:p7", "w4:p9", "[box1/p42]"})
	if code != 0 {
		t.Fatalf("code=%d out=%q stderr=%q, want rc 0 (today's unknown-agent rc)", code, out, err)
	}
	want := "outsider\tnot-in-roster\t\t-\t-\n" +
		"box1/w2:p42\tnot-in-roster\t\t-\t-\n" +
		"local/w3:p7\tnot-in-roster\t\t-\t-\n" +
		"w4:p9\tnot-in-roster\t\t-\t-\n" +
		"[box1/p42]\tnot-in-roster\t\t-\t-\n"
	if out != want {
		t.Fatalf("out=%q, want one not-in-roster line per agent\nwant %q", out, want)
	}
	wantErr := "herdr-soho: status: 'outsider' is not a worker of this workspace's team; herdr agent get outsider shows its state\n" +
		"herdr-soho: status: 'box1/w2:p42' is not a worker of this workspace's team; herdr --machine box1 agent get w2:p42 shows its state\n" +
		"herdr-soho: status: 'local/w3:p7' is not a worker of this workspace's team; herdr agent get w3:p7 shows its state\n" +
		"herdr-soho: status: 'w4:p9' is not a worker of this workspace's team; herdr agent get w4:p9 shows its state\n" +
		"herdr-soho: status: '[box1/p42]' is not a worker of this workspace's team; herdr agent get [box1/p42] shows its state\n"
	if err != wantErr {
		t.Fatalf("stderr=%q, want the not-in-roster hints quoting the command form that worked\nwant %q", err, wantErr)
	}
	// Only the single agent get per agent — the remote reference's query
	// carries the machine, the local reference and the bare pane id query
	// the pane id locally, and the unparseable value keeps the name path —
	// and nothing else: no agent list, no screen reads.
	wantLog(t, f.callLog(t),
		"agent\x00get\x00outsider",
		"--machine\x00box1\x00agent\x00get\x00w2:p42",
		"agent\x00get\x00w3:p7",
		"agent\x00get\x00w4:p9",
		"agent\x00get\x00[box1/p42]",
	)
}

// D11: what Herdr does not know either stays `unknown-agent` with no hint
// and today's rc 0 — a local agent_not_found, a remote one, a remote
// transport failure, a successful query with no agent, and the unparseable
// value that falls back to today's local name behavior.
func TestStatusUnknownAgentWhenHerdrDoesNotKnow(t *testing.T) {
	f := newNotInRosterFixture(t, "worker\tp1\tclaude\timplementer\tanthropic\t\t\t\tmodel-x\t\t\t\n", []fakecli.Rule{
		{Argv: []string{"agent", "get", "ghost"}, Code: 1, Stderr: `{"error":{"code":"agent_not_found","message":"agent target ghost not found"},"id":"cli:agent:get"}`},
		{Argv: []string{"--machine", "box1", "agent", "get", "w9:p1"}, Code: 1, Stderr: `{"error":{"code":"agent_not_found","message":"agent target w9:p1 not found"},"id":"cli:agent:get"}`},
		{Argv: []string{"--machine", "box1", "agent", "get", "w9:p2"}, Code: 1, Stderr: "Error: Os { code: 13, kind: PermissionDenied, message: \"Permission denied\" }"},
		{Argv: []string{"--machine", "box1", "agent", "get", "w9:p3"}, Stdout: `{"result":{}}`},
		{Argv: []string{"agent", "get", "[box1/p42]"}, Code: 1, Stderr: "Error: no rule"},
	})
	code, out, err := f.run(t, []string{"ghost", "box1/w9:p1", "box1/w9:p2", "box1/w9:p3", "[box1/p42]"})
	want := "ghost\tunknown-agent\t\t-\t-\n" +
		"box1/w9:p1\tunknown-agent\t\t-\t-\n" +
		"box1/w9:p2\tunknown-agent\t\t-\t-\n" +
		"box1/w9:p3\tunknown-agent\t\t-\t-\n" +
		"[box1/p42]\tunknown-agent\t\t-\t-\n"
	if code != 0 || out != want {
		t.Fatalf("code=%d out=%q stderr=%q, want all unknown-agent with today's rc", code, out, err)
	}
	if err != "" {
		t.Fatalf("stderr=%q, want no hint for what Herdr does not know", err)
	}
	wantLog(t, f.callLog(t),
		"agent\x00get\x00ghost",
		"--machine\x00box1\x00agent\x00get\x00w9:p1",
		"--machine\x00box1\x00agent\x00get\x00w9:p2",
		"--machine\x00box1\x00agent\x00get\x00w9:p3",
		"agent\x00get\x00[box1/p42]",
	)
}

// D11: a roster agent keeps today's behavior and today's herdr call count —
// the not-in-roster probe must not add any call on the roster branch. The
// logged sequence is exactly the pre-change sequence for a working worker:
// one agent get, the alive-in-other-pane agent list, and the working screen
// read (nothing else: idle/done probes are not reached by a working state).
func TestStatusRosterAgentKeepsTodayCalls(t *testing.T) {
	f := newNotInRosterFixture(t, "worker\tp1\tclaude\timplementer\tanthropic\t\t\t\tmodel-x\t\t\t\n", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`},
		{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Stdout: "Busy on the task 30%\n"},
	})
	code, out, err := f.run(t, []string{"worker"})
	if code != 0 || out != "worker\tworking\t\t-\t-\n" {
		t.Fatalf("code=%d out=%q stderr=%q, want today's five-column working line", code, out, err)
	}
	wantLog(t, f.callLog(t),
		"agent\x00get\x00worker",
		"agent\x00list",
		"agent\x00read\x00worker\x00--source\x00visible",
	)
}

// D11: the mixed batch — roster, known outsider, unknown — reports each in
// its own state; the hint names the outsider only; the call order is the
// roster sequence first (today's) and then one agent get per non-roster
// agent.
func TestStatusMixedBatchKeepsStatesAndCallBudget(t *testing.T) {
	f := newNotInRosterFixture(t, "worker\tp1\tclaude\timplementer\tanthropic\t\t\t\tmodel-x\t\t\t\n", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`},
		{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Stdout: "Busy on the task 30%\n"},
		{Argv: []string{"agent", "get", "outsider"}, Stdout: `{"result":{"agent":{"agent_status":"blocked"}}}`},
		{Argv: []string{"agent", "get", "ghost"}, Code: 1, Stderr: `{"error":{"code":"agent_not_found","message":"agent target ghost not found"},"id":"cli:agent:get"}`},
	})
	code, out, err := f.run(t, []string{"worker", "outsider", "ghost"})
	if code != 0 {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, err)
	}
	if out != "worker\tworking\t\t-\t-\noutsider\tnot-in-roster\t\t-\t-\nghost\tunknown-agent\t\t-\t-\n" {
		t.Fatalf("out=%q, want the three states in the argv order", out)
	}
	if !strings.Contains(err, "herdr-soho: status: 'outsider' is not a worker of this workspace's team") || strings.Contains(err, "'ghost'") {
		t.Fatalf("stderr=%q, want the hint for the outsider only", err)
	}
	wantLog(t, f.callLog(t),
		"agent\x00get\x00worker",
		"agent\x00list",
		"agent\x00read\x00worker\x00--source\x00visible",
		"agent\x00get\x00outsider",
		"agent\x00get\x00ghost",
	)
}
