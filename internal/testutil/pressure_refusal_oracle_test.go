package testutil

import (
	"strings"
	"testing"
)

func TestPressureRefusalOracleRowFormat(t *testing.T) {
	// The frozen anchor and the two inserted lines match the row format of
	// soho config (key padded to 18, value padded to 30, single-space
	// columns): a row-format or default drift fails here and in the parity
	// comparison.
	if pressureRefusalAnchor != "pressure_swap_percent"+" "+"80"+strings.Repeat(" ", 28)+" "+"defaults" {
		t.Fatalf("anchor drifted: %q", pressureRefusalAnchor)
	}
	if !strings.Contains(pressureRefusalInsert,
		"pressure_refuse_disk_free_percent"+" "+"5"+strings.Repeat(" ", 29)+" "+"defaults"+"\n") {
		t.Fatalf("disk refusal line drifted: %q", pressureRefusalInsert)
	}
	if !strings.HasSuffix(pressureRefusalInsert,
		"pressure_refuse_swap_percent"+" "+"95"+strings.Repeat(" ", 28)+" "+"defaults") {
		t.Fatalf("swap refusal line drifted: %q", pressureRefusalInsert)
	}
}

func TestPressureRefusalOracleInsertsTheTwoListingLines(t *testing.T) {
	in := "metrics            off                            defaults\n" +
		pressureRefusalAnchor + "\n" +
		"effort.cursor      xhigh                          defaults\n"
	want := "metrics            off                            defaults\n" +
		pressureRefusalInsert + "\n" +
		"effort.cursor      xhigh                          defaults\n"
	if got := RewritePressureRefusal(in); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestPressureRefusalOraclePassesThroughWithoutTheAnchor(t *testing.T) {
	in := "metrics            off                            defaults\n" +
		"effort.cursor      xhigh                          defaults\n"
	if got := RewritePressureRefusal(in); got != in {
		t.Fatalf("a string without the anchor changed: %q", got)
	}
	if got := RewritePressureRefusal(""); got != "" {
		t.Fatalf("the empty string changed: %q", got)
	}
}

func TestPressureRefusalOracleAdaptsEveryOccurrence(t *testing.T) {
	in := pressureRefusalAnchor + "\n" + pressureRefusalAnchor + "\n"
	if got := RewritePressureRefusal(in); strings.Count(got, "pressure_refuse_disk_free_percent") != 2 {
		t.Fatalf("both occurrences must be adapted: %q", got)
	}
}

func TestApplyPressureRefusalOracleWalksJSONTrees(t *testing.T) {
	value := map[string]any{"out": pressureRefusalAnchor + "\n", "n": 2}
	got, ok := ApplyPressureRefusalOracle(value).(map[string]any)
	if !ok {
		t.Fatalf("the map came back as %T", value)
	}
	if out, _ := got["out"].(string); !strings.Contains(out, "pressure_refuse_disk_free_percent") {
		t.Fatalf("the nested string was not adapted: %q", out)
	}
	if got["n"] != 2 {
		t.Fatalf("the non-string value changed: %v", got["n"])
	}
}
