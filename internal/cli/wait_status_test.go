package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestStatusQueuedPromptRouting(t *testing.T) {
	env, cwd := commandFixture(t)
	state := filepath.Join(env.Get("HERDR_SOHO_DIR"), "ws")
	if err := os.MkdirAll(filepath.Join(state, "wait"), 0o700); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(state, "reports", "worker.md")
	if err := os.MkdirAll(filepath.Dir(report), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte("# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\nworker\tp0a\tclaude\timplementer\t\t\t\t\t\t\t\t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "last-report-worker"), []byte(report+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := "123 - /tmp/worker prompt.md\n"
	if err := os.WriteFile(filepath.Join(state, "wait", "worker.queued"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"agent_status":"idle","state_change_seq":5}}}`},
		{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[{"name":"worker","pane_id":"p0a"}]}}`},
		{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Stdout: ""},
		{Argv: []string{"agent", "read", "worker", "--source", "recent-unwrapped", "--lines", "40"}, Stdout: "Read the file /tmp/worker prompt.md\n", ArgvPrefix: true},
	}
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	env = withFakeCLI(env, bin)
	env["HERDR_SOCKET_PATH"] = filepath.Join(t.TempDir(), "none.sock")
	env["HERDR_ENV"] = "1"
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	code := Run([]string{"status", "worker"}, env)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	if code != 15 || stderr.Len() != 0 || !strings.HasPrefix(out.String(), "worker\tnot-received\t") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(state, "wait", "worker.queued")); err != nil {
		t.Fatalf("status removed its read-only queued marker: %v", err)
	}
	calls, err := fakecli.ReadCalls(filepath.Join(bin, "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if strings.Contains(strings.Join(call.Argv, " "), "send-keys") {
			t.Fatalf("status sent a key: %#v", call.Argv)
		}
	}
	_ = cwd
}

func TestWaitRoutesAndValidatesTimeout(t *testing.T) { // JS: "wait: a --timeout that is not a number of milliseconds exits 2"
	env, _ := commandFixture(t)
	bin := t.TempDir()
	if _, err := fakecli.Install(t, bin, "herdr", nil); err != nil {
		t.Fatal(err)
	}
	env = withFakeCLI(env, bin)
	env["HERDR_ENV"] = "1"
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	code := Run([]string{"wait", "worker", "--timeout", "later"}, env)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	if code != 2 || out.Len() != 0 || stderr.String() != "herdr-soho: wait: --timeout expects milliseconds, got 'later'\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), stderr.String())
	}
}
