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

// TestDispatchIdlePiPromptSittingInTheInputBox: the prompt sent to an idle pi
// is left typed and not sent in its input box, with the composed path whole on
// one line between pi's two '─' borders. The dispatch must not report received:
// it follows the existing stuck-in-input path — send Enter, look for arrival
// once more — and ends not-received when the prompt never leaves the box.
func TestDispatchIdlePiPromptSittingInTheInputBox(t *testing.T) {
	base := time.Date(2026, 10, 1, 16, 17, 14, 0, time.Local)
	began := time.Now()
	oldNow := platform.Now
	platform.Now = func() time.Time { return base.Add(time.Since(began)) }
	t.Cleanup(func() { platform.Now = oldNow })
	sep := strings.Repeat("─", 66)
	screen := "history: the previous step finished\n" +
		sep + "\n" +
		"Read the file " + filepath.Join("$ROOT", "state", "ws", "briefs", "worker-20261001T161714.md") + " in full and execute it.\n" +
		sep + "\n"
	f := newDispatchArrivalFixture(t, "idle", 1, 1, "old output\n", "0",
		fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Call: 1, ArgvPrefix: true, Stdout: "boot\n"},
		fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "visible"}, ArgvPrefix: true, Stdout: screen},
		fakecli.Rule{Argv: []string{"agent", "read", "worker", "--source", "recent-unwrapped"}, ArgvPrefix: true, Stdout: screen},
		fakecli.Rule{Argv: []string{"agent", "send-keys", "worker", "enter"}, ArgvPrefix: true},
	)
	// The region rules are kind-aware: the target's row is pi, so the box
	// (and only the box) is the recognized composer.
	arrivalRosterKind(t, f, "pi")
	f.env["HERDR_SOHO_WAIT_POLL_MS"] = "100"
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
}
