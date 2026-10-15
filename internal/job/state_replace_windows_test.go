package job

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const fileReadAttributesAccess = 0x80

// holdOpen opens path with the given access and share mode and keeps the
// handle open until the test ends (also when an assertion fails).
func holdOpen(t *testing.T, path string, access, share uint32) {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(name, access, share, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(h) })
}

func noTempLeft(t *testing.T, dir string) {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(dir, ".state.json-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

func TestWriteAtomicReplacesUnderShareDeleteReader(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "state.json")
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The readReplacedFile open: read access, all three share flags.
	holdOpen(t, dest, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE)
	if err := writeAtomic0600(dest, []byte("new")); err != nil {
		t.Fatalf("writeAtomic0600 under a FILE_SHARE_DELETE reader: %v", err)
	}
	got, err := readReplacedFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
	noTempLeft(t, dir)
}

func TestWriteAtomicReplacesUnderAttributeReader(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "state.json")
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An attribute-only handle, the kind an os.Stat leaves open for a moment.
	holdOpen(t, dest, fileReadAttributesAccess,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE)
	if err := writeAtomic0600(dest, []byte("new")); err != nil {
		t.Fatalf("writeAtomic0600 under an attribute-only reader: %v", err)
	}
	got, err := readReplacedFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
	noTempLeft(t, dir)
}

func TestWriteAtomicFailsClosedUnderExclusiveReader(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "state.json")
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The os.ReadFile share mode: no FILE_SHARE_DELETE.
	name, err := syscall.UTF16PtrFromString(dest)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	closeReader := func() {
		if !closed {
			closed = true
			_ = syscall.CloseHandle(h)
		}
	}
	t.Cleanup(closeReader)

	werr := writeAtomic0600(dest, []byte("new"))
	if werr == nil {
		t.Fatal("writeAtomic0600 succeeded under a reader without FILE_SHARE_DELETE, want an error")
	}
	text := werr.Error()
	if !strings.HasPrefix(text, "rename ") || !strings.Contains(text, ".state.json-") || !strings.Contains(text, "state.json") {
		t.Fatalf("error = %q, want the rename <from> <to>: message shape naming the temp .state.json-* and state.json", text)
	}
	closeReader()
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("content after the failed replace = %q, want %q", got, "old")
	}
	noTempLeft(t, dir)
}
