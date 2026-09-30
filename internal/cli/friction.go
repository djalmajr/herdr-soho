package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/stats"
)

type frictionSummaryRow struct {
	count   int
	level   string
	command string
}

func fieldOr(fields []string, i int, fallback string) string {
	if i < len(fields) {
		return fields[i]
	}
	return fallback
}

func frictionLineMatches(since time.Time, hasSince bool, level, command, agent string, fields []string) bool {
	if hasSince {
		ts, ok := stats.ParseSince(fields[0])
		if !ok || ts.Before(since) {
			return false
		}
	}
	if level == "error" {
		if !strings.HasPrefix(fields[1], "error(") {
			return false
		}
	} else if level != "" && fields[1] != level {
		return false
	}
	if command != "" && fields[2] != command {
		return false
	}
	if agent != "" && !strings.Contains(fields[3], "'"+agent+"'") {
		return false
	}
	return true
}

func cmdFriction(argv []string, ctx *core.Config, env platform.Env, cwd string) {
	var (
		sinceRaw, level, command, agent string
		summary                         bool
		filterFlag                      string
	)
	sub, brief := "", ""
	words := []string{}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case sub == "" && (a == "--since" || a == "--level" || a == "--command" || a == "--agent"):
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "--") {
				platform.DieFriction("friction: "+a+" expects a value", 2)
			}
			switch a {
			case "--since":
				sinceRaw = argv[i+1]
			case "--level":
				level = argv[i+1]
			case "--command":
				command = argv[i+1]
			case "--agent":
				agent = argv[i+1]
			}
			if filterFlag == "" {
				filterFlag = a
			}
			i++
		case sub == "" && a == "--summary":
			summary = true
			if filterFlag == "" {
				filterFlag = a
			}
		case a == "--brief":
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "--") {
				platform.DieFriction("friction add: --brief expects a path", 2)
			}
			brief = argv[i+1]
			i++
		case strings.HasPrefix(a, "--"):
			platform.DieFriction("friction: unknown option "+a, 2)
		case sub == "" && len(words) == 0:
			sub = a
		default:
			words = append(words, a)
		}
	}
	if sub != "" && (sinceRaw != "" || level != "" || command != "" || agent != "" || summary) {
		platform.DieFriction("friction: unknown option "+filterFlag, 2)
	}
	sd := core.StateDir(ctx, env, cwd)
	logPath := filepath.Join(sd, "friction.log")
	if sub == "" {
		filtered := sinceRaw != "" || level != "" || command != "" || agent != "" || summary
		if !filtered {
			st, err := os.Stat(logPath)
			if err != nil || st.Size() == 0 {
				fmt.Fprintf(platform.Stdout, "no friction recorded under %s\n", logPath)
				return
			}
			fmt.Fprintf(platform.Stdout, "friction log (%s): timestamp, level, command, message\n", logPath)
			if text, readErr := platform.ReadTextFile(logPath); readErr == nil {
				_, _ = platform.Stdout.Write([]byte(text))
			}
			return
		}
		var since time.Time
		hasSince := sinceRaw != ""
		if hasSince {
			var ok bool
			since, ok = stats.ParseSince(sinceRaw)
			if !ok {
				platform.DieFriction("friction: --since expects AAAA-MM-DD or an ISO date (got '"+sinceRaw+"')", 2)
			}
		}
		if level != "" && level != "warning" && level != "note" && level != "error" {
			platform.DieFriction("friction: --level expects warning, note or error (got '"+level+"')", 2)
		}
		text, readErr := platform.ReadTextFile(logPath)
		if readErr != nil {
			fmt.Fprintf(platform.Stdout, "no friction matches under %s\n", logPath)
			return
		}
		lines := strings.Split(text, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		kept := []string{}
		groups := map[[2]string]int{}
		for _, line := range lines {
			if line == "" {
				continue
			}
			fields := strings.SplitN(line, "\t", 4)
			if len(fields) < 4 {
				if !summary {
					continue
				}
				groups[[2]string{"?", fieldOr(fields, 2, "?")}]++
				continue
			}
			if !frictionLineMatches(since, hasSince, level, command, agent, fields) {
				continue
			}
			if summary {
				groups[[2]string{fields[1], fields[2]}]++
			} else {
				kept = append(kept, line)
			}
		}
		if summary {
			rows := make([]frictionSummaryRow, 0, len(groups))
			for key, count := range groups {
				rows = append(rows, frictionSummaryRow{count: count, level: key[0], command: key[1]})
			}
			sort.Slice(rows, func(i, j int) bool {
				if rows[i].count != rows[j].count {
					return rows[i].count > rows[j].count
				}
				if rows[i].level != rows[j].level {
					return rows[i].level < rows[j].level
				}
				return rows[i].command < rows[j].command
			})
			if len(rows) == 0 {
				fmt.Fprintf(platform.Stdout, "no friction matches under %s\n", logPath)
				return
			}
			fmt.Fprintln(platform.Stdout, "count  level  command")
			for _, row := range rows {
				fmt.Fprintf(platform.Stdout, "%d  %s  %s\n", row.count, row.level, row.command)
			}
			return
		}
		if len(kept) == 0 {
			fmt.Fprintf(platform.Stdout, "no friction matches under %s\n", logPath)
			return
		}
		fmt.Fprintf(platform.Stdout, "friction log (%s): timestamp, level, command, message\n", logPath)
		for _, line := range kept {
			fmt.Fprintln(platform.Stdout, line)
		}
		return
	}
	if sub != "add" {
		platform.DieFriction(fmt.Sprintf("friction: unknown subcommand '%s'", sub), 2)
	}
	message := strings.Join(words, " ")
	if message == "" {
		platform.DieFriction("friction add: give the text to record", 2)
	}
	if brief != "" {
		message += " (brief: " + brief + ")"
	}
	line := fmt.Sprintf("%s\tnote\tfriction\t%s\n", core.NowISO(platform.Now()), core.FrictionSafe(message))
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		platform.DieFriction(fmt.Sprintf("friction add: could not write %s (%v)", logPath, err), 4)
	}
	_, writeErr := f.WriteString(line)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		platform.DieFriction(fmt.Sprintf("friction add: could not write %s", logPath), 4)
	}
	fmt.Fprintln(platform.Stdout, "recorded")
}
