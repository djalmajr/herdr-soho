package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

type laneGolden struct {
	Out  string `json:"out"`
	Err  string `json:"err"`
	RC   int    `json:"rc"`
	File string `json:"file"`
}

func TestParityLanesGolden(t *testing.T) {
	// JS: "parity: lane_names / lane_of_role / max_workers matrix"
	data, err := os.ReadFile(filepath.Join("..", "testdata", "legacy", "parity-lanes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]laneGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	type scenario struct {
		name, project, user string
		env                 map[string]string
	}
	roles := []string{"implementer", "designer", "tasker", "scouter", "researcher", "reviewer", "security-reviewer", "ui-reviewer", "inspector", "sub-orchestrator"}
	cases := []scenario{
		{name: "preset-4"}, {name: "preset-3", project: "panes=3\n"}, {name: "preset-3-env", env: map[string]string{"HERDR_SOHO_PANES": "3"}},
		{name: "custom-file", project: "panes=4\nlane.ops.roles=implementer,tasker\n"}, {name: "custom-env", env: map[string]string{"HERDR_SOHO_LANE_OPS_ROLES": "implementer,tasker"}},
		{name: "hyphen-file-key", project: "lane.ui-review.roles=ui-reviewer,inspector\n"}, {name: "hyphen-env-invisible", env: map[string]string{"HERDR_SOHO_LANE_UI-REVIEW_ROLES": "ui-reviewer,inspector"}},
		{name: "hyphen-env-plus-file", project: "lane.ui-review.roles=ui-reviewer,inspector\n", env: map[string]string{"HERDR_SOHO_LANE_UI-REVIEW_ROLES": "ui-reviewer,inspector"}},
		{name: "mixed-file-env", project: "lane.a.roles=scouter\n", env: map[string]string{"HERDR_SOHO_LANE_B_ROLES": "researcher"}}, {name: "lanes-off", env: map[string]string{"HERDR_SOHO_LANES": "off"}},
		{name: "explicit-max", project: "max_workers=5\n"}, {name: "explicit-max-env", env: map[string]string{"HERDR_SOHO_MAX_WORKERS": "0"}},
		{name: "empty-roles", project: "lane.empty.roles=\nlane.ops.roles=implementer\n", env: map[string]string{"HERDR_SOHO_LANE_B_ROLES": "researcher"}},
		{name: "case-order", project: "lane.A.roles=reviewer\nlane.b.roles=scouter\n"},
		{name: "user-layer", user: "panes=3\nlane.u.roles=implementer\n"}, {name: "user-and-project", user: "lane.u.roles=implementer\n", project: "lane.p.roles=tasker\n"},
		{name: "space-separated-roles", project: "lane.x.roles=reviewer inspector\n"}, {name: "space-separated-env", env: map[string]string{"HERDR_SOHO_LANE_X_ROLES": "scouter  researcher,reviewer"}},
		{name: "empty-lane-name", project: "lane..roles=reviewer\nlane.y.roles=scouter\n"},
	}
	for _, sc := range cases {
		t.Run(sc.name, func(t *testing.T) {
			// Mutation captured: changing custom-lane precedence, role splitting, pane presets, or the derived worker cap changes the golden stdout.
			env, repo := fixture(t)
			if sc.project != "" {
				write(t, filepath.Join(repo, ".agents", "herdr-soho.conf"), sc.project)
			}
			if sc.user != "" {
				write(t, filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho", "config"), sc.user)
			}
			for k, v := range sc.env {
				env[k] = v
			}
			ctx := LoadConfig(env, repo)
			lines := []string{}
			for _, lane := range LaneNames(&ctx, env) {
				lines = append(lines, lane)
			}
			for _, role := range roles {
				lane := LaneOfRole(&ctx, role, env)
				if lane == "" {
					lane = "NONE"
				}
				lines = append(lines, "ROLE "+role+" "+lane)
			}
			lines = append(lines, "COUNT "+strconv.Itoa(LaneCount(&ctx, env)), "MAXWORKERS "+MaxWorkers(&ctx, env))
			got := strings.Join(lines, "\n") + "\n"
			want, ok := golden[sc.name]
			if !ok {
				t.Fatalf("scenario %q missing from parity-lanes golden", sc.name)
			}
			if want.RC != 0 || want.Err != "" {
				t.Fatalf("golden scenario status unexpected: %#v", want)
			}
			if got != want.Out {
				t.Fatalf("lane probe differs from JS golden\ngot:\n%s\nwant:\n%s", got, want.Out)
			}
		})
	}
}

func TestSetupLaneSpecGolden(t *testing.T) {
	// JS: "parity: setup_lane_spec over valid and rejected specs"
	data, err := os.ReadFile(filepath.Join("..", "testdata", "legacy", "parity-lanes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]laneGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	specs := []string{"build=codex", "build=codex:gpt-6:high", "x=claude:m:high::", "x=claude:::", "x=claude:m:high:e:", "x=claude\n:m", "build", "Build=codex", "build=nope", "build=codex:m:huge", "build=codex:g#x"}
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			// Mutation captured: accepting a malformed --lane value changes status/error; dropping a field changes the TSV output.
			want, ok := golden[spec]
			if !ok {
				t.Fatalf("%q missing from golden", spec)
			}
			got, err := SetupLaneSpec(spec)
			if err != nil {
				if want.RC == 0 {
					t.Fatalf("SetupLaneSpec(%q): %v; golden=%#v", spec, err, want)
				}
				exit, ok := err.(*platform.ExitError)
				if !ok || exit.Code != want.RC {
					t.Fatalf("error=%#v, golden=%#v", err, want)
				}
				msg := strings.TrimPrefix(strings.TrimSuffix(want.Err, "\n"), "PROG: ")
				if exit.Msg != msg {
					t.Fatalf("message=%q, want=%q", exit.Msg, msg)
				}
				return
			}
			if want.RC != 0 {
				t.Fatalf("accepted %q but golden rc=%d", spec, want.RC)
			}
			out := strings.Join([]string{got.Name, got.Kind, got.Model, got.Effort}, "\t") + "\n"
			if out != want.Out {
				t.Fatalf("stdout=%q want=%q", out, want.Out)
			}
		})
	}
}

func TestApplyLaneFileGolden(t *testing.T) {
	// JS: "parity: apply_lane_file (nine seeds: bytes, modes, printed lines, warnings, no temps)"
	data, err := os.ReadFile(filepath.Join("..", "testdata", "legacy", "parity-lanes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]laneGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	type seed struct{ name, panes, file string }
	seeds := []seed{{"empty", "4", ""}, {"preset3-to-4", "4", "lane.build.roles=implementer,designer,tasker\nlane.read.roles=scouter,researcher,reviewer,security-reviewer,ui-reviewer,inspector\n"}, {"preset4-to-3", "3", "lane.build.roles=implementer,designer,tasker\nlane.explore.roles=scouter,researcher\nlane.review.roles=reviewer,security-reviewer,ui-reviewer,inspector\n"}, {"custom", "4", "lane.ops.roles=implementer,tasker\n"}, {"unanimous-kind", "4", "role.implementer.kind=grok\nrole.designer.kind=grok\nrole.tasker.kind=grok\n"}, {"divergent-kind", "4", "role.implementer.kind=grok\nrole.designer.kind=agy\n"}, {"planner-keys", "4", "role.planner.kind=codex\nrole.planner.model=gpt-5\nrole.implementer.kind=grok\nrole.designer.kind=grok\nrole.tasker.kind=grok\n"}, {"existing-lane-kind", "4", "lane.build.kind=codex\nrole.implementer.kind=grok\nrole.designer.kind=grok\nrole.tasker.kind=grok\n"}, {"unanimous-model", "4", "lane.build.kind=agy\nrole.implementer.model=m1\nrole.designer.model=m1\nrole.tasker.model=m1\n"}, {"custom-space-roles", "4", "lane.x.roles=implementer designer\nrole.implementer.kind=grok\nrole.designer.kind=grok\n"}}
	for _, sc := range seeds {
		t.Run(sc.name, func(t *testing.T) {
			// Mutation captured: changing preset recognition or rewriting one key differently changes the stored file/output/warning contract.
			want, ok := golden[sc.name]
			if !ok {
				t.Fatalf("%q missing from golden", sc.name)
			}
			env, repo := fixture(t)
			root := filepath.Dir(repo)
			dest := filepath.Join(root, "dest", "herdr-soho.conf")
			if sc.file != "" {
				write(t, dest, sc.file)
			} else if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			oldErr := platform.Stderr
			platform.Stderr = &stderr
			lines := ApplyLaneFile(dest, sc.panes, env, repo)
			platform.Stderr = oldErr
			out := ""
			if len(lines) > 0 {
				out = strings.Join(lines, "\n") + "\n"
			}
			errText := strings.ReplaceAll(stderr.String(), root, "<ROOT>")
			errText = normalizeGoldenRootPathSeparators(errText)
			errText = strings.ReplaceAll(errText, "herdr-soho:", "PROG:")
			if out != want.Out || errText != want.Err {
				t.Fatalf("out=%q want=%q\nstderr=%q want=%q", out, want.Out, errText, want.Err)
			}
			b, e := os.ReadFile(dest)
			if e != nil {
				t.Fatal(e)
			}
			if string(b) != want.File {
				t.Fatalf("file=%q want=%q", b, want.File)
			}
			temps := []string{}
			for _, dir := range []string{filepath.Dir(dest), env.Get("TMPDIR")} {
				if dir == "" {
					continue
				}
				entries, _ := os.ReadDir(dir)
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), "herdr-soho-conf.") || (strings.HasPrefix(entry.Name(), ".herdr-soho.conf.") && strings.HasSuffix(entry.Name(), ".tmp")) {
						temps = append(temps, filepath.Join(dir, entry.Name()))
					}
				}
			}
			if len(temps) > 0 {
				t.Fatalf("temporary files left: %v", temps)
			}
		})
	}
}

func normalizeGoldenRootPathSeparators(value string) string {
	if runtime.GOOS != "windows" {
		return value
	}
	var normalized strings.Builder
	for {
		start := strings.Index(value, "<ROOT>")
		if start < 0 {
			normalized.WriteString(value)
			return normalized.String()
		}
		normalized.WriteString(value[:start])
		end := start + len("<ROOT>")
		for end < len(value) && value[end] != ' ' && value[end] != '\t' && value[end] != '\r' && value[end] != '\n' && value[end] != '"' {
			end++
		}
		normalized.WriteString(strings.ReplaceAll(value[start:end], `\`, "/"))
		value = value[end:]
	}
}
