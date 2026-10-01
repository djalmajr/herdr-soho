package peer_test

import (
	"bytes"
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/peer"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// notIdleErr is the structured herdr error for a `recent` read of a working
// full-screen agent: its alternate-screen history is only captured while the
// agent is idle.
const notIdleErr = `{"error":{"code":"agent_not_idle","message":"cannot read 67 lines while the agent is working: its alternate-screen history can only be captured by scrolling while idle"}}`

// claudeSendScreen models a working Claude Code visible screen (cinzel): the
// sent message fills the screen, the `ctrl+enter` hint sits below it, and the
// queued-messages footer line is present while Claude's queue is open.
func claudeSendScreen(message string, queueLine bool) string {
	out := "✻ Cranking out changes…\n" + message + "\nctrl+enter to send now\n"
	if queueLine {
		out += "Press up to edit queued messages\n"
	}
	return out
}

// TestSendClaudeQueue covers the send fix for a working Claude Code worker:
// a `recent` read that fails with agent_not_idle falls back to the visible
// screen, and the visible Claude queue (the message's id plus the
// `Press up to edit queued messages` line) is reported queued with no Enter.
func TestSendClaudeQueue(t *testing.T) {
	// The delivered message: header + quoted body + end line, id 01020304.
	newPromptFixture := func(t *testing.T) string {
		id := "01020304"
		oldReader := rand.Reader
		rand.Reader = bytes.NewReader([]byte{1, 2, 3, 4})
		t.Cleanup(func() { rand.Reader = oldReader })
		return peer.PeerHeader("local/-", "-", "-", "-", id) + "\n\n> hello\n" + peer.PeerEndLine(id)
	}
	countEnters := func(t *testing.T, f *fixture) int {
		t.Helper()
		calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
		if err != nil {
			t.Fatal(err)
		}
		enters := 0
		for _, call := range calls {
			if len(call.Argv) > 3 && call.Argv[1] == "send-keys" && call.Argv[3] == "enter" {
				enters++
			}
		}
		return enters
	}
	countVisibleReads := func(t *testing.T, f *fixture) int {
		t.Helper()
		calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, call := range calls {
			if len(call.Argv) == 5 && call.Argv[1] == "read" && call.Argv[4] == "visible" {
				n++
			}
		}
		return n
	}

	t.Run("claude working: an agent_not_idle recent read falls back to visible and the Claude queue is queued without Enter", func(t *testing.T) {
		prompt := newPromptFixture(t)
		pre := "✻ Cranking out changes…\n"
		queued := claudeSendScreen(prompt, true)
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, ArgvPrefix: true, Stdout: agentJSONKind("claude", "working", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: pre},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, ArgvPrefix: true, Stdout: queued},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stderr: notIdleErr, Code: 1},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}},
		}
		f := newFixture(t, rules)
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
		code, out, stderr := f.run([]string{"send", "w0test:p0a", "--now", "hello"})
		if code != 0 || !strings.HasPrefix(out, "queued for local/w0test:p0a") || stderr != "" {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
		if enters := countEnters(t, f); enters != 0 {
			t.Fatalf("no Enter may go to a busy claude with a queued message: %d enters", enters)
		}
	})
	t.Run("claude working: the same screen without the queue line keeps today's behavior (Enter, then lost)", func(t *testing.T) {
		prompt := newPromptFixture(t)
		pre := "✻ Cranking out changes…\n"
		noQueue := claudeSendScreen(prompt, false)
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, ArgvPrefix: true, Stdout: agentJSONKind("claude", "working", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: pre},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, ArgvPrefix: true, Stdout: noQueue},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stderr: notIdleErr, Code: 1},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}},
			{Argv: []string{"agent", "send-keys", "w0test:p0a", "enter"}},
		}
		f := newFixture(t, rules)
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
		code, _, stderr := f.run([]string{"send", "w0test:p0a", "--now", "hello"})
		if code != 15 || !strings.Contains(stderr, "did not take the message (no sign of it in its state or screen)") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if enters := countEnters(t, f); enters != 1 {
			t.Fatalf("today's behavior sends the box Enter once: %d enters", enters)
		}
	})
	t.Run("claude working: another recent error is not replaced by a visible read and exits 15 unverified", func(t *testing.T) {
		prompt := newPromptFixture(t)
		pre := "✻ Cranking out changes…\n"
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, ArgvPrefix: true, Stdout: agentJSONKind("claude", "working", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: pre},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stderr: "could not capture the alternate screen", Code: 1},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}},
		}
		f := newFixture(t, rules)
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
		code, _, stderr := f.run([]string{"send", "w0test:p0a", "--now", "hello"})
		if code != 15 || !strings.Contains(stderr, "could not confirm that local/w0test:p0a took the message (could not capture the alternate screen)") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if reads := countVisibleReads(t, f); reads != 1 {
			t.Fatalf("no visible read may replace a non-agent_not_idle recent error: %d visible reads", reads)
		}
		if enters := countEnters(t, f); enters != 0 {
			t.Fatalf("an unverified send sends no keys: %d enters", enters)
		}
	})
	t.Run("proof window: the Claude queue line on the visible screen is not proof, so a pi screen with the id outside the box is queued", func(t *testing.T) {
		prompt := newPromptFixture(t)
		// The message sits in the chat history above the empty input box,
		// next to Claude's queued-messages line: the pi box check alone would
		// prove it as delivered; the Claude queue must keep it queued.
		history := "old turn output\n" + prompt + "\nPress up to edit queued messages"
		screen := piSendScreen(history, "")
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, ArgvPrefix: true, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: piSendScreen("old turn output", "")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, ArgvPrefix: true, Stdout: screen},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stderr: notIdleErr, Code: 1},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}},
		}
		f := newFixture(t, rules)
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
		code, out, stderr := f.run([]string{"send", "w0test:p0a", "--now", "hello"})
		if code != 0 || !strings.HasPrefix(out, "queued for local/w0test:p0a") || stderr != "" {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
		if enters := countEnters(t, f); enters != 0 {
			t.Fatalf("no Enter may go to a queued screen: %d enters", enters)
		}
	})
}
