package wait

// The wait's Enter retries (the not-received retry and the queued retry)
// now need the same composer proof as the dispatch: a recognized composer
// (the roster kind) holding the stored composed path, and no known
// trust/approval/question UI. A missing identity/path, an unrecognized
// screen, or an unrecognized kind keeps the delivery uncertain without
// keys, and a failed send-key result does not count as an attempt. The
// transport is the fake herdr installed by the shared queued probe
// fixture: a re-executed Go test binary, no scripts.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// claudeBoxScreen renders the claude input box (the fixture roster kind is
// claude) with the given composer line between the two '─' borders, the
// given history above and the given footer below.
func claudeBoxScreen(history, box, footer string) string {
	sep := strings.Repeat("─", 66)
	return history + sep + "\n" + box + "\n" + sep + "\n" + footer
}

// countEnterKeys counts the send-keys calls the probe sent.
func countEnterKeys(f *queuedProbeFixture) int {
	count := 0
	for _, call := range f.calls() {
		if len(call.Argv) >= 2 && call.Argv[0] == "agent" && call.Argv[1] == "send-keys" {
			count++
		}
	}
	return count
}

// TestWaitEnterRetryNeedsRecognizedComposer: the not-received marker holds
// the stored path and the screen shows the prompt text, but the screen
// carries no recognized claude composer (a plain line, no box). The old
// marker rule sent the Enter; the region rule keeps the delivery uncertain
// without keys — the marker stays and only positive evidence settles it.
func TestWaitEnterRetryNeedsRecognizedComposer(t *testing.T) {
	fixed := time.Unix(2_000_000_000, 0)
	oldNow := platform.Now
	platform.Now = func() time.Time { return fixed }
	t.Cleanup(func() { platform.Now = oldNow })
	prompt := "/tmp/worker-brief.md"
	f := newQueuedProbeFixture(t, "idle", "5", "Read the file "+prompt+" in full and execute it.\n", map[string]string{
		"not-received": "1999999900 5 " + prompt + "\n",
	})
	f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "15"
	if got := f.probe(""); got != "not-received" {
		t.Fatalf("probe=%q, want not-received without keys", got)
	}
	if n := countEnterKeys(f); n != 0 {
		t.Fatalf("an unrecognized screen got %d key(s); want none; calls=%#v", n, f.calls())
	}
	if _, err := os.Stat(f.marker("not-received")); err != nil {
		t.Fatalf("the marker must stay while the delivery is uncertain: %v", err)
	}
}

// TestWaitEnterRetryOnRecognizedComposerSendsEnter is the legitimate retry:
// the marker holds the stored path and the screen is the recognized claude
// box holding that exact path. After the marker's window the retry sends
// exactly one Enter and records the attempt.
func TestWaitEnterRetryOnRecognizedComposerSendsEnter(t *testing.T) {
	fixed := time.Unix(2_000_000_000, 0)
	oldNow := platform.Now
	platform.Now = func() time.Time { return fixed }
	t.Cleanup(func() { platform.Now = oldNow })
	prompt := "/tmp/worker-brief.md"
	f := newQueuedProbeFixture(t, "idle", "5", claudeBoxScreen("Welcome to the worker\n", "❯ Read the file "+prompt+" in full and execute it.", "  [Opus 5.5] 67% [main*]\n"), map[string]string{
		"not-received": "1999999900 5 " + prompt + "\n",
	})
	f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "15"
	if got := f.probe(""); got != "working" {
		t.Fatalf("probe=%q, want working after the retry Enter", got)
	}
	if n := countEnterKeys(f); n != 1 {
		t.Fatalf("send-keys calls=%d, want exactly one retry Enter; calls=%#v", n, f.calls())
	}
	if got, err := os.ReadFile(f.marker("enter-retry")); err != nil || strings.TrimSpace(string(got)) != "1 2000000000" {
		t.Fatalf("enter-retry=%q err=%v, want \"1 2000000000\"", got, err)
	}
	if _, err := os.Stat(f.marker("not-received")); err != nil {
		t.Fatalf("the marker must survive the retry: %v", err)
	}
}

// TestWaitEnterRetryRefusesKnownDialogUI: the composer holds the exact path
// and the screen also carries a known trust/approval/question UI (the path
// collision). The Enter would be the dialog's input: refused, the delivery
// stays not-received without keys.
func TestWaitEnterRetryRefusesKnownDialogUI(t *testing.T) {
	run := func(t *testing.T, kind, screen string) int {
		fixed := time.Unix(2_000_000_000, 0)
		oldNow := platform.Now
		platform.Now = func() time.Time { return fixed }
		t.Cleanup(func() { platform.Now = oldNow })
		prompt := "/tmp/worker-brief.md"
		f := newQueuedProbeFixture(t, "idle", "5", screen, map[string]string{
			"not-received": "1999999900 5 " + prompt + "\n",
		})
		if kind != "claude" {
			f.cursorRoster(t, kind)
		}
		f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "15"
		if got := f.probe(""); got != "not-received" {
			t.Fatalf("probe=%q, want not-received after the dialog refusal", got)
		}
		return countEnterKeys(f)
	}
	t.Run("the claude approval below the box with the path", func(t *testing.T) {
		if n := run(t, "claude", claudeBoxScreen("Welcome to the worker\n", "❯ Read the file /tmp/worker-brief.md in full and execute it.", "Do you want to make this edit?\n  1. Yes\n  2. No\n")); n != 0 {
			t.Fatalf("the dialog got %d key(s); want none", n)
		}
	})
	t.Run("the codex question above the composer with the path", func(t *testing.T) {
		if n := run(t, "codex", "Question: which file should I edit?\nenter to submit answer · esc to cancel\n› Read the file /tmp/worker-brief.md in full and execute it.\n"); n != 0 {
			t.Fatalf("the question dialog got %d key(s); want none", n)
		}
	})
}

// TestWaitEnterRetryHonorsFailedSendKey: the gate passes, but the send-key
// call fails. The failed call consumes one attempt and starts the retry
// interval, without claiming that the key landed.
func TestWaitEnterRetryHonorsFailedSendKey(t *testing.T) {
	fixed := time.Unix(2_000_000_000, 0)
	oldNow := platform.Now
	platform.Now = func() time.Time { return fixed }
	t.Cleanup(func() { platform.Now = oldNow })
	prompt := "/tmp/worker-brief.md"
	f := newQueuedProbeFixture(t, "idle", "5", claudeBoxScreen("Welcome to the worker\n", "❯ Read the file "+prompt+" in full and execute it.", "  [Opus 5.5] 67% [main*]\n"), map[string]string{
		"not-received": "1999999900 5 " + prompt + "\n",
	}, fakecli.Rule{Argv: []string{"agent", "send-keys", "worker", "enter"}, Code: 1, Stderr: `{"error":{"code":"send_failed","message":"the fake send-keys failed"}}`})
	f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "15"
	for probe := 1; probe <= 2; probe++ {
		if got := f.probe(""); got != "working" {
			t.Fatalf("probe %d=%q, want working (the failed key does not end the retry)", probe, got)
		}
		if raw, err := os.ReadFile(f.marker("enter-retry")); err != nil || strings.TrimSpace(string(raw)) != "1 2000000000" {
			t.Fatalf("probe %d: failed call must consume one bounded attempt: raw=%q err=%v", probe, raw, err)
		}
	}
	if n := countEnterKeys(f); n != 1 {
		t.Fatalf("send-keys calls=%d, want one while the retry interval has not elapsed; calls=%#v", n, f.calls())
	}
	if _, err := os.Stat(f.marker("not-received")); err != nil {
		t.Fatalf("the marker must stay: %v", err)
	}
}

// TestWaitEnterRetryWithoutStoredPathGetsNoKey: the marker carries no path
// (epoch and seq only) and there is no last-report to resolve one. The
// identity/path is missing: the delivery keeps uncertain without keys,
// even when the screen shows the marker text.
func TestWaitEnterRetryWithoutStoredPathGetsNoKey(t *testing.T) {
	fixed := time.Unix(2_000_000_000, 0)
	oldNow := platform.Now
	platform.Now = func() time.Time { return fixed }
	t.Cleanup(func() { platform.Now = oldNow })
	f := newQueuedProbeFixture(t, "idle", "5", "Read the file /tmp/worker-brief.md in full and execute it.\n", map[string]string{
		"not-received": "1999999900 5\n",
	})
	f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "15"
	if got := f.probe(""); got != "not-received" {
		t.Fatalf("probe=%q, want not-received without keys", got)
	}
	if n := countEnterKeys(f); n != 0 {
		t.Fatalf("a pathless marker got %d key(s); want none; calls=%#v", n, f.calls())
	}
	if _, err := os.Stat(f.marker("not-received")); err != nil {
		t.Fatalf("the marker must stay: %v", err)
	}
}

// TestWaitEnterRetryUnrecognizedKindGetsNoKey: the screen shows the prompt
// text and the marker holds the path, but the roster kind has no
// recognized composer rule. The kind is not positive proof: no key.
func TestWaitEnterRetryUnrecognizedKindGetsNoKey(t *testing.T) {
	fixed := time.Unix(2_000_000_000, 0)
	oldNow := platform.Now
	platform.Now = func() time.Time { return fixed }
	t.Cleanup(func() { platform.Now = oldNow })
	prompt := "/tmp/worker-brief.md"
	f := newQueuedProbeFixture(t, "idle", "5", "Read the file "+prompt+" in full and execute it.\n", map[string]string{
		"not-received": "1999999900 5 " + prompt + "\n",
	})
	f.cursorRoster(t, "grok")
	f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "15"
	if got := f.probe(""); got != "not-received" {
		t.Fatalf("probe=%q, want not-received without keys", got)
	}
	if n := countEnterKeys(f); n != 0 {
		t.Fatalf("an unrecognized kind got %d key(s); want none; calls=%#v", n, f.calls())
	}
}

// TestWaitQueuedRetryNeedsRecognizedComposer: the queued marker holds the
// path and the screen shows the prompt text, but there is no recognized
// composer. The queued retry stays not-received without keys (the
// not-received marker inherits the stored path for the next probe).
func TestWaitQueuedRetryNeedsRecognizedComposer(t *testing.T) {
	fixed := time.Unix(2_000_000_000, 0)
	oldNow := platform.Now
	platform.Now = func() time.Time { return fixed }
	t.Cleanup(func() { platform.Now = oldNow })
	prompt := "/tmp/worker-brief.md"
	f := newQueuedProbeFixture(t, "idle", "5", "Read the file "+prompt+" in full and execute it.\n", map[string]string{
		"queued":      "1 5 " + prompt + "\n",
		"enter-retry": "1 1234567890\n",
	})
	f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "15"
	if got := f.probe(""); got != "not-received" {
		t.Fatalf("probe=%q, want not-received without keys", got)
	}
	if n := countEnterKeys(f); n != 0 {
		t.Fatalf("an unrecognized screen got %d key(s); want none; calls=%#v", n, f.calls())
	}
	if got, err := os.ReadFile(f.marker("not-received")); err != nil || !strings.Contains(string(got), prompt) {
		t.Fatalf("the not-received marker must inherit the stored path: %q err=%v", got, err)
	}
	if _, err := os.Stat(f.marker("queued")); !os.IsNotExist(err) {
		t.Fatalf("the queued marker must clear: %v", err)
	}
}

// TestWaitQueuedRetryOnRecognizedComposerSendsEnter is the legitimate
// queued retry: the queued marker holds the path and the screen is the
// recognized claude box holding that exact path. The retry sends exactly
// one Enter and advances the attempt counter.
func TestWaitQueuedRetryOnRecognizedComposerSendsEnter(t *testing.T) {
	fixed := time.Unix(2_000_000_000, 0)
	oldNow := platform.Now
	platform.Now = func() time.Time { return fixed }
	t.Cleanup(func() { platform.Now = oldNow })
	prompt := "/tmp/worker-brief.md"
	f := newQueuedProbeFixture(t, "idle", "5", claudeBoxScreen("Welcome to the worker\n", "❯ Read the file "+prompt+" in full and execute it.", "  [Opus 5.5] 67% [main*]\n"), map[string]string{
		"queued":      "1 5 " + prompt + "\n",
		"enter-retry": "1 1234567890\n",
	})
	f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "15"
	if got := f.probe(""); got != "working" {
		t.Fatalf("probe=%q, want working after the retry Enter", got)
	}
	if n := countEnterKeys(f); n != 1 {
		t.Fatalf("send-keys calls=%d, want exactly one retry Enter; calls=%#v", n, f.calls())
	}
	if got, err := os.ReadFile(f.marker("enter-retry")); err != nil || strings.TrimSpace(string(got)) != "2 2000000000" {
		t.Fatalf("enter-retry=%q err=%v, want \"2 2000000000\"", got, err)
	}
}
