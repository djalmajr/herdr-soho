package peer

import (
	"strings"
	"testing"
)

func TestSohoPeerMarkers(t *testing.T) {
	if PeerPrefix != "[soho:peer]" {
		t.Fatalf("PeerPrefix=%q, want the soho opening marker", PeerPrefix)
	}
	if PeerEndPrefix != "[/soho:peer]" {
		t.Fatalf("PeerEndPrefix=%q, want the soho closing marker", PeerEndPrefix)
	}
	id := "01020304"
	end := PeerEndLine(id)
	if end != "[/soho:peer] #01020304 end of message" {
		t.Fatalf("PeerEndLine(%q)=%q", id, end)
	}
	header := PeerHeaderFor(PeerSender{Ref: "local/w1:p1"}, id)
	if !strings.HasPrefix(header, "[soho:peer] #01020304 Message from another agent") {
		t.Fatalf("sent header does not open with the soho marker:\n%s", header)
	}
	if !strings.Contains(header, "soho send local/w1:p1") {
		t.Fatalf("sent header reply line is not the soho send command:\n%s", header)
	}
}

func TestSohoPeerLegacyAcceptance(t *testing.T) {
	id := "01020304"
	legacyEnd := "[/herdr-soho:peer] #" + id + " end of message"
	legacyOpen := "[herdr-soho:peer] #" + id + " Message from another agent — local/w1:p1."
	currentEnd := "[/soho:peer] #" + id + " end of message"
	currentOpen := "[soho:peer] #" + id + " Message from another agent — local/w1:p1."
	t.Run("the exact end line proves delivery in both formats", func(t *testing.T) {
		for _, line := range []string{legacyEnd, "› " + legacyEnd, currentEnd, "› " + currentEnd} {
			if !peerEndLineIn(line, id) {
				t.Fatalf("peerEndLineIn(%q)=%v, want the end line to prove delivery", line, false)
			}
		}
	})
	t.Run("the opening header is a receipt marker in both formats", func(t *testing.T) {
		for _, line := range []string{legacyOpen, "› " + legacyOpen, currentOpen, "› " + currentOpen} {
			if !receiptLineIn(line, id) {
				t.Fatalf("receiptLineIn(%q)=%v, want the header to count", line, false)
			}
		}
	})
	t.Run("a longer id with the same prefix is not this message, in either format", func(t *testing.T) {
		for _, line := range []string{"[herdr-soho:peer] #01020304abcd Message from another agent — local/w1:p1.", "[soho:peer] #01020304abcd Message from another agent — local/w1:p1."} {
			if receiptLineIn(line, id) {
				t.Fatalf("receiptLineIn(%q) counted a longer id", line)
			}
		}
	})
	t.Run("quoted lines never count, in both formats", func(t *testing.T) {
		for _, line := range []string{"> " + legacyEnd, "  > " + currentEnd, "> " + legacyOpen, "  > " + currentOpen} {
			if peerEndLineIn(line, id) || receiptLineIn(line, id) || receiptLineInText(line, id) {
				t.Fatalf("quoted line %q counted as a receipt marker", line)
			}
		}
	})
	t.Run("the composer header and the codex history recognition accept both formats", func(t *testing.T) {
		if !composerHeaderLineIn(legacyOpen, id) || !composerHeaderLineIn(currentOpen, id) {
			t.Fatalf("composerHeaderLineIn must accept both formats: legacy=%v current=%v", composerHeaderLineIn(legacyOpen, id), composerHeaderLineIn(currentOpen, id))
		}
		if composerHeaderLineIn("> "+legacyOpen, id) || composerHeaderLineIn("> "+currentOpen, id) {
			t.Fatal("composerHeaderLineIn counted a quoted line")
		}
		// A codex history turn: the closing line (either format) plus a turn
		// line after it proves the message was delivered.
		legacyScreen := "› " + legacyOpen + "\n• working\n" + legacyEnd + "\n• done\n"
		currentScreen := "› " + currentOpen + "\n• working\n" + currentEnd + "\n• done\n"
		if !peerMessageInHistory(legacyScreen, id) || !peerMessageInHistory(currentScreen, id) {
			t.Fatalf("peerMessageInHistory must accept both formats: legacy=%v current=%v", peerMessageInHistory(legacyScreen, id), peerMessageInHistory(currentScreen, id))
		}
	})
}
