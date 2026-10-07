package wait

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// Review probe: a failed Enter must not be claimed, and the existing
// three-attempt bound must still stop the retries.

func TestReviewFailedEnterStopsAfterThreeAttempts(t *testing.T) {
	fixed := time.Unix(2_000_000_000, 0)
	oldNow := platform.Now
	platform.Now = func() time.Time { return fixed }
	t.Cleanup(func() { platform.Now = oldNow })
	prompt := "/tmp/worker-brief.md"
	screen := claudeBoxScreen("Welcome to the worker\n", "❯ Read the file "+prompt+" in full and execute it.", "  [Opus 5.5] 67% [main*]\n")
	fail := fakecli.Rule{Argv: []string{"agent", "send-keys", "worker", "enter"}, Code: 1, Stderr: `{"error":{"code":"send_failed","message":"the fake send-keys failed"}}`}
	f := newQueuedProbeFixture(t, "idle", "5", screen, map[string]string{
		"not-received": "1999999900 5 " + prompt + "\n",
	}, fail)
	f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "15"
	var last string
	for probe := 1; probe <= 4; probe++ {
		if probe > 1 {
			fixed = fixed.Add(15 * time.Second)
		}
		last = f.probe("")
		if probe <= 3 && last != "working" {
			t.Fatalf("probe %d=%q, want working while attempts remain", probe, last)
		}
	}
	if last != "not-received" {
		t.Fatalf("probe 4=%q, want not-received after 3 failed attempts", last)
	}
	if n := countEnterKeys(f); n != 3 {
		t.Fatalf("send-keys=%d, want 3; calls=%#v", n, f.calls())
	}
	raw, err := os.ReadFile(f.marker("enter-retry"))
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(raw)), "3 ") {
		t.Fatalf("enter-retry=%q err=%v, want attempt 3 recorded without claiming success", raw, err)
	}
}

func TestReviewQueuedFailedEnterStopsAfterThreeAttempts(t *testing.T) {
	fixed := time.Unix(2_000_000_000, 0)
	oldNow := platform.Now
	platform.Now = func() time.Time { return fixed }
	t.Cleanup(func() { platform.Now = oldNow })
	prompt := "/tmp/worker-brief.md"
	screen := claudeBoxScreen("Welcome to the worker\n", "❯ Read the file "+prompt+" in full and execute it.", "  [Opus 5.5] 67% [main*]\n")
	fail := fakecli.Rule{Argv: []string{"agent", "send-keys", "worker", "enter"}, Code: 1, Stderr: `{"error":{"code":"send_failed","message":"the fake send-keys failed"}}`}
	f := newQueuedProbeFixture(t, "idle", "5", screen, map[string]string{
		"queued": "1 5 " + prompt + "\n",
	}, fail)
	f.env["HERDR_SOHO_PROMPT_CHECK_SECONDS"] = "15"
	var last string
	for probe := 1; probe <= 4; probe++ {
		if probe > 1 {
			fixed = fixed.Add(15 * time.Second)
		}
		last = f.probe("")
	}
	if last != "not-received" {
		t.Fatalf("probe 4=%q, want not-received after 3 failed queued attempts", last)
	}
	if n := countEnterKeys(f); n != 3 {
		t.Fatalf("send-keys=%d, want 3; calls=%#v", n, f.calls())
	}
}
