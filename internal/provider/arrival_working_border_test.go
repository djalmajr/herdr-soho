package provider

import (
	"strings"
	"testing"
)

// piWorkingBorder is the real pi 0.99.2 top border of the input box while pi
// works (evidence-rc10/pi-steer-t1.txt, line 11): two '─', the activity
// indicator, and the rest of the line in '─'.
var piWorkingBorder = "── \u2834 Working " + strings.Repeat("─", 54)

// piSteerT1Screen is the real recent-unwrapped screen from
// evidence-rc10/pi-steer-t1.txt (14 lines): history above, the empty input box
// between the Working border (line 11) and the '─' border (line 12), and the
// 2-line footer below.
var piSteerT1Screen = " Teste do orquestrador, sem relação com a sua fatia: rode no bash\n" +
	" exatamente 'sleep 45' (timeout 60 segundos) e depois responda só\n" +
	" 'fim'.\n" +
	" The orchestrator is requesting a test: run exactly sleep 45 in\n" +
	" bash (with a 60-second timeout), then reply only with \"fim\". This\n" +
	" is a simple task. Let's just execute it as is.\n" +
	" $ sleep 45 (timeout 60s)\n" +
	" Elapsed 7.0s\n" +
	" Steering: Read the file /var/folders/f2/r857c16x45z6p82wsq_0d_...\n" +
	" ↳ Option+Up to edit all queued messages\n" +
	piWorkingBorder + "\n" +
	strings.Repeat("─", 66) + "\n" +
	"~/Developer/djalmajr/herdr-soho/.worktrees/s37 (fix/provider-har...\n" +
	"↑160k ↓70k R8.8M CH99.9% 59.5%/262k (auto)       model-x • high\n"

// workingPiScreen models the real pi shape of a working target: the given box
// line between the Working border and the '─' border, the 2-line footer below.
// history keeps the lines above the Working border (a real queue line like the
// one in pi-steer-t1.txt would itself be arrival proof, so a negative test
// needs neutral history here).
func workingPiScreen(history, box string) string {
	sep := strings.Repeat("─", 66)
	return history + "\n" +
		piWorkingBorder + "\n" +
		box + "\n" +
		sep + "\n" +
		"~/Developer/djalmajr/herdr-soho/.worktrees/s48 (fix/arrival-pi-box-whole-line)\n" +
		"↑160k ↓70k R8.8M CH99.9% 59.5%/262k (auto)       model-x • high\n"
}

func TestPiInputRegionWorkingBorder(t *testing.T) {
	t.Run("the real pi-steer-t1 screen has the region between the two borders", func(t *testing.T) {
		start, end, ok := PiInputRegion(piSteerT1Screen)
		if !ok || start != 11 || end != 11 {
			t.Fatalf("region=[%d,%d) ok=%v, want [11,11) ok=true (empty box between the borders)\n%s", start, end, ok, piSteerT1Screen)
		}
	})
	t.Run("the real screen with a line typed into the box holds that line", func(t *testing.T) {
		lines := strings.Split(piSteerT1Screen, "\n")
		typed := "Read the file /tmp/herdr-soho/ws/reports/build-3-20261001T165751.brief.md in full"
		lines = append(lines[:11], append([]string{typed}, lines[11:]...)...)
		start, end, ok := PiInputRegion(strings.Join(lines, "\n"))
		if !ok || start != 11 || end != 12 {
			t.Fatalf("region=[%d,%d) ok=%v, want [11,12) ok=true", start, end, ok)
		}
	})
	t.Run("a text line with a border rune in the middle is not a border", func(t *testing.T) {
		if _, _, ok := PiInputRegion("a ─ b\nx\na ─ b\n"); ok {
			t.Fatal("two 'a ─ b' lines must not form a region")
		}
		sep := strings.Repeat("─", 66)
		lines, ok := piBoxRegion("history\n" + sep + "\na ─ b\n" + sep + "\nboxed\n" + sep + "\nfooter\n")
		if !ok || len(lines) != 1 || lines[0] != "boxed" {
			t.Fatalf("the 'a ─ b' line must not count as a border: region=%+v ok=%v", lines, ok)
		}
	})
	t.Run("a line not framed by two border runes at each end is not a border", func(t *testing.T) {
		if isPiBorderLine("─ x") || isPiBorderLine("x"+strings.Repeat("─", 66)) || isPiBorderLine(strings.Repeat("─", 64)+"x") {
			t.Fatal("'─ x', a leading-text line, and a trailing-text line are not borders")
		}
	})
	t.Run("a line of only border runes still counts", func(t *testing.T) {
		if !isPiBorderLine(strings.Repeat("─", 66)) || !isPiBorderLine("  "+strings.Repeat("─", 66)+"  ") || !isPiBorderLine("─") {
			t.Fatal("a line of '─' (with or without end spaces) must keep counting as a border")
		}
	})
}

// piBoxRegion is the provider's view of the box lines between the last two
// borders, mirroring the peer adapter.
func piBoxRegion(screen string) ([]string, bool) {
	lines := strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n")
	start, end, ok := PiInputRegion(screen)
	if !ok {
		return nil, false
	}
	return lines[start:end], true
}

func TestPromptEvidenceWorkingPiBox(t *testing.T) {
	composed := "/tmp/herdr-soho/ws/reports/build-3-20261001T165751.brief.md"
	prompt := "Read the file " + composed + " in full and execute it."
	// realScreenWith inserts the given line at box index 11 (between the two
	// borders) of the real pi-steer-t1 screen.
	realScreenWith := func(box string) string {
		lines := strings.Split(piSteerT1Screen, "\n")
		lines = append(lines[:11], append([]string{box}, lines[11:]...)...)
		return strings.Join(lines, "\n")
	}
	t.Run("the verbatim real screen with the path typed in the box still proves, via its real queue line", func(t *testing.T) {
		// Control: the box line must not flip the result either way. The file
		// keeps proving through its own `Steering:` queue line in the history
		// (above the region), so a future region bounds mistake that would
		// swallow the history fails here.
		if !PromptEvidence(realScreenWith(prompt), composed) {
			t.Fatalf("the real screen's queue line must keep proving the prompt:\n%s", realScreenWith(prompt))
		}
	})
	t.Run("the real pi-steer-t1 screen with the path in the history above the Working border is proof", func(t *testing.T) {
		lines := strings.Split(piSteerT1Screen, "\n")
		lines[3] = " The orchestrator is requesting a test: " + prompt
		if !PromptEvidence(strings.Join(lines, "\n"), composed) {
			t.Fatalf("the whole path in the history above the Working border must prove the prompt:\n%s", strings.Join(lines, "\n"))
		}
	})
	// Note: the verbatim pi-steer-t1 screen never becomes a negative fixture:
	// its history carries a real `Steering: Read the file …` queue line, which
	// QueuedPromptEvidence proves unconditionally (chrome rule), so the file
	// keeps proving the arrival of any dispatch. The box exclusion itself is
	// what the neutral-history screens below and the mutation prove: with the
	// border rule off, the typed box line proves the path over the whole
	// screen; with it on, the box line never contributes.
	t.Run("the real working screen with the path typed in the box is not proof", func(t *testing.T) {
		screen := workingPiScreen("history: the previous step finished", prompt)
		if PromptEvidence(screen, composed) {
			t.Fatalf("a typed prompt in the box under the Working border must not prove the prompt:\n%s", screen)
		}
	})
	t.Run("the same path in the history above the Working border is proof", func(t *testing.T) {
		screen := workingPiScreen("history: "+prompt, "continuing the previous step")
		if !PromptEvidence(screen, composed) {
			t.Fatalf("the whole path in the history above the Working border must prove the prompt:\n%s", screen)
		}
	})
	t.Run("the real screen keeps proving from its own queue line", func(t *testing.T) {
		// pi-steer-t1.txt carries a real "Steering: Read the file …" queue line
		// in the history: it is proof on its own, not the box line (the real
		// capture's box was empty, so nothing can come from it).
		if !PromptEvidence(piSteerT1Screen, "/var/folders/f2/r857c16x45z6p82wsq_0d_v00000gp/T/herdr-soho/w14/reports/build-2-20261001T999999-amend-probe-with-a-long-name.brief.md") {
			t.Fatalf("the real screen's queue line must keep proving the prompt:\n%s", piSteerT1Screen)
		}
	})
}
