package peer_test

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/peer"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestSendShortTimeoutWarnsOnceAndProceeds(t *testing.T) { // --timeout 45 means 45 ms: one warning, then the send still happens
	id := "01020304"
	oldReader := rand.Reader
	rand.Reader = bytes.NewReader([]byte{1, 2, 3, 4})
	t.Cleanup(func() { rand.Reader = oldReader })
	prompt := peer.PeerHeader("local/-", "-", "-", "-", id) + "\n\n> hello\n" + peer.PeerEndLine(id)
	history := "old turn output"
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSONKind("pi", "idle", "1")},
		{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: piSendScreen(history, "")},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSONKind("pi", "idle", "1")},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSONKind("pi", "idle", "1")},
		{Argv: []string{"agent", "prompt", "w0test:p0a", prompt, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "15000"}},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 4, Stdout: agentJSONKind("pi", "idle", "1")},
		{Argv: []string{"agent", "read", "w0test:p0a", "--source", "recent-unwrapped"}, ArgvPrefix: true, Call: 1, Delay: 5, Stdout: prompt + "\n"},
		{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 2, Stdout: piSendScreen(history+"\n"+prompt, "")},
	}
	f := newFixture(t, rules)
	if err := os.MkdirAll(filepath.Join(f.dir, "state", "ws-test"), 0o700); err != nil { // the state dir exists in a real workspace before the send
		t.Fatal(err)
	}
	f.env["HERDR_SOHO_SEND_WINDOW_MS"], f.env["HERDR_SOHO_SEND_POLL_MS"] = "1", "1"
	code, out, stderr := f.run([]string{"send", "w0test:p0a", "hello", "--timeout", "45"})
	if code != 0 || out != "sent to local/w0test:p0a\n" {
		t.Fatalf("code=%d out=%q stderr=%q; want the send delivered", code, out, stderr)
	}
	if n := strings.Count(stderr, "send: --timeout is in milliseconds; 45 is under a second (for 45 seconds pass 45000)"); n != 1 {
		t.Fatalf("warning count=%d stderr=%q", n, stderr)
	}
	log, err := os.ReadFile(filepath.Join(f.dir, "state", "ws-test", "friction.log"))
	if err != nil || !strings.Contains(string(log), "warning\tsend\tsend: --timeout is in milliseconds; 45 is under a second (for 45 seconds pass 45000)") {
		t.Fatalf("friction log=%q err=%v", log, err)
	}
}
