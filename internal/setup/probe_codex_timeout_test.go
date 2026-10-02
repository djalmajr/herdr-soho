package setup

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// D8 (infra, Windows): `setup --probe --kind codex` timed out at the 20 s
// default (a false negative; --timeout 60 gave ready). Without --timeout nor
// HERDR_SOHO_PROBE_TIMEOUT the codex default is now 60 s (every other kind
// keeps 20 s), and a timeout cause carries the fixed retry hint.
func TestProbeCodexTimeout(t *testing.T) {
	const wantCause = "timeout after 1s (a timeout does not prove the assistant is unavailable; retry with --timeout 90)"
	t.Run("probe_codex_timeout_defaults_are_60s_codex_20s_rest", func(t *testing.T) {
		// Mutation captured: a single 20 s default (or a codex 20 s) fails
		// here; the per-kind default lives in probeKindDefaultTimeout.
		if n := probeKindDefaultTimeout("codex"); n != 60 {
			t.Fatalf("codex default=%d, want 60", n)
		}
		for _, kind := range []string{"claude", "grok", "agy", "gemini", "cursor", "pi", "opencode"} {
			if n := probeKindDefaultTimeout(kind); n != 20 {
				t.Fatalf("%s default=%d, want 20", kind, n)
			}
		}
	})
	t.Run("probe_codex_timeout_invalid_env_variable_dies_2", func(t *testing.T) {
		env := fakeEnv(t, t.TempDir())
		env["HERDR_SOHO_PROBE_TIMEOUT"] = "bad"
		func() {
			defer func() {
				e, ok := recover().(*platform.ExitError)
				if !ok || e.Code != 2 {
					t.Fatalf("panic=%#v", recover())
				}
			}()
			CmdProbe([]string{"--kind", "codex"}, nil, env, t.TempDir())
		}()
	})
	t.Run("probe_codex_timeout_flag_timeout_wins_end_to_end", func(t *testing.T) {
		// The flag wins end to end: with the 60 s default the probe would
		// outlive this bound, so a timeout within it proves the flag applied.
		bin := t.TempDir()
		if _, err := fakecli.Install(t, bin, "codex", []fakecli.Rule{{Argv: []string{"exec", ProbePrompt}, Delay: 1800}}); err != nil {
			t.Fatal(err)
		}
		env := fakeEnv(t, bin)
		env["HERDR_SOHO_SKILL_DIR"] = filepath.Join("..", "..", "skills", "herdr-soho")
		ctx := &core.Config{Entries: map[string]core.ConfigEntry{}, Order: []string{}}
		old := platform.Stdout
		var out bytes.Buffer
		platform.Stdout = &out
		t.Cleanup(func() { platform.Stdout = old })
		start := time.Now()
		CmdProbe([]string{"--kind", "codex", "--timeout", "1"}, ctx, env, t.TempDir())
		elapsed := time.Since(start)
		var got struct {
			Probes []Probe `json:"probes"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Probes) != 1 || got.Probes[0].Kind != "codex" || got.Probes[0].Status != "error" || got.Probes[0].Cause != wantCause || elapsed < 900*time.Millisecond || elapsed > 5*time.Second {
			t.Fatalf("probe=%+v elapsed=%v output=%s", got.Probes, elapsed, out.String())
		}
	})
	t.Run("probe_codex_timeout_env_variable_wins_end_to_end", func(t *testing.T) {
		bin := t.TempDir()
		if _, err := fakecli.Install(t, bin, "codex", []fakecli.Rule{{Argv: []string{"exec", ProbePrompt}, Delay: 1800}}); err != nil {
			t.Fatal(err)
		}
		env := fakeEnv(t, bin)
		env["HERDR_SOHO_SKILL_DIR"] = filepath.Join("..", "..", "skills", "herdr-soho")
		env["HERDR_SOHO_PROBE_TIMEOUT"] = "1"
		ctx := &core.Config{Entries: map[string]core.ConfigEntry{}, Order: []string{}}
		old := platform.Stdout
		var out bytes.Buffer
		platform.Stdout = &out
		t.Cleanup(func() { platform.Stdout = old })
		start := time.Now()
		CmdProbe([]string{"--kind", "codex"}, ctx, env, t.TempDir())
		elapsed := time.Since(start)
		var got struct {
			Probes []Probe `json:"probes"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Probes) != 1 || got.Probes[0].Cause != wantCause || elapsed < 900*time.Millisecond || elapsed > 5*time.Second {
			t.Fatalf("probe=%+v elapsed=%v output=%s", got.Probes, elapsed, out.String())
		}
	})
}
