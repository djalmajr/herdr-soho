package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestA13bRosterMessageNamesStateDir(t *testing.T) {
	f := newReleaseFixture(t, "", "")
	state := filepath.Join(f.env.Get("HERDR_SOHO_DIR"), "ws")

	// release: the refusal names the state dir it searched.
	r := f.run(t, "nobody")
	wantRelease := "agent 'nobody' is not in the roster (state dir: " + state + ")"
	if r.code != 3 || !strings.Contains(r.stderr, wantRelease) {
		t.Fatalf("release status=%d stderr=%q, want 3 containing %q", r.code, r.stderr, wantRelease)
	}

	// wait: the same roster refusal, same state dir.
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(f.cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	code := Run([]string{"wait", "missing"}, f.env)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	wantWait := "agent 'missing' is not in the roster (state dir: " + state + ")"
	if code != 3 || !strings.Contains(stderr.String(), wantWait) {
		t.Fatalf("wait status=%d stdout=%q stderr=%q, want 3 containing %q", code, out.String(), stderr.String(), wantWait)
	}
}
