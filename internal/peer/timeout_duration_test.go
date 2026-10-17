package peer_test

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/peer"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

func TestSendTimeoutDurationUnits(t *testing.T) {
	newSendFixture := func(t *testing.T) *fixture {
		t.Helper()
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
		f.env["SOHO_SEND_WINDOW_MS"], f.env["SOHO_SEND_POLL_MS"] = "1", "1"
		return f
	}
	t.Run("--timeout 1s sends and never warns", func(t *testing.T) {
		f := newSendFixture(t)
		code, out, stderr := f.run([]string{"send", "w0test:p0a", "hello", "--timeout", "1s"})
		if code != 0 || out != "sent to local/w0test:p0a\n" {
			t.Fatalf("code=%d out=%q stderr=%q; want the send delivered", code, out, stderr)
		}
		if strings.Contains(stderr, "--timeout is in milliseconds") {
			t.Fatalf("a unit value must not warn: stderr=%q", stderr)
		}
		log, _ := os.ReadFile(filepath.Join(f.dir, "state", "ws-test", "friction.log"))
		if strings.Contains(string(log), "--timeout is in milliseconds") {
			t.Fatalf("a unit value must not reach the friction log: %q", log)
		}
	})
	t.Run("an invalid duration exits 2 before any herdr call", func(t *testing.T) {
		f := newFixture(t, nil)
		code, out, stderr := f.run([]string{"send", "w0test:p0a", "hello", "--timeout", "1h30m"})
		if code != 2 || out != "" {
			t.Fatalf("code=%d out=%q; want exit 2", code, out)
		}
		if stderr != "soho: send: --timeout expects the wait in milliseconds or a duration such as 90s, 30m or 2h\n" {
			t.Fatalf("stderr=%q", stderr)
		}
		calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
		if (err != nil && !os.IsNotExist(err)) || (err == nil && len(calls) != 0) {
			t.Fatalf("herdr calls=%+v err=%v; the refusal must cost no subprocess", calls, err)
		}
	})
}

func TestFindTimeoutDurationUnits(t *testing.T) {
	t.Run("--timeout 1m runs the discovery with the unit deadline", func(t *testing.T) {
		f := newFixture(t, []fakecli.Rule{
			{Argv: []string{"api", "snapshot"}, Stdout: `{"result":{"snapshot":` + localSnapshot + `}}`},
		})
		code, out, stderr := f.run([]string{"find", "build", "--timeout", "1m"})
		if code != 0 || stderr != "" {
			t.Fatalf("code=%d out=%q stderr=%q; want the find to proceed with the unit deadline", code, out, stderr)
		}
		if !strings.Contains(out, "local/w1:p1") {
			t.Fatalf("out=%q; the matching row is missing", out)
		}
	})
	t.Run("an invalid duration exits 2 with the unit message before any herdr call", func(t *testing.T) {
		f := newFixture(t, []fakecli.Rule{{Argv: []string{"api", "snapshot"}, Stdout: `{"result":{"snapshot":` + localSnapshot + `}}`}})
		code, out, stderr := f.run([]string{"find", "build", "--timeout", "1h30m"})
		if code != 2 || out != "" {
			t.Fatalf("code=%d out=%q; want exit 2", code, out)
		}
		if stderr != "soho: find: --timeout expects the deadline in milliseconds or a duration such as 90s, 30m or 2h\n" {
			t.Fatalf("stderr=%q", stderr)
		}
		calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
		if (err != nil && !os.IsNotExist(err)) || (err == nil && len(calls) != 0) {
			t.Fatalf("herdr calls=%+v err=%v; the refusal must cost no subprocess", calls, err)
		}
	})
}
