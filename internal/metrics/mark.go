package metrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

var (
	markFindingValues    = map[string]bool{"real": true, "false": true}
	markMissedSeverities = map[string]bool{"P0": true, "P1": true, "P2": true, "P3": true}
	markAmendmentValues  = map[string]bool{"implementer": true, "brief": true}
)

type markFinding struct {
	n     int
	value string
}

// CmdMark labels a settled report after the orchestrator checked it (issue
// #39, part 2): it appends one {"label":"mark"} line to the same
// metrics.jsonl the wait settled the report into. Marking is an explicit
// action, so it works with metrics=off too. A later mark appends a later
// line: readers apply the lines in file order, and for the same finding the
// last one wins.
func CmdMark(args []string, c CommandContext) int {
	var report string
	var findings []markFinding
	var missed []string
	var amendment string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--finding", "--missed", "--amendment":
			if i+1 >= len(args) {
				core.DieFriction("metrics mark: "+a+" expects a value — "+usageLine, 2, c.FrictionLog, "metrics")
			}
			i++
			value := args[i]
			switch a {
			case "--finding":
				f, ok := parseMarkFinding(value)
				if !ok {
					core.DieFriction("metrics mark: --finding expects <n>=real|false: '"+value+"'", 2, c.FrictionLog, "metrics")
				}
				for _, prev := range findings {
					if prev.n == f.n {
						core.DieFriction("metrics mark: --finding "+strconv.Itoa(f.n)+" is given more than once", 2, c.FrictionLog, "metrics")
					}
				}
				findings = append(findings, f)
			case "--missed":
				if !markMissedSeverities[value] {
					core.DieFriction("metrics mark: --missed expects P0|P1|P2|P3: '"+value+"'", 2, c.FrictionLog, "metrics")
				}
				missed = append(missed, value)
			case "--amendment":
				if !markAmendmentValues[value] {
					core.DieFriction("metrics mark: --amendment expects implementer|brief: '"+value+"'", 2, c.FrictionLog, "metrics")
				}
				amendment = value
			}
		default:
			if strings.HasPrefix(a, "-") {
				core.DieFriction("metrics mark: unknown option "+a+" — "+usageLine, 2, c.FrictionLog, "metrics")
			}
			if report != "" {
				core.DieFriction("metrics mark: one report per command — "+usageLine, 2, c.FrictionLog, "metrics")
			}
			report = a
		}
	}
	if report == "" {
		core.DieFriction("metrics mark: <report> is required — "+usageLine, 2, c.FrictionLog, "metrics")
	}
	if len(findings) == 0 && len(missed) == 0 && amendment == "" {
		core.DieFriction("metrics mark: nothing to mark — "+usageLine, 2, c.FrictionLog, "metrics")
	}
	name := filepath.Base(report)
	// StateDir rejects a state dir inside the skill, like the other state
	// commands, and the mark line goes to this workspace's metrics.jsonl.
	sd := core.StateDir(c.Config, c.Env, c.Cwd)
	line, found := markSettledLine(sd, name)
	if !found {
		core.DieFriction("metrics mark: no metrics line for "+name+" (was metrics on when it settled?)", 2, c.FrictionLog, "metrics")
	}
	findingsCount := markLineFindings(line)
	if len(findings) > 0 && findingsCount < 1 {
		core.DieFriction("metrics mark: --finding only applies to a review line with a findings count; this line has none", 2, c.FrictionLog, "metrics")
	}
	for _, f := range findings {
		if f.n > findingsCount {
			core.DieFriction(fmt.Sprintf("metrics mark: --finding %d is outside 1..%d (the line's findings)", f.n, findingsCount), 2, c.FrictionLog, "metrics")
		}
	}
	if len(missed) > 0 {
		if _, ok := line.Get("verdict"); !ok {
			core.DieFriction("metrics mark: --missed only applies to a review line (one with a verdict); this line is not a review", 2, c.FrictionLog, "metrics")
		}
	}
	if amendment != "" {
		if v, ok := line.Get("amendment"); !ok || v != true {
			core.DieFriction("metrics mark: --amendment only applies to a line the task report lists as an amendment (\"amendment\": true)", 2, c.FrictionLog, "metrics")
		}
	}
	mark := jsonjs.O("ts", platform.Now().UTC().Format(time.RFC3339), "label", "mark", "report", name)
	if len(findings) > 0 {
		findingsObj := jsonjs.O()
		for _, f := range findings {
			findingsObj.Set(strconv.Itoa(f.n), f.value)
		}
		mark.Set("findings", findingsObj)
	}
	if len(missed) > 0 {
		counts := map[string]int{}
		order := []string{}
		for _, severity := range missed {
			if counts[severity] == 0 {
				order = append(order, severity)
			}
			counts[severity]++
		}
		missedObj := jsonjs.O()
		for _, severity := range order {
			missedObj.Set(severity, counts[severity])
		}
		mark.Set("missed", missedObj)
	}
	if amendment != "" {
		mark.Set("amendment", amendment)
	}
	lineOut := jsonjs.Stringify(mark)
	core.WithRosterLock(sd, func() {
		f, err := os.OpenFile(filepath.Join(sd, "metrics.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			panic(err)
		}
		_, werr := f.WriteString(lineOut + "\n")
		cerr := f.Close()
		if werr != nil {
			err = werr
		} else if cerr != nil {
			err = cerr
		}
		if err != nil {
			core.DieFriction("metrics mark: could not append the mark line to metrics.jsonl: "+err.Error(), 2, c.FrictionLog, "metrics")
		}
	})
	_, _ = fmt.Fprintln(platform.Stdout, lineOut)
	return 0
}

func parseMarkFinding(value string) (markFinding, bool) {
	which, label, ok := strings.Cut(value, "=")
	if !ok || which == "" || label == "" {
		return markFinding{}, false
	}
	n, err := strconv.Atoi(which)
	if err != nil || n < 1 || !markFindingValues[label] {
		return markFinding{}, false
	}
	return markFinding{n: n, value: label}, true
}

// markSettledLine finds the settled (label-less) metrics line whose report
// field is name: the metrics-recorded marker settles a report name once, so
// the first settled line with the name is the line the report settled into.
func markSettledLine(sd, name string) (*jsonjs.Object, bool) {
	raw, err := os.ReadFile(filepath.Join(sd, "metrics.jsonl"))
	if err != nil {
		return nil, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		value, err := jsonjs.Parse([]byte(line))
		if err != nil {
			continue
		}
		obj, ok := value.(*jsonjs.Object)
		if !ok {
			continue
		}
		if _, hasLabel := obj.Get("label"); hasLabel {
			continue
		}
		report, ok := obj.Get("report")
		if ok && report == name {
			return obj, true
		}
	}
	return nil, false
}

func markLineFindings(line *jsonjs.Object) int {
	value, ok := line.Get("findings")
	if !ok {
		return 0
	}
	count, ok := value.(float64)
	if !ok || count < 0 {
		return 0
	}
	return int(count)
}
