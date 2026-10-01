package wait

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// The real screen of a done pi that cannot load one of its own files (a
// module of its own package was replaced while it ran).
const harnessModuleScreen = "Error: Cannot find module\n" +
	" '/private/tmp/hs-go/orch/pi-broken/dist/bundle/chunks/openai-completions-JXDDPZ23.js' imported\n" +
	" from /private/tmp/hs-go/orch/pi-broken/dist/bundle/chunks/chunk-3YAHQSW6.js\n" +
	" If this looks like a pi bug, /bug sends a report to the developers.\n" +
	"────────────────────────────────────────────────────────────────────────────────────────────────────────\n" +
	"────────────────────────────────────────────────────────────────────────────────────────────────────────\n" +
	"/private/tmp/hs-go/orch\n" +
	"3.2%/262k (auto)                                                          (prov) model-x • low\n"

func assertNoResend(t *testing.T, f *queuedProbeFixture, label string) {
	t.Helper()
	calls := f.calls()
	for _, call := range calls {
		if len(call.Argv) >= 2 && call.Argv[0] == "agent" && (call.Argv[1] == "prompt" || call.Argv[1] == "send-keys") {
			t.Fatalf("%s: the prompt was resent or a key was sent: %#v", label, call)
		}
	}
}

func TestHarnessModuleDoneWithQueuedPrompt(t *testing.T) { // JS: "wait: a done pi with a harness module error ends provider-error, never not-received"
	// The provider detection runs before the queued prompt would be declared
	// not-received: a done pi with the module error screen and no report ends
	// provider-error, with no second prompt and no not-received marker.
	f := newQueuedProbeFixture(t, "done", "5", harnessModuleScreen, map[string]string{
		"queued": "1 5 /tmp/worker-brief.md\n",
	})
	if got := f.probe(""); got != "working" {
		t.Fatalf("first probe = %q, want working (the detection confirms on the next probe)", got)
	}
	if got := f.probe(""); got != "provider-error" {
		t.Fatalf("second probe = %q, want provider-error", got)
	}
	if _, err := os.Stat(f.marker("not-received")); !os.IsNotExist(err) {
		t.Fatalf("not-received marker written: err=%v", err)
	}
	if _, err := os.Stat(f.marker("queued")); !os.IsNotExist(err) {
		t.Fatalf("queued marker remains after the harness stop: err=%v", err)
	}
	cause, err := os.ReadFile(f.marker("provider-cause"))
	if err != nil || string(cause) != "Error: Cannot find module\n" {
		t.Fatalf("provider-cause = %q err=%v, want the fixed cause", cause, err)
	}
	assertNoResend(t, f, "queued harness stop")
}

func TestHarnessModuleDoneFreshWait(t *testing.T) { // JS: "wait: a done pi with a harness module error ends provider-error, never not-received"
	// The same stop without any marker: the detection still beats the
	// settled-no-report fallback of the unchanged screen.
	f := newQueuedProbeFixture(t, "done", "5", harnessModuleScreen, nil)
	if got := f.probe(""); got != "working" {
		t.Fatalf("first probe = %q, want working", got)
	}
	if got := f.probe(""); got != "provider-error" {
		t.Fatalf("second probe = %q, want provider-error", got)
	}
	if _, err := os.Stat(f.marker("not-received")); !os.IsNotExist(err) {
		t.Fatalf("not-received marker written: err=%v", err)
	}
	cause, err := os.ReadFile(f.marker("provider-cause"))
	if err != nil || string(cause) != "Error: Cannot find module\n" {
		t.Fatalf("provider-cause = %q err=%v, want the fixed cause", cause, err)
	}
	assertNoResend(t, f, "fresh harness stop")
}

func TestHarnessModuleWaitForExits14(t *testing.T) { // JS: "wait: a done pi with a harness module error ends provider-error, never not-received"
	f := newQueuedProbeFixture(t, "done", "5", harnessModuleScreen, nil)
	oldOut := platform.Stdout
	var buf bytes.Buffer
	platform.Stdout = &buf
	rc := WaitFor([]string{"worker"}, f.sd, f.ctx, f.env, 3000, false, t.TempDir())
	platform.Stdout = oldOut
	if rc != 14 {
		t.Fatalf("WaitFor rc = %d, want 14; stdout=%q", rc, buf.String())
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("WaitFor printed %d lines, want 1: %q", len(lines), buf.String())
	}
	value, err := jsonjs.Parse([]byte(lines[0]))
	if err != nil {
		t.Fatalf("wait JSON parse: %v: %s", err, lines[0])
	}
	obj, ok := value.(*jsonjs.Object)
	if !ok {
		t.Fatalf("wait JSON has type %T: %s", value, lines[0])
	}
	status, _ := obj.Get("status")
	cause, _ := obj.Get("cause")
	if status != "provider-error" || cause != "Error: Cannot find module" {
		t.Fatalf("wait line = %s, want status provider-error and the fixed cause", lines[0])
	}
}
