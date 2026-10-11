//go:build unix

package platform

import "testing"

// Catches the mutations "the BSD exiting flag is an unknown state" (the
// stop's re-read of a process caught mid-exit, which macOS prints as ?E,
// would be an unreadable identity and the release would refuse to claim
// the stop) and "the exiting flag is a gone state" (an exiting root would
// be dropped without stopping its tree or waiting for its disappearance):
// an exiting process is present, never gone.
func TestProcStateExitingIsPresentNotGone(t *testing.T) {
	for _, state := range []string{"?E", "SE", "RE+"} {
		if !procStateRunning(state) || procStateGone(state) {
			t.Errorf("state %q: running=%v gone=%v, want present (running, not gone)", state, procStateRunning(state), procStateGone(state))
		}
	}
	for _, state := range []string{"Z", "Z+", "Zs"} {
		if !procStateGone(state) {
			t.Errorf("procStateGone(%q) = false, want true", state)
		}
	}
	for _, state := range []string{"S", "Ss", "S+", "R", "R+", "SN", "S<", "Sl", "I", "U", "T", "D"} {
		if !procStateRunning(state) || procStateGone(state) {
			t.Errorf("state %q: running=%v gone=%v", state, procStateRunning(state), procStateGone(state))
		}
	}
	for _, state := range []string{"", "?", "?s"} {
		if procStateRunning(state) || procStateGone(state) {
			t.Errorf("state %q reads as running=%v gone=%v, want neither (kernel check)", state, procStateRunning(state), procStateGone(state))
		}
	}
	m := procStartRe.FindStringSubmatch("Sun Oct 11 00:34:13 2026 ?E   sh")
	if m == nil || m[2] != "?E" || m[3] != "sh" {
		t.Fatalf("procStartRe on an exiting line = %q, want the ?E state and the sh comm", m)
	}
}
