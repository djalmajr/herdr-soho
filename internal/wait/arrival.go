package wait

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const PromptMarker = "Read the file "

var promptPathMarker = regexp.MustCompile(`^\S+\s+\S+\s+(.+)$`)
var markerPathShape = regexp.MustCompile(`^\s*\S+\s+\S+\s+.+\s*$`)

func LastNonEmptyLines(screen string, n int) []string {
	screen = strings.ReplaceAll(screen, "\r\n", "\n")
	lines := []string{}
	for _, line := range strings.Split(screen, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if n < len(lines) {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func PromptSitsInInput(screen string) bool {
	for _, line := range LastNonEmptyLines(screen, 15) {
		if strings.Contains(line, PromptMarker) {
			return true
		}
	}
	return false
}

func MarkerSeq(marker string) string {
	parts := strings.Fields(strings.TrimSpace(marker))
	if len(parts) < 2 || parts[1] == "-" {
		return ""
	}
	return parts[1]
}

func MarkerSeqChanged(marker string, current any) bool {
	seq := MarkerSeq(marker)
	cur := jsString(current)
	return seq != "" && cur != "" && seq != cur
}

func QueuedPromptPath(marker, sd, agent string) string {
	if parts := promptPathMarker.FindStringSubmatch(strings.TrimSpace(marker)); parts != nil {
		return parts[1]
	}
	report, _ := os.ReadFile(filepath.Join(sd, "last-report-"+agent))
	name := strings.TrimSpace(string(report))
	if name == "" {
		return ""
	}
	base := filepath.Base(name)
	statePrompt := filepath.Join(sd, "briefs", base)
	if _, err := os.Stat(statePrompt); err == nil {
		return statePrompt
	}
	stem := strings.TrimSuffix(base, ".md")
	tmpPrompt := filepath.Join(filepath.Dir(name), stem+".brief.md")
	if _, err := os.Stat(tmpPrompt); err == nil {
		return tmpPrompt
	}
	return statePrompt
}

func MarkerHasPromptPath(marker string) bool { return markerPathShape.MatchString(marker) }

func QueuedPromptSitsInInput(marker, screen, sd, agent string) bool {
	promptPath := QueuedPromptPath(marker, sd, agent)
	if promptPath == "" {
		return false
	}
	for _, line := range LastNonEmptyLines(screen, 3) {
		if strings.Contains(line, promptPath) {
			return true
		}
	}
	return false
}

func jsString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return toString(value)
}

func toString(value any) string {
	switch v := value.(type) {
	case bool:
		if !v {
			return ""
		}
		return "true"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}
