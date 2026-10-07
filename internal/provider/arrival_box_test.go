package provider

import "testing"

func TestPromptEvidenceJoinsTheBoxWrappedPath(t *testing.T) {
	composed := "/work/state/ws/briefs/worker-20260930T125455.md"
	wrapped := func(stamp string) string {
		return "  ┃  Read the file /work/state/ws/briefs/worker-\n" +
			"  ┃  " + stamp + ".md in full and execute it. When\n"
	}
	cases := []struct {
		name, screen string
		want         bool
	}{
		{"the path whole on a line", "Read the file " + composed + " in full\n", true},
		{"the box-wrapped path joined back", wrapped("20260930T125455"), true},
		{"a box that wrapped at a space", "  ┃  Read the file /work/state/ws/briefs/worker-2026\n  ┃  0930T125455.md in full\n", true},
		{"an earlier brief of the same agent in the box", wrapped("19990101T000000"), false},
		{"the shared prefix alone in the box", "  ┃  Read the file /work/state/ws/briefs/worker-\n", false},
		{"a truncated queue line with an incomplete name outside a box is not proof", "  ↳ Read the file /work/state/ws/briefs/worker-2026…\n", false},
		{"a queue line with the file identity outside a box is proof", "  ↳ Read the file …/briefs/worker-20260930T125455.md…\n", true},
		{"an empty screen", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PromptEvidence(tc.screen, composed); got != tc.want {
				t.Fatalf("PromptEvidence=%v want %v for %q", got, tc.want, tc.screen)
			}
		})
	}
}
