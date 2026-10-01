package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestLayoutPlanFixture(t *testing.T) {
	t.Run(`Go: layout plan reports the largest candidate and grid for a fixture`, func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "layout.json")
		raw := `{"result":{"layout":{"area":{"width":100,"height":100},"panes":[{"pane_id":"C","rect":{"x":0,"y":0,"width":50,"height":100}},{"pane_id":"A","rect":{"x":50,"y":0,"width":50,"height":100}}]}}}`
		if err := os.WriteFile(file, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{"HERDR_SOHO_SPLIT_MAX_PANES": "6", "HERDR_SOHO_SKILL_DIR": testSkillDir(t)}
		ctx := core.LoadConfig(env, dir)
		old := platform.Stdout
		var out bytes.Buffer
		platform.Stdout = &out
		defer func() { platform.Stdout = old }()
		if code := cmdLayoutPlan([]string{"--layout", file, "--me", "C", "--mine", "A"}, &ctx, env, dir); code != 0 {
			t.Fatalf("exit code=%d", code)
		}
		want := `{"placement":"split","anchor":"A","direction":"down","reason":"largest-area","cap":6,"min_pane":0.18,"candidates":[{"pane_id":"C","caller":true,"width":0.5,"height":1},{"pane_id":"A","caller":false,"width":0.5,"height":1}],"grid":{"cells":3,"cols":2,"rows_per_col":[1,2]}}` + "\n"
		if out.String() != want {
			t.Fatalf("output=%q, want %q", out.String(), want)
		}
	})
}

func TestLayoutPlanFullFixtureMatrix(t *testing.T) {
	t.Run(`layout-plan over a fixture: default cap full, cap 6 split, full grid`, func(t *testing.T) { // JS: "layout-plan over a fixture: default cap full, cap 6 split, full grid"
		// Mutation captured: changing cap handling or grid overflow changes one of these three fixture plans.
		const grid = `{"result":{"layout":{"area":{"x":0,"y":0,"width":213,"height":57},"panes":[{"pane_id":"C","rect":{"x":0,"y":0,"width":71,"height":57}},{"pane_id":"A","rect":{"x":71,"y":0,"width":71,"height":29}},{"pane_id":"B","rect":{"x":71,"y":29,"width":71,"height":28}},{"pane_id":"D","rect":{"x":142,"y":0,"width":71,"height":29}},{"pane_id":"E","rect":{"x":142,"y":29,"width":71,"height":28}}]}}}`
		const full = `{"result":{"layout":{"area":{"x":0,"y":0,"width":213,"height":57},"panes":[{"pane_id":"C","rect":{"x":0,"y":0,"width":71,"height":29}},{"pane_id":"F","rect":{"x":0,"y":29,"width":71,"height":28}},{"pane_id":"A","rect":{"x":71,"y":0,"width":71,"height":29}},{"pane_id":"B","rect":{"x":71,"y":29,"width":71,"height":28}},{"pane_id":"D","rect":{"x":142,"y":0,"width":71,"height":29}},{"pane_id":"E","rect":{"x":142,"y":29,"width":71,"height":28}}]}}}`
		run := func(raw, cap, mine string) string {
			t.Helper()
			file := filepath.Join(t.TempDir(), "layout.json")
			if err := os.WriteFile(file, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			env := platform.Env{"HERDR_SOHO_SKILL_DIR": testSkillDir(t)}
			if cap != "" {
				env["HERDR_SOHO_SPLIT_MAX_PANES"] = cap
			}
			ctx := core.LoadConfig(env, filepath.Dir(file))
			old := platform.Stdout
			var out bytes.Buffer
			platform.Stdout = &out
			defer func() { platform.Stdout = old }()
			if code := cmdLayoutPlan([]string{"--layout", file, "--me", "C", "--mine", mine}, &ctx, env, filepath.Dir(file)); code != 0 {
				t.Fatalf("exit code=%d", code)
			}
			return out.String()
		}
		if got := run(grid, "", "A B D"); !strings.Contains(got, `"placement":"herd"`) || !strings.Contains(got, `"reason":"full","cap":4`) {
			t.Fatalf("default plan=%q", got)
		}
		if got := run(grid, "6", "A B D E"); !strings.Contains(got, `"placement":"split","anchor":"C","direction":"down","reason":"largest-area","cap":6`) || !strings.Contains(got, `"grid":{"cells":6,"cols":3,"rows_per_col":[2,2,2]}`) {
			t.Fatalf("cap-six plan=%q", got)
		}
		if got := run(full, "6", "A B D E F"); !strings.Contains(got, `"placement":"herd"`) || !strings.Contains(got, `"reason":"full","cap":6`) || !strings.Contains(got, `"grid":null`) {
			t.Fatalf("full-grid plan=%q", got)
		}
	})
}

func TestLayoutPlanRejectsMalformedLayoutDocument(t *testing.T) {
	t.Run(`layout-plan: a --layout document that is not JSON exits 2 with a message`, func(t *testing.T) { // JS: "layout-plan: a --layout document that is not JSON exits 2 with a message"
		dir := t.TempDir()
		file := filepath.Join(dir, "layout.json")
		if err := os.WriteFile(file, []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		skill := testSkillDir(t)
		oldCwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chdir(oldCwd); err != nil {
				t.Errorf("restore cwd: %v", err)
			}
		})
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HOME": t.TempDir(), "PATH": t.TempDir()}
		oldOut, oldErr := platform.Stdout, platform.Stderr
		var out, errOut bytes.Buffer
		platform.Stdout, platform.Stderr = &out, &errOut
		t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
		if code := Run([]string{"layout-plan", "--layout", file, "--me", "caller"}, env); code != 2 || out.Len() != 0 || errOut.Len() == 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
	})
}

func TestLayoutPlanJSBoundaryCases(t *testing.T) {
	// Mutation captured: discarding json.Marshal errors blanks output when inferred dimensions are valid.
	// Mutation captured: using encoding/json escapes '<' differently from JSON.stringify.
	cases := []struct {
		name string
		raw  string
		me   string
		mine string
		want string
	}{
		{name: "infer missing area", raw: `{"result":{"layout":{"panes":[{"pane_id":"caller","rect":{"x":0,"y":0,"width":80,"height":40}}]}}}`, me: "caller", want: `"candidates":[{"pane_id":"caller","caller":true,"width":1,"height":1}]`},
		{name: "skip missing rectangle", raw: `{"result":{"layout":{"area":{"width":100,"height":100},"panes":[{"pane_id":"caller","rect":{}}]}}}`, me: "caller", want: `"candidates":[],"grid":{"cells":1,"cols":1,"rows_per_col":[1]}`},
		{name: "coerce JS numeric strings and skip Infinity", raw: `{"result":{"layout":{"area":{"width":100,"height":100},"panes":[{"pane_id":"caller","rect":{"width":"50","height":100}},{"pane_id":"worker","rect":{"width":"Infinity","height":100}}]}}}`, me: "caller", mine: "worker", want: `"candidates":[{"pane_id":"caller","caller":true,"width":0.5,"height":1}]`},
		{name: "JSON.stringify leaves angle brackets", raw: `{"result":{"layout":{"area":{"width":100,"height":100},"panes":[{"pane_id":"a<b","rect":{"x":0,"y":0,"width":100,"height":100}}]}}}`, me: "a<b", want: `"anchor":"a<b"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "layout.json")
			if err := os.WriteFile(file, []byte(tc.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			env := platform.Env{"HERDR_SOHO_SKILL_DIR": testSkillDir(t)}
			ctx := core.LoadConfig(env, dir)
			old := platform.Stdout
			var out bytes.Buffer
			platform.Stdout = &out
			defer func() { platform.Stdout = old }()
			args := []string{"--layout", file, "--me", tc.me}
			if tc.mine != "" {
				args = append(args, "--mine", tc.mine)
			}
			if code := cmdLayoutPlan(args, &ctx, env, dir); code != 0 {
				t.Fatalf("exit code=%d", code)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("output=%q, want substring %q", out.String(), tc.want)
			}
		})
	}
}

func TestParityLayoutPlanSixFixturesByteForByte(t *testing.T) {
	// JS: "parity: layout-plan --layout (six fixtures, byte for byte)"
	area := `"area":{"x":0,"y":0,"width":213,"height":57}`
	pane := func(id string, x, y, w, h int) string {
		return `{"pane_id":"` + id + `","rect":{"x":` + fmt.Sprint(x) + `,"y":` + fmt.Sprint(y) + `,"width":` + fmt.Sprint(w) + `,"height":` + fmt.Sprint(h) + `}}`
	}
	doc := func(panes ...string) string {
		return `{"result":{"layout":{` + area + `,"panes":[` + strings.Join(panes, ",") + `]}}}`
	}
	fixtures := []struct {
		name, raw, mine, cap string
	}{
		{"fx-vazio", doc(), "A B D", ""},
		{"fx-caller", doc(pane("C", 0, 0, 213, 57)), "", ""},
		{"fx-cheio", doc(pane("C", 0, 0, 71, 29), pane("F", 0, 29, 71, 28), pane("A", 71, 0, 71, 29), pane("B", 71, 29, 71, 28), pane("D", 142, 0, 71, 29), pane("E", 142, 29, 71, 28)), "A B D E F", ""},
		{"fx-min", doc(pane("C", 0, 0, 71, 19), pane("A", 0, 19, 71, 19), pane("B", 0, 38, 71, 19), pane("D", 71, 0, 71, 19), pane("E", 71, 19, 71, 19), pane("F", 71, 38, 71, 19), pane("G", 142, 0, 71, 19), pane("H", 142, 19, 71, 19), pane("I", 142, 38, 71, 19)), "A B D E F G H I", "12"},
		{"fx-empate", doc(pane("C", 0, 0, 107, 57), pane("A", 107, 0, 106, 57)), "A", ""},
		{"fx-3x2", doc(pane("C", 0, 0, 71, 57), pane("A", 71, 0, 71, 29), pane("B", 71, 29, 71, 28), pane("D", 142, 0, 71, 29), pane("E", 142, 29, 71, 28)), "A B D E", "6"},
	}
	goldenPath := filepath.Join(testSkillDir(t), "scripts", "test", "golden", "parity-layout.json")
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Layout struct {
			Steps []struct {
				Out string `json:"out"`
				RC  int    `json:"rc"`
			} `json:"steps"`
		} `json:"layout-plan"`
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden.Layout.Steps) != len(fixtures) {
		t.Fatalf("golden steps=%d, fixtures=%d", len(golden.Layout.Steps), len(fixtures))
	}
	for i, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), fixture.name+".json")
			if err := os.WriteFile(file, []byte(fixture.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			env := platform.Env{"HERDR_SOHO_SKILL_DIR": testSkillDir(t)}
			if fixture.cap != "" {
				env["HERDR_SOHO_SPLIT_MAX_PANES"] = fixture.cap
			}
			oldOut, oldErr := platform.Stdout, platform.Stderr
			var out, stderr bytes.Buffer
			platform.Stdout, platform.Stderr = &out, &stderr
			t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
			code := Run([]string{"layout-plan", "--layout", file, "--me", "C", "--mine", fixture.mine}, env)
			want := golden.Layout.Steps[i]
			if code != want.RC || out.String() != want.Out || stderr.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q; want code=%d stdout=%q", code, out.String(), stderr.String(), want.RC, want.Out)
			}
		})
	}
}

func TestLayoutPlanFromInsideTheSkillGitCheckout(t *testing.T) {
	// Mutation captured: StateRoot used to append the state dir to the
	// .gitignore of the git checkout the cwd resolved to; run from inside a
	// skill that is a git checkout, layout-plan would create or change the
	// skill's own .gitignore (StateDirPath resolves the state path through
	// StateRoot even though layout-plan writes nothing else).
	root := t.TempDir()
	skill := filepath.Join(root, "skill")
	if err := os.MkdirAll(filepath.Join(skill, "roles"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# herdr-soho\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(skill); err == nil {
		skill = resolved // git reports the resolved spelling (macOS /tmp)
	}
	if out, gitErr := exec.Command("git", "init", "-q", skill).CombinedOutput(); gitErr != nil {
		t.Fatalf("git init: %s %v", out, gitErr)
	}
	const seeded = "# pre-existing\n"
	if err := os.WriteFile(filepath.Join(skill, ".gitignore"), []byte(seeded), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	calls := filepath.Join(root, "herdr-calls.log")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	const layoutJSON = `{"result":{"layout":{"area":{"width":100,"height":100},"panes":[{"pane_id":"C","rect":{"x":0,"y":0,"width":50,"height":100}},{"pane_id":"A","rect":{"x":50,"y":0,"width":50,"height":100}}]}}}`
	script := "#!/bin/sh\necho \"$@\" >> " + calls + "\nif [ \"$1\" = pane ] && [ \"$2\" = layout ]; then printf '%s' '" + layoutJSON + "'; exit 0; fi\nexit 3\n"
	if err := os.WriteFile(filepath.Join(bin, "herdr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(skill); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldCwd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
	env := platform.Env{
		"HERDR_ENV": "1", "HERDR_PANE_ID": "C", "HERDR_WORKSPACE_ID": "ws",
		"HERDR_SOHO_SKILL_DIR": skill, "HOME": t.TempDir(),
		"PATH": bin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, errOut bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &errOut
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	if code := Run([]string{"layout-plan"}, env); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `"placement":"split","anchor":"C"`) {
		t.Fatalf("layout-plan did not proceed: %q", out.String())
	}
	logData, err := os.ReadFile(calls)
	if err != nil || !strings.Contains(string(logData), "pane layout") {
		t.Fatalf("pane layout was not requested: %q err=%v", logData, err)
	}
	got, err := os.ReadFile(filepath.Join(skill, ".gitignore"))
	if err != nil || string(got) != seeded {
		t.Fatalf("layout-plan changed the skill .gitignore: %q err=%v", got, err)
	}
}
