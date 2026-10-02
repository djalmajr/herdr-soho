package peer_test

import (
	"testing"

	"github.com/djalmajr/herdr-soho/internal/peer"
)

// remotePeerHeader is the header send builds for a remote target in fixtures
// that run without HERDR_PANE_ID: the pane reads "-" and the hostname is
// pinned, so the expected prompt does not depend on the host running the test.
func remotePeerHeader(t *testing.T, id string) string {
	t.Helper()
	setSenderHostname(t, "Run2Biz.local")
	return peer.PeerHeaderRemote("-", "Run2Biz.local", "-", "-", "-", id)
}
