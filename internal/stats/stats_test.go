package stats

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestToFixedOneMatchesNodeFixture(t *testing.T) {
	// Mutation captured: formatting with fmt("%.1f") keeps -0.0 and rounds 2.25 to 2.2.
	data, err := os.ReadFile("testdata/rounding.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input        string
		Fixed        string
		Rounded      float64
		NegativeZero bool
		Rendered     string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Input, func(t *testing.T) {
			value, err := strconv.ParseFloat(tc.Input, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := toFixedOne(value); got != tc.Fixed {
				t.Fatalf("toFixedOne(%s)=%q want %q", tc.Input, got, tc.Fixed)
			}
			rounded := round1(value)
			negativeZero := rounded == 0 && math.Signbit(rounded)
			if rounded != tc.Rounded || negativeZero != tc.NegativeZero {
				t.Fatalf("round1(%s)=%v negativeZero=%t want %v negativeZero=%t", tc.Input, rounded, negativeZero, tc.Rounded, tc.NegativeZero)
			}
			if got := toFixedOne(rounded); got != tc.Rendered {
				t.Fatalf("render round1(%s)=%q want %q", tc.Input, got, tc.Rendered)
			}
			oldStdout := platform.Stdout
			var output bytes.Buffer
			platform.Stdout = &output
			printStatsText(map[string]*aggregate{"tasker": {minutes: []float64{value}}}, map[string]*review{}, false, "")
			platform.Stdout = oldStdout
			rows := strings.Split(output.String(), "\n")
			if len(rows) < 3 || strings.Contains(rows[2], "-0.0") || strings.Count(rows[2], tc.Rendered) != 3 {
				t.Fatalf("stats text row for %s = %q; want three %q fields", tc.Input, output.String(), tc.Rendered)
			}
		})
	}
}

func TestStatsJSONPreservesRoleInsertionOrder(t *testing.T) {
	// Mutation captured: sorting group keys changes JavaScript's first-seen role order.
	groups := map[string]*aggregate{}
	groupOrder := []string{}
	groupFor(groups, &groupOrder, "reviewer")
	groupFor(groups, &groupOrder, "implementer")
	groupFor(groups, &groupOrder, "reviewer")
	oldStdout := platform.Stdout
	defer func() { platform.Stdout = oldStdout }()
	var output bytes.Buffer
	platform.Stdout = &output
	printStatsJSON(groups, map[string]*review{}, false, "", groupOrder)
	got := output.String()
	reviewer := strings.Index(got, `"roles":{"reviewer":`)
	implementer := strings.Index(got, `"implementer":`)
	if reviewer < 0 || implementer <= reviewer {
		t.Fatalf("default stats JSON role order = %s; want reviewer before implementer", got)
	}

	output.Reset()
	printStatsJSON(groups, map[string]*review{}, true, "role", groupOrder)
	got = output.String()
	implementer = strings.Index(got, `"groups":{"implementer":`)
	reviewer = strings.Index(got, `"reviewer":`)
	if implementer < 0 || reviewer <= implementer {
		t.Fatalf("--by stats JSON group order = %s; want sorted implementer before reviewer", got)
	}
}
