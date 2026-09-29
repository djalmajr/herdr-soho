package layout

// JS (fora): "test tools: linkTool creates a platform launcher and canSymlink caches its result" — exercita helpers de infraestrutura exclusivos do harness Node, sem contrato correspondente no produto Go.

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func parityTabFixture(t *testing.T, withTab bool) (platform.Env, *core.Config, string, string, string) {
	t.Helper()
	root := t.TempDir()
	repo, home, conf := filepath.Join(root, "repo"), filepath.Join(root, "home"), filepath.Join(root, "conf")
	state, tmp, bin, tabs := filepath.Join(root, "state"), filepath.Join(root, "tmp"), filepath.Join(root, "bin"), filepath.Join(root, "tabs")
	for _, dir := range []string{repo, home, conf, state, tmp, bin, tabs, filepath.Join(state, "ws")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$HA_LOG"
T="$HA_TABS"
P="$HA_PANES"
case "$1 $2" in
  "tab get")
    if [ -f "$T/$3" ]; then
      printf '{"result":{"tab":{"tab_id":"%s","label":"%s"}}}\n' "$3" "$(cat "$T/$3")"
    else
      printf '{"error":"tab_not_found"}\n'
      exit 1
    fi ;;
  "tab rename")
    shift 2; id="$1"; shift; printf '%s' "$*" > "$T/$id"; printf '{"result":{}}\n' ;;
  "pane list") cat "$P" ;;
  *) printf '{"error":"unexpected: %s"}\n' "$*" >&2; exit 1 ;;
esac
`
	herdr := filepath.Join(bin, "herdr")
	if err := os.WriteFile(herdr, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if withTab {
		for id, label := range map[string]string{"t1": "onda\n", "t2": "herd\n", "t3": ""} {
			if err := os.WriteFile(filepath.Join(tabs, id), []byte(label), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(state, "ws", "herd-tab"), []byte("t1\nt2\nt3\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		rows := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\n" +
			"a1\tw1\tclaude\tscouter\tanthropic\t1\t/tmp\tnow\n" + "a2\tw2\tclaude\treviewer\tanthropic\t1\t/tmp\tnow\n" + "a3\tw3\tclaude\tdesigner\tanthropic\t1\t/tmp\tnow\n"
		if err := os.WriteFile(filepath.Join(state, "ws", "agents.tsv"), []byte(rows), 0o600); err != nil {
			t.Fatal(err)
		}
		panes := `{"result":{"panes":[{"pane_id":"w1","tab_id":"t1"},{"pane_id":"w2","tab_id":"t1"},{"pane_id":"w3","tab_id":"t2"}]}}`
		if err := os.WriteFile(filepath.Join(root, "panes.json"), []byte(panes), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !withTab {
		// Match makeTabFixture('no-tab'): no tabs, roster or pane-list file.
	}
	pathValue := bin + string(os.PathListSeparator) + os.Getenv("PATH")
	env := platform.Env{"HOME": home, "USERPROFILE": home, "XDG_CONFIG_HOME": conf, "HERDR_SOHO_DIR": state, "HERDR_WORKSPACE_ID": "ws", "HERDR_TAB_ID": "caller", "HERDR_ENV": "1", "HERDR_SOHO_SKILL_DIR": "../../skills/herdr-soho", "TMPDIR": tmp, "PATH": pathValue, "HA_TABS": tabs, "HA_LOG": filepath.Join(root, "herdr.log"), "HA_PANES": filepath.Join(root, "panes.json")}
	ctx := labelContext(t, env)
	return env, ctx, repo, filepath.Join(state, "ws", "herd-tab"), tabs
}

func runTabLabelCaptured(args []string, ctx *core.Config, env platform.Env, cwd string) (code int, out, errOut string) {
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var stdout, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &stdout, &stderr
	defer func() { platform.Stdout, platform.Stderr = oldOut, oldErr }()
	defer func() {
		if value := recover(); value != nil {
			if e, ok := value.(*platform.ExitError); ok {
				code, errOut = e.Code, e.Msg
				return
			}
			panic(value)
		}
	}()
	code = CmdTabLabel(args, ctx, env, cwd)
	return code, stdout.String(), stderr.String()
}

func TestParityTabLabelGoldenScenarios(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake herdr is the JS TAB_FAKE sh script, which a native Windows process cannot run")
	}
	t.Run("parity: tab-label (list, rename, --auto, unknown tab)", func(t *testing.T) { // JS: "parity: tab-label (list, rename, --auto, unknown tab)"
		env, ctx, cwd, stateFile, tabs := parityTabFixture(t, true)
		steps := []struct {
			args []string
			out  string
		}{
			{nil, "TAB        LABEL              MODE\nt1         onda               manual\nt2         herd               auto\nt3         -                  auto\n"},
			{[]string{"nova", "aba"}, "{\n  \"tab\": \"t3\",\n  \"label\": \"nova aba\",\n  \"mode\": \"manual\"\n}\n"},
			{[]string{"--tab", "t3", "--auto"}, "{\n  \"tab\": \"t3\",\n  \"label\": \"herd\",\n  \"mode\": \"auto\"\n}\n"},
		}
		for i, step := range steps {
			code, out, errOut := runTabLabelCaptured(step.args, ctx, env, cwd)
			if code != 0 || out != step.out || errOut != "" {
				t.Fatalf("step %d code=%d out=%q err=%q; want %q", i, code, out, errOut, step.out)
			}
		}
		code, out, errOut := runTabLabelCaptured([]string{"--tab", "nope", "x"}, ctx, env, cwd)
		if code != 3 || out != "" || errOut != "tab-label: nope is not a herd tab of this workspace (see: tab-label)" {
			t.Fatalf("unknown tab code=%d out=%q err=%q", code, out, errOut)
		}
		wantState := "t1\tonda\tmanual\nt2\tdes\tauto\nt3\therd\tauto\n"
		got, err := os.ReadFile(stateFile)
		if err != nil || string(got) != wantState {
			t.Fatalf("herd-tab=%q err=%v, want %q", got, err, wantState)
		}
		for file, want := range map[string]string{"t1": "onda\n", "t2": "des", "t3": "herd"} {
			got, err := os.ReadFile(filepath.Join(tabs, file))
			if err != nil || string(got) != want {
				t.Errorf("tab %s=%q err=%v want %q", file, got, err, want)
			}
		}
	})
	t.Run("parity: tab-label with no herd tab at all", func(t *testing.T) { // JS: "parity: tab-label with no herd tab at all"
		env, ctx, cwd, _, _ := parityTabFixture(t, false)
		code, out, errOut := runTabLabelCaptured(nil, ctx, env, cwd)
		if code != 0 || out != "TAB        LABEL              MODE\n(no herd tab yet)\n" || errOut != "" {
			t.Fatalf("list code=%d out=%q err=%q", code, out, errOut)
		}
		code, out, errOut = runTabLabelCaptured([]string{"x"}, ctx, env, cwd)
		if code != 3 || out != "" || errOut != "tab-label: no herd tab yet (workers overflow into one when the caller's tab is full)" {
			t.Fatalf("no-tab code=%d out=%q err=%q", code, out, errOut)
		}
	})
}

func labelContext(t *testing.T, env platform.Env) *core.Config {
	t.Helper()
	env["HERDR_SOHO_SKILL_DIR"] = "../../skills/herdr-soho"
	ctx := core.LoadConfig(env, ".")
	return &ctx
}

func TestComposeHerdLabel(t *testing.T) {
	t.Run(`composeHerdLabel: {roles}, {n}, {i}, {orch}`, func(t *testing.T) { // JS: "composeHerdLabel: {roles}, {n}, {i}, {orch}"
		got := ComposeHerdLabel("{roles}:{n}:{i}:{orch}", []string{"implementer", "reviewer", "implementer"}, 2, "orchestrator")
		if got != "impl+rev:3:2:orchestrator" {
			t.Fatalf("ComposeHerdLabel()=%q", got)
		}
	})
	// Mutation captured: joining roles with + must not prepend a separator when the first roster role is empty.
	t.Run("JS: an empty first role does not prefix the next role", func(t *testing.T) {
		if got := ComposeHerdLabel("{roles}", []string{"", "implementer"}, 1, ""); got != "impl" {
			t.Fatalf("ComposeHerdLabel()=%q, want impl", got)
		}
		if got := ComposeHerdLabel("{roles}", []string{"implementer", "reviewer"}, 1, ""); got != "impl+rev" {
			t.Fatalf("neighbor label=%q, want impl+rev", got)
		}
	})
}

func TestRoleAbbreviations(t *testing.T) {
	t.Run(`role abbreviations`, func(t *testing.T) { // JS: "role abbreviations"
		// Mutation captured: removing known role abbreviations changes the persisted herd tab label.
		for _, tc := range []struct{ role, want string }{
			{"implementer", "impl"}, {"security-reviewer", "sec"},
			{"sub-orchestrator", "sub"}, {"solid-ui", "solid-ui"},
		} {
			if got := RoleAbbrev(tc.role); got != tc.want {
				t.Errorf("RoleAbbrev(%q)=%q, want %q", tc.role, got, tc.want)
			}
		}
	})
}

func TestHerdTabFileWhitespaceMatchesBash(t *testing.T) {
	t.Run(`herd-tab file: repeated, leading and trailing tabs read like bash`, func(t *testing.T) { // JS: "herd-tab file: repeated, leading and trailing tabs read like bash"
		// Mutation captured: parsing only one tab separator changes normalized tab metadata.
		env, ctx, cwd, dir := layoutFake(t, []fakecli.Rule{{Argv: []string{"tab", "get", "tab-1"}, Stdout: `{"result":{"tab":{"label":"label"}}}`}})
		file := filepath.Join(dir, "herd-tab")
		if err := os.WriteFile(file, []byte("\t\ttab-1\t\tlabel\t\tauto\t\t\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := HerdTabEntries(ctx, env, cwd)
		stored, err := os.ReadFile(file)
		if err != nil || !reflect.DeepEqual(got, []TabEntry{{Tab: "tab-1", Label: "label", Mode: "auto"}}) || string(stored) != "tab-1\tlabel\tauto\n" {
			t.Fatalf("entries=%#v file=%q err=%v", got, stored, err)
		}
	})
}

func TestHerdAutoLabel(t *testing.T) {
	// JS: "herdAutoLabel: cut to 16, strip dangling separators, \" 2\"/\" 3\" suffixes"
	t.Run(`herdAutoLabel: cut to 16, strip dangling separators, " 2"/" 3" suffixes`, func(t *testing.T) { // JS: "herdAutoLabel: cut to 16, strip dangling separators, \" 2\"/\" 3\" suffixes"
		env := platform.Env{"HERDR_SOHO_HERD_LABEL_MAX": "16"}
		ctx := labelContext(t, env)
		if got := HerdAutoLabel("impl", []string{"impl"}, ctx, env); got != "impl 2" {
			t.Fatalf("repeat label=%q", got)
		}
		if got := HerdAutoLabel("impl+rev", nil, ctx, env); got != "impl+rev" {
			t.Fatalf("base label=%q", got)
		}
		env["HERDR_SOHO_HERD_LABEL_MAX"] = "6"
		if got := HerdAutoLabel("impl+rev", nil, ctx, env); got != "impl+r" {
			t.Fatalf("cut label=%q", got)
		}
		// Mutation captured: RE2's ASCII-only \s drops the NBSP that JavaScript trims.
		env["HERDR_SOHO_HERD_LABEL_MAX"] = "5"
		if got := HerdAutoLabel("impl\u00a0rev", nil, ctx, env); got != "impl" {
			t.Fatalf("NBSP cut label=%q", got)
		}
	})
}

func TestHerdTabSplitUsesRootWhenPaneIDIsMissing(t *testing.T) {
	// Mutation captured: formatting a missing pane_id as <nil> attempts a pane split instead of returning the tab root.
	rules := []fakecli.Rule{
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[{"tab_id":"th"}]}}`},
		{Argv: []string{"tab", "get", "th"}, Stdout: `{"result":{"tab":{"label":"workers"},"root_pane":{"pane_id":"rootp"}}}`},
	}
	env, ctx, cwd, _ := layoutFake(t, rules)
	got := HerdTabSplit(ctx, "th", cwd, env, cwd)
	if !got.Ok || got.Pane != "rootp" {
		t.Fatalf("HerdTabSplit()=%+v, want rootp", got)
	}
}

func TestHerdTabSplitUsesRootWhenLastPaneIDIsMissing(t *testing.T) {
	// Mutation captured: skipping a final pane without pane_id keeps an earlier anchor and splits the wrong pane.
	rules := []fakecli.Rule{
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[{"pane_id":"p1","tab_id":"th"},{"tab_id":"th"}]}}`},
		{Argv: []string{"tab", "get", "th"}, Stdout: `{"result":{"tab":{"label":"workers"},"root_pane":{"pane_id":"rootp"}}}`},
	}
	env, ctx, cwd, _ := layoutFake(t, rules)
	got := HerdTabSplit(ctx, "th", cwd, env, cwd)
	if !got.Ok || got.Pane != "rootp" {
		t.Fatalf("HerdTabSplit()=%+v, want rootp", got)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(env["HERDR_SOHO_FAKECLI_CONFIG"], "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !reflect.DeepEqual(calls[1].Argv, rules[1].Argv) {
		t.Fatalf("calls=%#v, want pane list followed by tab get", calls)
	}
}

func TestTabLabelAutoPrintsStoredDash(t *testing.T) {
	// Mutation captured: converting the persisted empty-label sentinel to an empty JSON string changes the command output.
	rules := []fakecli.Rule{
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[]}}`},
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
		{Argv: []string{"tab", "rename", "t1", ""}},
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":""}}}`},
	}
	env, ctx, cwd, dir := layoutFake(t, rules)
	env["HERDR_SOHO_HERD_LABEL_MAX"] = "0"
	if err := os.WriteFile(filepath.Join(dir, "herd-tab"), []byte("t1\t-\tauto\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := platform.Stdout
	var out bytes.Buffer
	platform.Stdout = &out
	defer func() { platform.Stdout = old }()
	if code := CmdTabLabel([]string{"--tab", "t1", "--auto"}, ctx, env, cwd); code != 0 {
		t.Fatalf("exit code=%d", code)
	}
	if !strings.Contains(out.String(), `"label": "-"`) {
		t.Fatalf("tab-label output=%q, want persisted dash", out.String())
	}
}

func TestManualTabLabelsArePreserved(t *testing.T) {
	// JS: "herdTabsRelabel: auto from roles, manual kept, repeats suffixed, empty → herd"
	// Mutation captured: recomputing a manual label overwrites the operator's chosen tab name.
	rules := []fakecli.Rule{
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[]}}`},
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"release"}}}`},
	}
	env, ctx, cwd, dir := layoutFake(t, rules)
	file := filepath.Join(dir, "herd-tab")
	if err := os.WriteFile(file, []byte("t1\trelease\tmanual\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	HerdTabsRelabel(ctx, env, cwd)
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "t1\trelease\tmanual\n" {
		t.Fatalf("herd-tab=%q", got)
	}
}

func TestHerdTabEntriesPrunesDeadTabs(t *testing.T) {
	// Mutation captured: retaining an entry after tab get fails leaves a dead tab in the observable herd-tab file.
	rules := []fakecli.Rule{
		{Argv: []string{"tab", "get", "dead"}, Code: 1},
		{Argv: []string{"tab", "get", "live"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
	}
	env, ctx, cwd, dir := layoutFake(t, rules)
	file := filepath.Join(dir, "herd-tab")
	if err := os.WriteFile(file, []byte("dead\told\tauto\nlive\therd\tauto\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := HerdTabEntries(ctx, env, cwd)
	stored, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []TabEntry{{"live", "herd", "auto"}}) || string(stored) != "live\therd\tauto\n" {
		t.Fatalf("entries=%#v file=%q", got, stored)
	}
}

func TestRosterPanesDropsGoneAndKeepsUnavailable(t *testing.T) {
	t.Run(`rosterPanesInTab: a gone pane drops, a stuck one stays (test-status.sh)`, func(t *testing.T) { // JS: "rosterPanesInTab: a gone pane drops, a stuck one stays (test-status.sh)"
		// Mutation captured: treating unqueryable agents as gone silently removes a live pane from regrid.
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "gone"}, Stderr: `{"error":{"code":"agent_not_found","message":"gone"}}`, Code: 1},
			{Argv: []string{"agent", "get", "stuck"}, Stderr: "permission denied", Code: 1},
		}
		env, ctx, cwd, _ := layoutFake(t, rules)
		roster := filepath.Join(core.StateDirPath(ctx, env, cwd), "agents.tsv")
		if err := os.WriteFile(roster, []byte("gone\tp1\timplementer\nstuck\tp2\treviewer\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		live := []any{
			jsonjs.O("pane_id", "p1", "tab_id", "t1"),
			jsonjs.O("pane_id", "p2", "tab_id", "t1"),
		}
		if got := rosterPanes(ctx, live, "t1", env, cwd); !reflect.DeepEqual(got, []string{"p2"}) {
			t.Fatalf("rosterPanes()=%v, want [p2]", got)
		}
	})
}

func TestRegridKeepsUnavailablePanesAndDropsGoneOnes(t *testing.T) {
	t.Run(`cmdRegrid: the test-status.sh kept-panes case (unqueryable kept, gone dropped)`, func(t *testing.T) { // JS: "cmdRegrid: the test-status.sh kept-panes case (unqueryable kept, gone dropped)"
		// Mutation captured: treating an unqueryable agent as gone loses its live pane while retaining a confirmed-dead pane is stale.
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get", "gone"}, Stderr: `{"error":{"code":"agent_not_found","message":"gone"}}`, Code: 1},
			{Argv: []string{"agent", "get", "stuck"}, Stderr: "permission denied", Code: 1},
		}
		env, ctx, cwd, dir := layoutFake(t, rules)
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("gone\tp3\timplementer\nstuck\tp2\treviewer\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		live := []any{jsonjs.O("pane_id", "p3", "tab_id", "t0"), jsonjs.O("pane_id", "p2", "tab_id", "t0")}
		if got := rosterPanes(ctx, live, "t0", env, cwd); !reflect.DeepEqual(got, []string{"p2"}) {
			t.Fatalf("kept panes=%v, want [p2]", got)
		}
	})
}

func TestHerdTabFileMigratesLegacyOneColumnEntries(t *testing.T) {
	t.Run(`herd-tab file: one-column migration, dead tabs pruned, 3-column rewrite`, func(t *testing.T) { // JS: "herd-tab file: one-column migration, dead tabs pruned, 3-column rewrite"
		// Mutation captured: skipping legacy normalization leaves one-column state without its label mode.
		env, ctx, cwd, dir := layoutFake(t, []fakecli.Rule{
			{Argv: []string{"tab", "get", "legacy"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"tab", "get", "dead"}, Code: 1},
		})
		file := filepath.Join(dir, "herd-tab")
		if err := os.WriteFile(file, []byte("legacy\ndead\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := HerdTabEntries(ctx, env, cwd)
		stored, err := os.ReadFile(file)
		if err != nil || !reflect.DeepEqual(got, []TabEntry{{Tab: "legacy", Label: "herd", Mode: "auto"}}) || string(stored) != "legacy\therd\tauto\n" {
			t.Fatalf("entries=%#v file=%q err=%v", got, stored, err)
		}
	})
}

func TestHerdTabsRelabelFullContract(t *testing.T) {
	t.Run(`herdTabsRelabel: auto from roles, manual kept, repeats suffixed, empty → herd`, func(t *testing.T) { // JS: "herdTabsRelabel: auto from roles, manual kept, repeats suffixed, empty → herd"
		// Mutation captured: changing role-derived labels, duplicate suffixes, or manual preservation changes the normalized state.
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[{"pane_id":"p1","tab_id":"t1"},{"pane_id":"p2","tab_id":"t2"}]}}`},
			{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"old"}}}`},
			{Argv: []string{"tab", "get", "t2"}, Stdout: `{"result":{"tab":{"label":"custom"}}}`},
			{Argv: []string{"tab", "get", "t3"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"tab", "get", "t4"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"tab", "rename", "t4", "herd 2"}},
			{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"old"}}}`},
			{Argv: []string{"tab", "rename", "t1", "impl"}},
			{Argv: []string{"tab", "get", "t3"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"tab", "get", "t4"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
		}
		env, ctx, cwd, dir := layoutFake(t, rules)
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("one\tp1\tcursor\timplementer\ntwo\tp2\tcursor\timplementer\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "herd-tab")
		if err := os.WriteFile(file, []byte("t1\told\tauto\nt2\tcustom\tmanual\nt3\t-\tauto\nt4\t-\tauto\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		HerdTabsRelabel(ctx, env, cwd)
		stored, err := os.ReadFile(file)
		if err != nil || string(stored) != "t1\timpl\tauto\nt2\tcustom\tmanual\nt3\therd\tauto\nt4\therd 2\tauto\n" {
			t.Fatalf("herd-tab=%q err=%v", stored, err)
		}
	})
}

func TestHerdTabPaneJSContract(t *testing.T) {
	t.Run(`herdTabPane: label with room splits it; label full → "<label> ·2"; new manual; auto path`, func(t *testing.T) { // JS: "herdTabPane: label with room splits it; label full → \"<label> ·2\"; new manual; auto path"
		// Mutation captured: choosing a full labeled tab or losing its manual pin sends a worker to the wrong tab.
		t.Run("existing label with room splits that tab", func(t *testing.T) {
			paneList := `{"result":{"panes":[{"pane_id":"w4","tab_id":"t2"}]}}`
			rules := []fakecli.Rule{
				{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: paneList},
				{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"onda 3"}}}`},
				{Argv: []string{"tab", "get", "t2"}, Stdout: `{"result":{"tab":{"label":"onda 2"}}}`},
				{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
				{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: paneList},
				{Argv: []string{"pane", "layout", "--pane", "w4"}, Stdout: `{"result":{"layout":{"panes":[{"pane_id":"w4","rect":{"width":100,"height":200}}]}}`},
				{Argv: []string{"pane", "split", "w4", "--direction", "down", "--cwd", "/tmp", "--no-focus"}, Stdout: `{"result":{"pane":{"pane_id":"split-w4"}}}`},
			}
			env, ctx, cwd, dir := layoutFake(t, rules)
			env["HERDR_SOHO_SPLIT_MAX_PANES"] = "2"
			if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("worker\tw4\tcursor\tdesigner\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "herd-tab"), []byte("t1\tonda 3\tmanual\nt2\tonda 2\tmanual\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := HerdTabPane(ctx, "/tmp", "onda 2", "reviewer", env, cwd); !got.Ok || got.Pane != "split-w4" {
				t.Fatalf("HerdTabPane()=%+v", got)
			}
			stored, err := os.ReadFile(filepath.Join(dir, "herd-tab"))
			if err != nil || !strings.Contains(string(stored), "t2\tonda 2\tmanual") {
				t.Fatalf("herd-tab=%q err=%v", stored, err)
			}
		})
		t.Run("full label gets the next manual label", func(t *testing.T) {
			list := `{"result":{"panes":[{"pane_id":"w1","tab_id":"t1"},{"pane_id":"w2","tab_id":"t1"}]}}`
			root := t.TempDir()
			cwd := root
			rules := []fakecli.Rule{
				{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: list},
				{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"onda 2"}}}`},
				{Argv: []string{"agent", "get", "one"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
				{Argv: []string{"agent", "get", "two"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
				{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"onda 2"}}}`},
				{Argv: []string{"agent", "get", "one"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
				{Argv: []string{"agent", "get", "two"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
				{Argv: []string{"tab", "create", "--workspace", "ws", "--cwd", cwd, "--label", "onda 2 ·2", "--no-focus"}, Stdout: `{"result":{"tab":{"tab_id":"t2"},"root_pane":{"pane_id":"root-t2"}}}`},
			}
			env, ctx, _, dir := layoutFake(t, rules)
			env["HERDR_SOHO_SPLIT_MAX_PANES"] = "2"
			if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("one\tw1\tcursor\timplementer\ntwo\tw2\tcursor\timplementer\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "herd-tab"), []byte("t1\tonda 2\tmanual\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := HerdTabPane(ctx, cwd, "onda 2", "reviewer", env, cwd); !got.Ok || got.Pane != "root-t2" {
				t.Fatalf("HerdTabPane()=%+v", got)
			}
			stored, err := os.ReadFile(filepath.Join(dir, "herd-tab"))
			if err != nil || !strings.Contains(string(stored), "t2\tonda 2 ·2\tmanual") {
				t.Fatalf("herd-tab=%q err=%v", stored, err)
			}
		})
		t.Run("automatic path creates a role-labeled tab when all entries are full", func(t *testing.T) {
			// JS: "herd tabs: every tab full → a new auto tab named after the role"
			root := t.TempDir()
			rules := []fakecli.Rule{
				{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[{"pane_id":"p1","tab_id":"t1"},{"pane_id":"p2","tab_id":"t1"}]}}`},
				{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
				{Argv: []string{"agent", "get", "one"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
				{Argv: []string{"agent", "get", "two"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
				{Argv: []string{"tab", "create", "--workspace", "ws", "--cwd", root, "--label", "impl", "--no-focus"}, Stdout: `{"result":{"tab":{"tab_id":"t2"},"root_pane":{"pane_id":"root-t2"}}}`},
			}
			env, ctx, cwd, dir := layoutFake(t, rules)
			env["HERDR_SOHO_SPLIT_MAX_PANES"] = "2"
			if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("one\tp1\tcursor\timplementer\ntwo\tp2\tcursor\timplementer\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "herd-tab"), []byte("t1\therd\tauto\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := HerdTabPane(ctx, root, "", "implementer", env, cwd); !got.Ok || got.Pane != "root-t2" {
				t.Fatalf("HerdTabPane()=%+v", got)
			}
			stored, err := os.ReadFile(filepath.Join(dir, "herd-tab"))
			if err != nil || string(stored) != "t1\therd\tauto\nt2\timpl\tauto\n" {
				t.Fatalf("herd-tab=%q err=%v", stored, err)
			}
		})
	})
}

func TestHerdTabsChooseSecondTabAfterMigration(t *testing.T) {
	t.Run(`herd tabs: second tab with room splits it; dead tab pruned, old format migrated`, func(t *testing.T) { // JS: "herd tabs: second tab with room splits it; dead tab pruned, old format migrated"
		// Mutation captured: stopping at a full first tab or retaining a dead legacy entry chooses the wrong placement or leaves stale state.
		panes := `{"result":{"panes":[{"pane_id":"w1","tab_id":"t1"},{"pane_id":"w2","tab_id":"t1"},{"pane_id":"w3","tab_id":"t2"}]}}`
		rules := []fakecli.Rule{
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: panes},
			{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"tab", "get", "t2"}, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
			{Argv: []string{"tab", "get", "dead"}, Code: 1},
			{Argv: []string{"agent", "get", "one"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "two"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"agent", "get", "three"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: panes},
			{Argv: []string{"pane", "layout", "--pane", "w3"}, Stdout: `{"result":{"layout":{"panes":[{"pane_id":"w3","rect":{"width":100,"height":100}}]}}`},
			{Argv: []string{"pane", "split", "w3", "--direction", "down", "--cwd", "/tmp", "--no-focus"}, Stdout: `{"result":{"pane":{"pane_id":"split-w3"}}}`},
		}
		env, ctx, cwd, dir := layoutFake(t, rules)
		env["HERDR_SOHO_SPLIT_MAX_PANES"] = "2"
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("one\tw1\tcursor\timplementer\ntwo\tw2\tcursor\timplementer\nthree\tw3\tcursor\treviewer\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "herd-tab")
		if err := os.WriteFile(file, []byte("t1\nt2\ndead\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var splitPane string
		var splitOK bool
		func() {
			defer func() {
				if value := recover(); value != nil {
					calls, _ := fakecli.ReadCallsForConfig(filepath.Join(env["HERDR_SOHO_FAKECLI_CONFIG"], "herdr.json"))
					t.Fatalf("HerdTabPane panic=%v calls=%#v", value, calls)
				}
			}()
			split := HerdTabPane(ctx, "/tmp", "", "reviewer", env, cwd)
			splitOK, splitPane = split.Ok, split.Pane
		}()
		if !splitOK || splitPane != "split-w3" {
			t.Fatalf("HerdTabPane()=%v,%q", splitOK, splitPane)
		}
		stored, err := os.ReadFile(file)
		if err != nil || string(stored) != "t1\therd\tauto\nt2\therd\tauto\n" {
			t.Fatalf("herd-tab=%q err=%v", stored, err)
		}
	})
}

func TestTabLabelCommandJSContract(t *testing.T) {
	t.Run(`tab-label command: list, pin, pin on a given tab, --auto, dedupe, errors`, func(t *testing.T) { // JS: "tab-label command: list, pin, pin on a given tab, --auto, dedupe, errors"
		// Mutation captured: routing a label to the wrong tab or losing its mode changes the printed command result and saved metadata.
		t.Run("list and pin a chosen tab", func(t *testing.T) {
			rules := []fakecli.Rule{
				{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"release"}}}`},
				{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"release"}}}`},
				{Argv: []string{"tab", "rename", "t1", "review queue"}},
			}
			env, ctx, cwd, dir := layoutFake(t, rules)
			file := filepath.Join(dir, "herd-tab")
			if err := os.WriteFile(file, []byte("t1\trelease\tmanual\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			old := platform.Stdout
			var out bytes.Buffer
			platform.Stdout = &out
			defer func() { platform.Stdout = old }()
			if code := CmdTabLabel(nil, ctx, env, cwd); code != 0 || !strings.Contains(out.String(), "t1         release") {
				t.Fatalf("list code=%d out=%q", code, out.String())
			}
			out.Reset()
			if code := CmdTabLabel([]string{"--tab", "t1", "review", "queue"}, ctx, env, cwd); code != 0 || !strings.Contains(out.String(), `"label": "review queue"`) {
				t.Fatalf("pin code=%d out=%q", code, out.String())
			}
			stored, err := os.ReadFile(file)
			if err != nil || string(stored) != "t1\treview queue\tmanual\n" {
				t.Fatalf("herd-tab=%q err=%v", stored, err)
			}
		})
		t.Run("auto deduplicates labels and unknown tabs fail", func(t *testing.T) {
			rules := []fakecli.Rule{
				{Argv: []string{"tab", "get", "t1"}, Call: 1, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
				{Argv: []string{"tab", "get", "t2"}, Call: 1, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
				{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[]}}`},
				{Argv: []string{"tab", "get", "t1"}, Call: 2, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
				{Argv: []string{"tab", "get", "t2"}, Call: 2, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
				{Argv: []string{"tab", "get", "t1"}, Call: 3, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
				{Argv: []string{"tab", "get", "t2"}, Call: 3, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
				{Argv: []string{"tab", "rename", "t2", "herd 2"}},
				{Argv: []string{"tab", "get", "t1"}, Call: 4, Stdout: `{"result":{"tab":{"label":"herd"}}}`},
				{Argv: []string{"tab", "get", "t2"}, Call: 4, Stdout: `{"result":{"tab":{"label":"herd 2"}}}`},
			}
			env, ctx, cwd, dir := layoutFake(t, rules)
			file := filepath.Join(dir, "herd-tab")
			if err := os.WriteFile(file, []byte("t1\t-\tauto\nt2\t-\tauto\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			old := platform.Stdout
			var out bytes.Buffer
			platform.Stdout = &out
			defer func() { platform.Stdout = old }()
			if code := CmdTabLabel([]string{"--tab", "t1", "--auto"}, ctx, env, cwd); code != 0 {
				t.Fatalf("auto exit code=%d", code)
			}
			stored, err := os.ReadFile(file)
			if err != nil || !strings.Contains(out.String(), `"label": "herd"`) || string(stored) != "t1\therd\tauto\nt2\therd 2\tauto\n" {
				t.Fatalf("output=%q herd-tab=%q err=%v", out.String(), stored, err)
			}
		})
		t.Run("unknown tab is a usage-level error", func(t *testing.T) {
			env, ctx, cwd, dir := layoutFake(t, []fakecli.Rule{{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"release"}}}`}})
			if err := os.WriteFile(filepath.Join(dir, "herd-tab"), []byte("t1\trelease\tmanual\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			defer func() {
				value := recover()
				e, ok := value.(*platform.ExitError)
				if !ok || e.Code != 3 || e.Msg != "tab-label: nope is not a herd tab of this workspace (see: tab-label)" {
					t.Fatalf("panic=%#v", value)
				}
			}()
			CmdTabLabel([]string{"--tab", "nope", "x"}, ctx, env, cwd)
		})
	})
}

func TestHerdTabsRelabelDetectsManualRename(t *testing.T) {
	t.Run(`herdTabsRelabel: a rename done in Herdr by hand is respected (manual > auto)`, func(t *testing.T) { // JS: "herdTabsRelabel: a rename done in Herdr by hand is respected (manual > auto)"
		// Mutation captured: leaving a changed auto label in auto mode overwrites the user's Herdr rename later.
		env, ctx, cwd, dir := layoutFake(t, []fakecli.Rule{{
			Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[]}}`,
		}, {
			Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"chosen"}}}`,
		}})
		file := filepath.Join(dir, "herd-tab")
		if err := os.WriteFile(file, []byte("t1\tgenerated\tauto\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		HerdTabsRelabel(ctx, env, cwd)
		got, err := os.ReadFile(file)
		if err != nil || string(got) != "t1\tchosen\tmanual\n" {
			t.Fatalf("herd-tab=%q err=%v", got, err)
		}
	})
}
