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

// The rc.10 stall: `agent prompt` returns agent_prompt_stalled because the
// Enter of the typed text never landed, so the message sits in the input box
// while the state stays idle. The send reads the target's visible screen with
// the target's machine and, only when this message's marker is in the box,
// presses one Enter and runs the proof window: proven is sent (exit 0),
// unproved is the new 15, and a marker out of the box keeps today's 15.
// agent_blocked and timeout keep today's exit untouched.

const stalledPromptErr = `{"error":{"code":"agent_prompt_stalled","message":"stalled"}}`

// stalledID seeds the message id as the pi/remote tests do and returns the
// id and the exact prompt body the fake herdr must receive.
func stalledID(t *testing.T) (id, prompt string) {
	t.Helper()
	id = "01020304"
	oldReader := rand.Reader
	rand.Reader = bytes.NewReader([]byte{1, 2, 3, 4})
	t.Cleanup(func() { rand.Reader = oldReader })
	return id, peer.PeerHeader("local/-", "-", "-", "-", id) + "\n\n> hello\n" + peer.PeerEndLine(id)
}

// countPromptCalls counts `agent prompt` calls: the stalled path must never
// resend the text.
func countPromptCalls(t *testing.T, f *fixture) int {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	prompts := 0
	for _, call := range calls {
		if len(call.Argv) > 1 && call.Argv[1] == "prompt" {
			prompts++
		}
	}
	return prompts
}

// promptIsLastCall asserts the prompt call is the final herdr call: blocked
// and timeout keep today's exit, with no screen read or Enter after it.
func promptIsLastCall(t *testing.T, f *fixture) {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for i, call := range calls {
		if len(call.Argv) > 1 && call.Argv[1] == "prompt" {
			last = i
		}
	}
	if last != len(calls)-1 {
		t.Fatalf("a call after the prompt (today's exit, nothing after): %+v", calls)
	}
}

func TestSendStalledEnter(t *testing.T) {
	t.Run("stalled: the id in the pi input box gets one Enter and the proof passes", func(t *testing.T) {
		_, prompt := stalledID(t)
		history := "old turn output"
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: piSendScreen(history, "")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}, Stderr: stalledPromptErr, Code: 1},
			// The stalled read of the visible screen: the message still sits
			// in the box, between the two last '─' lines.
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 2, Stdout: piSendScreen(history, prompt)},
			{Argv: []string{"agent", "send-keys", "w0test:p0a", "enter"}, Code: 0},
			// After the Enter the state goes working: the sequence change
			// proves the turn.
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 4, Stdout: agentJSONKind("pi", "working", "2")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 3, Stdout: piSendScreen(history+"\n"+prompt, "")},
		}
		f := newFixture(t, rules)
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
		code, out, stderr := f.run([]string{"send", "w0test:p0a", "hello"})
		if code != 0 || out != "sent to local/w0test:p0a\n" || stderr != "" {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
		if enters := countSendKeyEnters(t, f); enters != 1 {
			t.Fatalf("the stalled message in the box needs exactly one Enter: %d", enters)
		}
		if prompts := countPromptCalls(t, f); prompts != 1 {
			t.Fatalf("the stalled path never resends the text: %d prompts", prompts)
		}
	})
	t.Run("stalled: the id in the pi input box but the Enter does not start a turn", func(t *testing.T) {
		_, prompt := stalledID(t)
		history := "old turn output"
		box := piSendScreen(history, prompt)
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: piSendScreen(history, "")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}, Stderr: stalledPromptErr, Code: 1},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 2, Stdout: box},
			{Argv: []string{"agent", "send-keys", "w0test:p0a", "enter"}, Code: 0},
			// The proof window: the state stays idle with the same sequence
			// and the message still sits in the box, so the window fails.
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 4, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Call: 1, Stdout: prompt + "\n"},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 3, Stdout: box},
			// Catch-alls for a second poll iteration on a slow machine.
			{Argv: []string{"agent", "get", "w0test:p0a"}, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stdout: prompt + "\n"},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Stdout: box},
		}
		f := newFixture(t, rules)
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
		code, _, stderr := f.run([]string{"send", "w0test:p0a", "hello"})
		want := "herdr-soho: send: local/w0test:p0a did not take the message: it sits in its input box after one Enter; read its pane before sending again\n"
		if code != 15 || stderr != want {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if enters := countSendKeyEnters(t, f); enters != 1 {
			t.Fatalf("the failed second proof still gets one Enter only: %d", enters)
		}
		if prompts := countPromptCalls(t, f); prompts != 1 {
			t.Fatalf("the stalled path never resends the text: %d prompts", prompts)
		}
	})
	t.Run("stalled: a dialog on screen gets no Enter and exits 17", func(t *testing.T) {
		_, prompt := stalledID(t)
		history := "old turn output"
		// The message sits in the box and a trust dialog shares the tail:
		// the Enter would answer the dialog, so the send presses nothing.
		box := piSendScreen(history, prompt+"\nDo you trust this workspace? [y/N]")
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: piSendScreen(history, "")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}, Stderr: stalledPromptErr, Code: 1},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 2, Stdout: box},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "send-keys", "w0test:p0a", "enter"}, Code: 0},
		}
		f := newFixture(t, rules)
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
		code, _, stderr := f.run([]string{"send", "w0test:p0a", "hello"})
		want := "herdr-soho: send: local/w0test:p0a is showing a dialog after the message was typed; press nothing and read its pane\n"
		if code != 17 || stderr != want {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if enters := countSendKeyEnters(t, f); enters != 0 {
			t.Fatalf("a dialog on screen must not get the stalled Enter: %d", enters)
		}
		if prompts := countPromptCalls(t, f); prompts != 1 {
			t.Fatalf("the stalled path never resends the text: %d prompts", prompts)
		}
	})
	t.Run("stalled: without the id in the box it keeps today's 15 and presses nothing", func(t *testing.T) {
		_, prompt := stalledID(t)
		_ = prompt
		history := "old turn output"
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: piSendScreen(history, "")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSONKind("pi", "idle", "1")},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}, Stderr: stalledPromptErr, Code: 1},
			// The stalled read of the visible screen: the box is empty, so no
			// Enter and today's exit with the cause.
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 2, Stdout: piSendScreen(history, "")},
		}
		f := newFixture(t, rules)
		code, _, stderr := f.run([]string{"send", "w0test:p0a", "hello"})
		want := "herdr-soho: send: local/w0test:p0a did not take the message (agent_prompt_stalled: stalled); read its pane before sending again\n"
		if code != 15 || stderr != want {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if enters := countSendKeyEnters(t, f); enters != 0 {
			t.Fatalf("no Enter without the marker in the box: %d", enters)
		}
		if prompts := countPromptCalls(t, f); prompts != 1 {
			t.Fatalf("the stalled path never resends the text: %d prompts", prompts)
		}
	})
	t.Run("stalled: a non-pi kind keeps the last-15-lines rule for the box", func(t *testing.T) {
		_, prompt := stalledID(t)
		// No '─' separator lines: for the claude kind the box is the last 15
		// lines of the visible screen, as before.
		pre := "chrome line\n❯ \n"
		box := prompt + "\n❯ \n"
		post := peer.PeerEndLine("01020304") + "\n✻ done\n❯ \n"
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSONKind("claude", "idle", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: pre},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSONKind("claude", "idle", "1")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSONKind("claude", "idle", "1")},
			// The claude transcript-path get: this agent has no
			// agent_session, so the transcript proof is not armed.
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 4, Stdout: agentJSONKind("claude", "idle", "1")},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}, Stderr: stalledPromptErr, Code: 1},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 2, Stdout: box},
			{Argv: []string{"agent", "send-keys", "w0test:p0a", "enter"}, Code: 0},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 5, Stdout: agentJSONKind("claude", "working", "2")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 3, Stdout: post},
		}
		f := newFixture(t, rules)
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
		code, out, stderr := f.run([]string{"send", "w0test:p0a", "hello"})
		if code != 0 || out != "sent to local/w0test:p0a\n" || stderr != "" {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
		if enters := countSendKeyEnters(t, f); enters != 1 {
			t.Fatalf("the non-pi stalled message in the box needs exactly one Enter: %d", enters)
		}
		if prompts := countPromptCalls(t, f); prompts != 1 {
			t.Fatalf("the stalled path never resends the text: %d prompts", prompts)
		}
	})
	t.Run("agent_blocked keeps today's exit with nothing after the prompt", func(t *testing.T) {
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSON("blocked", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Stdout: "screen\n"},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSON("blocked", "1")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSON("blocked", "1")},
			{Argv: []string{"agent", "prompt", "w0test:p0a"}, ArgvPrefix: true, AnyArgs: true, Stderr: `{"error":{"code":"agent_blocked","message":"blocked"}}`, Code: 1},
		}
		f := newFixture(t, rules)
		code, _, stderr := f.run([]string{"send", "w0test:p0a", "--now", "hello"})
		if code != 15 || !strings.Contains(stderr, "did not take the message (agent_blocked") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if enters := countSendKeyEnters(t, f); enters != 0 {
			t.Fatalf("agent_blocked presses nothing: %d", enters)
		}
		promptIsLastCall(t, f)
	})
	t.Run("a prompt timeout keeps today's exit with nothing after the prompt", func(t *testing.T) {
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSON("idle", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Stdout: "before\n"},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSON("idle", "1")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSON("idle", "1")},
			{Argv: []string{"agent", "prompt", "w0test:p0a"}, ArgvPrefix: true, AnyArgs: true, Stderr: `{"error":{"code":"timeout","message":"timeout"}}`, Code: 1},
		}
		f := newFixture(t, rules)
		code, _, stderr := f.run([]string{"send", "w0test:p0a", "hello"})
		if code != 15 || !strings.Contains(stderr, "did not take the message (timeout)") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if enters := countSendKeyEnters(t, f); enters != 0 {
			t.Fatalf("a timeout presses nothing: %d", enters)
		}
		promptIsLastCall(t, f)
	})
}
