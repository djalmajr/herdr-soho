package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func releasedForFixture(t *testing.T, family string, extra ...fakecli.Rule) *dispatchArrivalFixture {
	t.Helper()
	f := newDispatchArrivalFixture(t, "idle", 1, 1, "", "0", extra...)
	role := filepath.Join(f.root, "roles", "reviewer.md")
	if err := os.WriteFile(role, []byte("---\nname: Reviewer\nmode: review\n---\nReview.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	roster := "worker\tw0test:p0a\t" + map[bool]string{true: "codex", false: "claude"}[family == "openai"] + "\treviewer\t" + family + "\t0\t\tnow\tmodel-x\ttask\treviewer\n"
	if err := os.WriteFile(filepath.Join(f.state, "ws", "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func addReleasedForSidecar(t *testing.T, f *dispatchArrivalFixture, agent, stamp, kind, model, submission string, tmp bool) {
	t.Helper()
	dir := filepath.Join(f.state, "ws", "briefs")
	if tmp {
		dir = filepath.Join(f.env.Get("TMPDIR"), "herdr-soho", "ws", "reports")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stem := agent + "-" + stamp
	if err := os.WriteFile(filepath.Join(dir, stem+".dispatch.json"), []byte(fmt.Sprintf("{\"version\":1,\"kind\":%q,\"model\":%q,\"effort\":\"\",\"submission\":%q}\n", kind, model, submission)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchForReleasedCommandCases(t *testing.T) {
	t.Run("released author with coherent accepted sidecars resolves its family", func(t *testing.T) { // JS: "released author with coherent accepted sidecars resolves its family"
		f := releasedForFixture(t, "openai", fakecli.Rule{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: `{"result":{"agent":{"agent_status":"working","state_change_seq":2}}}`})
		addReleasedForSidecar(t, f, "build", "20260927T101001", "pi", "anthropic/claude-3", "accepted", false)
		addReleasedForSidecar(t, f, "build", "20260927T101002", "cursor", "claude-opus-4", "accepted", true)
		code, out, stderr := f.run(t, "worker", f.brief, "--for", "build", "--no-wait")
		if code != 0 || !strings.Contains(out, `"wait_status":"submitted"`) || stderr != "" {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
	})
	t.Run("released author with no accepted sidecar gets the usage diagnostic", func(t *testing.T) { // JS: "released author with no accepted sidecar gets the usage diagnostic"
		f := releasedForFixture(t, "openai")
		addReleasedForSidecar(t, f, "build", "20260927T101001", "claude", "", "failed", false)
		code, out, stderr := f.run(t, "worker", f.brief, "--for", "build", "--no-wait")
		want := "dispatch: --for 'build': not an agent in the roster, a family (anthropic|openai|xai|google), a kind with a fixed family, or an agent with an accepted dispatch recorded in this workspace"
		if code != 2 || out != "" || !strings.Contains(stderr, want) {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
	})
	t.Run("released author rejects disagreeing or unknown recorded families with counts", func(t *testing.T) { // JS: "released author rejects disagreeing or unknown recorded families with counts"
		f := releasedForFixture(t, "openai")
		addReleasedForSidecar(t, f, "build", "20260927T101001", "pi", "anthropic/claude-3", "accepted", false)
		addReleasedForSidecar(t, f, "build", "20260927T101002", "codex", "", "accepted", true)
		code, _, stderr := f.run(t, "worker", f.brief, "--for", "build", "--no-wait")
		if code != 2 || !strings.Contains(stderr, "(anthropic ×1, openai ×1)") {
			t.Fatalf("disagreeing families: code=%d stderr=%q", code, stderr)
		}
		addReleasedForSidecar(t, f, "build", "20260927T101003", "mystery", "", "accepted", false)
		code, _, stderr = f.run(t, "worker", f.brief, "--for", "build", "--no-wait")
		if code != 2 || !strings.Contains(stderr, "(anthropic ×1, openai ×1, unknown ×1)") {
			t.Fatalf("unknown family: code=%d stderr=%q", code, stderr)
		}
	})
	t.Run("a reviewer is refused when a released author shares its family", func(t *testing.T) { // JS: "a reviewer is refused when a released author shares its family"
		f := releasedForFixture(t, "anthropic")
		addReleasedForSidecar(t, f, "build", "20260927T101001", "pi", "anthropic/claude-3", "accepted", false)
		code, _, stderr := f.run(t, "worker", f.brief, "--for", "build", "--no-wait")
		if code != 5 || !strings.Contains(stderr, "shares a model family with the slice's author(s): build (anthropic)") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	})
	t.Run("--for prefers a live roster entry over its recorded dispatches", func(t *testing.T) { // JS: "--for prefers a live roster entry over its recorded dispatches"
		f := releasedForFixture(t, "openai")
		roster := "worker\tw0test:p0a\tcodex\treviewer\topenai\t0\t\tnow\tmodel-x\ttask\treviewer\nbuild\tp0b\tclaude\timplementer\topenai\t1\t/tmp\tnow\t\tfull\timplementer\t"
		if err := os.WriteFile(filepath.Join(f.state, "ws", "agents.tsv"), []byte(roster+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		addReleasedForSidecar(t, f, "build", "20260927T101001", "claude", "", "accepted", false)
		code, _, stderr := f.run(t, "worker", f.brief, "--for", "build", "--no-wait")
		if code != 5 || !strings.Contains(stderr, "slice's author(s): build (openai)") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	})
	t.Run("released agent names match exactly before the timestamp", func(t *testing.T) { // JS: "released agent names match exactly before the timestamp"
		f := releasedForFixture(t, "openai")
		f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "0"
		addReleasedForSidecar(t, f, "build-2", "20260927T101001", "claude", "", "accepted", false)
		code, out, stderr := f.run(t, "worker", f.brief, "--for", "build", "--no-wait")
		if code != 2 || out != "" || !strings.Contains(stderr, "not an agent in the roster") {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
	})
}

type taskReportDispatchResult struct {
	Report     string `json:"report"`
	TaskReport string `json:"task_report"`
}

func readArrivalCalls(t *testing.T, f *dispatchArrivalFixture) []fakecli.Call {
	t.Helper()
	calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

func runArrivalCommand(t *testing.T, f *dispatchArrivalFixture, args ...string) (int, string, string) {
	t.Helper()
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	code := func() int {
		defer func() { platform.Stdout, platform.Stderr = oldOut, oldErr }()
		return Run(args, f.env)
	}()
	return code, out.String(), stderr.String()
}

func readTaskDispatchResult(t *testing.T, out string) taskReportDispatchResult {
	t.Helper()
	var value taskReportDispatchResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &value); err != nil {
		t.Fatalf("dispatch JSON %q: %v", out, err)
	}
	if value.Report == "" || value.TaskReport == "" {
		t.Fatalf("dispatch omitted report paths: %+v", value)
	}
	if strings.Index(out, `"task_report"`) <= strings.Index(out, `"report"`) {
		t.Fatalf("task_report key does not follow report: %s", out)
	}
	return value
}

func TestTaskReportCommandLifecycleCases(t *testing.T) {
	t.Run("dispatch-amend-done-collect works when all symlink creation APIs fail with EPERM", func(t *testing.T) { // JS: "dispatch-amend-done-collect works when all symlink creation APIs fail with EPERM"
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "0"
		fixedNow := platform.Now
		now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
		platform.Now = func() time.Time { value := now; now = now.Add(2 * time.Second); return value }
		t.Cleanup(func() { platform.Now = fixedNow })
		code, out, stderr := f.run(t, "worker", f.brief, "--no-wait")
		first := readTaskDispatchResult(t, out)
		if code != 0 || stderr != "" {
			t.Fatalf("first dispatch code=%d out=%q stderr=%q", code, out, stderr)
		}
		if err := os.WriteFile(first.Report, []byte("# original\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr = runArrivalCommand(t, f, "wait", "worker", "--timeout", "10000")
		if result := readWaitCommandResult(t, out); code != 0 || stderr != "" || result.Status != "done" {
			t.Fatalf("first wait code=%d result=%+v stderr=%q", code, result, stderr)
		}
		code, out, stderr = f.run(t, "worker", f.brief, "--no-wait", "--amend")
		second := readTaskDispatchResult(t, out)
		if code != 0 || stderr != "" || second.TaskReport != first.TaskReport {
			t.Fatalf("amend code=%d first=%+v second=%+v stderr=%q", code, first, second, stderr)
		}
		if err := os.WriteFile(second.Report, []byte("# amended\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr = runArrivalCommand(t, f, "wait", "worker", "--timeout", "10000")
		if result := readWaitCommandResult(t, out); code != 0 || stderr != "" || result.Status != "done" {
			t.Fatalf("amend wait code=%d result=%+v stderr=%q", code, result, stderr)
		}
		code, out, stderr = runArrivalCommand(t, f, "collect", "worker")
		content, readErr := os.ReadFile(first.TaskReport)
		info, statErr := os.Lstat(first.TaskReport)
		if code != 0 || stderr != "" || readErr != nil || string(content) != "# amended\n" || statErr != nil || info.Mode()&os.ModeSymlink != 0 || !strings.Contains(out, "<!-- task report: "+first.TaskReport+" -->") {
			t.Fatalf("collect code=%d out=%q stderr=%q content=%q read=%v lstat=%v", code, out, stderr, content, readErr, statErr)
		}
	})
	t.Run("task report is stable across two amendments, wait and collect; clean and stats ignore the copy and pointer", func(t *testing.T) { // JS: "task report is stable across two amendments, wait and collect; clean and stats ignore the copy and pointer"
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		f.env["HERDR_SOHO_WAIT_POLL_MS"] = "1"
		f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "0"
		fixedNow := platform.Now
		now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
		platform.Now = func() time.Time { value := now; now = now.Add(2 * time.Second); return value }
		t.Cleanup(func() { platform.Now = fixedNow })

		code, out, stderr := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || stderr != "" {
			t.Fatalf("first dispatch code=%d out=%q stderr=%q calls=%#v", code, out, stderr, readArrivalCalls(t, f))
		}
		first := readTaskDispatchResult(t, out)
		pointerPath := filepath.Join(f.state, "ws", "task-report-worker.json")
		stable := first.TaskReport
		if _, err := os.Stat(stable); !os.IsNotExist(err) {
			t.Fatalf("active task stable copy exists or stat failed: %v", err)
		}
		var activePointer struct {
			Current    string   `json:"current"`
			History    []string `json:"history"`
			TaskReport string   `json:"task_report"`
		}
		pointerBytes, pointerErr := os.ReadFile(pointerPath)
		if pointerErr == nil {
			pointerErr = json.Unmarshal(pointerBytes, &activePointer)
		}
		if pointerErr != nil || activePointer.Current != first.Report || activePointer.TaskReport != stable || len(activePointer.History) != 0 {
			t.Fatalf("pointer=%q decoded=%+v err=%v", pointerBytes, activePointer, pointerErr)
		}
		if err := os.WriteFile(first.Report, []byte("# first report\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr = runArrivalCommand(t, f, "wait", "worker", "--timeout", "10000")
		waitResult := readWaitCommandResult(t, out)
		if code != 0 || stderr != "" || waitResult.Status != "done" || waitResult.TaskReport != stable {
			t.Fatalf("first wait code=%d out=%q stderr=%q", code, out, stderr)
		}
		if got, err := os.ReadFile(stable); err != nil || string(got) != "# first report\n" {
			t.Fatalf("stable=%q err=%v", got, err)
		}

		code, out, stderr = f.run(t, "worker", f.brief, "--no-wait", "--amend")
		if code != 0 || stderr != "" {
			t.Fatalf("amend one code=%d out=%q stderr=%q", code, out, stderr)
		}
		second := readTaskDispatchResult(t, out)
		if second.TaskReport != stable {
			t.Fatalf("stable changed: %q != %q", second.TaskReport, stable)
		}
		if _, err := os.Stat(stable); !os.IsNotExist(err) {
			t.Fatalf("amendment retained stale stable copy or stat failed: %v", err)
		}
		if err := os.WriteFile(second.Report, []byte("# amendment one\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr = f.run(t, "worker", f.brief, "--no-wait", "--amend")
		if code != 0 || stderr != "" {
			t.Fatalf("amend two code=%d out=%q stderr=%q", code, out, stderr)
		}
		third := readTaskDispatchResult(t, out)
		if third.TaskReport != stable {
			t.Fatalf("stable changed on second amendment: %q", third.TaskReport)
		}
		var pointer struct {
			Version    int      `json:"version"`
			TaskReport string   `json:"task_report"`
			Current    string   `json:"current"`
			History    []string `json:"history"`
		}
		if err := json.Unmarshal([]byte(mustRead(t, pointerPath)), &pointer); err != nil {
			t.Fatal(err)
		}
		if pointer.Version != 1 || pointer.TaskReport != stable || pointer.Current != third.Report || len(pointer.History) != 2 || pointer.History[0] != first.Report || pointer.History[1] != second.Report {
			t.Fatalf("pointer after amendments: %+v", pointer)
		}
		if err := os.WriteFile(third.Report, []byte("# amendment two\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr = runArrivalCommand(t, f, "wait", "worker", "--timeout", "10000")
		waitResult = readWaitCommandResult(t, out)
		if code != 0 || stderr != "" || waitResult.Report != third.Report || waitResult.TaskReport != stable {
			t.Fatalf("final wait code=%d out=%q stderr=%q", code, out, stderr)
		}
		if got, err := os.ReadFile(stable); err != nil || string(got) != "# amendment two\n" {
			t.Fatalf("stable=%q err=%v", got, err)
		}
		for _, path := range []string{first.Report, second.Report, third.Report} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("versioned report missing %s: %v", path, err)
			}
		}
		stableInfo, err := os.Stat(stable)
		if err != nil {
			t.Fatal(err)
		}
		stableMtime := stableInfo.ModTime()
		collectedCode, collected, collectedErr := runArrivalCommand(t, f, "collect", "worker")
		if collectedCode != 0 || collectedErr != "" || !strings.HasPrefix(collected, "<!-- report: "+third.Report+" -->\n<!-- task report: "+stable+" -->\n") {
			t.Fatalf("collect code=%d out=%q stderr=%q", collectedCode, collected, collectedErr)
		}
		collectedInfo, err := os.Stat(stable)
		if err != nil || !collectedInfo.ModTime().Equal(stableMtime) {
			t.Fatalf("collect rewrote stable snapshot: before=%v after=%v err=%v", stableMtime, collectedInfo, err)
		}
		old := time.Now().Add(-72 * time.Hour)
		if err := os.Chtimes(stable, old, old); err != nil {
			t.Fatal(err)
		}
		cleanCode, cleanOut, cleanErr := runArrivalCommand(t, f, "clean", "--older-than", "0")
		if cleanCode != 0 || cleanErr != "" {
			t.Fatalf("clean code=%d out=%q stderr=%q", cleanCode, cleanOut, cleanErr)
		}
		if _, err := os.Stat(stable); err != nil {
			t.Fatalf("clean removed stable snapshot: %v", err)
		}
		if _, err := os.Stat(pointerPath); err != nil {
			t.Fatalf("clean removed task pointer: %v", err)
		}
		statsCode, statsOut, statsErr := runArrivalCommand(t, f, "stats", "--json")
		var stats struct {
			Roles map[string]struct {
				Tasks      int `json:"tasks"`
				Amendments int `json:"amendments"`
			} `json:"roles"`
		}
		if statsCode != 0 || statsErr != "" || json.Unmarshal([]byte(statsOut), &stats) != nil || stats.Roles["implementer"].Tasks != 1 || stats.Roles["implementer"].Amendments != 2 {
			t.Fatalf("stats code=%d out=%q stderr=%q stats=%+v", statsCode, statsOut, statsErr, stats)
		}
		// The old mtime intentionally makes clean's retention behavior observable; the stable content must remain exact.
		if got, err := os.ReadFile(stable); err != nil || string(got) != "# amendment two\n" {
			t.Fatalf("stable after clean=%q err=%v", got, err)
		}
	})
	t.Run("--amend creates a task pointer for a legacy task without one", func(t *testing.T) { // JS: "--amend creates a task pointer for a legacy task without one"
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		f.env["HERDR_SOHO_WAIT_POLL_MS"] = "1"
		f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "0"
		fixedNow := platform.Now
		now := time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC)
		platform.Now = func() time.Time { value := now; now = now.Add(2 * time.Second); return value }
		t.Cleanup(func() { platform.Now = fixedNow })
		oldReport := filepath.Join(f.state, "ws", "reports", "worker-20260926T101010.md")
		if err := os.WriteFile(oldReport, []byte("# legacy version\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.state, "ws", "last-report-worker"), []byte(oldReport+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := f.run(t, "worker", f.brief, "--no-wait", "--amend")
		if code != 0 || stderr != "" {
			t.Fatalf("amend code=%d out=%q stderr=%q", code, out, stderr)
		}
		amended := readTaskDispatchResult(t, out)
		wantStable := filepath.Join(f.state, "ws", "reports", strings.TrimSuffix(filepath.Base(amended.Report), ".md")+".current.md")
		if amended.TaskReport != wantStable {
			t.Fatalf("task report=%q want=%q", amended.TaskReport, wantStable)
		}
		var pointer struct {
			TaskReport string   `json:"task_report"`
			Current    string   `json:"current"`
			History    []string `json:"history"`
		}
		if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(f.state, "ws", "task-report-worker.json"))), &pointer); err != nil {
			t.Fatal(err)
		}
		if pointer.TaskReport != wantStable || pointer.Current != amended.Report || len(pointer.History) != 1 || pointer.History[0] != oldReport {
			t.Fatalf("legacy pointer=%+v", pointer)
		}
		if err := os.WriteFile(amended.Report, []byte("# legacy task amended\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr = runArrivalCommand(t, f, "wait", "worker", "--timeout", "10000")
		waitResult := readWaitCommandResult(t, out)
		if code != 0 || stderr != "" || waitResult.TaskReport != wantStable {
			t.Fatalf("wait code=%d out=%q stderr=%q", code, out, stderr)
		}
		if got, err := os.ReadFile(wantStable); err != nil || string(got) != "# legacy task amended\n" {
			t.Fatalf("stable=%q err=%v", got, err)
		}
		if got, err := os.ReadFile(oldReport); err != nil || string(got) != "# legacy version\n" {
			t.Fatalf("old report=%q err=%v", got, err)
		}
	})
}

func TestTaskReportNewTaskCommandCase(t *testing.T) {
	t.Run("a new dispatch after a finished task gets its own stable copy and leaves the previous one", func(t *testing.T) { // JS: "a new dispatch after a finished task gets its own stable copy and leaves the previous one"
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		f.env["HERDR_SOHO_WAIT_POLL_MS"] = "1"
		f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "0"
		fixedNow := platform.Now
		now := time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)
		platform.Now = func() time.Time { value := now; now = now.Add(2 * time.Second); return value }
		t.Cleanup(func() { platform.Now = fixedNow })
		code, out, stderr := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || stderr != "" {
			t.Fatalf("first dispatch code=%d out=%q stderr=%q", code, out, stderr)
		}
		first := readTaskDispatchResult(t, out)
		if err := os.WriteFile(first.Report, []byte("# task one\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr = runArrivalCommand(t, f, "wait", "worker", "--timeout", "10000")
		waitResult := readWaitCommandResult(t, out)
		if code != 0 || stderr != "" || waitResult.Status != "done" {
			t.Fatalf("first wait code=%d out=%q stderr=%q", code, out, stderr)
		}
		if got, err := os.ReadFile(first.TaskReport); err != nil || string(got) != "# task one\n" {
			t.Fatalf("first stable=%q err=%v", got, err)
		}
		code, out, stderr = f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || stderr != "" {
			t.Fatalf("second dispatch code=%d out=%q stderr=%q", code, out, stderr)
		}
		next := readTaskDispatchResult(t, out)
		if next.TaskReport == first.TaskReport {
			t.Fatalf("new task reused stable copy %q", next.TaskReport)
		}
		if got, err := os.ReadFile(first.TaskReport); err != nil || string(got) != "# task one\n" {
			t.Fatalf("previous stable=%q err=%v", got, err)
		}
		if _, err := os.Stat(next.TaskReport); !os.IsNotExist(err) {
			t.Fatalf("new stable exists before completion or stat failed: %v", err)
		}
		if err := os.WriteFile(next.Report, []byte("# task two\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr = runArrivalCommand(t, f, "wait", "worker", "--timeout", "10000")
		waitResult = readWaitCommandResult(t, out)
		if code != 0 || stderr != "" || waitResult.TaskReport != next.TaskReport {
			t.Fatalf("second wait code=%d out=%q stderr=%q", code, out, stderr)
		}
		if got, err := os.ReadFile(next.TaskReport); err != nil || string(got) != "# task two\n" {
			t.Fatalf("new stable=%q err=%v", got, err)
		}
		if got, err := os.ReadFile(first.TaskReport); err != nil || string(got) != "# task one\n" {
			t.Fatalf("previous stable overwritten: %q err=%v", got, err)
		}
	})
}
