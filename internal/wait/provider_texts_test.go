package wait

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

const providerTextsScreen = "  ┃  nenhum worker de inferência pronto\n"

func newProviderTextsFixture(t *testing.T, capacityText string) *queuedProbeFixture {
	t.Helper()
	f := newQueuedProbeFixture(t, "idle", "5", providerTextsScreen, nil)
	bin := filepath.Join(t.TempDir(), "bin")
	if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"agent_status":"idle","state_change_seq":5}}}`},
		{Argv: []string{"agent", "read", "worker", "--source", "visible", "--lines", "20"}, Stdout: providerTextsScreen},
		{Argv: []string{"agent", "read", "worker", "--source", "visible"}, Stdout: providerTextsScreen},
		{Argv: []string{"agent", "read", "worker", "--source", "recent-unwrapped", "--lines", "40"}, Stdout: providerTextsScreen},
		{Argv: []string{"agent", "prompt", "worker"}, ArgvPrefix: true, Stdout: `{"result":{"submitted":true}}`},
	}); err != nil {
		t.Fatal(err)
	}
	f.bin = bin
	f.env["PATH"] = bin
	f.env["HERDR_SOHO_FAKECLI_CONFIG"] = bin
	f.env["HERDR_SOHO_PROVIDER_RETRIES"] = "1"
	f.env["HERDR_SOHO_PROVIDER_RETRY_DELAY"] = "0"
	if capacityText != "" {
		f.ctx = &core.Config{Entries: map[string]core.ConfigEntry{"provider_capacity_texts": {Value: capacityText}}}
	}
	return f
}

func TestProviderTextsWaitCases(t *testing.T) {
	t.Run("wait: a box line with a configured capacity text retries, then reports capacity", func(t *testing.T) {
		// The gateway line has no Error prefix; only the configured text detects it.
		fixed := time.Unix(2_000_000_000, 0)
		oldNow := platform.Now
		platform.Now = func() time.Time { return fixed }
		t.Cleanup(func() { platform.Now = oldNow })
		f := newProviderTextsFixture(t, "nenhum worker de inferência pronto")
		for i := 0; i < 4; i++ {
			if got := f.probe(""); got != "working" {
				t.Fatalf("capacity probe %d = %q, want working", i+1, got)
			}
		}
		if got := f.probe(""); got != "capacity" {
			t.Fatalf("exhausted probe = %q, want capacity (not settled-no-report)", got)
		}
		if got, err := os.ReadFile(f.marker("provider-cause")); err != nil || string(got) != "nenhum worker de inferncia pronto\n" {
			t.Fatalf("provider-cause = %q err=%v", got, err)
		}
		prompts := 0
		for _, call := range f.calls() {
			if len(call.Argv) >= 3 && call.Argv[0] == "agent" && call.Argv[1] == "prompt" && call.Argv[2] == "worker" {
				prompts++
			}
		}
		if prompts != 1 {
			t.Fatalf("continue count = %d, want 1 (provider_retries=1)", prompts)
		}
	})
	t.Run("wait: the same box line without the keys settles without a report", func(t *testing.T) {
		fixed := time.Unix(2_000_000_000, 0)
		oldNow := platform.Now
		platform.Now = func() time.Time { return fixed }
		t.Cleanup(func() { platform.Now = oldNow })
		f := newProviderTextsFixture(t, "")
		if got := f.probe(""); got != "working" {
			t.Fatalf("first probe = %q, want working", got)
		}
		fixed = fixed.Add(90 * time.Second)
		if got := f.probe(""); got != "settled" {
			t.Fatalf("still screen after the grace = %q, want settled (the reported bug)", got)
		}
		if _, err := os.Stat(f.marker("provider")); !os.IsNotExist(err) {
			t.Fatalf("provider marker written without the key: err=%v", err)
		}
	})
}
