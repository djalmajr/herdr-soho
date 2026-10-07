package provider

import (
	"strings"
	"testing"
)

// R-RC10F: the composed path whole on one line inside pi's input box is a
// prompt typed and not sent yet. The whole-line check, the `┃` box join, and
// the queue rules must all skip the region between pi's two '─' borders, and
// nothing changes without the two borders.
func TestPromptEvidencePiInputBoxWholeLine(t *testing.T) {
	composed := "/tmp/herdr-soho/ws/reports/build-20261001T161714.brief.md"
	prompt := "Read the file " + composed + " in full and execute it."
	t.Run("the path whole on a line inside the pi box is not proof", func(t *testing.T) {
		screen := piBoxScreen(prompt)
		if PromptEvidence(screen, composed) {
			t.Fatalf("a typed prompt with the whole path on one box line must not prove the prompt:\n%s", screen)
		}
	})
	t.Run("the same path in the history above the box is proof", func(t *testing.T) {
		sep := strings.Repeat("─", 66)
		screen := "history: the worker finishes its step\n" +
			prompt + "\n" +
			sep + "\n" +
			"\n" +
			sep + "\n" +
			"~/Developer/djalmajr/herdr-soho/.worktrees/s48 (fix/arrival-pi-box-whole-line)\n" +
			"↑160k ↓70k R8.8M CH99.9% 59.5%/262k (auto)       model-x • high\n"
		if !PromptEvidence(screen, composed) {
			t.Fatalf("the whole path in the history above the box must prove the prompt:\n%s", screen)
		}
	})
	t.Run("without the separators the path on a line is proof as today", func(t *testing.T) {
		screen := "history: the worker finishes its step\n" + prompt + "\n"
		if !PromptEvidence(screen, composed) {
			t.Fatalf("without the two borders the whole path on a line must keep proving the prompt:\n%s", screen)
		}
	})
	t.Run("the opencode box wrap is unchanged without the pi separators", func(t *testing.T) {
		screen := "  ┃  Read the file /tmp/herdr-soho/ws/reports/build-2026\n" +
			"  ┃  1001T161714.brief.md in full and execute it.\n"
		if !PromptEvidence(screen, composed) {
			t.Fatalf("the box-joined path must keep proving the prompt:\n%s", screen)
		}
	})
	t.Run("the box join keeps the path outside the pi region", func(t *testing.T) {
		sep := strings.Repeat("─", 66)
		screen := "  ┃  Read the file /tmp/herdr-soho/ws/reports/build-2026\n" +
			"  ┃  1001T161714.brief.md in full and execute it.\n" +
			sep + "\n" +
			"typed prompt\n" +
			sep + "\n" +
			"~/path (branch)\n"
		if !PromptEvidence(screen, composed) {
			t.Fatalf("the box-joined path outside the pi region must keep proving the prompt:\n%s", screen)
		}
	})
	t.Run("box lines inside the pi region are excluded from the join", func(t *testing.T) {
		sep := strings.Repeat("─", 66)
		screen := sep + "\n" +
			"  ┃  Read the file /tmp/herdr-soho/ws/reports/build-2026\n" +
			"  ┃  1001T161714.brief.md in full and execute it.\n" +
			sep + "\n" +
			"~/path (branch)\n"
		if PromptEvidence(screen, composed) {
			t.Fatalf("a path joined from box lines inside the pi region must not prove the prompt:\n%s", screen)
		}
	})
	t.Run("a truncated queue line with the identity inside the box is not proof via the queue rules", func(t *testing.T) {
		// The whole-line rule cannot match a truncated path, so only the
		// queue identity rule could count this line; the region exclusion
		// is what keeps it out.
		screen := piBoxScreen("Read the file …/reports/build-20261001T161714.brief.md in full")
		if PromptEvidence(screen, composed) {
			t.Fatalf("a truncated typed line in the box must not prove the prompt:\n%s", screen)
		}
	})
	t.Run("control: the same truncated queue line in the history above the box is proof", func(t *testing.T) {
		sep := strings.Repeat("─", 66)
		screen := "Read the file …/reports/build-20261001T161714.brief.md in full\n" +
			sep + "\n" +
			"\n" +
			sep + "\n" +
			"~/path (branch)\n"
		if !PromptEvidence(screen, composed) {
			t.Fatalf("the truncated queue line in the history must keep proving the prompt:\n%s", screen)
		}
	})
}
