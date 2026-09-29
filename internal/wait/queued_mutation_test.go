package wait

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestQueuedBlockedClearsMarker(t *testing.T) { // JS: "wait: blocked and gone clear queued markers while unavailable preserves retry state"
	f := newQueuedProbeFixture(t, "blocked", "5", "Allow command?\nPress enter to confirm or esc to cancel\n", map[string]string{
		"queued":      "1 5 /tmp/worker-brief.md\n",
		"enter-retry": "1 1234567890\n",
	})
	if got := f.probe(""); got != "working" {
		t.Fatalf("first blocked probe=%q", got)
	}
	if _, err := os.Stat(f.marker("queued")); err != nil {
		t.Fatalf("queued marker cleared before blocked confirmation: %v", err)
	}
	if got := f.probe(""); got != "blocked" {
		t.Fatalf("confirmed blocked probe=%q", got)
	}
	if _, err := os.Stat(f.marker("queued")); !os.IsNotExist(err) {
		t.Fatalf("queued marker remains after blocked confirmation: err=%v", err)
	}
}

func TestQueuedUnavailablePreservesRetryState(t *testing.T) { // JS: "wait: blocked and gone clear queued markers while unavailable preserves retry state"
	f := newQueuedProbeFixture(t, "unavailable", "", "", map[string]string{
		"queued":      "1 5 /tmp/worker-brief.md\n",
		"enter-retry": "1 1234567890\n",
	})
	if got := f.probe(""); !strings.HasPrefix(got, "unavailable\t") {
		t.Fatalf("unavailable probe=%q", got)
	}
	for name, want := range map[string]string{"queued": "1 5 /tmp/worker-brief.md\n", "enter-retry": "1 1234567890\n"} {
		got, err := os.ReadFile(f.marker(name))
		if err != nil || string(got) != want {
			t.Errorf("%s state=%q err=%v, want preserved %q", name, got, err, want)
		}
	}
}

func TestQueuedReportArrivalClearsMarkerAfterSeqChange(t *testing.T) { // JS: "wait: queued marker is cleared when the worker has a new seq and the report completes"
	f := newQueuedProbeFixtureWithSequences(t, "working", []string{"5", "6"}, "working\n", map[string]string{
		"queued": "1 5 /tmp/worker-brief.md\n",
	})
	report := filepath.Join(f.sd, "reports", "worker.md")
	if err := os.MkdirAll(filepath.Dir(report), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got := f.probe(report); got != "working" {
		t.Fatalf("probe before report arrival=%q", got)
	}
	if _, err := os.Stat(f.marker("queued")); err != nil {
		t.Fatalf("same-seq working probe cleared queued marker: %v", err)
	}
	state := herdr.AgentState("worker", f.env, herdr.Timeout, nil)
	if fmt.Sprint(state.Seq) != "6" {
		t.Fatalf("state_change_seq=%v, want 6", state.Seq)
	}
	if err := os.WriteFile(report, []byte("# Report\nfinished\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := f.probe(report); got != "pending" {
		t.Fatalf("first report probe=%q", got)
	}
	if _, err := os.Stat(f.marker("queued")); !os.IsNotExist(err) {
		t.Fatalf("queued marker remains after report arrival: err=%v", err)
	}
}

func TestQueuedWorkingSameSeqKeepsMarker(t *testing.T) { // JS: "wait: a queued marker survives a working probe with the same seq"
	marker := "1 5 /tmp/worker-brief.md\n"
	f := newQueuedProbeFixture(t, "working", "5", "working\n", map[string]string{"queued": marker})
	if got := f.probe(""); got != "working" {
		t.Fatalf("working probe=%q", got)
	}
	if got, err := os.ReadFile(f.marker("queued")); err != nil || string(got) != marker {
		t.Fatalf("queued marker=%q err=%v", got, err)
	}
}

func TestQueuedRetryUsesOriginalEpochAndStartsRetryNow(t *testing.T) { // JS: "wait: queued prompt leaves working and is retried with Enter when still in the input box"
	fixedNow := time.Unix(2_000_000_000, 0)
	previousNow := platform.Now
	platform.Now = func() time.Time { return fixedNow }
	t.Cleanup(func() { platform.Now = previousNow })
	prompt := "/tmp/worker-brief.md"
	f := newQueuedProbeFixture(t, "idle", "5", "Read the file "+prompt+" in full and execute it.\n", map[string]string{
		"queued": "1 5 " + prompt + "\n",
	})
	if got := f.probe(""); got != "working" {
		t.Fatalf("queued retry probe=%q", got)
	}
	calls := f.calls()
	if !hasCall(calls, "agent send-keys worker enter") {
		t.Fatalf("old queued epoch did not retry Enter: %#v", calls)
	}
	retry, err := os.ReadFile(f.marker("enter-retry"))
	if err != nil || !strings.HasPrefix(string(retry), "1 ") {
		t.Fatalf("retry marker=%q err=%v", retry, err)
	}
	parts := strings.Fields(string(retry))
	if len(parts) != 2 || parts[1] != "2000000000" {
		t.Fatalf("retry epoch=%q, want the current fixed epoch", retry)
	}
}

func TestQueuedPathUsesOnlyFinalThreeInputLines(t *testing.T) { // JS: "wait: queued prompt with a moved seq and outside the input box ends not-received and persists the result"
	prompt := "/tmp/worker-brief.md"
	lines := []string{"Read the file " + prompt + " in full and execute it."}
	for i := 2; i <= 15; i++ {
		lines = append(lines, fmt.Sprintf("output line %d", i))
	}
	f := newQueuedProbeFixture(t, "idle", "9", strings.Join(lines, "\n")+"\n", map[string]string{
		"queued": "1 5 " + prompt + "\n",
	})
	if got := f.probe(""); got != "not-received" {
		t.Fatalf("prompt outside the final three lines was treated as in the input: %q", got)
	}
	if hasCall(f.calls(), "agent send-keys worker enter") {
		t.Fatal("prompt echo outside the final three lines received Enter")
	}
}

func TestBlockedClearsQueuedAndRetryMarkers(t *testing.T) { // JS: "wait: blocked and gone clear queued markers while unavailable preserves retry state"
	f := newQueuedProbeFixture(t, "blocked", "5", "Allow command?\nPress enter to confirm or esc to cancel\n", map[string]string{
		"blocked":     "\n",
		"queued":      "1 5 /tmp/worker-brief.md\n",
		"enter-retry": "1 1234567890\n",
	})
	if got := f.probe(""); got != "blocked" {
		t.Fatalf("blocked probe=%q", got)
	}
	for _, name := range []string{"queued", "enter-retry"} {
		if _, err := os.Stat(f.marker(name)); !os.IsNotExist(err) {
			t.Errorf("%s marker remains after blocked handling: err=%v", name, err)
		}
	}
}

func TestAuthProviderFailureClearsQueuedMarker(t *testing.T) { // JS: "wait: an auth failure returns provider-error on the first probe"
	f := newQueuedProbeFixture(t, "idle", "5", "Error: 401 Unauthorized: Incorrect API key provided\n", map[string]string{
		"queued": "1 5 /tmp/worker-brief.md\n",
	})
	if got := f.probe(""); got != "provider-error" {
		t.Fatalf("auth probe=%q", got)
	}
	if _, err := os.Stat(f.marker("queued")); !os.IsNotExist(err) {
		t.Fatalf("queued marker remains after terminal auth error: err=%v", err)
	}
}

func TestGoneClearsQueuedMarker(t *testing.T) { // JS: "wait: blocked and gone clear queued markers while unavailable preserves retry state"
	f := newQueuedProbeFixture(t, "gone", "", "", map[string]string{
		"queued":      "1 5 /tmp/worker-brief.md\n",
		"enter-retry": "1 1234567890\n",
	})
	if got := f.probe(""); got != "gone" {
		t.Fatalf("gone probe=%q", got)
	}
	for _, name := range []string{"queued", "enter-retry"} {
		if _, err := os.Stat(f.marker(name)); !os.IsNotExist(err) {
			t.Errorf("%s marker remains after gone result: err=%v", name, err)
		}
	}
}

type queuedProbeFixture struct {
	sd  string
	bin string
	env platform.Env
	ctx *core.Config
}

func newQueuedProbeFixture(t *testing.T, state, seq, screen string, markers map[string]string) *queuedProbeFixture {
	return newQueuedProbeFixtureWithSequences(t, state, []string{seq}, screen, markers)
}

func newQueuedProbeFixtureWithSequences(t *testing.T, state string, sequences []string, screen string, markers map[string]string) *queuedProbeFixture {
	t.Helper()
	base := t.TempDir()
	f := &queuedProbeFixture{sd: filepath.Join(base, "state", "ws"), bin: filepath.Join(base, "bin"), ctx: &core.Config{Entries: map[string]core.ConfigEntry{}}}
	waitDir := filepath.Join(f.sd, "wait")
	if err := os.MkdirAll(waitDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.sd, "agents.tsv"), []byte("# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\nworker\tp0a\tclaude\timplementer\t\t\t\t\t\t\t\t\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, contents := range markers {
		if err := os.WriteFile(filepath.Join(waitDir, "worker."+name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var rules []fakecli.Rule
	if state == "gone" {
		rules = append(rules, fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Stderr: `{"error":{"code":"agent_not_found"}}`, Code: 137})
	} else if state == "unavailable" {
		rules = append(rules, fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Stderr: `{"error":{"code":"permission_denied","message":"permission denied"}}`, Code: 1})
	} else {
		for i, seq := range sequences {
			status := fmt.Sprintf(`{"result":{"agent":{"agent_status":%q`, state)
			if seq != "" {
				status += `,"state_change_seq":` + seq
			}
			status += `}}}`
			call := 0
			if len(sequences) > 1 {
				call = i + 1
			}
			rules = append(rules, fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: call, Stdout: status})
		}
	}
	for _, read := range [][]string{
		{"agent", "read", "worker", "--source", "visible", "--lines", "20"},
		{"agent", "read", "worker", "--source", "visible", "--lines", "40"},
		{"agent", "read", "worker", "--source", "visible"},
		{"agent", "read", "worker", "--source", "recent-unwrapped", "--lines", "40"},
	} {
		value := screen
		if read[4] == "recent-unwrapped" {
			value = screen
		}
		rules = append(rules, fakecli.Rule{Argv: read, Stdout: value})
	}
	rules = append(rules, fakecli.Rule{Argv: []string{"agent", "send-keys", "worker", "enter"}})
	if _, err := fakecli.Install(t, f.bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	f.env = platform.Env{"PATH": f.bin, "HERDR_SOHO_FAKECLI_CONFIG": f.bin, "HERDR_SOCKET_PATH": filepath.Join(base, "none.sock"), "HERDR_SOHO_WAIT_POLL_MS": "1"}
	return f
}

func (f *queuedProbeFixture) marker(name string) string {
	return filepath.Join(f.sd, "wait", "worker."+name)
}

func (f *queuedProbeFixture) probe(report string) string {
	return ProbeAgent(f.sd, "worker", report, f.ctx, f.env)
}

func (f *queuedProbeFixture) calls() []fakecli.Call {
	calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
	if err != nil {
		return nil
	}
	return calls
}

func hasCall(calls []fakecli.Call, expected string) bool {
	for _, call := range calls {
		if strings.Join(call.Argv, " ") == expected {
			return true
		}
	}
	return false
}
