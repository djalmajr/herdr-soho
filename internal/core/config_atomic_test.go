package core

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestConfigAtomicWriteFailure(t *testing.T) {
	t.Run("config writer defaults to platform.AtomicWrite", func(t *testing.T) {
		if reflect.ValueOf(writeConfigAtomic).Pointer() != reflect.ValueOf(platform.AtomicWrite).Pointer() {
			t.Fatal("writeConfigAtomic no longer defaults to platform.AtomicWrite")
		}
	})
	t.Run(`JS: "a failed rename leaves the config and the session file untouched"`, func(t *testing.T) {
		// Mutation captured: swallowing the atomic-write error returns success instead of exit 4.
		root := t.TempDir()
		env := platform.Env{"HERDR_SOHO_DIR": filepath.Join(root, ".herdr-soho")}
		project := filepath.Join(root, ".agents", "herdr-soho.conf")
		if err := os.MkdirAll(filepath.Dir(project), 0o700); err != nil {
			t.Fatal(err)
		}
		const original = "# keep\nmax_workers=2\n"
		if err := os.WriteFile(project, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		realWrite := writeConfigAtomic
		writeConfigAtomic = func(string, string) error { return errors.New("injected EIO") }
		t.Cleanup(func() { writeConfigAtomic = realWrite })
		oldStdout := platform.Stdout
		var stdout bytes.Buffer
		platform.Stdout = &stdout
		t.Cleanup(func() { platform.Stdout = oldStdout })
		assertExit := func(label, wantMessage string, run func()) {
			t.Helper()
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				run()
			}()
			exit, ok := recovered.(*platform.ExitError)
			if !ok || exit.Code != 4 || exit.Msg != wantMessage {
				t.Fatalf("%s error=%#v, want exit code 4 and message %q", label, recovered, wantMessage)
			}
			got, err := os.ReadFile(project)
			if err != nil || string(got) != original {
				t.Fatalf("%s config=%q err=%v", label, got, err)
			}
		}
		assertExit("config set", "config set: could not rewrite "+project+" (file left untouched)", func() { CmdConfigSet([]string{"max_workers", "5"}, nil, env, root) })
		assertExit("session clear", "session clear: could not rewrite "+project+" (file left untouched)", func() { ConfigClearKey(project, "max_workers") })
		if stdout.Len() != 0 {
			t.Fatalf("stdout after failed writes=%q", stdout.String())
		}
		entries, err := os.ReadDir(filepath.Dir(project))
		if err != nil || len(entries) != 1 || entries[0].Name() != "herdr-soho.conf" {
			t.Fatalf("config directory after failed writes=%v err=%v", entries, err)
		}
	})
}
