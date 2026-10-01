package provider

import (
	"strings"
	"testing"
)

// The real screen captured from a pi that broke while calling the provider
// after a Homebrew update of itself (evidence: pi-module-error-recent.txt):
// the message is wrapped across lines, with a box prefix and spaces in the
// wider terminals.
const harnessModuleScreen = "Error: Cannot find module\n" +
	" '/private/tmp/hs-go/orch/pi-broken/dist/bundle/chunks/openai-completions-JXDDPZ23.js' imported\n" +
	" from /private/tmp/hs-go/orch/pi-broken/dist/bundle/chunks/chunk-3YAHQSW6.js\n" +
	" If this looks like a pi bug, /bug sends a report to the developers.\n" +
	"────────────────────────────────────────────────────────────────────────────────────────────────────────\n" +
	"────────────────────────────────────────────────────────────────────────────────────────────────────────\n" +
	"/private/tmp/hs-go/orch\n" +
	"3.2%/262k (auto)                                                          (prov) model-x • low\n"

func assertHarnessDetection(t *testing.T, got *Detection, label string) {
	t.Helper()
	if got == nil || got.Status != "provider-error" || got.Auth {
		t.Fatalf("%s: ProviderDetect() = %#v, want the non-auth provider-error", label, got)
	}
	if got.Cause != HarnessModuleCause {
		t.Fatalf("%s: cause = %q, want exactly %q", label, got.Cause, HarnessModuleCause)
	}
	if strings.ContainsAny(got.Cause, "/\\'") {
		t.Fatalf("%s: cause carries the file path: %q", label, got.Cause)
	}
}

func TestProviderHarnessModule(t *testing.T) {
	t.Run("providerDetect: the real pi module error screen is the harness stop", func(t *testing.T) {
		assertHarnessDetection(t, ProviderDetect("done", harnessModuleScreen), "done screen")
		assertHarnessDetection(t, ProviderDetect("idle", harnessModuleScreen), "idle screen")
	})
	t.Run("providerDetect: the path and the imported from on one line is the same stop", func(t *testing.T) {
		screen := "Error: Cannot find module '/private/tmp/hs-go/orch/pi-broken/dist/bundle/chunks/openai-completions-JXDDPZ23.js' imported from /private/tmp/hs-go/orch/pi-broken/dist/bundle/chunks/chunk-3YAHQSW6.js\n" +
			" If this looks like a pi bug, /bug sends a report to the developers.\n"
		assertHarnessDetection(t, ProviderDetect("done", screen), "single line")
	})
	t.Run("providerDetect: the box prefix and the spaces are ignored", func(t *testing.T) {
		for _, screen := range []string{
			"  ┃  Error: Cannot find module\n  ┃  '/tmp/pi/dist/chunks/a.js' imported\n  ┃  from /tmp/pi/dist/chunks/b.js\n",
			"\tError: Cannot find module\n '/tmp/pi/dist/chunks/a.js' imported\n from /tmp/pi/dist/chunks/b.js\n",
			" Error: Cannot find module '/tmp/pi/dist/chunks/a.js' imported from /tmp/pi/dist/chunks/b.js\n",
		} {
			assertHarnessDetection(t, ProviderDetect("idle", screen), "box line")
		}
	})
	t.Run("providerDetect: imported and from on the wrapped boundary still collapse together", func(t *testing.T) {
		screen := "Error: Cannot find module\n'/tmp/pi/dist/chunks/a.js' imported\nfrom /tmp/pi/dist/chunks/b.js\n"
		assertHarnessDetection(t, ProviderDetect("idle", screen), "boundary")
	})
	t.Run("providerDetect: the imported from half counts within the two following lines", func(t *testing.T) {
		screen := "Error: Cannot find module\n'/tmp/pi/dist/chunks/a.js'\nimported from /tmp/pi/dist/chunks/b.js\n"
		assertHarnessDetection(t, ProviderDetect("idle", screen), "second following line")
	})
	t.Run("providerDetect: without the imported from half, nothing changes", func(t *testing.T) {
		for _, screen := range []string{
			"Error: Cannot find module 'x'\n",
			"Error: Cannot find module 'x'\n    at Function._resolveFilename (node:internal/modules/cjs/loader:1207:15)\nRequire stack:\n- /tmp/app.js\n",
			"Error: Cannot find module\n'/tmp/pi/dist/chunks/a.js'\nthe module was deleted by the update\n",
		} {
			if got := ProviderDetect("idle", screen); got != nil {
				t.Fatalf("ProviderDetect(%q) = %#v, want nil", screen, got)
			}
		}
	})
	t.Run("providerDetect: the imported from half only counts in the two following lines", func(t *testing.T) {
		screen := "Error: Cannot find module\n'/tmp/pi/dist/chunks/a.js'\nsome other line\nimported from /tmp/pi/dist/chunks/b.js\n"
		if got := ProviderDetect("idle", screen); got != nil {
			t.Fatalf("half on the third following line matched: %#v", got)
		}
	})
	t.Run("providerDetect: the prefix is the exact Error word, as Node prints it", func(t *testing.T) {
		for _, screen := range []string{
			"error: cannot find module '/tmp/a.js' imported from '/tmp/b.js'\n",
			"ERROR: Cannot find module '/tmp/a.js' imported from '/tmp/b.js'\n",
			"Error: Cannot find the module '/tmp/a.js' imported from '/tmp/b.js'\n",
		} {
			if got := ProviderDetect("idle", screen); got != nil {
				t.Fatalf("ProviderDetect(%q) = %#v, want nil", screen, got)
			}
		}
	})
	t.Run("providerDetect: never while working", func(t *testing.T) {
		if got := ProviderDetect("working", harnessModuleScreen); got != nil {
			t.Fatalf("working screen matched: %#v", got)
		}
	})
	t.Run("providerDetect: outside the bottom ten non-empty lines, nothing changes", func(t *testing.T) {
		screen := harnessModuleScreen + "output line a\noutput line b\noutput line c\n"
		if got := ProviderDetect("idle", screen); got != nil {
			t.Fatalf("pushed-up module screen matched: %#v", got)
		}
	})
	t.Run("providerDetect: a code line with the module text is not a stop", func(t *testing.T) {
		if got := ProviderDetect("idle", "// Error: Cannot find module '/tmp/a.js' imported from '/tmp/b.js'\n"); got != nil {
			t.Fatalf("code line matched: %#v", got)
		}
	})
	t.Run("providerDetect: the existing rules come first, bottom-up", func(t *testing.T) {
		// The module line is older than a connection error: the newest stop wins.
		screen := harnessModuleScreen + "Error: Connection error.\n"
		got := ProviderDetect("idle", screen)
		if got == nil || got.Status != "provider-error" || got.Cause != "Error: Connection error." {
			t.Fatalf("newest stop = %#v, want the connection error", got)
		}
		// A capacity line below the module line is the stop.
		screen = harnessModuleScreen + "Error: 529 at capacity\n"
		got = ProviderDetect("idle", screen)
		if got == nil || got.Status != "capacity" {
			t.Fatalf("capacity below the module line = %#v, want capacity", got)
		}
	})
	t.Run("providerDetect: an output bullet below the module line ends the search", func(t *testing.T) {
		screen := harnessModuleScreen + "• Ran the tests again\n"
		if got := ProviderDetect("idle", screen); got != nil {
			t.Fatalf("recovered module line matched: %#v", got)
		}
	})
}
