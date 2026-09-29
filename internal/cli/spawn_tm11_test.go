//go:build !windows

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

type tm11SpawnStep struct {
	Args []string `json:"args"`
	Err  string   `json:"err"`
	Log  *string  `json:"log"`
	Out  string   `json:"out"`
	RC   int      `json:"rc"`
}

type tm11SpawnGolden struct {
	Files map[string]*string `json:"files"`
	Steps []tm11SpawnStep    `json:"steps"`
}

const tm11SpawnFakeHerdr = `#!/bin/sh
printf '%s\n' "$*" >> "$HA_LOG"
target="${3:-}"
mode=$(cat "$HA_MODE" 2>/dev/null || echo idle)
case "$1 $2" in
  "--version"*) echo 'herdr 1.0.0' ;;
  "status server"*) echo 'server 1.0.0' ;;
  "agent get")
    case "$target" in
      gone|dead) echo '{"error":{"code":"agent_not_found","message":"gone"}}' >&2; exit 1 ;;
    esac
    case "$mode" in
      working) echo "{"result":{"agent":{"name":"$target","agent_status":"working"}}}" ;;
      blocked) echo "{"result":{"agent":{"name":"$target","agent_status":"blocked"}}}" ;;
      gone|gone-until-start) echo '{"error":{"code":"agent_not_found","message":"gone"}}' >&2; exit 1 ;;
      *) echo "{"result":{"agent":{"name":"$target","agent_status":"idle"}}}" ;;
    esac ;;
  "agent list")
    if [ -f "$HA_LIVE" ]; then cat "$HA_LIVE"; else echo '{"result":{"agents":[]}}'; fi ;;
  "agent start")
    case "$mode" in
      busy2)
        n=$(cat "$HA_STARTN" 2>/dev/null || echo 0)
        n=$((n + 1)); echo "$n" > "$HA_STARTN"
        if [ "$n" -le 2 ]; then
          echo '{"error":{"code":"agent_pane_busy","message":"shell not ready"}}' >&2
          exit 1
        fi ;;
      notready) echo 'agent_not_ready: login prompt' >&2; exit 1 ;;
      gone-until-start) echo idle > "$HA_MODE" ;; # the old worker is gone; the new one lives
    esac
    echo '{"result":{"started":true}}' ;;
  "agent read") cat "$HA_SCREEN" ;;
  "pane list")
    if [ -f "$HA_PANES" ]; then cat "$HA_PANES"; else echo '{"result":{"panes":[]}}'; fi ;;
  "pane layout")
    if [ -f "$HA_LAYOUT" ]; then cat "$HA_LAYOUT"; else echo '{"error":"no layout"}' >&2; exit 1; fi ;;
  "tab create") echo '{"result":{"tab":{"tab_id":"t-herd","label":"herd"},"root_pane":{"pane_id":"p-new"}}}' ;;
  "tab get") echo '{"result":{"tab":{"tab_id":"t-herd","label":"herd"},"root_pane":{"pane_id":"p-root"}}}' ;;
  "tab rename") echo '{"result":{}}' ;;
  "agent rename") echo '{"result":{}}' ;;
  *) echo "unexpected: $*" >&2; exit 1 ;;
esac
`

const tm11SpawnFakeGrok = `#!/bin/sh
[ "${1:-}" = models ] && printf '%s\n' grok-4.7
`

const tm11SpawnFakeAgy = `#!/bin/sh
[ "${1:-}" = models ] && printf 'gemini-2.5 (latest)\n'
`

func TestTM11SpawnCommandParity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "skills", "herdr-soho", "scripts", "test", "golden", "parity-spawn.json"))
	if err != nil {
		t.Fatal(err)
	}
	var goldens map[string]tm11SpawnGolden
	if err := json.Unmarshal(data, &goldens); err != nil {
		t.Fatal(err)
	}
	goBinary := tm11BuildSpawnBinary(t)
	t.Run(`JS: "parity spawn: planner is 12, a sub-orchestrator outside a lane is 3"`, func(t *testing.T) {
		golden := tm11RequireSpawnGolden(t, goldens, "errors")
		f := newTM11SpawnFixture(t, nil)
		f.runSteps(t, golden)
	})
	t.Run(`JS: "parity spawn: agent_not_ready registers, prints JSON + screen, exits 7"`, func(t *testing.T) {
		golden := tm11RequireSpawnGolden(t, goldens, "notready")
		rules := tm11SpawnRules(
			fakecli.Rule{Argv: []string{"agent", "start"}, ArgvPrefix: true, Stderr: "agent_not_ready: login prompt\n", Code: 1},
			fakecli.Rule{Argv: []string{"tab", "create"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"tab_id":"t-herd","label":"herd"},"root_pane":{"pane_id":"p-new"}}}`},
			fakecli.Rule{Argv: []string{"pane", "list", "--workspace", "ws"}, ArgvPrefix: true, Stdout: `{"result":{"panes":[{"pane_id":"p-new","tab_id":"t-herd"}]}}`},
			fakecli.Rule{Argv: []string{"tab", "get", "t-herd"}, Stdout: `{"result":{"tab":{"tab_id":"t-herd","label":"herd"}}}`},
			fakecli.Rule{Argv: []string{"agent", "read", "build"}, ArgvPrefix: true, Stdout: "plain screen\n"},
		)
		f := newTM11SpawnFixture(t, rules)
		f.runSteps(t, golden)
	})
	t.Run(`JS: "parity spawn: --pane places the worker in a given pane"`, func(t *testing.T) {
		golden := tm11RequireSpawnGolden(t, goldens, "given-pane")
		rules := tm11SpawnRules(
			fakecli.Rule{Argv: []string{"agent", "start"}, ArgvPrefix: true, Stdout: `{"result":{"started":true}}`},
			fakecli.Rule{Argv: []string{"agent", "get"}, ArgvPrefix: true, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			fakecli.Rule{Argv: []string{"agent", "read"}, ArgvPrefix: true, Stdout: "plain screen\n"},
		)
		f := newTM11SpawnFixture(t, rules)
		f.runSteps(t, golden)
	})
	t.Run(`JS: "parity spawn: agent_pane_busy twice, then success (15x1s retry budget)"`, func(t *testing.T) {
		golden := tm11RequireSpawnGolden(t, goldens, "busy2")
		rules := tm11SpawnRules(
			fakecli.Rule{Argv: []string{"agent", "start"}, ArgvPrefix: true, Call: 1, Stderr: `{"error":{"code":"agent_pane_busy","message":"shell not ready"}}`, Code: 1},
			fakecli.Rule{Argv: []string{"agent", "start"}, ArgvPrefix: true, Call: 2, Stderr: `{"error":{"code":"agent_pane_busy","message":"shell not ready"}}`, Code: 1},
			fakecli.Rule{Argv: []string{"agent", "start"}, ArgvPrefix: true, Call: 3, Stdout: `{"result":{"started":true}}`},
			fakecli.Rule{Argv: []string{"tab", "create"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"tab_id":"t-herd","label":"herd"},"root_pane":{"pane_id":"p-new"}}}`},
			fakecli.Rule{Argv: []string{"pane", "list", "--workspace", "ws"}, ArgvPrefix: true, Stdout: `{"result":{"panes":[{"pane_id":"p-new","tab_id":"t-herd"}]}}`},
			fakecli.Rule{Argv: []string{"tab", "get", "t-herd"}, Stdout: `{"result":{"tab":{"tab_id":"t-herd","label":"herd"}}}`},
			fakecli.Rule{Argv: []string{"agent", "get", "build"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
			fakecli.Rule{Argv: []string{"agent", "read", "build"}, ArgvPrefix: true, Stdout: "plain screen\n"},
		)
		f := newTM11SpawnFixture(t, rules)
		f.runSteps(t, golden)
	})
	t.Run(`JS: "parity spawn: a full caller tab overflows the worker into a herd tab"`, func(t *testing.T) {
		golden := tm11RequireSpawnGolden(t, goldens, "overflow")
		layout := `{"result":{"layout":{"area":{"x":0,"y":0,"width":107,"height":57},"panes":[{"pane_id":"c","tab_id":"t-main","focused":false,"rect":{"x":0,"y":0,"width":53,"height":57}},{"pane_id":"p-w1","tab_id":"t-main","focused":false,"rect":{"x":53,"y":0,"width":54,"height":57}},{"pane_id":"p-w2","tab_id":"t-main","focused":false,"rect":{"x":0,"y":28,"width":53,"height":29}},{"pane_id":"p-w3","tab_id":"t-main","focused":false,"rect":{"x":53,"y":28,"width":54,"height":29}}]}}}`
		panes := `{"result":{"panes":[{"pane_id":"c","tab_id":"t-main"},{"pane_id":"p-w1","tab_id":"t-main"},{"pane_id":"p-w2","tab_id":"t-main"},{"pane_id":"p-w3","tab_id":"t-main"}]}}`
		live := `{"result":{"agents":[{"name":"w1","pane_id":"w1","agent_status":"idle"},{"name":"w2","pane_id":"w2","agent_status":"idle"},{"name":"w3","pane_id":"w3","agent_status":"idle"}]}}`
		rules := []fakecli.Rule{
			{Argv: []string{"agent", "get"}, ArgvPrefix: true, Stdout: `{"result":{"agent":{"agent_status":"idle"}}}`},
			{Argv: []string{"agent", "list"}, Stdout: live},
			{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: panes},
			{Argv: []string{"pane", "list"}, Stdout: panes},
			{Argv: []string{"pane", "layout", "--current"}, Stdout: layout},
			{Argv: []string{"agent", "start"}, ArgvPrefix: true, Stdout: `{"result":{"started":true}}`},
			{Argv: []string{"tab", "create"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"tab_id":"t-herd","label":"herd"},"root_pane":{"pane_id":"p-new"}}}`},
			{Argv: []string{"tab", "get", "t-herd"}, Stdout: `{"result":{"tab":{"tab_id":"t-herd","label":"herd"},"root_pane":{"pane_id":"p-root"}}}`},
			{Argv: []string{"pane", "move"}, ArgvPrefix: true, Stdout: `{"result":{}}`},
			{Argv: []string{"agent", "rename"}, ArgvPrefix: true, Stdout: `{"result":{}}`},
			{Argv: []string{"agent", "read"}, ArgvPrefix: true, Stdout: "plain screen\n"},
		}
		f := newTM11SpawnFixture(t, rules)
		roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n" +
			"w1\tp-w1\tgrok\timplementer\txai\t1\t" + f.cwd + "\tnow\tfull\timplementer\tbuild\n" +
			"w2\tp-w2\tgrok\tscouter\txai\t1\t" + f.cwd + "\tnow\tfull\tscouter\texplore\n" +
			"w3\tp-w3\tgrok\treviewer\txai\t1\t" + f.cwd + "\tnow\tfull\treviewer\treview\n"
		if err := os.WriteFile(filepath.Join(f.state, "agents.tsv"), []byte(roster), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.bin, "panes.json"), []byte(panes), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.bin, "layout.json"), []byte(layout), 0o600); err != nil {
			t.Fatal(err)
		}
		f.env["HERDR_PANE_ID"] = "c"
		f.env["HERDR_TAB_ID"] = "t-main"
		f.env["HERDR_SOHO_LANES"] = "off"
		f.env["HERDR_SOHO_LAYOUT"] = "split"
		f.env["HERDR_SOHO_SPLIT_MAX_PANES"] = "3"
		f.env["HERDR_SOHO_MAX_WORKERS"] = "4"
		f.runSteps(t, golden)
	})
	t.Run(`JS: "parity spawn: fresh worker, lane reuse, kind mismatch, lane-kind reuse"`, func(t *testing.T) {
		golden := tm11RequireSpawnGolden(t, goldens, "kind")
		f := newTM11SpawnShellFixture(t, goBinary)
		f.runStepsPrepared(t, golden, func(i int) {
			switch i {
			case 0, 2:
				tm11SpawnReset(t, f)
			case 1:
				tm11SpawnSeed(t, f, [][4]string{{"explore", "scouter", "explore", "grok"}}, `{"result":{"agents":[{"name":"explore","pane_id":"p-explore","agent_status":"idle"}]}}`)
				_ = os.Remove(filepath.Join(f.state, "herd-tab"))
			case 3:
				if err := os.WriteFile(filepath.Join(f.bin, "live.json"), []byte(`{"result":{"agents":[{"name":"build","pane_id":"p-build","agent_status":"idle"}]}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case 4, 5:
				kind := "agy"
				if i == 5 {
					kind = "grok"
				}
				tm11SpawnSeed(t, f, [][4]string{{"build", "designer", "build", kind}}, `{"result":{"agents":[{"name":"build","pane_id":"p-build","agent_status":"idle"}]}}`)
				_ = tm11SpawnWriteConfig(t, f, "lane.build.kind = \"grok\"\n", "")
			}
			tm11SpawnInstallStateRules(t, f, "idle")
		})
	})
	t.Run(`JS: "parity spawn: busy 10, gone recreated, worker cap 8, locked 5, lane kind, lanes=off reuse"`, func(t *testing.T) {
		golden := tm11RequireSpawnGolden(t, goldens, "lane-states")
		f := newTM11SpawnShellFixture(t, goBinary)
		f.runStepsPreparedChecked(t, golden, func(i int) {
			status := "idle"
			live := `{"result":{"agents":[]}}`
			switch i {
			case 0:
				tm11SpawnReset(t, f)
				tm11SpawnSeed(t, f, [][4]string{{"build", "implementer", "build", "grok"}}, `{"result":{"agents":[{"name":"build","pane_id":"p-build","agent_status":"working"}]}}`)
				status = "working"
			case 1:
				tm11SpawnSeed(t, f, [][4]string{{"build", "implementer", "build", "grok"}}, live)
				_ = os.Remove(filepath.Join(f.state, "herd-tab"))
				status = "gone-until-start"
			case 2:
				rows := [][4]string{{"explore", "scouter", "explore", "grok"}, {"review", "reviewer", "review", "codex"}, {"extra", "researcher", "extra", "grok"}}
				tm11SpawnSeed(t, f, rows, `{"result":{"agents":[{"name":"explore","pane_id":"p-explore"},{"name":"review","pane_id":"p-review"},{"name":"extra","pane_id":"p-extra"}]}}`)
			case 3:
				tm11SpawnSeed(t, f, [][4]string{{"mix", "implementer", "mix", "grok"}}, `{"result":{"agents":[{"name":"mix","pane_id":"p-mix","agent_status":"idle"}]}}`)
				_ = tm11SpawnWriteConfig(t, f, "lane.mix.roles = \"implementer,reviewer\"\n", "")
			case 4:
				tm11SpawnReset(t, f)
				_ = tm11SpawnWriteConfig(t, f, "lane.build.kind = \"codex\"\n", "")
			case 5:
				roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n" + "implementer\tp-impl\tgrok\timplementer\txai\t1\t" + f.cwd + "\tnow\n"
				if err := os.WriteFile(filepath.Join(f.state, "agents.tsv"), []byte(roster), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(f.bin, "live.json"), []byte(`{"result":{"agents":[{"name":"implementer","pane_id":"p-impl","agent_status":"idle"}]}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if i == 5 {
				f.env["HERDR_SOHO_LANES"] = "off"
			}
			tm11SpawnInstallStateRules(t, f, status)
		})
	})
	t.Run(`JS: "parity spawn: config layers (user vs project vs env, kind/model/effort precedence)"`, func(t *testing.T) {
		golden := tm11RequireSpawnGolden(t, goldens, "layers")
		f := newTM11SpawnShellFixture(t, goBinary)
		f.runStepsPreparedChecked(t, golden, func(i int) {
			delete(f.env, "HERDR_SOHO_LANE_BUILD_KIND")
			user, project := "", ""
			switch i {
			case 0:
				tm11SpawnReset(t, f)
				user = "lane.explore.kind = \"grok\"\nlane.explore.model = \"grok-4.7\"\nlane.explore.model.codex.worker = \"gpt-6-luna\"\n"
				project = "lane.explore.kind = \"codex\"\n"
			case 1:
				tm11SpawnReset(t, f)
				project = "lane.explore.kind = \"codex\"\nlane.explore.model = \"grok-4.7\"\nlane.explore.model.codex.worker = \"gpt-6-luna\"\n"
			case 2:
				tm11SpawnReset(t, f)
				user = "lane.build.model = \"gpt-6-luna\"\nlane.build.model.pi.worker = \"my-provider/my-model\"\n"
				project = "lane.build.kind = \"pi\"\n"
				f.env["HERDR_SOHO_LANE_BUILD_KIND"] = "pi"
			case 3:
				tm11SpawnReset(t, f)
				user = "lane.build.kind = \"codex\"\nlane.build.effort = \"high\"\n"
				project = "lane.build.kind = \"pi\"\nlane.build.effort.pi = \"max\"\n"
			case 4:
				tm11SpawnReset(t, f)
				user = "lane.build.effort = \"high\"\n"
				project = "lane.build.effort.pi = \"max\"\nlane.build.model.pi.worker = \"my-provider/my-model\"\n"
			case 5:
				if err := os.WriteFile(filepath.Join(f.bin, "live.json"), []byte(`{"result":{"agents":[{"name":"build","pane_id":"p-build","agent_status":"idle"}]}}`), 0o600); err != nil {
					t.Fatal(err)
				}
				user = "lane.build.effort = \"high\"\n"
				project = "lane.build.effort.pi = \"max\"\nlane.build.model.pi.worker = \"my-provider/my-model\"\n"
			case 6:
				tm11SpawnReset(t, f)
				user = "lane.build.model = \"grok-4.7\"\n"
				project = "role.implementer.kind = \"codex\"\nrole.implementer.model.codex.worker = \"gpt-6-luna\"\n"
			case 7:
				tm11SpawnReset(t, f)
				user = "lane.build.model = \"grok-4.7\"\n"
			}
			if i != 5 {
				tm11SpawnWriteConfig(t, f, user, project)
			}
			live := `{"result":{"agents":[]}}`
			status := "idle"
			if i == 5 {
				live = `{"result":{"agents":[{"name":"build","pane_id":"p-build","agent_status":"idle"}]}}`
			}
			if i == 6 {
				_ = os.MkdirAll(filepath.Join(f.cwd, ".agents"), 0o700)
			}
			tm11SpawnInstallStateRules(t, f, status, live)
		})
	})
}

func tm11RequireSpawnGolden(t *testing.T, goldens map[string]tm11SpawnGolden, name string) tm11SpawnGolden {
	t.Helper()
	golden, ok := goldens[name]
	if !ok {
		t.Fatalf("parity-spawn golden %q missing", name)
	}
	return golden
}

type tm11SpawnFixture struct {
	env       platform.Env
	cwd       string
	state     string
	bin       string
	herdrLog  string
	shellFake bool
	cliBinary string
}

func newTM11SpawnFixture(t *testing.T, herdrRules []fakecli.Rule) *tm11SpawnFixture {
	t.Helper()
	env, cwd := commandFixture(t)
	root := filepath.Dir(cwd)
	state := filepath.Join(root, "state")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(filepath.Join(state, "ws", "briefs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "ws", "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "ws", "wait"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "ws", "agents.tsv"), []byte("# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	herdrRules = append(herdrRules, fakecli.Rule{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`}, fakecli.Rule{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`}, fakecli.Rule{Argv: []string{"pane", "list", "--workspace", "ws"}, ArgvPrefix: true, Stdout: `{"result":{"panes":[]}}`})
	if _, err := fakecli.Install(t, bin, "herdr", herdrRules); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "grok", []fakecli.Rule{{Argv: []string{"models"}, Stdout: "grok-4.7\n"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "agy", []fakecli.Rule{{Argv: []string{"models"}, Stdout: "gemini-2.5 (latest)\n"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "codex", []fakecli.Rule{{Argv: []string{"models"}, Stdout: "gpt-6-luna (latest)\n"}, {AnyArgs: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "pi", []fakecli.Rule{{AnyArgs: true}}); err != nil {
		t.Fatal(err)
	}
	clean := envFrom(testutil.CleanEnv(t))
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "HERDR_SOHO_SKILL_DIR", "HERDR_WORKSPACE_ID"} {
		clean[key] = env.Get(key)
	}
	clean["PATH"] = bin
	clean["TMPDIR"] = t.TempDir()
	clean["USERPROFILE"] = env.Get("HOME")
	clean["HERDR_ENV"] = "1"
	clean["HERDR_SOHO_DIR"] = state
	clean["HERDR_WORKSPACE_ID"] = "ws"
	clean["HERDR_SOHO_LAYOUT"] = "tab"
	clean["HERDR_SOHO_REGRID"] = "off"
	clean["HERDR_SOHO_WAIT_POLL_MS"] = "1"
	clean = withFakeCLI(clean, bin)
	return &tm11SpawnFixture{env: clean, cwd: cwd, state: filepath.Join(state, "ws"), bin: bin, herdrLog: filepath.Join(bin, "herdr.log")}
}

func newTM11SpawnShellFixture(t *testing.T, cliBinary string) *tm11SpawnFixture {
	t.Helper()
	f := newTM11SpawnFixture(t, nil)
	root := filepath.Dir(f.cwd)
	for key, value := range map[string]string{
		"HA_LOG":    f.herdrLog,
		"HA_MODE":   filepath.Join(f.bin, "mode"),
		"HA_LIVE":   filepath.Join(f.bin, "live.json"),
		"HA_SCREEN": filepath.Join(f.bin, "screen"),
		"HA_PANES":  filepath.Join(f.bin, "panes.json"),
		"HA_LAYOUT": filepath.Join(f.bin, "layout.json"),
		"HA_STARTN": filepath.Join(f.bin, "start-n"),
	} {
		f.env[key] = value
	}
	f.env["TMPDIR"] = filepath.Join(root, "tmp")
	f.env["HERDR_SOCKET_PATH"] = filepath.Join(root, "missing", "herdr.sock")
	f.env["PATH"] = f.bin + string(os.PathListSeparator) + os.Getenv("PATH")
	if node, err := exec.LookPath("node"); err == nil {
		f.env["HERDR_SOHO_JS_RUNTIME"] = node
	}
	if err := os.MkdirAll(f.env["TMPDIR"], 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.cwd, "AGENTS.md"), []byte("# Agent instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, value := range map[string]string{
		f.env["HA_MODE"]:   "idle\n",
		f.env["HA_LIVE"]:   `{"result":{"agents":[]}}` + "\n",
		f.env["HA_SCREEN"]: "plain screen\n",
	} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, source := range map[string]string{"herdr": tm11SpawnFakeHerdr, "grok": tm11SpawnFakeGrok, "agy": tm11SpawnFakeAgy, "pi": "#!/bin/sh\nexit 0\n"} {
		path := filepath.Join(f.bin, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f.shellFake = true
	f.cliBinary = cliBinary
	return f
}

func tm11BuildSpawnBinary(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate spawn parity test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	path := filepath.Join(t.TempDir(), "herdr-soho")
	cmd := exec.Command("go", "build", "-o", path, "./cmd/herdr-soho")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build Go CLI: %s: %v", output, err)
	}
	return path
}

func tm11SpawnRules(extra ...fakecli.Rule) []fakecli.Rule {
	return extra
}

func (f *tm11SpawnFixture) runSteps(t *testing.T, golden tm11SpawnGolden) {
	t.Helper()
	f.runStepsPrepared(t, golden, nil)
}

func (f *tm11SpawnFixture) runStepsPreparedChecked(t *testing.T, golden tm11SpawnGolden, prepare func(int)) {
	t.Helper()
	f.runStepsPrepared(t, golden, prepare)
}

func (f *tm11SpawnFixture) runStepsPrepared(t *testing.T, golden tm11SpawnGolden, prepare func(int)) {
	t.Helper()
	for i, want := range golden.Steps {
		if prepare != nil {
			prepare(i)
		}
		if err := f.clearHerdrCalls(); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := f.runStep(t, want.Args)
		actualOut := strings.ReplaceAll(out, filepath.Dir(f.cwd), "<ROOT>")
		actualErr := strings.ReplaceAll(normalizeParityError(stderr), filepath.Dir(f.cwd), "<ROOT>")
		callLog, err := f.herdrCallLog()
		if err != nil {
			t.Fatal(err)
		}
		actualLog := strings.ReplaceAll(callLog, filepath.Dir(f.cwd), "<ROOT>")
		if code != want.RC || actualOut != want.Out || actualErr != want.Err || (want.Log != nil && actualLog != *want.Log) {
			t.Fatalf("step %d args=%v\ncode=%d want=%d\nstdout=%q want=%q\nstderr=%q want=%q\nlog=%q want=%v", i, want.Args, code, want.RC, actualOut, want.Out, actualErr, want.Err, actualLog, want.Log)
		}
	}
	for name, rel := range map[string]string{"herdTab": "herd-tab", "roster": "agents.tsv"} {
		want := golden.Files[name]
		path := filepath.Join(f.state, rel)
		actual, err := os.ReadFile(path)
		if want == nil {
			if !os.IsNotExist(err) {
				t.Fatalf("%s should be absent, err=%v", name, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		value := strings.ReplaceAll(string(actual), filepath.Dir(f.cwd), "<ROOT>")
		value = normTM11SpawnRoster(value)
		if value != *want {
			t.Fatalf("%s=%q want=%q", name, value, *want)
		}
	}
}

func (f *tm11SpawnFixture) runStep(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	if !f.shellFake {
		return runIn(t, args, f.env, f.cwd)
	}
	cmd := exec.Command(f.cliBinary, args...)
	cmd.Dir = f.cwd
	cmd.Env = f.env.List()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), stdout.String(), stderr.String()
	}
	t.Fatalf("run Go CLI: %v", err)
	return 0, "", ""
}

func (f *tm11SpawnFixture) clearHerdrCalls() error {
	path := filepath.Join(f.bin, "herdr.calls.jsonl")
	if f.shellFake {
		path = f.herdrLog
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (f *tm11SpawnFixture) herdrCallLog() (string, error) {
	if f.shellFake {
		b, err := os.ReadFile(f.herdrLog)
		if os.IsNotExist(err) {
			return "", nil
		}
		return string(b), err
	}
	calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var log strings.Builder
	for _, call := range calls {
		log.WriteString(strings.Join(call.Argv, " "))
		log.WriteByte('\n')
	}
	return log.String(), nil
}

func tm11SpawnReset(t *testing.T, f *tm11SpawnFixture) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.state, "agents.tsv"), []byte("# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(f.state, "herd-tab"))
	if f.shellFake {
		if err := os.WriteFile(f.env["HA_MODE"], []byte("idle\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.env["HA_LIVE"], []byte(`{"result":{"agents":[]}}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(f.env["HA_STARTN"])
	}
}

func tm11SpawnSeed(t *testing.T, f *tm11SpawnFixture, rows [][4]string, live string) {
	t.Helper()
	var roster strings.Builder
	roster.WriteString("# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n")
	for _, row := range rows {
		fmt.Fprintf(&roster, "%s\tp-%s\t%s\t%s\txai\t1\t%s\tgrok-4.7\tfull\t%s\t%s\n", row[0], row[0], row[3], row[1], f.cwd, row[1], row[2])
	}
	if err := os.WriteFile(filepath.Join(f.state, "agents.tsv"), []byte(roster.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.bin, "live.json"), []byte(live), 0o600); err != nil {
		t.Fatal(err)
	}
}

func tm11SpawnWriteConfig(t *testing.T, f *tm11SpawnFixture, user, project string) error {
	t.Helper()
	userPath := filepath.Join(f.env["HOME"], ".config", "herdr-soho", "config.toml")
	projectPath := filepath.Join(f.cwd, ".agents", "herdr-soho.conf")
	if user == "" {
		_ = os.Remove(userPath)
	} else {
		if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(userPath, []byte(user), 0o600); err != nil {
			return err
		}
	}
	if project == "" {
		_ = os.Remove(projectPath)
	} else {
		if err := os.MkdirAll(filepath.Dir(projectPath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(projectPath, []byte(project), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func tm11SpawnInstallStateRules(t *testing.T, f *tm11SpawnFixture, status string, liveOverride ...string) {
	t.Helper()
	if f.shellFake {
		live := `{"result":{"agents":[]}}`
		if len(liveOverride) > 0 {
			live = liveOverride[0]
		} else if b, err := os.ReadFile(f.env["HA_LIVE"]); err == nil {
			live = string(b)
		}
		if err := os.WriteFile(f.env["HA_MODE"], []byte(status+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.env["HA_LIVE"], []byte(live), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	live := `{"result":{"agents":[]}}`
	if len(liveOverride) > 0 {
		live = liveOverride[0]
	} else if b, err := os.ReadFile(filepath.Join(f.bin, "live.json")); err == nil {
		live = string(b)
	}
	get := fakecli.Rule{Argv: []string{"agent", "get"}, ArgvPrefix: true, Stdout: `{"result":{"agent":{"agent_status":"idle"}}}`}
	switch status {
	case "working", "blocked":
		get.Stdout = `{"result":{"agent":{"agent_status":"` + status + `"}}}`
	case "gone":
		get.Stdout, get.Stderr, get.Code = "", `{"error":{"code":"agent_not_found","message":"gone"}}`, 1
	case "gone-until-start":
		get = fakecli.Rule{Argv: []string{"agent", "get"}, ArgvPrefix: true, Call: 1, Stderr: `{"error":{"code":"agent_not_found","message":"gone"}}`, Code: 1}
	case "missing-status":
		get.Stdout = `{"result":{"agent":{"name":"build"}}}`
	}
	rules := []fakecli.Rule{
		get,
		{Argv: []string{"agent", "list"}, Stdout: live},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[]}}`},
		{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`},
		{Argv: []string{"agent", "start"}, ArgvPrefix: true, Stdout: `{"result":{"started":true}}`},
		{Argv: []string{"tab", "create"}, ArgvPrefix: true, Stdout: `{"result":{"tab":{"tab_id":"t-herd","label":"herd"},"root_pane":{"pane_id":"p-new"}}}`},
		{Argv: []string{"tab", "get", "t-herd"}, Stdout: `{"result":{"tab":{"tab_id":"t-herd","label":"herd"},"root_pane":{"pane_id":"p-root"}}}`},
		{Argv: []string{"agent", "read"}, ArgvPrefix: true, Stdout: "plain screen\n"},
		{Argv: []string{"agent", "rename"}, ArgvPrefix: true, Stdout: `{"result":{}}`},
	}
	if status == "gone-until-start" {
		rules = append(rules, fakecli.Rule{Argv: []string{"agent", "get"}, ArgvPrefix: true, Call: 2, Stdout: `{"result":{"agent":{"agent_status":"idle"}}}`})
	}
	config := struct {
		Log   string         `json:"log"`
		Rules []fakecli.Rule `json:"rules"`
	}{Log: filepath.Join(f.bin, "herdr.calls.jsonl"), Rules: append(rules, fakecli.Rule{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`}, fakecli.Rule{Argv: []string{"pane", "list"}, Stdout: `{"result":{"panes":[]}}`})}
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.bin, "herdr.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func normTM11SpawnRoster(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) > 7 {
			fields[7] = "T"
		}
		lines[i] = strings.Join(fields, "\t")
	}
	return strings.Join(lines, "\n")
}
