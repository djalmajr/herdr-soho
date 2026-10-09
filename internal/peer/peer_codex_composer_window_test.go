package peer

import (
	"strings"
	"testing"
)

// TestCodexComposerStillReceivesTheHeader is the regression proof of the
// review finding: on a codex composer screen the header's opening line must
// stay among the last 8 non-empty lines, or codexComposerLine does not
// recognize the composer and idInInputBox loses the typed header, so the
// single recovery Enter is skipped and the send exits. The screen is the
// taken prompt: the opening line under the › glyph, the remaining header
// lines indented, a blank line, the one-line quoted body, the closing line
// and the two d15cStatus status lines.
func TestCodexComposerStillReceivesTheHeader(t *testing.T) {
	id := "01020304"
	for _, tc := range []struct {
		name   string
		header string
	}{
		{"local header with a Sender line", PeerHeader("local/w14:p1", "orchestrator-2", "claude", "-", id)},
		{"the longest header: remote with hostname and all four Sender fields", PeerHeaderRemoteFor(PeerSender{Name: "orchestrator-2", Kind: "claude", Role: "sub-orchestrator", Model: "claude-opus-5"}, "w14:p1", "Run2Biz.local", id)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := strings.Split(tc.header, "\n")
			if got := len(lines); got > 4 {
				t.Fatalf("the header has %d lines; the opening line would leave the codex composer's 8-line window", got)
			}
			screen := "› " + lines[0]
			for _, line := range lines[1:] {
				screen += "\n  " + line
			}
			screen += "\n\n> hello\n" + PeerEndLine(id) + "\n" + d15cStatus
			if !idInInputBox("codex", screen, id) {
				t.Fatalf("idInInputBox lost the header opening line in the codex composer:\n%s", screen)
			}
		})
	}
}
