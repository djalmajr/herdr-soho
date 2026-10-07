package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
	herdrtext "github.com/djalmajr/herdr-soho/internal/text"
)

func valueString(v any, fallback string) string {
	if v == nil {
		return fallback
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func field(m any, name string) any {
	if obj, ok := m.(map[string]any); ok {
		return obj[name]
	}
	if obj, ok := m.(*jsonjs.Object); ok {
		v, _ := obj.Get(name)
		return v
	}
	return nil
}
func strictEqual(a, b any) bool {
	return reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.DeepEqual(a, b)
}
func padRight(s string, width int) string {
	n := len(utf16.Encode([]rune(s)))
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}
func cutUTF16(s string, limit int) string {
	u := utf16.Encode([]rune(s))
	if len(u) <= limit {
		return s
	}
	return string(utf16.Decode(u[:limit]))
}

func tabLabelString(label any) string {
	if text, ok := label.(string); ok {
		return cutUTF16(text, 16)
	}
	return cutUTF16(jsonjs.Stringify(label), 16)
}

func rosterScope(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "workspace", true
	}
	if len(argv) == 2 && argv[0] == "--scope" && (argv[1] == "workspace" || argv[1] == "server") {
		return argv[1], true
	}
	return "", false
}

func cmdRoster(argv []string, ctx *core.Config, env platform.Env, cwd string) {
	scope, ok := rosterScope(argv)
	if !ok {
		platform.Die("usage: roster [--scope workspace|server]", 2)
	}
	sd := core.StateDir(ctx, env, cwd)
	live := liveAgentsFriction(env)
	ws := core.WorkspaceID(ctx, env, cwd)
	panes := herdr.PaneList(env, ws)
	tabs := herdr.TabList(env, ws)
	workspaceOf := map[string]string{}
	for _, p := range panes {
		workspaceOf[valueString(field(p, "pane_id"), "")] = ws
	}
	if scope == "server" {
		for _, p := range herdr.PaneListAll(env) {
			workspaceOf[valueString(field(p, "pane_id"), "")] = valueString(field(p, "workspace_id"), "unknown")
		}
	}
	tabOf := func(pane string) string {
		var tab any
		for _, p := range panes {
			if strictEqual(field(p, "pane_id"), pane) {
				tab = field(p, "tab_id")
				break
			}
		}
		if tab == nil {
			return "-"
		}
		var label any
		for _, t := range tabs {
			if strictEqual(field(t, "tab_id"), tab) {
				label = field(t, "label")
				break
			}
		}
		if label == nil || label == false {
			label = tab
		}
		return tabLabelString(label)
	}
	fmt.Fprintf(platform.Stdout, "%s %s %s %s %s %s %s CWD TASK\n", padRight("NAME", 20), padRight("ROLE", 18), padRight("KIND", 8), padRight("PANE", 8), padRight("TAB", 16), padRight("STATE", 9), padRight("REPORT", 16))
	rows := core.RosterRows(sd)
	rosterPanes := map[string]bool{}
	for _, line := range rows {
		f := strings.Split(line, "\t")
		col := func(i int) string {
			if i < len(f) {
				return f[i]
			}
			return ""
		}
		name, pane, kind, role, cwdCol := col(0), col(1), col(2), col(3), col(6)
		if pane != "" {
			rosterPanes[pane] = true
		}
		if name == "" {
			continue
		}
		var agent any
		for _, a := range live {
			if strictEqual(field(a, "name"), name) && (pane == "" || strictEqual(field(a, "pane_id"), pane)) {
				agent = a
				break
			}
		}
		state := valueString(field(agent, "agent_status"), "gone")
		report := "none"
		if reportPath := core.LastReport(sd, name); reportPath != "" {
			if st, err := os.Stat(reportPath); err == nil && st.Size() > 0 {
				report = "ready"
			} else {
				report = "pending"
			}
		}
		if history := col(10); history != "" && history != role {
			candidate := role + " (" + history + ")"
			if len(utf16.Encode([]rune(candidate))) <= 18 {
				role = candidate
			}
		}
		task := "-"
		if raw, err := os.ReadFile(filepath.Join(sd, "task-"+name)); err == nil {
			taskText := herdrtext.ToWellFormedUTF8(string(raw))
			taskText = strings.TrimRight(taskText, "\n")
			if taskText != "" {
				task = taskText
				if len(utf16.Encode([]rune(task))) > 40 {
					task = cutUTF16(task, 39) + "…"
				}
			}
		}
		fmt.Fprintf(platform.Stdout, "%s %s %s %s %s %s %s %s %s\n", padRight(name, 20), padRight(role, 18), padRight(kind, 8), padRight(pane, 8), padRight(tabOf(pane), 16), padRight(state, 9), padRight(report, 16), cwdCol, task)
	}
	groups := map[string][]any{}
	for _, agent := range live {
		p := valueString(field(agent, "pane_id"), "null")
		if rosterPanes[p] {
			continue
		}
		workspace, found := workspaceOf[p]
		if scope == "workspace" && (!found || workspace != ws) {
			continue
		}
		if workspace == "" {
			workspace = "unknown"
		}
		groups[workspace] = append(groups[workspace], agent)
	}
	if scope == "workspace" {
		fmt.Fprintf(platform.Stdout, "\n# other live agents in workspace %s (not spawned by this skill)\n", ws)
	} else {
		fmt.Fprintln(platform.Stdout, "\n# other live agents on this server (not spawned by this skill)")
	}
	keys := make([]string, 0, len(groups))
	for workspace := range groups {
		keys = append(keys, workspace)
	}
	sort.Strings(keys)
	for _, workspace := range keys {
		if scope == "server" {
			fmt.Fprintf(platform.Stdout, "# workspace %s\n", workspace)
		}
		for _, agent := range groups[workspace] {
			p := valueString(field(agent, "pane_id"), "null")
			fmt.Fprintf(platform.Stdout, "%s %s %s %s %s %s\n", padRight(valueString(field(agent, "name"), "-"), 20), padRight("-", 18), padRight(valueString(field(agent, "agent"), "null"), 8), padRight(p, 8), padRight(tabOf(p), 16), padRight(valueString(field(agent, "agent_status"), "null"), 9))
		}
	}
	fmt.Fprintf(platform.Stdout, "\nlayout=%s reuse_workers=%s multi_role=%s auto_approve=%s\n", core.Cfg(ctx, "layout", "split", env), core.Cfg(ctx, "reuse_workers", "on", env), core.Cfg(ctx, "multi_role", "on", env), core.Cfg(ctx, "auto_approve", "off", env))
}
