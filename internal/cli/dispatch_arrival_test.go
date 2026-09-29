package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

type dispatchArrivalFixture struct {
	root   string
	state  string
	bin    string
	env    platform.Env
	brief  string
	config *core.Config
}

func newDispatchArrivalFixture(t *testing.T, initialMode string, initialSeq, nextSeq int, recent string, settle string, extra ...fakecli.Rule) *dispatchArrivalFixture {
	t.Helper()
	root := t.TempDir()
	if recent == "$CURRENT_PATHS" {
		recent = dispatchPromptEvidence(root)
	}
	state, roles, bin := filepath.Join(root, "state"), filepath.Join(root, "roles"), filepath.Join(root, "bin")
	for _, dir := range []string{filepath.Join(state, "ws", "wait"), filepath.Join(state, "ws", "briefs"), filepath.Join(state, "ws", "reports"), roles, bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(roles, "implementer.md"), []byte("---\nname: Implementer\nmode: edit\n---\nRole body.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	brief := filepath.Join(root, "brief.md")
	if err := os.WriteFile(brief, []byte("# Goal\n\nDispatch a prompt.\n# Expected result\n\nObserved.\n# Owned files\n\nnone\n# Forbidden\n\nNo commit or push.\n# Report\n\ndone\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\tmode\targs\teffort\nworker\tw0test:p0a\tcodex\timplementer\topenai\t0\t\tnow\tgpt-5\ttask\timplementer\t\t\t\thigh\n"
	if err := os.WriteFile(filepath.Join(state, "ws", "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	stateJSON := func(mode string, seq int) string {
		return `{"result":{"agent":{"agent_status":"` + mode + `","state_change_seq":` + strings.TrimSpace(string(rune('0'+seq))) + `}}}`
	}
	for i := range extra {
		if extra[i].Stdout == "$CURRENT_PATHS" {
			extra[i].Stdout = dispatchPromptEvidence(root)
		}
	}
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "list"}, ArgvPrefix: true, Stdout: `{"result":{"agents":[]}}`},
	}
	rules = append(rules, extra...)
	// Agent get call 1 is the pre-send state; call 2 is the first arrival probe.
	rules = append(rules,
		fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: stateJSON(initialMode, initialSeq)},
		fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: stateJSON(initialMode, nextSeq)},
	)
	rules = append(rules,
		fakecli.Rule{Argv: []string{"agent", "get", "worker"}, ArgvPrefix: true, Stdout: stateJSON(initialMode, nextSeq)},
		fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, ArgvPrefix: true},
		fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stdout: recent},
		fakecli.Rule{Argv: []string{"agent", "prompt", "worker"}, ArgvPrefix: true, Stdout: `{"result":{"submitted":true}}`},
		fakecli.Rule{Argv: []string{"pane", "title"}, ArgvPrefix: true, Code: 1},
	)
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
	env["HERDR_ENV"], env["HERDR_SOHO_DIR"], env["HERDR_WORKSPACE_ID"] = "1", state, "ws"
	env["HERDR_SOHO_ROLES"], env["HERDR_SOHO_SKILL_DIR"] = roles, "../../skills/herdr-soho"
	env["HERDR_SOHO_PROMPT_CHECK_SECONDS"], env["HERDR_SOHO_PROMPT_SETTLE_SECONDS"] = "1", settle
	env["HERDR_SOHO_BRIEF_LINT"], env["TMPDIR"] = "off", root
	env["HERDR_SOCKET_PATH"] = filepath.Join(root, "missing.sock")
	return &dispatchArrivalFixture{root: root, state: state, bin: bin, env: env, brief: brief, config: &core.Config{Entries: map[string]core.ConfigEntry{}}}
}

func (f *dispatchArrivalFixture) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	code := func() int {
		defer func() { platform.Stdout, platform.Stderr = oldOut, oldErr }()
		return Run(append([]string{"dispatch"}, args...), f.env)
	}()
	return code, out.String(), stderr.String()
}

func dispatchPromptEvidence(root string) string {
	var out strings.Builder
	base := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 80; i++ {
		stamp := base.Add(time.Duration(i*2) * time.Second).Format("20060102T150405")
		fmt.Fprintf(&out, "Read the file %s in full\n", filepath.Join(root, "state", "ws", "briefs", "worker-"+stamp+".md"))
	}
	return out.String()
}

func dispatchOutputStatus(t *testing.T, out string) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &value); err != nil {
		t.Fatalf("dispatch JSON %q: %v", out, err)
	}
	status, _ := value["wait_status"].(string)
	return status
}

func TestDispatchArrivalPortedCases(t *testing.T) {
	fixedNow := platform.Now
	clock := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	platform.Now = func() time.Time {
		value := clock
		clock = clock.Add(2 * time.Second)
		return value
	}
	t.Cleanup(func() { platform.Now = fixedNow })
	t.Run(`arrival: working target skips settle and reports queued from the prompt marker`, func(t *testing.T) {
		// JS: "arrival: working target skips settle and reports queued from the prompt marker"
		f := newDispatchArrivalFixture(t, "working", 5, 5, "$CURRENT_PATHS", "20")
		code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "queued" || !strings.Contains(errText, "prompt queued") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if _, err := os.Stat(filepath.Join(f.state, "ws", "wait", "worker.queued")); err != nil {
			t.Fatalf("queued marker missing: %v", err)
		}
		if !strings.Contains(mustRead(t, filepath.Join(f.state, "ws", "briefs", "worker-20260929T000000.dispatch.json")), `"arrival":"queued"`) {
			t.Fatal("sidecar did not record queued arrival")
		}
	})
	t.Run(`arrival: working target with no prompt evidence is not-received and sends no key`, func(t *testing.T) {
		// JS: "arrival: working target with no prompt evidence is not-received and sends no key"
		f := newDispatchArrivalFixture(t, "working", 5, 5, "old prompt from another dispatch\n", "0")
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" {
			t.Fatalf("code=%d out=%s", code, out)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		for _, call := range calls {
			if len(call.Argv) > 1 && call.Argv[1] == "send-keys" {
				t.Fatalf("occupied target got a key: %#v", call.Argv)
			}
		}
	})
	t.Run(`arrival: a working target with a truncated queued prompt counts as queued`, func(t *testing.T) {
		// JS: "arrival: working target with no prompt evidence is not-received and sends no key" (the queued-proof side)
		// The queue shows the dispatched prompt with the path cut, so the full path never appears.
		f := newDispatchArrivalFixture(t, "working", 5, 5, "old work output\nRead the file "+filepath.Join("state", "ws", "briefs", "work")+"…\n", "0")
		code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "queued" || !strings.Contains(errText, "prompt queued: 'worker' is working") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if _, err := os.Stat(filepath.Join(f.state, "ws", "wait", "worker.queued")); err != nil {
			t.Fatalf("queued marker missing: %v", err)
		}
		sidecars, err := filepath.Glob(filepath.Join(f.state, "ws", "briefs", "worker-*.dispatch.json"))
		if err != nil || len(sidecars) == 0 {
			t.Fatalf("dispatch sidecar missing: %v", err)
		}
		if !strings.Contains(mustRead(t, sidecars[0]), `"arrival":"queued"`) {
			t.Fatalf("sidecar did not record queued arrival: %s", mustRead(t, sidecars[0]))
		}
	})
	t.Run(`arrival: a working target with another dispatch's queued prompt stays not-received`, func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "working", 5, 5, "old work output\nRead the file /…/briefs/review-20260928T090000.md in full\n", "0")
		code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" || !strings.Contains(errText, "not confirmed") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if _, err := os.Stat(filepath.Join(f.state, "ws", "wait", "worker.not-received")); err != nil {
			t.Fatalf("not-received marker missing: %v", err)
		}
	})
	t.Run(`arrival: amend ignores a generic marker from the previous prompt`, func(t *testing.T) {
		// JS: "arrival: amend ignores a generic marker from the previous prompt"
		f := newDispatchArrivalFixture(t, "working", 5, 5, "Read the file /tmp/old/previous-brief.md in full\n", "0")
		previous := filepath.Join(f.state, "ws", "reports", "previous.md")
		if err := os.WriteFile(previous, []byte("old report\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.state, "ws", "last-report-worker"), []byte(previous+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, _ := f.run(t, "worker", f.brief, "--amend", "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run(`arrival: s1 control with no prompt evidence stays not-received`, func(t *testing.T) {
		// JS: "arrival: s1 control with no prompt evidence stays not-received"
		f := newDispatchArrivalFixture(t, "working", 5, 5, "old work output without a prompt marker\n", "0")
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run(`arrival: working target with a moved seq counts as received`, func(t *testing.T) {
		// JS: "arrival: working target with a moved seq counts as received"
		f := newDispatchArrivalFixture(t, "working", 5, 6, "", "0")
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "submitted" {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run(`arrival: working target that becomes idle after its seq moves remains queued`, func(t *testing.T) { // Mutation captured: treating any moved seq as arrival reports submitted after the previous turn ends.
		f := newDispatchArrivalFixture(t, "working", 5, 6, "$CURRENT_PATHS", "0",
			fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: `{"result":{"agent":{"agent_status":"idle","state_change_seq":6}}}`},
			fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 3, Stdout: `{"result":{"agent":{"agent_status":"idle","state_change_seq":6}}}`},
		)
		code, out, stderr := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "queued" || !strings.Contains(stderr, "prompt queued") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, stderr)
		}
	})
	t.Run(`arrival: an input-box prompt sends Enter before stale auth becomes not-received`, func(t *testing.T) { // Mutation captured: checking stale auth before the input box omits the required Enter.
		screen := "Error: Incorrect API key\nRead the file /already/on/screen\n"
		f := newDispatchArrivalFixture(t, "blocked", 7, 7, "Error: Incorrect API key\n", "0",
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 1, ArgvPrefix: true, Stdout: screen},
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 2, ArgvPrefix: true, Stdout: screen},
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 3, ArgvPrefix: true, Stdout: screen},
		)
		code, _, stderr := f.run(t, "worker", f.brief, "--no-wait")
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if code != 15 || !strings.Contains(stderr, "sat in the input box; sent Enter") || !strings.Contains(stderr, "after an Enter") || strings.Contains(stderr, "after its block on a provider auth error") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if got := countDispatchCalls(calls, "send-keys"); got != 1 {
			t.Fatalf("send-keys calls=%d, want 1; calls=%#v", got, calls)
		}
	})
	t.Run(`arrival (a): idle target with changing screen, loses both prompts -> 1 resend and not-received exit 15`, func(t *testing.T) {
		// JS: "arrival (a): idle target with changing screen, loses both prompts -> 1 resend and not-received exit 15"
		f := newDispatchArrivalFixture(t, "idle", 1, 1, "old output\n", "0",
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 1, Stdout: "boot before\n", ArgvPrefix: true},
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 2, Stdout: "boot redraw\n", ArgvPrefix: true},
		)
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" {
			t.Fatalf("code=%d out=%s", code, out)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if got := countDispatchCalls(calls, "prompt"); got != 2 {
			t.Fatalf("prompt attempts=%d, want initial + one resend", got)
		}
	})
	t.Run(`arrival (b): idle target receives resend (seq changes) -> received with 1 resend`, func(t *testing.T) {
		// JS: "arrival (b): idle target receives resend (seq changes) -> received with 1 resend"
		// JS: "dispatch: an ignored prompt is resent once and the JSON carries resent"
		f := newDispatchArrivalFixture(t, "idle", 1, 1, "old output\n", "0",
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 1, Stdout: "screen before\n", ArgvPrefix: true},
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 2, Stdout: "screen redraw\n", ArgvPrefix: true},
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 3, Stdout: "screen before\n", ArgvPrefix: true},
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 4, Stdout: "screen after resend\n", ArgvPrefix: true},
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "recent-unwrapped"}, Call: 3, Stdout: "$CURRENT_PATHS", ArgvPrefix: true},
			fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 3, Stdout: `{"result":{"agent":{"agent_status":"idle","state_change_seq":1}}}`},
			fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 4, Stdout: `{"result":{"agent":{"agent_status":"idle","state_change_seq":1}}}`},
			fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 5, Stdout: `{"result":{"agent":{"agent_status":"working","state_change_seq":2}}}`},
		)
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "submitted" || !strings.Contains(out, `"resent":true`) {
			calls, _ := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
			config, _ := os.ReadFile(filepath.Join(f.bin, "herdr.json"))
			t.Fatalf("code=%d out=%s calls=%#v config=%s", code, out, calls, config)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if got := countDispatchCalls(calls, "prompt"); got != 2 {
			t.Fatalf("prompt attempts=%d, want 2", got)
		}
	})
	t.Run(`arrival (c): normal target turns working with changed seq on first prompt -> received without resend`, func(t *testing.T) {
		// JS: "arrival (c): normal target turns working with changed seq on first prompt -> received without resend"
		// JS: "dispatch: arrival confirmed by the state turning working → no resend"
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0",
			fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: `{"result":{"agent":{"agent_status":"working","state_change_seq":2}}}`},
		)
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "submitted" {
			t.Fatalf("code=%d out=%s", code, out)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if got := countDispatchCalls(calls, "prompt"); got != 1 {
			t.Fatalf("prompt attempts=%d, want 1", got)
		}
	})
	t.Run(`arrival (d): settle wait waits for interactive_ready and two identical visible screens before prompt 1, respects settle=0`, func(t *testing.T) {
		// JS: "arrival (d): settle wait waits for interactive_ready and two identical visible screens before prompt 1, respects settle=0"
		f := newDispatchArrivalFixture(t, "idle", 1, 1, "", "1",
			fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: `{"result":{"agent":{"agent_status":"idle","state_change_seq":1,"interactive_ready":true}}}`},
			fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 3, Stdout: `{"result":{"agent":{"agent_status":"idle","state_change_seq":1}}}`},
			fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 4, Stdout: `{"result":{"agent":{"agent_status":"working","state_change_seq":2}}}`},
		)
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "submitted" {
			t.Fatalf("code=%d out=%s", code, out)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		promptAt, readsBefore := -1, 0
		for i, call := range calls {
			if len(call.Argv) > 1 && call.Argv[1] == "prompt" {
				promptAt = i
				break
			}
		}
		for _, call := range calls[:promptAt] {
			if len(call.Argv) > 1 && call.Argv[1] == "read" {
				readsBefore++
			}
		}
		if readsBefore < 2 {
			t.Fatalf("visible settle reads before prompt=%d, want at least 2", readsBefore)
		}
		zero := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0",
			fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: `{"result":{"agent":{"agent_status":"working","state_change_seq":2}}}`},
		)
		code, out, _ = zero.run(t, "worker", zero.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "submitted" {
			t.Fatalf("settle=0 code=%d out=%s", code, out)
		}
		zeroCalls, err := fakecli.ReadCalls(filepath.Join(zero.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		zeroPromptAt, zeroReads := -1, 0
		for i, call := range zeroCalls {
			if len(call.Argv) > 1 && call.Argv[1] == "prompt" {
				zeroPromptAt = i
				break
			}
		}
		for _, call := range zeroCalls[:zeroPromptAt] {
			if len(call.Argv) > 1 && call.Argv[1] == "read" && len(call.Argv) > 4 && call.Argv[4] == "visible" {
				zeroReads++
			}
		}
		if zeroReads != 1 {
			t.Fatalf("visible reads before prompt with settle=0=%d, want 1", zeroReads)
		}
	})
	t.Run(`arrival (e): screen changed and composed prompt path visible outside last 3 lines -> received without resend`, func(t *testing.T) {
		// JS: "arrival (e): screen changed and composed prompt path visible outside last 3 lines -> received without resend"
		f := newDispatchArrivalFixture(t, "idle", 1, 1, "$CURRENT_PATHS", "0",
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 1, Stdout: "boot\n", ArgvPrefix: true},
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 2, Stdout: "screen changed\n", ArgvPrefix: true},
		)
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "submitted" {
			t.Fatalf("code=%d out=%s", code, out)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if got := countDispatchCalls(calls, "prompt"); got != 1 {
			t.Fatalf("prompt attempts=%d, want no resend", got)
		}
	})
	t.Run(`arrival (f): working target with prompt visible is queued without Enter or resend, including amend`, func(t *testing.T) {
		// JS: "arrival (f): working target with prompt visible is queued without Enter or resend, including amend"
		f := newDispatchArrivalFixture(t, "working", 5, 5, "$CURRENT_PATHS", "0")
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "queued" {
			t.Fatalf("code=%d out=%s", code, out)
		}
		code, out, _ = f.run(t, "worker", f.brief, "--amend", "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "queued" {
			t.Fatalf("amend code=%d out=%s", code, out)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if got := countDispatchCalls(calls, "prompt"); got != 2 {
			t.Fatalf("prompt attempts=%d, want one per dispatch", got)
		}
	})
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func countDispatchCalls(calls []fakecli.Call, command string) int {
	count := 0
	for _, call := range calls {
		if len(call.Argv) > 1 && call.Argv[0] == "agent" && call.Argv[1] == command {
			count++
		}
	}
	return count
}
