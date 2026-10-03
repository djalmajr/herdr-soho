package kinds

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// A fresh (less than 1 h old) copy of the 1 h copy file.
func freshCopy(t *testing.T, tmp, kind, list string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(tmp, "herdr-soho-models-"+kind+".txt"), []byte(list), 0600); err != nil {
		t.Fatal(err)
	}
}

// The 1 h copy in TMPDIR is stale: the source gained a model since the copy
// was written. codex never reads (or writes) the copy — it always resolves
// against the local models_cache.json; CLI kinds resolve against the copy
// but re-read the source once before declaring the spec unmatched.
func TestModelStaleCopy(t *testing.T) {
	t.Run("codex: a fresh copy without the new model does not shadow models_cache.json", func(t *testing.T) {
		home := t.TempDir()
		tmp := t.TempDir()
		codexDir := filepath.Join(home, ".codex")
		if err := os.MkdirAll(codexDir, 0o700); err != nil {
			t.Fatal(err)
		}
		cache := `{"models":[{"slug":"gpt-5-codex"},{"slug":"gpt-5.2-codex"}]}`
		if err := os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0600); err != nil {
			t.Fatal(err)
		}
		freshCopy(t, tmp, "codex", "gpt-5-codex\n") // stale: lacks the new model
		env := platform.Env{"HOME": home, "USERPROFILE": home, "TMPDIR": tmp}
		var warns []string
		got, err := ResolveModel("codex", "gpt-5.2-codex", "", env, func(m string) { warns = append(warns, m) })
		if err != nil || got != "gpt-5.2-codex" {
			t.Fatalf("resolved=%q err=%v warns=%v", got, err, warns)
		}
		if len(warns) != 0 {
			t.Fatalf("stale copy raised a no-match warning: %v", warns)
		}
		if copy, readErr := os.ReadFile(filepath.Join(tmp, "herdr-soho-models-codex.txt")); readErr != nil || string(copy) != "gpt-5-codex\n" {
			t.Fatalf("codex rewrote the 1 h copy it no longer uses: %q err=%v", copy, readErr)
		}
	})
	t.Run("cli kind: a spec missing from a fresh copy resolves after one source re-read", func(t *testing.T) {
		bin := t.TempDir()
		if _, err := fakecli.Install(t, bin, "grok", []fakecli.Rule{{AnyArgs: true, Stdout: "grok-1\ngrok-2\n"}}); err != nil {
			t.Fatal(err)
		}
		tmp := t.TempDir()
		freshCopy(t, tmp, "grok", "grok-1\n") // stale: lacks grok-2
		env := platform.Env{"PATH": bin, "HOME": t.TempDir(), "TMPDIR": tmp, "HERDR_SOHO_FAKECLI_CONFIG": bin}
		var warns []string
		got, err := ResolveModel("grok", "grok-2", "", env, func(m string) { warns = append(warns, m) })
		if err != nil || got != "grok-2" {
			t.Fatalf("resolved=%q err=%v warns=%v", got, err, warns)
		}
		if len(warns) != 0 {
			t.Fatalf("re-readable model raised a no-match warning: %v", warns)
		}
		calls, readErr := fakecli.ReadCallsForConfig(filepath.Join(bin, "grok.json"))
		if readErr != nil || len(calls) != 1 {
			t.Fatalf("source re-read was not exactly one CLI call: calls=%d err=%v", len(calls), readErr)
		}
		rewritten, readErr := os.ReadFile(filepath.Join(tmp, "herdr-soho-models-grok.txt"))
		if readErr != nil || !strings.Contains(string(rewritten), "grok-2") {
			t.Fatalf("the copy was not rewritten with the fresh list: %q err=%v", rewritten, readErr)
		}
	})
	t.Run("cli kind: a spec still missing after the re-read keeps the no-match warning", func(t *testing.T) {
		bin := t.TempDir()
		if _, err := fakecli.Install(t, bin, "grok", []fakecli.Rule{{AnyArgs: true, Stdout: "grok-1\ngrok-2\n"}}); err != nil {
			t.Fatal(err)
		}
		tmp := t.TempDir()
		freshCopy(t, tmp, "grok", "grok-1\n")
		env := platform.Env{"PATH": bin, "HOME": t.TempDir(), "TMPDIR": tmp, "HERDR_SOHO_FAKECLI_CONFIG": bin}
		var warns []string
		got, err := ResolveModel("grok", "grok-9", "", env, func(m string) { warns = append(warns, m) })
		if err != nil || got != "grok-9" {
			t.Fatalf("resolved=%q err=%v", got, err)
		}
		if len(warns) != 1 || warns[0] != "no grok model matches 'grok-9'; passing it through unchanged" {
			t.Fatalf("no-match warning changed: %v", warns)
		}
		calls, readErr := fakecli.ReadCallsForConfig(filepath.Join(bin, "grok.json"))
		if readErr != nil || len(calls) != 1 {
			t.Fatalf("re-read ran more than once: calls=%d err=%v", len(calls), readErr)
		}
	})
}
