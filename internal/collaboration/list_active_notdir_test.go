package collaboration

import (
	"os"
	"path/filepath"
	"testing"
)

// Catches the mutation "every not-found read of the directory is an empty
// store": a regular file in the directory's place (which some systems
// report as not found) would read as no collaborations instead of the
// directory-level failure, and the supervisor would release and close.
func TestListActiveRegularFileInPlaceOfTheDirectoryIsAnError(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "collaboration"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if active, unreadable, err := (Store{StateDir: ws}).ListActive(); err == nil {
		t.Fatalf("ListActive = %v, %v, nil; want a directory-level error", active, unreadable)
	}
	// Control: a truly absent directory is an empty store.
	if active, unreadable, err := (Store{StateDir: t.TempDir()}).ListActive(); err != nil || len(active) != 0 || len(unreadable) != 0 {
		t.Fatalf("absent directory: %v, %v, %v; want empty, nil", active, unreadable, err)
	}
}
