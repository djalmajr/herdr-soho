package dispatch

import "strings"

const sandboxGitNote = "- Your sandbox cannot write under .git: do not run git mv, git checkout, git add or git commit. Describe renames and restores in the report; the orchestrator runs them.\n"
const sandboxNetNote = "- Your sandbox has no network, local ports included: tests that start a local server fail with \"Operation not permitted\". Mark them [partial] and say so; the orchestrator runs them. Still write the integration tests the brief asks for, even if you cannot run them here; do not replace them with unit tests of helpers, and add a test seam (an injectable value) when the code depends on something fixed, such as the build type.\n"

// SandboxNotes returns Codex sandbox constraints for the worker's opening args.
func SandboxNotes(kind, agentArgs string) []string {
	if kind != "codex" {
		return nil
	}
	args := strings.Fields(agentArgs)
	full, net := false, false
	for _, a := range args {
		if a == "danger-full-access" || a == "--dangerously-bypass-approvals-and-sandbox" {
			full = true
		}
		if strings.HasSuffix(a, "network_access=true") {
			net = true
		}
	}
	var notes []string
	if !full {
		notes = append(notes, sandboxGitNote)
		if !net {
			notes = append(notes, sandboxNetNote)
		}
	}
	return notes
}
