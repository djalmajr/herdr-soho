package provider

import (
	"strings"
	"testing"
)

// The real probe path the orchestrator sent to the working pi: the consumed
// screen below carries it wrapped across history lines by pi itself.
const wrappedComposed = "/var/folders/f2/r857c16x45z6p82wsq_0d_v00000gp/T/herdr-soho/w14/reports/build-2-20261001T999999-amend-probe-with-a-long-name.brief.md"

// consumedPiScreen is the real pi 0.99.2 recent-unwrapped read after the agent
// consumes a queued prompt (evidence-rc10/pi-steer-consumed.txt): the prompt
// sits in the chat history, pi wrapped it across lines, the path split
// mid-line, and no box chrome or '─' separator is present.
const consumedPiScreen = `is a simple task. Let's just execute it as is.
 $ sleep 45 (timeout 60s)
 (no output)
 Took 45.0s
 Read the file
 /var/folders/f2/r857c16x45z6p82wsq_0d_v00000gp/T/herdr-soho/w14/r
 eports/build-2-20261001T999999-amend-probe-with-a-long-name.brief
 .md and follow it. Esta é só uma sonda do orquestrador: quando
 ler isto, responda apenas 'sonda recebida' e não abra o arquivo.
 The user is saying: "Please read the file
 .../build-2-20261001T999999-amend-probe-with-a-long-name.brief.md
 and follow its contents. This is just a probe from the
 orchestrator: when you read this, simply reply 'sonda recebida'
 and do not open the file."
`

// piBoxScreen puts a history line above and a footer below the two '─'
// separators so the region between them is pi's input box.
func piBoxScreen(box string) string {
	sep := strings.Repeat("─", 66)
	return "history: the worker finishes its step\n" +
		sep + "\n" +
		box + "\n" +
		sep + "\n" +
		"~/Developer/djalmajr/herdr-soho/.worktrees/s40 (fix/arrival-pi-wrapped)\n" +
		"↑160k ↓70k R8.8M CH99.9% 59.5%/262k (auto)       qwen3.8-27b • high\n"
}

// wrappedBlock splits the composed path across several lines exactly the way
// the consumed screen does, under a "Read the file" line.
func wrappedBlock(path string) string {
	cut := len(path) - 3
	return " Read the file\n" +
		" " + path[:cut] + "\n" +
		" " + path[cut:] + " and follow it\n"
}

func TestPromptEvidenceWrappedPiHistory(t *testing.T) {
	t.Run("the real consumed screen proves the prompt", func(t *testing.T) {
		if !PromptEvidence(consumedPiScreen, wrappedComposed) {
			t.Fatalf("the real consumed screen must prove the prompt:\n%s", consumedPiScreen)
		}
	})
	t.Run("the real Steering queue line still proves the prompt", func(t *testing.T) {
		screen := "is a simple task. Let's just execute it as is.\n" +
			" $ sleep 45 (timeout 60s)\n" +
			" Steering: Read the file /var/folders/f2/r857c16x45z6p82wsq_0d_...\n" +
			" ↳ Option+Up to edit all queued messages\n" +
			strings.Repeat("─", 66) + "\n" +
			"\n" +
			strings.Repeat("─", 66) + "\n" +
			"~/path (branch)\n"
		if !PromptEvidence(screen, wrappedComposed) {
			t.Fatalf("the real Steering queue line must keep proving the prompt:\n%s", screen)
		}
	})
	t.Run("the same wrapped block inside the pi input box is not proof", func(t *testing.T) {
		screen := piBoxScreen(strings.TrimSuffix(wrappedBlock(wrappedComposed), "\n"))
		if PromptEvidence(screen, wrappedComposed) {
			t.Fatalf("a typed-but-not-sent block in the input box must not prove the prompt:\n%s", screen)
		}
	})
	t.Run("control: the same wrapped block in the history above the box is proof", func(t *testing.T) {
		sep := strings.Repeat("─", 66)
		screen := "history\n" +
			" Read the file\n" +
			" " + wrappedComposed[:len(wrappedComposed)-3] + "\n" +
			" " + wrappedComposed[len(wrappedComposed)-3:] + " and follow it\n" +
			sep + "\n\n" +
			sep + "\n" +
			"~/path (branch)\n"
		if !PromptEvidence(screen, wrappedComposed) {
			t.Fatalf("the wrapped block in the history must prove the prompt:\n%s", screen)
		}
	})
	t.Run("a different path wrapped the same way is not proof", func(t *testing.T) {
		other := "/var/folders/f2/r857c16x45z6p82wsq_0d_v00000gp/T/herdr-soho/w14/reports/build-1-20260930T000000-amend-probe-with-a-long-name.brief.md"
		if PromptEvidence(consumedPiScreen, other) {
			t.Fatalf("a different path wrapped the same way must not prove the prompt")
		}
	})
	t.Run("path pieces across lines without the marker on top are not proof", func(t *testing.T) {
		screen := " /var/folders/f2/r857c16x45z6p82wsq_0d_v00000gp/T/herdr-soho/w14/r\n" +
			" eports/build-2-20261001T999999-amend-probe-with-a-long-name.brief\n" +
			" .md and follow it\n"
		if PromptEvidence(screen, wrappedComposed) {
			t.Fatalf("path pieces without the marker on top must not prove the prompt:\n%s", screen)
		}
	})
	t.Run("the joined block must begin with the marker plus the path", func(t *testing.T) {
		composed := "/tmp/herdr-soho/reports/build-2-20261001T999999.brief.md"
		screen := "The user is asking: Read the file\n" +
			" /tmp/herdr\n" +
			" -soho/reports/build-2-20261001T999999.brief.md\n"
		if PromptEvidence(screen, composed) {
			t.Fatalf("a block that does not begin with the marker must not prove the prompt:\n%s", screen)
		}
	})
}

func TestPromptEvidenceWrappedBlockLimits(t *testing.T) {
	composed := "/tmp/herdr-soho/reports/build-2-20261001T999999.brief.md"
	t.Run("the path completed on the 8th following line is proof", func(t *testing.T) {
		screen := "Read the file\n /tmp\n /herdr\n -soho\n /reports\n /build-2-\n 20261001\n T999999\n .brief.md and follow it\n"
		if !PromptEvidence(screen, composed) {
			t.Fatalf("the path completed within 8 following lines must prove the prompt")
		}
	})
	t.Run("the path completed on the 9th following line is not proof", func(t *testing.T) {
		screen := "Read the file\n /tmp\n /herdr\n -soho\n /reports\n /build-2-\n 2026100\n 1T9999\n 99.brief\n .md and follow it\n"
		if PromptEvidence(screen, composed) {
			t.Fatalf("a path wrapped beyond the 8-line block must not prove the prompt")
		}
	})
	t.Run("the block stops at the first empty line", func(t *testing.T) {
		screen := "Read the file\n /tmp/herdr\n -soho/reports\n\n /build-2-20261001T999999.brief.md\n"
		if PromptEvidence(screen, composed) {
			t.Fatalf("an empty line must cut the block before the path completes")
		}
	})
}

func TestPromptEvidenceOpencodeBoxUnchanged(t *testing.T) {
	composed := "/work/state/ws/briefs/worker-20260930T125455.md"
	t.Run("the box-wrapped path still proves via the box join rule", func(t *testing.T) {
		screen := "  ┃  Read the file /work/state/ws/briefs/worker-2026\n" +
			"  ┃  0930T125455.md in full\n"
		if !PromptEvidence(screen, composed) {
			t.Fatalf("the box-wrapped path must keep proving the prompt")
		}
	})
	t.Run("an earlier brief of the same agent in the box is not proof", func(t *testing.T) {
		screen := "  ┃  Read the file /work/state/ws/briefs/worker-1999\n" +
			"  ┃  0101T000000.md in full\n"
		if PromptEvidence(screen, composed) {
			t.Fatalf("another brief of the same agent must not prove the prompt")
		}
	})
}

// Round 2 (R-RC10F, P2): the composed path must end at a word boundary in
// the block's original text — matching skips only the whitespace between the
// pattern's runes, and the rune right after the path's last one must be
// whitespace or the end of the block, so a longer path on screen never proves
// the shorter composed one.
func TestBlockMatchesPathBoundary(t *testing.T) {
	pattern := "Read the file /tmp/herdr-soho/reports/build-2-20261001T999999.brief.md"
	cases := []struct {
		name, block string
		want        bool
	}{
		{"a space after the path is proof", "Read the file /tmp/herdr-soho/reports/build-2-20261001T999999.brief.md and follow it", true},
		{"the path at the end of the block is proof", "Read the file /tmp/herdr-soho/reports/build-2-20261001T999999.brief.md", true},
		{`.md-later is not proof`, "Read the file /tmp/herdr-soho/reports/build-2-20261001T999999.brief.md-later and follow it", false},
		{"a radical still followed by its continuation is not proof", "Read the file /tmp/herdr-soho/reports/build-2-20261001T999999-amend-probe-with-a-long-name.brief.md and follow it", false},
		{"a wrap between the pattern's runes is skipped", "Read the file\n /tmp/herdr-soho/reports/build-2-20261001T999999.brief.md and follow it", true},
		{"text before the marker is not proof", "The user is asking: Read the file /tmp/herdr-soho/reports/build-2-20261001T999999.brief.md", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := blockMatchesPath(tc.block, pattern); got != tc.want {
				t.Fatalf("blockMatchesPath=%v want %v for %q", got, tc.want, tc.block)
			}
		})
	}
}

func TestPromptEvidenceWrappedPathBoundary(t *testing.T) {
	composed := "/tmp/herdr-soho/reports/build-2-20261001T999999.brief.md"
	t.Run("a path continuing past the composed one is not proof", func(t *testing.T) {
		// The path is wrapped, so the whole-screen rule cannot see the composed
		// path; the block rule must reject the -later continuation.
		screen := "Read the file\n /tmp/herdr-soho/reports/build-2-20261001T\n 999999.brief.md-later and follow it\n"
		if PromptEvidence(screen, composed) {
			t.Fatalf("a longer wrapped path must not prove the composed one:\n%s", screen)
		}
	})
	t.Run("the radical of the real consumed path is not proof", func(t *testing.T) {
		radical := "/var/folders/f2/r857c16x45z6p82wsq_0d_v00000gp/T/herdr-soho/w14/reports/build-2-20261001T999999-amend-probe"
		if PromptEvidence(consumedPiScreen, radical) {
			t.Fatalf("the radical still followed by its continuation must not prove the path")
		}
	})
	t.Run("the path ending at the end of a line with the next line and follow it is proof", func(t *testing.T) {
		// Wrapped, so the whole-screen rule cannot see the path: the line break
		// right after the path's last rune is the boundary the block rule accepts.
		screen := "Read the file\n /tmp/herdr-soho/reports/build-2-20261001T\n 999999.brief.md\nand follow it\n"
		if !PromptEvidence(screen, composed) {
			t.Fatalf("the path ending at a line break must prove the prompt:\n%s", screen)
		}
	})
	t.Run("the path ending at the end of the block is proof", func(t *testing.T) {
		// Wrapped and cut off: the block ends right after the path's last rune.
		screen := "Read the file\n /tmp/herdr-soho/reports/build-2-20261001T\n 999999.brief.md\n"
		if !PromptEvidence(screen, composed) {
			t.Fatalf("the path ending the block must prove the prompt:\n%s", screen)
		}
	})
	t.Run("the real consumed screen still proves the prompt", func(t *testing.T) {
		if !PromptEvidence(consumedPiScreen, wrappedComposed) {
			t.Fatalf("the real consumed screen must keep proving the prompt")
		}
	})
}
