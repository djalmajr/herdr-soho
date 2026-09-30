package cli

import (
	"strings"
)

// commandHelp returns the general-usage lines that belong to command: the
// "  herdr-soho <command> …" line (or, when the command has no line of its
// own, the line that cites it together with another, as "roles | kinds") and
// the continuation lines indented deeper than it, up to the next
// "  herdr-soho " line. It is the text "<command> --help" prints.
func commandHelp(command string) string {
	lines := strings.Split(usage, "\n")
	start := -1
	for i, line := range lines {
		if !strings.HasPrefix(line, "  herdr-soho ") {
			continue
		}
		words := strings.Fields(strings.TrimPrefix(line, "  herdr-soho "))
		if len(words) > 0 && words[0] == command {
			start = i
			break
		}
	}
	if start == -1 {
		// The command has no line of its own (cited together with another).
		for i, line := range lines {
			if !strings.HasPrefix(line, "  herdr-soho ") {
				continue
			}
			for _, word := range strings.Fields(strings.TrimPrefix(line, "  herdr-soho ")) {
				if word == command {
					start = i
					break
				}
			}
			if start != -1 {
				break
			}
		}
	}
	if start == -1 {
		return usage
	}
	out := []string{lines[start]}
	for _, line := range lines[start+1:] {
		// Continuation lines are indented deeper than the command line; the
		// next "  herdr-soho " line or a non-indented line ends the block.
		if line == "" || strings.HasPrefix(line, "  herdr-soho ") || !strings.HasPrefix(line, "   ") {
			break
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n") + "\n"
}
