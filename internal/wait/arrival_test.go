package wait

import (
	"encoding/json"
	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { fakecli.RunTests(m) }

func TestArrivalMarkers(t *testing.T) {
	t.Run("queued path keeps spaces and identifies only its own prompt", func(t *testing.T) { // JS: "status: queued path uses unwrapped history while quota still uses visible screen"
		sd := t.TempDir()
		marker := "123 - /tmp/brief with spaces.md\n"
		if got := QueuedPromptPath(marker, sd, "worker"); got != "/tmp/brief with spaces.md" {
			t.Fatalf("path=%q", got)
		}
		if !MarkerHasPromptPath(marker) || !QueuedPromptSitsInInput(marker, "history\nRead the file /tmp/brief with spaces.md\n", sd, "worker") {
			t.Fatal("exact queued path should be in the final input lines")
		}
		if QueuedPromptSitsInInput(marker, "Read the file /tmp/other.md\n", sd, "worker") {
			t.Fatal("a foreign prompt echo matched the queued marker")
		}
	})
	t.Run("legacy queued marker derives the prompt from last-report", func(t *testing.T) { // JS: "wait: a legacy queued marker follows the prompt path"
		sd := t.TempDir()
		if err := os.MkdirAll(filepath.Join(sd, "briefs"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sd, "last-report-worker"), []byte("/tmp/reports/task.md\n"), 0600); err != nil {
			t.Fatal(err)
		}
		prompt := filepath.Join(sd, "briefs", "task.md")
		if err := os.WriteFile(prompt, []byte("brief"), 0600); err != nil {
			t.Fatal(err)
		}
		if got := QueuedPromptPath("123 7", sd, "worker"); got != prompt {
			t.Fatalf("path=%q, want %q", got, prompt)
		}
	})
	t.Run("dash seq is absent while a numeric changed seq is stale", func(t *testing.T) { // JS: "status: a dash in the not-received seq field means the seq is absent"
		if MarkerSeq("123 - /tmp/prompt.md") != "" || MarkerSeqChanged("123 - /tmp/prompt.md", 7) {
			t.Fatal("dash seq was treated as numeric")
		}
		if !MarkerSeqChanged("123 5 /tmp/prompt.md", 7) {
			t.Fatal("changed numeric seq was not detected")
		}
	})
	t.Run("last non-empty lines normalize CRLF and trim only for emptiness", func(t *testing.T) { // JS: "arrival helper limits the prompt marker to the final non-empty lines"
		lines := LastNonEmptyLines("a\r\n \r\nRead the file x\r\n", 2)
		if strings.Join(lines, "|") != "a|Read the file x" {
			t.Fatalf("lines=%q", lines)
		}
	})
}

func TestProbeQueuedPromptSafety(t *testing.T) {
	t.Run("foreign prompt echo becomes not-received without an Enter", func(t *testing.T) { // JS: "wait: queued prompt with a moved seq and outside the input box ends not-received and persists the result"
		got, marker, calls := runQueuedProbe(t, "Read the file /tmp/old previous prompt.md\n", "123 - /tmp/own current prompt.md\n", true)
		if got != "not-received" {
			t.Fatalf("probe=%s", got)
		}
		if !strings.Contains(marker, " - /tmp/own current prompt.md") {
			t.Fatalf("not-received marker=%q", marker)
		}
		for _, call := range calls {
			if strings.Contains(strings.Join(call.Argv, " "), "send-keys") {
				t.Fatalf("foreign echo got a key: %#v", call.Argv)
			}
		}
	})
	t.Run("own prompt in the recognized composer gets one bounded Enter", func(t *testing.T) { // Adapted contract: the queued retry needs a recognized composer holding the stored path (the claude box), not a plain history line.
		box := claudeBoxScreen("", "❯ Read the file /tmp/own current prompt.md in full and execute it.", "  [Opus 5.5] 67% [main*]\n")
		got, marker, calls := runQueuedProbe(t, box, "123 - /tmp/own current prompt.md\n")
		if got != "working" {
			t.Fatalf("probe=%s", got)
		}
		if !strings.Contains(marker, " - /tmp/own current prompt.md") {
			t.Fatalf("not-received marker=%q", marker)
		}
		found := false
		for _, call := range calls {
			if strings.Join(call.Argv, " ") == "agent send-keys worker enter" {
				found = true
			}
		}
		if !found {
			t.Fatalf("Enter not sent: %#v", calls)
		}
	})
	t.Run("an empty queued marker starts its retry window now", func(t *testing.T) { // JS: "wait: empty queued marker starts its retry window now"
		got, marker, calls := runQueuedProbe(t, "", "")
		if got != "working" {
			t.Fatalf("probe=%s", got)
		}
		parts := strings.Fields(marker)
		if len(parts) != 3 || parts[1] != "-" || !strings.HasSuffix(parts[2], filepath.Join("briefs", "task.md")) {
			t.Fatalf("not-received marker=%q", marker)
		}
		for _, call := range calls {
			if strings.Contains(strings.Join(call.Argv, " "), "send-keys") {
				t.Fatalf("fresh empty marker got a key: %#v", call.Argv)
			}
		}
	})
}

func runQueuedProbe(t *testing.T, screen, marker string, secondProbe ...bool) (string, string, []fakecli.Call) {
	t.Helper()
	base := t.TempDir()
	bin := filepath.Join(base, "bin")
	state := filepath.Join(base, "state", "ws")
	waitDir := filepath.Join(state, "wait")
	if err := os.MkdirAll(waitDir, 0700); err != nil {
		t.Fatal(err)
	}
	if marker == "" {
		prompt := filepath.Join(state, "briefs", "task.md")
		if err := os.MkdirAll(filepath.Dir(prompt), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, "last-report-worker"), []byte(filepath.Join(state, "reports", "task.md")+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(prompt, []byte("brief"), 0600); err != nil {
			t.Fatal(err)
		}
		// Adapted contract: the screen is the recognized claude composer
		// holding the resolved prompt path.
		screen = claudeBoxScreen("", "❯ Read the file "+prompt+" in full and execute it.", "  [Opus 5.5] 67% [main*]\n")
	}
	if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte("# name\t pane\t kind\t role\nworker\tp0a\tclaude\timplementer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(waitDir, "worker.queued"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"agent_status":"idle"}}}`},
		{Argv: []string{"agent", "read", "worker", "--source", "visible", "--lines", "20"}, Stdout: ""},
		{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Stdout: ""},
		{Argv: []string{"agent", "read", "worker", "--source", "recent-unwrapped", "--lines", "40"}, Stdout: screen, ArgvPrefix: true},
		{Argv: []string{"agent", "send-keys", "worker", "enter"}},
	}
	if _, err := fakecli.Install(t, bin, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{"PATH": bin, "HERDR_SOHO_FAKECLI_CONFIG": bin, "HERDR_SOCKET_PATH": filepath.Join(base, "none.sock"), "HERDR_SOHO_WAIT_POLL_MS": "1"}
	ctx := &core.Config{Entries: map[string]core.ConfigEntry{}}
	got := ProbeAgent(state, "worker", "", ctx, env)
	if len(secondProbe) > 0 && secondProbe[0] {
		got = ProbeAgent(state, "worker", "", ctx, env)
	}
	after, _ := os.ReadFile(filepath.Join(waitDir, "worker.not-received"))
	calls, err := fakecli.ReadCalls(filepath.Join(bin, "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return got, string(after), calls
}

func TestWaitOutputJSONUsesStableFieldOrder(t *testing.T) {
	// Mutation captured: changing completion line insertion order breaks the JavaScript JSON contract.
	obj := jsonjs.O("agent", "worker", "status", "done", "report", "r.md")
	got := jsonjs.Stringify(obj)
	if got != "{\"agent\":\"worker\",\"status\":\"done\",\"report\":\"r.md\"}" {
		t.Fatalf("json=%s", got)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil || decoded["agent"] != "worker" {
		t.Fatalf("decode=%v err=%v", decoded, err)
	}
}
