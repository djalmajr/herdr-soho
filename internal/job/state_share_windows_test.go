package job

import (
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// accessDelete is the access an in-flight atomic replace holds on the file
// it renames over: writeAtomic0600's rename takes DELETE on the old name.
// A reader whose share mode does not admit FILE_SHARE_DELETE fails its
// open with a sharing violation while that handle is held; a
// sharing-friendly reader (share FILE_SHARE_READ|FILE_SHARE_WRITE|
// FILE_SHARE_DELETE) still opens it. The holder helpers below use it to
// make the in-flight window deterministic: no rename races, the handle
// alone holds the window open.
const accessDelete = 0x00010000

// holdInFlightReplace opens path with the in-flight replace's DELETE
// access and the fully sharing mode, and registers a t.Cleanup that
// closes the handle. It blocks exactly the opens a pre-fix reader makes
// (share without FILE_SHARE_DELETE) and admits the sharing-friendly one.
func holdInFlightReplace(t *testing.T, path string) {
	t.Helper()
	holdHandle(t, path, accessDelete,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE)
}

// holdExclusive opens path as an exclusive holder: GENERIC_READ access and
// share mode 0, which a sharing-friendly reader must still not get past.
func holdExclusive(t *testing.T, path string) {
	t.Helper()
	holdHandle(t, path, syscall.GENERIC_READ, 0)
}

// holdHandle opens path with the given access and share mode (the exact
// syscall.CreateFile call the brief names: OPEN_EXISTING,
// FILE_ATTRIBUTE_NORMAL) and registers a t.Cleanup that closes the handle.
func holdHandle(t *testing.T, path string, access, share uint32) {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(name, access, share,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("open %s (access 0x%x, share 0x%x): %v", path, access, share, err)
	}
	t.Cleanup(func() {
		if err := syscall.CloseHandle(h); err != nil {
			t.Fatalf("close the holder on %s: %v", path, err)
		}
	})
}

// TestLockFreeReadsShareAnInFlightReplace proves the lock-free reads
// (wait, snapshot, events) keep working while state.json is being
// atomically replaced: the holder reproduces the in-flight rename's
// DELETE handle on the file.
func TestLockFreeReadsShareAnInFlightReplace(t *testing.T) {
	s, root := startedJob(t)
	dir := filepath.Join(root, "jobs", "job-1")
	holdInFlightReplace(t, filepath.Join(dir, "state.json"))

	// The job is not terminal, so wait with a zero deadline must come back
	// with the timeout's ExitError, not a state-read failure.
	_, err := s.Wait("job-1", 0)
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("Wait returned %v, want the zero-deadline ExitError", err)
	}
	if exit.Code != ExitTimeout {
		t.Fatalf("Wait returned ExitError %d %q, want %d (the job is not terminal)", exit.Code, exit.Msg, ExitTimeout)
	}

	snap, err := s.Snapshot("job-1")
	if err != nil {
		t.Fatalf("Snapshot returned %v, want nil", err)
	}
	if snap.State.Status != StatusAccepted {
		t.Fatalf("Snapshot status %q, want %q", snap.State.Status, StatusAccepted)
	}

	if _, err := s.Events("job-1", 0, 0); err != nil {
		t.Fatalf("Events returned %v, want nil", err)
	}
}

// TestSnapshotSharesAnInFlightReportReplace proves the same for
// report.json: the snapshot reads the report while it is being
// atomically replaced.
func TestSnapshotSharesAnInFlightReportReplace(t *testing.T) {
	s, root := startedJob(t)
	dir := filepath.Join(root, "jobs", "job-1")
	report := filepath.Join(dir, "report.json")
	if err := writeAtomic0600(report, []byte("{}\n")); err != nil {
		t.Fatalf("write report.json: %v", err)
	}
	holdInFlightReplace(t, report)

	snap, err := s.Snapshot("job-1")
	if err != nil {
		t.Fatalf("Snapshot returned %v, want nil", err)
	}
	if snap.Report == nil {
		t.Fatal("Snapshot report is nil, want the report.json being replaced")
	}
}

// TestStateReadKeepsAGenuineSharingError proves an exclusive holder of
// state.json (GENERIC_READ access, share mode 0) keeps blocking the
// lock-free reads with the genuine sharing violation: the plain error is
// what the CLI reports as exit 2, and nothing is retried or swallowed.
// It passes before the sharing-friendly read and must keep passing
// after it.
func TestStateReadKeepsAGenuineSharingError(t *testing.T) {
	s, root := startedJob(t)
	dir := filepath.Join(root, "jobs", "job-1")
	holdExclusive(t, filepath.Join(dir, "state.json"))

	_, err := s.Wait("job-1", 0)
	if err == nil {
		t.Fatal("Wait returned nil, want the sharing violation")
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		t.Fatalf("Wait returned ExitError %d %q, want the plain sharing violation", exit.Code, exit.Msg)
	}
	for _, want := range []string{"job: state unreadable:", "being used by another process"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Wait error %q does not contain %q", err.Error(), want)
		}
	}
}
