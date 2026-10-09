package job

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// herdrFixture is one hermetic herdr-CLI scenario: a fake herdr on an
// isolated PATH with its own HOME and XDG_CONFIG_HOME. It never touches the
// real herdr binary or a Herdr server.
type herdrFixture struct {
	t       *testing.T
	fakeDir string
	env     platform.Env
}

func newHerdrFixture(t *testing.T, rules []fakecli.Rule) *herdrFixture {
	t.Helper()
	fakeDir := t.TempDir()
	if _, err := fakecli.Install(t, fakeDir, "herdr", rules); err != nil {
		t.Fatalf("install the fake herdr: %v", err)
	}
	env := platform.Env{}
	for _, item := range fakecli.Env(nil, fakeDir) {
		key, value, ok := strings.Cut(item, "=")
		if ok && key != "" {
			env[key] = value
		}
	}
	env["HOME"] = t.TempDir()
	env["XDG_CONFIG_HOME"] = t.TempDir()
	return &herdrFixture{t: t, fakeDir: fakeDir, env: env}
}

func (f *herdrFixture) cli() *herdrCLI {
	return newHerdrCLI(f.env, nil)
}

// cliWithFriction wires a friction collector into the CLI; the raw
// friction sink stays nil unless the test sets it.
func (f *herdrFixture) cliWithFriction(messages *[]string) *herdrCLI {
	return newHerdrCLI(f.env, func(message string) { *messages = append(*messages, message) })
}

// calls reads the fake herdr call log; a missing log means no call ran.
func (f *herdrFixture) calls() []fakecli.Call {
	calls, err := fakecli.ReadCalls(filepath.Join(f.fakeDir, "herdr.calls.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		f.t.Fatalf("read the herdr call log: %v", err)
	}
	return calls
}

const (
	herdrTestCwd   = "/fixture/work"
	herdrTestLabel = "job TASK-1.a"
)

func herdrTestCreateArgv() []string {
	return []string{"workspace", "create", "--cwd", herdrTestCwd, "--label", herdrTestLabel, "--env", "A=1", "--env", "B=2", "--no-focus"}
}

func TestHerdrWorkspacesCreate(t *testing.T) {
	env := map[string]string{"B": "2", "A": "1"}
	t.Run("both envelope shapes parse into the workspace and root pane", func(t *testing.T) {
		for name, stdout := range map[string]string{
			"workspace object":   `{"id":"cli:x","result":{"workspace":{"workspace_id":"w9","root_pane":{"pane_id":"w9:p1"}}}}`,
			"flat with tab":      `{"id":"cli:x","result":{"workspace_id":"w9","tab":{"root_pane":{"pane_id":"w9:p1"}}}}`,
			"flat with pane":     `{"result":{"workspace_id":"w9","root_pane":{"pane_id":"w9:p1"}}}`,
			"workspace id win":   `{"result":{"workspace":{"workspace_id":"ws-nested","root_pane":{"pane_id":"w9:p1"}},"workspace_id":"ws-flat"}}`,
			"nested pane win":    `{"result":{"workspace_id":"w9","root_pane":{"pane_id":"flat"},"workspace":{"root_pane":{"pane_id":"nested"}}}}`,
			"workspace pane win": `{"result":{"workspace":{"workspace_id":"w9","root_pane":{"pane_id":"ws-p1"}},"tab":{"root_pane":{"pane_id":"tab-p1"}}}}`,
		} {
			t.Run(name, func(t *testing.T) {
				f := newHerdrFixture(t, []fakecli.Rule{{Argv: herdrTestCreateArgv(), Stdout: stdout}})
				workspaceID, rootPaneID, err := f.cli().Create(herdrTestCwd, herdrTestLabel, env)
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				wantWS, wantPane := "w9", "w9:p1"
				if name == "workspace id win" {
					wantWS = "ws-nested"
				}
				if name == "nested pane win" {
					wantPane = "flat"
				}
				if name == "workspace pane win" {
					wantPane = "ws-p1"
				}
				if workspaceID != wantWS || rootPaneID != wantPane {
					t.Fatalf("Create = %q %q, want %q %q", workspaceID, rootPaneID, wantWS, wantPane)
				}
				calls := f.calls()
				if len(calls) != 1 || !sameArgv(calls[0].Argv, herdrTestCreateArgv()) {
					t.Fatalf("herdr calls = %v, want exactly %v", calls, herdrTestCreateArgv())
				}
			})
		}
	})
	t.Run("a non-zero exit is a process failure without friction", func(t *testing.T) {
		var messages []string
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: herdrTestCreateArgv(), Code: 1, Stderr: "token=do-not-print"}})
		_, _, err := f.cliWithFriction(&messages).Create(herdrTestCwd, herdrTestLabel, env)
		wantExit(t, err, ExitHerdr, "job: herdr workspace create failed")
		if len(messages) != 0 {
			t.Fatalf("friction messages = %v, want none", messages)
		}
	})
}

func TestHerdrWorkspacesCreateUnknownShape(t *testing.T) {
	for name, stdout := range map[string]string{
		"empty result":    `{"result":{}}`,
		"top-level array": `[]`,
		"not json":        `not json`,
		"missing pane":    `{"result":{"workspace_id":"w9"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var messages, raw []string
			f := newHerdrFixture(t, []fakecli.Rule{{Argv: herdrTestCreateArgv(), Stdout: stdout}})
			cli := f.cliWithFriction(&messages)
			cli.RawFriction = func(message string) { raw = append(raw, message) }
			_, _, err := cli.Create(herdrTestCwd, herdrTestLabel, map[string]string{"A": "1", "B": "2"})
			wantExit(t, err, ExitHerdr, "job: herdr workspace create returned an unknown result")
			if len(messages) != 0 {
				t.Fatalf("normal friction = %v, want none (the raw line only goes to the raw sink)", messages)
			}
			if len(raw) != 1 {
				t.Fatalf("raw friction calls = %d, want 1", len(raw))
			}
			want := "job: herdr workspace create returned an unknown result: " + core.FrictionSafe(stdout)
			if raw[0] != want {
				t.Fatalf("raw friction = %q, want %q", raw[0], want)
			}
			if strings.Contains(err.Error(), stdout) {
				t.Fatalf("error %q carries the raw envelope", err)
			}
		})
	}
	t.Run("a long envelope is cut to 2000 bytes in the raw sink", func(t *testing.T) {
		stdout := strings.Repeat("a", 2500)
		var messages, raw []string
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: herdrTestCreateArgv(), Stdout: stdout}})
		cli := f.cliWithFriction(&messages)
		cli.RawFriction = func(message string) { raw = append(raw, message) }
		_, _, err := cli.Create(herdrTestCwd, herdrTestLabel, map[string]string{"A": "1", "B": "2"})
		wantExit(t, err, ExitHerdr, "job: herdr workspace create returned an unknown result")
		if len(messages) != 0 {
			t.Fatalf("normal friction = %v, want none", messages)
		}
		if len(raw) != 1 {
			t.Fatalf("raw friction calls = %d, want 1", len(raw))
		}
		want := "job: herdr workspace create returned an unknown result: " + strings.Repeat("a", 2000)
		if raw[0] != want {
			t.Fatalf("raw friction length = %d, want %d", len(raw[0]), len(want))
		}
	})
	t.Run("a nil raw sink drops the raw line without a fallback", func(t *testing.T) {
		var messages []string
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: herdrTestCreateArgv(), Stdout: `{"result":{"token":"x"}}`}})
		_, _, err := f.cliWithFriction(&messages).Create(herdrTestCwd, herdrTestLabel, map[string]string{"A": "1", "B": "2"})
		wantExit(t, err, ExitHerdr, "job: herdr workspace create returned an unknown result")
		if len(messages) != 0 {
			t.Fatalf("normal friction = %v, want none (no fallback to the normal sink)", messages)
		}
	})
}

func TestHerdrWorkspacesServerReachable(t *testing.T) {
	t.Run("exit 0 with a result key is reachable", func(t *testing.T) {
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: []string{"workspace", "list"}, Stdout: `{"id":"cli:x","result":[]}`}})
		if !f.cli().ServerReachable() {
			t.Fatal("ServerReachable = false, want true")
		}
		assertListOnly(t, f)
	})
	t.Run("a non-zero exit is unreachable", func(t *testing.T) {
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: []string{"workspace", "list"}, Code: 1}})
		if f.cli().ServerReachable() {
			t.Fatal("ServerReachable = true after a non-zero exit")
		}
		assertListOnly(t, f)
	})
	t.Run("non-JSON stdout is unreachable", func(t *testing.T) {
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: []string{"workspace", "list"}, Stdout: "not json"}})
		if f.cli().ServerReachable() {
			t.Fatal("ServerReachable = true for non-JSON stdout")
		}
		assertListOnly(t, f)
	})
	t.Run("a JSON object without a result key is unreachable", func(t *testing.T) {
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: []string{"workspace", "list"}, Stdout: `{"id":"cli:x"}`}})
		if f.cli().ServerReachable() {
			t.Fatal("ServerReachable = true without a result key")
		}
		assertListOnly(t, f)
	})
	t.Run("a missing executable is unreachable", func(t *testing.T) {
		env := platform.Env{
			"HOME":            t.TempDir(),
			"XDG_CONFIG_HOME": t.TempDir(),
			"PATH":            t.TempDir(),
		}
		if newHerdrCLI(env, nil).ServerReachable() {
			t.Fatal("ServerReachable = true with no herdr on PATH")
		}
	})
}

// assertListOnly fails when the fake saw any call other than exactly one
// `workspace list` — in particular a call whose first argument is `server`,
// which could start a server.
func assertListOnly(t *testing.T, f *herdrFixture) {
	t.Helper()
	calls := f.calls()
	if len(calls) != 1 {
		t.Fatalf("herdr calls = %d, want exactly 1 (workspace list)", len(calls))
	}
	for _, call := range calls {
		if len(call.Argv) > 0 && call.Argv[0] == "server" {
			t.Fatalf("ServerReachable ran a server command: %v", call.Argv)
		}
	}
	if !sameArgv(calls[0].Argv, []string{"workspace", "list"}) {
		t.Fatalf("herdr call argv = %v, want [workspace list]", calls[0].Argv)
	}
}

func TestHerdrWorkspacesList(t *testing.T) {
	t.Run("the rows are read from result.workspaces", func(t *testing.T) {
		stdout := `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[` +
			`{"active_tab_id":"w9:t1","agent_status":"done","focused":false,"label":"job-TASK-1","number":1,"pane_count":2,"tab_count":1,"workspace_id":"w9"},` +
			`{"label":"other","pane_count":1,"workspace_id":"w12"}]}}`
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: []string{"workspace", "list"}, Stdout: stdout}})
		rows, err := f.cli().List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		want := []herdrWorkspace{{ID: "w9", Label: "job-TASK-1", PaneCount: 2}, {ID: "w12", Label: "other", PaneCount: 1}}
		if len(rows) != len(want) {
			t.Fatalf("List = %+v, want %+v", rows, want)
		}
		for i := range want {
			if rows[i] != want[i] {
				t.Fatalf("List[%d] = %+v, want %+v", i, rows[i], want[i])
			}
		}
		assertListOnly(t, f)
	})
	t.Run("an empty list is no rows", func(t *testing.T) {
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: []string{"workspace", "list"}, Stdout: `{"result":{"workspaces":[]}}`}})
		rows, err := f.cli().List()
		if err != nil || len(rows) != 0 {
			t.Fatalf("List = %+v, %v; want no rows and no error", rows, err)
		}
	})
	t.Run("an unreadable list is exit 4", func(t *testing.T) {
		for name, stdout := range map[string]string{
			"not json":               "not json",
			"no result":              `{"id":"cli:x"}`,
			"result array":           `{"result":[]}`,
			"no workspaces":          `{"result":{}}`,
			"workspaces not array":   `{"result":{"workspaces":{}}}`,
			"row without id":         `{"result":{"workspaces":[{"label":"a","pane_count":1}]}}`,
			"row with empty id":      `{"result":{"workspaces":[{"workspace_id":"","label":"a","pane_count":1}]}}`,
			"row without label":      `{"result":{"workspaces":[{"workspace_id":"w1","pane_count":1}]}}`,
			"row without pane count": `{"result":{"workspaces":[{"workspace_id":"w1","label":"a"}]}}`,
			"negative pane count":    `{"result":{"workspaces":[{"workspace_id":"w1","label":"a","pane_count":-1}]}}`,
			"fractional pane count":  `{"result":{"workspaces":[{"workspace_id":"w1","label":"a","pane_count":1.5}]}}`,
			"string pane count":      `{"result":{"workspaces":[{"workspace_id":"w1","label":"a","pane_count":"1"}]}}`,
		} {
			t.Run(name, func(t *testing.T) {
				f := newHerdrFixture(t, []fakecli.Rule{{Argv: []string{"workspace", "list"}, Stdout: stdout}})
				rows, err := f.cli().List()
				wantExit(t, err, ExitHerdr, "job: herdr workspace list returned an unknown result")
				if rows != nil {
					t.Fatalf("List rows = %+v, want nil", rows)
				}
			})
		}
	})
	t.Run("a non-zero exit is exit 4", func(t *testing.T) {
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: []string{"workspace", "list"}, Code: 1, Stdout: `{"result":{"workspaces":[]}}`}})
		_, err := f.cli().List()
		wantExit(t, err, ExitHerdr, "job: herdr workspace list failed")
	})
}

func TestHerdrWorkspacesClose(t *testing.T) {
	t.Run("exact argv and success", func(t *testing.T) {
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: []string{"workspace", "close", "w9"}}})
		if err := f.cli().Close("w9"); err != nil {
			t.Fatalf("Close: %v", err)
		}
		calls := f.calls()
		if len(calls) != 1 || !sameArgv(calls[0].Argv, []string{"workspace", "close", "w9"}) {
			t.Fatalf("herdr calls = %v, want exactly one close of w9", calls)
		}
	})
	t.Run("a non-zero exit fails with code 4", func(t *testing.T) {
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: []string{"workspace", "close", "w9"}, Code: 1}})
		err := f.cli().Close("w9")
		wantExit(t, err, ExitHerdr, "job: herdr workspace close failed")
	})
	t.Run("an empty id refuses with code 2 without running anything", func(t *testing.T) {
		f := newHerdrFixture(t, nil)
		err := f.cli().Close("")
		wantExit(t, err, ExitUsage, "")
		if calls := f.calls(); len(calls) != 0 {
			t.Fatalf("herdr calls = %d, want 0", len(calls))
		}
	})
	t.Run("hostile ids refuse with code 2 without running anything", func(t *testing.T) {
		ids := []string{
			"../escape", "..", ".", "a/b", `a\b`, "/abs", `\rooted`,
			`C:\x`, "C:x", `\\server\share`, "-x", "--force",
			"a\x00b", "w\n2", " w2", "w2.",
		}
		// NUL is a reserved device name only on Windows, where
		// filepath.IsLocal refuses it; elsewhere it stays a valid name.
		if runtime.GOOS == "windows" {
			ids = append(ids, "NUL")
		}
		for _, id := range ids {
			t.Run(strconv.Quote(id), func(t *testing.T) {
				f := newHerdrFixture(t, nil)
				err := f.cli().Close(id)
				wantExit(t, err, ExitUsage, "job: invalid workspace id")
				if calls := f.calls(); len(calls) != 0 {
					t.Fatalf("herdr calls = %v, want none", calls)
				}
			})
		}
	})
	t.Run("a valid one-segment id runs workspace close", func(t *testing.T) {
		ids := []string{"w2A", "w12", "ws-1"}
		if runtime.GOOS != "windows" {
			ids = append(ids, "NUL")
		}
		for _, id := range ids {
			t.Run(strconv.Quote(id), func(t *testing.T) {
				want := []string{"workspace", "close", id}
				f := newHerdrFixture(t, []fakecli.Rule{{Argv: want}})
				if err := f.cli().Close(id); err != nil {
					t.Fatalf("Close(%q): %v", id, err)
				}
				calls := f.calls()
				if len(calls) != 1 || !sameArgv(calls[0].Argv, want) {
					t.Fatalf("herdr calls = %v, want exactly %v", calls, want)
				}
			})
		}
	})
}

func TestHerdrWorkspacesRun(t *testing.T) {
	argv := []string{"/opt/bin/herdr-soho", "job", "supervise", "--id", "TASK-1.a"}
	t.Run("exact argv and success", func(t *testing.T) {
		want := append([]string{"pane", "run", "w9:p1"}, argv...)
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: want}})
		if err := f.cli().Run("w9:p1", argv); err != nil {
			t.Fatalf("Run: %v", err)
		}
		calls := f.calls()
		if len(calls) != 1 || !sameArgv(calls[0].Argv, want) {
			t.Fatalf("herdr calls = %v, want exactly %v", calls, want)
		}
	})
	t.Run("unsafe arguments are refused with code 4 and no call", func(t *testing.T) {
		for name, tc := range map[string]struct {
			paneID string
			argv   []string
		}{
			"empty pane id":       {paneID: ""},
			"space in pane id":    {paneID: "w9 p1"},
			"semicolon in pane":   {paneID: "w9;p1"},
			"empty argv element":  {paneID: "w9:p1", argv: []string{"/bin/cmd", ""}},
			"space in argv":       {paneID: "w9:p1", argv: []string{"/Users/a b/herdr-soho"}},
			"semicolon in argv":   {paneID: "w9:p1", argv: []string{"x;y"}},
			"glob in argv":        {paneID: "w9:p1", argv: []string{"cmd", "*"}},
			"shell quote in argv": {paneID: "w9:p1", argv: []string{"cmd", "a'b"}},
		} {
			t.Run(name, func(t *testing.T) {
				f := newHerdrFixture(t, nil)
				err := f.cli().Run(tc.paneID, tc.argv)
				wantExit(t, err, ExitHerdr, "job: herdr pane run refused an unsafe argument")
				if calls := f.calls(); len(calls) != 0 {
					t.Fatalf("herdr calls = %d, want 0", len(calls))
				}
			})
		}
	})
	t.Run("a non-zero exit fails with code 4", func(t *testing.T) {
		want := append([]string{"pane", "run", "w9:p1"}, argv...)
		f := newHerdrFixture(t, []fakecli.Rule{{Argv: want, Code: 1}})
		err := f.cli().Run("w9:p1", argv)
		wantExit(t, err, ExitHerdr, "job: herdr pane run failed")
	})
}

// TestHerdrWorkspacesRunShortNames pins the 8.3 short-name rule: a tilde is
// accepted only as an 8.3 short-name (a letter or digit, then ~, then one
// or more digits), which Windows uses in executable paths, and every other
// tilde form stays refused with exit 4 and zero herdr calls.
func TestHerdrWorkspacesRunShortNames(t *testing.T) {
	t.Run("8.3 short-name arguments are accepted", func(t *testing.T) {
		for _, arg := range []string{
			`C:\Users\RUNNER~1\AppData\Local\Temp\go-build1\b001\cli.test.exe`,
			`C:\PROGRA~1\herdr\herdr-soho.exe`,
			`/tmp/ABCDEF~12/x`,
			`D:\a\b~3`,
		} {
			t.Run(arg, func(t *testing.T) {
				want := []string{"pane", "run", "w9:p1", arg}
				f := newHerdrFixture(t, []fakecli.Rule{{Argv: want}})
				if err := f.cli().Run("w9:p1", []string{arg}); err != nil {
					t.Fatalf("Run(%q): %v", arg, err)
				}
				calls := f.calls()
				if len(calls) != 1 || !sameArgv(calls[0].Argv, want) {
					t.Fatalf("herdr calls = %v, want exactly one %v", calls, want)
				}
			})
		}
	})
	// Tilde forms that are not 8.3 short names; refused as an argv element
	// and as the pane id.
	tildeArgs := []string{"~", `~/x`, `~user/bin`, "a~", "a~b", `/~1`, `\~1`, `x=~1`, `x:~1`, ".~1", "-~1"}
	// Unsafe arguments that are not tilde forms.
	otherArgs := []string{"a~1;b", "a~1 b", `%TEMP%`, `$HOME`, "a\x60b", "a|b", "a&b", "a>b", `a"b`, "a\nb"}
	for _, arg := range append(append([]string{}, tildeArgs...), otherArgs...) {
		t.Run("refused argument "+strconv.Quote(arg), func(t *testing.T) {
			f := newHerdrFixture(t, nil)
			err := f.cli().Run("w9:p1", []string{arg})
			wantExit(t, err, ExitHerdr, "job: herdr pane run refused an unsafe argument")
			if calls := f.calls(); len(calls) != 0 {
				t.Fatalf("herdr calls = %d, want 0", len(calls))
			}
		})
	}
	for _, paneID := range tildeArgs {
		t.Run("refused pane id "+strconv.Quote(paneID), func(t *testing.T) {
			f := newHerdrFixture(t, nil)
			err := f.cli().Run(paneID, nil)
			wantExit(t, err, ExitHerdr, "job: herdr pane run refused an unsafe argument")
			if calls := f.calls(); len(calls) != 0 {
				t.Fatalf("herdr calls = %d, want 0", len(calls))
			}
		})
	}
}

func TestHerdrWorkspacesDeadline(t *testing.T) {
	f := newHerdrFixture(t, []fakecli.Rule{{Argv: herdrTestCreateArgv(), Delay: 5000}})
	cli := f.cli()
	cli.Timeout = 200 * time.Millisecond
	start := time.Now()
	_, _, err := cli.Create(herdrTestCwd, herdrTestLabel, map[string]string{"A": "1", "B": "2"})
	elapsed := time.Since(start)
	wantExit(t, err, ExitHerdr, "job: herdr workspace create failed")
	if elapsed > 10*time.Second {
		t.Fatalf("Create took %s, want under 10s", elapsed)
	}
}

func TestFakeHerdrRecordsCalls(t *testing.T) {
	f := &fakeHerdr{CreateWorkspaceID: "w1", CreateRootPaneID: "w1:p1"}
	var workspaces herdrWorkspaces = f
	var panes herdrPanes = f
	workspaceID, rootPaneID, err := workspaces.Create("/work", "label", map[string]string{"K": "V"})
	if err != nil || workspaceID != "w1" || rootPaneID != "w1:p1" {
		t.Fatalf("Create = %q %q %v, want w1 w1:p1", workspaceID, rootPaneID, err)
	}
	if err := workspaces.Close("w1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if workspaces.ServerReachable() {
		t.Fatal("ServerReachable = true, want the configured false")
	}
	argv := []string{"cmd", "arg"}
	if err := panes.Run("w1:p1", argv); err != nil {
		t.Fatalf("Run: %v", err)
	}
	argv[0] = "mutated"
	if len(f.CreateCalls) != 1 || f.CreateCalls[0].Cwd != "/work" || f.CreateCalls[0].Label != "label" || f.CreateCalls[0].Env["K"] != "V" {
		t.Fatalf("create calls = %+v", f.CreateCalls)
	}
	if len(f.CloseIDs) != 1 || f.CloseIDs[0] != "w1" || f.ListCalls != 1 {
		t.Fatalf("close/list state = %v %d", f.CloseIDs, f.ListCalls)
	}
	if len(f.RunCalls) != 1 || f.RunCalls[0].PaneID != "w1:p1" || !sameArgv(f.RunCalls[0].Argv, []string{"cmd", "arg"}) {
		t.Fatalf("run calls = %+v (the fake must copy argv)", f.RunCalls)
	}
	if f.Reachable {
		t.Fatal("the fake must not mutate its configured results")
	}
}
