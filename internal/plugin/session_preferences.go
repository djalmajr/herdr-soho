package plugin

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/soho/internal/platform"
)

const sessionViewPreferenceLimit = 32

// Native popups share one user preference across projects and workspaces.
// The pure state constructors and non-TTY contracts remain filesystem-free.
func newSessionState(env platform.Env, platformName string, terminal bool) *BoardState {
	state := NewBoardState()
	if terminal {
		state.ViewState.Enabled = true
		state.viewPreferencePath = filepath.Join(filepath.Dir(platform.UserConfigPath(platformName, env)), "session-view")
		state.ViewState.View = loadSessionView(state.viewPreferencePath)
		state.teamViewPreferencePath = filepath.Join(filepath.Dir(platform.UserConfigPath(platformName, env)), "team-view")
		state.teamViewForm = loadTeamViewForm(state.teamViewPreferencePath)
	}
	return state
}

func loadSessionView(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > sessionViewPreferenceLimit {
		return sessionViewTree
	}
	file, err := os.Open(path)
	if err != nil {
		return sessionViewTree
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, sessionViewPreferenceLimit+1))
	if err != nil || len(data) > sessionViewPreferenceLimit {
		return sessionViewTree
	}
	switch view := strings.TrimSpace(string(data)); view {
	case sessionViewTree, sessionViewTable:
		return view
	default:
		return sessionViewTree
	}
}

func (s *BoardState) rememberSessionView() {
	if s.viewPreferencePath == "" {
		return
	}
	err := os.MkdirAll(filepath.Dir(s.viewPreferencePath), 0o700)
	if err == nil {
		err = platform.AtomicWrite(s.viewPreferencePath, s.ViewState.View+"\n")
	}
	s.viewPreferenceFailed = err != nil
}

// loadTeamViewForm reads the team view's form preference (the tree or
// the table, the way loadSessionView reads the session view's): a
// missing, unreadable or other value reads as the tree.
func loadTeamViewForm(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > sessionViewPreferenceLimit {
		return teamFormTree
	}
	file, err := os.Open(path)
	if err != nil {
		return teamFormTree
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, sessionViewPreferenceLimit+1))
	if err != nil || len(data) > sessionViewPreferenceLimit {
		return teamFormTree
	}
	switch form := strings.TrimSpace(string(data)); form {
	case teamFormTree, teamFormTable:
		return form
	default:
		return teamFormTree
	}
}

// rememberTeamViewForm persists the team view's form preference (the
// tree or the table plus a newline, in the team-view file beside the
// session-view one): a write failure is recorded in the view's own
// flag (nothing displays it - the list's message is the list's own
// preference's) and interrupts nothing, like the session view's.
func (s *BoardState) rememberTeamViewForm() {
	if s.teamViewPreferencePath == "" {
		return
	}
	err := os.MkdirAll(filepath.Dir(s.teamViewPreferencePath), 0o700)
	if err == nil {
		err = platform.AtomicWrite(s.teamViewPreferencePath, s.teamViewForm+"\n")
	}
	s.teamViewPreferenceFailed = err != nil
}
