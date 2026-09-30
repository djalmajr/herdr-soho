package wait

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/provider"
)

func aliveInOtherPane(name, pane string, env platform.Env) (alive bool) {
	defer func() {
		if recover() != nil {
			alive = false
		}
	}()
	for _, raw := range herdr.LiveAgents(env, herdr.Timeout) {
		obj, ok := raw.(*jsonjs.Object)
		if !ok {
			continue
		}
		agentName, _ := obj.Get("name")
		if agentName != name {
			continue
		}
		paneValue, _ := obj.Get("pane_id")
		livePane, _ := paneValue.(string)
		if livePane == pane {
			return false
		}
		alive = true
	}
	return alive
}

func statusJson(agent, state, report string, fields ...any) {
	pairs := []any{"agent", agent, "status", state, "report", report}
	pairs = append(pairs, fields...)
	jsonLine(jsonjs.O(pairs...))
}

func rosterNames(sd string) []string {
	names := []string{}
	for _, row := range core.RosterRows(sd) {
		name := strings.Split(row, "\t")[0]
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func CmdStatus(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	sd := core.StateDir(ctx, env, cwd)
	if len(argv) == 0 {
		argv = rosterNames(sd)
		if len(argv) == 0 {
			fmt.Fprintf(platform.Stderr, "status: no agents in the roster (state dir: %s)\n", sd)
			return 0
		}
	}
	rc := 0
	for _, agent := range argv {
		report := core.LastReport(sd, agent)
		state, cause := "", ""
		match, renewal, kind, model, lane, question := "", "", "", "", "", ""
		quota := false
		var p *provider.Detection
		if reportNonEmpty(report) {
			state = "done"
		} else if core.RosterLine(sd, agent) == "" {
			state = "unknown-agent"
		} else {
			fields := strings.Split(core.RosterLine(sd, agent), "\t")
			pane := field(fields, 1)
			st := herdr.AgentState(agent, env, herdr.Timeout, nil)
			state, cause = st.State, st.Cause
			orig := state
			stale := pane != "" && state != "gone" && state != "unavailable" && aliveInOtherPane(agent, pane, env)
			if stale {
				state = "gone"
				cause = ""
			}
			if state == "idle" || state == "done" {
				state = "no-report-yet"
			}
			if state == "unavailable" {
				if rc != 11 {
					rc = 4
				}
				core.Warn(fmt.Sprintf("agent '%s': herdr agent get failed: %s", agent, cause), frictionLogFile, "status")
			} else if !stale && markerExists(sd, agent, "not-received") && orig != "working" && orig != "blocked" && !MarkerSeqChanged(markerRead(sd, agent, "not-received"), st.Seq) {
				state = "not-received"
				if WaitRank(15) > WaitRank(rc) {
					rc = 15
				}
			} else if !stale && markerExists(sd, agent, "queued") && orig != "working" && orig != "blocked" && queuedPromptIsUnresolved(sd, agent, orig, env) {
				state = "not-received"
				if WaitRank(15) > WaitRank(rc) {
					rc = 15
				}
			} else if !stale && orig == "blocked" {
				kindNow := field(fields, 2)
				visible := herdr.AgentRead(env, agent, "visible", intPtr(40))
				if provider.DialogKind(kindNow, visible) == "question" {
					question = provider.QuestionText(visible)
					if WaitRank(7) > WaitRank(rc) {
						rc = 7
					}
				} else {
					p = provider.ProviderDetect(orig, herdr.AgentRead(env, agent, "recent-unwrapped", intPtr(40)))
					if p != nil && p.Status == "provider-error" && p.Auth {
						if WaitRank(14) > WaitRank(rc) {
							rc = 14
						}
					}
				}
			} else if !stale && orig != "working" && orig != "gone" && orig != "blocked" && orig != "unavailable" {
				q := provider.QuotaDetect(orig, herdr.AgentRead(env, agent, "visible", intPtr(20)))
				if q != nil {
					quota = true
					match, renewal = q[0], q[1]
					kind, model, lane = field(fields, 2), field(fields, 8), field(fields, 11)
					if lane == "" {
						lane = core.LaneOfRole(ctx, field(fields, 3), env)
					}
					core.Warn(fmt.Sprintf("quota: agent '%s' lane=%s kind=%s model=%s : %s%s", agent, showOr(lane, "?"), kind, showOr(model, "?"), match, renewalSuffix(renewal)), frictionLogFile, "status")
					rc = 11
				} else {
					p = provider.ProviderDetect(orig, herdr.AgentRead(env, agent, "recent-unwrapped", intPtr(40)))
					if p != nil && WaitRank(14) > WaitRank(rc) {
						rc = 14
					}
				}
			}
		}
		var taskS, activityS any
		if info, e := os.Stat(core.LastReportPath(sd, agent)); e == nil {
			end := platform.Now()
			if reportNonEmpty(report) {
				if reportInfo, e := os.Stat(report); e == nil {
					end = reportInfo.ModTime()
				}
			}
			taskS = math.Round(end.Sub(info.ModTime()).Seconds())
		}
		if state == "working" {
			if age := ActivityAgeSeconds(sd, agent, herdr.AgentRead(env, agent, "visible", nil), platform.Now().Unix()); age != nil {
				activityS = *age
			}
		}
		if quota {
			statusJson(agent, "quota", report, "lane", lane, "kind", kind, "model", model, "match", match, "renewal", renewal, "task_s", taskS, "activity_s", activityS)
		} else if p != nil {
			statusJson(agent, p.Status, report, "cause", p.Cause, "task_s", taskS, "activity_s", activityS)
		} else if question != "" {
			statusJson(agent, "question", report, "question", question, "task_s", taskS, "activity_s", activityS)
		} else {
			taskCol, activityCol := "-", "-"
			if taskS != nil {
				taskCol = fmt.Sprint(taskS)
			}
			if activityS != nil {
				activityCol = fmt.Sprint(activityS)
			}
			if cause != "" {
				fmt.Fprintf(platform.Stdout, "%s\t%s\t%s\t%s\t%s\t%s\n", agent, state, report, cause, taskCol, activityCol)
			} else {
				fmt.Fprintf(platform.Stdout, "%s\t%s\t%s\t%s\t%s\n", agent, state, report, taskCol, activityCol)
			}
		}
	}
	return rc
}

func markerExists(sd, agent, name string) bool {
	_, err := os.Stat(filepath.Join(sd, "wait", agent+"."+name))
	return err == nil
}
func markerRead(sd, agent, name string) string {
	value, _ := platform.ReadTextFile(filepath.Join(sd, "wait", agent+"."+name))
	return value
}
func queuedPromptIsUnresolved(sd, agent, state string, env platform.Env) bool {
	marker := markerRead(sd, agent, "queued")
	visible := herdr.AgentRead(env, agent, "visible", nil)
	recent := herdr.AgentRead(env, agent, "recent-unwrapped", intPtr(40))
	if !QueuedPromptSitsInInput(marker, recent, sd, agent) {
		return false
	}
	if provider.QuotaDetect(state, visible) != nil {
		return false
	}
	return provider.ProviderDetect(state, herdr.AgentRead(env, agent, "recent-unwrapped", intPtr(40))) == nil
}
