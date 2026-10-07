package peer_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/peer"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// streamSnapshot builds a minimal snapshot holding one workspace, one tab and
// one pane/agent: the pane id is "w<label>:p1", so its reference is
// "<machine>/w<label>:p1".
func streamSnapshot(label, name string) string {
	ws := "w" + label
	return fmt.Sprintf(`{"workspaces":[{"workspace_id":"%s","label":"%s"}],"tabs":[{"tab_id":"%s:t1","label":"main"}],"panes":[{"pane_id":"%s:p1","workspace_id":"%s","tab_id":"%s:t1","agent_status":"working","cwd":"/srv/%s"}],"agents":[{"pane_id":"%s:p1","name":"%s","agent":"pi","agent_status":"working"}]}`, ws, label, ws, ws, ws, ws, label, ws, name)
}

// streamRefs extracts the leading reference cell of each TSV row.
func streamRefs(out string) []string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	refs := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		refs = append(refs, strings.SplitN(line, "\t", 2)[0])
	}
	return refs
}

// streamCallCounts reads the fake herdr call log and returns how often each
// exact argv was invoked.
func streamCallCounts(t *testing.T, f *fixture) map[string]int {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
	if err != nil {
		t.Fatalf("read fake herdr calls: %v", err)
	}
	counts := map[string]int{}
	for _, call := range calls {
		counts[strings.Join(call.Argv, " ")]++
	}
	return counts
}

func TestDiscoverSessionsLocalFirstArrivalOrder(t *testing.T) {
	t.Run("local publishes first and remotes follow in completion order", func(t *testing.T) { // 决策: 本地先于枚举, 远程按到达顺序串行回调
		f := newFixture(t, []fakecli.Rule{
			{Argv: []string{"api", "snapshot"}, Stdout: `{"result":{"snapshot":` + localSnapshot + `}}`},
			{Argv: []string{"--machine", "fast", "api", "snapshot"}, Stdout: `{"result":{"snapshot":` + streamSnapshot("fast", "agent-fast") + `}}`, Delay: 150},
			{Argv: []string{"--machine", "slow", "api", "snapshot"}, Stdout: `{"result":{"snapshot":` + streamSnapshot("slow", "agent-slow") + `}}`, Delay: 600},
		})
		batches := []peer.SessionResult{}
		published := []time.Time{}
		start := time.Now()
		result := peer.DiscoverSessions(peer.DiscoverOptions{Env: f.env, Machines: []string{"local", "fast", "slow"}, TimeoutMS: 3000}, func(batch peer.SessionResult) {
			batches = append(batches, batch)
			published = append(published, time.Now())
		})
		elapsed := time.Since(start)
		if len(batches) != 3 {
			t.Fatalf("batches=%+v elapsed=%v", batches, elapsed)
		}
		if len(batches[0].Entries) != 2 || batches[0].Entries[0].Machine != "local" {
			t.Fatalf("first batch is not the local one: %+v", batches[0])
		}
		if len(batches[1].Entries) != 1 || batches[1].Entries[0].Ref != "fast/wfast:p1" {
			t.Fatalf("second batch=%+v (want fast, the earlier remote)", batches[1])
		}
		if len(batches[2].Entries) != 1 || batches[2].Entries[0].Ref != "slow/wslow:p1" {
			t.Fatalf("third batch=%+v (want slow, the later remote)", batches[2])
		}
		if len(result.Entries) != 4 || len(result.Failures) != 0 || result.ListCause != "" {
			t.Fatalf("aggregated=%+v", result)
		}
		if !published[0].Before(published[1]) || elapsed > 2000*time.Millisecond {
			t.Fatalf("published=%v elapsed=%v", published, elapsed)
		}
	})
	t.Run("documented defaults: 30000ms global deadline and the bound of 4", func(t *testing.T) {
		if peer.DiscoverTimeoutMS != 30000 || peer.DiscoverMaxConcurrency != 4 {
			t.Fatalf("defaults: timeout=%d concurrency=%d", peer.DiscoverTimeoutMS, peer.DiscoverMaxConcurrency)
		}
	})
	t.Run("a stopped enumeration leaves the local entries and reports the deadline", func(t *testing.T) { // 决策: 枚举受同一全局期限约束, 本地已先行
		dir := t.TempDir()
		f := newFixtureAt(t, dir, []fakecli.Rule{
			{Argv: []string{"api", "snapshot"}, Stdout: `{"result":{"snapshot":` + localSnapshot + `}}`},
			{Argv: []string{"machine", "list", "--json"}, WaitFile: filepath.Join(dir, "never-list")},
		})
		f.env["FAKECLI_WAIT_FILE_TIMEOUT_MS"] = "10000"
		start := time.Now()
		result := peer.DiscoverSessions(peer.DiscoverOptions{Env: f.env, All: true, TimeoutMS: 800}, func(peer.SessionResult) {})
		elapsed := time.Since(start)
		if len(result.Entries) != 2 || result.Entries[0].Ref != "local/w1:p1" {
			t.Fatalf("entries=%+v elapsed=%v", result.Entries, elapsed)
		}
		if !strings.Contains(result.ListCause, "timed out") {
			t.Fatalf("list cause=%q elapsed=%v", result.ListCause, elapsed)
		}
		// Near the 800ms deadline (calibrated over the race-instrumented
		// fake's re-exec cost; the fixture removes the race runtime's 1s
		// at-exit wait via GORACE=atexit_sleep_ms=0), not the fake's own
		// 10s wait_file cap.
		if elapsed < 600*time.Millisecond || elapsed > 8000*time.Millisecond {
			t.Fatalf("elapsed=%v (want near the 800ms deadline)", elapsed)
		}
	})
	t.Run("a remote not started by the deadline reports a deadline cause and spawns nothing", func(t *testing.T) { // 决策: 未启动任务必须有期限诊断
		dir := t.TempDir()
		rules := []fakecli.Rule{{Argv: []string{"api", "snapshot"}, Stdout: `{"result":{"snapshot":` + localSnapshot + `}}`}}
		machines := []string{"local"}
		for i := 1; i <= 5; i++ {
			label := fmt.Sprintf("m%d", i)
			machines = append(machines, label)
			// The fake waits for a file that never appears; the deadline must
			// kill it (or, for the fifth machine, never start it at all).
			rules = append(rules, fakecli.Rule{Argv: []string{"--machine", label, "api", "snapshot"}, WaitFile: filepath.Join(dir, "never-"+label)})
		}
		f := newFixtureAt(t, dir, rules)
		f.env["FAKECLI_WAIT_FILE_TIMEOUT_MS"] = "10000"
		// The subprocess timeout rounds the remaining budget to milliseconds.
		// Keep an expired remote transport occupied until the shared context
		// expires, so that rounding cannot free a slot for the fifth query.
		// The real fake CLI calls and timeout results remain observable.
		run := func(exe string, args []string, opts platform.RunOptions) platform.RunResult {
			result := platform.RunCli(exe, args, opts)
			if len(args) > 0 && args[0] == "--machine" && result.TimedOut {
				<-opts.Context.Done()
			}
			return result
		}
		start := time.Now()
		result := peer.DiscoverSessions(peer.DiscoverOptions{Env: f.env, Machines: machines, TimeoutMS: 800, Run: run}, func(peer.SessionResult) {})
		elapsed := time.Since(start)
		deadlines, timeouts := 0, 0
		for _, failure := range result.Failures {
			switch {
			case failure.Machine == "local":
				t.Fatalf("local failed: %+v", failure)
			case strings.Contains(failure.Cause, "deadline reached before querying"):
				deadlines++
			case strings.Contains(failure.Cause, "timed out"):
				timeouts++
			default:
				t.Fatalf("unexpected failure %+v elapsed=%v", failure, elapsed)
			}
		}
		// Four of the five start under the bound of 4 and are killed by the
		// deadline; the fifth never starts and gets the deadline cause.
		if deadlines != 1 || timeouts != 4 {
			t.Fatalf("deadlines=%d timeouts=%d failures=%+v elapsed=%v", deadlines, timeouts, result.Failures, elapsed)
		}
		counts := streamCallCounts(t, f)
		remote := 0
		for argv := range counts {
			if strings.HasPrefix(argv, "--machine ") {
				remote++
			}
		}
		if remote != 4 {
			t.Fatalf("remote snapshot calls=%d (want 4, the unstarted one spawns nothing): %v", remote, counts)
		}
		if elapsed < 600*time.Millisecond || elapsed > 9000*time.Millisecond {
			t.Fatalf("elapsed=%v (want near the 800ms deadline)", elapsed)
		}
	})
	t.Run("five remotes run under the bound of 4: two waves, not one, not serial", func(t *testing.T) { // 决策: 并发上限 4, 无界 goroutines 不允许
		dir := t.TempDir()
		rules := []fakecli.Rule{{Argv: []string{"api", "snapshot"}, Stdout: `{"result":{"snapshot":` + localSnapshot + `}}`}}
		machines := []string{"local"}
		for i := 1; i <= 5; i++ {
			label := fmt.Sprintf("c%d", i)
			machines = append(machines, label)
			rules = append(rules, fakecli.Rule{Argv: []string{"--machine", label, "api", "snapshot"}, Stdout: `{"result":{"snapshot":` + streamSnapshot(label, "agent-"+label) + `}}`, Delay: 400})
		}
		f := newFixtureAt(t, dir, rules)
		start := time.Now()
		result := peer.DiscoverSessions(peer.DiscoverOptions{Env: f.env, Machines: machines, TimeoutMS: 5000, Concurrency: 4}, func(peer.SessionResult) {})
		elapsed := time.Since(start)
		if len(result.Entries) != 7 || len(result.Failures) != 0 {
			t.Fatalf("result=%+v elapsed=%v", result, elapsed)
		}
		// One wave (no bound) finishes in ~0.45s; serial takes ~2.2s; the
		// bound of 4 takes two 400ms waves on top of the local snapshot
		// (calibrated over the race-instrumented fake's ~20ms re-exec, the
		// fixture removing the race runtime's 1s at-exit wait).
		if elapsed < 800*time.Millisecond || elapsed > 2000*time.Millisecond {
			t.Fatalf("elapsed=%v (want two 400ms waves under the bound of 4)", elapsed)
		}
	})
	t.Run("a failing remote is kept as a failure without losing the others", func(t *testing.T) { // 决策: 远端失败保留部分结果
		f := newFixture(t, []fakecli.Rule{
			{Argv: []string{"api", "snapshot"}, Stdout: `{"result":{"snapshot":` + localSnapshot + `}}`},
			{Argv: []string{"--machine", "ok", "api", "snapshot"}, Stdout: `{"result":{"snapshot":` + streamSnapshot("ok", "agent-ok") + `}}`},
			{Argv: []string{"--machine", "bad", "api", "snapshot"}, Stderr: "machine down", Code: 1},
		})
		result := peer.DiscoverSessions(peer.DiscoverOptions{Env: f.env, Machines: []string{"local", "ok", "bad"}, TimeoutMS: 3000}, func(peer.SessionResult) {})
		refs := map[string]bool{}
		for _, e := range result.Entries {
			refs[e.Ref] = true
		}
		if len(refs) != 3 || !refs["local/w1:p1"] || !refs["local/w1:p2"] || !refs["ok/wok:p1"] {
			t.Fatalf("refs=%v", refs)
		}
		if len(result.Failures) != 1 || result.Failures[0].Machine != "bad" || result.Failures[0].Cause != "machine down" {
			t.Fatalf("failures=%+v", result.Failures)
		}
	})
	t.Run("a failing local keeps the remote entries and names local", func(t *testing.T) {
		f := newFixture(t, []fakecli.Rule{
			{Argv: []string{"api", "snapshot"}, Stderr: "local down", Code: 1},
			{Argv: []string{"--machine", "ok", "api", "snapshot"}, Stdout: `{"result":{"snapshot":` + streamSnapshot("ok", "agent-ok") + `}}`},
		})
		result := peer.DiscoverSessions(peer.DiscoverOptions{Env: f.env, Machines: []string{"local", "ok"}, TimeoutMS: 3000}, func(peer.SessionResult) {})
		if len(result.Entries) != 1 || result.Entries[0].Ref != "ok/wok:p1" {
			t.Fatalf("entries=%+v", result.Entries)
		}
		if len(result.Failures) != 1 || result.Failures[0].Machine != "local" || result.Failures[0].Cause != "local down" {
			t.Fatalf("failures=%+v", result.Failures)
		}
	})
	t.Run("duplicate machine labels are queried once and never duplicated", func(t *testing.T) {
		f := newFixture(t, []fakecli.Rule{
			{Argv: []string{"api", "snapshot"}, Stdout: `{"result":{"snapshot":` + localSnapshot + `}}`},
			{Argv: []string{"--machine", "ok", "api", "snapshot"}, Stdout: `{"result":{"snapshot":` + streamSnapshot("ok", "agent-ok") + `}}`},
		})
		result := peer.DiscoverSessions(peer.DiscoverOptions{Env: f.env, Machines: []string{"local", "ok", "local", "ok"}, TimeoutMS: 3000}, func(peer.SessionResult) {})
		if len(result.Entries) != 3 || len(result.Failures) != 0 {
			t.Fatalf("result=%+v", result)
		}
		counts := streamCallCounts(t, f)
		if counts["api snapshot"] != 1 || counts["--machine ok api snapshot"] != 1 {
			t.Fatalf("call counts=%v (each machine queried once)", counts)
		}
	})
}
