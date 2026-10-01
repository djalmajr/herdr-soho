package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

const rosterHeader = "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n"

var currentFrictionCommand string

func SetFrictionCommand(command string) { currentFrictionCommand = command }

func NowStamp(t time.Time) string { return t.Format("20060102T150405") }
func NowISO(t time.Time) string   { return t.Format("2006-01-02T15:04:05") }

func FrictionSafe(value string) string {
	var out strings.Builder
	separator := false
	for _, r := range value {
		if r == '\r' || r == '\n' || r == '\t' {
			if !separator {
				out.WriteByte(' ')
			}
			separator = true
			continue
		}
		separator = false
		out.WriteRune(r)
	}
	return out.String()
}

func frictionLog(file, fallbackCommand, level, message string) {
	if file == "" {
		return
	}
	command := currentFrictionCommand
	if command == "" {
		command = fallbackCommand
	}
	if command == "" {
		command = "?"
	}
	line := fmt.Sprintf("%s\t%s\t%s\t%s\n", NowISO(platform.Now()), level, FrictionSafe(command), FrictionSafe(message))
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

func Warn(message, logFile string, fallbackCommand ...string) {
	_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: warning: %s\n", message)
	legacyCommand := ""
	if len(fallbackCommand) > 0 {
		legacyCommand = fallbackCommand[0]
	}
	frictionLog(logFile, legacyCommand, "warning", message)
}

func DieFriction(message string, code int, _, _ string) {
	platform.DieFriction(message, code)
}

func RecordFrictionError(message string, code int, logFile string, fallbackCommand ...string) {
	legacyCommand := ""
	if len(fallbackCommand) > 0 {
		legacyCommand = fallbackCommand[0]
	}
	frictionLog(logFile, legacyCommand, fmt.Sprintf("error(exit %d)", code), message)
}

func WorkspaceID(ctx *Config, env platform.Env, cwd string) string {
	if id := env.Get("HERDR_WORKSPACE_ID"); id != "" {
		return id
	}
	r := platform.RunCli("herdr", []string{"pane", "current", "--current"}, platform.RunOptions{Env: env, TimeoutMs: 30_000})
	if r.NotFound {
		platform.Die("herdr CLI not found in PATH", 2)
	}
	if r.Status == nil || *r.Status != 0 {
		if r.Stdout != "" {
			_, _ = platform.Stdout.Write([]byte(r.Stdout))
		}
		if r.Stderr != "" {
			_, _ = platform.Stderr.Write([]byte(r.Stderr))
		}
		code := 1
		if r.Status != nil {
			code = *r.Status
		}
		platform.Die("", code)
	}
	v, err := jsonjs.Parse([]byte(r.Stdout))
	if err != nil {
		platform.Die("", 2)
	}
	root, ok := v.(*jsonjs.Object)
	if !ok {
		platform.Die("herdr pane current returned a non-object result", 2)
	}
	result, _ := root.Get("result")
	resultObject, ok := result.(*jsonjs.Object)
	if !ok {
		platform.Die("herdr pane current returned a non-object result", 2)
	}
	pane, _ := resultObject.Get("pane")
	paneObject, ok := pane.(*jsonjs.Object)
	if !ok {
		platform.Die("herdr pane current returned a non-object pane", 2)
	}
	id, ok := paneObject.Get("workspace_id")
	if !ok || id == nil {
		return "null"
	}
	if s, ok := id.(string); ok {
		return s
	}
	switch id.(type) {
	case *jsonjs.Object, []any:
		platform.Die("herdr pane current returned an unsupported workspace_id type", 2)
	}
	return jsString(id)
}

func jsString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			if item != nil {
				parts[i] = jsString(item)
			}
		}
		return strings.Join(parts, ",")
	case *jsonjs.Object:
		return "[object Object]"
	default:
		return jsonjs.Stringify(value)
	}
}

func StateDirPath(ctx *Config, env platform.Env, cwd string) string {
	return filepath.Join(StateRoot(ctx, env, cwd), WorkspaceID(ctx, env, cwd))
}

func StateDir(ctx *Config, env platform.Env, cwd string) string {
	if !Nowrite(env) {
		if skill := StateInSkill(ctx, env, cwd); skill != "" {
			platform.Die(fmt.Sprintf("the state dir '%s' would be inside the herdr-soho skill ('%s'); run herdr-soho from the project's directory (nothing was written)", StateDirPath(ctx, env, cwd), skill), 2)
		}
	}
	dir := StateDirPath(ctx, env, cwd)
	if Nowrite(env) {
		return dir
	}
	for _, name := range []string{"briefs", "reports", "wait"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o777); err != nil {
			panic(err)
		}
	}
	roster := filepath.Join(dir, "agents.tsv")
	if _, err := os.Stat(roster); os.IsNotExist(err) {
		if err := os.WriteFile(roster, []byte(rosterHeader), 0o666); err != nil {
			panic(err)
		}
	}
	return dir
}

func tsvLines(dir string) []string {
	raw, err := platform.ReadTextFile(filepath.Join(dir, "agents.tsv"))
	if err != nil {
		return []string{}
	}
	lines := strings.Split(raw, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func RosterRows(dir string) []string {
	out := []string{}
	for _, line := range tsvLines(dir) {
		if !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

func RosterLine(dir, name string) string {
	found := ""
	for _, line := range RosterRows(dir) {
		fields := strings.Split(line, "\t")
		if fields[0] == name {
			found = line
		}
	}
	return found
}

func WithRosterLock(dir string, fn func()) {
	lock := filepath.Join(dir, "agents.lock")
	tries := 0
	for {
		err := os.Mkdir(lock, 0o700)
		if err == nil {
			break
		}
		removed := false
		if info, statErr := os.Lstat(lock); statErr == nil && platform.Now().Sub(info.ModTime()) > time.Minute && info.IsDir() {
			if os.Remove(lock) == nil {
				removed = true
			}
		}
		if removed {
			continue
		}
		tries++
		if tries >= 200 {
			platform.DieFriction(fmt.Sprintf("roster lock %s held for too long; remove it if no herdr-soho command is running", lock), 4)
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer func() { _ = os.Remove(lock) }()
	fn()
}

func RosterAppend(dir string, fields []string) {
	WithRosterLock(dir, func() {
		f, err := os.OpenFile(filepath.Join(dir, "agents.tsv"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			panic(err)
		}
		_, err = f.WriteString(strings.Join(fields, "\t") + "\n")
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			panic(err)
		}
	})
}

func atomicRoster(file string, lines []string) {
	content := ""
	if len(lines) != 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	if err := platform.AtomicWrite(file, content); err != nil {
		panic(err)
	}
}

func RosterRemove(dir, name string) {
	WithRosterLock(dir, func() {
		lines := tsvLines(dir)
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			if strings.Split(line, "\t")[0] != name {
				out = append(out, line)
			}
		}
		atomicRoster(filepath.Join(dir, "agents.tsv"), out)
	})
}

func RosterSetRole(dir, name, role string) {
	WithRosterLock(dir, func() {
		lines := tsvLines(dir)
		for i, line := range lines {
			f := strings.Split(line, "\t")
			if f[0] != name {
				continue
			}
			previous := ""
			if len(f) > 3 {
				previous = f[3]
			}
			history := ""
			if len(f) >= 11 {
				history = f[10]
			}
			if history == "" {
				history = previous
			} else if !contains(strings.Split(history, ","), previous) {
				history += "," + previous
			}
			if previous != role {
				if history == "" {
					history = role
				} else {
					history += "," + role
				}
			}
			for len(f) < 11 {
				f = append(f, "")
			}
			f[3], f[10] = role, history
			lines[i] = strings.Join(f, "\t")
		}
		atomicRoster(filepath.Join(dir, "agents.tsv"), lines)
	})
}

func RosterReplacePane(dir, oldPane, newPane string) {
	if oldPane == newPane || newPane == "" {
		return
	}
	WithRosterLock(dir, func() {
		lines := tsvLines(dir)
		for i, line := range lines {
			f := strings.Split(line, "\t")
			if len(f) > 1 && f[1] == oldPane {
				f[1] = newPane
			}
			lines[i] = strings.Join(f, "\t")
		}
		atomicRoster(filepath.Join(dir, "agents.tsv"), lines)
	})
}

func LastReportPath(dir, agent string) string { return filepath.Join(dir, "last-report-"+agent) }
func LastReport(dir, agent string) string {
	raw, err := platform.ReadTextFile(LastReportPath(dir, agent))
	if err != nil {
		return ""
	}
	return strings.TrimRight(raw, "\n")
}
