package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// A Claude Code target writes every prompt it takes into its local session
// transcript as a "type":"user" line. These tests prove the dispatch arrival
// proof that reads only the count of those lines holding the composed prompt
// path (the same window as --amend and the normal dispatch): a rise within
// the check window is arrival proof when the screen cannot show the prompt;
// without the transcript (or for a target whose session is not a local id)
// the screen-based path stands.

const (
	claudeTranscriptSession     = "a1b2c3d4-0000-1111-2222-334455667788"
	claudeTranscriptRemoteRef   = "b7e6f5a4-8888-4444-9999-000011112222"
	claudeTranscriptCwd         = "/tmp/work"
	claudeTranscriptStamp       = "20260929T000000"
	claudeTranscriptAppendDelay = 400 * time.Millisecond
)

type claudeDispatchFixture struct {
	*dispatchArrivalFixture
	composed   string
	report     string
	transcript string
}

// pinDispatchClock pins the clock at 2026-09-29 00:00:00 UTC (so the
// composed prompt is worker-20260929T000000.md) but still advances with the
// real clock, so the check window's deadline passes in real time.
func pinDispatchClock(t *testing.T) {
	t.Helper()
	base := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	began := time.Now()
	oldNow := platform.Now
	platform.Now = func() time.Time { return base.Add(time.Since(began)) }
	t.Cleanup(func() { platform.Now = oldNow })
}

func claudeAgentGet(mode string, seq int, sessionKind, sessionValue string) string {
	body := `{"result":{"agent":{"agent_status":"` + mode + `","state_change_seq":` + strconv.Itoa(seq)
	if sessionValue != "" {
		body += `,"cwd":"` + claudeTranscriptCwd + `","agent_session":{"source":"herdr","agent":"claude","kind":"` + sessionKind + `","value":"` + sessionValue + `"}}`
	}
	return body + `}}`
}

// newClaudeDispatchFixture builds the arrival fixture with the roster kind
// claude, the check window at 2 s and the poll at 100 ms. The Call 2
// `agent get` rule is the ClaudeTranscriptPath resolution; when sessionValue
// is set it carries the agent_session and cwd, the transcript file exists
// under a CLAUDE_CONFIG_DIR layout, and the fixture returns its path.
func newClaudeDispatchFixture(t *testing.T, mode string, seq int, sessionKind, sessionValue, settle string) *claudeDispatchFixture {
	t.Helper()
	extra := []fakecli.Rule{}
	if sessionValue != "" {
		extra = append(extra, fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: claudeAgentGet(mode, seq, sessionKind, sessionValue)})
	}
	base := newDispatchArrivalFixture(t, mode, seq, seq, "old work output without a prompt marker\n", settle, extra...)
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\tmode\targs\teffort\n" +
		"worker\tw0test:p0a\tclaude\timplementer\tanthropic\t0\t\tnow\tclaude-sonnet\ttask\timplementer\t\t\t\thigh\n"
	if err := os.WriteFile(filepath.Join(base.state, "ws", "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	base.env["CLAUDE_CONFIG_DIR"] = t.TempDir()
	base.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "2"
	base.env["HERDR_SOHO_WAIT_POLL_MS"] = "100"
	f := &claudeDispatchFixture{
		dispatchArrivalFixture: base,
		composed:               filepath.Join(base.state, "ws", "briefs", "worker-"+claudeTranscriptStamp+".md"),
		report:                 filepath.Join(base.state, "ws", "reports", "worker-"+claudeTranscriptStamp+".md"),
	}
	if sessionValue != "" {
		f.transcript = filepath.Join(base.env["CLAUDE_CONFIG_DIR"], "projects", herdr.ClaudeProjectDir(claudeTranscriptCwd), sessionValue+".jsonl")
		if err := os.MkdirAll(filepath.Dir(f.transcript), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.transcript, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// seedTranscript writes the transcript's pre-send content (it must exist and
// be readable before dispatch resolves the transcript path).
func (f *claudeDispatchFixture) seedTranscript(t *testing.T, lines ...string) {
	t.Helper()
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	if err := os.WriteFile(f.transcript, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func claudeTranscriptLine(role, text string) string {
	return `{"type":"` + role + `","message":{"role":"` + role + `","content":[{"type":"text","text":"` + text + `"}]}}`
}

// claudeDispatchText is the prompt text dispatch sends (internal/cli/
// dispatch.go), for a normal dispatch or an amendment.
func claudeDispatchText(composed, report string, amend bool) string {
	if amend {
		return fmt.Sprintf("Read the file %s in full and execute it. It amends the brief you are working on. When finished, write your report to %s and reply with exactly that path and nothing else.", composed, report)
	}
	return fmt.Sprintf("Read the file %s in full and execute it. It contains your role, your brief, and your report contract. When finished, write your report to %s and reply with exactly that path and nothing else.", composed, report)
}

// appendTranscriptLine appends a line to the transcript after the delay, so
// it lands after the pre-send count and inside the 2 s check window.
func appendTranscriptLine(t *testing.T, path, line string) {
	t.Helper()
	go func() {
		time.Sleep(claudeTranscriptAppendDelay)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Errorf("transcript append open: %v", err)
			return
		}
		defer f.Close()
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Errorf("transcript append: %v", err)
		}
	}()
}

// requireNoEnter asserts that dispatch sent no key to the target.
func requireNoEnter(t *testing.T, bin string) {
	t.Helper()
	calls, err := fakecli.ReadCalls(filepath.Join(bin, "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if len(call.Argv) > 1 && call.Argv[1] == "send-keys" {
			t.Fatalf("a key was sent to the target: %#v", call.Argv)
		}
	}
}

// withEarlierReport makes --amend valid for the fixture's worker.
func withEarlierReport(t *testing.T, f *claudeDispatchFixture) {
	t.Helper()
	previous := filepath.Join(f.state, "ws", "reports", "previous.md")
	if err := os.WriteFile(previous, []byte("old report\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.state, "ws", "last-report-worker"), []byte(previous+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchClaudeTranscriptArrival(t *testing.T) {
	previousPrompt := "Read the file /var/tmp/old/worker-19990101T000000.md in full and execute it."

	t.Run("amend to a working claude: the transcript rise proves arrival, exit 0, no Enter, no resend", func(t *testing.T) {
		pinDispatchClock(t)
		f := newClaudeDispatchFixture(t, "working", 5, "id", claudeTranscriptSession, "0")
		f.seedTranscript(t, claudeTranscriptLine("user", previousPrompt))
		withEarlierReport(t, f)
		appendTranscriptLine(t, f.transcript, claudeTranscriptLine("user", claudeDispatchText(f.composed, f.report, true)))
		code, out, errText := f.run(t, "worker", f.brief, "--amend", "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "queued" || !strings.Contains(errText, "prompt queued") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if strings.Contains(errText, "not confirmed") {
			t.Fatalf("the transcript rise was not accepted as arrival: %s", errText)
		}
		requireNoEnter(t, f.bin)
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if got := countDispatchCalls(calls, "prompt"); got != 1 {
			t.Fatalf("prompt attempts=%d, want 1 (no resend)", got)
		}
		if _, err := os.Stat(filepath.Join(f.state, "ws", "wait", "worker.not-received")); !os.IsNotExist(err) {
			t.Fatalf("a not-received marker was written: %v", err)
		}
		if _, err := os.Stat(filepath.Join(f.state, "ws", "wait", "worker.queued")); err != nil {
			t.Fatalf("queued marker missing: %v", err)
		}
		sidecar := mustRead(t, filepath.Join(f.state, "ws", "briefs", "worker-"+claudeTranscriptStamp+".dispatch.json"))
		if !strings.Contains(sidecar, `"arrival":"queued"`) {
			t.Fatalf("sidecar did not record the queued arrival: %s", sidecar)
		}
	})
	t.Run("the same amend without the transcript line: today's exit 15, no Enter", func(t *testing.T) {
		pinDispatchClock(t)
		f := newClaudeDispatchFixture(t, "working", 5, "id", claudeTranscriptSession, "0")
		f.seedTranscript(t, claudeTranscriptLine("user", previousPrompt))
		withEarlierReport(t, f)
		code, out, errText := f.run(t, "worker", f.brief, "--amend", "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" || !strings.Contains(errText, "not confirmed") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		requireNoEnter(t, f.bin)
		if _, err := os.Stat(filepath.Join(f.state, "ws", "wait", "worker.not-received")); err != nil {
			t.Fatalf("not-received marker missing: %v", err)
		}
	})
	t.Run("an assistant line holding the path is not proof", func(t *testing.T) {
		pinDispatchClock(t)
		f := newClaudeDispatchFixture(t, "working", 5, "id", claudeTranscriptSession, "0")
		f.seedTranscript(t, claudeTranscriptLine("user", previousPrompt))
		withEarlierReport(t, f)
		appendTranscriptLine(t, f.transcript, claudeTranscriptLine("assistant", claudeDispatchText(f.composed, f.report, true)))
		code, out, errText := f.run(t, "worker", f.brief, "--amend", "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" || !strings.Contains(errText, "not confirmed") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		requireNoEnter(t, f.bin)
	})
	t.Run("a normal dispatch to an idle claude: the transcript rise proves arrival", func(t *testing.T) {
		pinDispatchClock(t)
		f := newClaudeDispatchFixture(t, "idle", 1, "id", claudeTranscriptSession, "0")
		f.seedTranscript(t, claudeTranscriptLine("user", previousPrompt))
		appendTranscriptLine(t, f.transcript, claudeTranscriptLine("user", claudeDispatchText(f.composed, f.report, false)))
		code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "submitted" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		requireNoEnter(t, f.bin)
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if got := countDispatchCalls(calls, "prompt"); got != 1 {
			t.Fatalf("prompt attempts=%d, want 1 (no resend)", got)
		}
	})
	t.Run("a remote target (a session that is not a local id) keeps today's path", func(t *testing.T) {
		pinDispatchClock(t)
		// The agent_session is a reference to a session that does not run
		// locally: the local transcript must not resolve and must not prove
		// the target, even though a local file with that name gains a line
		// holding the path inside the window.
		f := newClaudeDispatchFixture(t, "working", 5, "ref", claudeTranscriptRemoteRef, "0")
		f.seedTranscript(t, claudeTranscriptLine("user", previousPrompt))
		withEarlierReport(t, f)
		appendTranscriptLine(t, f.transcript, claudeTranscriptLine("user", claudeDispatchText(f.composed, f.report, true)))
		code, out, errText := f.run(t, "worker", f.brief, "--amend", "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" || !strings.Contains(errText, "not confirmed") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		requireNoEnter(t, f.bin)
	})
	t.Run("a target without any agent_session keeps today's path", func(t *testing.T) {
		pinDispatchClock(t)
		f := newClaudeDispatchFixture(t, "working", 5, "", "", "0")
		withEarlierReport(t, f)
		code, out, errText := f.run(t, "worker", f.brief, "--amend", "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" || !strings.Contains(errText, "not confirmed") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		requireNoEnter(t, f.bin)
	})
	t.Run("a user line holding the path already in the transcript before the send is not a rise", func(t *testing.T) {
		pinDispatchClock(t)
		f := newClaudeDispatchFixture(t, "working", 5, "id", claudeTranscriptSession, "0")
		f.seedTranscript(t, claudeTranscriptLine("user", claudeDispatchText(f.composed, f.report, true)))
		withEarlierReport(t, f)
		code, out, errText := f.run(t, "worker", f.brief, "--amend", "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" || !strings.Contains(errText, "not confirmed") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		requireNoEnter(t, f.bin)
	})
}

// TestClaudeTranscriptPathMarker: the composed path lands in the transcript
// inside a JSON string; the marker must be the path escaped the way JSON
// writes it (a Windows separator doubled, a quote escaped).
func TestClaudeTranscriptPathMarker(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/var/tmp/herdr-soho/w14/reports/build-2-20261001T164144.brief.md", "/var/tmp/herdr-soho/w14/reports/build-2-20261001T164144.brief.md"},
		{`C:\herdr-soho\w14\reports\build-2-20261001T164144.brief.md`, `C:\\herdr-soho\\w14\\reports\\build-2-20261001T164144.brief.md`},
		{`C:\Users\x\re"port.md`, `C:\\Users\\x\\re\"port.md`},
		{"plain.md", "plain.md"},
	}
	for _, tc := range cases {
		if got := claudeTranscriptPathMarker(tc.in); got != tc.want {
			t.Fatalf("claudeTranscriptPathMarker(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
