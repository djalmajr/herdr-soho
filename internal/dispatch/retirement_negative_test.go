package dispatch

import "testing"

// F9 retirement negative contract: the JavaScript/Bash suite is retired, so
// these native controls keep its frozen negative cases alive. Each case
// mirrors a frozen-suite case that refused a wrong input (the win32 rows are
// the legitimate-equivalence controls the same frozen block pins), and
// asserts the observable result exactly.
func TestRetirementNegativeContracts(t *testing.T) {
	t.Run("SamePath refuses unrelated and POSIX case-different paths", func(t *testing.T) { // JS: "SamePath: win32 case variants equal, different dirs false, POSIX case-sensitive" (scripts/test/dispatch.test.mjs:43-48)
		cases := []struct {
			left, right, goos string
			want              bool
		}{
			{"C:\\work\\repo", "c:/WORK/REPO", "win32", true},     // control: win32 case variants stay equal
			{"", "", "win32", true},                               // control: win32 empty paths are equal
			{"C:\\work\\repo", "C:\\work\\other", "win32", false}, // refuse: different directories
			{"/work/Repo", "/work/repo", "linux", false},          // refuse: POSIX is case-sensitive
			{"/work/Repo", "/work/repo", "darwin", false},         // refuse: POSIX is case-sensitive
			{"/work/Repo/", "/work/REPO", "linux", false},         // refuse: a trailing slash does not fold case
		}
		for _, tc := range cases {
			if got := SamePath(tc.left, tc.right, tc.goos); got != tc.want {
				t.Fatalf("SamePath(%q, %q, %q) = %v; want %v", tc.left, tc.right, tc.goos, got, tc.want)
			}
		}
	})
}
