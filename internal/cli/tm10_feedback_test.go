package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func tm10FeedbackFixture(t *testing.T, extra platform.Env) (string, platform.Env) {
	t.Helper()
	root := t.TempDir()
	feedbackDir := filepath.Join(root, "feedback")
	if err := os.Mkdir(feedbackDir, 0o700); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(root, "report.md")
	if err := os.WriteFile(report, []byte("report bytes\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{"HERDR_ENV": "1", "HERDR_SOHO_FEEDBACK": "local", "HERDR_SOHO_FEEDBACK_DIR": feedbackDir}
	for k, v := range extra {
		env[k] = v
	}
	return report, env
}

func TestTM10FeedbackCases(t *testing.T) {
	t.Run(`JS: "feedback send: a policy other than local dies 2 with the exact message"`, func(t *testing.T) {
		for _, policy := range []string{"ask", "on", "off"} {
			report, env := tm10FeedbackFixture(t, platform.Env{"HERDR_SOHO_FEEDBACK": policy})
			code, out, stderr, _ := runTM10(t, []string{"feedback", "send", report, "stuck pane"}, nil, env)
			if code != 2 || out != "" || !strings.Contains(stderr, "feedback="+policy) {
				t.Fatalf("policy=%s code=%d out=%q err=%q", policy, code, out, stderr)
			}
			entries, _ := os.ReadDir(env.Get("HERDR_SOHO_FEEDBACK_DIR"))
			if len(entries) != 0 {
				t.Fatalf("policy=%s wrote files: %#v", policy, entries)
			}
		}
	})
	t.Run(`JS: "feedback send: feedback_dir empty or not a directory dies 2 naming the key"`, func(t *testing.T) {
		report, env := tm10FeedbackFixture(t, nil)
		env["HERDR_SOHO_FEEDBACK_DIR"] = ""
		code, _, stderr, _ := runTM10(t, []string{"feedback", "send", report, "summary"}, nil, env)
		if code != 2 || !strings.Contains(stderr, "set feedback_dir") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	})
	t.Run(`JS: "feedback send: a missing or empty report dies 2"`, func(t *testing.T) {
		_, env := tm10FeedbackFixture(t, nil)
		for _, name := range []string{"missing.md", filepath.Join(t.TempDir(), "empty.md")} {
			if strings.HasPrefix(name, string(filepath.Separator)) {
				_ = os.WriteFile(name, nil, 0o600)
			}
			code, _, stderr, _ := runTM10(t, []string{"feedback", "send", name, "summary"}, nil, env)
			if code != 2 || !strings.Contains(stderr, "report must be a non-empty file") {
				t.Fatalf("report=%q code=%d stderr=%q", name, code, stderr)
			}
		}
	})
	t.Run(`JS: "feedback send: an empty summary dies 2"`, func(t *testing.T) {
		report, env := tm10FeedbackFixture(t, nil)
		code, _, stderr, _ := runTM10(t, []string{"feedback", "send", report, ""}, nil, env)
		if code != 2 || !strings.Contains(stderr, "one-line summary is required") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	})
	t.Run(`JS: "feedback send: without feedback_to it files the report and prints the sent JSON"`, func(t *testing.T) {
		report, env := tm10FeedbackFixture(t, nil)
		code, out, stderr, calls := runTM10(t, []string{"feedback", "send", report, "summary"}, nil, env)
		var result struct {
			Status   string `json:"status"`
			File     string `json:"file"`
			Notified any    `json:"notified"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(result.File)
		if code != 0 || stderr != "" || result.Status != "sent" || result.Notified != nil || err != nil || string(got) != "report bytes\r\n" || len(calls) != 0 {
			t.Fatalf("code=%d result=%#v bytes=%q err=%v calls=%#v stderr=%q", code, result, got, err, calls, stderr)
		}
	})
	t.Run(`JS: "feedback send: with feedback_to it prompts the one line to that pane"`, func(t *testing.T) {
		report, env := tm10FeedbackFixture(t, platform.Env{"HERDR_SOHO_FEEDBACK_TO": "w9:p2"})
		rules := []fakecli.Rule{{ArgvPrefix: true, Argv: []string{"agent", "prompt", "w9:p2"}, Stdout: `{"result":{"submitted":true}}`}}
		code, out, stderr, calls := runTM10(t, []string{"feedback", "send", report, "lane review"}, rules, env)
		if code != 0 || stderr != "" || !strings.Contains(out, `"notified":"w9:p2"`) || len(calls) != 1 || len(calls[0].Argv) != 4 || !strings.Contains(calls[0].Argv[3], "lane review") {
			t.Fatalf("code=%d out=%q stderr=%q calls=%#v", code, out, stderr, calls)
		}
	})
	t.Run(`JS: "feedback send: a second send the same day takes -b and the first file stays intact"`, func(t *testing.T) {
		report, env := tm10FeedbackFixture(t, nil)
		_, first, _, _ := runTM10(t, []string{"feedback", "send", report, "first"}, nil, env)
		_, second, _, _ := runTM10(t, []string{"feedback", "send", report, "second"}, nil, env)
		var one, two struct {
			File string `json:"file"`
		}
		_ = json.Unmarshal([]byte(first), &one)
		_ = json.Unmarshal([]byte(second), &two)
		firstBytes, err := os.ReadFile(one.File)
		if err != nil || one.File == two.File || !strings.HasSuffix(two.File, "-b.md") || string(firstBytes) != "report bytes\r\n" {
			t.Fatalf("first=%#v second=%#v bytes=%q err=%v", one, two, firstBytes, err)
		}
	})
	t.Run(`JS: "feedback send: \\r, \\n and \\t in the summary become spaces (one prompt line)"`, func(t *testing.T) {
		report, env := tm10FeedbackFixture(t, platform.Env{"HERDR_SOHO_FEEDBACK_TO": "w9:p2"})
		rules := []fakecli.Rule{{ArgvPrefix: true, Argv: []string{"agent", "prompt", "w9:p2"}, Stdout: `{"result":{"submitted":true}}`}}
		_, _, _, calls := runTM10(t, []string{"feedback", "send", report, "first\r\nsecond\tthird"}, rules, env)
		if len(calls) != 1 || !strings.Contains(calls[0].Argv[3], "): first second third — ") || strings.ContainsAny(calls[0].Argv[3], "\r\n\t") {
			t.Fatalf("calls=%#v", calls)
		}
	})
	t.Run("feedback send: a failed notice exits 0 with the file saved, the friction line, the warning and the filed JSON", func(t *testing.T) {
		fakeDir := t.TempDir()
		rules := []fakecli.Rule{{ArgvPrefix: true, Argv: []string{"agent", "prompt", "w9:p2"}, Stderr: "prompt refused\n", Code: 1}}
		if _, err := fakecli.Install(t, fakeDir, "herdr", rules); err != nil {
			t.Fatal(err)
		}
		base := platform.Env{}
		for _, entry := range fakecli.Env(testutil.CleanEnv(t), fakeDir) {
			key, value, ok := strings.Cut(entry, "=")
			if ok {
				base[key] = value
			}
		}
		root := t.TempDir()
		stateDir := filepath.Join(root, "state")
		feedbackDir := filepath.Join(root, "feedback")
		if err := os.Mkdir(feedbackDir, 0o700); err != nil {
			t.Fatal(err)
		}
		report := filepath.Join(root, "report.md")
		if err := os.WriteFile(report, []byte("report bytes\r\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		base["HOME"], base["USERPROFILE"], base["XDG_CONFIG_HOME"] = root, root, filepath.Join(root, "config")
		base["HERDR_SOHO_DIR"], base["HERDR_SOHO_SKILL_DIR"], base["HERDR_WORKSPACE_ID"] = stateDir, testSkillDir(t), "ws"
		base["HERDR_ENV"], base["PATH"] = "1", fakeDir
		base["HERDR_SOHO_FAKECLI_CONFIG"] = filepath.Join(fakeDir, "herdr.json")
		base["HERDR_SOCKET_PATH"] = filepath.Join(root, "missing.sock")
		base["HERDR_SOHO_FEEDBACK"], base["HERDR_SOHO_FEEDBACK_DIR"], base["HERDR_SOHO_FEEDBACK_TO"] = "local", feedbackDir, "w9:p2"
		var out, stderr bytes.Buffer
		oldOut, oldErr := platform.Stdout, platform.Stderr
		platform.Stdout, platform.Stderr = &out, &stderr
		code := Run([]string{"feedback", "send", report, "summary"}, base)
		platform.Stdout, platform.Stderr = oldOut, oldErr
		var result struct {
			Status   string `json:"status"`
			File     string `json:"file"`
			Notified any    `json:"notified"`
			Error    string `json:"error"`
		}
		if err := json.Unmarshal([]byte(out.String()), &result); err != nil {
			t.Fatal(err)
		}
		saved, saveErr := os.ReadFile(result.File)
		logText, logErr := os.ReadFile(filepath.Join(stateDir, "ws", "friction.log"))
		wantWarn := "herdr-soho: warning: feedback send: the report is saved at " + result.File + ", but the notice to w9:p2 failed: prompt refused"
		if code != 0 || saveErr != nil || string(saved) != "report bytes\r\n" || result.Status != "filed" || result.Notified != nil || result.Error != "prompt refused" || !strings.Contains(stderr.String(), wantWarn) || logErr != nil || !strings.Contains(string(logText), "feedback sent: "+result.File) {
			t.Fatalf("code=%d result=%#v saved=%q stderr=%q log=%q", code, result, saved, stderr.String(), logText)
		}
	})
	t.Run(`JS: "feedback send: feedback_dir and feedback_to written by config set in a layer are read"`, func(t *testing.T) {
		report, env := tm10FeedbackFixture(t, nil)
		_, cwd := commandFixture(t)
		old, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(old) })
		fakeDir := t.TempDir()
		rules := []fakecli.Rule{{ArgvPrefix: true, Argv: []string{"agent", "prompt", "w9:p2"}, Stdout: `{"result":{"submitted":true}}`}, {AnyArgs: true}}
		if _, err := fakecli.Install(t, fakeDir, "herdr", rules); err != nil {
			t.Fatal(err)
		}
		configEnv := platform.Env{"HOME": t.TempDir(), "USERPROFILE": t.TempDir(), "XDG_CONFIG_HOME": filepath.Join(t.TempDir(), "config"), "HERDR_SOHO_SKILL_DIR": testSkillDir(t), "HERDR_SOHO_DIR": filepath.Join(t.TempDir(), "state"), "HERDR_WORKSPACE_ID": "ws", "HERDR_ENV": "1", "PATH": fakeDir, "HERDR_SOHO_FAKECLI_CONFIG": filepath.Join(fakeDir, "herdr.json"), "HERDR_SOCKET_PATH": filepath.Join(t.TempDir(), "missing.sock")}
		for _, args := range [][]string{{"config", "set", "feedback_dir", env.Get("HERDR_SOHO_FEEDBACK_DIR")}, {"config", "set", "feedback_to", "w9:p2"}} {
			oldOut, oldErr := platform.Stdout, platform.Stderr
			var out, stderr bytes.Buffer
			platform.Stdout, platform.Stderr = &out, &stderr
			code := Run(args, configEnv)
			platform.Stdout, platform.Stderr = oldOut, oldErr
			if code != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), "set ") {
				t.Fatalf("args=%v code=%d out=%q err=%q", args, code, out.String(), stderr.String())
			}
		}
		feedbackEnv := configEnv
		feedbackEnv["HERDR_SOHO_FEEDBACK"] = "local"
		oldOut, oldErr := platform.Stdout, platform.Stderr
		var out, stderr bytes.Buffer
		platform.Stdout, platform.Stderr = &out, &stderr
		code := Run([]string{"feedback", "send", report, "summary"}, feedbackEnv)
		platform.Stdout, platform.Stderr = oldOut, oldErr
		if code != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), `"notified":"w9:p2"`) {
			t.Fatalf("code=%d out=%q err=%q", code, out.String(), stderr.String())
		}
	})
}
