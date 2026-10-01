package peer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codexEvidenceScreen loads the real codex visible screen captured a few
// seconds after a send to a working codex (copied into the test package):
// the taken message sits in the history, above the composer.
func codexEvidenceScreen(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "evidence-rc11-codex-history.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// withCodexComposerLine returns the screen with its composer line (the last
// line whose left-trimmed text starts with '›') replaced.
func withCodexComposerLine(screen, line string) string {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if head := strings.TrimLeft(lines[i], " \t"); strings.HasPrefix(head, "›") {
			lines[i] = line
			break
		}
	}
	return strings.Join(lines, "\n")
}

func TestCodexComposerRegion(t *testing.T) {
	t.Run("the real screen: the region runs from the composer line to the end", func(t *testing.T) {
		lines, ok := codexComposerRegion(codexEvidenceScreen(t))
		if !ok {
			t.Fatal("the real screen's '› Ask Codex to do anything' line must form the region")
		}
		if lines[0] != "› Ask Codex to do anything" {
			t.Fatalf("region starts at %q", lines[0])
		}
		if len(lines) != 5 || lines[3] != "  ← for agents · ? for shortcuts          ⚠ 3 warnings · f2 to view" || lines[4] != "" {
			t.Fatalf("region must run to the end of the screen: %+v", lines)
		}
	})
	t.Run("an indented composer line counts", func(t *testing.T) {
		lines, ok := codexComposerRegion("history\n  › Ask Codex to do anything\nfooter\n")
		if !ok || len(lines) != 3 || lines[0] != "  › Ask Codex to do anything" {
			t.Fatalf("region=%+v ok=%v", lines, ok)
		}
	})
	t.Run("a bare '›' line counts", func(t *testing.T) {
		if _, ok := codexComposerRegion("history\n›\nfooter\n"); !ok {
			t.Fatal("a bare '›' line must form the region")
		}
	})
	t.Run("the last composer line inside the window wins", func(t *testing.T) {
		lines, ok := codexComposerRegion("history\n› older prompt\n› newer prompt\nfooter\n")
		if !ok || lines[0] != "› newer prompt" {
			t.Fatalf("region=%+v ok=%v", lines, ok)
		}
	})
	t.Run("the eighth non-empty line from the end still counts", func(t *testing.T) {
		if _, ok := codexComposerRegion("› Ask Codex to do anything\n" + strings.Repeat("filler\n", 7)); !ok {
			t.Fatal("a '›' line that is the 8th non-empty line from the end must form the region")
		}
	})
	t.Run("a composer line beyond the last 8 non-empty lines does not count", func(t *testing.T) {
		if _, ok := codexComposerRegion("› Ask Codex to do anything\n" + strings.Repeat("filler\n", 8)); ok {
			t.Fatal("a '›' line below the last 8 non-empty lines must not form the region")
		}
	})
	t.Run("no composer line: no region", func(t *testing.T) {
		if _, ok := codexComposerRegion("history\nfooter\n"); ok {
			t.Fatal("a screen without a '›' line must not form the region")
		}
	})
	t.Run("the real screen with the composer line gone has no region", func(t *testing.T) {
		if _, ok := codexComposerRegion(withCodexComposerLine(codexEvidenceScreen(t), "empty")); ok {
			t.Fatal("without a '›' line the region must not form")
		}
	})
	t.Run("'›' without the space is not a composer line", func(t *testing.T) {
		if _, ok := codexComposerRegion("history\n›x\nfooter\n"); ok {
			t.Fatal("'›x' must not form the region")
		}
	})
	t.Run("CRLF line endings", func(t *testing.T) {
		lines, ok := codexComposerRegion("history\r\n› Ask Codex to do anything\r\nfooter\r\n")
		if !ok || len(lines) != 3 || lines[0] != "› Ask Codex to do anything" {
			t.Fatalf("region=%+v ok=%v", lines, ok)
		}
	})
}

func TestCodexSendScreenMarkers(t *testing.T) {
	id := "b8ba14bf"
	endLine := PeerEndLine(id)
	screen := codexEvidenceScreen(t)
	t.Run("the real screen: a taken message in the history is not still on screen, queued, or in the composer", func(t *testing.T) {
		if messageStillInScreen("codex", screen, endLine, id) {
			t.Fatal("the delivered message in the history must not count as still on screen")
		}
		if codexQueued(screen, id) {
			t.Fatal("the history must not count as the follow-up queue")
		}
		if idInInputBox("codex", screen, id) {
			t.Fatal("the empty composer must not hold the id")
		}
	})
	t.Run("the same screen with the marker typed in the composer is still on screen and in the composer", func(t *testing.T) {
		typed := withCodexComposerLine(screen, "› [herdr-soho:peer] #"+id+" …")
		if !messageStillInScreen("codex", typed, endLine, id) {
			t.Fatal("the id in the composer must block the screen proof")
		}
		if !idInInputBox("codex", typed, id) {
			t.Fatal("the id in the composer must license the Enter")
		}
		if codexQueued(typed, id) {
			t.Fatal("the composer line is not a queue line")
		}
	})
	t.Run("a follow-up queue line above the composer is queued, not on screen", func(t *testing.T) {
		queued := withCodexComposerLine(screen, "↳ [herdr-soho:peer] #"+id+" Message from another agent\n› Ask Codex to do anything")
		if !codexQueued(queued, id) {
			t.Fatal("the '↳ #id' line above the composer must be the follow-up queue")
		}
		if messageStillInScreen("codex", queued, endLine, id) {
			t.Fatal("a queued message sits above the composer, not inside it")
		}
	})
	t.Run("a queue line at or below the composer is not queued", func(t *testing.T) {
		below := withCodexComposerLine(screen, "› x") + "\n↳ [herdr-soho:peer] #" + id + " Message from another agent"
		if codexQueued(below, id) {
			t.Fatal("only lines above the composer count as the queue")
		}
	})
	t.Run("a queue line with another id is not queued for this id", func(t *testing.T) {
		other := withCodexComposerLine(screen, "↳ [herdr-soho:peer] #01020304 Message from another agent\n› Ask Codex to do anything")
		if codexQueued(other, id) {
			t.Fatal("a queue line of another message must not match")
		}
	})
	t.Run("without a composer line the whole-screen rule stays", func(t *testing.T) {
		noComposer := "history " + endLine + " and #" + id + "\n"
		if !messageStillInScreen("codex", noComposer, endLine, id) {
			t.Fatal("without the composer line the whole visible screen still counts")
		}
		if !idInInputBox("codex", noComposer, id) {
			t.Fatal("without the composer line the last 15 lines still count")
		}
		pushedDown := noComposer + strings.Repeat("chrome\n", 15)
		if idInInputBox("codex", pushedDown, id) {
			t.Fatal("the last 15 lines without the composer do not hold the id")
		}
		if !messageStillInScreen("codex", pushedDown, endLine, id) {
			t.Fatal("the whole visible screen still counts without the composer")
		}
	})
	t.Run("a composer line beyond the last 8 non-empty lines keeps the whole-screen rule", func(t *testing.T) {
		stale := "history " + endLine + " and #" + id + "\n› Ask Codex to do anything\n" + strings.Repeat("filler\n", 8)
		if !messageStillInScreen("codex", stale, endLine, id) {
			t.Fatal("a stale composer line must not shrink the screen proof")
		}
		if !idInInputBox("codex", stale, id) {
			t.Fatal("a stale composer line must not shrink the box check")
		}
	})
	t.Run("every other kind keeps the whole-screen behavior", func(t *testing.T) {
		if !messageStillInScreen("claude", screen, endLine, id) {
			t.Fatal("a non-codex marker on screen still blocks the proof")
		}
		if idInInputBox("claude", screen, id) {
			t.Fatal("the real screen's last 15 lines do not hold the id")
		}
	})
}
