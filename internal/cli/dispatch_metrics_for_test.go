package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/dispatch"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
)

// newDispatchMetricsForFixture is the tm3b harness with a claude/anthropic
// reviewer and the roster authors the --for cases need (issue #39, part 2):
// author (codex/openai, full columns) and ghost (pi, empty family and
// effort columns).
func newDispatchMetricsForFixture(t *testing.T) tm3bHarness {
	t.Helper()
	h := newTM3bHarness(t, tm3bFullBrief())
	rolesDir := filepath.Dir(h.role)
	if err := os.WriteFile(filepath.Join(rolesDir, "reviewer.md"), []byte("---\nname: Reviewer\nmode: review\n---\nRole body.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\tmode\targs\teffort\n" +
		"rev\tw0test:p0r\tclaude\treviewer\tanthropic\t0\t" + h.root + "\tnow\tclaude-sonnet-4\ttask\treviewer\t\t\t\thigh\n" +
		"author\tw0test:p0o\tcodex\timplementer\topenai\t0\t" + h.root + "\tnow\tgpt-5\ttask\timplementer\t\t\t\thigh\n" +
		"ghost\tw0test:p0g\tpi\timplementer\t\t0\t\t\tm/1\ttask\timplementer\t\t\t\t\n"
	if err := os.WriteFile(filepath.Join(h.ws, "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	return h
}

// dispatchForSidecar runs the reviewer dispatch and returns the final sidecar
// next to the composed prompt.
func dispatchForSidecar(t *testing.T, h tm3bHarness, args ...string) (string, *jsonjs.Object) {
	t.Helper()
	dispatchArgs := append([]string{"rev", h.brief}, args...)
	code, out, stderr := h.run(t, append(dispatchArgs, "--no-wait")...)
	if code != 0 {
		t.Fatalf("dispatch code=%d stderr=%s", code, stderr)
	}
	composed, ok := dispatchOutputField(t, out, "composed_prompt")
	if !ok {
		t.Fatalf("success JSON omitted the composed prompt: %s", out)
	}
	sidecar := dispatch.DispatchSidecar(composed)
	raw, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("sidecar %s: %v", sidecar, err)
	}
	value, err := jsonjs.Parse(raw)
	if err != nil {
		t.Fatalf("sidecar is not JSON: %v: %q", err, raw)
	}
	obj, ok := value.(*jsonjs.Object)
	if !ok {
		t.Fatalf("sidecar is not an object: %q", raw)
	}
	return string(raw), obj
}

func TestDispatchMetricsForSidecar(t *testing.T) { // mutation: the sidecar drops the "for" array on a rewrite and the review line loses the authors
	t.Run("dispatch_metrics_for_: a roster author carries kind, model, effort and family, never the name", func(t *testing.T) {
		h := newDispatchMetricsForFixture(t)
		raw, obj := dispatchForSidecar(t, h, "--for", "author")
		forValue, ok := obj.Get("for")
		if !ok {
			t.Fatalf("sidecar has no for field: %s", raw)
		}
		want := `[{"kind":"codex","model":"gpt-5","effort":"high","family":"openai"}]`
		if got := jsonjs.Stringify(forValue); got != want {
			t.Fatalf("for=%s, want %s", got, want)
		}
		if strings.Contains(raw, "author") {
			t.Fatalf("the author name reached the sidecar: %s", raw)
		}
	})
	t.Run("dispatch_metrics_for_: a roster author with empty columns carries only the non-empty ones", func(t *testing.T) {
		h := newDispatchMetricsForFixture(t)
		raw, obj := dispatchForSidecar(t, h, "--for", "ghost")
		forValue, ok := obj.Get("for")
		if !ok {
			t.Fatalf("sidecar has no for field: %s", raw)
		}
		want := `[{"kind":"pi","model":"m/1"}]`
		if got := jsonjs.Stringify(forValue); got != want {
			t.Fatalf("for=%s, want %s", got, want)
		}
		if strings.Contains(raw, "ghost") {
			t.Fatalf("the author name reached the sidecar: %s", raw)
		}
	})
	t.Run("dispatch_metrics_for_: a family author carries only the family", func(t *testing.T) {
		h := newDispatchMetricsForFixture(t)
		// The reviewer is anthropic and the author family is anthropic too:
		// the family check is off because the case is the sidecar field, not
		// the check.
		h.env["HERDR_SOHO_FAMILY_CHECK"] = "off"
		raw, obj := dispatchForSidecar(t, h, "--for", "anthropic")
		forValue, ok := obj.Get("for")
		if !ok {
			t.Fatalf("sidecar has no for field: %s", raw)
		}
		if got := jsonjs.Stringify(forValue); got != `[{"family":"anthropic"}]` {
			t.Fatalf("for=%s, want [{\"family\":\"anthropic\"}]", got)
		}
	})
	t.Run("dispatch_metrics_for_: a fixed-family kind author carries kind and family", func(t *testing.T) {
		h := newDispatchMetricsForFixture(t)
		raw, obj := dispatchForSidecar(t, h, "--for", "codex")
		forValue, ok := obj.Get("for")
		if !ok {
			t.Fatalf("sidecar has no for field: %s", raw)
		}
		if got := jsonjs.Stringify(forValue); got != `[{"kind":"codex","family":"openai"}]` {
			t.Fatalf("for=%s, want [{\"kind\":\"codex\",\"family\":\"openai\"}]", got)
		}
	})
	t.Run("dispatch_metrics_for_: several authors keep one object per author in command order", func(t *testing.T) {
		h := newDispatchMetricsForFixture(t)
		raw, obj := dispatchForSidecar(t, h, "--for", "author,codex")
		forValue, ok := obj.Get("for")
		if !ok {
			t.Fatalf("sidecar has no for field: %s", raw)
		}
		want := `[{"kind":"codex","model":"gpt-5","effort":"high","family":"openai"},{"kind":"codex","family":"openai"}]`
		if got := jsonjs.Stringify(forValue); got != want {
			t.Fatalf("for=%s, want %s", got, want)
		}
		if strings.Contains(raw, "author") {
			t.Fatalf("the author name reached the sidecar: %s", raw)
		}
	})
	t.Run("dispatch_metrics_for_: a released author carries the family the dispatch resolves", func(t *testing.T) {
		h := newDispatchMetricsForFixture(t)
		released := filepath.Join(h.ws, "briefs", "gone-20260930T210000.dispatch.json")
		if err := os.MkdirAll(filepath.Dir(released), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(released, []byte(`{"version":1,"kind":"codex","model":"gpt-5","effort":"max","submission":"accepted"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		raw, obj := dispatchForSidecar(t, h, "--for", "gone")
		forValue, ok := obj.Get("for")
		if !ok {
			t.Fatalf("sidecar has no for field: %s", raw)
		}
		if got := jsonjs.Stringify(forValue); got != `[{"family":"openai"}]` {
			t.Fatalf("for=%s, want [{\"family\":\"openai\"}]", got)
		}
		if strings.Contains(raw, "gone") {
			t.Fatalf("the author name reached the sidecar: %s", raw)
		}
	})
	t.Run("dispatch_metrics_for_: without --for the sidecar has no for field", func(t *testing.T) {
		h := newDispatchMetricsForFixture(t)
		raw, obj := dispatchForSidecar(t, h)
		if _, ok := obj.Get("for"); ok {
			t.Fatalf("sidecar recorded a for field without --for: %s", raw)
		}
	})
}
