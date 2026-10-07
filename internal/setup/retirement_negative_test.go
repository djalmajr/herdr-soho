package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// F9 retirement negative contract (setup): the frozen test-setup.sh and
// test-probe.sh cases that a wrong flag combination or an invalid probe
// must refuse with the exact message before any write, state dir or
// provider CLI effect. The exact current contract messages are asserted
// whole, not by substring.
func TestRetirementNegativeContracts(t *testing.T) {
	t.Run("setup: --local and --target are exclusive with the exact message, empty stdout and no filesystem change", func(t *testing.T) { // JS: "setup --local --target -> rc 2, exact exclusive message, no writes" (scripts/test-setup.sh)
		root, env, ctx := setupWriteFixture(t)
		before := setupFilesystemSnapshot(t, root)
		code, out, msg := runSetupWrite([]string{"--local", "--target", "GUIDE.md"}, ctx, env, root)
		if code != 2 || out != "" || msg != "setup: --local and --target are exclusive" {
			t.Fatalf("code=%d stdout=%q msg=%q; want code=2, empty stdout, %q", code, out, msg, "setup: --local and --target are exclusive")
		}
		after := setupFilesystemSnapshot(t, root)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("the exclusive flags still wrote the filesystem: before=%v after=%v", before, after)
		}
	})
	t.Run("setup --probe: invalid conditions refuse with the exact message before any state dir or provider CLI", func(t *testing.T) { // JS: "no state dir on invalid probe; no CLI on bad timeout; no CLI on --model without --kind" (scripts/test-probe.sh:235,244,271)
		cases := []struct {
			args []string
			msg  string
		}{
			{[]string{"--probe", "--kind", "notepad"}, "setup --probe: unknown kind 'notepad' (see: kinds)"},
			{[]string{"--probe", "--kind", "pi", "--timeout", "0"}, "setup --probe: timeout must be a whole number of seconds ≥ 1"},
			{[]string{"--probe", "--kind", "pi", "--timeout", "abc"}, "setup --probe: timeout must be a whole number of seconds ≥ 1"},
			{[]string{"--probe", "--model", "requested"}, "setup --probe: --model needs --kind (probe one kind/model: --kind K --model M)"},
			{[]string{"--probe", "--kind"}, "setup --probe: --kind expects a value"},
		}
		for _, tc := range cases {
			bin := t.TempDir()
			if _, err := fakecli.Install(t, bin, "pi", []fakecli.Rule{{AnyArgs: true, Stdout: "models\n"}}); err != nil {
				t.Fatal(err)
			}
			env := fakeEnv(t, bin)
			env["HERDR_SOHO_SKILL_DIR"] = filepath.Join("..", "..", "skills", "herdr-soho")
			root := t.TempDir()
			ctx := core.LoadConfig(env, root)
			code, out, msg := runSetupWrite(tc.args, &ctx, env, root)
			if code != 2 || out != "" || msg != tc.msg {
				t.Fatalf("args=%v code=%d stdout=%q msg=%q; want code=2, empty stdout, %q", tc.args, code, out, msg, tc.msg)
			}
			if _, err := os.Stat(filepath.Join(root, ".herdr-soho")); !os.IsNotExist(err) {
				t.Fatalf("the invalid probe opened the state dir: %v", err)
			}
			if _, err := os.Stat(filepath.Join(bin, "pi.calls.jsonl")); !os.IsNotExist(err) {
				t.Fatalf("the invalid probe executed the provider CLI: %v", err)
			}
		}
	})
}
