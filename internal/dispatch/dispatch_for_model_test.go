package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/kinds"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// F1: --for accepts a model name or id whose family the project already
// recognizes; the model is only tried after the roster agent, family, kind
// and recorded-dispatch resolutions, and a spec nothing recognizes gets the
// old error plus the family suggestion.

func forModelConfig() *core.Config { return &core.Config{Entries: map[string]core.ConfigEntry{}} }

func forModelEnv(t *testing.T) platform.Env {
	t.Helper()
	return platform.Env{"HERDR_WORKSPACE_ID": "ws", "TMPDIR": t.TempDir()}
}

func TestForSpecFamilyModelResolution(t *testing.T) {
	t.Run("a model name or id resolves the family of the existing model-family rule", func(t *testing.T) {
		for spec, want := range map[string]string{
			"qwen":                    "alibaba",
			"qwen3.8-27b":             "alibaba",
			"gpt-6.1-sol":             "openai",
			"grok-4.7":                "xai",
			"gemini-2.5":              "google",
			"claude-opus-4-7":         "anthropic",
			"anthropic/claude-opus-4": "anthropic",
		} {
			got, err := ForSpecFamily(spec, t.TempDir(), forModelEnv(t), t.TempDir(), forModelConfig())
			if err != nil || got != want {
				t.Errorf("spec %q: got %q, %v; want %q", spec, got, err, want)
			}
		}
	})

	t.Run("a roster agent whose name also looks like a model stays the agent", func(t *testing.T) {
		sd := t.TempDir()
		roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\n" +
			"grok-4.7\tp1\tclaude\timplementer\tanthropic\t0\t/work\tnow\tclaude-opus\task\timplementer\n"
		if err := os.WriteFile(filepath.Join(sd, "agents.tsv"), []byte(roster), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := ForSpecFamily("grok-4.7", sd, forModelEnv(t), "/work", forModelConfig())
		// The roster agent (kind claude, recorded family anthropic) wins over
		// the model reading of the same name (grok- -> xai).
		if err != nil || got != "anthropic" {
			t.Fatalf("roster agent 'grok-4.7': got %q, %v; want anthropic", got, err)
		}
	})

	t.Run("a spec nothing recognizes keeps the old error and gains the family suggestion", func(t *testing.T) {
		_, err := ForSpecFamily("zzz-not-a-model", t.TempDir(), forModelEnv(t), t.TempDir(), forModelConfig())
		if err == nil {
			t.Fatal("an unrecognized spec must not resolve a family")
		}
		const wantPrefix = "dispatch: --for 'zzz-not-a-model': not an agent in the roster, a family (anthropic|openai|xai|google|alibaba), a kind with a fixed family, or an agent with an accepted dispatch recorded in this workspace"
		const wantSuffix = "; pass a family (for example --for alibaba for a Qwen model) or the worker's name"
		if !strings.HasPrefix(err.Error(), wantPrefix) || !strings.HasSuffix(err.Error(), wantSuffix) {
			t.Fatalf("error=%q", err)
		}
	})

	// The bare Claude tier name is not recognized by the existing rule
	// (kinds.AgentFamily has no bare opus/sonnet/haiku pattern): --for opus
	// therefore gets the error with the suggestion. This subtest pins that
	// premise; if the rule starts recognizing it, update the F1 tests.
	t.Run("the bare Claude tier name opus is not recognized by the existing rule: error with the suggestion", func(t *testing.T) {
		if fam := kinds.AgentFamily("", "opus"); fam != "unknown" {
			t.Fatalf("premise changed: the existing rule now reads opus as %q; this test must be updated", fam)
		}
		_, err := ForSpecFamily("opus", t.TempDir(), forModelEnv(t), t.TempDir(), forModelConfig())
		if err == nil {
			t.Fatal("opus must not resolve a family while the existing rule does not recognize it")
		}
		t.Logf("--for opus errors: %q", err)
		if !strings.HasSuffix(err.Error(), "; pass a family (for example --for alibaba for a Qwen model) or the worker's name") {
			t.Fatalf("error=%q", err)
		}
	})
}
