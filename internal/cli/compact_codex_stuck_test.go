package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// fakeFastClock makes every platform.Now call jump step ahead, so a window
// bounded by platform.Now expires after its first poll without any real
// sleep.
func fakeFastClock(t *testing.T, step time.Duration) {
	t.Helper()
	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	fixedNow := platform.Now
	platform.Now = func() time.Time {
		value := base
		base = base.Add(step)
		return value
	}
	t.Cleanup(func() { platform.Now = fixedNow })
}

// The codex composer screens: the box holds the typed /compact (stuck), the
// placeholder of an idle box (cleared), and the executed command (the echo
// in the history, the proof below it, the empty composer at the bottom).
const (
	codexStuckComposer    = "earlier reply\n› /compact\n"
	codexClearedComposer  = "earlier reply\n› Ask Codex to do anything\n"
	codexExecutedComposer = "earlier reply\n› /compact\nContext compacted\n› Ask Codex to do anything\n"
)

// TestCompactCodexStuckComposerExits4 reproduces the rc.13 report: a codex
// whose composer still shows /compact after the Enter (an Enter that landed
// before the TUI drew the command) must not wait out the full timeout. The
// confirm sees the line still in the box after two Enters, clears it with
// ctrl+u and exits 4, with no result JSON on stdout.
func TestCompactCodexStuckComposerExits4(t *testing.T) {
	fakeFastClock(t, 6*time.Second)
	f := newCompactFixture(t, "codex", []fakecli.Rule{
		// The pre-send state check and the two confirms' liveness checks.
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 3, Stdout: compactStateJSON("idle", 1)},
		// 1: pre-send; 2: the first proof poll, the line still in the box;
		// 3 and 4: the confirms after each Enter, still stuck.
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: codexClearedComposer},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: codexStuckComposer},
		{Argv: compactReadArgv("worker"), Call: 3, Stdout: codexStuckComposer},
		{Argv: compactReadArgv("worker"), Call: 4, Stdout: codexStuckComposer},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "ctrl+u"}, Stdout: `{"result":{}}`},
	})
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 4 {
		t.Fatalf("code=%d want 4 out=%s stderr=%s", code, out, errText)
	}
	if out != "" {
		t.Fatalf("stdout must be empty (no result JSON on the stuck exit): %s", out)
	}
	want := "compact: '/compact' stayed in the composer of 'worker' after two Enters; cleared it, nothing was compacted"
	if !strings.Contains(errText, want) {
		t.Fatalf("stderr=%q want %q", errText, want)
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-text", "p1", "/compact"}); n != 1 {
		t.Fatalf("send-text calls=%d want 1: %#v", n, calls)
	}
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "Enter"}); n != 2 {
		t.Fatalf("Enter calls=%d want 2 (the confirm retries once): %#v", n, calls)
	}
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "ctrl+u"}); n != 1 {
		t.Fatalf("ctrl+u calls=%d want 1 (the box is cleared): %#v", n, calls)
	}
	if n := countArgv(calls, compactReadArgv("worker")); n != 4 {
		t.Fatalf("recent reads=%d want 4 (pre-send, first poll, one per confirm): %#v", n, calls)
	}
}

// TestDispatchCompactCodexStuckComposerKeepsBriefUnsent verifies the same
// stuck composer inside a dispatch: the compact step exits 4, the dispatch
// says the phase stopped (exit 4) and the brief was not sent, and no task
// state points at a report that will never come.
func TestDispatchCompactCodexStuckComposerKeepsBriefUnsent(t *testing.T) {
	fakeFastClock(t, 6*time.Second)
	extra := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 3, Stdout: compactStateJSON("idle", 1)},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: codexClearedComposer},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: codexStuckComposer},
		{Argv: compactReadArgv("worker"), Call: 3, Stdout: codexStuckComposer},
		{Argv: compactReadArgv("worker"), Call: 4, Stdout: codexStuckComposer},
		{Argv: []string{"pane", "send-text", "w0test:p0a", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "w0test:p0a", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "w0test:p0a", "ctrl+u"}, Stdout: `{"result":{}}`},
	}
	f := newDispatchArrivalFixture(t, "working", 1, 2, "$CURRENT_PATHS", "0", extra...)
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait", "--compact")
	if code != 4 {
		t.Fatalf("code=%d want 4 out=%s stderr=%s", code, out, errText)
	}
	// No JSON line at all: the dispatch stops in the compact phase, before
	// its own result line.
	if out != "" {
		t.Fatalf("stdout must be empty before the stuck exit: %s", out)
	}
	want := "the compact step of 'worker' did not finish (exit 4); the brief was not sent"
	if !strings.Contains(errText, want) {
		t.Fatalf("stderr=%q want %q", errText, want)
	}
	stuckWant := "compact: '/compact' stayed in the composer of 'worker' after two Enters; cleared it, nothing was compacted"
	if !strings.Contains(errText, stuckWant) {
		t.Fatalf("stderr=%q want %q", errText, stuckWant)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(f.bin, "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := countArgvPrefix(calls, []string{"agent", "prompt", "worker"}); n != 0 {
		t.Fatalf("the brief was sent after the stuck composer exit: %#v", calls)
	}
	if n := countArgv(calls, []string{"pane", "send-keys", "w0test:p0a", "Enter"}); n != 2 {
		t.Fatalf("Enter calls=%d want 2: %#v", n, calls)
	}
	if n := countArgv(calls, []string{"pane", "send-keys", "w0test:p0a", "ctrl+u"}); n != 1 {
		t.Fatalf("ctrl+u calls=%d want 1: %#v", n, calls)
	}
	if _, err := os.Stat(filepath.Join(f.state, "ws", "last-report-worker")); !os.IsNotExist(err) {
		t.Fatalf("last-report was written before the stuck composer exit: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(f.state, "ws", "briefs")); len(entries) != 0 {
		t.Fatalf("a composed brief was written before the stuck composer exit: %v", entries)
	}
}

// TestDispatchCompactCodexRunsOnSecondEnter verifies the codex whose first
// Enter did not take the command but whose second one runs it: the compact
// completes on the second Enter (no ctrl+u) and the dispatch goes on to send
// the brief.
func TestDispatchCompactCodexRunsOnSecondEnter(t *testing.T) {
	fakeFastClock(t, 6*time.Second)
	extra := []fakecli.Rule{
		// 1: compact pre-send check, 2: the first confirm's liveness check,
		// 3: the idle wait after the proof, 4: the dispatch's before-state.
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 3, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 4, Stdout: compactStateJSON("idle", 1)},
		// 1: pre-send, 2: /compact still in the box on the first proof poll,
		// 3: still stuck on the first confirm, 4: the box cleared and the
		// proof on screen after the second Enter.
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: codexClearedComposer},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: codexStuckComposer},
		{Argv: compactReadArgv("worker"), Call: 3, Stdout: codexStuckComposer},
		{Argv: compactReadArgv("worker"), Call: 4, Stdout: codexExecutedComposer},
		{Argv: []string{"pane", "send-text", "w0test:p0a", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "w0test:p0a", "Enter"}, Stdout: `{"result":{}}`},
	}
	f := newDispatchArrivalFixture(t, "working", 1, 2, "$CURRENT_PATHS", "0", extra...)
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait", "--compact")
	if code != 0 {
		t.Fatalf("code=%d want 0 out=%s stderr=%s", code, out, errText)
	}
	if status := dispatchLastLineStatus(t, out); status != "submitted" {
		t.Fatalf("wait_status=%s want submitted out=%s", status, out)
	}
	// One JSON line on stdout (the dispatch's); the compaction is a stderr note.
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("stdout must hold only the dispatch line: %s", out)
	}
	if !strings.Contains(errText, "dispatch: compacted 'worker' in ") {
		t.Fatalf("missing the compacted note on stderr: %s", errText)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(f.bin, "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := countArgv(calls, []string{"pane", "send-text", "w0test:p0a", "/compact"}); n != 1 {
		t.Fatalf("send-text calls=%d want 1: %#v", n, calls)
	}
	if n := countArgv(calls, []string{"pane", "send-keys", "w0test:p0a", "Enter"}); n != 2 {
		t.Fatalf("Enter calls=%d want 2 (the second one ran the command): %#v", n, calls)
	}
	if n := countArgv(calls, []string{"pane", "send-keys", "w0test:p0a", "ctrl+u"}); n != 0 {
		t.Fatalf("ctrl+u calls=%d want 0 (the command ran, the box was not stuck past both Enters): %#v", n, calls)
	}
	if n := countArgvPrefix(calls, []string{"agent", "prompt", "worker"}); n != 1 {
		t.Fatalf("prompt calls=%d want 1 (the brief is sent after the compaction): %#v", n, calls)
	}
}

// TestCompactCodexNormalComposerSendsSingleEnter verifies that a normal codex
// (the box no longer holding /compact after the Enter) keeps the single-Enter
// flow: the first proof poll finds a cleared composer and the proof, with no
// second Enter, no ctrl+u, and no confirm reads.
func TestCompactCodexNormalComposerSendsSingleEnter(t *testing.T) {
	f := newCompactFixture(t, "codex", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: codexClearedComposer},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: codexExecutedComposer},
	})
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 0 || errText != "" {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
	}
	if value := compactJSON(t, out); value["status"] != "compacted" {
		t.Fatalf("json=%v", value)
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "Enter"}); n != 1 {
		t.Fatalf("Enter calls=%d want 1 (a cleared composer takes no second Enter): %#v", n, calls)
	}
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "ctrl+u"}); n != 0 {
		t.Fatalf("ctrl+u calls=%d want 0: %#v", n, calls)
	}
	if n := countArgv(calls, compactReadArgv("worker")); n != 2 {
		t.Fatalf("recent reads=%d want 2 (pre-send plus the first proof poll): %#v", n, calls)
	}
}

// TestCompactCodexStaleProofStuckComposerExits4 covers the path that used to
// end in a silent exit 0: an earlier compaction left its /compact echo and
// proof on screen, so on the read before the TUI draws the new composer line
// the old proof sits below the last /compact line — the read the old code
// turned into a false "compacted" (exit 0, no JSON inside a dispatch) while
// the fresh /compact stayed typed in the box. The composer check runs before
// any proof is trusted: a /compact on the composer line exits 4 even with a
// stale proof on screen, and nothing was compacted.
func TestCompactCodexStaleProofStuckComposerExits4(t *testing.T) {
	fakeFastClock(t, 6*time.Second)
	// The stale screen an earlier compaction left (the echo and its proof).
	stale := "old reply\n› /compact\nContext compacted\n"
	// The new composer line, drawn after the second Enter, still holding
	// /compact.
	stuck := stale + "› /compact\n"
	f := newCompactFixture(t, "codex", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 2, Stdout: compactStateJSON("idle", 1)},
		{Argv: []string{"agent", "get", "worker"}, Call: 3, Stdout: compactStateJSON("idle", 1)},
		// 1: pre-send, the stale screen. 2: the first proof poll, the TUI has
		// not drawn the new composer line yet (the stale proof still sits
		// below the last /compact line — the old code's false "compacted"
		// read; the last › line, the old echo, reads as the composer). 3 and
		// 4: the confirms, the composer line drawn and still holding /compact.
		{Argv: compactReadArgv("worker"), Call: 1, Stdout: stale},
		{Argv: compactReadArgv("worker"), Call: 2, Stdout: stale},
		{Argv: compactReadArgv("worker"), Call: 3, Stdout: stuck},
		{Argv: compactReadArgv("worker"), Call: 4, Stdout: stuck},
		{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
		{Argv: []string{"pane", "send-keys", "p1", "ctrl+u"}, Stdout: `{"result":{}}`},
	})
	code, out, errText := f.run(t, "compact", "worker", "--timeout", "30000")
	if code != 4 {
		t.Fatalf("code=%d want 4 (the stale proof must not read as this compaction): out=%s stderr=%s", code, out, errText)
	}
	if out != "" {
		t.Fatalf("stdout must be empty on the stuck exit: %s", out)
	}
	want := "compact: '/compact' stayed in the composer of 'worker' after two Enters; cleared it, nothing was compacted"
	if !strings.Contains(errText, want) {
		t.Fatalf("stderr=%q want %q", errText, want)
	}
	calls := f.calls(t)
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "Enter"}); n != 2 {
		t.Fatalf("Enter calls=%d want 2: %#v", n, calls)
	}
	if n := countArgv(calls, []string{"pane", "send-keys", "p1", "ctrl+u"}); n != 1 {
		t.Fatalf("ctrl+u calls=%d want 1: %#v", n, calls)
	}
}
