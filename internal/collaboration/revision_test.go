package collaboration

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRevisionBindsBytesPresenceAndStableSnapshot(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file.go")
	if err := os.WriteFile(file, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := []string{"file.go", "added.go"}
	old, err := Fingerprint(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "snapshot")
	if err := Snapshot(root, destination, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := Fingerprint(root, paths)
	if err != nil || current.Fingerprint == old.Fingerprint {
		t.Fatal("content change did not invalidate approval")
	}
	raw, err := os.ReadFile(filepath.Join(destination, "files", "file.go"))
	if err != nil || string(raw) != "old" {
		t.Fatal("snapshot followed mutable source")
	}
	if err := Snapshot(root, filepath.Join(t.TempDir(), "snapshot"), old); err == nil {
		t.Fatal("snapshot of stale revision accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "added.go"), []byte("added"), 0o600); err != nil {
		t.Fatal(err)
	}
	added, err := Fingerprint(root, paths)
	if err != nil || added.Fingerprint == current.Fingerprint {
		t.Fatal("new file did not invalidate revision")
	}
}

func TestSnapshotManifestDoesNotShadowDeclaredInputs(t *testing.T) {
	for _, present := range []bool{false, true} {
		root := t.TempDir()
		if present {
			if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte("project manifest"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		revision, err := Fingerprint(root, []string{"manifest.json"})
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(t.TempDir(), "snapshot")
		if err := Snapshot(root, destination, revision); err != nil {
			t.Fatal(err)
		}
		input, err := os.ReadFile(filepath.Join(destination, "files", "manifest.json"))
		if present && (err != nil || string(input) != "project manifest") {
			t.Fatal("metadata replaced project input")
		}
		if !present && !os.IsNotExist(err) {
			t.Fatal("metadata materialized a missing project input")
		}
		if _, err := os.ReadFile(filepath.Join(destination, "manifest.json")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRevisionRejectsEscapeDirectoryAndSymlink(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"../outside", ".git/config", "a/../../outside", "a\\b", ".", "src/*.go", "file?.go", "src/[ab].go", "C:relative.go"} {
		if _, err := Fingerprint(root, []string{path}); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Fingerprint(root, []string{"dir"}); err == nil {
		t.Fatal("directory scope accepted")
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		if _, err := Fingerprint(root, []string{"link/missing"}); err == nil {
			t.Fatal("symlink scope accepted")
		}
	}
}
