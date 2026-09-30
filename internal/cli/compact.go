package cli

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/spawn"
)

const compactDefaultTimeoutMS = 300000

const compactPollInterval = 2 * time.Second

const compactScreenLines = 40

// compactProofByKind holds the compaction proof each supported kind prints
// after `/compact`; a kind without an entry has no verified compact command.
// The proof only counts below the line where `/compact` was sent: an earlier
// compaction on the screen is not proof of this one.
var compactProofByKind = map[string]string{
	"claude": "Compacted",
	"codex":  "Context compacted",
	"pi":     "Compacted from",
}

// compactThinkingFooterRE matches a thinking level at the end of a footer
// line only when a boundary precedes it (start of line, whitespace, • or :),
// so a word like `cutoff` does not read as `off`.
var compactThinkingFooterRE = regexp.MustCompile(`(^|[\s•:])(off|minimal|low|medium|high|xhigh|max)\s*$`)

func compactProof(kind string) string {
	return compactProofByKind[kind]
}

func cmdCompact(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	agent := ""
	timeoutMS := int64(compactDefaultTimeoutMS)
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--timeout":
			if i+1 >= len(argv) {
				core.DieFriction("compact: --timeout expects a value", 2, frictionLogPath, "compact")
			}
			i++
			value, err := strconv.ParseInt(argv[i], 10, 64)
			if err != nil || value <= 0 {
				core.DieFriction("compact: --timeout expects milliseconds", 2, frictionLogPath, "compact")
			}
			timeoutMS = value
		default:
			if agent != "" {
				core.DieFriction(fmt.Sprintf("compact: unexpected argument '%s'", argv[i]), 2, frictionLogPath, "compact")
			}
			agent = argv[i]
		}
	}
	if agent == "" {
		core.DieFriction("compact: <agent> [--timeout MS]", 2, frictionLogPath, "compact")
	}
	sd := core.StateDirPath(ctx, env, cwd)
	line := core.RosterLine(sd, agent)
	if line == "" {
		core.DieFriction(fmt.Sprintf("agent '%s' is not in this skill's roster (spawn it first, or pass a name you spawned)", agent), 3, frictionLogPath, "compact")
	}
	cols := strings.Split(line, "\t")
	at := func(i int) string {
		if i < len(cols) {
			return cols[i]
		}
		return ""
	}
	kind := at(2)
	if compactProof(kind) == "" {
		core.DieFriction(fmt.Sprintf("compact: kind '%s' has no verified compact command; release --close and spawn --fresh instead", kind), 2, frictionLogPath, "compact")
	}
	return compactRun(agent, at(1), kind, at(3), at(11), at(8), timeoutMS, ctx, env, cwd, false)
}

// compactRun sends /compact once to a rostered worker through its pane and
// waits for the kind's compaction proof below the sent command, then for the
// worker back at idle or done. It is the shared step behind the `compact`
// command and `dispatch --compact`. The caller must have checked the roster
// and the kind. A busy or unreachable worker dies (10, 4, 6); on success it
// prints the result JSON (a stderr line inside a dispatch) and returns 0, and on timeout it prints the timeout
// JSON and returns 9. It never uses `agent prompt` and never resends.
func compactRun(agent, pane, kind, role, lane, model string, timeoutMS int64, ctx *core.Config, env platform.Env, cwd string, inDispatch bool) int {
	state := herdr.AgentState(agent, env, herdr.Timeout, nil)
	if state.State == "working" || state.State == "blocked" {
		core.DieFriction(fmt.Sprintf("compact: agent '%s' is %s; compact only an idle worker", agent, state.State), 10, frictionLogPath, "compact")
	}
	compactDieOnDeadWorker(agent, state)
	proof := compactProof(kind)
	// The proof lines already on screen before the send: pi does not echo the
	// /compact it runs, so a proof counts when it sits below the echoed command
	// (claude, codex) or when it is a line that was not there before the send.
	before, readOK := herdr.AgentReadOK(env, agent, "recent", intPtr(compactScreenLines))
	if !readOK {
		// Without the screen before the send, an earlier compaction still on screen
		// would read as this one's proof: send nothing.
		core.DieFriction(fmt.Sprintf("compact: could not read the screen of '%s' before sending; nothing was sent", agent), 4, frictionLogPath, "compact")
	}
	seen := compactProofLines(before, proof)
	if !herdr.PaneSendText(pane, "/compact", env) {
		core.DieFriction(fmt.Sprintf("compact: could not send /compact to pane '%s'; release --close and spawn --fresh instead", pane), 4, frictionLogPath, "compact")
	}
	if !herdr.PaneSendKeys(pane, "Enter", env) {
		core.DieFriction(fmt.Sprintf("compact: could not send Enter to pane '%s'; release --close and spawn --fresh instead", pane), 4, frictionLogPath, "compact")
	}
	start := platform.Now()
	deadline := start.Add(time.Duration(timeoutMS) * time.Millisecond)
	compacted := false
	for {
		if !compacted && compactProofNew(herdr.AgentRead(env, agent, "recent", intPtr(compactScreenLines)), proof, seen) {
			compacted = compactWaitIdle(agent, env, deadline)
		} else if !compacted {
			// No proof yet: a worker that died meanwhile stops the wait now (6, 4)
			// instead of running out the deadline.
			compactDieOnDeadWorker(agent, herdr.AgentState(agent, env, herdr.Timeout, nil))
		}
		if compacted || !platform.Now().Before(deadline) {
			break
		}
		time.Sleep(compactPollInterval)
	}
	elapsed := platform.Now().Sub(start).Milliseconds()
	if !compacted {
		fmt.Fprintln(platform.Stdout, jsonjs.Stringify(jsonjs.O("agent", agent, "kind", kind, "status", "timeout", "elapsed_ms", elapsed)))
		return 9
	}
	compactThinkingWarning(agent, kind, role, lane, model, ctx, env, cwd)
	if inDispatch {
		// The dispatch keeps one JSON line on stdout: its own result.
		fmt.Fprintf(platform.Stderr, "herdr-soho: dispatch: compacted '%s' in %d ms\n", agent, elapsed)
		return 0
	}
	fmt.Fprintln(platform.Stdout, jsonjs.Stringify(jsonjs.O("agent", agent, "kind", kind, "status", "compacted", "elapsed_ms", elapsed)))
	return 0
}

// compactDieOnDeadWorker exits 4 (unavailable) or 6 (gone) with the same
// message before the send and during the idle wait; it returns when the
// worker is neither.
func compactDieOnDeadWorker(agent string, state herdr.AgentStateResult) {
	if state.State == "unavailable" {
		cause := state.Cause
		if cause == "" {
			cause = "herdr agent get failed"
		}
		core.DieFriction(fmt.Sprintf("compact: agent '%s' is unavailable: %s", agent, cause), 4, frictionLogPath, "compact")
	}
	if state.State == "gone" {
		core.DieFriction(fmt.Sprintf("compact: agent '%s' is no longer live", agent), 6, frictionLogPath, "compact")
	}
}

// compactWaitIdle polls the agent state until it is idle or done or the
// deadline passes; a Codex worker stays working for a few seconds after the
// proof, so this keeps waiting instead of treating working as a failure. A
// worker that dies in the meantime (gone, unavailable) exits right away (6,
// 4) instead of waiting out the deadline as a timeout.
func compactWaitIdle(agent string, env platform.Env, deadline time.Time) bool {
	for {
		state := herdr.AgentState(agent, env, herdr.Timeout, nil)
		if state.State == "idle" || state.State == "done" {
			return true
		}
		compactDieOnDeadWorker(agent, state)
		if !platform.Now().Before(deadline) {
			return false
		}
		time.Sleep(compactPollInterval)
	}
}

// compactCommandLine reports whether the line is the sent /compact command
// itself: exactly /compact once surrounding whitespace and a leading prompt
// marker (>, ›, ❯, │ or $) are removed. A line that merely mentions /compact
// inside other text is not the anchor.
func compactCommandLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, marker := range []string{">", "›", "❯", "│", "$"} {
		if strings.HasPrefix(trimmed, marker) {
			trimmed = strings.TrimSpace(trimmed[len(marker):])
			break
		}
	}
	return trimmed == "/compact"
}

// compactProofBelow reports whether the kind's proof appears below the last
// line of the screen that is the sent /compact command; proof above it (an
// earlier compaction) does not count, and a screen without the sent command
// has no proof.
// compactProofLines returns the trimmed screen lines that hold the proof.
func compactProofLines(screen, proof string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n") {
		if strings.Contains(line, proof) {
			out[strings.TrimSpace(line)] = true
		}
	}
	return out
}

// compactProofNew reports a proof below the echoed /compact, or a proof line
// that was not on screen before the send (pi prints `Compacted from <n>
// tokens` with no echo of the command).
func compactProofNew(screen, proof string, seen map[string]bool) bool {
	if compactProofBelow(screen, proof) {
		return true
	}
	for line := range compactProofLines(screen, proof) {
		if !seen[line] {
			return true
		}
	}
	return false
}

func compactProofBelow(screen, proof string) bool {
	lines := strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n")
	marker := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if compactCommandLine(lines[i]) {
			marker = i
			break
		}
	}
	if marker < 0 {
		return false
	}
	for i := marker + 1; i < len(lines); i++ {
		if strings.Contains(lines[i], proof) {
			return true
		}
	}
	return false
}

// compactThinkingWarning warns, for pi only, when the last footer line of the
// screen containing a bullet ends with a thinking different from the
// worker's effective effort after compaction; it never changes the exit code.
func compactThinkingWarning(agent, kind, role, lane, model string, ctx *core.Config, env platform.Env, cwd string) {
	if kind != "pi" {
		return
	}
	screen := herdr.AgentRead(env, agent, "recent", intPtr(compactScreenLines))
	thinking := compactThinkingFooter(screen)
	if thinking == "" {
		return
	}
	effort := spawn.ResolveSpawnEffort(role, lane, kind, core.SpawnKindLayer(ctx, role, lane, false, env), ctx, env, cwd, model)
	if effort == "" || thinking == effort {
		return
	}
	core.Warn(fmt.Sprintf("compact: '%s' shows thinking '%s' after compaction; its effort is '%s'", agent, thinking, effort), frictionLogPath, "compact")
}

// compactThinkingFooter returns the thinking value the last footer line of
// the screen (the last line containing a bullet) ends with, or "" when that
// line does not end with a level.
func compactThinkingFooter(screen string) string {
	lines := strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.Contains(line, "•") {
			continue
		}
		if m := compactThinkingFooterRE.FindStringSubmatch(line); m != nil {
			return m[2]
		}
		return ""
	}
	return ""
}
