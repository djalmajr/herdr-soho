package provider

import (
	"strings"
	"testing"
)

func TestProviderDetectTexts(t *testing.T) {
	const boxLine = "  ┃  nenhum worker de inferência pronto\n"
	const capacityText = "nenhum worker de inferência pronto"
	const errorText = "vLLM backend unavailable"
	t.Run("providerDetectTexts: the real box line with a capacity text is capacity", func(t *testing.T) {
		got := ProviderDetectTexts("idle", boxLine, capacityText, "")
		if got == nil || got.Status != "capacity" || got.Cause != "nenhum worker de inferncia pronto" || got.Auth {
			t.Fatalf("ProviderDetectTexts() = %#v", got)
		}
	})
	t.Run("providerDetectTexts: without the keys the same line is nil", func(t *testing.T) {
		if got := ProviderDetectTexts("idle", boxLine, "", ""); got != nil {
			t.Fatalf("ProviderDetectTexts() = %#v, want nil", got)
		}
		if got := ProviderDetect("idle", boxLine); got != nil {
			t.Fatalf("ProviderDetect() = %#v, want nil", got)
		}
	})
	t.Run("providerDetectTexts: an error text is provider-error even without the Error prefix", func(t *testing.T) {
		got := ProviderDetectTexts("idle", "  ┃  vLLM backend unavailable\n", "", errorText)
		if got == nil || got.Status != "provider-error" || got.Auth {
			t.Fatalf("ProviderDetectTexts() = %#v", got)
		}
	})
	t.Run("providerDetectTexts: the texts match case-insensitively", func(t *testing.T) {
		got := ProviderDetectTexts("idle", "  ┃  VLLM BACKEND UNAVAILABLE\n", "", errorText)
		if got == nil || got.Status != "provider-error" {
			t.Fatalf("case-insensitive match = %#v, want provider-error", got)
		}
		got = ProviderDetectTexts("idle", "Nenhum worker de inferência pronto\n", capacityText, "")
		if got == nil || got.Status != "capacity" {
			t.Fatalf("case-insensitive capacity = %#v, want capacity", got)
		}
	})
	t.Run("providerDetectTexts: never while working", func(t *testing.T) {
		for _, got := range []*Detection{ProviderDetectTexts("working", boxLine, capacityText, ""), ProviderDetectTexts("working", "  ┃  vLLM backend unavailable\n", "", errorText)} {
			if got != nil {
				t.Fatalf("working screen matched: %#v", got)
			}
		}
	})
	t.Run("providerDetectTexts: only the bottom ten lines count", func(t *testing.T) {
		below := func(n int) string {
			lines := make([]string, 0, n)
			for i := 1; i <= n; i++ {
				lines = append(lines, "line "+string(rune('a'+i-1)))
			}
			return "  ┃  vLLM backend unavailable\n" + strings.Join(lines, "\n")
		}
		if got := ProviderDetectTexts("idle", below(10), "", errorText); got != nil {
			t.Fatalf("eleventh line from the bottom matched: %#v", got)
		}
		if got := ProviderDetectTexts("idle", below(9), "", errorText); got == nil || got.Status != "provider-error" {
			t.Fatalf("tenth line from the bottom = %#v, want provider-error", got)
		}
	})
	t.Run("providerDetectTexts: the box prefix and the spaces are ignored", func(t *testing.T) {
		for _, screen := range []string{"vLLM backend unavailable\n", "\tvLLM backend unavailable\n", " ┃vLLM backend unavailable\n", "  ┃  vLLM backend unavailable  \n"} {
			if got := ProviderDetectTexts("idle", screen, "", errorText); got == nil || got.Status != "provider-error" {
				t.Fatalf("ProviderDetectTexts(%q) = %#v, want provider-error", screen, got)
			}
		}
	})
	t.Run("providerDetectTexts: a capacity text and an error text on one line is capacity", func(t *testing.T) {
		got := ProviderDetectTexts("idle", "  ┃  vLLM backend unavailable, nenhum worker de inferência pronto\n", capacityText, errorText)
		if got == nil || got.Status != "capacity" {
			t.Fatalf("ProviderDetectTexts() = %#v, want capacity", got)
		}
	})
	t.Run("providerDetectTexts: today's rules win over the texts", func(t *testing.T) {
		got := ProviderDetectTexts("idle", "Error: 401 Unauthorized\n", "", "401 unauthorized")
		if got == nil || got.Status != "provider-error" || !got.Auth {
			t.Fatalf("auth rule = %#v, want the auth provider-error", got)
		}
		got = ProviderDetectTexts("idle", "■ Error: Request timed out\n", "request timed out", "")
		if got == nil || got.Status != "provider-error" {
			t.Fatalf("today's pattern = %#v, want provider-error", got)
		}
		for _, screen := range []string{"Error: expected a semicolon\n", "■ installing dependencies\n"} {
			if got := ProviderDetectTexts("idle", screen, "gateway overloaded|no worker ready", ""); got != nil {
				t.Fatalf("unrelated screen matched: %#v", got)
			}
		}
	})
	t.Run("providerDetectTexts: the cause is the redacted, sanitized line", func(t *testing.T) {
		got := ProviderDetectTexts("idle", "  ┃  vLLM backend unavailable token=sk_live_abcdefghij\n", "", errorText)
		if got == nil || got.Cause != "vLLM backend unavailable token=[redacted]" {
			t.Fatalf("cause = %#v", got)
		}
	})
}
