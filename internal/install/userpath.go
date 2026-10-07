package install

import (
	"fmt"
	"strings"
)

// Registry kinds the user-PATH seam preserves.
const (
	UserPathREGSZ       = "REG_SZ"
	UserPathREGExpandSZ = "REG_EXPAND_SZ"
)

// UserPathStore is the seam over the OS user-PATH value (on Windows: the
// HKCU Environment Path). Tests inject an in-memory store; production uses
// DefaultUserPathStore, which is platform-specific.
type UserPathStore interface {
	// Read returns the current user PATH value and its registry kind.
	Read() (value string, kind string, err error)
	// Write persists value with the given kind.
	Write(value string, kind string) error
}

// Windows path semantics the user-PATH update assumes: PATH entries are
// joined with ';' and directories carry a trailing '\' when written that
// way. UpdateUserPath is only used for the Windows user PATH.
const (
	windowsPathListSeparator = ";"
	windowsDirSeparator      = "\\"
)

// UpdateUserPath returns the user PATH value with dir appended (joined by
// the Windows list separator) when dir is absent, and the unchanged value
// when dir is already present. It preserves the entire current value
// byte for byte — the legacy installer appends exactly ";"+dir to the
// unchanged current value (or just dir when the value is empty) and never
// rewrites existing content — and presence ignores case and ALL trailing
// directory separators (the legacy TrimEnd('\') on both sides). Existing
// entries are never reordered, deduplicated or rewritten.
func UpdateUserPath(current, dir string) (string, bool) {
	if userPathContains(current, dir) {
		return current, false
	}
	if current == "" {
		return dir, true
	}
	return current + windowsPathListSeparator + dir, true
}

func userPathContains(current, dir string) bool {
	target := strings.TrimRight(dir, windowsDirSeparator)
	for _, entry := range strings.Split(current, windowsPathListSeparator) {
		entry = strings.TrimRight(entry, windowsDirSeparator)
		if entry != "" && strings.EqualFold(entry, target) {
			return true
		}
	}
	return false
}

// UpdateUserPathValue reads the user PATH through the store, appends dir
// when absent, and writes back only when the value actually changed,
// preserving the registry kind (REG_SZ/REG_EXPAND_SZ) and every existing
// entry byte for byte.
func UpdateUserPathValue(store UserPathStore, dir string) (bool, error) {
	current, kind, err := store.Read()
	if err != nil {
		return false, fmt.Errorf("install: user PATH: reading: %w", err)
	}
	next, changed := UpdateUserPath(current, dir)
	if !changed {
		return false, nil
	}
	if err := store.Write(next, kind); err != nil {
		return false, fmt.Errorf("install: user PATH: writing: %w", err)
	}
	return true, nil
}
