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
// was taken. These checks pin the screen functions on the real evidence so
// the composer region, the glued end line, and the queue rule keep reading
// it the way the one-shot proof (stalledTaken) needs.
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
	t.Run("the glued end line still holds the full end line for the substring check", func(t *testing.T) {
		found := false
		for _, line := range strings.Split(screen, "\n") {
			if strings.Contains(line, endLine) {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("the glued end line must still hold the full end line (the checks compare substrings)")
		}
	})
	t.Run("the marker is in the recent history for the proof window's recent branch", func(t *testing.T) {
		if !strings.Contains(screen, "#"+id) {
			t.Fatal("the screen must hold the marker in the history for the recent-based proof")
		}
	})
}
