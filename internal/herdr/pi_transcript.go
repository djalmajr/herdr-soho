package herdr

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// PiSessionPath returns the path of the agent's pi session file (the
// agent_session value herdr reports for a pi agent, for example
// <home>/.pi/agent/sessions/--Users-x-proj--/<timestamp>_<uuid>.jsonl), or
// "" when any data is missing: the agent get call fails, the agent has no
// agent_session, the session kind is not "path", the value is empty or not
// an absolute path, or the file does not exist locally.
func PiSessionPath(agent string, env platform.Env) string {
	r := run([]string{"agent", "get", agent}, env)
	if !ok(r) {
		return ""
	}
	agentObj := get(parsed(r.Stdout), "result", "agent")
	session := get(agentObj, "agent_session")
	if str(get(session, "kind")) != "path" {
		return ""
	}
	value := str(get(session, "value"))
	if value == "" || !filepath.IsAbs(value) {
		return ""
	}
	if _, err := os.Stat(value); err != nil {
		return ""
	}
	return value
}

// piTranscriptMessageType and piTranscriptUserRole mark the session lines
// that hold a message taken from pi's input: the delivered peer message
// lands in a "type":"message" line whose message role is "user", and the
// assistant replies and the tool results only cite the marker.
const (
	piTranscriptMessageType = `"type":"message"`
	piTranscriptUserRole    = `"role":"user"`
)

var (
	piTranscriptMessageTypeBytes = []byte(piTranscriptMessageType)
	piTranscriptUserRoleBytes    = []byte(piTranscriptUserRole)
)

// CountPiUserMarkerLines counts the session lines that contain the user
// message record ("type":"message" with "role":"user") and the given peer
// marker (#id). Like CountClaudeUserMarkerLines it streams the file line by
// line and returns only the number: it does not read into, retain, log, or
// return the content of any line, matching or not. The text pi writes inside
// the record is JSON-escaped, so a marker with a backslash or a quote must
// be passed escaped the same way (see transcriptPathMarker in the cli
// package); a line that only holds the escaped form cannot fake the user
// record, since the record markers themselves are plain JSON. ok is false
// when the file cannot be read.
func CountPiUserMarkerLines(path, marker string) (int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	// Session lines hold whole messages and run far past the scanner's
	// 64 KiB default token, so the buffer may grow before a line counts.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	markerBytes := []byte(marker)
	n := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if bytes.Contains(line, piTranscriptMessageTypeBytes) && bytes.Contains(line, piTranscriptUserRoleBytes) && bytes.Contains(line, markerBytes) {
			n++
		}
	}
	if scanner.Err() != nil {
		return 0, false
	}
	return n, true
}
