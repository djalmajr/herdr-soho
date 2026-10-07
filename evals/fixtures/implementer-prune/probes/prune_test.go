// Package probes holds the hidden implementer probes for the
// implementer-prune fixture. Each top-level test below corresponds to one
// named legacy probe in prune.test.mjs (old-name to Go-name map preserved in
// the E2a evidence); the file set and counts of retained backups match the
// legacy assertions exactly.
package probes

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"implementer-prune/src"
)

// now mirrors the legacy probe wall clock new Date('2026-09-27T21:19:50.000Z').
var now = time.Date(2026, 9, 27, 21, 19, 50, 0, time.UTC)

// backup creates a backup file with the legacy naming and opaque content.
func backup(t *testing.T, dir string, sequence int, timestamp string) string {
	t.Helper()
	name := fmt.Sprintf("backup-%016d-%s.json", sequence, timestamp)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(fmt.Sprintf("opaque %d", sequence)), 0o644); err != nil {
		t.Fatalf("write backup %s: %v", name, err)
	}
	return name
}

// names lists the directory entries sorted, like the legacy names(dir).
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out
}

// assertNames compares the directory entries against the sorted expected set.
func assertNames(t *testing.T, dir string, want []string) {
	t.Helper()
	got := names(t, dir)
	sorted := append([]string{}, want...)
	sort.Strings(sorted)
	if len(got) != len(sorted) {
		t.Fatalf("entries %v, want %v", got, sorted)
	}
	for i := range got {
		if got[i] != sorted[i] {
			t.Fatalf("entries %v, want %v", got, sorted)
		}
	}
}

// Legacy probe: "keeps highest sequences, not latest timestamp or mtime".
func TestKeepsHighestSequencesNotLatestTimestampOrMtime(t *testing.T) {
	// Mutation captured: sorting by timestamp or mtime deletes the wrong file.
	dir := t.TempDir()
	backup(t, dir, 1, "20261231T235959999Z")
	middle := backup(t, dir, 2, "20260101T000000000Z")
	newest := backup(t, dir, 3, "20260101T000001000Z")
	epoch := time.Unix(0, 0)
	if err := os.Chtimes(filepath.Join(dir, newest), epoch, epoch); err != nil {
		t.Fatalf("set mtime to epoch: %v", err)
	}
	removed, err := src.PruneBackups(src.Options{Dir: dir, Keep: 2, Now: now})
	if err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d, want 1", removed)
	}
	assertNames(t, dir, []string{middle, newest})
}

// Legacy probe: "a crash after deleting older files can be retried without
// losing the newest".
func TestCrashAfterDeletingOlderFilesRetriesWithoutLosingNewest(t *testing.T) {
	// Mutation captured: retry after a partially completed prune removes a
	// retained backup.
	dir := t.TempDir()
	retained := []string{
		backup(t, dir, 2, "20260101T000000000Z"),
		backup(t, dir, 3, "20260101T000000000Z"),
	}
	backup(t, dir, 1, "20260101T000000000Z")
	// A crash during pruning already removed the oldest backup.
	if err := os.Remove(filepath.Join(dir, "backup-0000000000000001-20260101T000000000Z.json")); err != nil {
		t.Fatalf("remove pre-deleted backup: %v", err)
	}
	removed, err := src.PruneBackups(src.Options{Dir: dir, Keep: 2, Now: now})
	if err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed %d, want 0", removed)
	}
	assertNames(t, dir, retained)
}

// Legacy probe: "published backup survives the publish-before-prune
// boundary and retry converges".
func TestPublishedBackupSurvivesPublishBeforePruneBoundaryAndRetryConverges(t *testing.T) {
	// Mutation captured: pruning to keep=1 removes the newly published
	// highest sequence.
	dir := t.TempDir()
	backup(t, dir, 1, "20260101T000000000Z")
	published := backup(t, dir, 2, "20260101T000000000Z")
	removed, err := src.PruneBackups(src.Options{Dir: dir, Keep: 1, Now: now})
	if err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d, want 1", removed)
	}
	assertNames(t, dir, []string{published})
	removed, err = src.PruneBackups(src.Options{Dir: dir, Keep: 1, Now: now})
	if err != nil {
		t.Fatalf("retry PruneBackups: %v", err)
	}
	if removed != 0 {
		t.Fatalf("retry removed %d, want 0", removed)
	}
	assertNames(t, dir, []string{published})
}

// Legacy probe: "clock rollback cannot change sequence ordering".
func TestClockRollbackCannotChangeSequenceOrdering(t *testing.T) {
	// Mutation captured: sorting by now-relative age prunes a needed high
	// sequence.
	dir := t.TempDir()
	backup(t, dir, 7, "20261231T235959999Z")
	latest := backup(t, dir, 8, "20200101T000000000Z")
	rolledBack := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	removed, err := src.PruneBackups(src.Options{Dir: dir, Keep: 1, Now: rolledBack})
	if err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d, want 1", removed)
	}
	assertNames(t, dir, []string{latest})
}

// Legacy probe: "ignores malformed names, temporary files, directories, and
// symlinks".
func TestIgnoresMalformedNamesTemporaryFilesDirectoriesAndSymlinks(t *testing.T) {
	// Mutation captured: broad matching or recursive deletion removes
	// unrelated entries.
	dir := t.TempDir()
	older := backup(t, dir, 1, "20260101T000000000Z")
	latest := backup(t, dir, 2, "20260101T000000000Z")
	malformed := "backup-final.json"
	temporary := "backup-0000000000000003-20260101T000000000Z.json.tmp"
	directory := "backup-0000000000000004-20260101T000000000Z.json"
	symlink := "backup-0000000000000005-20260101T000000000Z.json"
	if err := os.WriteFile(filepath.Join(dir, malformed), []byte("leave"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, temporary), []byte("leave"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, directory), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside")
	if err := os.WriteFile(outside, []byte("leave"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, symlink)); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	removed, err := src.PruneBackups(src.Options{Dir: dir, Keep: 1, Now: now})
	if err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d, want 1", removed)
	}
	if _, err := os.Lstat(filepath.Join(dir, older)); !os.IsNotExist(err) {
		t.Fatalf("oldest backup %s was not removed", older)
	}
	if _, err := os.Lstat(filepath.Join(dir, latest)); err != nil {
		t.Fatalf("retained backup %s missing: %v", latest, err)
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "leave" {
		t.Fatalf("outside content = %q (err %v), want leave", content, err)
	}
	for _, keep := range []string{malformed, temporary, directory} {
		if _, err := os.Lstat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("unrelated entry %s was removed: %v", keep, err)
		}
	}
	st, err := os.Lstat(filepath.Join(dir, symlink))
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink %s missing or not a symlink: %v", symlink, err)
	}
}

// Legacy probe: "invalid arguments and duplicate sequence fail before
// mutation".
func TestInvalidArgumentsAndDuplicateSequenceFailBeforeMutation(t *testing.T) {
	// Mutation captured: deleting before validation or guessing a duplicate
	// order loses data.
	dir := t.TempDir()
	first := backup(t, dir, 1, "20260101T000000000Z")
	duplicate := "backup-0000000000000001-20260102T000000000Z.json"
	if err := os.WriteFile(filepath.Join(dir, duplicate), []byte("duplicate"), 0o644); err != nil {
		t.Fatal(err)
	}
	zero := time.Time{} // the legacy invalid Date (NaN) maps to the zero time
	cases := []struct {
		name    string
		options src.Options
	}{
		{"keep zero", src.Options{Dir: dir, Keep: 0, Now: now}},
		{"keep fractional", src.Options{Dir: dir, Keep: 1.5, Now: now}},
		{"now zero", src.Options{Dir: dir, Keep: 1, Now: zero}},
		{"missing directory", src.Options{Dir: filepath.Join(dir, "missing"), Keep: 1, Now: now}},
	}
	for _, tc := range cases {
		if _, err := src.PruneBackups(tc.options); err == nil {
			t.Fatalf("%s: PruneBackups succeeded, want an error", tc.name)
		}
		assertNames(t, dir, []string{first, duplicate})
	}
	if _, err := src.PruneBackups(src.Options{Dir: dir, Keep: 1, Now: now}); err == nil {
		t.Fatal("duplicate sequence: PruneBackups succeeded, want an error before mutation")
	}
	assertNames(t, dir, []string{first, duplicate})
}
