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

// piWorkingSendScreen models the real pi 0.99.2 visible screen of a working
// target (evidence-rc10/pi-steer-t1.txt): chat history above, the input box
// between the `── ⠴ Working ──…` top border and the '─' bottom border (box may
// be empty), and the footer below.
func piWorkingSendScreen(history, box string) string {
	sep := strings.Repeat("─", 66)
	working := "── \u2834 Working " + strings.Repeat("─", 54)
	foot := "~/code/herdr-soho (fix/send-pi-input-box...)\n↑5.2M ↓43k 40.3%/262k (auto)           (lbvllm) qwen3.8-27b • high\n"
	return history + "\n\n" + working + "\n" + box + "\n" + sep + "\n" + foot
}

// TestSendPiWorkingBorder pins the rc.8 send behavior on a working pi whose
// box border carries the activity indicator: the region between the Working
// border and the '─' border is the input box, so a message typed in the box
// is not proof of delivery (not "sent"), and a message pi took sits in the
// history above the Working border, which is proof.
func TestSendPiWorkingBorder(t *testing.T) {
	newPromptFixture := func(t *testing.T) (prompt string) {
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

	t.Run("pi working: a message in the box under the Working border is not sent and gets one Enter", func(t *testing.T) {
		prompt := newPromptFixture(t)
		inBox := piWorkingSendScreen("old turn output", prompt)
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: piWorkingSendScreen("old turn output", "")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 4, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Delay: 5, Stdout: prompt + "\n"},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 2, Stdout: inBox},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 3, Stdout: inBox},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 5, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "send-keys", "w0test:p0a", "enter"}},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 6, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 4, Stdout: inBox},
		}
		f := newFixture(t, rules)
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
		code, out, stderr := f.run([]string{"send", "w0test:p0a", "--now", "hello"})
		if code != 15 || strings.Contains(out, "sent to") || !strings.Contains(stderr, "did not confirm taking the message (no sign of it in its state or screen)") {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
		if enters := countEnters(t, f); enters != 1 {
			t.Fatalf("the box message must get its Enter from the region check: %d enters", enters)
		}
	})
	t.Run("pi working: a message pi took above the Working border is sent without Enter", func(t *testing.T) {
		prompt := newPromptFixture(t)
		taken := piWorkingSendScreen("old turn output\n"+prompt, "")
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: piWorkingSendScreen("old turn output", "")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 4, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Delay: 5, Stdout: prompt + "\n"},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 2, Stdout: taken},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 3, Stdout: taken},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 5, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "send-keys", "w0test:p0a", "enter"}},
			{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 6, Stdout: agentJSONKind("pi", "working", "1")},
			{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 4, Stdout: taken},
		}
		f := newFixture(t, rules)
		f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
		code, out, stderr := f.run([]string{"send", "w0test:p0a", "--now", "hello"})
		if code != 0 || out != "sent to local/w0test:p0a\n" || stderr != "" {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
		if enters := countEnters(t, f); enters != 0 {
			t.Fatalf("no Enter may go to a message pi already took: %d enters", enters)
		}
	})
}
