package plugin

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

const teamRosterFixture = "NAME  KIND  STATUS  TASK\n" +
	"worker-1  codex  working  implement\n" +
	"worker-2  grok  idle  scout\n" +
	"\n" +
	"# other live agents\n" +
	"other-1  claude  idle  elsewhere\n"

const teamExplainFixture = "A equipe está implementando o módulo de relatórios.\n" +
	"worker-1 implementa e worker-2 faz a revisão.\n"

// teamPanelFixture installs the fake herdr (pane get) and the fake herdr-soho
// CLI (explain, roster, doctor, friction, gc, collect, release) with env
// capture, plus the panel's environment (the focused context and shell
// HERDR_* decoys that the target ids must win over).
func teamPanelFixture(t *testing.T) (platform.Env, string, string) {
	t.Helper()
	dir := t.TempDir()
	cwd := t.TempDir()
	paneJSON, _ := json.Marshal(map[string]any{"result": map[string]any{"pane": map[string]any{"workspace_id": "ws-a", "cwd": cwd}}})
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
		{Argv: []string{"pane", "get", "pane-a"}, Stdout: string(paneJSON)},
	})
	if err != nil {
		t.Fatal(err)
	}
	cli, err := fakecli.InstallWithOptions(t, dir, "cli", []fakecli.Rule{
		{Argv: []string{"explain"}, Stdout: teamExplainFixture},
		{Argv: []string{"roster"}, Stdout: teamRosterFixture},
		{Argv: []string{"doctor"}, Stdout: "doctor-ok\n"},
		{Argv: []string{"friction", "--summary"}, Stdout: "friction-ok\n"},
		{Argv: []string{"gc"}, Stdout: "gc dry-run ok\npressure 12%\n"},
		{Argv: []string{"gc", "--yes"}, Stdout: "gc done\n"},
		{Argv: []string{"collect", "worker-1", "--lines", "60"}, Stdout: "report one line\n"},
		{Argv: []string{"collect", "worker-2", "--lines", "60"}, Stdout: "report two line\n"},
		{Argv: []string{"release", "worker-1", "--close"}, Stdout: "released one\n"},
		{Argv: []string{"release", "worker-2", "--close"}, Stdout: "released two\n"},
	}, fakecli.InstallOptions{CaptureEnv: []string{"HERDR_WORKSPACE_ID", "HERDR_PANE_ID", "HERDR_TAB_ID", "HERDR_SOHO_NOWRITE", "PATH"}})
	if err != nil {
		t.Fatal(err)
	}
	env := teamEnv(t, dir, map[string]string{
		"HERDR_BIN_PATH":            herdr,
		"HERDR_PLUGIN_CONTEXT_JSON": `{"workspace_id":"ws-a","tab_id":"tab-a","focused_pane_id":"pane-a"}`,
		"HERDR_WORKSPACE_ID":        "shell-ws",
		"HERDR_TAB_ID":              "shell-tab",
		"HERDR_PANE_ID":             "shell-pane",
	})
	return env, cli, cwd
}

func teamEnv(t *testing.T, dir string, values map[string]string) platform.Env {
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

func teamStartWithPipes(t *testing.T, env platform.Env, cliPath string, args []string) (*os.File, *os.File, <-chan int) {
	t.Helper()
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "team-panel-")
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = input, output
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldIn, oldOut
		_ = input.Close()
		_ = writer.Close()
		_ = output.Close()
	})
	finished := make(chan int, 1)
	go func() { finished <- RunTeam(env, platform.Current(), cliPath, args) }()
	return writer, output, finished
}

func teamReadOutput(output *os.File) string {
	_, _ = output.Seek(0, io.SeekStart)
	data, _ := io.ReadAll(output)
	return string(data)
}

func teamWaitForOutput(t *testing.T, output *os.File, want string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		data := teamReadOutput(output)
		if strings.Contains(data, want) {
			return data
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %q in the panel output:\n%s", want, data)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func teamWaitExit(t *testing.T, finished <-chan int, want int) {
	t.Helper()
	select {
	case code := <-finished:
		if code != want {
			t.Fatalf("panel exit=%d want %d", code, want)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("timeout waiting for the panel to exit")
	}
}

// teamLastFrame is the screen as of the last redraw (\x1b[H … \x1b[J).
func teamLastFrame(output string) string {
	idx := strings.LastIndex(output, "\x1b[H")
	if idx < 0 {
		return output
	}
	rest := output[idx+len("\x1b[H"):]
	if end := strings.Index(rest, "\x1b[J"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func teamLastFrameFirstLine(output string) string {
	frame := teamLastFrame(output)
	line, _, _ := strings.Cut(frame, "\x1b[K")
	return line
}

func teamCalls(t *testing.T, cliPath string) []fakecli.Call {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(cliPath), "cli.json"))
	if err != nil {
		t.Fatalf("CLI call log: %v", err)
	}
	return calls
}

func teamCallIndex(calls []fakecli.Call, want []string) int {
	for i, call := range calls {
		if reflect.DeepEqual(call.Argv, want) {
			return i
		}
	}
	return -1
}

// teamWaitForLastFrame polls until the last frame satisfies cond (or 20 s).
func teamWaitForLastFrame(t *testing.T, output *os.File, cond func(frame string) bool, why string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if cond(teamLastFrame(teamReadOutput(output))) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s:\n%s", why, teamLastFrame(teamReadOutput(output)))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestTeamPanelShowsFirstLineAndTeamView: a valid context renders the
// first line, the explain text and the team's workers - the `# other live
// agents` section stays out of the view.
func TestTeamPanelShowsFirstLineAndTeamView(t *testing.T) {
	env, cli, cwd := teamPanelFixture(t)
	writer, output, finished := teamStartWithPipes(t, env, cli, nil)
	teamWaitForOutput(t, output, "worker-1")
	teamWaitForOutput(t, output, "módulo de relatórios")
	firstLine := teamLastFrameFirstLine(teamReadOutput(output))
	// Non-TTY panels render at 80 columns; the long test cwd makes the
	// first line the displaySlice of the full line.
	if want := displaySlice("herdr-soho · ws-a · "+cwd, 80); firstLine != want {
		t.Fatalf("first line=%q want %q", firstLine, want)
	}
	data := teamReadOutput(output)
	if strings.Contains(data, "other-1") {
		t.Fatalf("the # other live agents section leaked into the team view:\n%s", data)
	}
	_, _ = writer.Write([]byte("\x1b"))
	_ = writer.Close()
	teamWaitExit(t, finished, 0)
}

// TestTeamPanelRejectsWorkspaceDivergence: the focused pane belongs to
// another workspace; the panel shows the cause on one line, closes with
// Esc, and never invokes the CLI.
func TestTeamPanelRejectsWorkspaceDivergence(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	paneJSON, _ := json.Marshal(map[string]any{"result": map[string]any{"pane": map[string]any{"workspace_id": "ws-b", "cwd": cwd}}})
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{{Argv: []string{"pane", "get", "pane-a"}, Stdout: string(paneJSON)}})
	if err != nil {
		t.Fatal(err)
	}
	cli, err := fakecli.Install(t, dir, "cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	env := teamEnv(t, dir, map[string]string{
		"HERDR_BIN_PATH":            herdr,
		"HERDR_PLUGIN_CONTEXT_JSON": `{"workspace_id":"ws-a","tab_id":"tab-a","focused_pane_id":"pane-a"}`,
	})
	writer, output, finished := teamStartWithPipes(t, env, cli, nil)
	// The cause is one line, hard-cut to the panel width: assert the prefix
	// and that the last frame is a single content line.
	teamWaitForOutput(t, output, "workspace divergence: the context points to 'ws-a'")
	frame := teamLastFrame(teamReadOutput(output))
	if !strings.Contains(frame, "workspace divergence") {
		t.Fatalf("the cause is not on the screen:\n%s", frame)
	}
	if lines := strings.Split(strings.TrimSuffix(frame, "\x1b[J"), "\n"); len(lines) > 2 {
		t.Fatalf("the cause must be a single line:\n%s", frame)
	}
	_, _ = writer.Write([]byte("\x1b"))
	_ = writer.Close()
	teamWaitExit(t, finished, 0)
	if _, statErr := os.Stat(filepath.Join(dir, "cli.calls.jsonl")); !os.IsNotExist(statErr) {
		t.Fatalf("CLI invoked despite the rejected target: %v", statErr)
	}
}

// TestTeamPanelRejectsMissingCwd: the pane's cwd does not exist; the panel
// shows the cause and never invokes the CLI.
func TestTeamPanelRejectsMissingCwd(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing-dir")
	paneJSON, _ := json.Marshal(map[string]any{"result": map[string]any{"pane": map[string]any{"workspace_id": "ws-a", "cwd": missing}}})
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{{Argv: []string{"pane", "get", "pane-a"}, Stdout: string(paneJSON)}})
	if err != nil {
		t.Fatal(err)
	}
	cli, err := fakecli.Install(t, dir, "cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	env := teamEnv(t, dir, map[string]string{
		"HERDR_BIN_PATH":            herdr,
		"HERDR_PLUGIN_CONTEXT_JSON": `{"workspace_id":"ws-a","tab_id":"tab-a","focused_pane_id":"pane-a"}`,
	})
	writer, output, finished := teamStartWithPipes(t, env, cli, nil)
	teamWaitForOutput(t, output, "pane cwd does not exist: "+missing[:40])
	_, _ = writer.Write([]byte("\x1b"))
	_ = writer.Close()
	teamWaitExit(t, finished, 0)
	if _, statErr := os.Stat(filepath.Join(dir, "cli.calls.jsonl")); !os.IsNotExist(statErr) {
		t.Fatalf("CLI invoked despite the missing cwd: %v", statErr)
	}
}

// TestTeamPanelReadsCarryNowriteCwdAndTargetIDs: every read call carries
// HERDR_SOHO_NOWRITE=1, runs in the target's cwd, and uses the target's
// HERDR_* ids (not the invoking shell's).
func TestTeamPanelReadsCarryNowriteCwdAndTargetIDs(t *testing.T) {
	env, cli, cwd := teamPanelFixture(t)
	cwdMarker := filepath.Join(filepath.Dir(cli), "child-cwd.txt")
	env["HERDR_SOHO_PLUGIN_CWD_MARKER"] = cwdMarker
	writer, output, finished := teamStartWithPipes(t, env, cli, nil)
	teamWaitForOutput(t, output, "worker-1")
	_, _ = writer.Write([]byte("2"))
	teamWaitForOutput(t, output, "doctor-ok")
	_, _ = writer.Write([]byte("3"))
	teamWaitForOutput(t, output, "pressure 12%")
	_, _ = writer.Write([]byte("4"))
	teamWaitForOutput(t, output, "friction-ok")
	_, _ = writer.Write([]byte("\x1b"))
	_ = writer.Close()
	teamWaitExit(t, finished, 0)
	calls := teamCalls(t, cli)
	wantArgv := [][]string{{"explain"}, {"roster"}, {"doctor"}, {"gc"}, {"friction", "--summary"}}
	if len(calls) != len(wantArgv) {
		t.Fatalf("CLI calls=%#v want %d reads", calls, len(wantArgv))
	}
	for i, want := range wantArgv {
		if !reflect.DeepEqual(calls[i].Argv, want) {
			t.Fatalf("call %d argv=%#v want %#v", i, calls[i].Argv, want)
		}
		e := calls[i].Env
		if e["HERDR_SOHO_NOWRITE"] != "1" || e["HERDR_WORKSPACE_ID"] != "ws-a" || e["HERDR_PANE_ID"] != "pane-a" || e["HERDR_TAB_ID"] != "tab-a" {
			t.Fatalf("read %d (%v) env=%#v (NOWRITE, target ids)", i, want, e)
		}
	}
	childCwd, err := os.ReadFile(cwdMarker)
	resolved, _ := filepath.EvalSymlinks(cwd)
	if err != nil || string(childCwd) != resolved {
		t.Fatalf("child cwd=%q want %q err=%v", childCwd, resolved, err)
	}
}

// TestTeamWorkersParseOnlyTheFirstTable: only the first roster table is
// used - the `# other live agents` section (after the blank line) stays out.
func TestTeamWorkersParseOnlyTheFirstTable(t *testing.T) {
	lines := strings.Split(strings.TrimSuffix(teamRosterFixture, "\n"), "\n")
	workers := teamWorkers(lines)
	want := []string{"worker-1  codex  working  implement", "worker-2  grok  idle  scout"}
	if !reflect.DeepEqual(workers, want) {
		t.Fatalf("workers=%#v want %#v", workers, want)
	}
	if got := teamAgent(workers[1]); got != "worker-2" {
		t.Fatalf("agent=%q want worker-2", got)
	}
	if got := teamWorkers(nil); got != nil {
		t.Fatalf("workers from empty roster=%#v want nil", got)
	}
}

// TestTeamPanelEnterCollectsSelectedWorker: Enter opens the collect
// subview for the selected worker (`collect <agent> --lines 60`, read).
func TestTeamPanelEnterCollectsSelectedWorker(t *testing.T) {
	env, cli, _ := teamPanelFixture(t)
	writer, output, finished := teamStartWithPipes(t, env, cli, nil)
	teamWaitForOutput(t, output, "worker-1")
	_, _ = writer.Write([]byte("\x1b[B")) // down: select worker-2
	teamWaitForOutput(t, output, "> worker-2")
	_, _ = writer.Write([]byte("\r")) // Enter
	teamWaitForOutput(t, output, "report two line")
	calls := teamCalls(t, cli)
	i := teamCallIndex(calls, []string{"collect", "worker-2", "--lines", "60"})
	if i < 0 {
		t.Fatalf("no collect call for the selected worker: %#v", calls)
	}
	if got := calls[i].Env["HERDR_SOHO_NOWRITE"]; got != "1" {
		t.Fatalf("collect env NOWRITE=%q want 1 (a read)", calls[i].Env)
	}
	_, _ = writer.Write([]byte("\x1b")) // Esc: back to the team view
	_, _ = writer.Write([]byte("\x1b"))
	_, _ = writer.Write([]byte("\x1b"))
	_ = writer.Close()
	teamWaitExit(t, finished, 0)
}

// TestTeamPanelXCancelNeverCallsRelease: x opens the confirmation; n or
// Esc cancel it, and no release call is made.
func TestTeamPanelXCancelNeverCallsRelease(t *testing.T) {
	env, cli, cwd := teamPanelFixture(t)
	writer, output, finished := teamStartWithPipes(t, env, cli, nil)
	teamWaitForOutput(t, output, "worker-1")
	_, _ = writer.Write([]byte("x"))
	teamWaitForOutput(t, output, "release --close worker-1 in ws-a ("+cwd[:40])
	_, _ = writer.Write([]byte("n")) // cancel with n
	teamWaitForLastFrame(t, output, func(frame string) bool { return !strings.Contains(frame, "y executa") }, "the confirmation to disappear after n")
	_, _ = writer.Write([]byte("x"))
	teamWaitForOutput(t, output, "release --close worker-1 in ws-a ("+cwd[:40])
	_, _ = writer.Write([]byte("\x1b")) // Esc pending
	_, _ = writer.Write([]byte("q"))    // resolved by the next key: Esc cancels
	teamWaitForLastFrame(t, output, func(frame string) bool { return !strings.Contains(frame, "y executa") }, "the confirmation to disappear after Esc")
	_, _ = writer.Write([]byte("q"))
	_ = writer.Close()
	teamWaitExit(t, finished, 0)
	for _, call := range teamCalls(t, cli) {
		if call.Argv[0] == "release" {
			t.Fatalf("release invoked without confirmation: %#v", call)
		}
	}
}

// TestTeamPanelXConfirmsReleaseWithoutNowriteAndReloads: y runs
// `release <agent> --close` WITHOUT HERDR_SOHO_NOWRITE, the view shows the
// output and reloads (explain and roster run again after the release).
func TestTeamPanelXConfirmsReleaseWithoutNowriteAndReloads(t *testing.T) {
	env, cli, cwd := teamPanelFixture(t)
	writer, output, finished := teamStartWithPipes(t, env, cli, nil)
	teamWaitForOutput(t, output, "worker-1")
	_, _ = writer.Write([]byte("\x1b[B")) // down: select worker-2
	teamWaitForOutput(t, output, "> worker-2")
	_, _ = writer.Write([]byte("x"))
	teamWaitForOutput(t, output, "release --close worker-2 in ws-a ("+cwd[:40])
	_, _ = writer.Write([]byte("y"))
	teamWaitForOutput(t, output, "released two") // the release output block
	calls := teamCalls(t, cli)
	release := teamCallIndex(calls, []string{"release", "worker-2", "--close"})
	if release < 0 {
		t.Fatalf("no release call: %#v", calls)
	}
	if got := calls[release].Env["HERDR_SOHO_NOWRITE"]; got != "" {
		t.Fatalf("release env NOWRITE=%q want absent (a write)", calls[release].Env)
	}
	for _, want := range [][]string{{"explain"}, {"roster"}} {
		if i := teamCallIndexAfter(calls, release, want); i < 0 {
			t.Fatalf("no %v after the release (the view must reload): %#v", want, calls)
		}
	}
	_, _ = writer.Write([]byte("q"))
	_ = writer.Close()
	teamWaitExit(t, finished, 0)
}

func teamCallIndexAfter(calls []fakecli.Call, after int, want []string) int {
	for i := after + 1; i < len(calls); i++ {
		if reflect.DeepEqual(calls[i].Argv, want) {
			return i
		}
	}
	return -1
}

// TestTeamPanelGConfirmsGCYes: g opens the confirmation; n cancels; y runs
// `gc --yes` WITHOUT HERDR_SOHO_NOWRITE, the view shows the output and
// reloads the dry-run.
func TestTeamPanelGConfirmsGCYes(t *testing.T) {
	env, cli, cwd := teamPanelFixture(t)
	writer, output, finished := teamStartWithPipes(t, env, cli, nil)
	teamWaitForOutput(t, output, "worker-1")
	_, _ = writer.Write([]byte("3"))
	teamWaitForOutput(t, output, "pressure 12%")
	_, _ = writer.Write([]byte("g"))
	teamWaitForOutput(t, output, "gc --yes in "+cwd[:40])
	_, _ = writer.Write([]byte("n"))
	teamWaitForLastFrame(t, output, func(frame string) bool { return !strings.Contains(frame, "y executa") }, "the confirmation to disappear after n")
	_, _ = writer.Write([]byte("g"))
	teamWaitForOutput(t, output, "gc --yes in "+cwd[:40])
	_, _ = writer.Write([]byte("y"))
	teamWaitForOutput(t, output, "gc done")
	calls := teamCalls(t, cli)
	gcYes := teamCallIndex(calls, []string{"gc", "--yes"})
	if gcYes < 0 {
		t.Fatalf("no gc --yes call: %#v", calls)
	}
	if got := calls[gcYes].Env["HERDR_SOHO_NOWRITE"]; got != "" {
		t.Fatalf("gc --yes env NOWRITE=%q want absent (a write)", calls[gcYes].Env)
	}
	if i := teamCallIndexAfter(calls, gcYes, []string{"gc"}); i < 0 {
		t.Fatalf("no dry-run gc after gc --yes (the view must reload): %#v", calls)
	}
	_, _ = writer.Write([]byte("q"))
	_ = writer.Close()
	teamWaitExit(t, finished, 0)
}

// TestTeamPanelCLIFailureShowsExitAndStderr: a failed call shows its exit
// code and stderr in the view, and the panel keeps working.
func TestTeamPanelCLIFailureShowsExitAndStderr(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	paneJSON, _ := json.Marshal(map[string]any{"result": map[string]any{"pane": map[string]any{"workspace_id": "ws-a", "cwd": cwd}}})
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{{Argv: []string{"pane", "get", "pane-a"}, Stdout: string(paneJSON)}})
	if err != nil {
		t.Fatal(err)
	}
	cli, err := fakecli.InstallWithOptions(t, dir, "cli", []fakecli.Rule{
		{Argv: []string{"explain"}, Stdout: teamExplainFixture},
		{Argv: []string{"roster"}, Stdout: teamRosterFixture},
		{Argv: []string{"doctor"}, Stderr: "boom\n", Code: 3},
	}, fakecli.InstallOptions{CaptureEnv: []string{"HERDR_SOHO_NOWRITE"}})
	if err != nil {
		t.Fatal(err)
	}
	env := teamEnv(t, dir, map[string]string{
		"HERDR_BIN_PATH":            herdr,
		"HERDR_PLUGIN_CONTEXT_JSON": `{"workspace_id":"ws-a","tab_id":"tab-a","focused_pane_id":"pane-a"}`,
	})
	writer, output, finished := teamStartWithPipes(t, env, cli, nil)
	teamWaitForOutput(t, output, "worker-1") // the panel survived the load
	_, _ = writer.Write([]byte("2"))
	teamWaitForOutput(t, output, "doctor failed (exit 3)")
	teamWaitForOutput(t, output, "boom")
	_, _ = writer.Write([]byte("q"))
	_ = writer.Close()
	teamWaitExit(t, finished, 0)
}

// TestTeamPanelInitialViewDoctor: --view doctor starts on the doctor view
// (the first CLI call is doctor) and marks it in the view bar.
func TestTeamPanelInitialViewDoctor(t *testing.T) {
	env, cli, _ := teamPanelFixture(t)
	writer, output, finished := teamStartWithPipes(t, env, cli, []string{"--view", "doctor"})
	teamWaitForOutput(t, output, "doctor-ok")
	teamWaitForOutput(t, output, ">2 doctor")
	calls := teamCalls(t, cli)
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Argv, []string{"doctor"}) {
		t.Fatalf("initial view calls=%#v want only the doctor read", calls)
	}
	_, _ = writer.Write([]byte("q"))
	_ = writer.Close()
	teamWaitExit(t, finished, 0)
}

// TestTeamInitialViewParsesFlags: no flag means the equipe view, --view
// accepts only "doctor", and anything else is rejected.
func TestTeamInitialViewParsesFlags(t *testing.T) {
	if view, err := teamInitialView(nil); view != teamViewTeam || err != nil {
		t.Fatalf("default view=%d err=%v", view, err)
	}
	if view, err := teamInitialView([]string{"--view", "doctor"}); view != teamViewDoctor || err != nil {
		t.Fatalf("doctor view=%d err=%v", view, err)
	}
	for _, args := range [][]string{{"--view", "team"}, {"--bogus"}, {"--view"}} {
		if _, err := teamInitialView(args); err == nil {
			t.Fatalf("args %v accepted", args)
		}
	}
}

// TestTeamRenderNeverPassesWidth: with wide and mixed content, every
// rendered line (any view, subview or confirmation) fits the width in
// display columns.
func TestTeamRenderNeverPassesWidth(t *testing.T) {
	state := NewTeamState(teamViewTeam)
	state.target = teamTarget{WorkspaceID: "ws-a", PaneID: "p", TabID: "t", Cwd: "/a/very/long/cwd/path/for/the/team/panel/target"}
	state.viewLoaded = [4]bool{true, true, true, true}
	state.explainLines = []string{"A equipe " + strings.Repeat("界", 40) + " está trabalhando " + strings.Repeat("x", 80)}
	state.workers = []string{strings.Repeat("worker-name ", 6) + "codex working " + strings.Repeat("\U0001F600", 30)}
	state.selected = 1
	state.doctorLines = []string{strings.Repeat("doctor line ", 10)}
	state.gcLines = []string{"pressure 12%", strings.Repeat("gc ", 40)}
	state.frictionLines = []string{strings.Repeat("friction ", 40)}
	state.releaseAgent = "worker-1"
	state.releaseOut = []string{strings.Repeat("released ", 20)}
	state.gcYesOut = []string{strings.Repeat("gc done ", 20)}
	check := func(subview string, collectAgent string) {
		state.subview = subview
		state.confirmAgent = collectAgent
		state.collectAgent = collectAgent
		state.collectLoaded = true
		state.collectLines = []string{strings.Repeat("report ", 40) + strings.Repeat("界", 20)}
		for _, width := range []int{20, 30, 80} {
			render := RenderTeam(state, width)
			for i, line := range strings.Split(strings.TrimSuffix(render, "\n"), "\n") {
				if w := displayWidth(line); w > width {
					t.Fatalf("view %d subview %q width %d: line %d %q has display width %d", state.view, subview, width, i, line, w)
				}
			}
		}
	}
	for view := teamViewTeam; view <= teamViewFriction; view++ {
		state.view = view
		check("", "worker-1")
	}
	check("collect", "worker-1")
	check("confirm-release", "worker-1")
	check("confirm-gc", "")
}
