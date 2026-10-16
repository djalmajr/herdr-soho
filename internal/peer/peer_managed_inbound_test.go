package peer_test

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/peer"
	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// TestBoundSendKeepsTheTargetInboundPolicy: a team command bound to the job
// starter's selection sends with a sender configuration loaded from that
// selection (cli.Run: LoadConfigFor with the selection, then CmdSend). A
// permissive starter user file (inbound=auto) never overrides the target's
// restrictive baseline (inbound=off in the server's user file): the send
// refuses with exit 18 and no prompt or keys reach the target. Control:
// without the restrictive baseline the same bound send is delivered.
func TestBoundSendKeepsTheTargetInboundPolicy(t *testing.T) {
	dir := t.TempDir()
	id := "01020304"
	oldReader := rand.Reader
	defer func() { rand.Reader = oldReader }()
	prompt := peer.PeerHeader("local/-", "-", "-", "-", id) + "\n\n> hi\n" + peer.PeerEndLine(id)
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSON("idle", "1")},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 2, Stdout: agentJSON("idle", "1")},
		{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 1, Stdout: "before\n"},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 3, Stdout: agentJSON("idle", "1")},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 4, Stdout: agentJSON("idle", "1")},
		{Argv: []string{"agent", "prompt", "w0test:p0a", prompt}, ArgvPrefix: true},
		{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 5, Stdout: agentJSON("working", "2")},
		{Argv: []string{"agent", "read", "w0test:p0a", "--source", "visible"}, Call: 2, Stdout: "after prompt\n"},
	}
	f := newFixtureAt(t, dir, rules)
	serverUser := filepath.Join(dir, "config", "soho", "config")
	if err := os.MkdirAll(filepath.Dir(serverUser), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverUser, []byte("inbound=off\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	starter := filepath.Join(dir, "starter")
	if err := os.MkdirAll(filepath.Join(starter, "soho"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(starter, "soho", "config"), []byte("inbound=auto\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(dir, "home")
	// send runs CmdSend as cli.Run does: a platform.Die is recovered into
	// its exit code and message.
	send := func() (code int, stdout, stderr string) {
		t.Helper()
		ctx := core.LoadConfigFor(f.env, cwd, core.UserSelection{Base: starter})
		if got := ctx.Entries["inbound"].Value; got != "auto" {
			t.Fatalf("the bound sender's configuration has inbound=%q, want the starter's auto", got)
		}
		oldOut, oldErr := platform.Stdout, platform.Stderr
		var out, errOut bytes.Buffer
		platform.Stdout, platform.Stderr = &out, &errOut
		defer func() {
			platform.Stdout, platform.Stderr = oldOut, oldErr
			if value := recover(); value != nil {
				exitErr, ok := value.(*platform.ExitError)
				if !ok {
					panic(value)
				}
				code, stderr = exitErr.Code, errOut.String()+exitErr.Msg
			}
			stdout = out.String()
		}()
		rand.Reader = bytes.NewReader([]byte{1, 2, 3, 4})
		code = peer.CmdSend([]string{"w0test:p0a", "hi"}, &ctx, f.env, cwd)
		return code, out.String(), errOut.String()
	}
	calls := func() []string {
		t.Helper()
		recorded, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
		if err != nil {
			t.Fatal(err)
		}
		var verbs []string
		for _, call := range recorded {
			if len(call.Argv) > 1 {
				verbs = append(verbs, call.Argv[0]+" "+call.Argv[1])
			}
		}
		return verbs
	}

	code, _, stderr := send()
	if code != 18 || !strings.Contains(stderr, "inbound=off") {
		t.Fatalf("bound send with a restrictive target: code=%d stderr=%q", code, stderr)
	}
	for _, verb := range calls() {
		if strings.HasSuffix(verb, " prompt") || strings.Contains(verb, "keys") || strings.HasSuffix(verb, " send") {
			t.Fatalf("a refused send reached the target: %s", verb)
		}
	}

	if err := os.Remove(serverUser); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := send()
	if code != 0 || out != "sent to local/w0test:p0a\n" || stderr != "" {
		t.Fatalf("control: code=%d out=%q stderr=%q", code, out, stderr)
	}
	prompts := 0
	for _, verb := range calls() {
		if strings.HasSuffix(verb, " prompt") {
			prompts++
		}
	}
	if prompts != 1 {
		t.Fatalf("control: prompts=%d", prompts)
	}
}
