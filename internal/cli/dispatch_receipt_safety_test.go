package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// Review probes for the required receipt contract: a path that is not a
// current submission (unrecognized multi-line draft, footer, older history)
// must not close the dispatch as submitted.

func reviewComposedPath() string {
	stamp := ""
	for {
		cur := core.NowStamp(platform.Now())
		if cur != stamp {
			stamp = cur
			continue
		}
		if core.NowStamp(platform.Now().Add(300*time.Millisecond)) == stamp {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return filepath.Join("$ROOT", "state", "ws", "briefs", "worker-"+stamp+".md")
}

func reviewReceiptFixture(t *testing.T, kind, screen string) *dispatchArrivalFixture {
	t.Helper()
	f := newDispatchArrivalFixture(t, "idle", 1, 1, "old output\n", "0",
		fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 1, ArgvPrefix: true, Stdout: "boot\n"},
		fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, ArgvPrefix: true, Stdout: screen},
		fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stdout: screen},
		fakecli.Rule{Argv: []string{"agent", "send-keys", "worker", "enter"}, ArgvPrefix: true},
	)
	arrivalRosterKind(t, f, kind)
	f.env["HERDR_SOHO_WAIT_POLL_MS"] = "50"
	return f
}

func TestReviewUnrecognizedMultilineDraftIsNotReceipt(t *testing.T) {
	base := time.Date(2026, 10, 6, 21, 10, 0, 0, time.UTC)
	began := time.Now()
	oldNow := platform.Now
	platform.Now = func() time.Time { return base.Add(time.Since(began)) }
	t.Cleanup(func() { platform.Now = oldNow })
	composed := reviewComposedPath()
	screen := "Read the file " + composed + " in full and execute it.\n" +
		"draft line two still typed\n" +
		"draft line three still typed\n" +
		"draft line four still typed\n"
	f := reviewReceiptFixture(t, "codex", screen)
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
	if code != 15 || dispatchOutputStatus(t, out) != "not-received" {
		t.Fatalf("unrecognized multi-line draft closed as receipt: code=%d status=%s out=%s stderr=%s", code, dispatchOutputStatus(t, out), out, errText)
	}
	calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := countDispatchCalls(calls, "send-keys"); got != 0 {
		t.Fatalf("send-keys=%d, want 0; calls=%#v", got, calls)
	}
}

func TestReviewClaudeFooterPathIsNotReceipt(t *testing.T) {
	base := time.Date(2026, 10, 6, 21, 10, 0, 0, time.UTC)
	began := time.Now()
	oldNow := platform.Now
	platform.Now = func() time.Time { return base.Add(time.Since(began)) }
	t.Cleanup(func() { platform.Now = oldNow })
	composed := reviewComposedPath()
	sep := strings.Repeat("─", 66)
	screen := "older history without the path\n" + sep + "\n❯ unrelated draft\n" + sep + "\n" +
		"Read the file " + composed + " in full and execute it.\n"
	f := reviewReceiptFixture(t, "claude", screen)
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
	if code != 15 || dispatchOutputStatus(t, out) != "not-received" {
		t.Fatalf("claude footer path closed as receipt: code=%d status=%s out=%s stderr=%s", code, dispatchOutputStatus(t, out), out, errText)
	}
	calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := countDispatchCalls(calls, "send-keys"); got != 0 {
		t.Fatalf("send-keys=%d, want 0; calls=%#v", got, calls)
	}
}

func TestReviewCodexStatusLinePathDoesNotSendEnter(t *testing.T) {
	base := time.Date(2026, 10, 6, 21, 10, 0, 0, time.UTC)
	began := time.Now()
	oldNow := platform.Now
	platform.Now = func() time.Time { return base.Add(time.Since(began)) }
	t.Cleanup(func() { platform.Now = oldNow })
	composed := reviewComposedPath()
	screen := "old output\n› unrelated draft the user is typing\n\n  GPT-6.1-Sol medium · Context 20% " + composed + "\n"
	f := reviewReceiptFixture(t, "codex", screen)
	code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
	calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := countDispatchCalls(calls, "send-keys"); got != 0 {
		t.Fatalf("status-line path sent Enter: keys=%d code=%d out=%s stderr=%s calls=%#v", got, code, out, errText, calls)
	}
	if code == 0 && dispatchOutputStatus(t, out) == "submitted" && !strings.Contains(out, `"enter_sent":true`) {
		t.Fatalf("status-line path closed as receipt without a submission: out=%s stderr=%s", out, errText)
	}
}

func TestDispatchRetryTextRequiresReadableEmptyComposer(t *testing.T) {
	for _, tc := range []struct {
		name, visible, recent   string
		visibleCode, recentCode int
	}{
		{name: "unknown layout", visible: "renderer unavailable\n"},
		{name: "unrelated draft", visible: "› keep this unsent user draft\n\nstatus\n"},
		{name: "failed visible read", visibleCode: 1},
		{name: "failed recent read", visible: "›\n", recentCode: 1},
		{name: "clipped path in recent history", visible: "›\n", recent: "$CURRENT_PATHS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldNow := platform.Now
			clock := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
			platform.Now = func() time.Time {
				value := clock
				clock = clock.Add(2 * time.Second)
				return value
			}
			t.Cleanup(func() { platform.Now = oldNow })
			f := newDispatchArrivalFixture(t, "idle", 1, 1, "", "0",
				fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 1, ArgvPrefix: true, Stdout: "boot\n"},
				fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, ArgvPrefix: true, Stdout: tc.visible, Code: tc.visibleCode},
				fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stdout: tc.recent, Code: tc.recentCode},
			)
			code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
			calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if code != 15 || dispatchOutputStatus(t, out) != "not-received" || countDispatchCalls(calls, "prompt") != 1 || countDispatchCalls(calls, "send-keys") != 0 {
				t.Fatalf("unsafe retry on %s: code=%d out=%s stderr=%s calls=%#v", tc.name, code, out, errText, calls)
			}
		})
	}
}
