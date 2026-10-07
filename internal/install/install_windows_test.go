//go:build windows

// Windows-specific proof for the replacement path. os.Rename on Windows is
// MoveFileEx with MOVEFILE_REPLACE_EXISTING (internal/syscall/windows.
// Rename), so it replaces a normal existing destination; these tests prove
// that replacement succeeds with static payloads, and that a destination
// held open by a running process makes the rename fail cleanly — the core
// surfaces the error, keeps the old bytes intact, and removes its temp
// files. No Go toolchain is needed at test time: the fixture is the
// re-executed native test binary itself (its own bytes are the installed
// artifact), not a program built by the test. The generic tests in
// install_test.go also run on Windows for the shared contracts.
package install

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

// testBinaryBytes reads the running test binary. On Windows it is a native
// PE executable, so its bytes can be installed as the artifact and then
// launched to hold its file open — without building anything.
func testBinaryBytes(t *testing.T) []byte {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestMain doubles as the re-executed helper. Launched with -install.hold
// as the installed destination, the process blocks until killed, keeping
// its executable image file open share-read only; that is what makes
// MoveFileEx replacement of the same path fail on Windows.
func TestMain(m *testing.M) {
	for _, arg := range os.Args {
		if arg == "-install.hold" {
			for {
				time.Sleep(time.Hour) // keep a timer live, avoiding a Go deadlock exit
			}
		}
	}
	os.Exit(m.Run())
}

// Replacement of an existing destination through os.Rename
// (MoveFileEx REPLACE_EXISTING), with static payloads and no toolchain.
func TestInstallWindowsReplacesExistingDestination(t *testing.T) {
	first := testBinaryBytes(t)
	got := runCore(t, fixtureOptions{payload: first})
	if got.err != nil {
		t.Fatalf("first install: %v", got.err)
	}
	second := []byte("static replacement payload v2\n")
	again := runCore(t, fixtureOptions{payload: second, installDir: got.installDir})
	if again.err != nil {
		t.Fatalf("replace install: %v", again.err)
	}
	installed, err := os.ReadFile(again.res.Destination)
	if err != nil || !bytes.Equal(installed, second) {
		t.Fatalf("destination not replaced: len=%d err=%v", len(installed), err)
	}
	assertNoTempFiles(t, got.installDir)
}

// A destination whose bytes are a running executable is locked by the
// loader (share-read only). The replacement must fail cleanly: the error
// surfaces, the old bytes are intact, and the temp files are removed.
func TestInstallWindowsLockedDestinationFailsWithoutDamage(t *testing.T) {
	exe := testBinaryBytes(t)
	got := runCore(t, fixtureOptions{payload: exe})
	if got.err != nil {
		t.Fatalf("first install: %v", got.err)
	}
	destination := got.res.Destination
	// The installed file is a copy of the test binary; re-executing it in
	// hold mode locks the destination the same way a running install does.
	cmd := exec.Command(destination, "-install.hold")
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start installed executable: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	again := runCore(t, fixtureOptions{payload: testBinaryBytes(t), installDir: got.installDir})
	if again.err == nil {
		t.Fatal("replacing a locked destination succeeded; os.Rename (MoveFileEx) must fail while the file is held open, so the error must surface")
	}
	t.Logf("replacement error (expected sharing violation): %v", again.err)
	installed, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(installed, exe) {
		t.Fatalf("locked destination changed: len=%d err=%v", len(installed), err)
	}
	assertNoTempFiles(t, got.installDir)
}
