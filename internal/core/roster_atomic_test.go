package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// subOrchRow is the row the tests promote: pane ws:pS, name sub-orch, role
// sub-orchestrator. The compare-and-remove validates the whole line, so the
// tests pass complete expected rows.
const subOrchRow = "sub-orch\tws:pS\tcodex\tsub-orchestrator\topenai\t0\t/repo\tsub-start\tgpt-5.4\task\tsub-orchestrator\t-"

// rosterAtomicFixture returns a state dir holding the header, the row the
// tests promote and two sibling rows that must survive any removal.
func rosterAtomicFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rows := rosterHeader +
		subOrchRow + "\n" +
		"impl-1\tws:p1\tclaude\timplementer\tanthropic\t0\t/repo\timpl-start\tclaude-opus-5-5\task\timplementer\tbuild\n" +
		"rev-1\tws:p2\tcodex\treviewer\topenai\t0\t/repo\trev-start\tgpt-5.4\task\treviewer\treview\n"
	if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(rows), 0o666); err != nil {
		t.Fatal(err)
	}
	return dir
}

func rosterAtomicRead(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "agents.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// makeUnreadable makes path unreadable for the test user (mode 000) and
// restores it in cleanup; it skips under root, where mode 000 does not
// deny reads.
func makeUnreadable(t *testing.T, path string) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skipf("running as root: mode 000 does not deny reads to %s", path)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
}

func TestRosterRemoveIfRemovesExactlyTheMatchingRow(t *testing.T) {
	dir := rosterAtomicFixture(t)
	removed, err := RosterRemoveIf(dir, "ws:pS", subOrchRow)
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	want := rosterHeader +
		"impl-1\tws:p1\tclaude\timplementer\tanthropic\t0\t/repo\timpl-start\tclaude-opus-5-5\task\timplementer\tbuild\n" +
		"rev-1\tws:p2\tcodex\treviewer\topenai\t0\t/repo\trev-start\tgpt-5.4\task\treviewer\treview\n"
	if got := rosterAtomicRead(t, dir); got != want {
		t.Fatalf("roster after removal:\n%q\nwant:\n%q", got, want)
	}
}

// TestRosterRemoveIfRejectsAChangedRow is the changed-row consumer: a
// concurrent writer rewrote the row between the caller's check and the
// compare-and-remove. The pane now holds a row that is not the verified
// one, so the removal refuses with a corrupt-roster error and writes
// nothing (a bare no-op would let a stale verification pass silently).
func TestRosterRemoveIfRejectsAChangedRow(t *testing.T) {
	dir := rosterAtomicFixture(t)
	changed := rosterAtomicRead(t, dir)
	changed = strings.Replace(changed, "sub-orch\tws:pS", "sub-orch-2\tws:pS", 1)
	if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(changed), 0o666); err != nil {
		t.Fatal(err)
	}
	removed, err := RosterRemoveIf(dir, "ws:pS", subOrchRow)
	if removed || err == nil {
		t.Fatalf("removed=%v err=%v; a changed same-pane row must refuse", removed, err)
	}
	if !strings.Contains(err.Error(), "ws:pS") || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("the refusal must name the pane and the corrupt roster: %v", err)
	}
	if got := rosterAtomicRead(t, dir); got != changed {
		t.Fatalf("the changed row was touched:\n%q", got)
	}
}

// TestRosterRemoveIfRejectsAChangedSessionRow is the concurrent-session
// consumer: name, pane and role are unchanged, but the owner/session fields
// (started) changed between the check and the write. The full-row compare
// must refuse the removal (the pane holds a different row than the verified
// one) and touch nothing.
func TestRosterRemoveIfRejectsAChangedSessionRow(t *testing.T) {
	for _, field := range []string{"started", "cwd", "model"} {
		t.Run(field, func(t *testing.T) {
			dir := rosterAtomicFixture(t)
			changed := strings.Replace(rosterAtomicRead(t, dir), subOrchRow, strings.Replace(subOrchRow, "sub-start", "other-start", 1), 1)
			if field == "cwd" {
				changed = strings.Replace(rosterAtomicRead(t, dir), subOrchRow, strings.Replace(subOrchRow, "/repo", "/other", 1), 1)
			}
			if field == "model" {
				changed = strings.Replace(rosterAtomicRead(t, dir), subOrchRow, strings.Replace(subOrchRow, "gpt-5.4", "gpt-5.3", 1), 1)
			}
			if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(changed), 0o666); err != nil {
				t.Fatal(err)
			}
			removed, err := RosterRemoveIf(dir, "ws:pS", subOrchRow)
			if removed || err == nil {
				t.Fatalf("%s changed: removed=%v err=%v; a same-name/pane/role row with changed session fields must refuse", field, removed, err)
			}
			if got := rosterAtomicRead(t, dir); got != changed {
				t.Fatalf("the changed row was touched:\n%q", got)
			}
		})
	}
}

// TestRosterRemoveIfRejectsASecondRowForTheSamePane is the mandated
// second-row consumer: while the verified row is still present, a DIFFERENT
// row for the same pane (a re-dispatch into the pane, a renamed row, or a
// fresh session) must refuse the removal — the old rule only rejected an
// exact duplicate and would have removed the verified row and left the
// caller's pane holding a worker row.
func TestRosterRemoveIfRejectsASecondRowForTheSamePane(t *testing.T) {
	t.Run("a differently-named second row for the same pane", func(t *testing.T) {
		dir := rosterAtomicFixture(t)
		second := "sub-orch-2\tws:pS\tcodex\tsub-orchestrator\topenai\t0\t/repo\tnew-start\tgpt-5.4\task\tsub-orchestrator\t-"
		before := rosterAtomicRead(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(before+second+"\n"), 0o666); err != nil {
			t.Fatal(err)
		}
		removed, err := RosterRemoveIf(dir, "ws:pS", subOrchRow)
		if removed || err == nil {
			t.Fatalf("removed=%v err=%v; a second different row for the same pane must refuse (the caller would be left in the worker roster)", removed, err)
		}
		if !strings.Contains(err.Error(), "ws:pS") || !strings.Contains(err.Error(), "corrupt") {
			t.Fatalf("the refusal must name the pane and the corrupt roster: %v", err)
		}
		if got := rosterAtomicRead(t, dir); got != before+second+"\n" {
			t.Fatalf("the roster was rewritten on a refusal:\n%q", got)
		}
	})
	t.Run("a same-name row with a new session", func(t *testing.T) {
		dir := rosterAtomicFixture(t)
		second := strings.Replace(subOrchRow, "sub-start", "other-start", 1)
		before := rosterAtomicRead(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(before+second+"\n"), 0o666); err != nil {
			t.Fatal(err)
		}
		removed, err := RosterRemoveIf(dir, "ws:pS", subOrchRow)
		if removed || err == nil {
			t.Fatalf("removed=%v err=%v; a second same-name row with a new session must refuse", removed, err)
		}
		if got := rosterAtomicRead(t, dir); got != before+second+"\n" {
			t.Fatalf("the roster was rewritten on a refusal:\n%q", got)
		}
	})
}

func TestRosterRemoveIfRejectsAMissingRow(t *testing.T) {
	dir := rosterAtomicFixture(t)
	before := rosterAtomicRead(t, dir)
	// A complete row the pane does not hold: the pane's actual row is a
	// different row, so the stale verification refuses (no write).
	roleMismatch := strings.Replace(subOrchRow, "sub-orchestrator\topenai", "implementer\tanthropic", 1)
	removed, err := RosterRemoveIf(dir, "ws:pS", roleMismatch)
	if removed || err == nil {
		t.Fatalf("removed=%v err=%v; a stale verification over a different same-pane row must refuse", removed, err)
	}
	// A complete row for a pane with no rows: no-op.
	removed, err = RosterRemoveIf(dir, "ws:p9", "nobody\tws:p9\tclaude\timplementer\tanthropic\t0\t/repo\tn-start\tm\task\timplementer\t-")
	if removed || err != nil {
		t.Fatalf("removed=%v err=%v; a missing row must not remove", removed, err)
	}
	// An absent roster: no-op, no error.
	empty := t.TempDir()
	removed, err = RosterRemoveIf(empty, "ws:pS", subOrchRow)
	if removed || err != nil {
		t.Fatalf("removed=%v err=%v; an absent roster must be a no-op", removed, err)
	}
	if got := rosterAtomicRead(t, dir); got != before {
		t.Fatalf("the roster changed on a no-op removal:\n%q", got)
	}
}

func TestRosterRemoveIfRejectsACorruptDuplicate(t *testing.T) {
	dir := rosterAtomicFixture(t)
	raw := rosterAtomicRead(t, dir)
	// A duplicate of the exact expected row: two matches is corrupt.
	dup := strings.Replace(raw, subOrchRow+"\n", subOrchRow+"\n"+subOrchRow+"\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(dup), 0o666); err != nil {
		t.Fatal(err)
	}
	removed, err := RosterRemoveIf(dir, "ws:pS", subOrchRow)
	if removed || err == nil {
		t.Fatalf("removed=%v err=%v; two identical rows must be a corrupt-roster error", removed, err)
	}
	if got := rosterAtomicRead(t, dir); got != dup {
		t.Fatalf("the corrupt roster was rewritten:\n%q", got)
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("the error must name the corrupt roster: %v", err)
	}
}

func TestRosterRemoveIfKeepsHeaderOnEveryPath(t *testing.T) {
	dir := rosterAtomicFixture(t)
	// A refused stale verification and a successful removal both keep the
	// header.
	_, _ = RosterRemoveIf(dir, "ws:pS", "nobody\tws:pS\tcodex\tsub-orchestrator\topenai\t0\t/repo\tx\tm\task\tsub-orchestrator\t-")
	got := rosterAtomicRead(t, dir)
	if !strings.HasPrefix(got, rosterHeader) {
		t.Fatalf("the header line was lost on a refusal:\n%q", got)
	}
	if removed, err := RosterRemoveIf(dir, "ws:pS", subOrchRow); !removed || err != nil {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	got = rosterAtomicRead(t, dir)
	if !strings.HasPrefix(got, rosterHeader) {
		t.Fatalf("the header line was lost on removal:\n%q", got)
	}
	// A short row (fewer than the expected fields) for the same pane is a
	// row that is not the verified row: the refusal must leave the file
	// untouched.
	weird := t.TempDir()
	short := rosterHeader + "x\tp1\n"
	if err := os.WriteFile(filepath.Join(weird, "agents.tsv"), []byte(short), 0o666); err != nil {
		t.Fatal(err)
	}
	removed, err := RosterRemoveIf(weird, "p1", "x\tp1\tcodex\timplementer\topenai\t0\t/repo\ts\tm\task\timplementer\t-")
	if removed || err == nil {
		t.Fatalf("removed=%v err=%v; a short same-pane row is not the verified row", removed, err)
	}
	if got := rosterAtomicRead(t, weird); got != short {
		t.Fatalf("the short row was touched:\n%q", got)
	}
}

// TestRosterRemoveIfUnreadableRosterIsAnError is the fail-closed read
// consumer: an existing but unreadable roster is an error (naming the
// unreadable roster), never a vanished row, and the file is left exactly
// as it was.
func TestRosterRemoveIfUnreadableRosterIsAnError(t *testing.T) {
	dir := rosterAtomicFixture(t)
	before := rosterAtomicRead(t, dir)
	path := filepath.Join(dir, "agents.tsv")
	makeUnreadable(t, path)
	removed, err := RosterRemoveIf(dir, "ws:pS", subOrchRow)
	_ = os.Chmod(path, 0o600)
	if removed || err == nil {
		t.Fatalf("removed=%v err=%v; an unreadable roster must be an error, not a vanished row", removed, err)
	}
	if !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("the error must name the unreadable roster: %v", err)
	}
	if got := rosterAtomicRead(t, dir); got != before {
		t.Fatalf("the unreadable roster was rewritten:\n%q", got)
	}
}

// TestRosterRemoveIfRerunsTheCallbackUnderTheLock proves the revalidation
// slot: the beforeRemove callback runs while the roster lock is held, a
// refusal from it removes nothing and surfaces as *RosterRemoveRefusal with
// the caller's message and code, and a passing callback removes as before.
func TestRosterRemoveIfRerunsTheCallbackUnderTheLock(t *testing.T) {
	dir := rosterAtomicFixture(t)
	before := rosterAtomicRead(t, dir)
	lockPath := filepath.Join(dir, "agents.lock")
	var lockHeld bool
	var refusal *RosterRemoveRefusal
	removed, err := RosterRemoveIf(dir, "ws:pS", subOrchRow, func() (string, int) {
		_, statErr := os.Stat(lockPath)
		lockHeld = statErr == nil
		return "the obligation no longer holds", 3
	})
	if removed || err == nil {
		t.Fatalf("removed=%v err=%v; a callback refusal must not remove", removed, err)
	}
	if !lockHeld {
		t.Fatal("the callback must run under the roster lock (agents.lock held)")
	}
	if !errors.As(err, &refusal) || refusal.Code != 3 || refusal.Msg != "the obligation no longer holds" {
		t.Fatalf("the refusal must surface as *RosterRemoveRefusal with the caller's code: %v", err)
	}
	if got := rosterAtomicRead(t, dir); got != before {
		t.Fatalf("the roster was rewritten on a callback refusal:\n%q", got)
	}
	// Control: a passing callback removes exactly as before.
	removed, err = RosterRemoveIf(dir, "ws:pS", subOrchRow, func() (string, int) {
		_, statErr := os.Stat(lockPath)
		lockHeld = statErr == nil
		return "", 0
	})
	if !removed || err != nil {
		t.Fatalf("a passing callback must remove: removed=%v err=%v", removed, err)
	}
	if !lockHeld {
		t.Fatal("the passing callback must also run under the lock")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("the roster lock was left behind: %v", err)
	}
}

func TestStrictRosterLines(t *testing.T) {
	t.Run("an absent roster is an empty roster", func(t *testing.T) {
		dir := t.TempDir()
		lines, err := StrictRosterLines(dir)
		if err != nil || len(lines) != 0 {
			t.Fatalf("lines=%v err=%v; an absent roster is an empty roster", lines, err)
		}
	})
	t.Run("a readable roster returns its raw lines including the header", func(t *testing.T) {
		dir := rosterAtomicFixture(t)
		lines, err := StrictRosterLines(dir)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{
			strings.TrimSuffix(rosterHeader, "\n"),
			subOrchRow,
			"impl-1\tws:p1\tclaude\timplementer\tanthropic\t0\t/repo\timpl-start\tclaude-opus-5-5\task\timplementer\tbuild",
			"rev-1\tws:p2\tcodex\treviewer\topenai\t0\t/repo\trev-start\tgpt-5.4\task\treviewer\treview",
		}
		if len(lines) != len(want) {
			t.Fatalf("lines=%q want %d lines", lines, len(want))
		}
		for i := range want {
			if lines[i] != want[i] {
				t.Fatalf("line %d: %q want %q", i, lines[i], want[i])
			}
		}
	})
	t.Run("an unreadable roster is an error", func(t *testing.T) {
		dir := rosterAtomicFixture(t)
		makeUnreadable(t, filepath.Join(dir, "agents.tsv"))
		_, err := StrictRosterLines(dir)
		if err == nil || !strings.Contains(err.Error(), "unreadable") {
			t.Fatalf("err=%v; an unreadable roster must be an error naming it", err)
		}
	})
	t.Run("an oversized roster is unreadable", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(strings.Repeat("x", 1<<20+1)), 0o666); err != nil {
			t.Fatal(err)
		}
		_, err := StrictRosterLines(dir)
		if err == nil || !strings.Contains(err.Error(), "unreadable") {
			t.Fatalf("err=%v; an oversized roster must be unreadable", err)
		}
	})
	t.Run("a non-regular roster is unreadable", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "agents.tsv"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := StrictRosterLines(dir)
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("err=%v; a non-regular roster must be unreadable", err)
		}
	})
}
