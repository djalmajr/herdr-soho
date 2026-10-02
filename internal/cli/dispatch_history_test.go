package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/dispatch"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// s72: the durable task link of the dispatch sidecars (task_report +
// brief_sha256) and the one-send-once guard — the same brief for the agent's
// open task is never sent twice.

func briefSHA256File(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func readSidecarFields(t *testing.T, f *dispatchArrivalFixture, composed string) map[string]any {
	t.Helper()
	var side map[string]any
	data, err := os.ReadFile(dispatch.DispatchSidecar(composed))
	if err != nil || json.Unmarshal(data, &side) != nil {
		t.Fatalf("sidecar=%q err=%v", data, err)
	}
	return side
}

func readPointerTaskReport(t *testing.T, f *dispatchArrivalFixture, agent string) string {
	t.Helper()
	var pointer struct {
		TaskReport string `json:"task_report"`
	}
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(f.state, "ws", "task-report-"+agent+".json"))), &pointer); err != nil {
		t.Fatalf("pointer parse: %v", err)
	}
	return pointer.TaskReport
}

// firstDispatch runs a plain --no-wait dispatch with the arrival check off
// and returns its composed brief and report paths.
func firstDispatch(t *testing.T, f *dispatchArrivalFixture) (composed, report string) {
	t.Helper()
	f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "0"
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
	if code != 0 {
		t.Fatalf("first dispatch code=%d out=%s stderr=%s", code, out, errText)
	}
	composed, _ = dispatchOutputField(t, out, "composed_prompt")
	report, _ = dispatchOutputField(t, out, "report")
	return composed, report
}

func TestDispatchHistorySidecarFields(t *testing.T) {
	t.Run("dispatch: the sidecar carries the pointer's task_report and the brief's brief_sha256", func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		composed, _ := firstDispatch(t, f)
		side := readSidecarFields(t, f, composed)
		sha := briefSHA256File(t, f.brief)
		taskReport := readPointerTaskReport(t, f, "worker")
		if side["submission"] != "accepted" || side["kind"] != "codex" || side["model"] != "gpt-5" || side["effort"] != "high" || side["session"] != "now" {
			t.Fatalf("sidecar changed fields: %v", side)
		}
		if side["task_report"] != taskReport {
			t.Fatalf("sidecar task_report=%v, want the pointer's %q: %v", side["task_report"], taskReport, side)
		}
		if side["brief_sha256"] != sha {
			t.Fatalf("sidecar brief_sha256=%v, want %q: %v", side["brief_sha256"], sha, side)
		}
	})
	t.Run("dispatch: an amendment's sidecar keeps the current task's task_report", func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "0"
		firstDispatch(t, f)
		firstTask := readPointerTaskReport(t, f, "worker")
		amend := filepath.Join(f.root, "amend.md")
		if err := os.WriteFile(amend, []byte("# Amendment to your current brief\n\nChange it.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, errText := f.run(t, "worker", amend, "--amend", "--no-wait")
		if code != 0 {
			t.Fatalf("amend code=%d out=%s stderr=%s", code, out, errText)
		}
		secondComposed, _ := dispatchOutputField(t, out, "composed_prompt")
		side := readSidecarFields(t, f, secondComposed)
		if side["task_report"] != firstTask {
			t.Fatalf("amendment sidecar task_report=%v, want the current task's %q: %v", side["task_report"], firstTask, side)
		}
		if side["brief_sha256"] != briefSHA256File(t, amend) {
			t.Fatalf("amendment sidecar brief_sha256=%v: %v", side["brief_sha256"], side)
		}
		if readPointerTaskReport(t, f, "worker") != firstTask {
			t.Fatal("the amendment repointed the task report")
		}
	})
}

func TestDispatchHistoryOneSendOnce(t *testing.T) {
	t.Run("dispatch: the same open brief is not sent again; the wait returns the original's report", func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		f.env["HERDR_SOHO_WAIT_POLL_MS"] = "50"
		original, firstReport := firstDispatch(t, f)
		// The worker settles while the second dispatch waits.
		go func() {
			time.Sleep(400 * time.Millisecond)
			if err := os.WriteFile(firstReport, []byte("# Report\n\nDone.\n"), 0o600); err != nil {
				t.Error(err)
			}
		}()
		code, out, errText := f.run(t, "worker", f.brief, "--timeout", "15000")
		if code != 0 || dispatchOutputStatus(t, out) != "done" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		want := "herdr-soho: dispatch: worker already has this brief (" + original + "); waiting on it without sending again (--resend sends it again)"
		if !strings.Contains(errText, want) {
			t.Fatalf("stderr missing %q: %s", want, errText)
		}
		if dup, _ := dispatchOutputField(t, out, "duplicate_of"); dup != original {
			t.Fatalf("duplicate_of=%q, want %q: %s", dup, original, out)
		}
		if report, _ := dispatchOutputField(t, out, "report"); report != firstReport {
			t.Fatalf("report=%q, want the original's %q: %s", report, firstReport, out)
		}
		if taskReport, _ := dispatchOutputField(t, out, "task_report"); taskReport != readPointerTaskReport(t, f, "worker") {
			t.Fatalf("task_report=%q: %s", taskReport, out)
		}
		if got := countDispatchCalls(mustCalls(t, f), "prompt"); got != 1 {
			t.Fatalf("agent prompt calls=%d, want 1 (the second dispatch must not send): %v", got, mustCalls(t, f))
		}
		// No new composed brief: only the original's stays under briefs.
		entries, err := os.ReadDir(filepath.Join(f.state, "ws", "briefs"))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "worker-") && strings.HasSuffix(e.Name(), ".md") && e.Name() != filepath.Base(original) {
				t.Fatalf("a new composed brief was created: %s", e.Name())
			}
		}
	})
	t.Run("dispatch: the same open brief with --no-wait exits 0 with duplicate_of", func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		original, _ := firstDispatch(t, f)
		code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "submitted" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if dup, _ := dispatchOutputField(t, out, "duplicate_of"); dup != original {
			t.Fatalf("duplicate_of=%q, want %q: %s", dup, original, out)
		}
		if got := countDispatchCalls(mustCalls(t, f), "prompt"); got != 1 {
			t.Fatalf("agent prompt calls=%d, want 1: %v", got, mustCalls(t, f))
		}
	})
	t.Run("dispatch: --resend sends the same brief again", func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		original, _ := firstDispatch(t, f)
		code, out, errText := f.run(t, "worker", f.brief, "--resend", "--no-wait")
		if code != 0 {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if strings.Contains(errText, "already has this brief") {
			t.Fatalf("--resend must not report a duplicate: %s", errText)
		}
		if dup, ok := dispatchOutputField(t, out, "duplicate_of"); ok {
			t.Fatalf("--resend output must not carry duplicate_of %q: %s", dup, out)
		}
		newComposed, _ := dispatchOutputField(t, out, "composed_prompt")
		if newComposed == original {
			t.Fatalf("--resend must create a fresh composed brief, got the original %q: %s", newComposed, out)
		}
		if got := countDispatchCalls(mustCalls(t, f), "prompt"); got != 2 {
			t.Fatalf("agent prompt calls=%d, want 2: %v", got, mustCalls(t, f))
		}
	})
	t.Run("dispatch: a not-received original is resent", func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		original, _ := firstDispatch(t, f)
		sidePath := dispatch.DispatchSidecar(original)
		var side map[string]any
		if data, err := os.ReadFile(sidePath); err != nil || json.Unmarshal(data, &side) != nil {
			t.Fatalf("sidecar=%q err=%v", data, err)
		}
		side["arrival"] = "not-received"
		data, _ := json.Marshal(side)
		if err := os.WriteFile(sidePath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if strings.Contains(errText, "already has this brief") {
			t.Fatalf("a not-received original must be resent, not reported as a duplicate: %s", errText)
		}
		newComposed, _ := dispatchOutputField(t, out, "composed_prompt")
		if newComposed == original {
			t.Fatalf("a resent brief must get a fresh composed brief, got the original %q: %s", newComposed, out)
		}
		if got := countDispatchCalls(mustCalls(t, f), "prompt"); got != 2 {
			t.Fatalf("agent prompt calls=%d, want 2: %v", got, mustCalls(t, f))
		}
	})
	t.Run("dispatch: an original with a report is sent as a new task", func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		original, firstReport := firstDispatch(t, f)
		if err := os.WriteFile(firstReport, []byte("# Report\n\nDone.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
		if code != 0 {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if strings.Contains(errText, "already has this brief") {
			t.Fatalf("a reported original closes the task; the same brief starts a new one: %s", errText)
		}
		newComposed, _ := dispatchOutputField(t, out, "composed_prompt")
		if newComposed == original {
			t.Fatalf("a new task must get a fresh composed brief, got the original %q: %s", newComposed, out)
		}
		// The new task gets its own task_report: the old sidecar stays a
		// member of the old task and never matches the new brief's dispatch.
		newSide := readSidecarFields(t, f, newComposed)
		oldSide := readSidecarFields(t, f, original)
		if newSide["task_report"] == oldSide["task_report"] {
			t.Fatalf("new task reuses the old task_report %v", newSide)
		}
		if got := countDispatchCalls(mustCalls(t, f), "prompt"); got != 2 {
			t.Fatalf("agent prompt calls=%d, want 2: %v", got, mustCalls(t, f))
		}
	})
	t.Run("dispatch: a brief with different content is sent", func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		original, _ := firstDispatch(t, f)
		other := filepath.Join(f.root, "other.md")
		if err := os.WriteFile(other, []byte("# Goal\n\nA different task.\n# Expected result\n\nObserved.\n# Owned files\n\nnone\n# Forbidden\n\nNo commit or push.\n# Report\n\ndone\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, errText := f.run(t, "worker", other, "--no-wait")
		if code != 0 {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if strings.Contains(errText, "already has this brief") {
			t.Fatalf("different content is a new task, not a duplicate: %s", errText)
		}
		newComposed, _ := dispatchOutputField(t, out, "composed_prompt")
		if newComposed == original {
			t.Fatalf("a different brief must get a fresh composed brief, got the original %q: %s", newComposed, out)
		}
		if got := countDispatchCalls(mustCalls(t, f), "prompt"); got != 2 {
			t.Fatalf("agent prompt calls=%d, want 2: %v", got, mustCalls(t, f))
		}
	})
	t.Run("dispatch: an open amendment with the same content is not sent again", func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		f.env["HERDR_SOHO_WAIT_POLL_MS"] = "50"
		firstDispatch(t, f)
		amend := filepath.Join(f.root, "amend.md")
		if err := os.WriteFile(amend, []byte("# Amendment to your current brief\n\nChange it.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, errText := f.run(t, "worker", amend, "--amend", "--no-wait")
		if code != 0 {
			t.Fatalf("amend code=%d out=%s stderr=%s", code, out, errText)
		}
		amendComposed, _ := dispatchOutputField(t, out, "composed_prompt")
		// The orchestrator re-sends the same amendment while the task is open.
		code, out, errText = f.run(t, "worker", amend, "--amend", "--no-wait")
		if code != 0 || dispatchOutputStatus(t, out) != "submitted" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if dup, _ := dispatchOutputField(t, out, "duplicate_of"); dup != amendComposed {
			t.Fatalf("duplicate_of=%q, want the open amendment's %q: %s", dup, amendComposed, out)
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &fields); err != nil {
			t.Fatalf("dispatch JSON %q: %v", out, err)
		}
		if am, ok := fields["amend"]; ok && am != true {
			t.Fatalf("amend=%v: %s", am, out)
		}
		if got := countDispatchCalls(mustCalls(t, f), "prompt"); got != 2 {
			t.Fatalf("agent prompt calls=%d, want 2 (the repeated amendment must not send): %v", got, mustCalls(t, f))
		}
	})
	t.Run("dispatch: a duplicate waits on the task's current report (the amendment's), not the matched member's", func(t *testing.T) {
		f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
		f.env["HERDR_SOHO_WAIT_POLL_MS"] = "50"
		original, _ := firstDispatch(t, f)
		amendFile := filepath.Join(f.root, "amend.md")
		if err := os.WriteFile(amendFile, []byte("# Amendment to your current brief\n\nChange it.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, second, _ := f.run(t, "worker", amendFile, "--amend", "--no-wait")
		if code != 0 {
			t.Fatalf("the amendment dispatch must exit 0, got %d", code)
		}
		amendComposed, _ := dispatchOutputField(t, second, "composed_prompt")
		amendReport, _ := dispatchOutputField(t, second, "report")
		if amendComposed == "" || amendReport == "" || amendComposed == original {
			t.Fatalf("amendment dispatched=%q report=%q, want a distinct pair", amendComposed, amendReport)
		}
		// The worker reports on the amendment's path — the task's current.
		reported := make(chan error, 1)
		go func() {
			time.Sleep(400 * time.Millisecond)
			reported <- os.WriteFile(amendReport, []byte("# Report\n\nDone.\n"), 0o600)
		}()
		// A plain dispatch with the original's content: no new prompt; the
		// wait ends when the amendment's report appears.
		code, out, errText := f.run(t, "worker", f.brief, "--timeout", "15000")
		if code != 0 || dispatchOutputStatus(t, out) != "done" {
			t.Fatalf("duplicate dispatch=%d status=%q stderr=%s", code, dispatchOutputStatus(t, out), errText)
		}
		if dup, _ := dispatchOutputField(t, out, "duplicate_of"); dup != original {
			t.Fatalf("duplicate_of=%q, want the original's composed %q", dup, original)
		}
		if report, _ := dispatchOutputField(t, out, "report"); report != amendReport {
			t.Fatalf("report=%q, want the amendment's current %q", report, amendReport)
		}
		if taskReport, _ := dispatchOutputField(t, out, "task_report"); taskReport != readPointerTaskReport(t, f, "worker") {
			t.Fatalf("task_report=%q, want the pointer's %q", taskReport, readPointerTaskReport(t, f, "worker"))
		}
		if !strings.Contains(errText, "already has this brief ("+original+")") {
			t.Fatalf("stderr=%q, want the duplicate line naming the original's composed", errText)
		}
		if got := countDispatchCalls(mustCalls(t, f), "prompt"); got != 2 {
			t.Fatalf("agent prompt calls=%d, want 2 (the original and the amendment only): %v", got, mustCalls(t, f))
		}
		if err := <-reported; err != nil {
			t.Fatal(err)
		}
		// No new composed brief: only the original's and the amendment's stay under briefs.
		entries, err := os.ReadDir(filepath.Join(f.state, "ws", "briefs"))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]bool{filepath.Base(original): true, filepath.Base(amendComposed): true}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "worker-") && strings.HasSuffix(e.Name(), ".md") {
				if !want[e.Name()] {
					t.Fatalf("a new composed brief was created: %s", e.Name())
				}
				delete(want, e.Name())
			}
		}
		if len(want) != 0 {
			t.Fatalf("composed briefs missing: %v", want)
		}
	})
}

// s72, emenda 2 (R-S72, revisão 026bc92): adapted from the reviewer's probes
// in review-4-20261002T114058.probe.go (TestReviewCompletedAmendment,
// TestReviewRawHash, TestReviewRestoredPointer). Before the three P2 fixes
// they fail; after, they pass.

// TestDispatchHistoryCompletedAmendment: a task closed through its current
// report (the amendment's) does not keep suppressing a plain send of the
// same content: the send happens as a new task.
func TestDispatchHistoryCompletedAmendment(t *testing.T) {
	f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
	firstDispatch(t, f)
	amend := filepath.Join(f.root, "amend.md")
	if err := os.WriteFile(amend, []byte("# Amendment to your current brief\nChange it.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errText := f.run(t, "worker", amend, "--amend", "--no-wait")
	if code != 0 {
		t.Fatalf("amend code=%d out=%s stderr=%s", code, out, errText)
	}
	current, _ := dispatchOutputField(t, out, "report")
	if err := os.WriteFile(current, []byte("completed task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errText = f.run(t, "worker", f.brief, "--no-wait")
	if code != 0 {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	if got := countDispatchCalls(mustCalls(t, f), "prompt"); got != 3 {
		t.Fatalf("agent prompt calls=%d, want 3 (the completed task must not block a new send): %v", got, mustCalls(t, f))
	}
	if strings.Contains(out, "duplicate_of") {
		t.Fatalf("the completed task was reported as a duplicate: %s", out)
	}
}

// TestDispatchHistoryRawHash: the sidecar's brief_sha256 is the digest of
// the brief's input bytes (raw, CRLF and all), not of the normalized text.
func TestDispatchHistoryRawHash(t *testing.T) {
	f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0")
	raw, err := os.ReadFile(f.brief)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.brief, []byte(strings.ReplaceAll(string(raw), "\n", "\r\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	composed, _ := firstDispatch(t, f)
	side := readSidecarFields(t, f, composed)
	want := briefSHA256File(t, f.brief)
	if side["brief_sha256"] != want {
		t.Fatalf("brief_sha256=%v, want the raw input bytes digest %q: %v", side["brief_sha256"], want, side)
	}
}

// TestDispatchHistoryRestoredPointer: a failed plain send restores the
// pointer to the previous task, and the failed sidecar records the restored
// pointer's task_report (absent without a restored pointer).
func TestDispatchHistoryRestoredPointer(t *testing.T) {
	f := newDispatchArrivalFixture(t, "idle", 1, 2, "", "0", fakecli.Rule{Argv: []string{"agent", "prompt", "worker"}, ArgvPrefix: true, Call: 2, Code: 7, Stderr: "refused second transport"})
	firstDispatch(t, f)
	prior := readPointerTaskReport(t, f, "worker")
	configPath := filepath.Join(f.bin, "herdr.json")
	cfgRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["rules"] = []fakecli.Rule{
		{Argv: []string{"agent", "prompt", "worker"}, ArgvPrefix: true, Code: 7, Stderr: "refused transport"},
		{Argv: []string{"agent", "list"}, ArgvPrefix: true, Stdout: `{"result":{"agents":[]}}`},
	}
	cfgRaw, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, cfgRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.brief, []byte("# Goal\nA different task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
	composed, _ := dispatchOutputField(t, out, "composed_prompt")
	side := readSidecarFields(t, f, composed)
	current := readPointerTaskReport(t, f, "worker")
	t.Logf("code=%d prior=%s restored=%s failed_sidecar=%v", code, prior, current, side)
	if code != 4 || current != prior || side["submission"] != "failed" {
		t.Fatalf("restoration fixture failed: code=%d prior=%s restored=%s sidecar=%v out=%s stderr=%s", code, prior, current, side, out, errText)
	}
	if side["task_report"] != current {
		t.Fatalf("failed sidecar task_report=%v, want the restored pointer's %q: %v", side["task_report"], current, side)
	}
}
