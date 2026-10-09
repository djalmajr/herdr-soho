package job

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type dirEntry struct {
	size  int64
	mtime int64
	mode  os.FileMode
}

// snapshotTree records the names, sizes, mtimes and modes of the job tree.
func snapshotTree(t *testing.T, root string) map[string]dirEntry {
	t.Helper()
	out := map[string]dirEntry{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		out[rel] = dirEntry{size: info.Size(), mtime: info.ModTime().UnixNano(), mode: info.Mode().Perm()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertTreeSame(t *testing.T, root string, before map[string]dirEntry) {
	t.Helper()
	after := snapshotTree(t, root)
	if len(before) != len(after) {
		t.Fatalf("job tree changed: before=%v after=%v", before, after)
	}
	for name, prev := range before {
		next := after[name]
		if next.size != prev.size || next.mtime != prev.mtime || next.mode != prev.mode {
			t.Fatalf("entry %s changed: before=%v after=%v", name, prev, next)
		}
	}
}

// TestReadsCreateNothing proves the read-only contract: the job is valid and
// its lock file is absent, so a read that opens the lock with O_CREATE or
// creates anything leaves the tree changed.
func TestReadsCreateNothing(t *testing.T) {
	s, root := startedJob(t)
	dir := filepath.Join(root, "jobs", "job-1")
	if err := os.Remove(filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, dir)
	page, err := s.Events("job-1", 0, 0)
	if err != nil || len(page.Events) != 1 || page.Events[0].Seq != 1 || page.Estado != StatusAccepted {
		t.Fatalf("events: %+v err=%v", page, err)
	}
	_, err = s.Wait("job-1", 200*time.Millisecond)
	if code := exitCode(t, err); code != ExitTimeout {
		t.Fatalf("non-terminal wait: code %d err %v", code, err)
	}
	assertTreeSame(t, dir, before)
	if _, err := os.Stat(filepath.Join(dir, "lock")); !os.IsNotExist(err) {
		t.Fatalf("lock file was created by a read: %v", err)
	}
	if err := writeStatus(root, "job-1", StatusDone); err != nil {
		t.Fatal(err)
	}
	beforeTerminal := snapshotTree(t, dir)
	st, err := s.Wait("job-1", time.Second)
	if err != nil || st.Status != StatusDone {
		t.Fatalf("terminal wait: %+v err=%v", st, err)
	}
	assertTreeSame(t, dir, beforeTerminal)
}

// TestReadsWorkOnReadOnlyJob proves the read-only contract from the
// permission side: a read-only job directory and read-only job files still
// answer the read APIs, which is impossible if a read opens the lock for
// writing or needs write access.
func TestReadsWorkOnReadOnlyJob(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	s, root := startedJob(t)
	if err := writeStatus(root, "job-1", StatusDone); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "jobs", "job-1")
	files := []string{"state.json", "events.jsonl", "brief.json", "brief.md", "lock"}
	for _, name := range files {
		if err := os.Chmod(filepath.Join(dir, name), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(dir, "control"), 0o500); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, name := range files {
			_ = os.Chmod(filepath.Join(dir, name), 0o600)
		}
		_ = os.Chmod(filepath.Join(dir, "control"), 0o700)
		_ = os.Chmod(dir, 0o700)
	})
	page, err := s.Events("job-1", 0, 0)
	if err != nil {
		t.Fatalf("events on a read-only job: %v", err)
	}
	if len(page.Events) != 1 || page.Estado != StatusDone {
		t.Fatalf("events: %+v", page)
	}
	st, err := s.Wait("job-1", 100*time.Millisecond)
	if err != nil || st.Status != StatusDone {
		t.Fatalf("wait on a read-only job: %+v err=%v", st, err)
	}
}

// TestWaitAndEventsBoundWhileLockHeld proves the bounded-wait contract: a
// writer that holds withLock for longer than the wait must not delay
// Events(..., wait) or Wait(..., timeout) past their bound. The holder keeps
// the writer lock for the whole test body, so a read that enters it would
// hang until the 2 s select fires.
func TestWaitAndEventsBoundWhileLockHeld(t *testing.T) {
	s, root := startedJob(t)
	last := mustPage(t, s, 0, 0).UltimoSeq
	dir := filepath.Join(root, "jobs", "job-1")
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseNow := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseNow()
	held := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := withLock(dir, func() error {
			close(held)
			<-release
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-held:
	case <-time.After(2 * time.Second):
		t.Fatal("the lock holder never acquired the writer lock")
	}
	start := time.Now()
	pageCh := make(chan EventsPage, 1)
	errCh := make(chan error, 1)
	go func() {
		page, err := s.Events("job-1", last, 200*time.Millisecond)
		pageCh <- page
		errCh <- err
	}()
	var page EventsPage
	select {
	case page = <-pageCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Events returned later than the 2 s bound while the writer lock was held")
	}
	eventsElapsed := time.Since(start)
	if err := <-errCh; err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(page.Events) != 0 {
		t.Fatalf("events page = %+v", page)
	}
	if eventsElapsed < 150*time.Millisecond || eventsElapsed > 2*time.Second {
		t.Fatalf("events took %v, want within [150 ms, 2 s)", eventsElapsed)
	}
	start = time.Now()
	stCh := make(chan State, 1)
	waitErrCh := make(chan error, 1)
	go func() {
		st, err := s.Wait("job-1", 200*time.Millisecond)
		stCh <- st
		waitErrCh <- err
	}()
	var st State
	select {
	case st = <-stCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait returned later than the 2 s bound while the writer lock was held")
	}
	waitElapsed := time.Since(start)
	if code := exitCode(t, <-waitErrCh); code != ExitTimeout {
		t.Fatalf("wait code %d", code)
	}
	if st.Status != StatusAccepted {
		t.Fatalf("wait state = %+v", st)
	}
	if waitElapsed < 150*time.Millisecond || waitElapsed > 2*time.Second {
		t.Fatalf("wait took %v, want within [150 ms, 2 s)", waitElapsed)
	}
	releaseNow()
	<-done
}

// TestEventsIgnoresTrailingPartialLine proves that a torn final line
// (no trailing newline) is not a complete line: reads ignore it, writers
// refuse to append onto it, and the file is never rewritten.
func TestEventsIgnoresTrailingPartialLine(t *testing.T) {
	s, root := startedJob(t)
	dir := filepath.Join(root, "jobs", "job-1")
	path := filepath.Join(dir, "events.jsonl")
	partial := []byte(`{"seq":2,"ts":"2026-01-01T12:00:00-03:00","tipo":"note","resumo":"tear`)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(partial); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	torn, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Events("job-1", 0, 0)
	if err != nil {
		t.Fatalf("events with a trailing partial line: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].Seq != 1 || page.UltimoSeq != 1 {
		t.Fatalf("events = %+v, want only the complete lines", page)
	}
	_, err = s.Note("job-1", EventIn{Tipo: "note", Resumo: "after tear"})
	if err == nil {
		t.Fatal("append onto the torn tail succeeded")
	}
	if err.Error() != "job: events.jsonl ends with an incomplete line; refusing to append" {
		t.Fatalf("refusal = %v, want the torn-tail refusal", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, torn) {
		t.Fatalf("the log was rewritten:\nbefore %s\nafter %s", torn, after)
	}
	page, err = s.Events("job-1", 0, 0)
	if err != nil {
		t.Fatalf("events after the refused append: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].Seq != 1 || page.UltimoSeq != 1 {
		t.Fatalf("events = %+v, want only the complete lines", page)
	}
}

// TestEventsRejectCorruptCompleteLine proves that a corrupt complete line
// (invalid JSON ending in a newline) is an error, that no exit code is
// invented for it, and that the file is neither truncated nor rewritten.
func TestEventsRejectCorruptCompleteLine(t *testing.T) {
	s, root := startedJob(t)
	path := filepath.Join(root, "jobs", "job-1", "events.jsonl")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write([]byte("not a json line\n")); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = s.Events("job-1", 0, 0)
	if err == nil {
		t.Fatal("a corrupt complete line was accepted")
	}
	var exit *ExitError
	if errors.As(err, &exit) && exit != nil {
		t.Fatalf("a corrupt line returned exit code %d, want a plain error", exit.Code)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(append([]byte{}, before...), []byte("not a json line\n")...), after) {
		t.Fatalf("the log changed:\nbefore %s\nafter %s", before, after)
	}
}
