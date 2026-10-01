package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

const shortTimeoutWarning45 = ": --timeout is in milliseconds; 45 is under a second (for 45 seconds pass 45000)"

func TestTimeoutWarnOncePerCommand(t *testing.T) {
	t.Run("dispatch: one warning, the dispatch proceeds", func(t *testing.T) {
		h := newTM3bHarness(t, tm3bFullBrief())
		oldFriction := frictionLogPath
		frictionLogPath = filepath.Join(h.state, "ws", "friction.log") // cmdDispatch is called directly, not through Run
		t.Cleanup(func() { frictionLogPath = oldFriction })
		core.SetFrictionCommand("dispatch") // Run would set it; cmdDispatch is called directly here
		code, out, stderr := h.run(t, "build", h.brief, "--timeout", "45", "--no-wait")
		if code != 0 {
			t.Fatalf("code=%d out=%s stderr=%s; want the dispatch to proceed", code, out, stderr)
		}
		if n := strings.Count(stderr, "dispatch"+shortTimeoutWarning45); n != 1 {
			t.Fatalf("warning count=%d stderr=%s", n, stderr)
		}
		if strings.Contains(stderr, "spawn"+shortTimeoutWarning45) {
			t.Fatalf("a spawn warning leaked into dispatch: %s", stderr)
		}
		log, err := os.ReadFile(filepath.Join(h.state, "ws", "friction.log"))
		if err != nil || !strings.Contains(string(log), "warning\tdispatch\tdispatch"+shortTimeoutWarning45) {
			t.Fatalf("friction log=%q err=%v", log, err)
		}
	})
	t.Run("run: one warning (the dispatch's), no spawn warning, the run proceeds", func(t *testing.T) {
		h := newTM3bHarness(t, tm3bFullBrief())
		prepareRunHarness(t, &h)
		code, out, stderr := runForTest(t, h, "--no-wait", "--pane", "w0test:p0b", "--timeout", "45")
		if code != 0 {
			t.Fatalf("code=%d out=%s stderr=%s; want the run to proceed", code, out, stderr)
		}
		if n := strings.Count(stderr, "dispatch"+shortTimeoutWarning45); n != 1 {
			t.Fatalf("dispatch warning count=%d stderr=%s", n, stderr)
		}
		if strings.Contains(stderr, "spawn: --timeout is in milliseconds") {
			t.Fatalf("the run warned through spawn too: %s", stderr)
		}
	})
	t.Run("compact: one warning, the compact runs to its 45 ms timeout", func(t *testing.T) {
		f := newCompactFixture(t, "claude", []fakecli.Rule{
			{Argv: []string{"agent", "get", "worker"}, Call: 1, Stdout: compactStateJSON("idle", 1)},
			{Argv: []string{"pane", "send-text", "p1", "/compact"}, Stdout: `{"result":{}}`},
			{Argv: []string{"pane", "send-keys", "p1", "Enter"}, Stdout: `{"result":{}}`},
			{Argv: compactReadArgv("worker"), ArgvPrefix: true, Stdout: compactScreens.noProof},
			// The polls without a proof also check the worker is alive.
			{Argv: []string{"agent", "get", "worker"}, Stdout: compactStateJSON("idle", 2)},
		})
		code, out, errText := f.run(t, "compact", "worker", "--timeout", "45")
		if code != 9 {
			t.Fatalf("code=%d out=%s stderr=%s; want the 45 ms compact timeout", code, out, errText)
		}
		if n := strings.Count(errText, "compact"+shortTimeoutWarning45); n != 1 {
			t.Fatalf("warning count=%d stderr=%s", n, errText)
		}
		log, err := os.ReadFile(filepath.Join(f.root, "state", "ws", "friction.log"))
		if err != nil || !strings.Contains(string(log), "warning\tcompact\tcompact"+shortTimeoutWarning45) {
			t.Fatalf("friction log=%q err=%v", log, err)
		}
	})
}
