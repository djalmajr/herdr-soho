package job

// fakeHerdr is the test-only in-memory fake for herdrCLI. It records every
// call and returns the configured results, so later slices' tests exercise
// the job against both interfaces without a subprocess.
var (
	_ herdrWorkspaces = (*fakeHerdr)(nil)
	_ herdrPanes      = (*fakeHerdr)(nil)
)

// fakeHerdrCreateCall is one recorded Create call.
type fakeHerdrCreateCall struct {
	Cwd   string
	Label string
	Env   map[string]string
}

// fakeHerdrRunCall is one recorded Run call; Argv is a copy.
type fakeHerdrRunCall struct {
	PaneID string
	Argv   []string
}

type fakeHerdr struct {
	// Configurable results.
	CreateWorkspaceID string
	CreateRootPaneID  string
	CreateErr         error
	CloseErr          error
	Reachable         bool
	RunErr            error
	Workspaces        []herdrWorkspace
	ListErr           error
	// PanesRows, when set, is the pane list returned by Panes (a copy);
	// PanesErr fails every Panes call; PanesFn, when set, overrides both
	// with one scripted list per call, indexed by the 1-based call number
	// (nil is an empty list). The default is an empty list and no error: no
	// foreign pane, so the finalize close watch closes at the first
	// observation.
	PanesRows []herdrPane
	PanesErr  error
	PanesFn   func(call int) []herdrPane
	// OnRun, when set, runs after a Run is recorded, before its result is
	// returned: start tests use it to stand in for the launched supervisor
	// (an implementation-generated confirmation, never evidence of a real
	// Herdr pane).
	OnRun func(paneID string, argv []string)

	// Recorded calls.
	CreateCalls  []fakeHerdrCreateCall
	CloseIDs     []string
	ListCalls    int
	ListReads    int
	RunCalls     []fakeHerdrRunCall
	PanesCalls   int
	PanesCallIDs []string
}

func (f *fakeHerdr) Create(cwd, label string, env map[string]string) (string, string, error) {
	f.CreateCalls = append(f.CreateCalls, fakeHerdrCreateCall{Cwd: cwd, Label: label, Env: env})
	if f.CreateErr != nil {
		return "", "", f.CreateErr
	}
	return f.CreateWorkspaceID, f.CreateRootPaneID, nil
}

func (f *fakeHerdr) Close(workspaceID string) error {
	f.CloseIDs = append(f.CloseIDs, workspaceID)
	if f.CloseErr != nil {
		return f.CloseErr
	}
	// A successful close takes the workspace out of the list, as Herdr's
	// own list does.
	kept := f.Workspaces[:0]
	for _, ws := range f.Workspaces {
		if ws.ID != workspaceID {
			kept = append(kept, ws)
		}
	}
	f.Workspaces = kept
	return nil
}

// Panes lists the workspace's panes: the finalize close watch
// type-asserts the fake for this method. By default it returns no pane
// and no error (no foreign pane: the watch closes at the first
// observation); PanesErr fails every call, and PanesFn scripts one list
// per call. PanesCalls counts the calls and PanesCallIDs records the
// queried workspace ids.
func (f *fakeHerdr) Panes(workspaceID string) ([]herdrPane, error) {
	f.PanesCalls++
	f.PanesCallIDs = append(f.PanesCallIDs, workspaceID)
	if f.PanesErr != nil {
		return nil, f.PanesErr
	}
	if f.PanesFn != nil {
		return f.PanesFn(f.PanesCalls), nil
	}
	return append([]herdrPane(nil), f.PanesRows...), nil
}

func (f *fakeHerdr) ServerReachable() bool {
	f.ListCalls++
	return f.Reachable
}

// List returns a copy of the configured rows; ListReads counts the calls
// (ListCalls counts ServerReachable).
func (f *fakeHerdr) List() ([]herdrWorkspace, error) {
	f.ListReads++
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return append([]herdrWorkspace(nil), f.Workspaces...), nil
}

func (f *fakeHerdr) Run(paneID string, argv []string) error {
	f.RunCalls = append(f.RunCalls, fakeHerdrRunCall{PaneID: paneID, Argv: append([]string(nil), argv...)})
	if f.OnRun != nil {
		f.OnRun(paneID, argv)
	}
	return f.RunErr
}
