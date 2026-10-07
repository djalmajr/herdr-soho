package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// piBoxWithFooter builds the real pi shape from the evidence screens: the
// prompt typed between the two '─' borders and pi's 2-line footer below the
// box. $ROOT stands for the fixture root (the harness substitutes it).
func piBoxWithFooter(boxLine string) string {
	sep := strings.Repeat("─", 66)
	return sep + "\n" +
		boxLine + "\n" +
		sep + "\n" +
		"~/Developer/djalmajr/herdr-soho/.worktrees/s48 (fix/arrival-pi-box-whole-line)\n" +
		"↑160k ↓70k R8.8M CH99.9% 59.5%/262k (auto)       model-x • high\n"
}

// A path in pi's current input can authorize one bounded Enter. History,
// status footer and unrecognized layout alone never prove submission.
func TestDispatchIdlePiInputRegionWithFooter(t *testing.T) {
	base := time.Date(2026, 10, 1, 16, 35, 25, 0, time.Local)
	began := time.Now()
	oldNow := platform.Now
	platform.Now = func() time.Time { return base.Add(time.Since(began)) }
	t.Cleanup(func() { platform.Now = oldNow })
	// composedName returns the brief name the next dispatch will compose, using
	// the same stamp the dispatch uses (core.NowStamp of platform.Now). It waits
	// until the per-second stamp has a stable 300 ms window so the screen built
	// below matches the dispatch's own stamp.
	composedName := func() string {
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
	newFixture := func(t *testing.T, screen string) *dispatchArrivalFixture {
		t.Helper()
		f := newDispatchArrivalFixture(t, "idle", 1, 1, "old output\n", "0",
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 1, ArgvPrefix: true, Stdout: "boot\n"},
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, ArgvPrefix: true, Stdout: screen},
			fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stdout: screen},
			fakecli.Rule{Argv: []string{"agent", "send-keys", "worker", "enter"}, ArgvPrefix: true},
		)
		// The region rules are kind-aware: the target's row is pi, so the
		// borders (when present) are the recognized composer region.
		arrivalRosterKind(t, f, "pi")
		f.env["HERDR_SOHO_WAIT_POLL_MS"] = "100"
		return f
	}
	t.Run("the prompt in the box with the 2-line footer does not close received and sends Enter", func(t *testing.T) {
		composed := composedName()
		f := newFixture(t, piBoxWithFooter("Read the file "+composed+" in full and execute it."))
		code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if !strings.Contains(errText, "sat in the input box; sent Enter") || !strings.Contains(errText, "after an Enter on the text left in its input box") {
			t.Fatalf("the stuck-in-input path was not followed: stderr=%q", errText)
		}
		if _, err := os.Stat(filepath.Join(f.state, "ws", "wait", "worker.not-received")); err != nil {
			t.Fatalf("not-received marker missing: %v", err)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if got := countDispatchCalls(calls, "send-keys"); got != 1 {
			t.Fatalf("send-keys calls=%d, want exactly one Enter; calls=%#v", got, calls)
		}
		if got := countDispatchCalls(calls, "prompt"); got != 1 {
			t.Fatalf("prompt attempts=%d, want no resend; calls=%#v", got, calls)
		}
	})
	t.Run("the prompt in the history above the box keeps receipt uncertain", func(t *testing.T) {
		composed := composedName()
		screen := "history: Read the file " + composed + " in full and execute it.\n" +
			piBoxWithFooter("continuing the previous step")
		f := newFixture(t, screen)
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" {
			t.Fatalf("code=%d out=%s", code, out)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if got := countDispatchCalls(calls, "send-keys"); got != 0 {
			t.Fatalf("send-keys calls=%d, want none; calls=%#v", got, calls)
		}
		if got := countDispatchCalls(calls, "prompt"); got != 1 {
			t.Fatalf("prompt attempts=%d, want no resend; calls=%#v", got, calls)
		}
	})
	t.Run("without the separators a path above the last three lines is uncertain", func(t *testing.T) {
		composed := composedName()
		screen := "old work output\n" +
			"Read the file " + composed + " in full and execute it.\n" +
			"line a\n" +
			"line b\n" +
			"line c\n" +
			"line d\n"
		f := newFixture(t, screen)
		code, out, _ := f.run(t, "worker", f.brief, "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run("without the separators the path in the last 3 lines keeps the delivery uncertain", func(t *testing.T) {
		// Adapted contract: without pi's borders the screen carries no
		// recognized composer for the pi target, and a path in the last 3
		// lines may be a typed prompt — not positive proof. The dispatch
		// sends no Enter (it would be an unconfirmed input) and no resend
		// (the path is visible on the screen): it ends not-received and
		// leaves the pane to be read.
		composed := composedName()
		screen := "h1\n" +
			"h2\n" +
			"h3\n" +
			"Read the file " + composed + " in full and execute it.\n"
		f := newFixture(t, screen)
		code, out, errText := f.run(t, "worker", f.brief, "--no-wait")
		if code != 15 || dispatchOutputStatus(t, out) != "not-received" {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, errText)
		}
		if !strings.Contains(errText, "after an unconfirmed input region") {
			t.Fatalf("the uncertain cause is missing: stderr=%q", errText)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if got := countDispatchCalls(calls, "send-keys"); got != 0 {
			t.Fatalf("send-keys calls=%d, want none (an unconfirmed input gets no Enter); calls=%#v", got, calls)
		}
		if got := countDispatchCalls(calls, "prompt"); got != 1 {
			t.Fatalf("prompt attempts=%d, want 1 (the visible path keeps the delivery uncertain, no resend); calls=%#v", got, calls)
		}
	})
}
