package peer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rc.12 real screen, used by the peer_arrival_busy_d15 tests: the codex
// drew the taken user message #29a25864 in the history with the composer's
// "› " prefix (line 29), the end line came out glued to the last quoted line
// (line 45), and the composer is empty (line 49). The false "stalled" the
// send reported is not a screen-functions failure on this screen — the
// marker is provably in the history and out of the composer — it is the
// agent_prompt_stalled path dying 15 without ever checking that the message
// was taken. The receipt the recent gate needs on this screen is the
// unquoted opening header (line 29); the glued quoted end line (line 45) is
// quoted payload and is asserted to be no receipt proof. These checks pin
// the screen functions on the real evidence so the composer region, the
// receipt line, and the queue rule keep reading it the way the one-shot
// proof (stalledTaken) needs.
func rc12CodexScreen(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "evidence-rc12-pinar-codex-after.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestArrivalBusyCodexRC12Screen(t *testing.T) {
	const id = "29a25864"
	endLine := PeerEndLine(id)
	screen := rc12CodexScreen(t)
	t.Run("the composer region is the real composer, not the history '›' line", func(t *testing.T) {
		region, ok := codexComposerRegion(screen)
		if !ok {
			t.Fatal("the empty composer line must form the region")
		}
		if !strings.HasPrefix(region[0], "› Ask Codex to do anything") {
			t.Fatalf("the region must start at the composer line, not at the history '›' line: %q", region[0])
		}
	})
	t.Run("the taken message with the glued end line is not still on screen", func(t *testing.T) {
		if messageStillInScreen("codex", screen, endLine, id) {
			t.Fatal("the message in the history (end line glued to the last quoted line) must not count as still on screen")
		}
	})
	t.Run("the empty composer does not hold the id and the history is not a queue", func(t *testing.T) {
		if idInInputBox("codex", screen, id) {
			t.Fatal("the id in the history must not count as in the input box")
		}
		if codexQueued(screen, id) {
			t.Fatal("the history must not count as a follow-up queue line")
		}
	})
	t.Run("the glued quoted end line is not receipt; the unquoted header is", func(t *testing.T) {
		// Intentional contract adaptation: the rc12 evidence is frozen and
		// holds the legacy closing line glued onto the last quoted line
		// ("quoted payload, never positive receipt"), and it also holds the
		// same message's unquoted opening header with the history's "›"
		// prefix. The receipt the recent gate needs is that unquoted header;
		// the glued quoted line is asserted to be no proof at all.
		glued, header := -1, -1
		lines := strings.Split(screen, "\n")
		for i, line := range lines {
			if strings.Contains(line, "Não precisa responder.[herdr-soho:peer]") {
				glued = i
			}
			if strings.Contains(line, "[herdr-soho:peer] #29a25864 Message from another agent") {
				header = i
			}
		}
		if glued < 0 || header < 0 {
			t.Fatal("the frozen evidence lost the glued quoted line or the unquoted header")
		}
		if peerEndLineIn(lines[glued], id) || receiptLineIn(lines[glued], id) {
			t.Fatalf("the glued quoted line must be no end-line or receipt proof: %q", lines[glued])
		}
		if !receiptLineIn(lines[header], id) {
			t.Fatalf("the unquoted opening header must be the receipt: %q", lines[header])
		}
	})
	t.Run("the marker is in the recent history for the proof window's recent gate", func(t *testing.T) {
		if !receiptLineInText(screen, id) {
			t.Fatal("the screen must hold the receipt marker for the recent gate (a bare #id citation would not do)")
		}
	})
}
