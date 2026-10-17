package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/testutil"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// DJA-392: `run` (and `spawn`) refuse to open a new pane under extreme
// pressure with exit 25 and the exact stderr line cli.Run renders;
// `--force` (passed by `run` to the spawn) skips only the refusal. The
// core parts rule and the spawn-side behavior are covered in
// internal/core and internal/spawn.

const (
	cliRefuseLine = "soho: spawn: refusing to open a new pane under resource pressure: "
	cliWarnLine   = "soho: resource pressure: swap 96% used (24 GB of 25 GB); run 'soho gc' and close idle panes before opening more\n"
)

const cliPressureGB = int64(1024 * 1024 * 1024)

// runPressureFixture builds the spawn+dispatch fake environment with the
// swap faked at 96% (above both the 80 warning and the 95 refusal
// defaults) and the disk calm.
func runPressureFixture(t *testing.T) (env platform.Env, root, bin, state, brief string) {
	t.Helper()
	root = t.TempDir()
	stateDir, rolesDir, bin := filepath.Join(root, "state"), filepath.Join(root, "roles"), filepath.Join(root, "bin")
	state = stateDir
	for _, dir := range []string{filepath.Join(stateDir, "ws"), filepath.Join(stateDir, "ws", "wait"), filepath.Join(stateDir, "ws", "briefs"), rolesDir, bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(rolesDir, "worker.md"), []byte("---\nkind: grok\nmodel: grok-4.7\neffort: high\napprovals: ask\n---\nRole body.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Pre-seed the roster header so the before/after snapshot proves the
	// refusal writes nothing beyond the pre-existing core.StateDir init.
	if err := os.WriteFile(filepath.Join(stateDir, "ws", "agents.tsv"), []byte("# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	brief = filepath.Join(root, "brief.md")
	if err := os.WriteFile(brief, []byte("# Goal\n\nDispatch a prompt.\n# Expected result\n\nObserved.\n# Owned files\n\nnone\n# Forbidden\n\nNo commit or push.\n# Report\n\ndone\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "list"}, ArgvPrefix: true, Stdout: `{"result":{"agents":[]}}`},
		{Argv: []string{"agent", "start"}, ArgvPrefix: true, Stdout: `{"result":{"started":true}}`},
		{Argv: []string{"agent", "get"}, ArgvPrefix: true, Stdout: `{"result":{"agent":{"agent_status":"idle"}}}`},
		{Argv: []string{"agent", "read"}, ArgvPrefix: true, Stdout: "ready\n"},
		{Argv: []string{"agent", "prompt", "worker"}, ArgvPrefix: true, Stdout: `{"result":{"submitted":true}}`},
		{Argv: []string{"pane", "list"}, ArgvPrefix: true, Stdout: `{"result":{"panes":[]}}`},
		{Argv: []string{"pane", "layout"}, ArgvPrefix: true, Stdout: `{"result":{"layout":{"area":{"width":1000,"height":700},"panes":[{"pane_id":"p1","rect":{"x":0,"y":0,"width":1000,"height":700}}]}}`},
		{Argv: []string{"pane", "split"}, ArgvPrefix: true, Stdout: `{"result":{"pane":{"pane_id":"p2"}}}`},
		{Argv: []string{"pane", "close"}, ArgvPrefix: true},
		{Argv: []string{"tab", "list"}, ArgvPrefix: true, Stdout: `{"result":{"tabs":[]}}`},
		{Argv: []string{"tab", "create"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"tab_id":"t-herd","label":"herd"},"root_pane":{"pane_id":"p-herd"}}}`},
		{Argv: []string{"pane", "title"}, ArgvPrefix: true, Code: 1},
	}
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "grok", nil); err != nil {
		t.Fatal(err)
	}
	entries := fakecli.Env(testutil.CleanEnv(t), bin)
	env = platform.Env{}
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	env["HERDR_ENV"], env["HERDR_WORKSPACE_ID"], env["SOHO_DIR"] = "1", "ws", stateDir
	env["SOHO_ROLES"], env["SOHO_SKILL_DIR"] = rolesDir, testSkillDir(t)
	env["SOHO_LANES"], env["SOHO_LAYOUT"], env["SOHO_REGRID"] = "off", "split", "off"
	env["SOHO_WAIT_POLL_MS"] = "1"
	env["SOHO_PRESSURE_DISK_FREE_PERCENT"], env["SOHO_PRESSURE_SWAP_PERCENT"] = "15", "80"
	env["SOHO_PROMPT_CHECK_SECONDS"], env["SOHO_PROMPT_SETTLE_SECONDS"] = "0", "0"
	env["SOHO_BRIEF_LINT"] = "off"
	env["TMPDIR"], env["HERDR_SOCKET_PATH"] = root, filepath.Join(root, "missing.sock")
	oldDisk, oldSwap := platform.DiskFree, platform.SwapUsage
	platform.DiskFree = func(string) (int64, int64, bool) { return 90 * cliPressureGB, 100 * cliPressureGB, true }
	platform.SwapUsage = func(platform.Env) (int64, int64, bool) { return 24 * cliPressureGB, 25 * cliPressureGB, true }
	t.Cleanup(func() { platform.DiskFree, platform.SwapUsage = oldDisk, oldSwap })
	return env, root, bin, state, brief
}

func TestRunRefusesUnderExtremePressureAndForceOpens(t *testing.T) {
	t.Run("run under pressure exits 25 with the exact line and touches nothing", func(t *testing.T) {
		env, root, bin, state, brief := runPressureFixture(t)
		before := snapshotTree(t, state)
		code, out, stderr := runIn(t, []string{"run", "worker", brief, "--no-wait"}, env, root)
		if code != 25 {
			t.Fatalf("exit=%d, want 25: stderr=%q", code, stderr)
		}
		want := "soho: resource pressure: swap 96% used (24 GB of 25 GB); run 'soho gc' and close idle panes before opening more\n" +
			"soho: spawn: refusing to open a new pane under resource pressure: swap 96% used (24 GB of 25 GB); close idle panes or run 'soho gc', or pass --force\n"
		if stderr != want {
			t.Fatalf("stderr=%q, want exactly %q", stderr, want)
		}
		if out != "" {
			t.Fatalf("the refusal printed stdout: %q", out)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(bin, "herdr.calls.jsonl"))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		for _, call := range calls {
			if len(call.Argv) < 2 {
				continue
			}
			switch call.Argv[0] + " " + call.Argv[1] {
			case "agent list", "agent get", "pane list", "pane layout", "agent read":
			default:
				t.Fatalf("run under pressure made a herdr call beyond the pre-refusal reads: %#v", call.Argv)
			}
		}
		if after := snapshotTree(t, state); !reflect.DeepEqual(before, after) {
			t.Fatalf("run under pressure wrote state: before=%v after=%v", before, after)
		}
	})

	t.Run("run --force follows the spawn to the dispatch", func(t *testing.T) {
		env, root, _, state, brief := runPressureFixture(t)
		code, out, stderr := runIn(t, []string{"run", "worker", brief, "--no-wait", "--force"}, env, root)
		if code != 0 {
			t.Fatalf("exit=%d, want 0: stderr=%q out=%q", code, stderr, out)
		}
		if strings.Contains(stderr, cliRefuseLine) {
			t.Fatalf("--force kept the refusal: %q", stderr)
		}
		if !strings.Contains(stderr, cliWarnLine) {
			t.Fatalf("--force dropped the warning: %q", stderr)
		}
		if !strings.Contains(out, `"status"`) || !strings.Contains(out, `"wait_status"`) {
			t.Fatalf("run --force did not complete the spawn and the dispatch: %q", out)
		}
		roster, err := os.ReadFile(filepath.Join(state, "ws", "agents.tsv"))
		if err != nil || !strings.Contains(string(roster), "\tgrok\tworker\t") {
			t.Fatalf("run --force wrote no roster line: %q err=%v", roster, err)
		}
	})
}

// DJA-392: the reopen's spawn half always opens a new pane (--fresh), so the
// refusal runs before the release half changes anything (no roster removal,
// no pane close); --force skips it and is forwarded to the spawn half.
func TestReopenRefusesUnderExtremePressure(t *testing.T) {
	seedRoster := func(t *testing.T, state, root string) []byte {
		t.Helper()
		path := filepath.Join(state, "ws", "agents.tsv")
		header, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		row := "worker-old\tp-1\tgrok\tworker\txai\t1\t" + root + "\tnow\tgrok-4.7\task\tworker\t\t\t\thigh\n"
		if err := os.WriteFile(path, append(header, row...), 0o600); err != nil {
			t.Fatal(err)
		}
		return header
	}

	t.Run("reopen under pressure exits 25 before releasing anything", func(t *testing.T) {
		env, root, bin, state, _ := runPressureFixture(t)
		header := seedRoster(t, state, root)
		code, out, stderr := runIn(t, []string{"reopen", "worker-old"}, env, root)
		if code != 25 {
			t.Fatalf("exit=%d, want 25: stderr=%q", code, stderr)
		}
		want := "soho: resource pressure: swap 96% used (24 GB of 25 GB); run 'soho gc' and close idle panes before opening more\n" +
			"soho: spawn: refusing to open a new pane under resource pressure: swap 96% used (24 GB of 25 GB); close idle panes or run 'soho gc', or pass --force\n"
		if stderr != want {
			t.Fatalf("stderr=%q, want exactly %q", stderr, want)
		}
		if out != "" {
			t.Fatalf("the refusal printed stdout: %q", out)
		}
		roster, err := os.ReadFile(filepath.Join(state, "ws", "agents.tsv"))
		if err != nil || !bytes.Equal(roster, append(append([]byte{}, header...), "worker-old\tp-1\tgrok\tworker\txai\t1\t"+root+"\tnow\tgrok-4.7\task\tworker\t\t\t\thigh\n"...)) {
			t.Fatalf("the refusal changed the roster: %q err=%v", roster, err)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(bin, "herdr.calls.jsonl"))
		if err == nil && len(calls) > 0 {
			t.Fatalf("the refusal made herdr calls: %#v", calls)
		}
	})

	t.Run("reopen --force skips the refusal and reopens the worker", func(t *testing.T) {
		env, root, _, state, _ := runPressureFixture(t)
		seedRoster(t, state, root)
		code, out, stderr := runIn(t, []string{"reopen", "worker-old", "--force"}, env, root)
		if code != 0 {
			t.Fatalf("exit=%d, want 0: stderr=%q out=%q", code, stderr, out)
		}
		if strings.Contains(stderr, cliRefuseLine) {
			t.Fatalf("--force kept the refusal: %q", stderr)
		}
		if !strings.Contains(stderr, cliWarnLine) {
			t.Fatalf("--force dropped the warning: %q", stderr)
		}
		if !strings.Contains(out, `"status"`) || !strings.Contains(out, `"worker-old"`) {
			t.Fatalf("the forced reopen printed no spawn JSON: %q", out)
		}
		roster, err := os.ReadFile(filepath.Join(state, "ws", "agents.tsv"))
		if err != nil || !strings.Contains(string(roster), "worker-old") {
			t.Fatalf("the forced reopen left no roster line: %q err=%v", roster, err)
		}
	})
}
