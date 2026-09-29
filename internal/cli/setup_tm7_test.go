package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/setuptext"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

type setupTM7Step struct {
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
	Err  string            `json:"err"`
	Out  string            `json:"out"`
	RC   int               `json:"rc"`
}

type setupTM7File struct {
	Rel     string  `json:"rel"`
	Content *string `json:"content"`
}

type setupTM7SetupGolden struct {
	Files []setupTM7File `json:"files"`
	Steps []setupTM7Step `json:"steps"`
}

type setupTM7Tree struct {
	Repo  [][2]string `json:"repo"`
	Conf  [][2]string `json:"conf"`
	State [][2]string `json:"state"`
}

type setupTM7PlanGolden struct {
	After    setupTM7Tree   `json:"after"`
	Before   setupTM7Tree   `json:"before"`
	Leftover []string       `json:"leftovers"`
	Steps    []setupTM7Step `json:"steps"`
}

type setupTM7Fixture struct {
	Conf  string
	Env   platform.Env
	Home  string
	Repo  string
	Root  string
	State string
	Tmp   string
}

func newSetupTM7Fixture(t *testing.T) setupTM7Fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fix := setupTM7Fixture{
		Root:  root,
		Repo:  filepath.Join(root, "repo"),
		Home:  filepath.Join(root, "home"),
		Conf:  filepath.Join(root, "conf"),
		State: filepath.Join(root, "state"),
		Tmp:   filepath.Join(root, "tmp"),
	}
	for _, dir := range []string{fix.Repo, fix.Home, fix.Conf, fix.State, fix.Tmp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	gitRules := []fakecli.Rule{
		{Argv: []string{"rev-parse", "--show-toplevel"}, Stdout: fix.Repo + "\n"},
		{Argv: []string{"rev-parse", "--git-dir"}, Code: 1},
		{Argv: []string{"rev-parse", "--git-common-dir"}, Code: 1},
		{Argv: []string{"-C", fix.Repo, "rev-parse", "--is-inside-work-tree"}, Stdout: "true\n"},
		{ArgvPrefix: true, Argv: []string{"-C", fix.Repo, "check-ignore", "-q"}, Code: 1},
		{AnyArgs: true, Code: 1},
	}
	if _, err := fakecli.Install(t, bin, "git", gitRules); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{{AnyArgs: true}}); err != nil {
		t.Fatal(err)
	}
	fix.Env = platform.Env{}
	for _, item := range fakecli.Env(testutil.CleanEnv(t), bin) {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			fix.Env[key] = value
		}
	}
	fix.Env["HOME"], fix.Env["USERPROFILE"] = fix.Home, fix.Home
	fix.Env["XDG_CONFIG_HOME"], fix.Env["HERDR_SOHO_DIR"] = fix.Conf, fix.State
	fix.Env["HERDR_WORKSPACE_ID"], fix.Env["TMPDIR"] = "ws", fix.Tmp
	fix.Env["HERDR_SOHO_SKILL_DIR"] = setupTM7SkillDir(t)
	return fix
}

func setupTM7GoldenPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "skills", "herdr-soho", "scripts", "test", "golden", name)
}

func readSetupTM7Golden[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := os.ReadFile(setupTM7GoldenPath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	var goldens T
	if err := json.Unmarshal(data, &goldens); err != nil {
		t.Fatal(err)
	}
	return goldens
}

func writeSetupTM7Seed(t *testing.T, root, rel, content string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runSetupTM7Steps(t *testing.T, fix setupTM7Fixture, steps []setupTM7Step) []setupTM7Step {
	t.Helper()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fix.Repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })
	t.Setenv("PATH", fix.Env.Get("PATH"))
	oldOut, oldErr := platform.Stdout, platform.Stderr
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	got := make([]setupTM7Step, 0, len(steps))
	for _, step := range steps {
		env := fix.Env.Clone()
		for key, value := range step.Env {
			env[key] = value
		}
		var out, errOut bytes.Buffer
		platform.Stdout, platform.Stderr = &out, &errOut
		code := Run(step.Args, env)
		stdout := normalizeGoldenRoot(out.String(), fix.Root)
		stderr := normalizeGoldenRoot(errOut.String(), fix.Root)
		program := filepath.Join(env.Get("HERDR_SOHO_SKILL_DIR"), "scripts", "herdr-soho")
		if platform.Current() == "win32" {
			program += ".cmd"
		}
		stdout = strings.ReplaceAll(stdout, program, "PROG")
		stderr = strings.ReplaceAll(stderr, program, "PROG")
		stderr = strings.ReplaceAll(stderr, "herdr-soho: ", "PROG: ")
		got = append(got, setupTM7Step{Args: step.Args, RC: code, Out: stdout, Err: stderr})
	}
	return got
}

func setupTM7ReadRel(root, rel string) *string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	content := string(data)
	return &content
}

func setupTM7ReadFiles(root string, paths []string) []setupTM7File {
	files := make([]setupTM7File, 0, len(paths))
	for _, rel := range paths {
		files = append(files, setupTM7File{Rel: rel, Content: setupTM7ReadRel(root, rel)})
	}
	return files
}

func runSetupTM7Golden(t *testing.T, name string, seed func(t *testing.T, fix setupTM7Fixture), steps []setupTM7Step) {
	t.Helper()
	goldens := readSetupTM7Golden[map[string]setupTM7SetupGolden](t, "parity-setup.json")
	want, ok := goldens[name]
	if !ok {
		t.Fatalf("setup golden %q missing", name)
	}
	fix := newSetupTM7Fixture(t)
	if seed != nil {
		seed(t, fix)
	}
	gotSteps := runSetupTM7Steps(t, fix, steps)
	if !reflect.DeepEqual(gotSteps, want.Steps) {
		got, _ := json.MarshalIndent(gotSteps, "", "  ")
		expected, _ := json.MarshalIndent(want.Steps, "", "  ")
		for i := range min(len(gotSteps), len(want.Steps)) {
			if !reflect.DeepEqual(gotSteps[i], want.Steps[i]) {
				t.Fatalf("setup steps differ at index %d: stdout %s; stderr %s\ngot=%s\nwant=%s", i, firstGoldenLineDifference(gotSteps[i].Out, want.Steps[i].Out), firstGoldenLineDifference(gotSteps[i].Err, want.Steps[i].Err), got, expected)
			}
		}
		t.Fatalf("setup steps differ\ngot=%s\nwant=%s", got, expected)
	}
	paths := make([]string, 0, len(want.Files))
	for _, file := range want.Files {
		paths = append(paths, file.Rel)
	}
	gotFiles := setupTM7ReadFiles(fix.Root, paths)
	gotFiles = normalizeSetupTM7Files(gotFiles, fix.Root)
	if !reflect.DeepEqual(gotFiles, want.Files) {
		got, _ := json.MarshalIndent(gotFiles, "", "  ")
		expected, _ := json.MarshalIndent(want.Files, "", "  ")
		t.Fatalf("setup files differ\ngot=%s\nwant=%s", got, expected)
	}
}

func normalizeSetupTM7Files(files []setupTM7File, root string) []setupTM7File {
	for i := range files {
		if files[i].Content != nil {
			content := normalizeGoldenRoot(*files[i].Content, root)
			files[i].Content = &content
		}
	}
	return files
}

func setupTM7SetupStep(args []string) setupTM7Step { return setupTM7Step{Args: args} }

func TestSetupTM7ParityGoldens(t *testing.T) {
	t.Run(`// JS: "parity: setup on a fresh repo writes AGENTS.md, hooks, no .gitignore outside the repo"`, func(t *testing.T) {
		runSetupTM7Golden(t, "setup-fresh", nil, []setupTM7Step{setupTM7SetupStep([]string{"setup"})})
	})
	t.Run(`// JS: "parity: setup appends to an existing AGENTS.md (one blank line before the block)"`, func(t *testing.T) {
		runSetupTM7Golden(t, "setup-agents-existing", func(t *testing.T, f setupTM7Fixture) {
			writeSetupTM7Seed(t, f.Repo, "AGENTS.md", "# Agent instructions\n")
		}, []setupTM7Step{setupTM7SetupStep([]string{"setup"})})
	})
	t.Run(`// JS: "parity: setup is idempotent on a block already present (second run replaces, same bytes)"`, func(t *testing.T) {
		block := "# Agent instructions\n\n" + setuptext.SetupBlock()
		runSetupTM7Golden(t, "setup-idempotent", func(t *testing.T, f setupTM7Fixture) { writeSetupTM7Seed(t, f.Repo, "AGENTS.md", block) }, []setupTM7Step{setupTM7SetupStep([]string{"setup"}), setupTM7SetupStep([]string{"setup"})})
	})
	t.Run(`// JS: "parity: CLAUDE.md as a symlink to AGENTS.md takes the block through the link, no warning"`, func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink privilege is required by the JS fixture")
		}
		runSetupTM7Golden(t, "setup-claude-symlink", func(t *testing.T, f setupTM7Fixture) {
			writeSetupTM7Seed(t, f.Repo, "AGENTS.md", "# Agent instructions\n")
			if err := os.Symlink(filepath.Join(f.Repo, "AGENTS.md"), filepath.Join(f.Repo, "CLAUDE.md")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		}, []setupTM7Step{setupTM7SetupStep([]string{"setup"}), setupTM7SetupStep([]string{"setup"})})
	})
	t.Run(`// JS: "parity: a separate CLAUDE.md without the block warns, and is left alone"`, func(t *testing.T) {
		runSetupTM7Golden(t, "setup-claude-separate", func(t *testing.T, f setupTM7Fixture) {
			writeSetupTM7Seed(t, f.Repo, "AGENTS.md", "# Agent instructions\n")
			writeSetupTM7Seed(t, f.Repo, "CLAUDE.md", "claude-only content\n")
		}, []setupTM7Step{setupTM7SetupStep([]string{"setup"})})
	})
	t.Run(`// JS: "parity: --target with a relative path targets the repo file"`, func(t *testing.T) {
		runSetupTM7Golden(t, "setup-target-relative", nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--target", "CLAUDE.md"})})
	})
	t.Run(`// JS: "parity: --no-hooks leaves no settings.json and skips the hook lines"`, func(t *testing.T) {
		runSetupTM7Golden(t, "setup-no-hooks", func(t *testing.T, f setupTM7Fixture) {
			writeSetupTM7Seed(t, f.Repo, "AGENTS.md", "# Agent instructions\n")
		}, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--no-hooks"})})
	})
	t.Run(`// JS: "parity: --dry-run prints the would-lines and writes nothing"`, func(t *testing.T) {
		runSetupTM7Golden(t, "setup-dry-run", func(t *testing.T, f setupTM7Fixture) {
			writeSetupTM7Seed(t, f.Repo, "AGENTS.md", "# Agent instructions\n")
		}, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--dry-run"}), setupTM7SetupStep([]string{"setup", "--dry-run", "--panes", "3", "--lane", "review=claude:opus:high"})})
	})
	t.Run(`// JS: "parity: --panes 3 applies the preset to the project config"`, func(t *testing.T) {
		runSetupTM7Golden(t, "setup-panes-3", nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--panes", "3"})})
	})
	t.Run(`// JS: "parity: --panes 4 with --lane writes the lane kind/model/effort"`, func(t *testing.T) {
		runSetupTM7Golden(t, "setup-panes-4-lane", nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--panes", "4", "--lane", "review=claude:opus:high"})})
	})
	t.Run(`// JS: "parity: --panes 5 and an unknown option are usage errors (rc 2), nothing written"`, func(t *testing.T) {
		args := [][]string{{"setup", "--panes", "5"}, {"setup", "--bogus"}, {"setup", "--target"}, {"setup", "--panes", "--no-hooks"}}
		steps := make([]setupTM7Step, 0, len(args))
		for _, a := range args {
			steps = append(steps, setupTM7SetupStep(a))
		}
		runSetupTM7Golden(t, "setup-usage-errors", nil, steps)
	})
	t.Run(`// JS: "parity: settings.json with other hooks is merged, other entries untouched"`, func(t *testing.T) {
		runSetupTM7Golden(t, "setup-settings-other", func(t *testing.T, f setupTM7Fixture) {
			writeSetupTM7Seed(t, f.Repo, ".claude/settings.json", "{\"other\":{\"x\":1},\"hooks\":{\"PreToolUse\":[{\"hooks\":[{\"type\":\"command\",\"command\":\"echo keep-me\"}]}],\"SessionStart\":[{\"hooks\":[{\"type\":\"command\",\"command\":\"bash something-else.sh\"}]}]}}\n")
		}, []setupTM7Step{setupTM7SetupStep([]string{"setup"})})
	})
	t.Run(`// JS: "parity: an empty or blank settings.json gets the hooks (read as {})"`, func(t *testing.T) {
		for _, tc := range []struct{ name, content string }{{"empty", ""}, {"blank", "\n\n"}} {
			t.Run(tc.name, func(t *testing.T) {
				runSetupTM7Golden(t, "setup-settings-"+tc.name, func(t *testing.T, f setupTM7Fixture) {
					writeSetupTM7Seed(t, f.Repo, ".claude/settings.json", tc.content)
				}, []setupTM7Step{setupTM7SetupStep([]string{"setup"})})
			})
		}
	})
	t.Run("// JS: \"parity: false where jq expects a list is read as an empty list (`// []`)\"", func(t *testing.T) {
		for _, tc := range []struct{ name, content string }{{"event-false", "{\"hooks\":{\"UserPromptSubmit\":false}}\n"}, {"entry-hooks-false", "{\"hooks\":{\"SessionStart\":[{\"hooks\":false},{\"hooks\":[{\"type\":\"command\",\"command\":\"echo keep\"}]}]}}\n"}} {
			t.Run(tc.name, func(t *testing.T) {
				runSetupTM7Golden(t, "setup-settings-"+tc.name, func(t *testing.T, f setupTM7Fixture) {
					writeSetupTM7Seed(t, f.Repo, ".claude/settings.json", tc.content)
				}, []setupTM7Step{setupTM7SetupStep([]string{"setup"})})
			})
		}
	})
	t.Run(`// JS: "parity: invalid settings.json refuses with rc 4, file untouched (jq error line not reproduced)"`, func(t *testing.T) {
		goldens := readSetupTM7Golden[map[string]setupTM7SetupGolden](t, "parity-setup.json")
		js, ok := goldens["setup-settings-invalid"]
		if !ok || len(js.Steps) != 1 || js.Steps[0].RC != 4 {
			t.Fatalf("invalid-settings JS golden missing expected rc 4: %+v", js)
		}
		jsWroteBlock := false
		for _, file := range js.Files {
			if file.Rel == "repo/AGENTS.md" && file.Content != nil && strings.Contains(*file.Content, setuptext.SetupStart) {
				jsWroteBlock = true
			}
		}
		if !jsWroteBlock {
			t.Fatalf("invalid-settings JS golden no longer proves the accepted write-before-refusal difference: %+v", js.Files)
		}
		fix := newSetupTM7Fixture(t)
		writeSetupTM7Seed(t, fix.Repo, ".claude/settings.json", "{invalid\n")
		before := setupTM7TreeOf(t, fix.Root)
		gotSteps := runSetupTM7Steps(t, fix, []setupTM7Step{setupTM7SetupStep([]string{"setup"})})
		gotFiles := setupTM7ReadFiles(fix.Root, []string{"repo/.claude/settings.json", "repo/AGENTS.md"})
		if len(gotSteps) != 1 || gotSteps[0].RC != 4 || gotSteps[0].Out != "" || !strings.Contains(gotSteps[0].Err, "settings.json") {
			t.Fatalf("Go refusal changed: steps=%+v", gotSteps)
		}
		if gotFiles[0].Content == nil || *gotFiles[0].Content != "{invalid\n" || gotFiles[1].Content != nil {
			t.Fatalf("Go must refuse before writing the instruction block: files=%+v", gotFiles)
		}
		if after := setupTM7TreeOf(t, fix.Root); !reflect.DeepEqual(before, after) {
			t.Fatalf("Go refusal changed the filesystem: before=%+v after=%+v", before, after)
		}
		t.Log("Accepted difference per PLAN section 7: JS writes AGENTS.md before rc 4; Go refuses before writing.")
	})
	t.Run(`// JS: "parity: the state dir is git-ignored when it lives inside the repo (HERDR_SOHO_DIR empty)"`, func(t *testing.T) {
		runSetupTM7Golden(t, "setup-gitignore", nil, []setupTM7Step{{Args: []string{"setup"}, Env: map[string]string{"HERDR_SOHO_DIR": ""}}})
	})
	t.Run(`// JS: "parity: test-setup.sh scenario — the block embeds no absolute installer path (project-local skill resolves the hook)"`, func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink privilege is required by the JS fixture")
		}
		runSetupTM7Golden(t, "setup-no-abs-path", func(t *testing.T, f setupTM7Fixture) {
			skillLink := filepath.Join(f.Repo, ".agents", "skills", "herdr-soho")
			if err := os.MkdirAll(filepath.Dir(skillLink), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(setupTM7SkillDir(t), skillLink); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			writeSetupTM7Seed(t, f.Repo, "AGENTS.md", "# Agent instructions\n")
			writeSetupTM7Seed(t, f.Repo, ".claude/settings.json", "{\"hooks\":{\"PreToolUse\":[{\"hooks\":[{\"type\":\"command\",\"command\":\"echo keep-me\"}]}]}}\n")
		}, []setupTM7Step{setupTM7SetupStep([]string{"setup"}), setupTM7SetupStep([]string{"setup"})})
	})
	t.Run(`// JS: "node: setup --probe --plan is exclusive (rc 2, the bash message)"`, func(t *testing.T) {
		fix := newSetupTM7Fixture(t)
		got := runSetupTM7Steps(t, fix, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--probe", "--plan"})})[0]
		if got.RC != 2 || got.Out != "" || got.Err != "PROG: setup: --probe and --plan are exclusive\n" {
			t.Fatalf("result=%+v", got)
		}
	})
}

func setupTM7SkillDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "skills", "herdr-soho"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatalf("skill directory %q: %v", dir, err)
	}
	return dir
}

func setupTM7TreeOf(t *testing.T, root string) setupTM7Tree {
	t.Helper()
	return setupTM7Tree{Repo: setupTM7Walk(t, root, "repo"), Conf: setupTM7Walk(t, root, "conf"), State: setupTM7Walk(t, root, "state")}
}

func normalizeSetupTM7Tree(tree setupTM7Tree, root string) setupTM7Tree {
	for _, entries := range []*[][2]string{&tree.Repo, &tree.Conf, &tree.State} {
		for i := range *entries {
			(*entries)[i][1] = normalizeGoldenRoot((*entries)[i][1], root)
		}
	}
	return tree
}

func setupTM7Walk(t *testing.T, root, relRoot string) [][2]string {
	t.Helper()
	base := filepath.Join(root, relRoot)
	var out [][2]string
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == base {
			return nil
		}
		if entry.Name() == ".git" && entry.IsDir() {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			out = append(out, [2]string{filepath.ToSlash(rel), "<dir>"})
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			out = append(out, [2]string{filepath.ToSlash(rel), "<symlink>" + target})
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, [2]string{filepath.ToSlash(rel), string(data)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func runSetupTM7PlanGolden(t *testing.T, name string, files map[string]string, dirs []string, steps []setupTM7Step) {
	t.Helper()
	goldens := readSetupTM7Golden[map[string]setupTM7PlanGolden](t, "parity-setup-plan.json")
	want, ok := goldens[name]
	if !ok {
		t.Fatalf("setup-plan golden %q missing", name)
	}
	fix := newSetupTM7Fixture(t)
	for rel, content := range files {
		root := fix.Repo
		if strings.HasPrefix(rel, "user/") {
			root, rel = fix.Conf, strings.TrimPrefix(rel, "user/")
		}
		writeSetupTM7Seed(t, root, rel, content)
	}
	for _, rel := range dirs {
		if err := os.MkdirAll(filepath.Join(fix.Repo, filepath.FromSlash(rel)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	before := setupTM7TreeOf(t, fix.Root)
	gotSteps := runSetupTM7Steps(t, fix, steps)
	after := setupTM7TreeOf(t, fix.Root)
	before = normalizeSetupTM7Tree(before, fix.Root)
	after = normalizeSetupTM7Tree(after, fix.Root)
	leftovers := []string{}
	entries, err := os.ReadDir(fix.Tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "herdr-soho-plan.") {
			leftovers = append(leftovers, entry.Name())
		}
	}
	if !reflect.DeepEqual(gotSteps, want.Steps) {
		got, _ := json.MarshalIndent(gotSteps, "", "  ")
		expected, _ := json.MarshalIndent(want.Steps, "", "  ")
		gitLog, _ := os.ReadFile(filepath.Join(strings.Split(fix.Env.Get("PATH"), string(os.PathListSeparator))[0], "git.calls.jsonl"))
		for i := range min(len(gotSteps), len(want.Steps)) {
			if !reflect.DeepEqual(gotSteps[i], want.Steps[i]) {
				t.Fatalf("plan steps differ at index %d: stdout %s; stderr %s\ngit calls=%s\ngot=%s\nwant=%s", i, firstGoldenLineDifference(gotSteps[i].Out, want.Steps[i].Out), firstGoldenLineDifference(gotSteps[i].Err, want.Steps[i].Err), gitLog, got, expected)
			}
		}
		t.Fatalf("plan steps differ\ngit calls=%s\ngot=%s\nwant=%s", gitLog, got, expected)
	}
	if !reflect.DeepEqual(before, after) {
		got, _ := json.MarshalIndent(struct {
			Before setupTM7Tree `json:"before"`
			After  setupTM7Tree `json:"after"`
		}{before, after}, "", "  ")
		t.Fatalf("plan changed files: %s", got)
	}
	if len(leftovers) != 0 {
		t.Fatalf("plan temp dirs remain: %v", leftovers)
	}
}

func TestSetupTM7PlanParityGoldens(t *testing.T) {
	base := map[string]string{
		"AGENTS.md":             "# Agent instructions\n",
		".claude/settings.json": "{\"hooks\":{\"PreToolUse\":[{\"hooks\":[{\"type\":\"command\",\"command\":\"echo keep-me\"}]}]}}\n",
	}
	t.Run(`// JS: "parity: plan --panes 4 on a fresh project (nothing written)"`, func(t *testing.T) {
		runSetupTM7PlanGolden(t, "parity: plan --panes 4 on a fresh project (nothing written)", base, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--panes", "4"})})
	})
	t.Run(`// JS: "parity: plan shows the before value of a key being set"`, func(t *testing.T) {
		files := map[string]string{}
		for k, v := range base {
			files[k] = v
		}
		files[".agents/herdr-soho.conf"] = "panes=3\n"
		runSetupTM7PlanGolden(t, "parity: plan shows the before value of a key being set", files, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--panes", "4"})})
	})
	t.Run(`// JS: "parity: plan with an invalid --lane dies 2"`, func(t *testing.T) {
		runSetupTM7PlanGolden(t, "parity: plan with an invalid --lane dies 2", base, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--panes", "4", "--lane", "build=boguskind"})})
	})
	t.Run(`// JS: "parity: plan a valid --lane and a --set"`, func(t *testing.T) {
		runSetupTM7PlanGolden(t, "parity: plan a valid --lane and a --set", base, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--panes", "4", "--lane", "build=grok:grok-4.7:high", "--set", "max_workers", "5"})})
	})
	t.Run(`// JS: "parity: plan --user-set with no user file yet"`, func(t *testing.T) {
		runSetupTM7PlanGolden(t, "parity: plan --user-set with no user file yet", base, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--user-set", "model.pi.worker", "my-provider/my-model"})})
	})
	t.Run(`// JS: "parity: plan --user-set over an existing user value"`, func(t *testing.T) {
		files := map[string]string{}
		for k, v := range base {
			files[k] = v
		}
		files["user/herdr-soho/config"] = "model.pi.worker=other/model\n"
		runSetupTM7PlanGolden(t, "parity: plan --user-set over an existing user value", files, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--user-set", "model.pi.worker", "new/model"})})
	})
	t.Run(`// JS: "parity: plan --session-set with no session file yet"`, func(t *testing.T) {
		runSetupTM7PlanGolden(t, "parity: plan --session-set with no session file yet", base, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--session-set", "lane.build.kind", "pi"})})
	})
	t.Run(`// JS: "parity: plan a key the real setup would delete"`, func(t *testing.T) {
		files := map[string]string{}
		for k, v := range base {
			files[k] = v
		}
		files[".agents/herdr-soho.conf"] = "role.planner.model=fable\n"
		runSetupTM7PlanGolden(t, "parity: plan a key the real setup would delete", files, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--panes", "4"})})
	})
	t.Run(`// JS: "parity: plan dies 2 on an invalid key or value"`, func(t *testing.T) {
		args := [][]string{{"setup", "--plan", "--set", "nope", "1"}, {"setup", "--plan", "--set", "max_workers", "-1"}, {"setup", "--plan", "--panes", "5"}}
		steps := make([]setupTM7Step, 0, len(args))
		for _, a := range args {
			steps = append(steps, setupTM7SetupStep(a))
		}
		runSetupTM7PlanGolden(t, "parity: plan dies 2 on an invalid key or value", base, nil, steps)
	})
	t.Run(`// JS: "parity: bare plan plans the block and the hooks"`, func(t *testing.T) {
		runSetupTM7PlanGolden(t, "parity: bare plan plans the block and the hooks", base, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan"})})
	})
	t.Run(`// JS: "parity: plan the .gitignore entry without writing it"`, func(t *testing.T) {
		files := map[string]string{}
		for k, v := range base {
			files[k] = v
		}
		files[".gitignore"] = "*.log\n"
		runSetupTM7PlanGolden(t, "parity: plan the .gitignore entry without writing it", files, nil, []setupTM7Step{{Args: []string{"setup", "--plan", "--session-set", "lane.build.kind", "pi"}, Env: map[string]string{"HERDR_SOHO_DIR": ""}}})
	})
	t.Run(`// JS: "parity: no .gitignore section when the entry is already there"`, func(t *testing.T) {
		files := map[string]string{}
		for k, v := range base {
			files[k] = v
		}
		files[".gitignore"] = "*.log\n.herdr-soho/\n"
		runSetupTM7PlanGolden(t, "parity: no .gitignore section when the entry is already there", files, []string{".herdr-soho"}, []setupTM7Step{{Args: []string{"setup", "--plan", "--session-set", "lane.build.kind", "pi"}, Env: map[string]string{"HERDR_SOHO_DIR": ""}}})
	})
	t.Run(`// JS: "parity: plan dies 2 when a flag is missing its value"`, func(t *testing.T) {
		args := [][]string{{"setup", "--plan", "--set", "max_workers"}, {"setup", "--plan", "--lane"}, {"setup", "--plan", "--lane", "--panes", "4"}}
		steps := make([]setupTM7Step, 0, len(args))
		for _, a := range args {
			steps = append(steps, setupTM7SetupStep(a))
		}
		runSetupTM7PlanGolden(t, "parity: plan dies 2 when a flag is missing its value", base, nil, steps)
	})
	t.Run(`// JS: "parity: plan dies 4 when the hooks merge would fail"`, func(t *testing.T) {
		files := map[string]string{"AGENTS.md": "# Agent instructions\n", ".claude/settings.json": "not-json\n"}
		runSetupTM7PlanGolden(t, "parity: plan dies 4 when the hooks merge would fail", files, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--panes", "4"})})
	})
	t.Run(`// JS: "parity: plan with --set, --user-set and --session-set together"`, func(t *testing.T) {
		files := map[string]string{}
		for k, v := range base {
			files[k] = v
		}
		files[".agents/herdr-soho.conf"] = "max_workers=3\n"
		runSetupTM7PlanGolden(t, "parity: plan with --set, --user-set and --session-set together", files, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--set", "max_workers", "5", "--user-set", "model.pi.worker", "p/m", "--session-set", "lane.build.kind", "pi"})})
	})
	t.Run(`// JS: "parity: plan --target on a file carrying the block"`, func(t *testing.T) {
		files := map[string]string{"AGENTS.md": "# head\n<!-- herdr-soho:start -->\nold block line\n<!-- herdr-soho:end -->\n# tail\n", ".claude/settings.json": base[".claude/settings.json"]}
		runSetupTM7PlanGolden(t, "parity: plan --target on a file carrying the block", files, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--target", "AGENTS.md"})})
	})
	t.Run(`// JS: "parity: plan --no-hooks skips the hooks section"`, func(t *testing.T) {
		runSetupTM7PlanGolden(t, "parity: plan --no-hooks skips the hooks section", base, nil, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--no-hooks"})})
	})
}

func TestSetupTM7SetupPlanEndToEndCLI(t *testing.T) {
	baseFiles := func(t *testing.T, f setupTM7Fixture) {
		writeSetupTM7Seed(t, f.Repo, "AGENTS.md", "# Agent instructions\n")
		writeSetupTM7Seed(t, f.Repo, ".claude/settings.json", "{\"hooks\":{\"PreToolUse\":[{\"hooks\":[{\"type\":\"command\",\"command\":\"echo keep-me\"}]}]}}\n")
	}
	assertNoWrite := func(t *testing.T, f setupTM7Fixture, args [][]string, wantCodes []int, wantOut [][]string) {
		t.Helper()
		before := setupTM7TreeOf(t, f.Root)
		steps := make([]setupTM7Step, len(args))
		for i := range args {
			steps[i] = setupTM7SetupStep(args[i])
		}
		got := runSetupTM7Steps(t, f, steps)
		for i, step := range got {
			if step.RC != wantCodes[i] {
				t.Fatalf("step %d got rc=%d out=%q err=%q, want rc=%d", i, step.RC, step.Out, step.Err, wantCodes[i])
			}
			for _, want := range wantOut[i] {
				if !strings.Contains(step.Out, want) {
					t.Fatalf("step %d output %q does not contain %q", i, step.Out, want)
				}
			}
		}
		if after := setupTM7TreeOf(t, f.Root); !reflect.DeepEqual(before, after) {
			t.Fatalf("setup --plan wrote files: before=%+v after=%+v", before, after)
		}
	}
	t.Run(`// JS: "e2e: setup --plan --panes prints the plan and writes nothing"`, func(t *testing.T) {
		f := newSetupTM7Fixture(t)
		baseFiles(t, f)
		writeSetupTM7Seed(t, f.Repo, ".agents/herdr-soho.conf", "panes=3\n")
		assertNoWrite(t, f, [][]string{{"setup", "--plan", "--panes", "4"}}, []int{0}, [][]string{{"plan (nothing is written):", "panes                3 → 4"}})
	})
	t.Run(`// JS: "e2e: --panes 2 plans the 2-pane preset (nothing written)"`, func(t *testing.T) {
		f := newSetupTM7Fixture(t)
		baseFiles(t, f)
		assertNoWrite(t, f, [][]string{{"setup", "--plan", "--panes", "2"}}, []int{0}, [][]string{{"panes                (unset) → 2", "reuse_workers        (unset) → on"}})
	})
	t.Run(`// JS: "e2e: --set, --user-set and --session-set together — the three sections, nothing written"`, func(t *testing.T) {
		f := newSetupTM7Fixture(t)
		baseFiles(t, f)
		assertNoWrite(t, f, [][]string{{"setup", "--plan", "--set", "max_workers", "5", "--user-set", "model.pi.worker", "my-provider/my-model", "--session-set", "lane.build.kind", "pi"}}, []int{0}, [][]string{{"max_workers", "model.pi.worker", "lane.build.kind"}})
	})
	t.Run(`// JS: "e2e: invalid keys/values and a missing flag value die 2 before anything is shown or written"`, func(t *testing.T) {
		f := newSetupTM7Fixture(t)
		baseFiles(t, f)
		assertNoWrite(t, f, [][]string{{"setup", "--plan", "--set", "nope", "1"}, {"setup", "--plan", "--set", "max_workers", "-1"}, {"setup", "--plan", "--lane"}}, []int{2, 2, 2}, [][]string{{}, {}, {}})
		before := setupTM7TreeOf(t, f.Root)
		got := runSetupTM7Steps(t, f, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--bogus"})})[0]
		if got.RC != 2 || got.Out != "" || !strings.Contains(got.Err, "setup --plan: unknown option '--bogus'") {
			t.Fatalf("unknown plan option rc=%d out=%q err=%q", got.RC, got.Out, got.Err)
		}
		if after := setupTM7TreeOf(t, f.Root); !reflect.DeepEqual(before, after) {
			t.Fatalf("unknown plan option changed files: before=%+v after=%+v", before, after)
		}
	})
	t.Run(`// JS: "e2e: a write the real setup would refuse is refused by the plan (rc 4), file untouched"`, func(t *testing.T) {
		f := newSetupTM7Fixture(t)
		writeSetupTM7Seed(t, f.Repo, "AGENTS.md", "# Agent instructions\n")
		writeSetupTM7Seed(t, f.Repo, ".claude/settings.json", "not-json\n")
		before := setupTM7TreeOf(t, f.Root)
		got := runSetupTM7Steps(t, f, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--panes", "4"})})[0]
		if got.RC != 4 || !strings.Contains(got.Out, "plan (nothing is written):") || !strings.Contains(got.Err, "settings.json") {
			t.Fatalf("rc=%d out=%q err=%q", got.RC, got.Out, got.Err)
		}
		if after := setupTM7TreeOf(t, f.Root); !reflect.DeepEqual(before, after) {
			t.Fatalf("refused plan changed files: before=%+v after=%+v", before, after)
		}
	})
	t.Run(`// JS: "e2e: --target on a file with the block plans the in-place replacement; --no-hooks skips the hooks"`, func(t *testing.T) {
		f := newSetupTM7Fixture(t)
		writeSetupTM7Seed(t, f.Repo, "AGENTS.md", "# head\n"+setuptext.SetupStart+"\nold block\n"+setuptext.SetupEnd+"\n# tail\n")
		assertNoWrite(t, f, [][]string{{"setup", "--plan", "--target", "AGENTS.md", "--no-hooks"}}, []int{0}, [][]string{{"AGENTS.md", "old block"}})
	})
	t.Run(`// JS: "e2e: the .gitignore entry is planned (not written) when the state dir would not be ignored"`, func(t *testing.T) {
		f := newSetupTM7Fixture(t)
		writeSetupTM7Seed(t, f.Repo, ".gitignore", "*.log\n")
		before := setupTM7TreeOf(t, f.Root)
		got := runSetupTM7Steps(t, f, []setupTM7Step{{Args: []string{"setup", "--plan", "--session-set", "lane.build.kind", "pi"}, Env: map[string]string{"HERDR_SOHO_DIR": ""}}})[0]
		if got.RC != 0 || !strings.Contains(got.Out, ".gitignore") {
			t.Fatalf("rc=%d out=%q err=%q", got.RC, got.Out, got.Err)
		}
		if after := setupTM7TreeOf(t, f.Root); !reflect.DeepEqual(before, after) {
			t.Fatalf("planned gitignore changed files: before=%+v after=%+v", before, after)
		}
	})
	t.Run(`// JS: "e2e: --session-set without a resolvable workspace dies 2"`, func(t *testing.T) {
		f := newSetupTM7Fixture(t)
		before := setupTM7TreeOf(t, f.Root)
		got := runSetupTM7Steps(t, f, []setupTM7Step{{Args: []string{"setup", "--plan", "--session-set", "lane.build.kind", "pi"}, Env: map[string]string{"HERDR_WORKSPACE_ID": ""}}})[0]
		if got.RC != 2 || got.Out != "" {
			t.Fatalf("rc=%d out=%q err=%q", got.RC, got.Out, got.Err)
		}
		if after := setupTM7TreeOf(t, f.Root); !reflect.DeepEqual(before, after) {
			t.Fatalf("unresolvable session plan changed files: before=%+v after=%+v", before, after)
		}
	})
	t.Run(`// JS: "cmdSetupPlan: in-process — DieError 2 for a bad lane spec, stdout untouched"`, func(t *testing.T) {
		f := newSetupTM7Fixture(t)
		before := setupTM7TreeOf(t, f.Root)
		got := runSetupTM7Steps(t, f, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--plan", "--lane", "build=boguskind"})})[0]
		if got.RC != 2 || got.Out != "" {
			t.Fatalf("rc=%d out=%q err=%q", got.RC, got.Out, got.Err)
		}
		if after := setupTM7TreeOf(t, f.Root); !reflect.DeepEqual(before, after) {
			t.Fatalf("bad lane plan changed files: before=%+v after=%+v", before, after)
		}
	})
}

func TestSetupTM7PanesWriteCLI(t *testing.T) {
	t.Run(`// JS: "setup --panes: 2 writes the 2-pane preset (no frozen roles/limits), 5 dies 2"`, func(t *testing.T) {
		f := newSetupTM7Fixture(t)
		got := runSetupTM7Steps(t, f, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--panes", "2"})})[0]
		content := setupTM7ReadRel(f.Repo, ".agents/herdr-soho.conf")
		if got.RC != 0 || content == nil || !strings.Contains(*content, "panes=2") || !strings.Contains(*content, "reuse_workers=on") || strings.Contains(*content, "lane.build.roles=") || strings.Contains(*content, "max_workers=") || strings.Contains(*content, "split_max_panes=") || strings.Contains(got.Err, "setup --panes 2|3|4") == false {
			t.Fatalf("setup --panes 2 rc=%d out=%q err=%q config=%v", got.RC, got.Out, got.Err, content)
		}
		before := setupTM7TreeOf(t, f.Root)
		bad := runSetupTM7Steps(t, f, []setupTM7Step{setupTM7SetupStep([]string{"setup", "--panes", "5"})})[0]
		if bad.RC != 2 || bad.Out != "" || !strings.Contains(bad.Err, "--panes must be 2, 3 or 4") || !reflect.DeepEqual(before, setupTM7TreeOf(t, f.Root)) {
			t.Fatalf("invalid --panes result rc=%d out=%q err=%q", bad.RC, bad.Out, bad.Err)
		}
	})
}
