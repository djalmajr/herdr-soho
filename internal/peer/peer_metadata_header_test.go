package peer_test

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	_ "unsafe"

	"github.com/djalmajr/herdr-soho/internal/peer"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

//go:linkname receiptLineIn github.com/djalmajr/herdr-soho/internal/peer.receiptLineIn
func receiptLineIn(line, id string) bool

// bannedHeaderWords are the substrings the metadata-only peer header must
// never carry: the old intent/approval disclaimer and its behavioral
// instruction are gone from every emitted header.
var bannedHeaderWords = []string{"intent", "approval", "authorized", "not from your user", "if useful"}

func assertMetadataOnly(t *testing.T, label, header string) {
	t.Helper()
	for _, banned := range bannedHeaderWords {
		if strings.Contains(header, banned) {
			t.Fatalf("%s header carries %q:\n%q", label, banned, header)
		}
	}
}

// TestPeerHeaderMetadataOnlyBytes pins the exact emitted header bytes for
// every field-knownness shape: the Sender line lists only the known
// name/kind/role/model items and is omitted when nothing is known, the send
// time on the opening line is the pinned instant, and unknown fields are
// never placeholders.
func TestPeerHeaderMetadataOnlyBytes(t *testing.T) {
	t.Run("local header with all four sender fields known", func(t *testing.T) {
		got := peer.PeerHeaderFor(peer.PeerSender{Ref: "local/w14:p1", Name: "soho-s4", Kind: "pi", Role: "sub-orchestrator", Model: "gpt-5.1"}, "01020304")
		want := "[herdr-soho:peer] #01020304 Message from another agent — local/w14:p1, sent " + peerTestSentTime + ".\n" +
			"Sender: name soho-s4; kind pi; role sub-orchestrator; model gpt-5.1.\n" +
			`Reply with: herdr-soho send local/w14:p1 "<your reply>"` + "\n" +
			`The message follows, each line quoted with "> ".`
		if got != want {
			t.Fatalf("header=%q want=%q", got, want)
		}
		assertMetadataOnly(t, "local", got)
	})
	t.Run("local header with only some fields known omits the unknown ones", func(t *testing.T) {
		got := peer.PeerHeaderFor(peer.PeerSender{Ref: "local/w14:p1", Name: "soho-s4", Kind: "pi", Role: "-", Model: "unknown"}, "01020304")
		want := "[herdr-soho:peer] #01020304 Message from another agent — local/w14:p1, sent " + peerTestSentTime + ".\n" +
			"Sender: name soho-s4; kind pi.\n" +
			`Reply with: herdr-soho send local/w14:p1 "<your reply>"` + "\n" +
			`The message follows, each line quoted with "> ".`
		if got != want {
			t.Fatalf("header=%q want=%q", got, want)
		}
		assertMetadataOnly(t, "local", got)
	})
	t.Run("local header with nothing known has no Sender line and the local/- ref", func(t *testing.T) {
		got := peer.PeerHeaderFor(peer.PeerSender{Ref: "local/-", Name: "-", Kind: "-", Role: "-", Model: "-"}, "01020304")
		want := "[herdr-soho:peer] #01020304 Message from another agent — local/-, sent " + peerTestSentTime + ".\n" +
			`Reply with: herdr-soho send local/- "<your reply>"` + "\n" +
			`The message follows, each line quoted with "> ".`
		if got != want {
			t.Fatalf("header=%q want=%q", got, want)
		}
		assertMetadataOnly(t, "local", got)
	})
	t.Run("remote header with a hostname", func(t *testing.T) {
		got := peer.PeerHeaderRemoteFor(peer.PeerSender{Name: "orchestrator-2", Kind: "claude", Role: "-", Model: "claude-opus-5"}, "w14:p1", "Run2Biz.local", "01020304")
		want := "[herdr-soho:peer] #01020304 Message from another agent — w14:p1 on Run2Biz.local, sent " + peerTestSentTime + ".\n" +
			"Sender: name orchestrator-2; kind claude; model claude-opus-5.\n" +
			`Reply with: herdr-soho send <this machine's name in your herdr machine list>/w14:p1 "<your reply>" (this machine is Run2Biz.local)` + "\n" +
			`The message follows, each line quoted with "> ".`
		if got != want {
			t.Fatalf("header=%q want=%q", got, want)
		}
		assertMetadataOnly(t, "remote", got)
	})
	t.Run("remote header with an empty hostname has no machine part or parenthetical", func(t *testing.T) {
		got := peer.PeerHeaderRemoteFor(peer.PeerSender{Name: "orchestrator-2", Kind: "claude", Role: "-", Model: "-"}, "w14:p1", "", "01020304")
		want := "[herdr-soho:peer] #01020304 Message from another agent — w14:p1, sent " + peerTestSentTime + ".\n" +
			"Sender: name orchestrator-2; kind claude.\n" +
			`Reply with: herdr-soho send <this machine's name in your herdr machine list>/w14:p1 "<your reply>"` + "\n" +
			`The message follows, each line quoted with "> ".`
		if got != want {
			t.Fatalf("header=%q want=%q", got, want)
		}
		assertMetadataOnly(t, "remote", got)
	})
	t.Run("the send time on the opening line is the pinned instant in UTC RFC 3339 seconds", func(t *testing.T) {
		got := peer.PeerHeaderFor(peer.PeerSender{Ref: "local/w14:p1", Name: "-", Kind: "-", Role: "-", Model: "-"}, "01020304")
		lines := strings.Split(got, "\n")
		wantFirst := "[herdr-soho:peer] #01020304 Message from another agent — local/w14:p1, sent " + peerTestSentTime + "."
		if len(lines) != 3 || lines[0] != wantFirst || peerTestSentTime != "2026-10-09T14:22:05Z" {
			t.Fatalf("header=%q (the opening line must carry the pinned send time %q)", got, wantFirst)
		}
	})
	t.Run("the header is at most four lines on every path", func(t *testing.T) {
		headers := []string{
			peer.PeerHeaderFor(peer.PeerSender{Ref: "local/w14:p1", Name: "soho-s4", Kind: "pi", Role: "sub-orchestrator", Model: "gpt-5.1"}, "01020304"),
			peer.PeerHeaderFor(peer.PeerSender{Ref: "local/-", Name: "-", Kind: "-", Role: "-", Model: "-"}, "01020304"),
			peer.PeerHeaderRemoteFor(peer.PeerSender{Name: "orchestrator-2", Kind: "claude", Role: "sub-orchestrator", Model: "claude-opus-5"}, "w14:p1", "Run2Biz.local", "01020304"),
			peer.PeerHeaderRemoteFor(peer.PeerSender{Name: "-", Kind: "-", Role: "-", Model: "-"}, "w14:p1", "", "01020304"),
			peer.PeerHeaderAssignment(peer.PeerSender{Ref: "local/ws:p1", Name: "author", Kind: "codex", Role: "implementer", Model: "gpt-5.4"}, "author", "c-0102030405060708", "01020304"),
		}
		for i, h := range headers {
			if lines := strings.Count(h, "\n") + 1; lines > 4 {
				t.Fatalf("header %d has %d lines (the codex composer only recognizes the opening line among the last 8 non-empty screen lines): %q", i+1, lines, h)
			}
		}
	})
	t.Run("an empty id keeps the opening line without the id", func(t *testing.T) {
		got := peer.PeerHeaderFor(peer.PeerSender{Ref: "local/w14:p1", Name: "soho-s4", Kind: "pi", Role: "-", Model: "-"}, "")
		if !strings.HasPrefix(got, "[herdr-soho:peer] Message from another agent — local/w14:p1, sent ") {
			t.Fatalf("header=%q (no id, no \"#\")", got)
		}
	})
	t.Run("the legacy PeerHeader produces the new format without a model", func(t *testing.T) {
		got := peer.PeerHeader("local/w14:p1", "soho-s4", "pi", "implementer", "01020304")
		want := peer.PeerHeaderFor(peer.PeerSender{Ref: "local/w14:p1", Name: "soho-s4", Kind: "pi", Role: "implementer"}, "01020304")
		if got != want {
			t.Fatalf("legacy=%q want=%q", got, want)
		}
		if strings.Contains(got, " model ") {
			t.Fatalf("the legacy header must not show a model item: %q", got)
		}
		assertMetadataOnly(t, "legacy", got)
	})
}

// TestPeerHeaderAssignmentBytes pins the assignment delivery header: the
// metadata-only local header whose reply line is the assignment reply
// command, with no disclaimer line.
func TestPeerHeaderAssignmentBytes(t *testing.T) {
	s := peer.PeerSender{Ref: "local/ws:p1", Name: "author", Kind: "codex", Role: "implementer", Model: "gpt-5.4"}
	got := peer.PeerHeaderAssignment(s, "author", "c-0102030405060708", "01020304")
	want := "[herdr-soho:peer] #01020304 Message from another agent — local/ws:p1, sent " + peerTestSentTime + ".\n" +
		"Sender: name author; kind codex; role implementer; model gpt-5.4.\n" +
		`Reply within this assignment: herdr-soho send author --assignment c-0102030405060708 --type review.question "<your reply>"` + "\n" +
		`The message follows, each line quoted with "> ".`
	if got != want {
		t.Fatalf("header=%q want=%q", got, want)
	}
	assertMetadataOnly(t, "assignment", got)
}

// TestReceiptLineInNewHeaderOpening proves receipt recognition of the new
// metadata-only opening line: the unquoted line of this id (plain, indented,
// or with the history "›") is the receipt, and a quoted copy of it is
// payload, never the receipt.
func TestReceiptLineInNewHeaderOpening(t *testing.T) {
	id := "01020304"
	open := "[herdr-soho:peer] #" + id + " Message from another agent — local/w14:p1, sent " + peerTestSentTime + "."
	if !receiptLineIn(open, id) {
		t.Fatalf("the new opening line of this id is not a receipt: %q", open)
	}
	if !receiptLineIn("  "+open, id) {
		t.Fatalf("the indented opening line is not a receipt: %q", open)
	}
	if !receiptLineIn("› "+open, id) {
		t.Fatalf("the history-prefixed opening line is not a receipt: %q", open)
	}
	if receiptLineIn("> "+open, id) {
		t.Fatalf("a quoted copy of the opening line proved receipt: %q", open)
	}
	if receiptLineIn("  > "+open, id) {
		t.Fatalf("an indented quoted copy proved receipt: %q", open)
	}
	if receiptLineIn("[herdr-soho:peer] #01020304abcd Message from another agent — local/w14:p9.", id) {
		t.Fatal("a longer id with this one as a prefix proved receipt")
	}
}

// TestSendHeaderModelFromRoster runs the send path with the fake herdr: the
// sender's roster row is the only source of the model, so a row that carries
// column 9 puts the model in the emitted Sender line and a row without it
// emits no model item.
func TestSendHeaderModelFromRoster(t *testing.T) {
	const senderJSON = `{"result":{"agent":{"name":"soho-s4","agent":"pi","agent_status":"idle","state_change_seq":"1"}}}`
	for _, tc := range []struct {
		name       string
		roster     string
		sender     peer.PeerSender
		wantSender string
	}{
		{"a roster row carrying a model puts it in the Sender line",
			"soho-s4\tw0test:p0b\tpi\t-\topenai\t\tstate-dir\t2026-10-09T14:00:00Z\tgpt-5.1\n",
			peer.PeerSender{Ref: "local/w0test:p0b", Name: "soho-s4", Kind: "pi", Model: "gpt-5.1"},
			"Sender: name soho-s4; kind pi; model gpt-5.1."},
		{"a roster row without a model column emits no model item",
			"soho-s4\tw0test:p0b\tpi\t-\n",
			peer.PeerSender{Ref: "local/w0test:p0b", Name: "soho-s4", Kind: "pi"},
			"Sender: name soho-s4; kind pi."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "01020304"
			oldReader := rand.Reader
			rand.Reader = bytes.NewReader([]byte{1, 2, 3, 4})
			t.Cleanup(func() { rand.Reader = oldReader })
			prompt := peer.PeerHeaderFor(tc.sender, id) + "\n\n> hello\n" + peer.PeerEndLine(id)
			rules := successfulSendRules(prompt, fakecli.Rule{Argv: []string{"agent", "get", "w0test:p0a"}, Call: 1, Stdout: agentJSON("idle", "1")})
			rules = append([]fakecli.Rule{{Argv: []string{"agent", "get", "w0test:p0b"}, Stdout: senderJSON}}, rules...)
			f := newFixture(t, rules)
			f.env["HERDR_PANE_ID"] = "w0test:p0b"
			stateDir := filepath.Join(f.env["HERDR_SOHO_DIR"], "ws-test")
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stateDir, "agents.tsv"), []byte(tc.roster), 0o600); err != nil {
				t.Fatal(err)
			}
			code, _, stderr := f.run([]string{"send", "w0test:p0a", "hello"})
			if code != 0 || stderr != "" {
				t.Fatalf("send code=%d stderr=%q", code, stderr)
			}
			calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
			if err != nil {
				t.Fatal(err)
			}
			var got string
			for _, call := range calls {
				if len(call.Argv) > 3 && call.Argv[1] == "prompt" {
					got = call.Argv[3]
					break
				}
			}
			if got != prompt {
				t.Fatalf("prompt=%q want=%q", got, prompt)
			}
			if !strings.Contains(got, tc.wantSender+"\n") {
				t.Fatalf("the Sender line is not %q:\n%q", tc.wantSender, got)
			}
			assertMetadataOnly(t, "send-path", got[:strings.Index(got, "\n\n")])
		})
	}
}

// TestAssignmentDeliveryHeaderIsMetadataOnly runs a full assignment delivery
// with the fake herdr: the emitted header has no disclaimer, keeps the
// assignment reply line and the Assignment metadata line, and carries the
// sender's model from its roster row.
func TestAssignmentDeliveryHeaderIsMetadataOnly(t *testing.T) {
	f, _, a := assignedFixture(t, nil)
	oldReader := rand.Reader
	rand.Reader = bytes.NewReader([]byte{1, 2, 3, 4})
	t.Cleanup(func() { rand.Reader = oldReader })
	id := "01020304"
	code, out, stderr := f.run([]string{"send", "reviewer", "question", "--assignment", a.ID, "--type", "review.question"})
	if code != 0 || !strings.Contains(out, `"status":"submitted"`) {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, stderr)
	}
	prompts := assignmentPrompts(t, f)
	if len(prompts) != 1 {
		t.Fatalf("delivery=%+v", prompts)
	}
	wantMessage := "[herdr-soho:peer] #" + id + " Message from another agent — local/ws:p1, sent " + peerTestSentTime + ".\n" +
		"Sender: name author; kind codex; role implementer; model gpt-5.4.\n" +
		fmt.Sprintf("Reply within this assignment: herdr-soho send author --assignment %s --type review.question \"<your reply>\"\n", a.ID) +
		`The message follows, each line quoted with "> ".` + "\n" +
		fmt.Sprintf("Assignment: %s; type: review.question; round: %d; revision: %s.\n\n", a.ID, a.Round, a.Revision) +
		"> question\n" +
		"[/herdr-soho:peer] #" + id + " end of message"
	if prompts[0].Argv[3] != wantMessage {
		t.Fatalf("message=%q want=%q", prompts[0].Argv[3], wantMessage)
	}
	assertMetadataOnly(t, "assignment delivery", prompts[0].Argv[3][:strings.Index(prompts[0].Argv[3], "\n\n")])
}
