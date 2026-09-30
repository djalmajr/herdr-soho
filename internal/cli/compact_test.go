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
	piStale           string
	piFresh           string
	piFooterHigh      string
	piFooterMedium    string
	claudeStale       string
	claudeFresh       string
	claudeNothing     string
	claudeNoMessages  string
	claudeFailed      string
	codexProof        string
	noProof           string
	codexLaterMention string
	piFooterCutoff    string
}{
	piStale:          "old output line\n[compaction] Compacted from 99999 tokens\n> /compact\n",
	piFresh:          "old output line\n[compaction] Compacted from 99999 tokens\n> /compact\n[compaction] Compacted from 12345 tokens\n",
	piFooterHigh:     "old output line\n> /compact\n[compaction] Compacted from 12345 tokens\n• thinking: high\n",
	piFooterMedium:   "old output line\n> /compact\n[compaction] Compacted from 12345 tokens\n• thinking: medium\n",
	claudeStale:      "old output line\nCompacted\n> /compact\n",
	claudeFresh:      "old output line\nCompacted\n> /compact\nCompacted the conversation to 42 tokens\n",
	claudeNothing:    "old output line\n> /compact\nNot enough messages to compact.\n",
	claudeNoMessages: "old output line\n> /compact\nNo messages to compact\n",
	claudeFailed:     "old output line\n> /compact\nError compacting conversation\n",
	codexProof:       "> /compact\nContext compacted\n",
	noProof:          "> /compact\n",
	// The review's screens: a later line that only mentions /compact must not
	// become the anchor, and `cutoff` must not read as thinking `off`.
	codexLaterMention: "> /compact\nContext compacted\nthe history mentions /compact again\n",
	piFooterCutoff:    "old output line\n> /compact\n[compaction] Compacted from 12345 tokens\n• cutoff\n",
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
			// The polls without a proof also check the worker is alive.
			{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 2)},
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
		// The polls without a proof also check the worker is alive.
		{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 2)},
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

// TestCompactClaudeNothingToCompact verifies that claude's answer for a
// conversation with nothing to compact ends the wait as a success without
// compaction: no idle wait, no thinking warning, exit 0 with the
// nothing-to-compact status; the answer already on screen before the send
// does not count.
func TestCompactClaudeNothingToCompact(t *testing.T) {
	t.Run("the answer below the echoed /compact counts", func(t *testing.T) {
		f := newCompactFixture(t, "claude", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), Call: 1, Stdout: "old output line\n"},
			{Argv: compactReadArgv("worker"), Call: 2, Stdout: compactScreens.claudeNothing},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
		if code != 0 || errText != "" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		value := compactJSON(t, out)
		if value["agent"] != "worker" || value["kind"] != "claude" || value["status"] != "nothing-to-compact" {
			t.Fatalf("json=%v", value)
		}
		if elapsed, ok := value["elapsed_ms"].(float64); !ok || elapsed < 0 {
			t.Fatalf("elapsed_ms=%v", value["elapsed_ms"])
		}
		calls := f.calls(t)
		// The pre-send check is the only state read: the ending does not wait
		// for the worker back at idle.
		if n := countArgv(calls, []string{"agent", "get", "worker"}); n != 1 {
			t.Fatalf("agent get calls=%d want 1: %#v", n, calls)
		}
		if n := countArgv(calls, compactReadArgv("worker")); n != 2 {
			t.Fatalf("recent reads=%d want 2: %#v", n, calls)
		}
	})
	t.Run("the answer already on screen before the send does not count", func(t *testing.T) {
		screen := "earlier reply\nNot enough messages to compact.\n> /compact\n"
		f := newCompactFixture(t, "claude", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: screen},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "800")
		if code != 9 || compactJSON(t, out)["status"] != "timeout" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
	})
}

// TestCompactClaudeStaleEndingBelowOldCompact verifies the endings' anchor:
// an ending that already sat below an older /compact on the stable screen
// (equal before and after the send) does not count, while a new /compact
// with the same phrase repeated below it does.
func TestCompactClaudeStaleEndingBelowOldCompact(t *testing.T) {
	stableNothing := "old output line\n> /compact\nNot enough messages to compact.\n"
	stableFailed := "old output line\n> /compact\nError compacting conversation\n"
	t.Run("a stable screen with the old answer times out", func(t *testing.T) {
		f := newCompactFixture(t, "claude", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: stableNothing},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "800")
		if code != 9 || compactJSON(t, out)["status"] != "timeout" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
	})
	t.Run("a stable screen with the old error times out", func(t *testing.T) {
		f := newCompactFixture(t, "claude", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}`},
			{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: stableFailed},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "800")
		if code != 9 || compactJSON(t, out)["status"] != "timeout" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
	})
	t.Run("a new /compact with the same phrase repeated below it counts", func(t *testing.T) {
		f := newCompactFixture(t, "claude", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), Call: 1, Stdout: stableNothing},
			{Argv: compactReadArgv("worker"), Call: 2, Stdout: stableNothing + "> /compact\nNot enough messages to compact.\n"},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
		if code != 0 || compactJSON(t, out)["status"] != "nothing-to-compact" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
	})
}

// TestCompactClaudeFailedCompaction verifies that claude's compaction error
// ends the wait as a failure: exit 9 with the failed status and a friction
// warning, no idle wait, well before the deadline.
func TestCompactClaudeFailedCompaction(t *testing.T) {
	f := newCompactFixture(t, "claude", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: "old output line\n"},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: compactScreens.claudeFailed},
	})
	start := time.Now()
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	elapsed := time.Since(start)
	if code != 9 {
		t.Fatalf("code=%d want 9 out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["agent"] != "worker" || value["kind"] != "claude" || value["status"] != "failed" {
		t.Fatalf("json=%v", value)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("exited after %v; the reported failure must end the wait long before the 30 s deadline", elapsed)
	}
	want := `compact: 'worker' reported "Error compacting conversation"; nothing was compacted`
	if !strings.Contains(errText, want) {
		t.Fatalf("stderr=%q want %q", errText, want)
	}
	calls := f.calls(t)
	// The failure is found on the first poll, before any liveness or idle wait.
	if n := countArgv(calls, []string{"agent", "get", "worker"}); n != 1 {
		t.Fatalf("agent get calls=%d want 1: %#v", n, calls)
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
		// The polls without a proof also check the worker is alive.
		{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 2)},
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
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: "old output line\n"},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: compactScreens.piFresh},
		{Argv: compactReadArgv("worker"), Call: 3, Stdout: compactScreens.piFooterMedium},
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

// TestCompactPiThinkingOffAndRestored verifies the pi thinking handling: a
// footer at a level is turned off with shift+tab before the /compact and the
// level is given back after the compaction; the result JSON carries
// thinking_restored and no warning is emitted.
func TestCompactPiThinkingOffAndRestored(t *testing.T) {
	before := "old output line\n• thinking: high\n"
	off := "old output line\n• thinking: off\n"
	proof := "old output line\n[compaction] Compacted from 12345 tokens\n• thinking: off\n"
	restored := "old output line\n[compaction] Compacted from 12345 tokens\n• thinking: high\n"
	f := newCompactFixture(t, "pi", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 2)},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: before},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: off},
		{Argv: compactReadArgv("worker"), Call: 3, Stdout: proof},
		{Argv: compactReadArgv("worker"), Call: 4, Stdout: restored},
		{Argv: compactReadArgv("worker"), Call: 5, Stdout: restored},
		{Argv: []string{"pane", "send-keys", "p1", "shift+tab"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
	})
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 0 || errText != "" {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["agent"] != "worker" || value["kind"] != "pi" || value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	if restored, ok := value["thinking_restored"].(bool); !ok || !restored {
		t.Fatalf("thinking_restored=%v want true: %v", value["thinking_restored"], value)
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "shift+tab"}); n != 2 {
		t.Fatalf("shift+tab calls=%d want 2 (one off, one restore): %#v", n, calls)
	}
	if n := countArgv(calls, compactReadArgv("worker")); n != 5 {
		t.Fatalf("recent reads=%d want 5 (before, off, proof, restore, footer check): %#v", n, calls)
	}
}

// TestCompactPiAlreadyOffNoChange verifies that a pi footer already at off is
// left as is: no shift+tab is sent and the result JSON has no thinking_restored.
func TestCompactPiAlreadyOffNoChange(t *testing.T) {
	off := "old output line\n• thinking: off\n"
	proof := "old output line\n[compaction] Compacted from 12345 tokens\n• thinking: off\n"
	f := newCompactFixture(t, "pi", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 2)},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: off},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: proof},
		{Argv: compactReadArgv("worker"), Call: 3, Stdout: proof},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
	})
	code, out, _ := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 0 {
		t.Fatalf("code=%d out=%s", code, out)
	}
	value := compactJSON(t, out)
	if value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	if _, present := value["thinking_restored"]; present {
		t.Fatalf("thinking_restored must be absent when the level was not changed: %v", value)
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "shift+tab"}); n != 0 {
		t.Fatalf("shift+tab calls=%d want 0 (footer already off): %#v", n, calls)
	}
}

// TestCompactPiNeverReachesOff verifies that when the footer never reaches off
// after the eight taps, the compact warns and still compacts; the level is
// given back and the result carries thinking_restored.
func TestCompactPiNeverReachesOff(t *testing.T) {
	stuck := "old output line\n• thinking: high\n"
	proof := "old output line\n[compaction] Compacted from 12345 tokens\n• thinking: high\n"
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 2)},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: stuck},
		// eight taps in the off phase, all re-reading the stuck footer
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: stuck},
		{Argv: compactReadArgv("worker"), Call: 3, Stdout: stuck},
		{Argv: compactReadArgv("worker"), Call: 4, Stdout: stuck},
		{Argv: compactReadArgv("worker"), Call: 5, Stdout: stuck},
		{Argv: compactReadArgv("worker"), Call: 6, Stdout: stuck},
		{Argv: compactReadArgv("worker"), Call: 7, Stdout: stuck},
		{Argv: compactReadArgv("worker"), Call: 8, Stdout: stuck},
		{Argv: compactReadArgv("worker"), Call: 9, Stdout: stuck},
		{Argv: compactReadArgv("worker"), Call: 10, Stdout: proof},
		{Argv: compactReadArgv("worker"), Call: 11, Stdout: stuck},
		{Argv: compactReadArgv("worker"), Call: 12, Stdout: stuck},
		{Argv: []string{"pane", "send-keys", "p1", "shift+tab"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
	}
	f := newCompactFixture(t, "pi", rules)
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 0 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	if restored, ok := value["thinking_restored"].(bool); !ok || !restored {
		t.Fatalf("thinking_restored=%v want true (the footer never left its level): %v", value["thinking_restored"], value)
	}
	want := "compact: could not turn 'worker' thinking off before compacting (still 'high'); compacting anyway"
	if !strings.Contains(errText, want) {
		t.Fatalf("stderr=%q want %q", errText, want)
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "shift+tab"}); n != 9 {
		t.Fatalf("shift+tab calls=%d want 9 (eight off, one restore): %#v", n, calls)
	}
}

// TestCompactPiNotRestored verifies that when the level does not come back
// after the eight restore taps, the result carries thinking_restored false and
// the stderr warning names the level the footer still shows.
func TestCompactPiNotRestored(t *testing.T) {
	before := "old output line\n• thinking: high\n"
	off := "old output line\n• thinking: off\n"
	// After the compact the footer stays at off: it never comes back to high.
	stuckOff := "old output line\n[compaction] Compacted from 12345 tokens\n• thinking: off\n"
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 2)},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: before},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: off},
		{Argv: compactReadArgv("worker"), Call: 3, Stdout: stuckOff},
		// eight restore taps, all re-reading the stuck-off footer
		{Argv: compactReadArgv("worker"), Call: 4, Stdout: stuckOff},
		{Argv: compactReadArgv("worker"), Call: 5, Stdout: stuckOff},
		{Argv: compactReadArgv("worker"), Call: 6, Stdout: stuckOff},
		{Argv: compactReadArgv("worker"), Call: 7, Stdout: stuckOff},
		{Argv: compactReadArgv("worker"), Call: 8, Stdout: stuckOff},
		{Argv: compactReadArgv("worker"), Call: 9, Stdout: stuckOff},
		{Argv: compactReadArgv("worker"), Call: 10, Stdout: stuckOff},
		{Argv: compactReadArgv("worker"), Call: 11, Stdout: stuckOff},
		{Argv: compactReadArgv("worker"), Call: 12, Stdout: stuckOff},
		{Argv: []string{"pane", "send-keys", "p1", "shift+tab"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
	}
	f := newCompactFixture(t, "pi", rules)
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 0 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	if restored, ok := value["thinking_restored"].(bool); !ok || restored {
		t.Fatalf("thinking_restored=%v want false: %v", value["thinking_restored"], value)
	}
	want := "compact: could not restore 'worker' thinking to 'high' (shows 'off'); set it by hand before the next brief"
	if !strings.Contains(errText, want) {
		t.Fatalf("stderr=%q want %q", errText, want)
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "shift+tab"}); n != 9 {
		t.Fatalf("shift+tab calls=%d want 9 (one off, eight restore): %#v", n, calls)
	}
}

// TestCompactPiTimeoutStillRestores verifies that a timeout with the thinking
// still off tries to restore the level before exiting; the timeout JSON carries
// thinking_restored.
func TestCompactPiTimeoutStillRestores(t *testing.T) {
	before := "old output line\n• thinking: high\n"
	off := "old output line\n• thinking: off\n"
	// After the send the footer shows high (no proof): the compact times out,
	// but the restore read finds the level back at high.
	noProof := "old output line\n• thinking: high\n"
	f := newCompactFixture(t, "pi", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 1)},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: before},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: off},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: noProof},
		{Argv: []string{"pane", "send-keys", "p1", "shift+tab"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
	})
	code, out, _ := f.run(t, "compact", "worker", "--timeout", "500")
	if code != 9 {
		t.Fatalf("code=%d want 9 out=%s", code, out)
	}
	value := compactJSON(t, out)
	if value["status"] != "timeout" {
		t.Fatalf("json=%v", value)
	}
	if restored, ok := value["thinking_restored"].(bool); !ok || !restored {
		t.Fatalf("thinking_restored=%v want true (restored before the timeout exit): %v", value["thinking_restored"], value)
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "shift+tab"}); n != 2 {
		t.Fatalf("shift+tab calls=%d want 2 (one off, one restore): %#v", n, calls)
	}
}

// TestCompactPiFailureAfterOffRestores (R-RC7B): a send that fails after the
// thinking went off exits 4 through DieFriction, and the level goes back first.
func TestCompactPiFailureAfterOffRestores(t *testing.T) {
	before := "old output line\n• thinking: high\n"
	off := "old output line\n• thinking: off\n"
	f := newCompactFixture(t, "pi", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 1)},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: before},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: off},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: before},
		{Argv: []string{"pane", "send-keys", "p1", "shift+tab"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Code: 1, Stderr: `{"error":{"code":"herdr_failed","message":"boom"}}`},
	})
	code, _, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 4 || !strings.Contains(errText, "could not send /compact") {
		t.Fatalf("code=%d stderr=%q", code, errText)
	}
	if strings.Contains(errText, "could not restore") {
		t.Fatalf("the level came back, yet a restore warning: %q", errText)
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "shift+tab"}); n != 2 {
		t.Fatalf("shift+tab calls=%d want 2 (one off, one restore before the exit): %#v", n, calls)
	}
}

// TestCompactClaudeNoMessagesToCompact verifies Claude Code 2.1.285's second
// nothing-to-compact string: below the echoed /compact it ends the wait as a
// success without compaction, and the same line already on screen before the
// send does not count.
func TestCompactClaudeNoMessagesToCompact(t *testing.T) {
	t.Run("the answer below the echoed /compact counts", func(t *testing.T) {
		f := newCompactFixture(t, "claude", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), Call: 1, Stdout: "old output line\n"},
			{Argv: compactReadArgv("worker"), Call: 2, Stdout: compactScreens.claudeNoMessages},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
		if code != 0 || errText != "" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		value := compactJSON(t, out)
		if value["agent"] != "worker" || value["kind"] != "claude" || value["status"] != "nothing-to-compact" {
			t.Fatalf("json=%v", value)
		}
		calls := f.calls(t)
		if n := countArgv(calls, []string{"agent", "get", "worker"}); n != 1 {
			t.Fatalf("agent get calls=%d want 1: %#v", n, calls)
		}
		if n := countArgv(calls, compactReadArgv("worker")); n != 2 {
			t.Fatalf("recent reads=%d want 2: %#v", n, calls)
		}
	})
	t.Run("the same line already on screen before the send does not count", func(t *testing.T) {
		stable := "old output line\n> /compact\nNo messages to compact\n"
		f := newCompactFixture(t, "claude", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: stable},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "800")
		if code != 9 || compactJSON(t, out)["status"] != "timeout" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
	})
}

// TestCompactLaterMentionDoesNotAnchor verifies the review's later-mention
// screen: a line that only mentions /compact after the proof is not the
// anchor, so the proof below the sent command still counts (old code: 9).
func TestCompactLaterMentionDoesNotAnchor(t *testing.T) {
	f := newCompactFixture(t, "codex", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: compactScreens.codexLaterMention},
	})
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "500")
	if code != 0 || errText != "" {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["agent"] != "worker" || value["kind"] != "codex" || value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	calls := f.calls(t)
	if n := countArgv(calls, compactReadArgv("worker")); n != 2 {
		t.Fatalf("recent reads=%d want 2 (the pre-send read, then the proof on the first poll): %#v", n, calls)
	}
	if n := countArgv(calls, []string{"pane", "send-text", "p1", "/compact"}); n != 1 {
		t.Fatalf("send-text calls=%d want 1: %#v", n, calls)
	}
}

// TestCompactWorkerDiesDuringWait verifies that gone and unavailable during
// the idle wait exit 6 and 4 right away (well before the deadline) with the
// same messages the pre-send checks use, instead of waiting out a timeout.
func TestCompactWorkerDiesDuringWait(t *testing.T) {
	cases := []struct {
		name    string
		rule    fakecli.Rule
		code    int
		wantErr string
	}{
		{
			name:    "agent_not_found after the proof is gone",
			rule:    fakecli.Rule{Stderr: `{"error":{"code":"agent_not_found","message":"gone"}}`},
			code:    6,
			wantErr: "compact: agent 'worker' is no longer live",
		},
		{
			name:    "herdr_failed after the proof is unavailable",
			rule:    fakecli.Rule{Code: 1, Stderr: `{"error":{"code":"herdr_failed","message":"boom"}}`},
			code:    4,
			wantErr: "compact: agent 'worker' is unavailable:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := tc.rule
			rule.Argv = []string{"agent", "get", "worker"}
			rule.Call = 2
			f := newCompactFixture(t, "codex", []fakecli.Rule{
				{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
				rule,
				{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
				{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
				{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: compactScreens.codexProof},
			})
			start := time.Now()
			code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
			elapsed := time.Since(start)
			if code != tc.code {
				t.Fatalf("code=%d want %d out=%s stderr=%s", code, tc.code, out, errText)
			}
			if out != "" {
				t.Fatalf("stdout on a dead worker: %q", out)
			}
			if !strings.Contains(errText, tc.wantErr) {
				t.Fatalf("stderr=%q want %q", errText, tc.wantErr)
			}
			if elapsed > 10*time.Second {
				t.Fatalf("exited after %v; the worker's death must stop the wait long before the 30 s deadline", elapsed)
			}
			calls := f.calls(t)
			if n := countArgv(calls, []string{"agent", "get", "worker"}); n != 2 {
				t.Fatalf("agent get calls=%d want 2 (pre-send plus the one that died): %#v", n, calls)
			}
		})
	}
}

// TestCompactWorkerDiesBeforeProof verifies that a worker that dies while
// the proof has not appeared yet stops the wait at once (6) instead of
// running out the deadline.
func TestCompactWorkerDiesBeforeProof(t *testing.T) {
	f := newCompactFixture(t, "codex", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stderr: `{"error":{"code":"agent_not_found","message":"gone"}}`},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: compactScreens.noProof},
	})
	start := time.Now()
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 6 || out != "" || !strings.Contains(errText, "compact: agent 'worker' is no longer live") {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, errText)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("exited after %v; the death must stop the wait long before the 30 s deadline", elapsed)
	}
}

// TestCompactFooterThinkingBoundary verifies the review's footer screens:
// `• cutoff` is not a thinking level (no warning) and `• thinking: high`
// against an effective effort of medium warns.
func TestCompactFooterThinkingBoundary(t *testing.T) {
	t.Run("`• cutoff` is not a thinking level: no warning", func(t *testing.T) {
		f := newCompactFixture(t, "pi", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), Call: 1, Stdout: "old output line\n"},
			{Argv: compactReadArgv("worker"), Call: 2, Stdout: compactScreens.piFresh},
			{Argv: compactReadArgv("worker"), Call: 3, Stdout: compactScreens.piFooterCutoff},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
		if code != 0 {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if value := compactJSON(t, out); value["status"] != "compacted" {
			t.Fatalf("json=%v", value)
		}
		if strings.Contains(errText, "shows thinking") {
			t.Fatalf("`cutoff` was read as a thinking level: %s", errText)
		}
	})
	t.Run("`• thinking: high` with effort medium warns", func(t *testing.T) {
		f := newCompactFixture(t, "pi", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), Call: 1, Stdout: "old output line\n"},
			{Argv: compactReadArgv("worker"), Call: 2, Stdout: compactScreens.piFresh},
			{Argv: compactReadArgv("worker"), Call: 3, Stdout: compactScreens.piFooterHigh},
		})
		roleFile := filepath.Join(f.root, "roles", "implementer.md")
		if err := os.WriteFile(roleFile, []byte("---\nname: implementer\nmode: edit\neffort: medium\n---\nRole body.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
		if code != 0 {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if value := compactJSON(t, out); value["status"] != "compacted" {
			t.Fatalf("json=%v", value)
		}
		want := "compact: 'worker' shows thinking 'high' after compaction; its effort is 'medium'"
		if !strings.Contains(errText, want) {
			t.Fatalf("stderr=%q want %q", errText, want)
		}
	})
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

// TestDispatchCompactClaudeNothingToCompact verifies that a claude with
// nothing to compact inside a dispatch keeps the flow: the compact note is a
// stderr line, the dispatch's own JSON line stays alone on stdout, and the
// brief is sent.
func TestDispatchCompactClaudeNothingToCompact(t *testing.T) {
	// Call 1 is the compact pre-send check; the ending is found on the first
	// poll, so calls 2 and 3 are the dispatch's pre-send state and first
	// arrival probe.
	extra := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 3, Stdout: compactStateJSON("working", 2)},
		{Argv: []string{"pane", "send-text", "w0test:p0a", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "w0test:p0a", "Enter"}, Stdout: `{"result":{}}`},
		// The pre-send read has no ending; the poll reads the answer below the
		// newly echoed /compact.
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: "old output line\n"},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: compactScreens.claudeNothing},
	}
	f := newDispatchArrivalFixture(t, "working", 1, 2, "$CURRENT_PATHS", "0", extra...)
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\tmode\targs\teffort\nworker\tw0test:p0a\tclaude\timplementer\t\t0\t\tnow\tclaude-1\ttask\timplementer\t\t\t\thigh\n"
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
	// One JSON line on stdout (the dispatch's); the nothing-to-compact note is
	// a stderr line, not a compact JSON.
	if strings.Contains(out, `"status":"nothing-to-compact"`) || strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("stdout must hold only the dispatch line: %s", out)
	}
	if !strings.Contains(errText, "dispatch: 'worker' had nothing to compact") {
		t.Fatalf("missing the nothing-to-compact note on stderr: %s", errText)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(f.bin, "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := countArgvPrefix(calls, []string{"agent", "prompt", "worker"}); n != 1 {
		t.Fatalf("prompt calls=%d want 1: %#v", n, calls)
	}
	if n := countArgv(calls, []string{"pane", "send-text", "w0test:p0a", "/compact"}); n != 1 {
		t.Fatalf("send-text calls=%d want 1: %#v", n, calls)
	}
}

// TestCompactPiWithoutEcho uses the screen pi really shows: it does not echo
// the /compact it runs, only a `[compaction]` box with `Compacted from <n>
// tokens`. A proof line that was not on screen before the send counts; the
// same line as before the send does not (rc.3 timed out on every pi worker).
func TestCompactPiWithoutEcho(t *testing.T) {
	before := "earlier reply\n/path/to/report.md\n"
	after := before + "[compaction]\nCompacted from 64,446 tokens (ctrl+o to expand)\n"
	t.Run("a new proof line without the echoed command counts", func(t *testing.T) {
		f := newCompactFixture(t, "pi", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), Call: 1, Stdout: before},
			{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: after},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
		if code != 0 || compactJSON(t, out)["status"] != "compacted" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
	})
	t.Run("the proof of an earlier compaction does not count", func(t *testing.T) {
		f := newCompactFixture(t, "pi", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: after},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "800")
		if code != 9 || compactJSON(t, out)["status"] != "timeout" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
	})
}

// TestCompactPreSendReadFailureSendsNothing: without the screen before the
// send an earlier proof would count as this one, so nothing is sent (exit 4).
func TestCompactPreSendReadFailureSendsNothing(t *testing.T) {
	f := newCompactFixture(t, "pi", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), Call: 1, Code: 1, Stderr: `{"error":{"code":"herdr_failed","message":"boom"}}`},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: "[compaction]\nCompacted from 64,446 tokens\n"},
	})
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 4 || out != "" || !strings.Contains(errText, "could not read the screen of 'worker' before sending; nothing was sent") {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, errText)
	}
	if n := countArgv(f.calls(t), []string{"pane", "send-text", "p1", "/compact"}); n != 0 {
		t.Fatalf("/compact was sent after a failed pre-send read (%d)", n)
	}
}
