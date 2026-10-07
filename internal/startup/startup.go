// Package startup is the shared cursor-on-Windows startup assessment
// (issue #59). Herdr reports a cursor agent ready (agent_status idle,
// interactive_ready true) while its TUI has not drawn: the verified case is
// the Workspace Trust Required dialog on the visible screen, and the
// historical case from the issue is a screen holding only the shell launch
// line. The spawn, dispatch and wait consumers all take their verdict from
// this one assessment; it never grants trust, never answers a dialog and
// never changes a public status or exit code — the consumers map a
// refusing verdict onto their existing blocked paths.
package startup

import (
	"regexp"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/text"
)

// CurrentPlatform is the platform the gate is evaluated against. It is a
// variable (not a direct call) so portable tests can simulate Windows;
// production always uses platform.Current, which is "win32" on Windows.
// No production environment override exists.
var CurrentPlatform = platform.Current

// The gate's fixed inputs: the assessment applies only to the cursor kind
// on the Windows platform string.
const (
	KindCursor     = "cursor"
	PlatformWin32  = "win32"
	SourceVisible  = "visible"
	FieldState     = "state"
	FieldReason    = "reason"
	FieldSource    = "source"
	EvidencePrefix = "cursor-startup-"
)

// Verdicts and reasons of the assessment. The reasons are the public
// startup_evidence values; they never carry raw screen content or native
// command arguments.
const (
	VerdictObserved   = "observed"
	VerdictBlocked    = "blocked"
	VerdictUnverified = "unverified"

	ReasonWorkspaceTrust    = "workspace-trust"
	ReasonEmptyScreen       = "empty-screen"
	ReasonScreenUnavailable = "screen-unavailable"
	ReasonScreenIncomplete  = "screen-incomplete"
	ReasonLaunchOnly        = "launch-only"
	ReasonVisibleOutput     = "visible-agent-output"
	ReasonStateUnavailable  = "agent-state-unavailable"
)

// Evaluation is the assessment's result: the verdict (observed, blocked or
// unverified), the reason it stands on and the source the screen came from
// (the visible screen read; always "visible" today).
type Evaluation struct {
	Verdict string
	Reason  string
	Source  string
}

// Refuses reports whether the assessment refuses the start or the send:
// a blocked (verified trust dialog) or unverified (blank, unreadable,
// launch-only, state unreadable) screen. Consumers map a refusal onto
// their existing blocked path; an observed verdict proceeds unchanged.
func (e Evaluation) Refuses() bool {
	return e.Verdict == VerdictBlocked || e.Verdict == VerdictUnverified
}

// EnabledFor reports whether the assessment applies: the cursor kind on
// the Windows platform. Every other kind or platform keeps its existing
// public behavior and JSON unchanged.
func EnabledFor(kind, goos string) bool {
	return kind == KindCursor && goos == PlatformWin32
}

// Enabled is EnabledFor against the current platform (the production
// gate; tests set CurrentPlatform to simulate Windows).
func Enabled(kind string) bool {
	return EnabledFor(kind, CurrentPlatform())
}

// Dialog lines are normalized without terminal box borders. A quoted
// transcript is not an active question, and only navigation hints may
// follow the Quit choice: later shell or composer output supersedes it.
func dialogLine(line string) string {
	line = strings.TrimSpace(line)
	if strings.Trim(line, " ┌┐└┘╭╮╯╰│┃║─━═┬┴├┤┼+") == "" {
		return ""
	}
	line = strings.Trim(line, "│┃║ ")
	return strings.Join(strings.Fields(text.ASCIILower(line)), " ")
}

var workspacePathLine = regexp.MustCompile(`(?i)^(?:[a-z]:[\\/]|\\\\)`)

var trustChoiceLine = regexp.MustCompile(`^(?:▶\s*)?(?:\[a\]\s*)?trust this workspace$`)
var quitChoiceLine = regexp.MustCompile(`^(?:▶\s*)?(?:\[q\]\s*)?quit$`)

// ActiveTrustDialog recognizes the current question and ordered choices,
// including wrapped questions in narrow terminal boxes. The last content
// must still belong to that dialog, rather than a quoted old transcript.
func ActiveTrustDialog(screen string) bool {
	lines := []string{}
	for _, line := range strings.Split(screen, "\n") {
		if norm := dialogLine(line); norm != "" {
			lines = append(lines, norm)
		}
	}
	for trust := 0; trust < len(lines); trust++ {
		if !trustChoiceLine.MatchString(lines[trust]) {
			continue
		}
		question := false
		for first := 0; first < trust && !question; first++ {
			for end := first + 1; end <= trust; end++ {
				if strings.TrimSuffix(strings.Join(lines[first:end], " "), "?") != "do you trust the contents of this directory" {
					continue
				}
				// Cursor shows the workspace path between the question and
				// choices; that path may itself wrap in a narrow terminal.
				if end == trust || workspacePathLine.MatchString(lines[end]) {
					question = true
					break
				}
			}
		}
		if !question || trust+1 >= len(lines) || !quitChoiceLine.MatchString(lines[trust+1]) {
			continue
		}
		tail := strings.TrimSuffix(strings.Join(lines[trust+2:], " "), ".")
		if tail == "" || tail == "use arrow keys to navigate, enter to select, or press the key shown" {
			return true
		}
	}
	return false
}

// PartialTrustDialog recognizes the visible choices and navigation hint
// when a short viewport clips the question. It is inconclusive startup
// evidence, never a verified question or permission to send input.
func PartialTrustDialog(screen string) bool {
	lines := []string{}
	for _, line := range strings.Split(screen, "\n") {
		if norm := dialogLine(line); norm != "" {
			lines = append(lines, norm)
		}
	}
	for i := 0; i+2 < len(lines); i++ {
		if !trustChoiceLine.MatchString(lines[i]) || !quitChoiceLine.MatchString(lines[i+1]) {
			continue
		}
		tail := strings.TrimSuffix(strings.Join(lines[i+2:], " "), ".")
		if tail == "use arrow keys to navigate, enter to select, or press the key shown" {
			return true
		}
	}
	return false
}

var powershellPromptLine = regexp.MustCompile(`(?i)^ps\s+[^>]+>\s*`)
var directCursorLaunch = regexp.MustCompile(`(?i)^cursor-agent(?:\.cmd)?(?:\s+--[a-z0-9-]+(?:[ =](?:"[^"]*"|'[^']*'|[a-z0-9._/:-]+))?)*\s*$`)

// LaunchOnly examines the most recent PowerShell prompt, ignoring older
// shell output. Terminal soft wraps are joined for the Herdr launch
// wrapper; any subsequent agent output makes the screen observed.
func LaunchOnly(screen string) bool {
	lines := strings.Split(screen, "\n")
	last, command := -1, ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if loc := powershellPromptLine.FindStringIndex(trimmed); loc != nil {
			last, command = i, trimmed[loc[1]:]
		}
	}
	if last < 0 {
		return false
	}
	continuation := false
	for _, line := range lines[last+1:] {
		if strings.TrimSpace(line) != "" {
			continuation = true
			command += strings.TrimRight(line, "\r")
		}
	}
	if !continuation {
		return true
	}
	compact := strings.Join(strings.Fields(text.ASCIILower(command)), "")
	if strings.HasPrefix(compact, "if((get-commandcursor-agent") && strings.Contains(compact, "start-process-filepathcursor-agent") && strings.HasSuffix(compact, "-nonewwindow-wait}") {
		return true
	}
	return directCursorLaunch.MatchString(command)
}

// Evaluate classifies the visible screen of a fresh or reused cursor start
// together with the native agent state. kind and goos carry the gate (the
// caller passes platform.Current in production and a simulated platform in
// tests); state is the agent_status the last probe read ("" or
// "unavailable" when the state read failed); screen is the visible screen
// text and screenOK whether that read succeeded. Non-target kind/platform
// pairs come back observed with no further meaning — the gate is off.
func Evaluate(kind, goos, state, screen string, screenOK bool) Evaluation {
	if !EnabledFor(kind, goos) {
		return Evaluation{Verdict: VerdictObserved, Reason: ReasonVisibleOutput, Source: SourceVisible}
	}
	if state == "" || state == "unavailable" {
		return Evaluation{Verdict: VerdictUnverified, Reason: ReasonStateUnavailable, Source: SourceVisible}
	}
	if ActiveTrustDialog(screen) {
		return Evaluation{Verdict: VerdictBlocked, Reason: ReasonWorkspaceTrust, Source: SourceVisible}
	}
	if PartialTrustDialog(screen) {
		return Evaluation{Verdict: VerdictUnverified, Reason: ReasonScreenIncomplete, Source: SourceVisible}
	}
	if !screenOK {
		return Evaluation{Verdict: VerdictUnverified, Reason: ReasonScreenUnavailable, Source: SourceVisible}
	}
	if strings.TrimSpace(screen) == "" {
		return Evaluation{Verdict: VerdictUnverified, Reason: ReasonEmptyScreen, Source: SourceVisible}
	}
	if LaunchOnly(screen) {
		return Evaluation{Verdict: VerdictUnverified, Reason: ReasonLaunchOnly, Source: SourceVisible}
	}
	return Evaluation{Verdict: VerdictObserved, Reason: ReasonVisibleOutput, Source: SourceVisible}
}
