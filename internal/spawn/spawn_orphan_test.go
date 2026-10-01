package spawn

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// startAgentExit runs startAgent on a spawn-created pane (created=true) and
// returns the DieFriction exit and message.
func startAgentExit(t *testing.T, o spawnOptions, kind string, env platform.Env) (code int, msg string) {
	t.Helper()
	func() {
		defer func() {
			value := recover()
			if value == nil {
				t.Fatal("startAgent returned without dying")
			}
			if e, ok := value.(*platform.ExitError); ok {
				code, msg = e.Code, e.Msg
				return
			}
			t.Fatalf("recovered=%#v", value)
		}()
		startAgent(o, kind, env, true, nil)
	}()
	return code, msg
}

func TestStartAgentFailedCloseRechecksThePane(t *testing.T) {
	// A spawn-created pane whose agent start failed and whose pane close
	// failed: the pane is re-queried with `herdr pane get` before reporting.
	const screen = "screen line 1\nscreen line 2\n"
	const tail = "screen line 1 / screen line 2"
	failedStart := []fakecli.Rule{
		{Argv: []string{"agent", "start"}, ArgvPrefix: true, Stderr: `{"error":{"code":"agent_start_failed","message":"boom"}}`, Code: 1},
		{Argv: []string{"agent", "read", "w0test:p0a"}, ArgvPrefix: true, Stdout: screen},
		{Argv: []string{"pane", "close", "w0test:p0a"}, Stderr: `{"error":{"code":"pane_close_failed","message":"refused"}}`, Code: 1},
	}

	t.Run("pane get with pane_not_found: the pane is already gone", func(t *testing.T) {
		rules := append(append([]fakecli.Rule{}, failedStart...),
			fakecli.Rule{Argv: []string{"pane", "get", "w0test:p0a"}, Stderr: `{"error":{"code":"pane_not_found","message":"no pane"}}`, Code: 1})
		f := newSpawnFixture(t, rules)
		o := spawnOptions{name: "worker", pane: "w0test:p0a", timeout: "1"}
		code, msg := startAgentExit(t, o, "grok", f.env)
		want := fmt.Sprintf("agent start failed for worker (grok) in pane w0test:p0a; the pane close failed but the pane is already gone (last screen lines: %s)", tail)
		if code != 4 || msg != want {
			t.Fatalf("exit=%d msg=%q want %q", code, msg, want)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		closes, gets := 0, 0
		for i, c := range calls {
			if len(c.Argv) >= 2 && c.Argv[0] == "pane" && c.Argv[1] == "close" {
				closes++
			}
			if len(c.Argv) >= 2 && c.Argv[0] == "pane" && c.Argv[1] == "get" {
				gets++
			}
			if len(c.Argv) >= 2 && c.Argv[0] == "pane" && c.Argv[1] == "get" && i+1 != len(calls) {
				t.Fatalf("the recheck is not the last herdr call: %#v", calls)
			}
		}
		if closes != 1 || gets != 1 {
			t.Fatalf("close/get counts=%d/%d calls=%#v", closes, gets, calls)
		}
	})
	t.Run("pane get with the pane present: today's message", func(t *testing.T) {
		rules := append(append([]fakecli.Rule{}, failedStart...),
			fakecli.Rule{Argv: []string{"pane", "get", "w0test:p0a"}, Stdout: `{"result":{"pane":{"pane_id":"w0test:p0a"}}}`})
		f := newSpawnFixture(t, rules)
		o := spawnOptions{name: "worker", pane: "w0test:p0a", timeout: "1"}
		code, msg := startAgentExit(t, o, "grok", f.env)
		want := fmt.Sprintf("agent start failed for worker (grok) in pane w0test:p0a; pane close failed and the pane is still open, check it for a running agent (last screen lines: %s)", tail)
		if code != 4 || msg != want {
			t.Fatalf("exit=%d msg=%q want %q", code, msg, want)
		}
	})
	t.Run("pane get failing for another reason: today's message", func(t *testing.T) {
		rules := append(append([]fakecli.Rule{}, failedStart...),
			fakecli.Rule{Argv: []string{"pane", "get", "w0test:p0a"}, Stderr: "cannot resolve pane: server is not running", Code: 1})
		f := newSpawnFixture(t, rules)
		o := spawnOptions{name: "worker", pane: "w0test:p0a", timeout: "1"}
		code, msg := startAgentExit(t, o, "grok", f.env)
		want := fmt.Sprintf("agent start failed for worker (grok) in pane w0test:p0a; pane close failed and the pane is still open, check it for a running agent (last screen lines: %s)", tail)
		if code != 4 || msg != want {
			t.Fatalf("exit=%d msg=%q want %q", code, msg, want)
		}
	})
}

// Mutation control: the recheck never fires when the close succeeded.
func TestStartAgentSuccessfulCloseNeverRechecks(t *testing.T) {
	f := newSpawnFixture(t, []fakecli.Rule{
		{Argv: []string{"agent", "start"}, ArgvPrefix: true, Stderr: `{"error":{"code":"agent_start_failed","message":"boom"}}`, Code: 1},
		{Argv: []string{"agent", "read", "w0test:p0a"}, ArgvPrefix: true, Stdout: "screen line 1\n"},
		{Argv: []string{"pane", "close", "w0test:p0a"}},
	})
	o := spawnOptions{name: "worker", pane: "w0test:p0a", timeout: "1"}
	code, msg := startAgentExit(t, o, "grok", f.env)
	if code != 4 || !strings.Contains(msg, "closed the pane this spawn opened") {
		t.Fatalf("exit=%d msg=%q", code, msg)
	}
	calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if len(c.Argv) >= 2 && c.Argv[0] == "pane" && c.Argv[1] == "get" {
			t.Fatalf("pane rechecked after a successful close: %#v", calls)
		}
	}
}
