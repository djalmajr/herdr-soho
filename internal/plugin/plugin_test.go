package plugin_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/plugin"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestMain(m *testing.M) {
	if marker := os.Getenv("HERDR_SOHO_PLUGIN_CWD_MARKER"); marker != "" {
		if cwd, err := os.Getwd(); err == nil {
			_ = os.WriteFile(marker, []byte(cwd), 0o600)
		}
	}
	fakecli.RunTests(m)
}

func envWithFake(t *testing.T, dir string, values map[string]string) platform.Env {
	t.Helper()
	entries := fakecli.Env(os.Environ(), dir, fakecli.EnvOptions{IncludeBasePath: true})
	env := platform.Env{}
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	for key, value := range values {
		env[key] = value
	}
	return env
}

func installBridgeFakes(t *testing.T, paneJSON string, paneCode int, cliCode int, stdout, stderr string) (platform.Env, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	cwd := t.TempDir()
	if paneJSON == "__default__" {
		data, _ := json.Marshal(map[string]any{"result": map[string]any{"pane": map[string]any{"workspace_id": "ws-a", "cwd": cwd}}})
		paneJSON = string(data)
	}
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
		{Argv: []string{"pane", "get", "pane-a"}, Stdout: paneJSON, Code: paneCode},
		{Argv: plugin.PickerArguments(), Stdout: "opened\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cliBin, err := fakecli.InstallWithOptions(t, dir, "cli", []fakecli.Rule{{Argv: []string{"doctor"}, Stdout: stdout, Stderr: stderr, Code: cliCode}}, fakecli.InstallOptions{CaptureEnv: []string{"HERDR_WORKSPACE_ID", "HERDR_TAB_ID", "HERDR_PANE_ID", "HERDR_SOHO_NOWRITE", "PATH"}})
	if err != nil {
		t.Fatal(err)
	}
	env := envWithFake(t, dir, map[string]string{
		"HERDR_BIN_PATH":            herdr,
		"HERDR_PLUGIN_CONTEXT_JSON": `{"workspace_id":"ws-a","tab_id":"tab-a","focused_pane_id":"pane-a"}`,
		"HERDR_WORKSPACE_ID":        "shell-ws",
		"HERDR_TAB_ID":              "shell-tab",
		"HERDR_PANE_ID":             "shell-pane",
	})
	return env, cliBin, cwd, paneJSON
}

func TestBridgeReadOnlyAndFocusedTarget(t *testing.T) {
	// JS: "doctor: valid context invokes the CLI in the pane cwd with the context ids"
	// Mutation captured: removing NOWRITE or using the shell cwd changes the captured child invocation.
	env, cliBin, cwd, _ := installBridgeFakes(t, "__default__", 0, 0, "doctor-ok\n", "")
	cwdMarker := filepath.Join(filepath.Dir(cliBin), "child-cwd.txt")
	env["HERDR_SOHO_PLUGIN_CWD_MARKER"] = cwdMarker
	r, err := plugin.Bridge("doctor", env, platform.Current(), cliBin)
	if err != nil || r.Code != 0 || !strings.Contains(r.Out, "target workspace=ws-a pane=pane-a cwd="+cwd) || !strings.Contains(r.Out, "doctor-ok") {
		t.Fatalf("bridge: result=%+v err=%v", r, err)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(cliBin), "cli.json"))
	if err != nil || len(calls) != 1 {
		t.Fatalf("CLI calls=%#v err=%v", calls, err)
	}
	if calls[0].Env["HERDR_SOHO_NOWRITE"] != "1" || calls[0].Env["HERDR_WORKSPACE_ID"] != "ws-a" || calls[0].Env["HERDR_PANE_ID"] != "pane-a" || calls[0].Env["HERDR_TAB_ID"] != "tab-a" {
		t.Fatalf("child env=%#v", calls[0].Env)
	}
	if !strings.HasPrefix(calls[0].Env["PATH"], filepath.Dir(env.Get("HERDR_BIN_PATH"))+string(os.PathListSeparator)) {
		t.Fatalf("child PATH does not start with the Herdr directory: %q", calls[0].Env["PATH"])
	}
	childCwd, err := os.ReadFile(cwdMarker)
	resolvedCwd, _ := filepath.EvalSymlinks(cwd)
	if err != nil || string(childCwd) != resolvedCwd {
		t.Fatalf("child cwd=%q want %q err=%v", childCwd, cwd, err)
	}
	herdrCalls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(cliBin), "herdr.json"))
	if err != nil || len(herdrCalls) != 1 || !reflect.DeepEqual(herdrCalls[0].Argv, []string{"pane", "get", "pane-a"}) {
		t.Fatalf("Herdr calls=%#v err=%v", herdrCalls, err)
	}
}

func TestBridgeRunsHerdrAndSkillExecutablesOutsidePath(t *testing.T) {
	// Mutation captured: replacing either exact-path call with PATH resolution can select the decoy.
	env, cliBin, _, _ := installBridgeFakes(t, "__default__", 0, 0, "exact-skill\n", "")
	pathDir := t.TempDir()
	for _, name := range []string{"herdr", filepath.Base(cliBin)} {
		if _, err := fakecli.Install(t, pathDir, name, []fakecli.Rule{{AnyArgs: true, Stdout: "decoy\n"}}); err != nil {
			t.Fatal(err)
		}
	}
	env["PATH"] = pathDir
	result, err := plugin.Bridge("doctor", env, platform.Current(), cliBin)
	if err != nil || result.Code != 0 || !strings.Contains(result.Out, "exact-skill\n") || strings.Contains(result.Out, "decoy\n") {
		t.Fatalf("bridge exact path result=%+v err=%v", result, err)
	}
	herdrCalls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(env.Get("HERDR_BIN_PATH")), "herdr.json"))
	if err != nil || len(herdrCalls) != 1 || !reflect.DeepEqual(herdrCalls[0].Argv, []string{"pane", "get", "pane-a"}) {
		t.Fatalf("HERDR_BIN_PATH call log=%#v err=%v", herdrCalls, err)
	}
	cliCalls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(cliBin), "cli.json"))
	if err != nil || len(cliCalls) != 1 || !reflect.DeepEqual(cliCalls[0].Argv, []string{"doctor"}) {
		t.Fatalf("exact skill call log=%#v err=%v", cliCalls, err)
	}
	if !filepath.IsAbs(env.Get("HERDR_BIN_PATH")) || !filepath.IsAbs(cliBin) || strings.HasPrefix(env.Get("HERDR_BIN_PATH"), pathDir) || strings.HasPrefix(cliBin, pathDir) {
		t.Fatalf("fixtures are not separate from PATH: Herdr=%q CLI=%q PATH=%q", env.Get("HERDR_BIN_PATH"), cliBin, pathDir)
	}
}

func TestBridgeRejectsMalformedContextBeforeHerdr(t *testing.T) {
	// JS: "context malformed: fails without invoking the CLI"
	// Mutation captured: accepting invalid context would produce Herdr or CLI calls.
	env, cliBin, _, _ := installBridgeFakes(t, "__default__", 0, 0, "", "")
	env["HERDR_PLUGIN_CONTEXT_JSON"] = `{"workspace_id":"ws-a",`
	_, err := plugin.Bridge("doctor", env, platform.Current(), cliBin)
	bridgeErr, ok := err.(*plugin.BridgeError)
	if !ok || bridgeErr.Code != 2 || !strings.Contains(bridgeErr.Message, "malformed JSON") {
		t.Fatalf("error=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(cliBin), "herdr.calls.jsonl")); !os.IsNotExist(statErr) {
		t.Fatalf("Herdr invoked for invalid context: %v", statErr)
	}
}

func TestBridgeRejectsWorkspaceDivergence(t *testing.T) {
	// JS: "workspace divergence: the pane belongs to another workspace"
	// Mutation captured: dropping the workspace comparison invokes the CLI on the wrong project.
	cwd := t.TempDir()
	data, _ := json.Marshal(map[string]any{"result": map[string]any{"pane": map[string]any{"workspace_id": "ws-b", "cwd": cwd}}})
	env, cliBin, _, _ := installBridgeFakes(t, string(data), 0, 0, "", "")
	_, err := plugin.Bridge("doctor", env, platform.Current(), cliBin)
	bridgeErr, ok := err.(*plugin.BridgeError)
	if !ok || bridgeErr.Code != 2 || !strings.Contains(bridgeErr.Message, "workspace divergence") {
		t.Fatalf("error=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(cliBin), "cli.calls.jsonl")); !os.IsNotExist(statErr) {
		t.Fatalf("CLI invoked on divergence: %v", statErr)
	}
}

func TestBridgeHerdrFailureAndUnknownAction(t *testing.T) {
	// JS: "pane get non-zero exit: fails without invoking the CLI"
	// Mutation captured: treating a failed validation as success would pass the guard and invoke the CLI.
	env, cliBin, _, _ := installBridgeFakes(t, "", 9, 0, "", "pane not found")
	_, err := plugin.Bridge("doctor", env, platform.Current(), cliBin)
	bridgeErr, ok := err.(*plugin.BridgeError)
	if !ok || bridgeErr.Code != 4 || !strings.Contains(bridgeErr.Message, "pane get failed") {
		t.Fatalf("error=%v", err)
	}
	_, err = plugin.Bridge("dispatch", env, platform.Current(), cliBin)
	bridgeErr, ok = err.(*plugin.BridgeError)
	if !ok || bridgeErr.Code != 2 || !strings.Contains(bridgeErr.Message, "unknown subcommand") {
		t.Fatalf("unknown action error=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(cliBin), "cli.calls.jsonl")); !os.IsNotExist(statErr) {
		t.Fatalf("CLI invoked on pane validation failure: %v", statErr)
	}
}

func TestBridgePickUsesExpectedArguments(t *testing.T) {
	// JS: "pick: opens the picker pane with the exact arguments and does not run the CLI"
	// Mutation captured: changing a picker argument breaks the recorded Herdr argv contract.
	env, cliBin, _, _ := installBridgeFakes(t, "__default__", 0, 0, "", "")
	var out, errOut bytes.Buffer
	oldOut, oldErr := platform.Stdout, platform.Stderr
	platform.Stdout, platform.Stderr = &out, &errOut
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	code := plugin.PluginCommand([]string{"bridge", "pick"}, env, platform.Current(), cliBin, "")
	if code != 0 || !strings.Contains(out.String(), "target workspace=ws-a pane=pane-a") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(cliBin), "herdr.json"))
	if err != nil || len(calls) != 2 || !reflect.DeepEqual(calls[1].Argv, plugin.PickerArguments()) {
		t.Fatalf("Herdr calls=%#v err=%v", calls, err)
	}
}

func TestClipboardLinuxOrderFallbackAndOSC52(t *testing.T) {
	// JS: "clipboard: linux tries wl-copy, then xclip, then xsel — a failing tool yields the next"
	// Mutation captured: changing candidate order or not advancing after failure changes the selected path.
	want := [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	if got := plugin.ClipboardCandidates("linux"); !reflect.DeepEqual(got, want) {
		t.Fatalf("linux order=%#v", got)
	}
	dir := t.TempDir()
	_, _ = fakecli.Install(t, dir, "wl-copy", []fakecli.Rule{{AnyArgs: true, Code: 1}})
	_, err := fakecli.InstallWithOptions(t, dir, "xclip", []fakecli.Rule{{Argv: []string{"-selection", "clipboard"}}}, fakecli.InstallOptions{CaptureStdin: true})
	if err != nil {
		t.Fatal(err)
	}
	env := envWithFake(t, dir, map[string]string{})
	env["PATH"] = dir
	oldOut := platform.Stdout
	var out bytes.Buffer
	platform.Stdout = &out
	t.Cleanup(func() { platform.Stdout = oldOut })
	if runtime.GOOS == "windows" {
		noTool := platform.Env{"PATH": filepath.Join(dir, "missing")}
		fallback := plugin.CopyText("olá referência", noTool, "linux")
		if fallback.Path != "osc52" || out.String() != "\x1b]52;c;b2zDoSByZWZlcsOqbmNpYQ==\x07" {
			t.Fatalf("fallback result=%+v output=%q", fallback, out.String())
		}
		t.Skip("Linux clipboard subprocess fixtures require a Linux host")
	}
	r := plugin.CopyText("linux text", env, "linux")
	if r.Path != "xclip" {
		t.Fatalf("copy result=%+v", r)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(dir, "xclip.json"))
	if err != nil || len(calls) != 1 || calls[0].Stdin != "linux text" {
		t.Fatalf("xclip calls=%#v err=%v", calls, err)
	}
	noTool := platform.Env{"PATH": filepath.Join(dir, "missing")}
	r = plugin.CopyText("olá referência", noTool, "linux")
	if r.Path != "osc52" || out.String() != "\x1b]52;c;b2zDoSByZWZlcsOqbmNpYQ==\x07" {
		t.Fatalf("fallback result=%+v output=%q", r, out.String())
	}
	if got := plugin.OSC52Sequence("olá referência"); got != "\x1b]52;c;b2zDoSByZWZlcsOqbmNpYQ==\x07" {
		t.Fatalf("OSC52=%q", got)
	}
	if plugin.ClipboardCandidates("darwin")[0][0] != "pbcopy" || plugin.ClipboardCandidates("win32")[0][0] != "powershell" {
		t.Fatalf("platform candidates darwin=%v windows=%v", plugin.ClipboardCandidates("darwin"), plugin.ClipboardCandidates("win32"))
	}
}

func TestClipboardWindowsUsesPowerShellAndExactArguments(t *testing.T) {
	// JS: "clipboard: windows uses a powershell that reads the stdin — exact argv, hostile text"
	// Mutation captured: using clip.exe or changing the UTF-8 ReadToEnd command changes the fake process argv.
	dir := t.TempDir()
	powershell, err := fakecli.InstallWithOptions(t, dir, "powershell", []fakecli.Rule{{AnyArgs: true}}, fakecli.InstallOptions{CaptureStdin: true})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Rename(powershell, filepath.Join(dir, "powershell.exe")); err != nil {
			t.Fatal(err)
		}
	}
	env := envWithFake(t, dir, map[string]string{})
	r := plugin.CopyText("price is $5 and \"quoted\" and 'single' and não\n", env, "win32")
	if r.Path != "powershell" {
		t.Fatalf("clipboard path=%q", r.Path)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(dir, "powershell.json"))
	wantArgs := []string{"-NoProfile", "-Command", "[Console]::InputEncoding=[Text.Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())"}
	if err != nil || len(calls) != 1 || !reflect.DeepEqual(calls[0].Argv, wantArgs) || calls[0].Stdin != "price is $5 and \"quoted\" and 'single' and não\n" {
		t.Fatalf("PowerShell calls=%#v err=%v", calls, err)
	}
}

func TestClipboardDarwinUsesInputAndFallbackBytes(t *testing.T) {
	// JS: "clipboard: darwin uses pbcopy from a controlled PATH, text on stdin"
	// Mutation captured: dropping stdin or changing the OSC 52 terminator changes captured data/bytes.
	dir := t.TempDir()
	_, err := fakecli.InstallWithOptions(t, dir, "pbcopy", []fakecli.Rule{{AnyArgs: true}}, fakecli.InstallOptions{CaptureStdin: true})
	if err != nil {
		t.Fatal(err)
	}
	env := envWithFake(t, dir, map[string]string{})
	env["PATH"] = dir
	oldOut := platform.Stdout
	var out bytes.Buffer
	platform.Stdout = &out
	t.Cleanup(func() { platform.Stdout = oldOut })
	seq := plugin.OSC52Sequence("olá")
	if seq != "\x1b]52;c;b2zDoQ==\x07" || len([]byte(seq)) != 16 {
		t.Fatalf("OSC52 sequence=%q bytes=%d", seq, len([]byte(seq)))
	}
	if runtime.GOOS == "windows" {
		t.Skip("Darwin clipboard subprocess fixture requires a Darwin host")
	}
	r := plugin.CopyText("hello clip", env, "darwin")
	if r.Path != "pbcopy" {
		t.Fatalf("clipboard path=%q", r.Path)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(dir, "pbcopy.json"))
	if err != nil || len(calls) != 1 || calls[0].Stdin != "hello clip" {
		t.Fatalf("clipboard calls=%#v err=%v", calls, err)
	}
}

func TestBridgeMatchesJSReferenceWithFakeHerdr(t *testing.T) {
	// JS: "read-only bridge: context validation, workspace guard, Herdr failure, and action dispatch"
	// Mutation captured: any divergent precondition or action code changes the shared black-box result.
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is unavailable for Go/JS differential")
	}
	bridgeFile, err := filepath.Abs(filepath.Join("..", "..", "plugin", "bridge.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	bridgeImport, _ := json.Marshal(filepath.ToSlash(bridgeFile))
	driver := filepath.Join(t.TempDir(), "bridge-diff.mjs")
	script := fmt.Sprintf(`import { pathToFileURL } from 'node:url';
const { run } = await import(pathToFileURL(%s).href);
const result = (() => { try { return run(process.env.BRIDGE_ACTION, { env: process.env, cliScript: process.env.CLI_SCRIPT }); }
		catch (e) { return { code: e.code, out: "", err: "herdr-soho plugin: " + e.message + "\n" }; } })();
process.stdout.write(JSON.stringify(result));
`, bridgeImport)
	if err = os.WriteFile(driver, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	type diffCase struct {
		name      string
		action    string
		context   string
		paneWs    string
		paneCode  int
		paneError string
		cliCode   int
		cliOut    string
		cliErr    string
	}
	for _, tc := range []diffCase{
		{name: "valid context", action: "doctor", context: `{"workspace_id":"ws-a","tab_id":"tab-a","focused_pane_id":"pane-a"}`, paneWs: "ws-a", cliOut: "doctor-result\n"},
		{name: "malformed context", action: "doctor", context: `{"workspace_id":"ws-a",`, paneWs: "ws-a"},
		{name: "workspace divergence", action: "doctor", context: `{"workspace_id":"ws-a","focused_pane_id":"pane-a"}`, paneWs: "ws-b"},
		{name: "pane get failure", action: "doctor", context: `{"workspace_id":"ws-a","focused_pane_id":"pane-a"}`, paneWs: "ws-a", paneCode: 7, paneError: "pane unavailable"},
		{name: "unknown action", action: "dispatch", context: `{"workspace_id":"ws-a","focused_pane_id":"pane-a"}`, paneWs: "ws-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cwd := t.TempDir()
			paneReply, _ := json.Marshal(map[string]any{"result": map[string]any{"pane": map[string]any{"workspace_id": tc.paneWs, "cwd": cwd}}})
			herdr, installErr := fakecli.InstallWithOptions(t, dir, "herdr", []fakecli.Rule{{Argv: []string{"pane", "get", "pane-a"}, Stdout: string(paneReply), Stderr: tc.paneError, Code: tc.paneCode}}, fakecli.InstallOptions{CaptureEnv: []string{"HERDR_BIN_PATH", "HERDR_PLUGIN_CONTEXT_JSON"}})
			if installErr != nil {
				t.Fatal(installErr)
			}
			cliBin, installErr := fakecli.InstallWithOptions(t, dir, "cli", []fakecli.Rule{{Argv: []string{tc.action}, Stdout: tc.cliOut, Stderr: tc.cliErr, Code: tc.cliCode}}, fakecli.InstallOptions{CaptureEnv: []string{"HERDR_SOHO_NOWRITE", "HERDR_WORKSPACE_ID", "HERDR_PANE_ID", "HERDR_TAB_ID", "PATH"}})
			if installErr != nil {
				t.Fatal(installErr)
			}
			cliScript := filepath.Join(dir, "cli-reference.mjs")
			marker := filepath.Join(dir, "js-cli.json")
			jsCLI := fmt.Sprintf(`import fs from 'node:fs';
fs.writeFileSync(%q, JSON.stringify({cwd:process.cwd(), argv:process.argv.slice(2), env:{HERDR_SOHO_NOWRITE:process.env.HERDR_SOHO_NOWRITE,HERDR_WORKSPACE_ID:process.env.HERDR_WORKSPACE_ID,HERDR_PANE_ID:process.env.HERDR_PANE_ID,HERDR_TAB_ID:process.env.HERDR_TAB_ID,PATH_FIRST:process.env.PATH.split(process.platform === 'win32' ? ';' : ':')[0]}}));
process.stdout.write(%q); process.stderr.write(%q); process.exit(%d);
`, marker, tc.cliOut, tc.cliErr, tc.cliCode)
			if writeErr := os.WriteFile(cliScript, []byte(jsCLI), 0o600); writeErr != nil {
				t.Fatal(writeErr)
			}
			env := envWithFake(t, dir, map[string]string{"HERDR_BIN_PATH": herdr, "HERDR_PLUGIN_CONTEXT_JSON": tc.context, "CLI_SCRIPT": cliScript, "BRIDGE_ACTION": tc.action, "HERDR_WORKSPACE_ID": "shell-ws", "HERDR_TAB_ID": "shell-tab", "HERDR_PANE_ID": "shell-pane"})
			command := exec.Command(node, driver)
			command.Env = env.List()
			jsOut, jsErr := command.CombinedOutput()
			if jsErr != nil {
				t.Fatalf("JS reference failed: %v output=%s", jsErr, jsOut)
			}
			var jsResult plugin.Result
			var jsPayload struct {
				Code int    `json:"code"`
				Out  string `json:"out"`
				Err  string `json:"err"`
			}
			if err := json.Unmarshal(jsOut, &jsPayload); err != nil {
				t.Fatalf("JS result %q: %v", jsOut, err)
			}
			goResult, goErr := plugin.Bridge(tc.action, env, platform.Current(), cliBin)
			if goErr != nil {
				bridgeErr, ok := goErr.(*plugin.BridgeError)
				if !ok {
					t.Fatal(goErr)
				}
				jsResult = plugin.Result{Code: jsPayload.Code, Out: jsPayload.Out, Err: jsPayload.Err}
				goResult = plugin.Result{Code: bridgeErr.Code, Err: "herdr-soho plugin: " + bridgeErr.Message + "\n"}
			} else {
				jsResult = plugin.Result{Code: jsPayload.Code, Out: jsPayload.Out, Err: jsPayload.Err}
			}
			if tc.name == "valid context" || tc.name == "workspace divergence" || tc.name == "pane get failure" {
				herdrCalls, readErr := fakecli.ReadCallsForConfig(filepath.Join(dir, "herdr.json"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				calls := make([][]string, len(herdrCalls))
				for i, call := range herdrCalls {
					calls[i] = call.Argv
				}
				t.Logf("stdout=%q stderr=%q code=%d herdr_calls=%#v", jsResult.Out, jsResult.Err, jsResult.Code, calls)
			} else {
				t.Logf("stdout=%q stderr=%q code=%d herdr_calls=none", jsResult.Out, jsResult.Err, jsResult.Code)
			}
			if !reflect.DeepEqual(goResult, jsResult) {
				t.Fatalf("Go=%+v JS=%+v", goResult, jsResult)
			}
			if tc.name == "valid context" {
				jsChild, readErr := os.ReadFile(marker)
				if readErr != nil {
					t.Fatal(readErr)
				}
				var jsEnv struct {
					Cwd string            `json:"cwd"`
					Env map[string]string `json:"env"`
				}
				resolvedCwd, _ := filepath.EvalSymlinks(cwd)
				if err := json.Unmarshal(jsChild, &jsEnv); err != nil || jsEnv.Cwd != resolvedCwd || jsEnv.Env["HERDR_SOHO_NOWRITE"] != "1" || jsEnv.Env["PATH_FIRST"] != filepath.Dir(herdr) {
					t.Fatalf("JS child cwd/env=%s err=%v", jsChild, err)
				}
				t.Logf("JS CLI child cwd=%q env=%v", jsEnv.Cwd, jsEnv.Env)
				goCalls, readErr := fakecli.ReadCallsForConfig(filepath.Join(dir, "cli.json"))
				if readErr != nil || len(goCalls) != 1 || goCalls[0].Env["HERDR_SOHO_NOWRITE"] != "1" || !strings.HasPrefix(goCalls[0].Env["PATH"], filepath.Dir(herdr)+string(os.PathListSeparator)) {
					t.Fatalf("Go child env/calls=%#v err=%v", goCalls, readErr)
				}
				goEnv := map[string]string{
					"HERDR_PANE_ID":      goCalls[0].Env["HERDR_PANE_ID"],
					"HERDR_SOHO_NOWRITE": goCalls[0].Env["HERDR_SOHO_NOWRITE"],
					"HERDR_TAB_ID":       goCalls[0].Env["HERDR_TAB_ID"],
					"HERDR_WORKSPACE_ID": goCalls[0].Env["HERDR_WORKSPACE_ID"],
					"PATH_FIRST":         strings.Split(goCalls[0].Env["PATH"], string(os.PathListSeparator))[0],
				}
				t.Logf("Go CLI child env=%v", goEnv)
			}
		})
	}
}
