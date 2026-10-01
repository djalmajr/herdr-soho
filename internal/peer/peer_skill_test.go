package peer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	_ "unsafe"

	"github.com/djalmajr/herdr-soho/internal/peer"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

//go:linkname appendPeerLog github.com/djalmajr/herdr-soho/internal/peer.appendPeerLog
func appendPeerLog(stateDir, from, to, result string, chars int, id string, env platform.Env)

func TestSendRefusesTheSkillDir(t *testing.T) {
	// The state dir (root/state) lives inside the fixture root, which this
	// test points HERDR_SOHO_SKILL_DIR at: send must refuse before any
	// herdr call.
	root := t.TempDir()
	f := newFixtureAt(t, root, nil)
	f.env["HERDR_SOHO_SKILL_DIR"] = root
	code, _, stderr := f.run([]string{"send", "w0test:p0a", "hello"})
	if code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	want := "the state dir '" + filepath.Join(root, "state", "ws-test") + "' would be inside the herdr-soho skill ('" + root + "'); run herdr-soho from the project's directory (nothing was sent)"
	if !strings.Contains(stderr, want) {
		t.Fatalf("stderr=%q want %q", stderr, want)
	}
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(filepath.Dir(f.bin), "herdr.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return // no herdr call at all: the refusal came first
		}
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("herdr calls before the refusal: %#v", calls)
	}
}

func TestAppendPeerLogSkipsTheSkillDir(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "skill")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(skill, 0o700); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{"HERDR_SOHO_SKILL_DIR": skill, "HOME": root, "USERPROFILE": root}

	// Defense: a state dir inside the skill never gets the log written.
	inside := filepath.Join(skill, "ws")
	appendPeerLog(inside, "a", "b", "ok", 3, "id1", env)
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Fatalf("peer log written inside the skill: %v", err)
	}

	// Control: the same append outside the skill writes the file.
	outside := filepath.Join(state, "ws")
	appendPeerLog(outside, "a", "b", "ok", 3, "id2", env)
	data, err := os.ReadFile(filepath.Join(outside, peer.PeerLogFile))
	if err != nil {
		t.Fatalf("peer log outside the skill missing: %v", err)
	}
	if !strings.Contains(string(data), "\tok\t3\tid2\n") {
		t.Fatalf("peer log=%q", data)
	}
}
