package herdr

import (
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// PaneCurrentResult is the validated transport outcome of
// `herdr pane current --current`: the calling pane's identity as the
// native server reports it. Ok is set only when the CLI call succeeded
// and result.pane is an object; the identity fields are exposed exactly
// as reported (empty when absent). This is transport, not policy: no
// workspace scope or live-topology validation is applied here — that
// decision belongs to the caller.
type PaneCurrentResult struct {
	Ok          bool
	PaneID      string
	TabID       string
	WorkspaceID string
}

// PaneCurrent resolves the calling pane's actual identity through the
// native Herdr CLI. The caller context (HERDR_*) inherited by a moved
// process keeps resolving the pane's old id for that process; this
// channel reports the current pane, tab, and workspace instead. Like the
// other transport reads in this package it never dies: a transport
// failure returns Ok=false, and a well-formed call exposes the fields it
// carries.
func PaneCurrent(env platform.Env) PaneCurrentResult {
	r := run([]string{"pane", "current", "--current"}, env)
	if !ok(r) {
		return PaneCurrentResult{}
	}
	v, err := jsonjs.Parse([]byte(r.Stdout))
	if err != nil {
		return PaneCurrentResult{}
	}
	paneValue, valid := jqPath(v, "result", "pane")
	if !valid {
		return PaneCurrentResult{}
	}
	pane, isObj := paneValue.(*jsonjs.Object)
	if !isObj {
		return PaneCurrentResult{}
	}
	return PaneCurrentResult{
		Ok:          true,
		PaneID:      str(get(pane, "pane_id")),
		TabID:       str(get(pane, "tab_id")),
		WorkspaceID: str(get(pane, "workspace_id")),
	}
}
