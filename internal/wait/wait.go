package wait

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/provider"
	"github.com/djalmajr/herdr-soho/internal/reportscan"
	"github.com/djalmajr/herdr-soho/internal/taskreport"
)

const enterRetryLimit = 3

var uintPattern = regexp.MustCompile(`^[0-9]+$`)
var frictionLogFile string

func SetFrictionLogFile(file string) { frictionLogFile = file }

func readWaitFile(sd, agent, name string) (string, bool) {
	data, err := platform.ReadTextFile(filepath.Join(sd, "wait", agent+"."+name))
	if err != nil {
		return "", false
	}
	return strings.TrimRight(data, "\n"), true
}

func writeWaitFile(sd, agent, name, value string) {
	if err := os.WriteFile(filepath.Join(sd, "wait", agent+"."+name), []byte(value), 0o666); err != nil {
		panic(err)
	}
}

func removeWaitFile(sd, agent, name string) { _ = os.Remove(filepath.Join(sd, "wait", agent+"."+name)) }

func reportNonEmpty(file string) bool {
	if file == "" {
		return false
	}
	info, err := os.Stat(file)
	return err == nil && info.Size() > 0
}

func CksumField(value string) uint32 {
	data := []byte(value)
	n := uint64(len(data))
	for {
		data = append(data, byte(n))
		n >>= 8
		if n == 0 {
			break
		}
	}
	crc := uint32(0)
	for _, b := range data {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return ^crc
}

func NormalizeScreen(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	digitRuns := regexp.MustCompile(`[0-9]+`).ReplaceAllString(value, "#")
	progress := regexp.MustCompile(`[\x{2800}-\x{28ff}\x{25d0}-\x{25d3}\x{2588}\x{258c}\x{2590}\x{2591}]`)
	return progress.ReplaceAllString(digitRuns, "*")
}

func ActivityAgeSeconds(sd, agent, screen string, now int64) *int64 {
	hashRaw, ok := readWaitFile(sd, agent, "stuck-hash")
	if !ok || strings.TrimSpace(hashRaw) == "" || strings.TrimSpace(screen) == "" || strings.TrimSpace(hashRaw) == strconv.FormatUint(uint64(CksumField(NormalizeScreen(""))), 10) {
		return nil
	}
	hash := strconv.FormatUint(uint64(CksumField(NormalizeScreen(screen))), 10)
	if hash != strings.TrimSpace(hashRaw) {
		since, valid := positiveWaitInt(sd, agent, "probe-at")
		if !valid {
			since, valid = positiveWaitInt(sd, agent, "stuck-since")
		}
		if !valid {
			return nil
		}
		age := now - since
		return &age
	}
	at, valid := positiveWaitInt(sd, agent, "activity-at")
	if !valid {
		return nil
	}
	age := now - at
	return &age
}

func positiveWaitInt(sd, agent, name string) (int64, bool) {
	raw, ok := readWaitFile(sd, agent, name)
	if !ok {
		return 0, false
	}
	raw = strings.TrimSpace(raw)
	if !uintPattern.MatchString(raw) {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	return n, err == nil && n > 0
}

func NormalizeApproveScreen(value string) string { return NormalizeScreen(value) }

func WaitRank(code int) int {
	switch code {
	case 4:
		return 6
	case 11:
		return 5
	case 14:
		return 4
	case 15:
		return 3
	case 7:
		return 2
	case 6:
		return 1
	default:
		return 0
	}
}

func PollIntervalMs(env platform.Env) int {
	raw := env.Get("HERDR_SOHO_WAIT_POLL_MS")
	if !uintPattern.MatchString(raw) {
		return 3000
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 3000 {
		return 3000
	}
	return n
}

func markerEpoch(marker string, fallback int64) int64 {
	parts := strings.Fields(marker)
	if len(parts) == 0 || !uintPattern.MatchString(parts[0]) {
		return fallback
	}
	n, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func promptWindow(ctx *core.Config, env platform.Env) int64 {
	raw := core.Cfg(ctx, "prompt_check_seconds", "15", env)
	if !uintPattern.MatchString(raw) {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func sendWarning(message string) { core.Warn(message, frictionLogFile, "wait") }

func TryAutoApprove(sd, agent string, ctx *core.Config, env platform.Env) bool {
	if core.Cfg(ctx, "auto_approve", "off", env) != "on" {
		return false
	}
	maxRaw := core.Cfg(ctx, "max_auto_approvals", "20", env)
	max, err := strconv.Atoi(maxRaw)
	if err != nil {
		max = 0
	}
	n := 0
	if raw, ok := readWaitFile(sd, agent, "approvals"); ok {
		trimmed := strings.TrimSpace(raw)
		if !uintPattern.MatchString(trimmed) {
			n = math.MaxInt32
		} else if parsed, e := strconv.Atoi(trimmed); e == nil {
			n = parsed
		} else {
			n = math.MaxInt32
		}
	}
	if n >= max {
		sendWarning(fmt.Sprintf("auto_approve: %s reached max_auto_approvals=%s; leaving it blocked", agent, maxRaw))
		return false
	}
	line := strings.Split(core.RosterLine(sd, agent), "\t")
	kind := ""
	if len(line) > 2 {
		kind = line[2]
	}
	key := "enter"
	if kind == "codex" {
		key = "y"
	}
	if !herdr.AgentSendKeys(agent, key, env) {
		return false
	}
	next := n + 1
	writeWaitFile(sd, agent, "approvals", fmt.Sprintf("%d\n", next))
	log, err := os.OpenFile(filepath.Join(sd, "wait", agent+".approvals.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err == nil {
		_, _ = fmt.Fprintf(log, "%s auto-approved dialog #%d\n", core.NowStamp(platform.Now()), next)
		_ = log.Close()
	}
	sendWarning(fmt.Sprintf("auto_approve: answered dialog #%d for '%s' with its default option", next, agent))
	removeWaitFile(sd, agent, "blocked")
	return true
}

func retryNotReceived(sd, agent, marker string, ctx *core.Config, env platform.Env, st herdr.AgentStateResult) string {
	window := promptWindow(ctx, env)
	lastEpoch := markerEpoch(marker, 0)
	attempts := 0
	if retryRaw, ok := readWaitFile(sd, agent, "enter-retry"); ok {
		parts := strings.Fields(strings.TrimSpace(retryRaw))
		if len(parts) != 2 || !uintPattern.MatchString(parts[0]) || !uintPattern.MatchString(parts[1]) {
			return "not-received"
		}
		attempts, _ = strconv.Atoi(parts[0])
		lastEpoch, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	now := platform.Now().Unix()
	screen := herdr.AgentRead(env, agent, "recent-unwrapped", intPtr(40))
	var inInput *bool
	if MarkerHasPromptPath(marker) {
		v := QueuedPromptSitsInInput(marker, screen, sd, agent)
		inInput = &v
	}
	if inInput != nil && !*inInput {
		return "not-received"
	}
	if now-lastEpoch < window {
		return "working"
	}
	markerInInput := PromptSitsInInput(screen)
	if inInput != nil {
		markerInInput = *inInput
	}
	if attempts < enterRetryLimit && markerInInput {
		next := attempts + 1
		herdr.AgentSendKeys(agent, "enter", env)
		writeWaitFile(sd, agent, "enter-retry", fmt.Sprintf("%d %d\n", next, now))
		sendWarning(fmt.Sprintf("prompt to '%s' was still in its input box; sent Enter again (%d of %d)", agent, next, enterRetryLimit))
		return "working"
	}
	return "not-received"
}

func intPtr(value int) *int { return &value }

func ProbeAgent(sd, agent, report string, ctx *core.Config, env platform.Env) string {
	grace, _ := strconv.ParseFloat(core.Cfg(ctx, "settled_grace", "45", env), 64)
	if reportNonEmpty(report) {
		removeWaitFile(sd, agent, "queued")
		removeWaitFile(sd, agent, "not-received")
		removeWaitFile(sd, agent, "enter-retry")
		info, _ := os.Stat(report)
		size := strconv.FormatInt(info.Size(), 10)
		prev, ok := readWaitFile(sd, agent, "size")
		writeWaitFile(sd, agent, "size", wcSize(info.Size())+"\n")
		if ok && strings.TrimSpace(prev) == size {
			return "done"
		}
		return "pending"
	}
	st := herdr.AgentState(agent, env, herdr.Timeout, nil)
	nrFile := filepath.Join(sd, "wait", agent+".not-received")
	queuedFile := filepath.Join(sd, "wait", agent+".queued")
	retryFile := filepath.Join(sd, "wait", agent+".enter-retry")
	if st.State == "gone" {
		_ = os.Remove(queuedFile)
		_ = os.Remove(retryFile)
		return "gone"
	}
	if st.State == "unavailable" {
		return "unavailable\t" + st.Cause
	}
	queuedText, queuedExists := readWaitFile(sd, agent, "queued")
	queuedPending, queuedTerminal := false, false
	if queuedExists {
		if st.State == "working" {
			if MarkerSeqChanged(queuedText, st.Seq) {
				_ = os.Remove(queuedFile)
				_ = os.Remove(retryFile)
				queuedExists = false
			}
		} else if st.State == "blocked" {
			queuedTerminal = true
		} else {
			queuedPending = true
		}
	}
	if !queuedPending {
		if mark, ok := readWaitFile(sd, agent, "not-received"); ok {
			if st.State == "working" || st.State == "blocked" || MarkerSeqChanged(mark, st.Seq) {
				_ = os.Remove(nrFile)
				_ = os.Remove(retryFile)
			} else {
				return retryNotReceived(sd, agent, mark, ctx, env, st)
			}
		}
	}
	if st.State == "blocked" {
		_ = os.Remove(retryFile)
		clearQueued := func() {
			if queuedTerminal {
				_ = os.Remove(queuedFile)
				_ = os.Remove(retryFile)
			}
		}
		if _, ok := readWaitFile(sd, agent, "blocked"); ok {
			line := strings.Split(core.RosterLine(sd, agent), "\t")
			kind := ""
			if len(line) > 2 {
				kind = line[2]
			}
			visible := herdr.AgentRead(env, agent, "visible", intPtr(40))
			if provider.DialogKind(kind, visible) == "question" {
				writeWaitFile(sd, agent, "question", provider.QuestionText(visible)+"\n")
				clearQueued()
				return "question"
			}
			h := strconv.FormatUint(uint64(CksumField(NormalizeApproveScreen(visible))), 10)
			count := 1
			if raw, ok := readWaitFile(sd, agent, "approve-screen"); ok {
				parts := strings.Split(raw, "\t")
				if len(parts) > 1 && parts[1] == h {
					if n, e := strconv.Atoi(parts[0]); e == nil && n > 0 {
						count = n + 1
					}
				}
			}
			if count >= 3 {
				sendWarning(fmt.Sprintf("auto_approve: the same dialog came back 3 times for '%s'; leaving it blocked", agent))
				clearQueued()
				return "blocked"
			}
			if !TryAutoApprove(sd, agent, ctx, env) {
				clearQueued()
				return "blocked"
			}
			writeWaitFile(sd, agent, "approve-screen", fmt.Sprintf("%d\t%s\n", count, h))
			clearQueued()
			return "working"
		}
		writeWaitFile(sd, agent, "blocked", "")
		return "working"
	}
	removeWaitFile(sd, agent, "blocked")
	pfile := filepath.Join(sd, "wait", agent+".provider")
	clearProvider := func() { _ = os.Remove(pfile); removeWaitFile(sd, agent, "capacity-at") }
	if st.State == "working" {
		clearProvider()
	}
	var screen string
	screenRead := false
	visible := func() string {
		if !screenRead {
			screen = herdr.AgentRead(env, agent, "visible", nil)
			screenRead = true
		}
		return screen
	}
	if st.State == "working" {
		text := visible()
		hash := strconv.FormatUint(uint64(CksumField(NormalizeScreen(text))), 10)
		now := platform.Now().Unix()
		prev, hasPrev := readWaitFile(sd, agent, "stuck-hash")
		if !hasPrev || prev != hash {
			prevProbe, ok := positiveWaitInt(sd, agent, "probe-at")
			if !ok {
				prevProbe, ok = positiveWaitInt(sd, agent, "stuck-since")
			}
			if hasPrev && prev != "" && prev != strconv.FormatUint(uint64(CksumField(NormalizeScreen(""))), 10) && strings.TrimSpace(text) != "" && ok {
				writeWaitFile(sd, agent, "activity-at", fmt.Sprintf("%d\n", prevProbe))
			}
			writeWaitFile(sd, agent, "stuck-hash", hash+"\n")
			writeWaitFile(sd, agent, "stuck-since", fmt.Sprintf("%d\n", now))
			removeWaitFile(sd, agent, "stuck-warned")
		} else if limit, e := strconv.ParseFloat(core.Cfg(ctx, "stuck_warn_minutes", "20", env), 64); e == nil && limit > 0 {
			if _, warned := readWaitFile(sd, agent, "stuck-warned"); !warned {
				raw, ok := readWaitFile(sd, agent, "stuck-since")
				since, e := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
				if !ok || e != nil || since <= 0 {
					writeWaitFile(sd, agent, "stuck-since", fmt.Sprintf("%d\n", now))
				} else if float64(now-since) >= limit*60 {
					mins := (now - since) / 60
					sendWarning(fmt.Sprintf("agent '%s' has shown the same screen (apart from counters) for %d min while working; it may be stuck in one tool call. Inspect: herdr agent read %s --source recent-unwrapped --lines 60", agent, mins, agent))
					writeWaitFile(sd, agent, "stuck-warned", "")
				}
			}
		}
		if strings.TrimSpace(text) != "" {
			writeWaitFile(sd, agent, "probe-at", fmt.Sprintf("%d\n", now))
		}
	}
	if st.State != "working" {
		if quota := provider.QuotaDetect(st.State, herdr.AgentRead(env, agent, "visible", intPtr(20))); quota != nil {
			_ = os.Remove(queuedFile)
			_ = os.Remove(retryFile)
			content := quota[0] + "\n"
			if quota[1] != "" {
				content += quota[1] + "\n"
			}
			writeWaitFile(sd, agent, "quota", content)
			clearProvider()
			return "quota"
		}
		ptext := herdr.AgentRead(env, agent, "recent-unwrapped", intPtr(40))
		p := provider.ProviderDetectTexts(st.State, ptext, core.Cfg(ctx, "provider_capacity_texts", "", env), core.Cfg(ctx, "provider_error_texts", "", env))
		if p != nil {
			if p.Status == "provider-error" && p.Auth {
				_ = os.Remove(queuedFile)
				_ = os.Remove(retryFile)
				clearProvider()
				writeWaitFile(sd, agent, "provider-cause", p.Cause+"\n")
				return "provider-error"
			}
			target := fmt.Sprintf("%d\n%s", CksumField(ptext), p.Status)
			prev, _ := readWaitFile(sd, agent, "provider")
			if prev != target {
				_ = os.Remove(pfile)
				writeWaitFile(sd, agent, "provider", target+"\n")
				return "working"
			}
			writeWaitFile(sd, agent, "provider-cause", p.Cause+"\n")
			if p.Status == "provider-error" {
				_ = os.Remove(queuedFile)
				_ = os.Remove(retryFile)
				return "provider-error"
			}
			used := 0
			if raw, ok := readWaitFile(sd, agent, "capacity-retries"); ok {
				if !uintPattern.MatchString(strings.TrimSpace(raw)) {
					_ = os.Remove(queuedFile)
					_ = os.Remove(retryFile)
					return "capacity"
				}
				used, _ = strconv.Atoi(strings.TrimSpace(raw))
			}
			limit, e := strconv.Atoi(core.Cfg(ctx, "provider_retries", "3", env))
			if e != nil || used >= limit {
				_ = os.Remove(queuedFile)
				_ = os.Remove(retryFile)
				return "capacity"
			}
			at, ok := readWaitFile(sd, agent, "capacity-at")
			if !ok {
				writeWaitFile(sd, agent, "capacity-at", fmt.Sprintf("%d\n", platform.Now().Unix()))
				return "working"
			}
			delay, e := strconv.ParseInt(core.Cfg(ctx, "provider_retry_delay", "60", env), 10, 64)
			last, e2 := strconv.ParseInt(strings.TrimSpace(at), 10, 64)
			now := platform.Now().Unix()
			if e != nil || e2 != nil || now-last < delay {
				return "working"
			}
			report := core.LastReport(sd, agent)
			prompt := fmt.Sprintf("The model provider was at capacity and your last request failed. Continue the task from where you stopped; do not redo finished steps. When finished, write your report to %s and reply with only that path.", report)
			sent := herdr.AgentPrompt(agent, prompt, env)
			if !sent.Ok {
				sendWarning("provider capacity: failed to send the continue to '" + agent + "': " + sent.Raw + "; acting as exhausted")
				_ = os.Remove(queuedFile)
				_ = os.Remove(retryFile)
				return "capacity"
			}
			writeWaitFile(sd, agent, "capacity-retries", fmt.Sprintf("%d\n", used+1))
			removeWaitFile(sd, agent, "capacity-at")
			_ = os.Remove(pfile)
			sendWarning(fmt.Sprintf("provider capacity: sent continue #%d of %d to '%s': %s", used+1, limit, agent, p.Cause))
			_ = os.Remove(queuedFile)
			_ = os.Remove(retryFile)
			return "working"
		}
		clearProvider()
	}
	if queuedPending {
		recent := herdr.AgentRead(env, agent, "recent-unwrapped", intPtr(40))
		inInput := QueuedPromptSitsInInput(queuedText, recent, sd, agent)
		promptPath := QueuedPromptPath(queuedText, sd, agent)
		if promptPath == "" {
			promptPath = "-"
		}
		seq := jsString(st.Seq)
		if seq == "" {
			seq = "-"
		}
		if !inInput {
			writeWaitFile(sd, agent, "not-received", fmt.Sprintf("%d %s %s\n", platform.Now().Unix(), seq, promptPath))
			_ = os.Remove(queuedFile)
			_ = os.Remove(retryFile)
			return "not-received"
		}
		epoch := markerEpoch(queuedText, platform.Now().Unix())
		writeWaitFile(sd, agent, "not-received", fmt.Sprintf("%d %s %s\n", epoch, seq, promptPath))
		_ = os.Remove(queuedFile)
		now := platform.Now().Unix()
		attempts := 0
		last := epoch
		if raw, ok := readWaitFile(sd, agent, "enter-retry"); ok {
			parts := strings.Fields(strings.TrimSpace(raw))
			if len(parts) != 2 || !uintPattern.MatchString(parts[0]) || !uintPattern.MatchString(parts[1]) {
				return "not-received"
			}
			attempts, _ = strconv.Atoi(parts[0])
			last, _ = strconv.ParseInt(parts[1], 10, 64)
		}
		if now-last < promptWindow(ctx, env) {
			return "working"
		}
		if attempts < enterRetryLimit {
			herdr.AgentSendKeys(agent, "enter", env)
			writeWaitFile(sd, agent, "enter-retry", fmt.Sprintf("%d %d\n", attempts+1, now))
			sendWarning(fmt.Sprintf("prompt to '%s' was still in its input box; sent Enter again (%d of %d)", agent, attempts+1, enterRetryLimit))
			return "working"
		}
		return "not-received"
	}
	hash := strconv.FormatUint(uint64(CksumField(visible())), 10)
	last, ok := readWaitFile(sd, agent, "screen")
	now := platform.Now().Unix()
	if st.State == "working" || !ok || hash != last {
		writeWaitFile(sd, agent, "screen", hash+"\n")
		writeWaitFile(sd, agent, "since", fmt.Sprintf("%d\n", now))
		return "working"
	}
	sinceRaw, ok := readWaitFile(sd, agent, "since")
	since, e := strconv.ParseFloat(strings.TrimSpace(sinceRaw), 64)
	if !ok || e != nil {
		since = float64(now)
	}
	if float64(now)-since >= grace {
		return "settled"
	}
	return "working"
}

func wcSize(size int64) string {
	s := strconv.FormatInt(size, 10)
	w := 8
	if len(s) == 9 {
		w = 10
	} else if len(s) > 9 {
		w = len(s)
	}
	return strings.Repeat(" ", max(0, w-len(s))) + s
}

func jsonLine(value *jsonjs.Object) {
	_, _ = platform.Stdout.Write([]byte(jsonjs.Stringify(value) + "\n"))
}

func WaitFor(agents []string, sd string, ctx *core.Config, env platform.Env, timeoutMs float64, anyDone bool, cwd string) int {
	for _, agent := range agents {
		removeWaitFile(sd, agent, "size")
	}
	started := platform.Now()
	deadline := started.Add(time.Duration(timeoutMs * float64(time.Millisecond)))
	remaining := append([]string(nil), agents...)
	last := map[string]string{}
	rc := 0
	for {
		pending := []string{}
		for _, agent := range remaining {
			report := core.LastReport(sd, agent)
			state := ProbeAgent(sd, agent, report, ctx, env)
			tag := strings.SplitN(state, "\t", 2)[0]
			switch tag {
			case "done":
				text, _ := platform.ReadTextFile(report)
				partial := reportscan.PartialCount(text)
				header := reportscan.ReviewHeader(text)
				line := jsonjs.O("agent", agent, "status", "done", "report", report)
				pointer := taskreport.ReadTaskReportPointer(sd, agent)
				if pointer != nil {
					current, _ := pointer.Get("current")
					stable, _ := pointer.Get("task_report")
					if current == report {
						synced, e := taskreport.SyncTaskReport(sd, agent)
						if e == nil && synced != "" {
							line.Set("task_report", stable)
						}
					}
				}
				if header != nil {
					line.Set("verdict", header.Verdict)
					line.Set("findings", header.Findings)
					line.Set("severity", jsonjs.O("P0", header.Severity["P0"], "P1", header.Severity["P1"], "P2", header.Severity["P2"], "P3", header.Severity["P3"]))
				}
				if partial > 0 {
					line.Set("partial", partial)
				}
				if header != nil || partial > 0 {
					effective := "pass"
					if partial > 0 || header.Verdict == "fail" || header.Severity["P0"]+header.Severity["P1"]+header.Severity["P2"] > 0 {
						effective = "fail"
					}
					line.Set("verdict_effective", effective)
				}
				jsonLine(line)
				if partial > 0 {
					prev, _ := readWaitFile(sd, agent, "partial-warned")
					if prev != report {
						sendWarning(fmt.Sprintf("report of '%s' marks %d item(s) partial: a partial item is not a pass; read them before commit, push or release", agent, partial))
						writeWaitFile(sd, agent, "partial-warned", report+"\n")
					}
				}
				if header != nil && header.Verdict == "pass" {
					open := header.Severity["P0"] + header.Severity["P1"] + header.Severity["P2"]
					if open > 0 {
						sendWarning(fmt.Sprintf("report of '%s' says verdict pass with %s open P0-P2 finding(s): read them before commit, push or release", agent, numberString(open)))
					}
				}
				if header != nil {
					sum := header.Severity["P0"] + header.Severity["P1"] + header.Severity["P2"] + header.Severity["P3"]
					if sum != header.Findings {
						sendWarning(fmt.Sprintf("report of '%s': findings %s but P0..P3 add up to %s", agent, numberString(header.Findings), numberString(sum)))
					}
				} else {
					role := field(strings.Split(core.RosterLine(sd, agent), "\t"), 3)
					if core.IsReviewRole(role) {
						sendWarning(fmt.Sprintf("report of '%s' has no 'findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail' first line", agent))
					}
				}
				if core.Cfg(ctx, "notify", "off", env) == "on" {
					herdr.NotificationShow("herdr-soho: "+agent+" finished", report, env)
				}
				_ = core.MarkTaskDone(sd, agent, env)
				mirrorReport(sd, agent, report, ctx, env, cwd)
				if anyDone {
					return 0
				}
			case "blocked":
				dialog := lastNonEmpty(herdr.AgentRead(env, agent, "visible", nil), 20)
				jsonLine(jsonjs.O("agent", agent, "status", "blocked", "report", report, "dialog", dialog))
				if WaitRank(7) > WaitRank(rc) {
					rc = 7
				}
			case "question":
				q, _ := readWaitFile(sd, agent, "question")
				jsonLine(jsonjs.O("agent", agent, "status", "question", "report", report, "question", q))
				sendWarning("agent '" + agent + "' asked a question; nobody answers it automatically. Ask the user, then answer with herdr agent send-keys/prompt, or release the worker.")
				if WaitRank(7) > WaitRank(rc) {
					rc = 7
				}
			case "gone":
				jsonLine(jsonjs.O("agent", agent, "status", "gone", "report", report))
				if WaitRank(6) > WaitRank(rc) {
					rc = 6
				}
			case "settled":
				jsonLine(jsonjs.O("agent", agent, "status", "settled-no-report", "report", report))
				if WaitRank(6) > WaitRank(rc) {
					rc = 6
				}
			case "unavailable":
				cause := strings.TrimPrefix(state, "unavailable\t")
				jsonLine(jsonjs.O("agent", agent, "status", "unavailable", "report", report, "error", cause))
				sendWarning(fmt.Sprintf("agent '%s': herdr agent get failed: %s", agent, cause))
				if WaitRank(4) > WaitRank(rc) {
					rc = 4
				}
			case "quota":
				match, renewal := readQuotaFile(sd, agent)
				fields := strings.Split(core.RosterLine(sd, agent), "\t")
				kind, model, lane, role := field(fields, 2), field(fields, 8), field(fields, 11), field(fields, 3)
				if lane == "" {
					lane = core.LaneOfRole(ctx, role, env)
				}
				jsonLine(jsonjs.O("agent", agent, "status", "quota", "report", report, "lane", lane, "kind", kind, "model", model, "match", match, "renewal", renewal))
				sendWarning(fmt.Sprintf("quota: agent '%s' lane=%s kind=%s model=%s : %s%s", agent, showOr(lane, "?"), kind, showOr(model, "?"), match, renewalSuffix(renewal)))
				if WaitRank(11) > WaitRank(rc) {
					rc = 11
				}
			case "not-received":
				jsonLine(jsonjs.O("agent", agent, "status", "not-received", "report", report))
				sendWarning(fmt.Sprintf("prompt to '%s' never reached it: read the pane (herdr agent read %s --source visible), then dispatch again", agent, agent))
				if WaitRank(15) > WaitRank(rc) {
					rc = 15
				}
			case "provider-error", "capacity":
				cause, _ := readWaitFile(sd, agent, "provider-cause")
				fields := strings.Split(core.RosterLine(sd, agent), "\t")
				kind, model, lane, role := field(fields, 2), field(fields, 8), field(fields, 11), field(fields, 3)
				if lane == "" {
					lane = core.LaneOfRole(ctx, role, env)
				}
				if tag == "provider-error" {
					jsonLine(jsonjs.O("agent", agent, "status", tag, "report", report, "lane", lane, "kind", kind, "model", model, "cause", cause))
					sendWarning(fmt.Sprintf("provider error: agent '%s' lane=%s kind=%s model=%s : %s", agent, showOr(lane, "?"), kind, showOr(model, "?"), cause))
				} else {
					retriesRaw, _ := readWaitFile(sd, agent, "capacity-retries")
					retries, _ := strconv.Atoi(strings.TrimSpace(retriesRaw))
					jsonLine(jsonjs.O("agent", agent, "status", tag, "report", report, "lane", lane, "kind", kind, "model", model, "cause", cause, "retries", retries))
					sendWarning(fmt.Sprintf("provider capacity: agent '%s' lane=%s kind=%s model=%s : %s", agent, showOr(lane, "?"), kind, showOr(model, "?"), cause))
				}
				if WaitRank(14) > WaitRank(rc) {
					rc = 14
				}
			default:
				last[agent] = tag
				pending = append(pending, agent)
			}
		}
		remaining = pending
		if len(remaining) == 0 {
			return rc
		}
		if !platform.Now().Before(deadline) {
			now := platform.Now().Unix()
			win, _ := strconv.ParseFloat(core.Cfg(ctx, "stuck_warn_minutes", "20", env), 64)
			if win <= 0 {
				win = 20
			}
			for _, agent := range remaining {
				tag := last[agent]
				if tag == "" {
					tag = "working"
				}
				age := ActivityAgeSeconds(sd, agent, herdr.AgentRead(env, agent, "visible", nil), now)
				active := tag == "working" && age != nil && float64(*age) < win*60
				var ageValue any
				if age != nil {
					ageValue = *age
				}
				jsonLine(jsonjs.O("agent", agent, "status", "timeout", "elapsed_ms", platform.Now().Sub(started).Milliseconds(), "state", tag, "checkpoint", active, "activity_age_s", ageValue))
				if active {
					suffix := ""
					if !math.IsNaN(timeoutMs) && !math.IsInf(timeoutMs, 0) {
						suffix = fmt.Sprintf(" --timeout %s", formatTimeout(timeoutMs))
					}
					_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: checkpoint: '%s' is still working (screen changed %ds ago); wait again: herdr-soho wait %s%s\n", agent, *age, agent, suffix)
				} else if !math.IsNaN(timeoutMs) && !math.IsInf(timeoutMs, 0) {
					sendWarning(fmt.Sprintf("timeout waiting for '%s'; it may still be working (state: %s). Run: herdr-soho wait %s --timeout %s", agent, tag, agent, formatTimeout(timeoutMs*2)))
				} else {
					sendWarning(fmt.Sprintf("timeout waiting for '%s'; it may still be working (state: %s)", agent, tag))
				}
			}
			return 9
		}
		time.Sleep(time.Duration(PollIntervalMs(env)) * time.Millisecond)
	}
}

func field(fields []string, i int) string {
	if i < len(fields) {
		return fields[i]
	}
	return ""
}
func showOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func renewalSuffix(value string) string {
	if value != "" {
		return "; renewal: " + value
	}
	return ""
}
func formatTimeout(value float64) string { return strconv.FormatInt(int64(value), 10) }

func numberString(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
func lastNonEmpty(screen string, n int) string {
	lines := LastNonEmptyLines(screen, n)
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.Join(lines, "\n")
}
func readQuotaFile(sd, agent string) (string, string) {
	raw, ok := readWaitFile(sd, agent, "quota")
	if !ok {
		return "", ""
	}
	parts := strings.Split(raw, "\n")
	return field(parts, 0), field(parts, 1)
}

func mirrorReport(sd, agent, report string, ctx *core.Config, env platform.Env, cwd string) {
	tmp := env.Get("TMPDIR")
	if tmp == "" {
		tmp = os.TempDir()
	}
	dir := filepath.Join(tmp, "herdr-soho", core.WorkspaceID(ctx, env, cwd), "reports")
	if !strings.HasPrefix(report, dir+string(os.PathSeparator)) {
		return
	}
	base := filepath.Base(report)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	jobs := [][3]string{{report, filepath.Join(sd, "reports", base), ""}, {filepath.Join(filepath.Dir(report), stem+".brief.md"), filepath.Join(sd, "briefs", stem+".md"), ""}, {filepath.Join(filepath.Dir(report), stem+".dispatch.json"), filepath.Join(sd, "briefs", stem+".dispatch.json"), "optional"}}
	for _, job := range jobs {
		src, dst := job[0], job[1]
		if job[2] != "" {
			if _, err := os.Stat(src); err != nil {
				continue
			}
		}
		data, err := os.ReadFile(src)
		if err != nil {
			sendWarning("mirror: could not copy " + filepath.Base(src) + " of '" + agent + "' to the state dir: " + err.Error())
			continue
		}
		if existing, e := os.ReadFile(dst); e == nil {
			if string(existing) != string(data) {
				sendWarning("kept " + dst + ": it differs from " + src + ", which was not copied over it")
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o777); err == nil {
			err = os.WriteFile(dst, data, 0o666)
		}
		if err != nil {
			sendWarning("mirror: could not copy " + filepath.Base(src) + " of '" + agent + "' to the state dir: " + err.Error())
		}
	}
}

func CmdWait(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	agents := []string{}
	timeout := float64(math.NaN())
	any := false
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch arg {
		case "--timeout":
			if i+1 >= len(argv) {
				core.DieFriction("wait: --timeout expects a value", 2, "", "wait")
			}
			raw := argv[i+1]
			if !uintPattern.MatchString(raw) {
				core.DieFriction("wait: --timeout expects milliseconds, got '"+raw+"'", 2, "", "wait")
			}
			timeout, _ = strconv.ParseFloat(raw, 64)
			i++
		case "--any":
			any = true
		default:
			if strings.HasPrefix(arg, "--") {
				core.DieFriction("wait: unknown option "+arg, 2, "", "wait")
			}
			agents = append(agents, arg)
		}
	}
	if len(agents) == 0 {
		core.DieFriction("wait: give at least one agent name", 2, "", "wait")
	}
	sd := core.StateDir(ctx, env, cwd)
	for _, agent := range agents {
		if core.RosterLine(sd, agent) == "" {
			core.DieFriction("agent '"+agent+"' is not in the roster (state dir: "+sd+")", 3, "", "wait")
		}
	}
	if math.IsNaN(timeout) {
		for _, agent := range agents {
			role := field(strings.Split(core.RosterLine(sd, agent), "\t"), 3)
			if ms := core.RoleTimeoutMs(role, ctx, env, cwd); float64(ms) > timeout || math.IsNaN(timeout) {
				timeout = float64(ms)
			}
		}
	}
	// cmdWait turns every DieError out of waitFor into dieFriction, the
	// empty one included: die prints "herdr-soho: " and the log gets an
	// error(exit N) line with no text.
	defer func() {
		value := recover()
		if value == nil {
			return
		}
		if exitErr, ok := value.(*platform.ExitError); ok && !exitErr.Friction {
			if exitErr.Msg == "" {
				core.RecordFrictionError("", exitErr.Code, frictionLogFile, "wait")
				_, _ = fmt.Fprint(platform.Stderr, "herdr-soho: \n")
			} else {
				exitErr.Friction = true
			}
		}
		panic(value)
	}()
	return WaitFor(agents, sd, ctx, env, timeout, any, cwd)
}
