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

	// Recorded calls.
	CreateCalls []fakeHerdrCreateCall
	CloseIDs    []string
	ListCalls   int
	RunCalls    []fakeHerdrRunCall
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
	return f.CloseErr
}

func (f *fakeHerdr) ServerReachable() bool {
	f.ListCalls++
	return f.Reachable
}

func (f *fakeHerdr) Run(paneID string, argv []string) error {
	f.RunCalls = append(f.RunCalls, fakeHerdrRunCall{PaneID: paneID, Argv: append([]string(nil), argv...)})
	return f.RunErr
}
