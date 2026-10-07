package plugin

import (
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestTeamShowsCycleMetadataAndRefusesReleaseControl(t *testing.T) {
	s := &TeamState{view: teamViewTeam, workers: []string{"author implementer idle ready"}, collaborations: teamCollaborationRows([]string{`{"agent":"author","status":"collaborating","assignment":"c-0000000000000000","phase":"reviewing","round":2,"revision":"abcdef","delivery":"pending"}`})}
	s.viewLoaded[teamViewTeam-1] = true
	lines, _ := s.renderContent(100)
	text := strings.Join(lines, "\n")
	for _, want := range []string{"c-0000000000000000", "reviewing", "rodada 2", "abcdef", "pending"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	s.ApplyKey("x")
	if s.subview != "" || s.pendingRelease != "" || !strings.Contains(s.releaseFailure, "orquestrador") {
		t.Fatal("active participant reached close confirmation")
	}
}

func TestTeamKeepsPartialStatusCausesAndNoExternalReleaseTargets(t *testing.T) {
	code := 4
	lines, cause := teamCallResult([]string{"status"}, platform.RunResult{Status: &code, Stdout: `{"agent":"author","status":"collaboration-disabled","cause":"worker_messages=off"}` + "\n"})
	rows := teamCollaborationRows(lines)
	if cause == "" || rows["author"].Cause != "worker_messages=off" {
		t.Fatalf("partial status lost its cause: %v %s", rows, cause)
	}
	workers := teamWorkers([]string{"NAME ROLE STATE", "author implementer idle", "", "# other live agents in workspace ws", "external codex working"})
	if len(workers) != 1 || strings.Contains(workers[0], "external") {
		t.Fatal("external pane became a release target")
	}
}

func TestTeamPartialStatusProtectsOnlyTheAssignedParticipant(t *testing.T) {
	for _, code := range []int{4, 7, 11, 14, 15} {
		lines, cause := teamCallResult([]string{"status"}, platform.RunResult{Status: &code, Stdout: `{"agent":"author","status":"collaborating","assignment":"c-0000000000000000","phase":"reviewing"}` + "\nscout\tdone\treport.md\n"})
		s := &TeamState{view: teamViewTeam, workers: []string{"author implementer working", "scout scouter done"}, collaborations: teamCollaborationRows(lines), statusFailure: cause, selected: 1}
		s.ApplyKey("x")
		if s.subview != "confirm-release" || s.confirmAgent != "scout" {
			t.Fatalf("status exit %d blocked an unrelated worker: %+v", code, s)
		}
		s.subview, s.selected = "", 0
		s.ApplyKey("x")
		if s.subview != "" || s.releaseFailure == "" {
			t.Fatalf("status exit %d lost the participant guard", code)
		}
	}
}
