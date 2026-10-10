package setup

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/kinds"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func tm5DetectFixture(t *testing.T) (string, string, platform.Env, *core.Config) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "conf")
	tmp := filepath.Join(root, "tmp")
	for _, dir := range []string{home, conf, tmp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := platform.Env{"HOME": home, "USERPROFILE": home, "XDG_CONFIG_HOME": conf, "TMPDIR": tmp, "HERDR_SOHO_DIR": filepath.Join(root, "state"), "HERDR_WORKSPACE_ID": "ws", "PATH": "", "HERDR_SOHO_SKILL_DIR": filepath.Join("..", "..", "skills", "herdr-soho")}
	ctx := &core.Config{Entries: map[string]core.ConfigEntry{}, Order: []string{}}
	return root, home, env, ctx
}

func tm5DetectDoc(t *testing.T, ctx *core.Config, env platform.Env, cwd string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(DetectJSON(ctx, env, cwd)), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func tm5DetectKind(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	for _, value := range doc["kinds"].([]any) {
		row := value.(map[string]any)
		if row["kind"] == name {
			return row
		}
	}
	t.Fatalf("kind %q missing", name)
	return nil
}

func TestSetupDetectTM5OwnProviderCases(t *testing.T) {
	t.Run(`// JS: "piCustomModelsJson: provider/model ids + max declared level, no secrets (test-detect-custom.sh)"`, func(t *testing.T) {
		root, home, env, ctx := tm5DetectFixture(t)
		file := filepath.Join(home, ".pi", "agent", "models.json")
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		data := `{"providers":{"own":{"apiKey":"SENTINEL_SECRET","models":[{"id":"m1","thinkingLevelMap":{"low":true,"high":true,"max":true}},{"id":"m2","thinkingLevelMap":{"low":true}},{"id":"m3"}]}}}`
		if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		doc := tm5DetectDoc(t, ctx, env, root)
		encoded, _ := json.Marshal(doc)
		models := tm5DetectKind(t, doc, "pi")["custom_models"].([]any)
		if strings.Contains(string(encoded), "SENTINEL_SECRET") || len(models) != 3 {
			t.Fatalf("custom models=%v output leaks secret=%t", models, strings.Contains(string(encoded), "SENTINEL_SECRET"))
		}
		first := models[0].(map[string]any)
		if first["id"] != "own/m1" || first["max_effort"] != "max" {
			t.Fatalf("first model=%v", first)
		}
	})
	t.Run(`// JS: "piCustomModelsJson: missing file, no thinkingLevelMap, partial map, CRLF"`, func(t *testing.T) {
		root, home, env, ctx := tm5DetectFixture(t)
		if got := tm5DetectKind(t, tm5DetectDoc(t, ctx, env, root), "pi")["custom_models"].([]any); len(got) != 0 {
			t.Fatalf("missing file models=%v", got)
		}
		file := filepath.Join(home, ".pi", "agent", "models.json")
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("{\r\n\"providers\":{\"own\":{\"models\":[{\"id\":\"plain\"},{\"id\":\"partial\",\"thinkingLevelMap\":{\"low\":true,\"made-up\":true}}]}}}\r\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		models := tm5DetectKind(t, tm5DetectDoc(t, ctx, env, root), "pi")["custom_models"].([]any)
		if len(models) != 2 || models[0].(map[string]any)["max_effort"] != "" || models[1].(map[string]any)["max_effort"] != "low" {
			t.Fatalf("CRLF/partial models=%v", models)
		}
	})
	t.Run(`// JS: "piCustomModelsJson: malformed files and bad shapes degrade to []"`, func(t *testing.T) {
		root, home, env, ctx := tm5DetectFixture(t)
		file := filepath.Join(home, ".pi", "agent", "models.json")
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"not json", `[]`, `{"providers":[]}`, `{"providers":{"p":{"models":{}}}}`, `{"providers":{"p":{"models":[null]}}}`} {
			if err := os.WriteFile(file, []byte(bad), 0o600); err != nil {
				t.Fatal(err)
			}
			models := tm5DetectKind(t, tm5DetectDoc(t, ctx, env, root), "pi")["custom_models"].([]any)
			if len(models) != 0 {
				t.Errorf("input %q produced %v", bad, models)
			}
		}
	})
	t.Run(`// JS: "opencodeCustomModelsJson: project + $OPENCODE_CONFIG + user, project wins, no secrets (test-detect-custom.sh)"`, func(t *testing.T) {
		root, home, env, ctx := tm5DetectFixture(t)
		write := func(path, body string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		write(filepath.Join(root, "opencode.json"), `{"provider":{"p":{"options":{"apiKey":"SECRET"},"models":{"project":{},"same":{}}}}}`)
		custom := filepath.Join(t.TempDir(), "open-code-env.json")
		write(custom, `{"provider":{"p":{"models":{"same":{},"env":{}}}}}`)
		env["OPENCODE_CONFIG"] = custom
		xdg := filepath.Join(home, ".config")
		env["XDG_CONFIG_HOME"] = xdg
		write(filepath.Join(xdg, "opencode", "opencode.json"), `{"provider":{"p":{"models":{"user":{}}}}}`)
		doc := tm5DetectDoc(t, ctx, env, root)
		encoded, _ := json.Marshal(doc)
		models := tm5DetectKind(t, doc, "opencode")["custom_models"].([]any)
		ids := []string{}
		for _, v := range models {
			ids = append(ids, v.(map[string]any)["id"].(string))
		}
		if strings.Contains(string(encoded), "SECRET") || !reflect.DeepEqual(ids, []string{"p/project", "p/same", "p/env", "p/user"}) {
			t.Fatalf("ids=%v secret=%t", ids, strings.Contains(string(encoded), "SECRET"))
		}
	})
	t.Run(`// JS: "opencodeCustomModelsJson: no files, malformed files, CRLF, missing provider sections"`, func(t *testing.T) {
		root, home, env, ctx := tm5DetectFixture(t)
		if got := tm5DetectKind(t, tm5DetectDoc(t, ctx, env, root), "opencode")["custom_models"].([]any); len(got) != 0 {
			t.Fatalf("missing files=%v", got)
		}
		file := filepath.Join(root, "opencode.json")
		for _, body := range []string{"not json", "{\r\n\"provider\":{\"p\":{\"models\":{}}}\r\n}", `{"providers":{"p":{"models":{"m":{}}}}}`, `{"provider":[]}`} {
			if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			got := tm5DetectKind(t, tm5DetectDoc(t, ctx, env, root), "opencode")["custom_models"].([]any)
			if len(got) != 0 {
				t.Errorf("%q => %v", body, got)
			}
		}
		_ = home
	})
	t.Run(`// JS: "piCustomModelsJson: a thinkingLevelMap key named like an Object.prototype member is an unknown level"`, func(t *testing.T) {
		_, home, env, _ := tm5DetectFixture(t)
		file := filepath.Join(home, ".pi", "agent", "models.json")
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(`{"providers":{"p":{"models":[{"id":"m","thinkingLevelMap":{"toString":true}}]}}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		models := kinds.PiOwnModels(env)
		if len(models) != 1 || models[0].MaxEffort != "" {
			t.Fatalf("prototype key treated as a level: %+v", models)
		}
	})
	t.Run(`// JS: "piOwnModels: the trap warnings per model (literal key, headroom, defaults, effort.pi)"`, func(t *testing.T) {
		_, home, env, ctx := tm5DetectFixture(t)
		env["HERDR_SOHO_EFFORT_PI"] = "medium"
		if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".pi", "agent", "models.json"), []byte(`{"providers":{"p":{"apiKey":"secret","models":[{"id":"small","maxTokens":10000},{"id":"large","maxTokens":20000}]}}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		got := kinds.PiOwnModelsWithConfig(ctx, env)
		if len(got) != 2 || len(got[0].Warnings) != 2 || len(got[1].Warnings) != 1 || !strings.Contains(strings.Join(got[0].Warnings, " "), "8192") || !strings.Contains(strings.Join(got[0].Warnings, " "), "literal apiKey") {
			t.Fatalf("warnings=%+v", got)
		}
	})
	t.Run(`// JS: "opencodeOwnModels: the trap warnings per model (literal key, missing budget, first declaration wins)"`, func(t *testing.T) {
		root, _, env, _ := tm5DetectFixture(t)
		write := func(path, data string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		write(filepath.Join(root, "opencode.json"), `{"provider":{"p":{"options":{"apiKey":"literal"},"models":{"m":{"options":{"thinking_token_budget":12000}},"missing":{}}}}}`)
		write(filepath.Join(env.Get("XDG_CONFIG_HOME"), "opencode", "opencode.json"), `{"provider":{"p":{"models":{"m":{},"later":{}}}}}`)
		got := kinds.OpencodeOwnModels(env, root)
		if len(got) != 3 || got[0].ID != "p/m" || len(got[0].Warnings) != 1 || strings.Contains(strings.Join(got[0].Warnings, " "), "budget") || len(got[1].Warnings) != 2 || got[2].ID != "p/later" {
			t.Fatalf("models=%+v", got)
		}
	})
}

func TestSetupDetectTM5PolicyCases(t *testing.T) {
	t.Run(`// JS: "recommendReviewerJson: build of each family; not-installed kinds are not candidates"`, func(t *testing.T) {
		_, _, env, ctx := tm5DetectFixture(t)
		bin := t.TempDir()
		for _, exe := range []string{"claude", "codex", "grok", "agy"} {
			if _, err := fakecli.Install(t, bin, exe, []fakecli.Rule{{AnyArgs: true}}); err != nil {
				t.Fatal(err)
			}
		}
		for _, item := range fakecli.Env(env.List(), bin) {
			key, value, ok := strings.Cut(item, "=")
			if ok {
				env[key] = value
			}
		}
		if runtime.GOOS == "windows" {
			env["PATHEXT"] = ".EXE;.CMD;.BAT;.COM"
		}
		for _, pair := range []struct{ build, want string }{{"claude", "codex"}, {"codex", "claude"}, {"grok", "codex"}, {"agy", "codex"}} {
			ctx.Entries["lane_build_kind"] = core.ConfigEntry{Value: pair.build, Source: "project"}
			cwd := t.TempDir()
			output := DetectJSON(ctx, env, cwd)
			var doc map[string]any
			if err := json.Unmarshal([]byte(output), &doc); err != nil {
				t.Fatalf("build=%s detect exit=0; output was not JSON: %v\n%s", pair.build, err, output)
			}
			recommended, ok := doc["recommended_reviewer"].(map[string]any)
			if !ok {
				t.Fatalf("build=%s detect exit=0; recommended_reviewer missing or invalid; output:\n%s", pair.build, output)
			}
			rec := recommended
			if rec["kind"] != pair.want {
				t.Errorf("build=%s detect exit=0 reviewer=%v want=%s; output:\n%s", pair.build, rec, pair.want, output)
			}
		}
	})
	t.Run(`// JS: "effectiveBuildFamily: lane kind+model, role config, frontmatter, layer rule"`, func(t *testing.T) {
		_, _, env, ctx := tm5DetectFixture(t)
		for _, tc := range []struct{ lane, role, model, family string }{{"codex", "claude", "", "openai"}, {"", "claude", "", "anthropic"}, {"", "", "grok-4.7", "xai"}} {
			ctx.Entries["lane_build_kind"] = core.ConfigEntry{Value: tc.lane, Source: "project"}
			ctx.Entries["role_implementer_kind"] = core.ConfigEntry{Value: tc.role, Source: "user"}
			ctx.Entries["lane_build_model"] = core.ConfigEntry{Value: tc.model, Source: "project"}
			doc := tm5DetectDoc(t, ctx, env, t.TempDir())
			_ = doc
			kind := core.LaneAttr(ctx, "build", "kind", nil, env)
			if kind == "" {
				kind = core.ResolvedRoleKind("implementer", ctx, env, t.TempDir())
			}
			got := kinds.AgentFamily(kind, tc.model)
			if got != tc.family {
				t.Errorf("lane=%q role=%q model=%q family=%q want %q", tc.lane, tc.role, tc.model, got, tc.family)
			}
		}
	})
	t.Run(`// JS: "detectTopModels: HERDR_SOHO_MODELS_TIMEOUT is honored when set, else the 5 s default"`, func(t *testing.T) {
		root, _, env, ctx := tm5DetectFixture(t)
		bin := t.TempDir()
		if _, err := fakecli.Install(t, bin, "grok", []fakecli.Rule{{Argv: []string{"models"}, Stdout: "grok-4.7 - latest\n", Delay: 5200}}); err != nil {
			t.Fatal(err)
		}
		env = platform.Env{}
		for _, item := range fakecli.Env(testutil.CleanEnv(t), bin, fakecli.EnvOptions{SystemPath: "/usr/bin:/bin"}) {
			key, value, ok := strings.Cut(item, "=")
			if ok {
				env[key] = value
			}
		}
		home := t.TempDir()
		env["HOME"], env["USERPROFILE"], env["XDG_CONFIG_HOME"], env["TMPDIR"] = home, home, filepath.Join(home, ".config"), t.TempDir()
		env["HERDR_SOHO_SKILL_DIR"] = filepath.Join("..", "..", "skills", "herdr-soho")
		delete(env, "HERDR_SOHO_MODELS_TIMEOUT")
		started := time.Now()
		row := tm5DetectKind(t, tm5DetectDoc(t, ctx, env, root), "grok")
		models := row["models"].([]any)
		elapsed := time.Since(started)
		// A timeout longer than the fake's 5.2 s lets it answer, so models is
		// not empty; an upper bound on elapsed would only add load flakes.
		if len(models) != 0 || elapsed < 4900*time.Millisecond {
			t.Fatalf("default timeout models=%v elapsed=%v", models, elapsed)
		}
	})
	t.Run(`// JS: "detectRoleKindsJson: frontmatter, config overrides per layer, first dir wins"`, func(t *testing.T) {
		root, home, env, ctx := tm5DetectFixture(t)
		roleDir := filepath.Join(root, ".agents", "herdr-roles")
		if err := os.MkdirAll(roleDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(roleDir, "reviewer.md"), []byte("---\nkind: pi\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		extra := filepath.Join(root, "extra-roles")
		if err := os.MkdirAll(extra, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extra, "reviewer.md"), []byte("---\nkind: grok\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(roleDir, "scouter.md"), []byte("---\nkind: pi\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extra, "scouter.md"), []byte("---\nkind: grok\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		env["HERDR_SOHO_ROLES"] = extra
		if err := os.MkdirAll(filepath.Join(home, ".config", "herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".config", "herdr-soho", "config"), []byte("role.reviewer.kind=codex\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx.Entries["role_reviewer_kind"] = core.ConfigEntry{Value: "claude", Source: "project"}
		rows := tm5DetectDoc(t, ctx, env, root)["config"].(map[string]any)["role_kinds"].([]any)
		var reviewer map[string]any
		for _, row := range rows {
			if row.(map[string]any)["key"] == "role.reviewer.kind" {
				reviewer = row.(map[string]any)
				break
			}
		}
		var scouter map[string]any
		for _, row := range rows {
			if row.(map[string]any)["key"] == "role.scouter.kind" {
				scouter = row.(map[string]any)
			}
		}
		if reviewer == nil || reviewer["value"] != "claude" || reviewer["source"] != "project" || scouter == nil || scouter["value"] != "pi" || scouter["source"] != "role" {
			t.Fatalf("role kinds=%v", rows)
		}
	})
}

func TestSetupDetectTM5ParityGoldens(t *testing.T) {
	for _, scenario := range []struct{ name, title string }{
		{"detect-custom", `// JS: "parity: setup --detect with the custom provider files (test-detect-custom.sh)"`},
		{"detect-bad-pi", `// JS: "parity: setup --detect with a malformed pi models.json degrades to []"`},
		{"detect-bare", `// JS: "parity: setup --detect with no agent CLI on the PATH and no provider files"`},
		{"detect-lanes", `// JS: "parity: setup --detect with custom lanes"`},
		{"detect-layers", `// JS: "parity: setup --detect with role.<r>.kind and model.<kind>.worker in different layers"`},
		{"detect-oc-env", `// JS: "parity: setup --detect with the $OPENCODE_CONFIG file between project and user"`},
	} {
		t.Run(scenario.title, func(t *testing.T) { runDetectTM5Golden(t, scenario.name) })
	}
}

func runDetectTM5Golden(t *testing.T, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "legacy", "parity-setup-detect.json"))
	if err != nil {
		t.Fatal(err)
	}
	var goldens map[string]struct {
		Steps []struct {
			Out string `json:"out"`
			Err string `json:"err"`
			RC  int    `json:"rc"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(data, &goldens); err != nil {
		t.Fatal(err)
	}
	want, ok := goldens[name]
	if !ok || len(want.Steps) != 1 {
		t.Fatalf("golden %q missing or malformed", name)
	}
	fixture, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo, home, conf, tmp := filepath.Join(fixture, "repo"), filepath.Join(fixture, "home"), filepath.Join(fixture, "conf"), filepath.Join(fixture, "tmp")
	for _, dir := range []string{repo, home, conf, tmp, filepath.Join(repo, ".agents")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	bin := filepath.Join(fixture, "fakes")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	install := func(kind string, rules []fakecli.Rule) {
		t.Helper()
		if _, err := fakecli.Install(t, bin, kind, rules); err != nil {
			t.Fatal(err)
		}
	}
	listing := func(args []string, output string) []fakecli.Rule {
		return []fakecli.Rule{{Argv: args, Stdout: output}, {AnyArgs: true}}
	}
	grokOutput := "Available models:\ngrok-4.7 - xAI Grok 4.7 (default)\ngrok-4.7-build-fast - quick variant\ngrok-4.6 - older release\n"
	env := platform.Env{}
	for _, item := range fakecli.Env(testutil.CleanEnv(t), bin, fakecli.EnvOptions{SystemPath: "/usr/bin:/bin"}) {
		k, v, ok := strings.Cut(item, "=")
		if ok {
			env[k] = v
		}
	}
	env["HOME"], env["USERPROFILE"], env["XDG_CONFIG_HOME"], env["TMPDIR"] = home, home, conf, tmp
	env["HERDR_SOHO_DIR"], env["HERDR_WORKSPACE_ID"] = filepath.Join(fixture, "state"), "ws"
	env["HERDR_SOHO_SKILL_DIR"] = filepath.Join("..", "..", "skills", "herdr-soho")
	env["HERDR_SOHO_MODELS_TIMEOUT"], env["OPENCODE_CONFIG"] = "30", ""
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := core.LoadConfig(env, repo)
	switch name {
	case "detect-custom":
		install("pi", []fakecli.Rule{{AnyArgs: true}})
		install("grok", listing([]string{"models"}, "Available models:\ngrok-4.7 - xAI Grok 4.7 (default)\ngrok-4.7-build-fast - quick variant\ngrok-4.6 - older release\n"))
		install("cursor-agent", listing([]string{"--list-models"}, "grok-4.7-max - xAI\ngrok-4.7-high - xAI\ngrok-4.6 - xAI\nclaude-opus-4-8-max - Anthropic\n"))
		install("agy", listing([]string{"models"}, "gemini-3.8-flash  Google\ngemini-3.8-flash-high  Google\nclaude-opus-4-6  Anthropic\ngpt-oss-120b  OpenAI\n"))
		write(filepath.Join(home, ".pi", "agent", "models.json"), `{"providers":{"my-provider":{"apiKey":"sk-live-PISECRET987654321","models":[{"id":"my-model","thinkingLevelMap":{"off":null,"minimal":null,"low":"low","medium":null,"high":"high","xhigh":"xhigh","max":"max"}},{"id":"plain","thinkingLevelMap":null}]},"second":{"apiKey":"{env:SECOND_KEY}","models":[{"id":"cheap-fast"}]}}}`)
		write(filepath.Join(home, ".codex", "models_cache.json"), `{"models":[{"slug":"gpt-5"},{"slug":"gpt-5.1"},{"slug":"codex-astra"}]}`)
		write(filepath.Join(repo, "opencode.json"), `{"provider":{"proj-provider":{"models":{"proj-model":{}}},"shared":{"options":{"apiKey":"sk-test-OPENSECRET42"},"models":{"shared-model":{}}}}}`)
		write(filepath.Join(conf, "opencode", "opencode.json"), `{"provider":{"user-provider":{"options":{"apiKey":"sk-live-USERSECRET111"},"models":{"user-model":{}}}}}`)
	case "detect-bad-pi":
		install("pi", []fakecli.Rule{{AnyArgs: true}})
		install("grok", listing([]string{"models"}, grokOutput))
		write(filepath.Join(home, ".pi", "agent", "models.json"), "not json")
		write(filepath.Join(repo, "opencode.json"), `{"provider":{"proj-provider":{"options":{"apiKey":"{env:PROJ_KEY}"},"models":{"proj-model":{}}},"shared":{"options":{"apiKey":"sk-test-OPENSECRET42"},"models":{"shared-model":{}}}}}`)
		write(filepath.Join(conf, "opencode", "opencode.json"), `{"provider":{"user-provider":{"options":{"apiKey":"sk-live-USERSECRET111"},"models":{"user-model":{}}}}}`)
	case "detect-bare":
	case "detect-lanes":
		install("pi", []fakecli.Rule{{AnyArgs: true}})
		install("grok", listing([]string{"models"}, grokOutput))
		write(filepath.Join(repo, ".agents", "herdr-soho.conf"), "lane.build.roles=implementer,designer\nlane.build.kind=codex\nlane.review.roles=reviewer\n")
	case "detect-layers":
		install("pi", []fakecli.Rule{{AnyArgs: true}})
		install("grok", listing([]string{"models"}, grokOutput))
		write(filepath.Join(conf, "herdr-soho", "config"), "role.reviewer.kind=codex\n")
		write(filepath.Join(repo, ".agents", "herdr-soho.conf"), "role.implementer.kind=codex\nmodel.pi.worker=custom/pi-model\n")
		env["HERDR_SOHO_MODEL_GROK_WORKER"] = "grok-4.7"
	case "detect-oc-env":
		env["OPENCODE_CONFIG"] = filepath.Join(fixture, "oc-env.json")
		write(filepath.Join(repo, "opencode.json"), `{"provider":{"pp":{"models":{"pm":{},"dup":{}}},"sp":{"models":{"sm":{}}}}}`)
		write(env.Get("OPENCODE_CONFIG"), `{"provider":{"pp":{"options":{"apiKey":"sk-live-ENVSECRET555"},"models":{"dup":{},"em":{}}}}}`)
		write(filepath.Join(conf, "opencode", "opencode.json"), `{"provider":{"pp":{"models":{"dup":{},"xm":{}}}}}`)
	}
	ctx = core.LoadConfig(env, repo)
	got := DetectJSON(&ctx, env, repo) + "\n"
	got = normalizeDetectGoldenText(got, fixture)
	// The frozen golden predates the shipped job-orchestrator role; the
	// exact-insertion role oracle adapts the expected role_kinds entry.
	wantOut := testutil.ApplyRoleOracleToDetectDocument(want.Steps[0].Out)
	if want.Steps[0].RC != 0 || want.Steps[0].Err != "" || got != wantOut {
		line := firstDifferentLine(got, wantOut)
		t.Fatalf("scenario=%s detect differs at line %d\nGo:     %s\ngolden: %s", name, line, lineAt(got, line), lineAt(wantOut, line))
	}
}

func normalizeDetectGoldenText(value, root string) string {
	for _, path := range []string{root, filepath.ToSlash(root)} {
		value = strings.ReplaceAll(value, strings.ReplaceAll(path, `\`, `\\`), "<ROOT>")
		value = strings.ReplaceAll(value, path, "<ROOT>")
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, `\\`, "/")
}

func firstDifferentLine(a, b string) int {
	aa, bb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(aa) && i < len(bb); i++ {
		if aa[i] != bb[i] {
			return i + 1
		}
	}
	return min(len(aa), len(bb)) + 1
}
func lineAt(s string, line int) string {
	xs := strings.Split(s, "\n")
	if line < 1 || line > len(xs) {
		return "<eof>"
	}
	return xs[line-1]
}
