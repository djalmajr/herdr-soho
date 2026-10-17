package testutil

import (
	"strings"
	"testing"
)

// Frozen golden anchors (parity-spawn.json, parity-dispatch.json,
// parity-kinds.json — bytes as frozen; see DJA-392 adaptation comment).
const (
	frozenPiJSON = `{
  "name": "build",
  "pane_id": "p-new",
  "kind": "pi",
  "role": "implementer",
  "family": "unknown",
  "created_pane": true,
  "layout": "tab",
  "placement": "herd",
  "effort": "xhigh",
  "model": "default",
  "model_spec": "",
  "approvals": "ask",
  "agent_args": "--thinking xhigh",
  "status": "ready"
}
`
	frozenCodexJSON = `{
  "name": "build",
  "pane_id": "p-new",
  "kind": "codex",
  "role": "implementer",
  "family": "openai",
  "created_pane": true,
  "layout": "tab",
  "placement": "herd",
  "effort": "xhigh",
  "model": "sol|gpt-5",
  "model_spec": "sol|gpt-5",
  "approvals": "ask",
  "agent_args": "-m sol|gpt-5 -c model_reasoning_effort=\"xhigh\"",
  "status": "ready"
}
`
	frozenGrokJSON = `{
  "name": "build",
  "pane_id": "p-new",
  "kind": "grok",
  "role": "implementer",
  "family": "xai",
  "created_pane": true,
  "layout": "tab",
  "placement": "herd",
  "effort": "xhigh",
  "model": "grok-4.7",
  "model_spec": "grok",
  "approvals": "ask",
  "agent_args": "--model grok-4.7 --reasoning-effort xhigh",
  "status": "ready"
}
`
	frozenDesignerJSON = `{
  "name": "designer",
  "pane_id": "p-new",
  "kind": "agy",
  "role": "designer",
  "family": "google",
  "created_pane": true,
  "layout": "split",
  "placement": "herd",
  "effort": "high",
  "model": "gemini-2.5",
  "model_spec": "gemini|opus",
  "approvals": "ask",
  "agent_args": "--model gemini-2.5 --effort high",
  "status": "ready"
}
`
	frozenReuseJSON = `{
  "name": "build",
  "pane_id": "p-new",
  "kind": "pi",
  "role": "implementer",
  "family": "unknown",
  "reused": true,
  "previous_role": "implementer",
  "status": "ready"
}
`
)

func TestMcpPerRoleOracleSpawnJSON(t *testing.T) {
	t.Run("the pi flag lands after the effort args", func(t *testing.T) {
		got, err := RewriteMcpSpawnJSON(frozenPiJSON)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, `"agent_args": "--thinking xhigh --no-mcp",`) {
			t.Fatalf("out=%q", got)
		}
		if strings.Replace(got, "--thinking xhigh --no-mcp", "--thinking xhigh", 1) != frozenPiJSON {
			t.Fatalf("only the agent_args field may change:\n%s", got)
		}
	})

	t.Run("the codex flag is two fields after the effort config", func(t *testing.T) {
		got, err := RewriteMcpSpawnJSON(frozenCodexJSON)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, `"agent_args": "-m sol|gpt-5 -c model_reasoning_effort=\"xhigh\" -c mcp_servers={}",`) {
			t.Fatalf("out=%q", got)
		}
	})

	t.Run("an unsupported kind never changes the JSON", func(t *testing.T) {
		got, err := RewriteMcpSpawnJSON(frozenGrokJSON)
		if err != nil || got != frozenGrokJSON {
			t.Fatalf("out=%q err=%v", got, err)
		}
	})

	t.Run("a role without the frontmatter key (inherit) is untouched", func(t *testing.T) {
		got, err := RewriteMcpSpawnJSON(frozenDesignerJSON)
		if err != nil || got != frozenDesignerJSON {
			t.Fatalf("out=%q err=%v", got, err)
		}
	})

	t.Run("a reuse JSON is untouched", func(t *testing.T) {
		got, err := RewriteMcpSpawnJSON(frozenReuseJSON)
		if err != nil || got != frozenReuseJSON {
			t.Fatalf("out=%q err=%v", got, err)
		}
	})

	t.Run("the flag lands before the config args that follow the anchor", func(t *testing.T) {
		out := strings.Replace(frozenPiJSON, `"agent_args": "--thinking xhigh",`, `"agent_args": "--thinking xhigh --config-x",`, 1)
		got, err := RewriteMcpSpawnJSON(out)
		if err != nil || !strings.Contains(got, `"agent_args": "--thinking xhigh --no-mcp --config-x",`) {
			t.Fatalf("out=%q err=%v", got, err)
		}
	})

	t.Run("a supported kind with no anchor at all fails instead of guessing", func(t *testing.T) {
		out := strings.Replace(frozenPiJSON, `"agent_args": "--thinking xhigh",`, `"agent_args": "--config-x --config-y --config-z",`, 1)
		if _, err := RewriteMcpSpawnJSON(out); err == nil {
			t.Fatal("the anchor-free three-arg form must fail")
		}
	})
	t.Run("a run out keeps the dispatch JSON after the adapted spawn JSON", func(t *testing.T) {
		out := frozenPiJSON + `{"wait_status":"submitted","agent":"build","role":"implementer","kind":"pi"}\n`
		got, err := RewriteMcpSpawnJSON(out)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(frozenPiJSON, "--thinking xhigh", "--thinking xhigh --no-mcp", 1) + `{"wait_status":"submitted","agent":"build","role":"implementer","kind":"pi"}\n`
		if got != want {
			t.Fatalf("out=%q, want %q", got, want)
		}
		gotErr, err := RewriteMcpErr("", out)
		if err != nil || gotErr != "" {
			t.Fatalf("a supported kind warns nothing: err=%q got=%v", gotErr, err)
		}
	})

	t.Run("a dispatch-only out is untouched", func(t *testing.T) {
		out := `{"wait_status":"quota","agent":"build","role":"implementer","kind":"grok"}\n`
		got, err := RewriteMcpSpawnJSON(out)
		if err != nil || got != out {
			t.Fatalf("out=%q err=%v", got, err)
		}
		gotErr, err := RewriteMcpErr("", out)
		if err != nil || gotErr != "" {
			t.Fatalf("a single-line dispatch out warns nothing: err=%q got=%v", gotErr, err)
		}
	})
}

func TestMcpPerRoleOracleAgentStartLog(t *testing.T) {
	t.Run("the frozen agent start lines adapt at the item-5 position", func(t *testing.T) {
		log := "agent list\n" +
			"agent start build --kind pi --pane p-new --timeout 60000 -- --thinking xhigh\n" +
			"agent start build --kind codex --pane p-new --timeout 60000 -- -m sol|gpt-5 -c model_reasoning_effort=\"xhigh\"\n" +
			"agent start build --kind grok --pane p-new --timeout 60000 -- --model grok-4.7 --reasoning-effort xhigh\n"
		got, err := RewriteMcpAgentStartLog(log)
		if err != nil {
			t.Fatal(err)
		}
		want := "agent list\n" +
			"agent start build --kind pi --pane p-new --timeout 60000 -- --thinking xhigh --no-mcp\n" +
			"agent start build --kind codex --pane p-new --timeout 60000 -- -m sol|gpt-5 -c model_reasoning_effort=\"xhigh\" -c mcp_servers={}\n" +
			"agent start build --kind grok --pane p-new --timeout 60000 -- --model grok-4.7 --reasoning-effort xhigh\n"
		if got != want {
			t.Fatalf("log:\n%swant:\n%s", got, want)
		}
	})

	t.Run("an empty argv gains the separator", func(t *testing.T) {
		got, err := RewriteMcpAgentStartLog("agent start build --kind pi --pane p-new --timeout 60000\n")
		if err != nil {
			t.Fatal(err)
		}
		if got != "agent start build --kind pi --pane p-new --timeout 60000 -- --no-mcp\n" {
			t.Fatalf("log=%q", got)
		}
	})

	t.Run("an empty log passes through", func(t *testing.T) {
		got, err := RewriteMcpAgentStartLog("")
		if err != nil || got != "" {
			t.Fatalf("log=%q err=%v", got, err)
		}
	})
}

func TestMcpPerRoleOracleErr(t *testing.T) {
	notready := "PROG: warning: agent 'build' is blocked during startup (update prompt, login, trust dialog…). Screen follows; ask the user before answering it, then: herdr agent send-keys build <keys>; herdr agent wait build --timeout 60000\n"

	t.Run("an empty stderr becomes the single warning line", func(t *testing.T) {
		got, err := RewriteMcpErr("", frozenGrokJSON)
		if err != nil {
			t.Fatal(err)
		}
		want := "PROG: warning: spawn: grok cannot turn MCP servers off per process; 'build' keeps the user's MCP servers\n"
		if got != want {
			t.Fatalf("err=%q, want %q", got, want)
		}
	})

	t.Run("the warning precedes the startup-blocked line", func(t *testing.T) {
		got, err := RewriteMcpErr(notready, frozenGrokJSON)
		if err != nil {
			t.Fatal(err)
		}
		want := "PROG: warning: spawn: grok cannot turn MCP servers off per process; 'build' keeps the user's MCP servers\n" + notready
		if got != want {
			t.Fatalf("err=%q, want %q", got, want)
		}
	})

	t.Run("after a decision line the warning is appended", func(t *testing.T) {
		decision := "PROG: warning: lane 'build' worker 'build' is gone; opening a new pane\n"
		got, err := RewriteMcpErr(decision, frozenGrokJSON)
		if err != nil {
			t.Fatal(err)
		}
		want := decision + "PROG: warning: spawn: grok cannot turn MCP servers off per process; 'build' keeps the user's MCP servers\n"
		if got != want {
			t.Fatalf("err=%q, want %q", got, want)
		}
	})

	t.Run("the warning precedes the tab-label line", func(t *testing.T) {
		tabLabel := "PROG: warning: --tab-label ignored: --pane places the worker in a given pane\n"
		got, err := RewriteMcpErr(tabLabel, frozenGrokJSON)
		if err != nil {
			t.Fatal(err)
		}
		want := "PROG: warning: spawn: grok cannot turn MCP servers off per process; 'build' keeps the user's MCP servers\n" + tabLabel
		if got != want {
			t.Fatalf("err=%q, want %q", got, want)
		}
	})

	t.Run("a supported kind warns nothing", func(t *testing.T) {
		got, err := RewriteMcpErr(notready, frozenPiJSON)
		if err != nil || got != notready {
			t.Fatalf("err=%q err=%v", got, err)
		}
	})

	t.Run("a reuse warns nothing", func(t *testing.T) {
		got, err := RewriteMcpErr("", frozenReuseJSON)
		if err != nil || got != "" {
			t.Fatalf("err=%q err=%v", got, err)
		}
	})

	t.Run("an inherit role warns nothing", func(t *testing.T) {
		got, err := RewriteMcpErr("", frozenDesignerJSON)
		if err != nil || got != "" {
			t.Fatalf("err=%q err=%v", got, err)
		}
	})
}

func TestMcpPerRoleOracleKindsTable(t *testing.T) {
	frozen := "KIND     EXECUTABLE    FAMILY     EFFORT   INSTALLED\n" +
		"claude   claude        anthropic  max      yes\n" +
		"codex    codex         openai     max      yes\n" +
		"grok     grok          xai        xhigh    yes\n" +
		"agy      agy           google     high     yes\n" +
		"gemini   gemini        google     high     no\n" +
		"cursor   cursor-agent  by model   xhigh    yes\n" +
		"pi       pi            by model   max      yes\n" +
		"opencode opencode      by model            yes\n"
	want := "KIND     EXECUTABLE    FAMILY     EFFORT   INSTALLED MCP\n" +
		"claude   claude        anthropic  max      yes       yes\n" +
		"codex    codex         openai     max      yes       yes\n" +
		"grok     grok          xai        xhigh    yes       no\n" +
		"agy      agy           google     high     yes       no\n" +
		"gemini   gemini        google     high     no        no\n" +
		"cursor   cursor-agent  by model   xhigh    yes       no\n" +
		"pi       pi            by model   max      yes       yes\n" +
		"opencode opencode      by model            yes       no\n"
	got, err := RewriteMcpKindsTable(frozen)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("table:\n%swant:\n%s", got, want)
	}

	// Without the table anchor the rewrite is a no-op (the comparison with
	// the live output fails loudly instead).
	other := "model output --kind pi\n"
	got, err = RewriteMcpKindsTable(other)
	if err != nil || got != other {
		t.Fatalf("no-op failed: out=%q err=%v", got, err)
	}
}

func TestMcpPerRoleOracleFrictionLog(t *testing.T) {
	t.Run("exactly the one warning line is accepted", func(t *testing.T) {
		line := "TS\twarning\trun\tspawn: grok cannot turn MCP servers off per process; 'build' keeps the user's MCP servers\n"
		if !IsMcpFrictionLog("state/ws/friction.log", line) {
			t.Fatalf("the DJA-392 friction line must be accepted: %q", line)
		}
	})

	t.Run("a second line is refused", func(t *testing.T) {
		line := "TS\twarning\trun\tspawn: grok cannot turn MCP servers off per process; 'build' keeps the user's MCP servers\nTS\tnote\tdispatch\twhatever\n"
		if IsMcpFrictionLog("state/ws/friction.log", line) {
			t.Fatalf("a second line must fail: %q", line)
		}
	})

	t.Run("another command's warning is refused", func(t *testing.T) {
		line := "TS\twarning\tdispatch\tspawn: grok cannot turn MCP servers off per process; 'build' keeps the user's MCP servers\n"
		if IsMcpFrictionLog("state/ws/friction.log", line) {
			t.Fatalf("a non-run command must fail: %q", line)
		}
	})

	t.Run("a different message is refused", func(t *testing.T) {
		line := "TS\twarning\trun\tspawn: grok hit a provider error\n"
		if IsMcpFrictionLog("state/ws/friction.log", line) {
			t.Fatalf("another message must fail: %q", line)
		}
	})

	t.Run("a different file name is refused", func(t *testing.T) {
		line := "TS\twarning\trun\tspawn: grok cannot turn MCP servers off per process; 'build' keeps the user's MCP servers\n"
		if IsMcpFrictionLog("state/ws/other.log", line) {
			t.Fatalf("another file must fail")
		}
	})
}
