package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

type compactFixture struct {
	root string
	bin  string
	env  platform.Env
}

// newCompactFixture builds a hermetic workspace with one rostered worker of
// the given kind and installs the fake herdr CLI with the given rules. The
// worker's role file resolves the effective effort to `high`.
func newCompactFixture(t *testing.T, kind string, rules []fakecli.Rule) *compactFixture {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	roles := filepath.Join(root, "roles")
	skill := filepath.Join(root, "skill")
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "conf")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{filepath.Join(state, "ws"), roles, skill, home, conf, bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\tmode\targs\teffort\n"
	roster += "worker\tp1\t" + kind + "\timplementer\t\t0\t\t2026\t\ttask\timplementer\t\t\t\t\n"
	if err := os.WriteFile(filepath.Join(state, "ws", "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roles, "implementer.md"), []byte("---\nname: implementer\nmode: edit\neffort: high\n---\nRole body.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	entries := fakecli.Env(testutil.CleanEnv(t), bin)
	env := platform.Env{}
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	env["HERDR_ENV"] = "1"
	env["HERDR_SOHO_DIR"] = state
	env["HERDR_WORKSPACE_ID"] = "ws"
	env["HERDR_SOHO_ROLES"] = roles
	env["HERDR_SOHO_SKILL_DIR"] = skill
	env["HERDR_SOCKET_PATH"] = filepath.Join(root, "missing.sock")
	env["HOME"] = home
	env["USERPROFILE"] = home
	env["XDG_CONFIG_HOME"] = conf
	env["TMPDIR"] = root
	return &compactFixture{root: root, bin: bin, env: env}
}

func (f *compactFixture) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	code := Run(args, f.env)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	return code, out.String(), stderr.String()
}

func (f *compactFixture) calls(t *testing.T) []fakecli.Call {
	t.Helper()
	logPath := filepath.Join(f.bin, "herdr.calls.jsonl")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return nil // herdr was never invoked
	}
	calls, err := fakecli.ReadCalls(logPath)
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

func compactStateJSON(state string, seq int) string {
	return `{"result":{"agent":{"agent_status":"` + state + `","state_change_seq":` + strconv.Itoa(seq) + `}}}`
}

// countArgv returns how many recorded calls match argv exactly.
func countArgv(calls []fakecli.Call, argv []string) int {
	n := 0
	for _, c := range calls {
		if reflect.DeepEqual(c.Argv, argv) {
			n++
		}
	}
	return n
}

// countArgvPrefix counts recorded calls whose argv starts with prefix.
func countArgvPrefix(calls []fakecli.Call, prefix []string) int {
	n := 0
	for _, c := range calls {
		if len(c.Argv) < len(prefix) {
			continue
		}
		match := true
		for i, p := range prefix {
			if c.Argv[i] != p {
				match = false
				break
			}
		}
		if match {
			n++
		}
	}
	return n
}

func compactJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		t.Fatalf("no JSON on stdout")
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		t.Fatalf("stdout %q is not a JSON object: %v", out, err)
	}
	return value
}

var compactScreens = struct {
	piStale        string
	piFresh        string
	piFooterHigh   string
	piFooterMedium string
	claudeStale    string
	claudeFresh    string
	codexProof     string
	noProof        string
}{
	piStale:        "old output line\n[compaction] Compacted from 99999 tokens\n> /compact\n",
	piFresh:        "old output line\n[compaction] Compacted from 99999 tokens\n> /compact\n[compaction] Compacted from 12345 tokens\n",
	piFooterHigh:   "old output line\n> /compact\n[compaction] Compacted from 12345 tokens\n• thinking: high\n",
	piFooterMedium: "old output line\n> /compact\n[compaction] Compacted from 12345 tokens\n• thinking: medium\n",
	claudeStale:    "old output line\nCompacted\n> /compact\n",
	claudeFresh:    "old output line\nCompacted\n> /compact\nCompacted the conversation to 42 tokens\n",
	codexProof:     "> /compact\nContext compacted\n",
	noProof:        "> /compact\n",
}

func compactReadArgv(agent string) []string {
	return []string{"agent", "read", agent, "--source", "recent", "--lines", "40"}
}

func TestCompactStaleProofAboveMarkerDoesNotCount(t *testing.T) {
	// A Compacted from line above the sent /compact is an earlier compaction;
	// only the proof below it ends the wait.
	t.Run("pi: the stale proof above /compact is ignored and the new one below counts", func(t *testing.T) {
		f := newCompactFixture(t, "pi", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), Call: 1, Stdout: compactScreens.piStale},
			{Argv: compactReadArgv("worker"), Call: 2, Stdout: compactScreens.piFresh},
			{Argv: compactReadArgv("worker"), Call: 3, Stdout: compactScreens.piFooterHigh},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
		if code != 0 || errText != "" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		value := compactJSON(t, out)
		if value["agent"] != "worker" || value["kind"] != "pi" || value["status"] != "compacted" {
			t.Fatalf("json=%v", value)
		}
		if elapsed, ok := value["elapsed_ms"].(float64); !ok || elapsed <= 0 {
			t.Fatalf("elapsed_ms=%v", value["elapsed_ms"])
		}
		calls := f.calls(t)
		if n := countArgv(calls, []string{"pane", "send-text", "p1", "/compact"}); n != 1 {
			t.Fatalf("send-text calls=%d want 1: %#v", n, calls)
		}
		if n := countArgv(calls, []string{"pane", "send-keys", "p1", "Enter"}); n != 1 {
			t.Fatalf("send-keys calls=%d want 1: %#v", n, calls)
		}
		// Proof polling reads twice (the stale screen, then the fresh one) and
		// the pi footer check reads once more.
		if n := countArgv(calls, compactReadArgv("worker")); n != 3 {
			t.Fatalf("recent reads=%d want 3: %#v", n, calls)
		}
	})
}

func TestCompactClaudeProofBelowMarker(t *testing.T) {
	f := newCompactFixture(t, "claude", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: compactScreens.claudeStale},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: compactScreens.claudeFresh},
	})
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 0 || errText != "" {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["agent"] != "worker" || value["kind"] != "claude" || value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	calls := f.calls(t)
	if n := countArgv(calls, compactReadArgv("worker")); n != 2 {
		t.Fatalf("recent reads=%d want 2: %#v", n, calls)
	}
	if n := countArgv(calls, []string{"pane", "send-text", "p1", "/compact"}); n != 1 {
		t.Fatalf("send-text calls=%d want 1", n)
	}
}

func TestCompactCodexWaitsForIdleAfterProof(t *testing.T) {
	// Codex stays working for a few seconds after the proof; the wait keeps
	// polling agent get until it is idle again.
	f := newCompactFixture(t, "codex", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("working", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 3, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: compactScreens.codexProof},
	})
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 0 || errText != "" {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["agent"] != "worker" || value["kind"] != "codex" || value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	calls := f.calls(t)
	// One pre-send state check plus the two idle-wait polls (working, then idle).
	if n := countArgv(calls, []string{"agent", "get", "worker"}); n != 3 {
		t.Fatalf("agent get calls=%d want 3: %#v", n, calls)
	}
}

func TestCompactKindWithoutVerifiedCommandExits2(t *testing.T) {
	f := newCompactFixture(t, "grok", nil)
	code, out, errText := f.run(t, "compact", "worker")
	if code != 2 || out != "" {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	if !strings.Contains(errText, "compact: kind 'grok' has no verified compact command; release --close and spawn --fresh instead") {
		t.Fatalf("stderr=%q", errText)
	}
	calls := f.calls(t)
	if len(calls) != 0 {
		t.Fatalf("herdr calls before the kind check: %#v", calls)
	}
}

func TestCompactBusyWorkerExits10(t *testing.T) {
	for _, state := range []string{"working", "blocked"} {
		t.Run(state, func(t *testing.T) {
			f := newCompactFixture(t, "pi", []fakecli.Rule{
				{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON(state, 1)},
			})
			code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
			if code != 10 || out != "" {
				t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
			}
			want := "compact: agent 'worker' is " + state + "; compact only an idle worker"
			if !strings.Contains(errText, want) {
				t.Fatalf("stderr=%q want %q", errText, want)
			}
			calls := f.calls(t)
			if n := countArgvPrefix(calls, []string{"pane", "send-text"}); n != 0 {
				t.Fatalf("send-text calls on a busy worker: %#v", calls)
			}
		})
	}
}

func TestCompactTimeoutExits9(t *testing.T) {
	f := newCompactFixture(t, "claude", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: compactScreens.noProof},
	})
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "500")
	if code != 9 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["agent"] != "worker" || value["kind"] != "claude" || value["status"] != "timeout" {
		t.Fatalf("json=%v", value)
	}
	if elapsed, ok := value["elapsed_ms"].(float64); !ok || elapsed < 500 || elapsed >= 10000 {
		t.Fatalf("elapsed_ms=%v want >= 500", value["elapsed_ms"])
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-text", "p1", "/compact"}); n != 1 {
		t.Fatalf("send-text calls=%d want 1: %#v", n, calls)
	}
}

func TestCompactPiThinkingWarning(t *testing.T) {
	// The effective effort is high (the role file); the footer shows medium.
	f := newCompactFixture(t, "pi", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: compactScreens.piFresh},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: compactScreens.piFooterMedium},
	})
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 0 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	if value := compactJSON(t, out); value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	want := "compact: 'worker' shows thinking 'medium' after compaction; its effort is 'high'"
	if !strings.Contains(errText, want) {
		t.Fatalf("stderr=%q want %q", errText, want)
	}
}

func TestCompactUnknownAgentExits3(t *testing.T) {
	f := newCompactFixture(t, "pi", nil)
	code, out, errText := f.run(t, "compact", "ghost")
	if code != 3 || out != "" {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	if !strings.Contains(errText, "agent 'ghost' is not in this skill's roster") {
		t.Fatalf("stderr=%q", errText)
	}
}

func TestCompactUsageErrors(t *testing.T) {
	cases := [][]string{
		{"compact"},
		{"compact", "--timeout"},
		{"compact", "worker", "--timeout", "abc"},
		{"compact", "worker", "--timeout", "0"},
		{"compact", "worker", "extra"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := newCompactFixture(t, "pi", nil)
			code, out, errText := f.run(t, args...)
			if code != 2 || out != "" {
				t.Fatalf("args=%v code=%d out=%s stderr=%s", args, code, out, errText)
			}
		})
	}
}

// dispatchLastLineStatus parses the last JSON line of dispatch's stdout; with
// --compact the compact result line precedes it.
func dispatchLastLineStatus(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var value map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &value); err != nil {
		t.Fatalf("dispatch JSON %q: %v", out, err)
	}
	status, _ := value["wait_status"].(string)
	return status
}

// TestDispatchCompactCompactsThenSends verifies that dispatch --compact runs
// the same compact step for an idle worker and then sends the brief.
func TestDispatchCompactCompactsThenSends(t *testing.T) {
	extra := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 3, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "w0test:p0a", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "w0test:p0a", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: compactScreens.codexProof},
	}
	f := newDispatchArrivalFixture(t, "working", 1, 2, "$CURRENT_PATHS", "0", extra...)
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait", "--compact")
	if code != 0 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	if status := dispatchLastLineStatus(t, out); status != "submitted" {
		t.Fatalf("wait_status=%s out=%s", status, out)
	}
	// One JSON line on stdout (the dispatch's); the compaction is a stderr note.
	if strings.Contains(out, `"status":"compacted"`) || strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("stdout must hold only the dispatch line: %s", out)
	}
	if !strings.Contains(errText, "dispatch: compacted 'worker' in ") {
		t.Fatalf("missing the compacted note on stderr: %s", errText)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(f.bin, "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := countArgv(calls, []string{"pane", "send-text", "w0test:p0a", "/compact"}); n != 1 {
		t.Fatalf("send-text calls=%d want 1: %#v", n, calls)
	}
	if n := countArgvPrefix(calls, []string{"agent", "prompt", "worker"}); n != 1 {
		t.Fatalf("prompt calls=%d want 1: %#v", n, calls)
	}
}

// TestDispatchCompactTimeoutKeepsBriefUnsent verifies that a compact timeout
// exits dispatch with 9 and the brief is never sent.
func TestDispatchCompactTimeoutKeepsBriefUnsent(t *testing.T) {
	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	fixedNow := platform.Now
	platform.Now = func() time.Time {
		value := base
		base = base.Add(310 * time.Second)
		return value
	}
	t.Cleanup(func() { platform.Now = fixedNow })
	extra := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "w0test:p0a", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "w0test:p0a", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: compactScreens.noProof},
	}
	f := newDispatchArrivalFixture(t, "working", 1, 2, "$CURRENT_PATHS", "0", extra...)
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait", "--compact")
	if code != 9 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	if !strings.Contains(out, `"status":"timeout"`) {
		t.Fatalf("missing the timeout JSON: %s", out)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(f.bin, "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := countArgvPrefix(calls, []string{"agent", "prompt", "worker"}); n != 0 {
		t.Fatalf("the brief was sent after the compact timeout: %#v", calls)
	}
	// No task state points at a report that will never come.
	if _, err := os.Stat(filepath.Join(f.state, "ws", "last-report-worker")); !os.IsNotExist(err) {
		t.Fatalf("last-report was written before the compact timeout: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(f.state, "ws", "briefs")); len(entries) != 0 {
		t.Fatalf("a composed brief was written before the compact timeout: %v", entries)
	}
	if n := countArgv(calls, []string{"pane", "send-text", "w0test:p0a", "/compact"}); n != 1 {
		t.Fatalf("send-text calls=%d want 1: %#v", n, calls)
	}
}

// TestDispatchCompactBusyWorkerExits10 verifies that a working worker stops
// the dispatch with 10 before the brief is sent.
func TestDispatchCompactBusyWorkerExits10(t *testing.T) {
	extra := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("working", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("working", 1)},
	}
	f := newDispatchArrivalFixture(t, "working", 1, 2, "$CURRENT_PATHS", "0", extra...)
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait", "--compact")
	if code != 10 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	want := "compact: agent 'worker' is working; compact only an idle worker"
	if !strings.Contains(errText, want) {
		t.Fatalf("stderr=%q want %q", errText, want)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(f.bin, "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := countArgvPrefix(calls, []string{"agent", "prompt", "worker"}); n != 0 {
		t.Fatalf("the brief was sent to a busy worker: %#v", calls)
	}
}

// TestDispatchCompactSkipsUnsupportedKind verifies that dispatch --compact
// warns for a kind without a verified command and still sends the brief.
func TestDispatchCompactSkipsUnsupportedKind(t *testing.T) {
	extra := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("working", 2)},
	}
	f := newDispatchArrivalFixture(t, "working", 1, 2, "$CURRENT_PATHS", "0", extra...)
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\tmode\targs\teffort\nworker\tw0test:p0a\tgrok\timplementer\t\t0\t\tnow\tgrok-1\ttask\timplementer\t\t\t\thigh\n"
	if err := os.WriteFile(filepath.Join(f.state, "ws", "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait", "--compact")
	if code != 0 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	if status := dispatchLastLineStatus(t, out); status != "submitted" {
		t.Fatalf("wait_status=%s out=%s", status, out)
	}
	want := "dispatch: --compact skipped: kind 'grok' has no verified compact command"
	if !strings.Contains(errText, want) {
		t.Fatalf("stderr=%q want %q", errText, want)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(f.bin, "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := countArgvPrefix(calls, []string{"pane", "send-text"}); n != 0 {
		t.Fatalf("send-text calls for an unsupported kind: %#v", calls)
	}
	if n := countArgvPrefix(calls, []string{"agent", "prompt", "worker"}); n != 1 {
		t.Fatalf("prompt calls=%d want 1: %#v", n, calls)
	}
}
