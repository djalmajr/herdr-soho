package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func runTM10(t *testing.T, args []string, rules []fakecli.Rule, env platform.Env) (int, string, string, []fakecli.Call) {
	t.Helper()
	fakeDir := t.TempDir()
	_, err := fakecli.Install(t, fakeDir, "herdr", rules)
	if err != nil {
		t.Fatal(err)
	}
	clean := platform.Env{}
	for _, entry := range fakecli.Env(testutil.CleanEnv(t), fakeDir) {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			clean[key] = value
		}
	}
	for key, value := range env {
		clean[key] = value
	}
	root := t.TempDir()
	clean["HOME"] = root
	clean["USERPROFILE"] = root
	clean["XDG_CONFIG_HOME"] = filepath.Join(root, "config")
	clean["HERDR_SOHO_DIR"] = filepath.Join(root, "state")
	clean["HERDR_SOHO_SKILL_DIR"] = testSkillDir(t)
	clean["HERDR_WORKSPACE_ID"] = "ws"
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	code := Run(args, clean)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(fakeDir, "herdr.json"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return code, out.String(), stderr.String(), calls
}

func TestTM10TitleCases(t *testing.T) {
	cases := []struct {
		title string
		args  []string
		rules []fakecli.Rule
		env   platform.Env
		code  int
		out   string
		call  bool
	}{
		{`title: the positionals are joined with one space and stored once`, []string{"title", "a", "b"}, []fakecli.Rule{{Argv: []string{"pane", "report-metadata", "p1", "--source", "herdr-soho", "--title", "orchestrator: a b"}}}, platform.Env{"HERDR_ENV": "1", "HERDR_PANE_ID": "p1"}, 0, "{\n  \"pane_id\": \"p1\",\n  \"title\": \"orchestrator: a b\"\n}\n", true},
		{`title: a 70-code-point objective is cut at 60 (astral chars counted once)`, []string{"title", strings.Repeat("𝕆", 5) + strings.Repeat("x", 65)}, []fakecli.Rule{{Argv: []string{"pane", "report-metadata", "p1", "--source", "herdr-soho", "--title", "orchestrator: " + strings.Repeat("𝕆", 5) + strings.Repeat("x", 55)}}}, platform.Env{"HERDR_ENV": "1", "HERDR_PANE_ID": "p1"}, 0, "{\n  \"pane_id\": \"p1\",\n  \"title\": \"orchestrator: " + strings.Repeat("𝕆", 5) + strings.Repeat("x", 55) + "\"\n}\n", true},
		{`title: --clear clears the pane title and prints the empty title`, []string{"title", "--clear"}, []fakecli.Rule{{Argv: []string{"pane", "report-metadata", "p1", "--source", "herdr-soho", "--clear-title"}}}, platform.Env{"HERDR_ENV": "1", "HERDR_PANE_ID": "p1"}, 0, "{\n  \"pane_id\": \"p1\",\n  \"title\": \"\"\n}\n", true},
		{`title: no objective (or only spaces) dies 2 without touching herdr`, []string{"title"}, nil, platform.Env{"HERDR_ENV": "1", "HERDR_PANE_ID": "p1"}, 2, "", false},
		{`title: an unknown option dies 2 naming it`, []string{"title", "--bogus", "x"}, nil, platform.Env{"HERDR_ENV": "1", "HERDR_PANE_ID": "p1"}, 2, "", false},
		{`title: a failing report-metadata dies 4 (objective and --clear)`, []string{"title", "x"}, []fakecli.Rule{{ArgvPrefix: true, Argv: []string{"pane", "report-metadata"}, Code: 1}}, platform.Env{"HERDR_ENV": "1", "HERDR_PANE_ID": "p1"}, 4, "", true},
		{`title: newlines and tabs become a space, edges trimmed`, []string{"title", "a\tb\n c  "}, []fakecli.Rule{{Argv: []string{"pane", "report-metadata", "p1", "--source", "herdr-soho", "--title", "orchestrator: a b  c"}}}, platform.Env{"HERDR_ENV": "1", "HERDR_PANE_ID": "p1"}, 0, "{\n  \"pane_id\": \"p1\",\n  \"title\": \"orchestrator: a b  c\"\n}\n", true},
		{`title: --clear with an objective dies 2 and changes nothing`, []string{"title", "x", "--clear"}, nil, platform.Env{"HERDR_ENV": "1", "HERDR_PANE_ID": "p1"}, 2, "", false},
		{`title: a living command — without HERDR_ENV it refuses with rc 2`, []string{"title", "x"}, nil, platform.Env{"HERDR_PANE_ID": "p1"}, 2, "", false},
	}
	for _, tc := range cases {
		t.Run(`JS: "`+tc.title+`"`, func(t *testing.T) {
			code, out, stderr, calls := runTM10(t, tc.args, tc.rules, tc.env)
			if code != tc.code || out != tc.out {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
			}
			if tc.call != (len(calls) > 0) {
				t.Fatalf("calls=%#v", calls)
			}
			if code == 2 && stderr == "" {
				t.Fatal("usage error did not explain the refusal")
			}
		})
	}
}

func TestTM10FrictionCases(t *testing.T) {
	cases := []struct {
		title string
		args  []string
		code  int
		out   string
		line  string
	}{
		{`friction: no argument prints the log as today (the no-friction line when empty)`, []string{"friction"}, 0, "no friction recorded under", ""},
		{`friction add: records a note line with the command friction and prints recorded`, []string{"friction", "add", "a note"}, 0, "recorded\n", "\tnote\tfriction\ta note\n"},
		{`friction add: --brief appends the brief path to the message`, []string{"friction", "add", "--brief", "brief.md", "a note"}, 0, "recorded\n", "\tnote\tfriction\ta note (brief: brief.md)\n"},
		{`friction add: an empty text dies 2 with the usage message`, []string{"friction", "add"}, 2, "", ""},
		{`friction add: an unknown option or subcommand dies 2`, []string{"friction", "bogus"}, 2, "", ""},
		{`friction add: the text is sanitized into one four-column line`, []string{"friction", "add", "one\r\n two\tthree"}, 0, "recorded\n", "\tnote\tfriction\tone  two three\n"},
	}
	for _, tc := range cases {
		t.Run(`JS: "`+tc.title+`"`, func(t *testing.T) {
			env, _ := commandFixture(t)
			fakeDir := t.TempDir()
			if _, err := fakecli.Install(t, fakeDir, "herdr", nil); err != nil {
				t.Fatal(err)
			}
			env = withFakeCLI(env, fakeDir)
			env["HERDR_ENV"] = "1"
			oldOut, oldErr := platform.Stdout, platform.Stderr
			var out, stderr bytes.Buffer
			platform.Stdout, platform.Stderr = &out, &stderr
			code := Run(tc.args, env)
			platform.Stdout, platform.Stderr = oldOut, oldErr
			if code != tc.code || !strings.Contains(out.String(), tc.out) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
			}
			logPath := filepath.Join(env.Get("HERDR_SOHO_DIR"), "ws", "friction.log")
			data, err := os.ReadFile(logPath)
			if tc.line != "" && (err != nil || !strings.Contains(string(data), tc.line)) {
				t.Fatalf("friction log=%q err=%v", data, err)
			}
		})
	}
}
