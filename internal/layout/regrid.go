package layout

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

type gridResult struct {
	Ok      bool
	Changes [][2]string
}

func ratio4(n int) string { return fmt.Sprintf("%.4f", math.Round((1/float64(n))*10000)/10000) }
func moveInTab(pane, tab, split, target, ratio string, env platform.Env) herdr.PaneMoveResult {
	return herdr.PaneMove([]string{pane, "--tab", tab, "--split", split, "--target-pane", target, "--ratio", ratio, "--no-focus"}, env)
}
func buildGrid(tab string, cells []string, env platform.Env) gridResult {
	g := GridSizes(len(cells))
	ids := append([]string(nil), cells...)
	heads := []int{}
	idx := 0
	for _, n := range g.Rows {
		heads = append(heads, idx)
		idx += n
	}
	changes := [][2]string{}
	note := func(i int, m herdr.PaneMoveResult) {
		if m.Pane != "" && m.Pane != ids[i] {
			changes = append(changes, [2]string{ids[i], m.Pane})
			ids[i] = m.Pane
		}
	}
	prev := ids[0]
	for c := 1; c < g.Cols; c++ {
		i := heads[c]
		m := moveInTab(ids[i], tab, "right", prev, ratio4(g.Cols-c+1), env)
		if !m.Ok {
			return gridResult{false, changes}
		}
		note(i, m)
		prev = ids[i]
	}
	for c, n := range g.Rows {
		idx := heads[c]
		prev = ids[idx]
		for j := 1; j < n; j++ {
			idx++
			m := moveInTab(ids[idx], tab, "down", prev, ratio4(n-j+1), env)
			if !m.Ok {
				return gridResult{false, changes}
			}
			note(idx, m)
			prev = ids[idx]
		}
	}
	return gridResult{true, changes}
}
func applyGrid(ctx *core.Config, tab string, cells []string, env platform.Env, cwd string) bool {
	r := buildGrid(tab, cells, env)
	if !r.Ok {
		return false
	}
	dir := core.StateDirPath(ctx, env, cwd)
	for _, change := range r.Changes {
		core.RosterReplacePane(dir, change[0], change[1])
	}
	return true
}
func parkPanes(panes []string, env platform.Env) (bool, string) {
	if len(panes) == 0 {
		return false, ""
	}
	first := panes[0]
	m := herdr.PaneMove([]string{first, "--new-tab", "--label", "herd-park", "--no-focus"}, env)
	if !m.Ok || m.Tab == "" {
		return false, ""
	}
	for _, pane := range panes[1:] {
		r := herdr.PaneMove([]string{pane, "--tab", m.Tab, "--split", "down", "--target-pane", first, "--ratio", "0.5", "--no-focus"}, env)
		if !r.Ok {
			return false, m.Tab
		}
	}
	return true, m.Tab
}

type RegridEntry struct {
	Tab   string `json:"tab"`
	Label string `json:"label"`
	Panes int    `json:"panes"`
	Cols  int    `json:"cols"`
}
type RegridOutput struct {
	Regridded []RegridEntry `json:"regridded"`
}

func dieRegrid(ctx *core.Config, env platform.Env, cwd, message string) {
	platform.DieFriction(message, 4)
}

func relabelHerdTabs(relabel func(), ctx *core.Config, env platform.Env, cwd string) {
	defer func() {
		if recover() != nil {
			core.Warn("regrid: relabel of the herd tabs failed", frictionPath(ctx, env, cwd))
		}
	}()
	relabel()
}

func canPullBack(workerCount, cap int) bool { return workerCount+1 < cap }

func CmdRegrid(_ []string, ctx *core.Config, env platform.Env, cwd string) int {
	dir := core.StateDirPath(ctx, env, cwd)
	ws := core.WorkspaceID(ctx, env, cwd)
	root := platform.ProjectRoot(env, cwd)
	layout := core.Cfg(ctx, "layout", "split", env)
	focusBefore := UIFocusedPane(env)
	live := herdr.PaneList(env, ws)
	summary := []RegridEntry{}
	if layout == "split" && env.Get("HERDR_TAB_ID") != "" && env.Get("HERDR_PANE_ID") != "" {
		callerTab, callerPane := env.Get("HERDR_TAB_ID"), env.Get("HERDR_PANE_ID")
		panes := rosterPanes(ctx, live, callerTab, env, cwd)
		cap := SplitCap(ctx, env)
	outer:
		for _, e := range HerdTabEntries(ctx, env, cwd) {
			if e.Tab == "" {
				continue
			}
			for _, hp := range rosterPanes(ctx, live, e.Tab, env, cwd) {
				if !canPullBack(len(panes), cap) {
					break outer
				}
				m := moveInTab(hp, callerTab, "right", callerPane, "0.5", env)
				if !m.Ok {
					core.Warn(fmt.Sprintf("regrid: could not bring %s back into %s", hp, callerTab), frictionPath(ctx, env, cwd))
					continue
				}
				p := hp
				if m.Pane != "" && m.Pane != hp {
					core.RosterReplacePane(dir, hp, m.Pane)
					p = m.Pane
				}
				panes = append(panes, p)
			}
		}
		live = herdr.PaneList(env, ws)
		if len(panes) >= 1 {
			ok, park := parkPanes(panes, env)
			if !ok {
				dieRegrid(ctx, env, cwd, fmt.Sprintf("regrid: could not park the workers of tab %s in a temporary tab", callerTab))
			}
			if !applyGrid(ctx, callerTab, append([]string{callerPane}, panes...), env, cwd) {
				dieRegrid(ctx, env, cwd, fmt.Sprintf("regrid: a move back into %s failed; remaining workers are alive in tab %s (label herd-park)", callerTab, park))
			}
			summary = append(summary, RegridEntry{callerTab, "caller", len(panes) + 1, GridSizes(len(panes) + 1).Cols})
		}
	}
	kept := ""
	for _, e := range HerdTabEntries(ctx, env, cwd) {
		if e.Tab == "" {
			continue
		}
		label := e.Label
		if label == "-" {
			label = ""
		}
		panes := rosterPanes(ctx, live, e.Tab, env, cwd)
		if len(panes) < 1 {
			continue
		}
		tab := e.Tab
		if len(panes) >= 2 {
			createLabel := label
			if createLabel == "" {
				createLabel = "herd"
			}
			created := herdr.TabCreate(env, ws, root, createLabel)
			if !created.Ok {
				dieRegrid(ctx, env, cwd, "tab create failed")
			}
			newtab := created.Tab
			HerdTabSet(ctx, newtab, label, e.Mode, env, cwd)
			m := moveInTab(panes[0], newtab, "right", created.Root, "0.5", env)
			if !m.Ok {
				dieRegrid(ctx, env, cwd, fmt.Sprintf("regrid: move of %s failed; remaining workers are alive in tab %s", panes[0], e.Tab))
			}
			herdr.PaneClose(created.Root, env)
			if m.Pane != "" && m.Pane != panes[0] {
				core.RosterReplacePane(dir, panes[0], m.Pane)
				panes[0] = m.Pane
			}
			if !applyGrid(ctx, newtab, panes, env, cwd) {
				dieRegrid(ctx, env, cwd, fmt.Sprintf("regrid: a move into %s failed; remaining workers are alive in tab %s", newtab, e.Tab))
			}
			display := label
			if display == "" {
				display = "herd"
			}
			summary = append(summary, RegridEntry{newtab, display, len(panes), GridSizes(len(panes)).Cols})
			tab = newtab
		}
		stored := label
		if stored == "" {
			stored = "-"
		}
		kept += tab + "\t" + stored + "\t" + e.Mode + "\n"
	}
	file := filepath.Join(dir, "herd-tab")
	if _, err := os.Stat(file); err == nil {
		if err := os.WriteFile(file, []byte(kept), 0o666); err != nil {
			panic(err)
		}
	}
	relabelHerdTabs(func() { HerdTabsRelabel(ctx, env, cwd) }, ctx, env, cwd)
	RestoreFocusIfStolen(focusBefore, UIFocusedPane(env), "", env)
	items := make([]any, len(summary))
	for i, entry := range summary {
		items[i] = jsonjs.O("tab", entry.Tab, "label", entry.Label, "panes", entry.Panes, "cols", entry.Cols)
	}
	_, _ = fmt.Fprintf(platform.Stdout, "%s\n", jsonjs.Stringify(jsonjs.O("regridded", items)))
	return 0
}

// AutoRegrid is the exported hook for the later spawn and release Go ports.
func AutoRegrid(ctx *core.Config, env platform.Env, cwd, message string) {
	oldOut, oldErr := platform.Stdout, platform.Stderr
	failed := false
	func() {
		defer func() { platform.Stdout, platform.Stderr = oldOut, oldErr }()
		defer func() {
			if v := recover(); v != nil {
				e, ok := v.(*platform.ExitError)
				if !ok {
					panic(v)
				}
				core.RecordFrictionError(e.Msg, e.Code, frictionPath(ctx, env, cwd))
				failed = true
			}
		}()
		platform.Stdout = discardWriter{}
		platform.Stderr = discardWriter{}
		CmdRegrid(nil, ctx, env, cwd)
	}()
	if failed {
		core.Warn(message, frictionPath(ctx, env, cwd))
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
