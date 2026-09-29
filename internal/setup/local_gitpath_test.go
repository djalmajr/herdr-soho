package setup

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSetupGitPathParity(t *testing.T) {
	t.Run("Windows paths from Git and setup targets use native separators", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("Windows filepath semantics require a Windows host")
		}
		root := `C:\Users\test\repo`
		if got, want := setupTargetPath(root, "OTHER.md"), `C:\Users\test\repo\OTHER.md`; got != want {
			t.Fatalf("setup target=%q, want %q", got, want)
		}
		if got, want := setupTargetPath(root, `deep\guide.md`), `C:\Users\test\repo\deep\guide.md`; got != want {
			t.Fatalf("nested setup target=%q, want %q", got, want)
		}
	})

	t.Run("stateDirRelShown uses Git separators for Windows relative state paths", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("Windows filepath semantics require a Windows host")
		}
		root, env, ctx := localPlanFixture(t)
		parts := strings.Split(`a\b`, `\`)
		env["HERDR_SOHO_DIR"] = filepath.Join(append([]string{root}, parts...)...)
		state := classifyLocalState(root, ctx, env, root)
		if state.kind != "inside" || state.rel != "a/b" || state.shown != "a/b/" {
			t.Fatalf("state classification=%+v, want inside a/b/", state)
		}
	})

	t.Run("Git exclude entries convert Windows separators on every host", func(t *testing.T) {
		if got, want := excludeEntry(`deep\cache`), "/deep/cache"; got != want {
			t.Fatalf("Git exclude entry=%q, want %q", got, want)
		}
	})
}
