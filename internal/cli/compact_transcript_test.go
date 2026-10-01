package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

const transcriptTestUUID = "3f2b8c1a-9d4e-4c7a-b1f0-5e6d7c8b9a0f"

// claudeAgentGetJSON is the `herdr agent get` response the compact transcript
// tests use: the worker's cwd and its claude session id.
func claudeAgentGetJSON(state, cwd string) string {
	return `{"result":{"agent":{"agent_status":"` + state + `","state_change_seq":1,"cwd":"` + cwd + `","agent_session":{"agent":"claude","kind":"id","value":"` + transcriptTestUUID + `"}}}}`
}

// newCompactTranscriptFixture builds a compact fixture of the given kind
// whose agent get reports the given cwd and, when transcriptContent is
// non-empty, the transcript file under its own CLAUDE_CONFIG_DIR. The
// transcript path follows Claude Code's encoding of the cwd (herdr.ClaudeProjectDir).
func newCompactTranscriptFixture(t *testing.T, kind, cwd, transcriptContent string, rules []fakecli.Rule) (*compactFixture, string) {
	t.Helper()
	f := newCompactFixture(t, kind, rules)
	configRoot := filepath.Join(f.root, "claude")
	f.env["CLAUDE_CONFIG_DIR"] = configRoot
	transcript := filepath.Join(configRoot, "projects", herdr.ClaudeProjectDir(cwd), transcriptTestUUID+".jsonl")
	if transcriptContent != "" {
		if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(transcript, []byte(transcriptContent), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return f, transcript
}

// waitForCall polls the fake herdr call log until a call with the exact argv
// is recorded (or the deadline runs out).
func waitForCall(t *testing.T, f *compactFixture, argv []string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if countArgv(f.calls(t), argv) >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %v call in the log before the deadline", argv)
}

// appendTranscript appends one line to the transcript, the way Claude Code
// appends its compact_boundary line when the compaction finishes.
func appendTranscript(t *testing.T, path, line string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(line); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestCompactClaudeTranscriptBoundaryProvesCompaction covers the real claude
// screen: after the /compact it keeps showing the older conversation, with no
// echo of the command and no Compacted line; the session transcript gains a
// compact_boundary line instead. The count past the pre-send baseline proves
// the compaction (the screen never does), then the idle wait and the compacted
// JSON run as today.
func TestCompactClaudeTranscriptBoundaryProvesCompaction(t *testing.T) {
	cwd := "/tmp/dot_work_1"
	oldScreen := "older conversation line one\nolder conversation line two\n"
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: claudeAgentGetJSON("idle", cwd)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		// The real screen never gains a proof line below the sent command.
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: oldScreen},
	}
	f, transcript := newCompactTranscriptFixture(t, "claude", cwd,
		`{"type":"user","content":"SENTINEL-OLD-CONVERSATION"}
{"type":"assistant","content":"SENTINEL-OLDER-REPLY"}
`, rules)
	var code int
	var out, errText string
	done := make(chan struct{})
	go func() {
		code, out, errText = f.run(t, "compact", "worker", "--timeout", "30000")
		close(done)
	}()
	// The baseline count ran before the send: once the /compact reached the
	// pane, append the boundary line the way the session transcript records
	// the compaction.
	waitForCall(t, f, []string{"pane", "send-keys", "p1", "Enter"})
	appendTranscript(t, transcript, `{"type":"system","subtype":"compact_boundary","summary":"SENTINEL-BOUNDARY-SUMMARY"}
`)
	<-done
	if code != 0 || errText != "" {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["agent"] != "worker" || value["kind"] != "claude" || value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	// No transcript content reaches stdout: only the result JSON does.
	for _, sentinel := range []string{"SENTINEL-OLD-CONVERSATION", "SENTINEL-OLDER-REPLY", "SENTINEL-BOUNDARY-SUMMARY"} {
		if strings.Contains(out, sentinel) {
			t.Fatalf("transcript content on stdout: %s", out)
		}
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-text", "p1", "/compact"}); n != 1 {
		t.Fatalf("send-text calls=%d want 1: %#v", n, calls)
	}
	data, err := os.ReadFile(transcript)
	if err != nil || !strings.Contains(string(data), "SENTINEL-BOUNDARY-SUMMARY") {
		t.Fatalf("the boundary line was not recorded in the transcript: %v", err)
	}
}

// TestCompactClaudeTranscriptCountStableTimesOut verifies the negative side:
// the session was compacted before (the baseline already holds one boundary)
// and no new boundary appears after the send, so the wait runs out as a
// timeout, exactly as today.
func TestCompactClaudeTranscriptCountStableTimesOut(t *testing.T) {
	cwd := "/tmp/dot_work_1"
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: claudeAgentGetJSON("idle", cwd)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: "older conversation line\n"},
	}
	f, transcript := newCompactTranscriptFixture(t, "claude", cwd,
		`{"type":"system","subtype":"compact_boundary","summary":"earlier compaction"}
{"type":"user","content":"after the earlier compaction"}
`, rules)
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "500")
	if code != 9 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["agent"] != "worker" || value["kind"] != "claude" || value["status"] != "timeout" {
		t.Fatalf("json=%v", value)
	}
	// The transcript is unchanged: no new boundary, nothing else read out.
	data, err := os.ReadFile(transcript)
	if err != nil || strings.Count(string(data), `"subtype":"compact_boundary"`) != 1 {
		t.Fatalf("transcript changed: %v %q", err, string(data))
	}
}

// TestCompactClaudeWithoutAgentSessionUsesScreenPath verifies that an agent
// get without agent_session leaves today's screen path as the evidence: the
// proof below the sent /compact still ends the wait, with no new warning.
func TestCompactClaudeWithoutAgentSessionUsesScreenPath(t *testing.T) {
	f := newCompactFixture(t, "claude", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: compactScreens.claudeStale},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: compactScreens.claudeFresh},
	})
	f.env["CLAUDE_CONFIG_DIR"] = filepath.Join(f.root, "claude")
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 0 || errText != "" {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["agent"] != "worker" || value["kind"] != "claude" || value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	calls := f.calls(t)
	// The screen proof is what ended the wait: the pre-send read plus the
	// first poll that found it.
	if n := countArgv(calls, compactReadArgv("worker")); n != 2 {
		t.Fatalf("recent reads=%d want 2: %#v", n, calls)
	}
}

// TestCompactCodexIgnoresTranscriptBoundary is the "claude only" side of the
// transcript rule: a codex worker whose session transcript gains a
// compact_boundary line is not compacted by that line (for codex only the
// screen proof is the evidence), so the wait runs out as a timeout.
func TestCompactCodexIgnoresTranscriptBoundary(t *testing.T) {
	cwd := "/tmp/dot_work_1"
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: claudeAgentGetJSON("idle", cwd)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		// The screen never gains codex's proof (Context compacted).
		{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: "older conversation line\n"},
	}
	f, transcript := newCompactTranscriptFixture(t, "codex", cwd,
		`{"type":"user","content":"old line"}
`, rules)
	var code int
	var out, errText string
	done := make(chan struct{})
	go func() {
		code, out, errText = f.run(t, "compact", "worker", "--timeout", "500")
		close(done)
	}()
	waitForCall(t, f, []string{"pane", "send-keys", "p1", "Enter"})
	appendTranscript(t, transcript, `{"type":"system","subtype":"compact_boundary"}
`)
	<-done
	if code != 9 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	value := compactJSON(t, out)
	if value["agent"] != "worker" || value["kind"] != "codex" || value["status"] != "timeout" {
		t.Fatalf("json=%v", value)
	}
}

// TestCompactClaudeTranscriptFileMissingUsesScreenPath verifies that an agent
// session that exists but whose transcript file is missing leaves today's
// screen path as the evidence, with no new warning.
func TestCompactClaudeTranscriptFileMissingUsesScreenPath(t *testing.T) {
	cwd := "/tmp/dot_work_1"
	f, _ := newCompactTranscriptFixture(t, "claude", cwd, "", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: claudeAgentGetJSON("idle", cwd)},
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
}
