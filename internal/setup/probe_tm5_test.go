package setup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

type tm5ProbeStep struct {
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
	Err  string            `json:"err"`
	Out  string            `json:"out"`
	RC   int               `json:"rc"`
}

type tm5ProbeGolden struct {
	Steps []tm5ProbeStep `json:"steps"`
}

func readProbeGoldens(t *testing.T) map[string]tm5ProbeGolden {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "skills", "herdr-soho", "scripts", "test", "golden", "parity-setup-probe.json"))
	if err != nil {
		t.Fatal(err)
	}
	var goldens map[string]tm5ProbeGolden
	if err := json.Unmarshal(data, &goldens); err != nil {
		t.Fatal(err)
	}
	return goldens
}

func probeGoldenContext(t *testing.T, kind, model string, rules []fakecli.Rule) (platform.Env, *core.Config) {
	t.Helper()
	bin := t.TempDir()
	if _, err := fakecli.Install(t, bin, kind, rules); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{}
	for _, item := range fakecli.Env(testutil.CleanEnv(t), bin) {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	delete(env, "HERDR_SOHO_MODEL_"+strings.ToUpper(kind)+"_WORKER")
	env["PATH"] = bin
	home := t.TempDir()
	env["HOME"], env["USERPROFILE"] = home, home
	env["XDG_CONFIG_HOME"], env["TMPDIR"] = filepath.Join(home, ".config"), t.TempDir()
	env["HERDR_SOHO_SKILL_DIR"] = filepath.Join("..", "..", "skills", "herdr-soho")
	env["HERDR_PANE_ID"], env["HERDR_WORKSPACE_ID"] = "w0test:p0a", "w0test"
	env["HERDR_SOCKET_PATH"] = filepath.Join(t.TempDir(), "missing-socket")
	if os.PathSeparator == '\\' {
		env["PATHEXT"] = ".EXE;.CMD"
	}
	ctx := &core.Config{Entries: map[string]core.ConfigEntry{}, Order: []string{}}
	if model != "" {
		ctx.Entries["model_"+kind+"_worker"] = core.ConfigEntry{Value: model, Source: "defaults"}
	}
	return env, ctx
}

func probeAllFakeEnv(t *testing.T, bin string) platform.Env {
	t.Helper()
	env := platform.Env{}
	for _, item := range fakecli.Env(testutil.CleanEnv(t), bin, fakecli.EnvOptions{SystemPath: "/usr/bin:/bin"}) {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	home := t.TempDir()
	env["HOME"], env["USERPROFILE"] = home, home
	env["XDG_CONFIG_HOME"], env["TMPDIR"] = filepath.Join(home, ".config"), t.TempDir()
	env["HERDR_SOHO_SKILL_DIR"] = filepath.Join("..", "..", "skills", "herdr-soho")
	return env
}

func runProbeGoldenStep(t *testing.T, step tm5ProbeStep, env platform.Env, ctx *core.Config) {
	t.Helper()
	old := platform.Stdout
	var out bytes.Buffer
	platform.Stdout = &out
	t.Cleanup(func() { platform.Stdout = old })
	for key, value := range step.Env {
		env[key] = value
	}
	code, message := 0, ""
	func() {
		defer func() {
			if value := recover(); value != nil {
				e, ok := value.(*platform.ExitError)
				if !ok {
					panic(value)
				}
				code, message = e.Code, e.Msg
			}
		}()
		CmdProbe(step.Args[2:], ctx, env, t.TempDir())
	}()
	gotErr := ""
	if message != "" {
		gotErr = "PROG: " + message + "\n"
	}
	if code != step.RC || gotErr != step.Err || out.String() != step.Out {
		t.Fatalf("args=%q got rc=%d err=%q out=%q; want rc=%d err=%q out=%q", step.Args, code, gotErr, out.String(), step.RC, step.Err, step.Out)
	}
}

func TestSetupProbeTM5ParityGoldens(t *testing.T) {
	t.Run("// JS: \"parity: setup --probe single kind with an explicit model\"", func(t *testing.T) { // Mutation captured: changing the probe's kind/model/status JSON diverges from the JS golden.
		golden := readProbeGoldens(t)["probe-one"].Steps[0]
		env, ctx := probeGoldenContext(t, "pi", "", []fakecli.Rule{{Argv: []string{"-p", "--no-session", ProbePrompt, "--model", "my-provider/my-model"}, Stdout: "ok\n"}})
		runProbeGoldenStep(t, golden, env, ctx)
	})
	t.Run("// JS: \"parity: setup --probe no-auth — the fixed cause, never the CLI line or its key\"", func(t *testing.T) { // Mutation captured: returning provider stderr leaks the authentication line into the public result.
		golden := readProbeGoldens(t)["probe-noauth"].Steps[0]
		env, ctx := probeGoldenContext(t, "claude", "opus", []fakecli.Rule{{Argv: []string{"-p", ProbePrompt, "--model", "opus"}, Code: 1, Stderr: "Not logged in token=sk_live_private"}})
		runProbeGoldenStep(t, golden, env, ctx)
	})
	t.Run("// JS: \"parity: setup --probe error — exit <code>, the printed key never reaches the JSON\"", func(t *testing.T) { // Mutation captured: provider output must not replace the stable exit-code cause.
		golden := readProbeGoldens(t)["probe-error"].Steps[0]
		env, ctx := probeGoldenContext(t, "codex", "sol|gpt-5", []fakecli.Rule{{Argv: []string{"exec", "-m", "sol|gpt-5", ProbePrompt}, Code: 3, Stderr: "token=sk_live_private"}})
		runProbeGoldenStep(t, golden, env, ctx)
	})
	t.Run("// JS: \"parity: setup --probe quota — with and without a renewal time\"", func(t *testing.T) { // Mutation captured: quota renewal output changes the golden cause and must not expose adjacent secrets.
		goldens := readProbeGoldens(t)
		env, ctx := probeGoldenContext(t, "grok", "grok", []fakecli.Rule{{Argv: []string{"-p", ProbePrompt, "--model", "grok"}, Code: 1, Stderr: "You have hit your usage limit. Try again in 5 minutes. token=sk_live_private"}})
		runProbeGoldenStep(t, goldens["probe-quota"].Steps[0], env, ctx)
		t.Run("renewal time is retained", func(t *testing.T) {
			env, ctx := probeGoldenContext(t, "grok", "grok", []fakecli.Rule{{Argv: []string{"-p", ProbePrompt, "--model", "grok"}, Code: 1, Stderr: "Error: You have hit your usage limit. Try again at 14:30."}})
			runProbeGoldenStep(t, goldens["probe-quotatime"].Steps[0], env, ctx)
		})
	})
	t.Run("// JS: \"parity: setup --probe usage errors (bad timeout, unknown kind, flag without a value) exit 2\"", func(t *testing.T) { // Mutation captured: accepting malformed probe argv changes the JS golden's exit and error output.
		golden := readProbeGoldens(t)["probe-usage"]
		env, ctx := probeGoldenContext(t, "pi", "", []fakecli.Rule{{AnyArgs: true, Stdout: "ok\n"}})
		for i, step := range golden.Steps {
			stepEnv := env.Clone()
			if i == 2 {
				stepEnv["HERDR_SOHO_PROBE_TIMEOUT"] = "bad"
			}
			t.Run(strings.Join(step.Args, " "), func(t *testing.T) { runProbeGoldenStep(t, step, stepEnv, ctx) })
		}
	})
	t.Run("// JS: \"parity: setup --probe a hung CLI is a timeout after the limit\"", func(t *testing.T) { // Mutation captured: dropping the timeout returns after the fake CLI's full delay instead of the golden timeout.
		// D8: the Go timeout cause gained the fixed retry hint (timeoutRetryHint)
		// while the frozen JS golden keeps "timeout after 1s": assert the golden's
		// exact output with the cause substituted, so the rest stays
		// byte-identical to the golden.
		golden := readProbeGoldens(t)["probe-timeout"].Steps[0]
		want := strings.Replace(golden.Out, "timeout after 1s\"", "timeout after 1s"+timeoutRetryHint+"\"", 1)
		env, ctx := probeGoldenContext(t, "pi", "", []fakecli.Rule{{Argv: []string{"-p", "--no-session", ProbePrompt}, Delay: 1600}})
		old := platform.Stdout
		var out bytes.Buffer
		platform.Stdout = &out
		t.Cleanup(func() { platform.Stdout = old })
		code, message := 0, ""
		func() {
			defer func() {
				if value := recover(); value != nil {
					e, ok := value.(*platform.ExitError)
					if !ok {
						panic(value)
					}
					code, message = e.Code, e.Msg
				}
			}()
			CmdProbe(golden.Args[2:], ctx, env, t.TempDir())
		}()
		gotErr := ""
		if message != "" {
			gotErr = "PROG: " + message + "\n"
		}
		if code != golden.RC || gotErr != golden.Err || out.String() != want {
			t.Fatalf("args=%q got rc=%d err=%q out=%q; want rc=%d err=%q out=%q", golden.Args, code, gotErr, out.String(), golden.RC, golden.Err, want)
		}
	})
}

func TestSetupProbeTM5Continuation(t *testing.T) {
	t.Run(`// JS: "probeKind: a CLI that cannot be run is exit 127, never exit null"`, func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("missing interpreter execution semantics are Unix-specific")
		}
		bin := t.TempDir()
		exe := filepath.Join(bin, "claude")
		if err := os.WriteFile(exe, []byte("#!/definitely/missing/herdr-test-interpreter\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"PATH": bin, "HOME": t.TempDir(), "HERDR_SOHO_SKILL_DIR": filepath.Join("..", "..", "skills", "herdr-soho")}
		got := probeOne(nil, "claude", "", "configured", 2, env, t.TempDir())
		if got.Status != "error" || got.Cause != "exit 127" {
			t.Fatalf("probe=%+v", got)
		}
	})
	t.Run(`// JS: "probeKind: a hang with a live child is a timeout after the limit, not later"`, func(t *testing.T) {
		bin := t.TempDir()
		env, _ := probeGoldenContext(t, "pi", "", []fakecli.Rule{{Argv: []string{"-p", "--no-session", ProbePrompt}, Delay: 1800}})
		env["PATH"] = bin
		if _, err := fakecli.Install(t, bin, "pi", []fakecli.Rule{{Argv: []string{"-p", "--no-session", ProbePrompt}, Delay: 1800}}); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		got := probeOne(nil, "pi", "", "configured", 1, env, t.TempDir())
		elapsed := time.Since(start)
		if got.Status != "error" || got.Cause != "timeout after 1s (a timeout does not prove the assistant is unavailable; retry with --timeout 90)" || elapsed < 900*time.Millisecond || elapsed > 2500*time.Millisecond {
			t.Fatalf("probe=%+v elapsed=%v", got, elapsed)
		}
	})
	t.Run(`// JS: "e2e: the timeout classifies as error with the fixed cause, not later than limit + grace"`, func(t *testing.T) {
		bin := t.TempDir()
		env, ctx := probeGoldenContext(t, "pi", "", []fakecli.Rule{{Argv: []string{"-p", "--no-session", ProbePrompt}, Delay: 1800}})
		env["PATH"] = bin
		if _, err := fakecli.Install(t, bin, "pi", []fakecli.Rule{{Argv: []string{"-p", "--no-session", ProbePrompt}, Delay: 1800}}); err != nil {
			t.Fatal(err)
		}
		old := platform.Stdout
		var out bytes.Buffer
		platform.Stdout = &out
		defer func() { platform.Stdout = old }()
		start := time.Now()
		CmdProbe([]string{"--kind", "pi", "--timeout", "1"}, ctx, env, t.TempDir())
		elapsed := time.Since(start)
		var got struct {
			Probes []Probe `json:"probes"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Probes) != 1 || got.Probes[0].Cause != "timeout after 1s (a timeout does not prove the assistant is unavailable; retry with --timeout 90)" || elapsed > 2500*time.Millisecond {
			t.Fatalf("probe=%+v elapsed=%v output=%s", got.Probes, elapsed, out.String())
		}
	})
	t.Run(`// JS: "e2e: the aggregate probe covers the user own models (5 probed, the rest skipped_custom)"`, func(t *testing.T) {
		bin := t.TempDir()
		env := probeAllFakeEnv(t, bin)
		if _, err := fakecli.Install(t, bin, "pi", []fakecli.Rule{{AnyArgs: true, Stdout: "ok\n"}}); err != nil {
			t.Fatal(err)
		}
		modelsPath := filepath.Join(env.Get("HOME"), ".pi", "agent", "models.json")
		if err := os.MkdirAll(filepath.Dir(modelsPath), 0o700); err != nil {
			t.Fatal(err)
		}
		models := `{"providers":{"own":{"apiKey":"CUSTOMSECRET","models":[{"id":"m1"},{"id":"m2"},{"id":"m3"},{"id":"m4"},{"id":"m5"},{"id":"m6"},{"id":"m7"}]}}}`
		if err := os.WriteFile(modelsPath, []byte(models), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx := &core.Config{Entries: map[string]core.ConfigEntry{}, Order: []string{}}
		old := platform.Stdout
		var out bytes.Buffer
		platform.Stdout = &out
		defer func() { platform.Stdout = old }()
		CmdProbe(nil, ctx, env, t.TempDir())
		var got struct {
			Probes  []Probe `json:"probes"`
			Skipped []struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"skipped_custom"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		custom := []string{}
		for _, p := range got.Probes {
			if p.Source == "custom" {
				custom = append(custom, p.Model)
			}
		}
		if len(custom) != 5 || !reflect.DeepEqual(custom, []string{"own/m1", "own/m2", "own/m3", "own/m4", "own/m5"}) || len(got.Skipped) != 2 || strings.Contains(out.String(), "CUSTOMSECRET") {
			t.Fatalf("custom=%v skipped=%v output=%s", custom, got.Skipped, out.String())
		}
	})
	t.Run(`// JS: "parity: setup --probe aggregate — shape, statuses, reviewer, model spec"`, func(t *testing.T) {
		golden := readProbeGoldens(t)["probe-aggregate"].Steps[0]
		bin := t.TempDir()
		rules := []string{"pi", "codex", "claude", "grok"}
		env := probeAllFakeEnv(t, bin)
		env["HERDR_SOHO_PROBE_TIMEOUT"] = "2"
		for _, kind := range rules {
			if _, err := fakecli.Install(t, bin, kind, []fakecli.Rule{{AnyArgs: true, Stdout: "ok\n"}}); err != nil {
				t.Fatal(err)
			}
		}
		ctx := &core.Config{Entries: map[string]core.ConfigEntry{
			"model_claude_worker": {Value: "opus", Source: "defaults"},
			"model_codex_worker":  {Value: "sol|gpt-5", Source: "defaults"},
			"model_grok_worker":   {Value: "grok", Source: "defaults"},
			"model_agy_worker":    {Value: "gemini|opus", Source: "defaults"},
			"model_cursor_worker": {Value: "grok|muse", Source: "defaults"},
			"lane_build_kind":     {Value: "grok", Source: "project"},
		}, Order: []string{"lane_build_kind", "model_claude_worker", "model_codex_worker", "model_grok_worker", "model_agy_worker", "model_cursor_worker"}}
		runProbeGoldenStep(t, golden, env, ctx)
	})
	t.Run(`// JS: "parity: the recommended reviewer follows the build family (claude, codex, null)"`, func(t *testing.T) {
		for _, tc := range []struct {
			build, want string
			kinds       []string
		}{{"claude", "codex", []string{"claude", "codex"}}, {"codex", "claude", []string{"claude", "codex"}}, {"grok", "", []string{"pi", "grok"}}} {
			bin := t.TempDir()
			env := probeAllFakeEnv(t, bin)
			for _, kind := range tc.kinds {
				if _, err := fakecli.Install(t, bin, kind, []fakecli.Rule{{AnyArgs: true, Stdout: "ok\n"}}); err != nil {
					t.Fatal(err)
				}
			}
			ctx := &core.Config{Entries: map[string]core.ConfigEntry{"lane_build_kind": {Value: tc.build, Source: "project"}}, Order: []string{"lane_build_kind"}}
			old := platform.Stdout
			var out bytes.Buffer
			platform.Stdout = &out
			CmdProbe(nil, ctx, env, t.TempDir())
			platform.Stdout = old
			var got struct {
				Reviewer *struct {
					Kind string `json:"kind"`
				} `json:"recommended_reviewer"`
			}
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if tc.want == "" && got.Reviewer != nil || tc.want != "" && (got.Reviewer == nil || got.Reviewer.Kind != tc.want) {
				t.Errorf("build=%s reviewer=%+v", tc.build, got.Reviewer)
			}
		}
	})
	t.Run(`// JS: "parity: setup --probe aggregate covers the own models (5 probed, 2 skipped_custom, no key leak)"`, func(t *testing.T) {
		golden := readProbeGoldens(t)["probe-custom"].Steps[0]
		bin := t.TempDir()
		env := probeAllFakeEnv(t, bin)
		env["HERDR_SOHO_PROBE_TIMEOUT"] = "2"
		for _, kind := range []string{"pi", "codex", "claude", "grok"} {
			if _, err := fakecli.Install(t, bin, kind, []fakecli.Rule{{AnyArgs: true, Stdout: "ok\n"}}); err != nil {
				t.Fatal(err)
			}
		}
		modelsPath := filepath.Join(env.Get("HOME"), ".pi", "agent", "models.json")
		if err := os.MkdirAll(filepath.Dir(modelsPath), 0o700); err != nil {
			t.Fatal(err)
		}
		models := `{"providers":{"own":{"apiKey":"CUSTOMSECRET","models":[{"id":"m1"},{"id":"m2"},{"id":"m3"},{"id":"m4"},{"id":"m5"},{"id":"m6"},{"id":"m7"}]}}}`
		if err := os.WriteFile(modelsPath, []byte(models), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx := &core.Config{Entries: map[string]core.ConfigEntry{
			"model_claude_worker": {Value: "opus", Source: "defaults"},
			"model_codex_worker":  {Value: "sol|gpt-5", Source: "defaults"},
			"model_grok_worker":   {Value: "grok", Source: "defaults"},
			"model_agy_worker":    {Value: "gemini|opus", Source: "defaults"},
			"model_cursor_worker": {Value: "grok|muse", Source: "defaults"},
			"lane_build_kind":     {Value: "grok", Source: "project"},
		}, Order: []string{"lane_build_kind", "model_claude_worker", "model_codex_worker", "model_grok_worker", "model_agy_worker", "model_cursor_worker"}}
		runProbeGoldenStep(t, golden, env, ctx)
	})
}
