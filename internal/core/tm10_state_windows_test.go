//go:build windows

package core

import (
	"testing"
)

func TestTM10RelativeStateWindows(t *testing.T) {
	t.Run(`JS: "relativeStatePath uses Windows separators and rejects sibling paths"`, func(t *testing.T) {
		root, child := `C:\repo`, `C:\repo\state`
		if got := RelativeStatePath(root, child); got != `state` {
			t.Fatalf("child relative=%q", got)
		}
		if got := RelativeStatePath(root, `C:\other`); got != "" {
			t.Fatalf("sibling relative=%q", got)
		}
		if got := StateGitignoreRel(root, child); got != "state" {
			t.Fatalf("gitignore relative=%q", got)
		}
	})
}
