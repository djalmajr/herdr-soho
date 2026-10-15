// Name oracle for the frozen legacy parity goldens.
//
// The frozen goldens (internal/testdata/legacy) predate the name-layer
// switch to soho: their expected outputs name the user config
// herdr-soho/config, the project file .agents/herdr-soho.conf, the state
// directory .herdr-soho and the environment prefix HERDR_SOHO_, which the
// Go implementation now emits as soho/config, .agents/soho.conf, .soho and
// SOHO_ (the legacy herdr-soho names are read through the legacy bridge).
// This adapter rewrites only those frozen name strings to the Go names so
// the frozen expectations compare against the Go behavior. The pre-rename
// slice also renamed user-visible message names (the soho launcher,
// the wait/roles suggestions, the dispatch scratch paths under TMPDIR
// and the Herdr report-metadata source); its anchored pairs are listed
// below with the same guarantee. Every rewrite is
// an exact, anchored substring: the setup-instruction block (still frozen
// as herdr-soho text, rewritten by a later slice) names the same files
// and must flow through the goldens unchanged, which the anchors guarantee
// (the block writes ".agents/herdr-soho.conf" after "(" and ".herdr-soho/"
// after a backtick or "(", never after "/" or a newline). Seeds, unrelated
// fields, ordering and every other frozen expectation pass through
// byte-identical.
package testutil

import (
	"strings"
)

// sohoNamePairs rewrites the frozen names in the expected order: the more
// specific anchored forms first, the generic ones after.
var sohoNamePairs = [][2]string{
	{"state dir ignored: .herdr-soho/", "state dir ignored: .soho/"},
	{"state_dir          .herdr-soho                    defaults", "state_dir          .soho                          defaults"},
	{".herdr-soho/ws/", ".soho/ws/"},
	{".herdr-soho/\n", ".soho/\n"},
	{"below flags and HERDR_SOHO_*)", "below flags and SOHO_*)"},
	{"HERDR_SOHO_<KEY>", "SOHO_<KEY>"},
	{"/.agents/herdr-soho.conf", "/.agents/soho.conf"},
	{"herdr-soho/config", "soho/config"},
	// User-visible message renames of the pre-rename slice (the CLI,
	// the launcher and the wait/roles suggestions now carry the soho
	// name; the frozen goldens still name the pre-rename binary). Anchored
	// pairs, as above.
	{"(run: herdr-soho roles)", "(run: soho roles)"},
	{"Run: herdr-soho wait ", "Run: soho wait "},
	{"wait again: herdr-soho wait ", "wait again: soho wait "},
	{"tmp/herdr-soho", "tmp/soho"},
	{"--source herdr-soho", "--source soho"},
	{"herdr-soho runs a small team", "soho runs a small team"},
}

// RewriteSohoName rewrites the frozen name-layer strings of one parity
// expectation to the Go soho names. It is a no-op for strings without any
// of the anchored forms, including the setup-instruction block.
func RewriteSohoName(s string) string {
	for _, pair := range sohoNamePairs {
		s = strings.ReplaceAll(s, pair[0], pair[1])
	}
	return s
}

// ApplySohoNameOracle recursively walks a decoded JSON value tree
// (objects, arrays and scalar strings) and rewrites every string through
// RewriteSohoName. Non-string values and strings without a frozen name
// pass through unchanged.
func ApplySohoNameOracle(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = ApplySohoNameOracle(item)
		}
		return v
	case []any:
		for i := range v {
			v[i] = ApplySohoNameOracle(v[i])
		}
		return v
	case string:
		return RewriteSohoName(v)
	default:
		return value
	}
}
