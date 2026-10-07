package peer_test

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/peer"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// The rc.12 claude case: send --now to a local Claude Code running a long
// tool call. Claude Code holds the message until the tool ends and only then
// writes the user line to the session transcript, later than the proof
// window: the send exited 15 (lost) while the message was received. When the
// transcript proof is armed (a local claude with a resolved transcript) and
// the window ends without proof while the target is still working, the send
// now keeps consulting the transcript until the command --timeout — the same
// value as the busy-target wait — warning once on stderr: a count growth with
// no queue line on screen is sent (exit 0), a visible queue line is queued
// (as after the window), a target that leaves working without the growth and
// the deadline keep today's outcome, and no key is sent in the wait. A remote
// target keeps today's behavior: the transcript proof is not armed at all.

// appendAfterPromptDelayed is appendAfterPrompt with a delay after the prompt
// call is logged: the transcript line lands only after the first proof window
// has expired, inside the extended transcript wait.
func appendAfterPromptDelayed(t *testing.T, callsLog, transcriptPath, id, line string, delayMS int) <-chan bool {
	t.Helper()
	done := make(chan bool, 1)
	go func() {
		ok := false
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if calls, err := fakecli.ReadCalls(callsLog); err == nil {
				for _, c := range calls {
					for i := 0; i+3 < len(c.Argv); i++ {
						if c.Argv[i] == "agent" && c.Argv[i+1] == "prompt" && strings.Contains(c.Argv[i+3], "#"+id) {
							time.Sleep(time.Duration(delayMS) * time.Millisecond)
							f, werr := os.OpenFile(transcriptPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
							if werr == nil {
								_, werr = f.WriteString(line + "\n")
								_ = f.Close()
								ok = werr == nil
							}
							break
						}
					}
					if ok {
						break
					}
				}
			}
			if ok {
				break
			}
			time.Sleep(time.Millisecond)
		}
		done <- ok
	}()
	return done
}

func TestSendArrivalBusyClaudeTranscript(t *testing.T) {
	const id = "01020304"
	const cwd = "/tmp/claude-work"
	const workingScreen = "✻ Cranking out changes…\n"
	// The transcript line holds the real peer end line (with the # marker),
	// the way Claude Code records the taken message.
	userLine := `{"type":"user","message":{"content":"` + peer.PeerEndLine(id) + `"}}`
	newPrompt := func(t *testing.T) string {
		t.Helper()
		oldReader := rand.Reader
		rand.Reader = bytes.NewReader([]byte{1, 2, 3, 4})
		t.Cleanup(func() { rand.Reader = oldReader })
		return peer.PeerHeader("local/-", "-", "-", "-", id) + "\n\n> hello\n" + peer.PeerEndLine(id)
	}
	promptArgv := func(prompt string) []string {
		return []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}
	}

	t.Run("claude working: the user line lands after the window: sent, exit 0, no key, the warning once", func(t *testing.T) {
		prompt := newPrompt(t)
		configRoot := t.TempDir()
		transcriptPath := claudeTranscriptPath(t, configRoot, cwd)
		writeClaudeTranscript(t, transcriptPath, `{"type":"user","message":{"content":"earlier conversation"}}`+"\n")
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, ArgvPrefix: true, Stdout: claudeSessionAgentJSON("working", "1", cwd)},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, ArgvPrefix: true, Stdout: workingScreen},
			// A working claude's alternate-screen history cannot be captured:
			// the recent read fails and falls back to the visible screen.
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stderr: notIdleErr, Code: 1},
			{Argv: promptArgv(prompt)},
		}
		f := newFixture(t, rules)
		f.env["CLAUDE_CONFIG_DIR"] = configRoot
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1000", "50"
		done := appendAfterPromptDelayed(t, filepath.Join(filepath.Dir(f.bin), "herdr.calls.jsonl"), transcriptPath, id, userLine, 1600)
		code, out, stderr := f.run([]string{"send", "w0test:p0a", "--now", "--timeout", "4000", "hello"})
		if code != 0 || out != "sent to local/w0test:p0a\n" {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
		if want := "send: local/w0test:p0a is busy; waiting for its transcript to show the message (up to 4s)\n"; stderr != want {
			t.Fatalf("the busy warning goes to stderr exactly once: %q", stderr)
		}
		if !<-done {
			t.Fatal("the transcript line was not appended after the prompt call")
		}
		if enters := countSendKeyEnters(t, f); enters != 0 {
			t.Fatalf("no key is sent in the extended wait: %d enters", enters)
		}
		if prompts := countPromptCalls(t, f); prompts != 1 {
			t.Fatalf("the busy path never resends the text: %d prompts", prompts)
		}
	})
	t.Run("claude working: the user line lands after the window with the queue line visible: queued, exit 0", func(t *testing.T) {
		prompt := newPrompt(t)
		configRoot := t.TempDir()
		transcriptPath := claudeTranscriptPath(t, configRoot, cwd)
		writeClaudeTranscript(t, transcriptPath, `{"type":"user","message":{"content":"earlier conversation"}}`+"\n")
		// After the prompt the message sits in claude's open queue: the
		// marker and the queue line are on screen.
		queuedScreen := workingScreen + prompt + "\nPress up to edit queued messages\n"
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, ArgvPrefix: true, Stdout: claudeSessionAgentJSON("working", "1", cwd)},
			// The pre-send screen is the working turn without the queue.
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: workingScreen},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, ArgvPrefix: true, Stdout: queuedScreen},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stderr: notIdleErr, Code: 1},
			{Argv: promptArgv(prompt)},
		}
		f := newFixture(t, rules)
		f.env["CLAUDE_CONFIG_DIR"] = configRoot
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1000", "50"
		done := appendAfterPromptDelayed(t, filepath.Join(filepath.Dir(f.bin), "herdr.calls.jsonl"), transcriptPath, id, userLine, 1600)
		code, out, stderr := f.run([]string{"send", "w0test:p0a", "--now", "--timeout", "4000", "hello"})
		if code != 0 || !strings.HasPrefix(out, "queued for local/w0test:p0a") {
			t.Fatalf("a visible queue line is queued, not sent: code=%d out=%q stderr=%q", code, out, stderr)
		}
		if want := "send: local/w0test:p0a is busy; waiting for its transcript to show the message (up to 4s)\n"; stderr != want {
			t.Fatalf("the busy warning goes to stderr exactly once: %q", stderr)
		}
		if !<-done {
			t.Fatal("the transcript line was not appended after the prompt call")
		}
		if enters := countSendKeyEnters(t, f); enters != 0 {
			t.Fatalf("no key may go to a busy claude with an open queue: %d enters", enters)
		}
		if prompts := countPromptCalls(t, f); prompts != 1 {
			t.Fatalf("the busy path never resends the text: %d prompts", prompts)
		}
	})
	t.Run("claude working: nothing until the deadline: today's outcome after the wait", func(t *testing.T) {
		prompt := newPrompt(t)
		configRoot := t.TempDir()
		transcriptPath := claudeTranscriptPath(t, configRoot, cwd)
		// The transcript holds only the earlier conversation: the count never
		// moves, and the target keeps working until the deadline.
		writeClaudeTranscript(t, transcriptPath, `{"type":"user","message":{"content":"earlier conversation"}}`+"\n")
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, ArgvPrefix: true, Stdout: claudeSessionAgentJSON("working", "1", cwd)},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, ArgvPrefix: true, Stdout: workingScreen},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stderr: notIdleErr, Code: 1},
			{Argv: promptArgv(prompt)},
		}
		f := newFixture(t, rules)
		f.env["CLAUDE_CONFIG_DIR"] = configRoot
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1000", "100"
		code, _, stderr := f.run([]string{"send", "w0test:p0a", "--now", "--timeout", "2000", "hello"})
		want := "send: local/w0test:p0a is busy; waiting for its transcript to show the message (up to 2s)\n" +
			"herdr-soho: send: local/w0test:p0a did not confirm taking the message (no sign of it in its state or screen); delivery is uncertain; read its pane for #01020304 or a reply before sending again\n"
		if code != 15 || stderr != want {
			t.Fatalf("the deadline keeps today's outcome: code=%d stderr=%q", code, stderr)
		}
		if enters := countSendKeyEnters(t, f); enters != 0 {
			t.Fatalf("no key is sent in the wait or after it: %d enters", enters)
		}
		if prompts := countPromptCalls(t, f); prompts != 1 {
			t.Fatalf("the busy path never resends the text: %d prompts", prompts)
		}
	})
	t.Run("claude leaving working without the growth: today's path, no Enter without the id", func(t *testing.T) {
		prompt := newPrompt(t)
		configRoot := t.TempDir()
		transcriptPath := claudeTranscriptPath(t, configRoot, cwd)
		writeClaudeTranscript(t, transcriptPath, `{"type":"user","message":{"content":"earlier conversation"}}`+"\n")
		// The state reads stay working through the window (a full second of
		// polls) and the wait entry, and report idle from the 21st read on:
		// the target leaves working while the count never moves.
		rules := make([]fakecli.Rule, 0, 24)
		for call := 1; call <= 20; call++ {
			rules = append(rules, fakecli.Rule{Argv: []string{"agent", "get", "w0test:p0a"}, Call: call, Stdout: claudeSessionAgentJSON("working", "1", cwd)})
		}
		rules = append(rules,
			fakecli.Rule{Argv: []string{"agent", "get", "w0test:p0a"}, ArgvPrefix: true, Stdout: claudeSessionAgentJSON("idle", "2", cwd)},
			fakecli.Rule{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, ArgvPrefix: true, Stdout: workingScreen},
			fakecli.Rule{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stderr: notIdleErr, Code: 1},
			fakecli.Rule{Argv: promptArgv(prompt)},
		)
		f := newFixture(t, rules)
		f.env["CLAUDE_CONFIG_DIR"] = configRoot
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1000", "100"
		code, _, stderr := f.run([]string{"send", "w0test:p0a", "--now", "--timeout", "5000", "hello"})
		want := "send: local/w0test:p0a is busy; waiting for its transcript to show the message (up to 5s)\n" +
			"herdr-soho: send: local/w0test:p0a did not confirm taking the message (no sign of it in its state or screen); delivery is uncertain; read its pane for #01020304 or a reply before sending again\n"
		if code != 15 || stderr != want {
			t.Fatalf("leaving working without the growth follows today's path: code=%d stderr=%q", code, stderr)
		}
		if enters := countSendKeyEnters(t, f); enters != 0 {
			t.Fatalf("today's path presses an Enter only with the id in the box: %d enters", enters)
		}
		if prompts := countPromptCalls(t, f); prompts != 1 {
			t.Fatalf("the busy path never resends the text: %d prompts", prompts)
		}
	})
	t.Run("a remote claude target: nothing changes, no transcript, no warning", func(t *testing.T) {
		_ = newPrompt(t) // pins the id
		prompt := remotePeerHeader(t, id) + "\n\n> hello\n" + peer.PeerEndLine(id)
		rules := []fakecli.Rule{
			{Argv: []string{"--machine", "windows", "agent", "get", "w0test:p0a"}, ArgvPrefix: true, Stdout: agentJSONKind("claude", "working", "1")},
			{Argv: []string{"--machine", "windows", "agent", "read", "w0test:p0a", "--source", "visible"}, ArgvPrefix: true, Stdout: workingScreen},
			{Argv: []string{"--machine", "windows", "agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stderr: notIdleErr, Code: 1},
			{Argv: append([]string{"--machine", "windows"}, promptArgv(prompt)...)}}
		f := newFixture(t, rules)
		f.env["CLAUDE_CONFIG_DIR"] = t.TempDir() // present, but a remote target never resolves a transcript
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "20"
		code, _, stderr := f.run([]string{"send", "windows/w0test:p0a", "--now", "hello"})
		want := "herdr-soho: send: windows/w0test:p0a did not confirm taking the message (no sign of it in its state or screen); delivery is uncertain; read its pane for #01020304 or a reply before sending again\n"
		if code != 15 || stderr != want {
			t.Fatalf("a remote target keeps today's outcome without the busy warning: code=%d stderr=%q", code, stderr)
		}
		if gets := countLocalAgentGets(t, f); gets != 0 {
			t.Fatalf("the transcript proof is not armed for a remote target: %d machineless gets", gets)
		}
		if enters := remoteEnters(t, f); enters != 0 {
			t.Fatalf("an unproved remote send presses nothing: %d enters", enters)
		}
		if prompts := remotePrompts(t, f); prompts != 1 {
			t.Fatalf("the remote path never resends the text: %d prompts", prompts)
		}
	})
}
