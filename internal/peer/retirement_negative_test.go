package peer_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// F9 retirement negative contract (peer): the frozen send.test.mjs cases
// that a wrong or refused send must refuse with the exact message and
// without driving the target or leaking the payload. The exact current
// contract messages are asserted whole, not by substring, and the no-effect
// side is asserted on the Herdr call log and the peer log.
func TestRetirementNegativeContracts(t *testing.T) {
	t.Run("send: empty message and --file with words exit 2 with the exact usage message and no Herdr calls", func(t *testing.T) { // JS: "send empty message rc 2 exact stderr; --file+words rc 2 exact stderr" (scripts/test/send.test.mjs:749,790)
		msgFile := filepath.Join(t.TempDir(), "msg.md")
		if err := os.WriteFile(msgFile, []byte("hello\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		emptyFile := filepath.Join(t.TempDir(), "empty.md")
		if err := os.WriteFile(emptyFile, []byte("\n\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			args []string
			want string
		}{
			{[]string{"send", "w0test:p0a"}, "herdr-soho: send: empty message (pass the message words or --file <path>)\n"},
			{[]string{"send", "w0test:p0a", ""}, "herdr-soho: send: empty message (pass the message words or --file <path>)\n"},
			{[]string{"send", "w0test:p0a", "--file", msgFile, "extra"}, "herdr-soho: send: use either the message words or --file, not both\n"},
			{[]string{"send", "w0test:p0a", "--file", emptyFile}, "herdr-soho: send: empty message (pass the message words or --file <path>)\n"},
		}
		for _, tc := range cases {
			f := newFixture(t, nil)
			code, _, stderr := f.run(tc.args)
			if code != 2 || stderr != tc.want {
				t.Fatalf("args=%v code=%d stderr=%q; want code=2 stderr=%q", tc.args, code, stderr, tc.want)
			}
			calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(calls) != 0 {
				t.Fatalf("usage error reached Herdr: %#v", calls)
			}
		}
	})
	t.Run("send: inbound=off target refuses with the exact message and no prompt, no payload log, no other mutation", func(t *testing.T) { // JS: "inbound=off in the target project exits 18 and sends nothing" (scripts/test/send.test.mjs:790)
		dir := t.TempDir()
		project := filepath.Join(dir, "target")
		if err := os.MkdirAll(filepath.Join(project, ".agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, ".agents", "herdr-soho.conf"), []byte("inbound=off\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// The agent get that resolves the target is the legitimate read the
		// frozen contract keeps; the refusal happens after it.
		f := newFixtureAt(t, dir, []fakecli.Rule{{Argv: []string{"agent", "get", "w0test:p0a"}, Stdout: fmt.Sprintf(`{"result":{"agent":{"pane_id":"w0test:p0a","agent_status":"idle","cwd":%q,"workspace_id":""}}}`, project)}})
		code, _, stderr := f.run([]string{"send", "w0test:p0a", "secret body"})
		if code != 18 || stderr != "herdr-soho: send: local/w0test:p0a does not accept peer messages (inbound=off)\n" {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		for _, call := range calls {
			if len(call.Argv) > 1 && (call.Argv[1] == "prompt" || call.Argv[1] == "send-keys") {
				t.Fatalf("the refused send still drove the target: %#v", call)
			}
		}
		log, err := os.ReadFile(filepath.Join(f.env.Get("HERDR_SOHO_DIR"), "peer-messages.tsv"))
		if err == nil && strings.Contains(string(log), "secret body") {
			t.Fatalf("the refusal logged the payload: %q", log)
		}
	})
}
