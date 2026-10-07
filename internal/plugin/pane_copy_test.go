package plugin

// TestPaneCopy pins the pane-copy operator correction (unbracketed final
// format): c copies the selected pane's full reference - EXACTLY the
// sanitized ref, no square brackets, no metadata, no trailing newline -
// in the list focus and keeps the modal open with a status-area
// confirmation; in the search focus c is ordinary text; Enter navigates
// and focuses (Ctrl+Enter the compatibility alias) and closes only on a
// verified navigation; the copy is gated while a navigation is pending,
// on an empty visible list, on a missing pinned ref and on a ref-less
// entry; the real loop copies twice through the fake clipboard, stays
// open and confirms in the status area, then Esc closes; and both
// footers show the c Copy / Enter Focus / Esc Close guidance at normal
// and narrow widths.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func paneCopyEntries() []PickerEntry {
	return []PickerEntry{
		{"ref": "local/w3:p", "machine": "local", "workspace_id": "w3", "workspace_label": "alpha", "tab_id": "w3:t1", "name": "orchestrator", "kind": "claude", "status": "working", "cwd": "/srv/three"},
		{"ref": "local/w4:q", "machine": "local", "workspace_id": "w4", "workspace_label": "beta", "tab_id": "w4:t1", "name": "worker", "kind": "codex", "status": "idle", "cwd": "/srv/four"},
		{"ref": "windows/w3:p", "machine": "windows", "workspace_id": "w3", "workspace_label": "alpha", "tab_id": "w3:t1", "name": "remote", "kind": "claude", "status": "working", "cwd": "C:\\repo"},
	}
}

func TestPaneCopy(t *testing.T) {
	t.Run("payload is exactly the full sanitized reference", func(t *testing.T) {
		local := paneCopyEntries()[0]
		if got := PickerCopyPayload(local); got != "local/w3:p" {
			t.Fatalf("local payload=%q want local/w3:p (no brackets, metadata or newline)", got)
		}
		remote := paneCopyEntries()[2]
		if got := PickerCopyPayload(remote); got != "windows/w3:p" {
			t.Fatalf("remote payload=%q want windows/w3:p (the machine prefix is never re-labeled local)", got)
		}
		// control characters are stripped, like every other display value
		noisy := PickerEntry{"ref": "local/w14:p\x1b[31m6M", "name": "orchestrator", "status": "working", "cwd": "/tmp/x"}
		if got := PickerCopyPayload(noisy); got != "local/w14:p6M" {
			t.Fatalf("stripped payload=%q want local/w14:p6M", got)
		}
		// the metadata never leaks into the payload
		if got := PickerCopyPayload(local); strings.Contains(got, "orchestrator") || strings.Contains(got, "working") || strings.Contains(got, "/srv/three") || strings.Contains(got, "[") {
			t.Fatalf("payload=%q carries metadata or brackets", got)
		}
		// an empty or missing reference fabricates no routable reference
		if got := PickerCopyPayload(PickerEntry{"ref": "", "name": "x"}); got != "" {
			t.Fatalf("empty ref payload=%q want empty", got)
		}
		if got := PickerCopyPayload(PickerEntry{"name": "x", "machine": "local"}); got != "" {
			t.Fatalf("missing ref payload=%q want empty", got)
		}
	})

	t.Run("c copies in list focus and is text in search focus", func(t *testing.T) {
		s := NewBoardState()
		s.ViewState.Enabled = true
		s.Entries = paneCopyEntries()
		if got := s.ApplyKey("c"); got != "copy" || s.Copied == nil || *s.Copied != "local/w3:p" {
			t.Fatalf("c in list focus: action=%q copied=%v want copy local/w3:p", got, s.Copied)
		}
		if s.Exit != "" || s.NavPending || s.Selected != 0 {
			t.Fatalf("the copy must keep the modal open and the selection: exit=%q pending=%v sel=%d", s.Exit, s.NavPending, s.Selected)
		}
		// a second copy works on the same open modal
		if got := s.ApplyKey("c"); got != "copy" || *s.Copied != "local/w3:p" {
			t.Fatalf("second c: action=%q copied=%v want another copy", got, s.Copied)
		}
		// search focus: c is ordinary text, nothing is copied
		s.ViewState.SearchFocused = true
		if got := s.ApplyKey("c"); got != "" || !strings.HasSuffix(s.Query, "c") || *s.Copied != "local/w3:p" {
			t.Fatalf("c in search focus: action=%q query=%q want ordinary text", got, s.Query)
		}
		// the picker state: the same contract, incl. the kitty encoded printable c
		p := NewPickerState()
		p.ViewState.Enabled = true
		p.Entries = paneCopyEntries()
		if got := p.ApplyKey("c"); got != "copy" || p.Copied == nil || *p.Copied != "local/w3:p" {
			t.Fatalf("picker c in list focus: action=%q copied=%v", got, p.Copied)
		}
		pk := NewPickerState()
		pk.ViewState.Enabled = true
		pk.Entries = paneCopyEntries()
		pk.ViewState.SearchFocused = true
		if got := pk.FeedChunk("\x1b[99u"); got != "" || !strings.HasSuffix(pk.Query, "c") || pk.Copied != nil {
			t.Fatalf("kitty c in search focus: action=%q query=%q want ordinary text", got, pk.Query)
		}
		pk2 := NewPickerState()
		pk2.ViewState.Enabled = true
		pk2.Entries = paneCopyEntries()
		if got := pk2.FeedChunk("\x1b[99u"); got != "copy" || pk2.Copied == nil || *pk2.Copied != "local/w3:p" {
			t.Fatalf("kitty c in list focus: action=%q copied=%v want the copy", got, pk2.Copied)
		}
		// the legacy (non-TTY) input keeps the coherent alias: c copies
		leg := NewPickerState()
		leg.Entries = paneCopyEntries()
		if got := leg.ApplyKey("c"); got != "copy" || leg.Copied == nil || *leg.Copied != "local/w3:p" {
			t.Fatalf("legacy c: action=%q copied=%v want the coherent copy", got, leg.Copied)
		}
	})

	t.Run("enter navigates and copies nothing", func(t *testing.T) {
		s := NewBoardState()
		s.ViewState.Enabled = true
		s.Entries = paneCopyEntries()
		if got := s.ApplyKey("enter"); got != "navigate" || !s.NavPending || pickerString(s.NavTarget["ref"]) != "local/w3:p" {
			t.Fatalf("enter: action=%q pending=%v target=%v want the frozen navigation", got, s.NavPending, s.NavTarget)
		}
		if s.Copied != nil || s.Exit != "" {
			t.Fatalf("enter copied (%v) or closed (exit=%q)", s.Copied, s.Exit)
		}
		// the compatibility alias: Ctrl+Enter, never a copy
		a := NewBoardState()
		a.ViewState.Enabled = true
		a.Entries = paneCopyEntries()
		if got := a.ApplyKey("ctrl-enter"); got != "navigate" || a.Copied != nil {
			t.Fatalf("ctrl-enter: action=%q copied=%v want the navigation alias", got, a.Copied)
		}
		// the picker state routes Enter the same way
		p := NewPickerState()
		p.Entries = paneCopyEntries()
		if got := p.ApplyKey("enter"); got != "navigate" || !p.NavPending || p.Copied != nil {
			t.Fatalf("picker enter: action=%q pending=%v copied=%v", got, p.NavPending, p.Copied)
		}
		// a verified navigation closes; a failure keeps the modal open
		ok := NewBoardState()
		ok.NavTarget = paneCopyEntries()[0]
		boardNavApplyResult(ok, NavigationResult{OK: true})
		if ok.Exit != "navigate" {
			t.Fatalf("verified navigation: exit=%q want navigate", ok.Exit)
		}
		fail := NewBoardState()
		fail.NavTarget = paneCopyEntries()[0]
		boardNavApplyResult(fail, NavigationResult{Cause: "selected pane no longer exists on this machine"})
		if fail.Exit != "" || fail.NavPending || len(fail.Failures) != 1 {
			t.Fatalf("failed navigation: exit=%q pending=%v failures=%d want open with the failure", fail.Exit, fail.NavPending, len(fail.Failures))
		}
	})

	t.Run("c and enter stay gated", func(t *testing.T) {
		// while a navigation is pending
		s := NewBoardState()
		s.ViewState.Enabled = true
		s.Entries = paneCopyEntries()
		if got := s.ApplyKey("enter"); got != "navigate" {
			t.Fatalf("setup enter: action=%q", got)
		}
		if got := s.ApplyKey("c"); got != "" || s.Copied != nil {
			t.Fatalf("c while pending: action=%q copied=%v want nothing", got, s.Copied)
		}
		if got := s.ApplyKey("enter"); got != "" || !s.NavPending {
			t.Fatalf("enter while pending: action=%q want nothing", got)
		}
		// on an empty visible list
		e := NewBoardState()
		e.ViewState.Enabled = true
		e.Entries = paneCopyEntries()
		e.Query = "zzz-not-there"
		if got := e.ApplyKey("c"); got != "" || e.Copied != nil {
			t.Fatalf("c on the empty list: action=%q copied=%v want nothing", got, e.Copied)
		}
		if got := e.ApplyKey("enter"); got != "" {
			t.Fatalf("enter on the empty list: action=%q want nothing", got)
		}
		// when the pinned selected ref left the load
		m := NewBoardState()
		m.ViewState.Enabled = true
		m.refreshing = true
		m.selectedRef = "local/w9:gone"
		m.Entries = paneCopyEntries()
		m.Selected = 0
		if got := m.ApplyKey("c"); got != "" || m.Copied != nil {
			t.Fatalf("c with the missing pin: action=%q copied=%v want nothing", got, m.Copied)
		}
		if got := m.ApplyKey("enter"); got != "" || m.NavPending {
			t.Fatalf("enter with the missing pin: action=%q pending=%v want nothing", got, m.NavPending)
		}
		// an entry without a reference fabricates no payload
		n := NewBoardState()
		n.ViewState.Enabled = true
		n.Entries = []PickerEntry{{"ref": "", "name": "orphan", "kind": "claude"}}
		if got := n.ApplyKey("c"); got != "" || n.Copied != nil || n.LastEntry != nil {
			t.Fatalf("c on the ref-less entry: action=%q copied=%v want nothing", got, n.Copied)
		}
	})

	t.Run("copy notice wording, survival and navigation takeover", func(t *testing.T) {
		if got := copyNoticeFor(ClipboardResult{Path: "pbcopy"}, "local/w3:p"); got != "Copied local/w3:p" {
			t.Fatalf("native notice=%q want Copied local/w3:p", got)
		}
		if got := copyNoticeFor(ClipboardResult{Path: "osc52"}, "local/w3:p"); got != "Copied via OSC 52 (unconfirmed): local/w3:p" {
			t.Fatalf("osc52 notice=%q want the honest unconfirmed wording", got)
		}
		// a progressive refresh publication must not drop the confirmation
		s := NewBoardState()
		s.ViewState.Enabled = true
		s.Entries = paneCopyEntries()
		s.CopyNotice = "Copied local/w3:p"
		u := NewBoardState()
		u.Entries = paneCopyEntries()
		u.UpdatedAt = "10:00:00"
		applyBoardUpdate(s, u)
		if s.CopyNotice != "Copied local/w3:p" {
			t.Fatalf("the refresh publication dropped the copy notice: %q", s.CopyNotice)
		}
		// the confirmation shows in the status area while nothing runs
		var plain strings.Builder
		redrawSession(&plain, s, 80)
		if !strings.Contains(plain.String(), "Copied local/w3:p") {
			t.Fatalf("the copy notice must show in the status area:\n%s", plain.String())
		}
		// a started navigation supersedes it
		s.NavPending = true
		s.NavTarget = paneCopyEntries()[0]
		var pending strings.Builder
		redrawSession(&pending, s, 80)
		if !strings.Contains(pending.String(), "Focusing selected pane… Esc cancels") || strings.Contains(pending.String(), "Copied local/w3:p") {
			t.Fatalf("the pending navigation must supersede the copy notice:\n%s", pending.String())
		}
	})

	t.Run("footer guidance at normal and narrow widths", func(t *testing.T) {
		opts := SessionRenderOptions{Entries: paneCopyEntries(), Selected: 0, Noun: "session", Board: true}
		normalLines := strings.Split(strings.TrimSuffix(renderSessionPopup(opts, 80, 24), "\n"), "\n")
		if got := normalLines[len(normalLines)-1]; !strings.Contains(got, "c Copy · Enter Focus · Esc Close") {
			t.Fatalf("enhanced normal footer=%q want the full guidance", got)
		}
		narrowLines := strings.Split(strings.TrimSuffix(renderSessionPopup(opts, 30, 24), "\n"), "\n")
		if got := strings.TrimLeft(narrowLines[len(narrowLines)-1], " "); got != "Enter Focus · Esc Close" {
			t.Fatalf("enhanced narrow footer=%q want Enter Focus · Esc Close (the close keeps its priority)", got)
		}
		// the legacy modal footer, normal and tiny
		legacyLines := strings.Split(strings.TrimSuffix(renderPickerLegacyModal(paneCopyEntries(), 0, 3, "", 0, false, nil, "", 80, 24), "\n"), "\n")
		if got := legacyLines[len(legacyLines)-1]; got != "3 panes · ↑↓ select · c Copy · Enter Focus · Esc Close" {
			t.Fatalf("legacy normal footer=%q", got)
		}
		tinyLines := strings.Split(strings.TrimSuffix(renderPickerLegacyModal(paneCopyEntries(), 0, 3, "", 0, false, nil, "", 20, 24), "\n"), "\n")
		if got := tinyLines[len(tinyLines)-1]; got != "Enter Focus · Esc" {
			t.Fatalf("legacy tiny footer=%q want Enter Focus · Esc (the cancellation wins the tiny width)", got)
		}
	})

	t.Run("the real loop copies twice, stays open, confirms and Esc closes", func(t *testing.T) {
		dir := t.TempDir()
		local := `{"result":{"snapshot":{"workspaces":[{"workspace_id":"w3","label":"alpha"},{"workspace_id":"w4","label":"beta"}],"tabs":[{"tab_id":"w3:t1","label":"1"},{"tab_id":"w4:t1","label":"2"}],"agents":[{"pane_id":"w3:p","name":"orchestrator","agent":"claude","agent_status":"working"}],"panes":[{"pane_id":"w3:p","workspace_id":"w3","tab_id":"w3:t1"},{"pane_id":"w4:q","workspace_id":"w4","tab_id":"w4:t1"}]}}}`
		herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
			{Argv: []string{"api", "snapshot"}, Stdout: local},
			{Argv: []string{"machine", "list", "--json"}, Stdout: "[]"},
		})
		if err != nil {
			t.Fatal(err)
		}
		clipboard := ClipboardCandidates(platform.Current())[0][0]
		if _, err := fakecli.InstallWithOptions(t, dir, clipboard, []fakecli.Rule{{AnyArgs: true}}, fakecli.InstallOptions{CaptureStdin: true}); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{}
		for _, item := range fakecli.Env(testutil.CleanEnv(t), dir, fakecli.EnvOptions{IncludeBasePath: true}) {
			key, value, ok := strings.Cut(item, "=")
			if ok {
				env[key] = value
			}
		}
		env["HERDR_BIN_PATH"] = herdr
		oldInterval := boardRefreshInterval
		boardRefreshInterval = time.Hour
		defer func() { boardRefreshInterval = oldInterval }()

		input, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := os.CreateTemp(t.TempDir(), "pane-copy-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = input.Close()
			_ = writer.Close()
			_ = output.Close()
		})
		shutdown := make(chan os.Signal, 1)
		finished := make(chan int, 1)
		state := NewBoardState()
		go func() {
			finished <- runSessionLoopWithSignals(env, platform.Current(), herdr, input, output, true, shutdown, true, state)
		}()
		// wait until the discovered rows are drawn, then copy twice and Esc
		deadline := time.Now().Add(10 * time.Second)
		drawn := false
		for time.Now().Before(deadline) {
			data, _ := os.ReadFile(output.Name())
			if strings.Contains(string(data), "orchestrator") {
				drawn = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !drawn {
			data, _ := os.ReadFile(output.Name())
			t.Fatalf("the discovered rows never appeared:\n%s", data)
		}
		if _, err := writer.Write([]byte("cc\x1b")); err != nil {
			t.Fatal(err)
		}
		_ = writer.Close()
		var code int
		select {
		case code = <-finished:
		case <-time.After(10 * time.Second):
			t.Fatal("the loop did not close after Esc")
		}
		if code != 0 {
			t.Fatalf("loop code=%d want 0 (Esc closed the modal, not a copy)", code)
		}
		calls, err := fakecli.ReadCallsForConfig(filepath.Join(dir, clipboard+".json"))
		if err != nil || len(calls) != 2 {
			t.Fatalf("clipboard calls=%#v err=%v want exactly two copies", calls, err)
		}
		for i, call := range calls {
			if call.Stdin != "local/w3:p" {
				t.Fatalf("clipboard call %d stdin=%q want exactly local/w3:p (no brackets, metadata or newline)", i, call.Stdin)
			}
		}
		data, _ := os.ReadFile(output.Name())
		if !strings.Contains(string(data), "Copied local/w3:p") {
			t.Fatalf("the status area never confirmed the copy:\n%s", data)
		}
		if strings.Contains(string(data), "Focusing selected pane") {
			t.Fatalf("c must never navigate:\n%s", data)
		}
		if !strings.Contains(string(data), "c Copy · Enter Focus · Esc Close") {
			t.Fatalf("the footer never showed the new guidance:\n%s", data)
		}
		herdrCalls, err := fakecli.ReadCallsForConfig(filepath.Join(dir, "herdr.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, call := range herdrCalls {
			if len(call.Argv) > 0 && call.Argv[0] == "notification" {
				t.Fatalf("the copy issued a Herdr notification call: %#v", herdrCalls)
			}
		}
	})
}
