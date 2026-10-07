package plugin

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/platform"
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
