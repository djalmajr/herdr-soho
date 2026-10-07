package provider

import (
	"testing"
)

func TestQueuedPromptEvidenceRules(t *testing.T) {
	// A literal slash path on every host: the screen shows the path as it was sent.
	composed := "/tmp/herdr-soho/ws/reports/build-20260929T121817.brief.md"
	t.Run("a line showing the full basename or radical with valid boundaries is proof", func(t *testing.T) {
		for _, screen := range []string{
			// The leading directories are clipped; the file's exact name stays whole.
			"Read the file …/reports/build-20260929T121817.brief.md in full and execute it\n",
			"Read the file '.../reports/build-20260929T121817.brief.md' in full and execute it\n",
			// The queue clips the extension after the radical.
			"Read the file '.../reports/build-20260929T121817…' in full and execute it\n",
			"Read the file .../reports/build-20260929T121817.brief.md in full\n",
			// The sentence continues after the quoted path.
			"Read the file '.../reports/build-20260929T121817.brief.md' in full and execute it, then…\n",
		} {
			if !QueuedPromptEvidence(screen, composed) {
				t.Fatalf("screen %q did not prove the queued prompt via the file identity", screen)
			}
		}
	})
	t.Run("a queue chrome line carrying the identity is proof", func(t *testing.T) {
		for _, screen := range []string{
			"\t↳ Read the file '.../reports/build-20260929T121817.brief.md' in full and execute it\n",
			"Steering: Read the file …/reports/build-20260929T121817.brief.md in full\n",
		} {
			if !QueuedPromptEvidence(screen, composed) {
				t.Fatalf("screen %q did not prove the queued prompt via the queue line with the identity", screen)
			}
		}
	})
	t.Run("a common directory fragment of the composed path is not proof", func(t *testing.T) {
		// The old rule (b) accepted any 8+ character fragment of the composed
		// path: these are shared directories, carried by every brief of the
		// project, so an old or unrelated queued line proved the prompt.
		for _, screen := range []string{
			"Read the file \"/tmp/herdr-soho/ws/…\n",
			"Read the file '.../herdr-soho/ws/rep…'\n",
			"Read the file …/reports/\n",
		} {
			if QueuedPromptEvidence(screen, composed) {
				t.Fatalf("a common directory fragment was treated as proof:\n%s", screen)
			}
		}
	})
	t.Run("an incomplete timestamp or name is not proof", func(t *testing.T) {
		for _, screen := range []string{
			"Read the file /tmp/herdr-soho/ws/reports/build-20260929T1…\n",
			"Read the file /tmp/h…\n",
		} {
			if QueuedPromptEvidence(screen, composed) {
				t.Fatalf("an incomplete name was treated as proof:\n%s", screen)
			}
		}
	})
	t.Run("a queue chrome line without the identity is not proof", func(t *testing.T) {
		// The old rule (c) accepted the queue chrome alone, so any old or
		// unrelated queued line proved the prompt.
		for _, screen := range []string{
			"\t↳ Read the file in full and execute it\n",
			"Steering: Read the file …\n",
			"Steering: Read the file /var/folders/f2/r857c16x45z6p82wsq_0d_...\n",
			"↳ Read the file '.../reports/review-20260928T090000.brief.md' in full\n",
		} {
			if QueuedPromptEvidence(screen, composed) {
				t.Fatalf("identity-less queue chrome was treated as proof:\n%s", screen)
			}
		}
	})
	t.Run("a bare Read the file line is not proof", func(t *testing.T) {
		if QueuedPromptEvidence("Read the file\n", composed) {
			t.Fatal("a bare marker line was treated as proof")
		}
	})
	t.Run("another full filename with the same prefix is not proof", func(t *testing.T) {
		for _, screen := range []string{
			"Read the file /tmp/herdr-soho/ws/reports/build-20260929T121817-final.brief.md in full\n",
			"Read the file …/reports/build-20260929T121817.brief.md-later\n",
		} {
			if QueuedPromptEvidence(screen, composed) {
				t.Fatalf("a longer same-prefix filename was treated as proof:\n%s", screen)
			}
		}
	})
	t.Run("a longer name embedding the radical is not proof", func(t *testing.T) {
		screen := "Read the file /tmp/herdr-soho/ws/reports/xbuild-20260929T121817.brief.md in full\n"
		if QueuedPromptEvidence(screen, composed) {
			t.Fatalf("an embedding filename was treated as proof:\n%s", screen)
		}
	})
	t.Run("a clip right before the name is not proof", func(t *testing.T) {
		// The clip could as well have clipped a longer name (xbuild-…).
		screen := "Read the file …build-20260929T121817.brief.md in full\n"
		if QueuedPromptEvidence(screen, composed) {
			t.Fatalf("a clip landing before the name was treated as proof:\n%s", screen)
		}
	})
	t.Run("quoted prose naming another dispatch is not proof", func(t *testing.T) {
		screen := "Steering: \"Read the file /tmp/herdr-soho/ws/reports/review-20260928T090000.brief.md in full and execute it\"\n"
		if QueuedPromptEvidence(screen, composed) {
			t.Fatalf("quoted prose with another filename was treated as proof:\n%s", screen)
		}
	})
	t.Run("a quoted prose line naming the exact file is not proof", func(t *testing.T) {
		// The marker sits mid-sentence: the line quotes the prompt, it is not
		// the queue carrying it, so the exact identity inside the prose never
		// proves the prompt.
		for _, screen := range []string{
			"They asked: \"Read the file …/reports/build-20260929T121817.brief.md in full\"\n",
			"The note says: \"Read the file '.../reports/build-20260929T121817…' in full\"\n",
			"Steering: \"Read the file …/reports/build-20260929T121817.brief.md in full\"\n",
		} {
			if QueuedPromptEvidence(screen, composed) {
				t.Fatalf("quoted prose with the exact identity was treated as proof:\n%s", screen)
			}
		}
	})
	t.Run("a Read the file line outside the last 15 non-empty lines is not proof", func(t *testing.T) {
		screen := "Read the file …/reports/build-20260929T121817.brief.md in full\n"
		for i := 1; i <= 15; i++ {
			screen += "work line " + string(rune('a'+i%26)) + "\n"
		}
		if QueuedPromptEvidence(screen, composed) {
			t.Fatalf("stale queued line was treated as proof:\n%s", screen)
		}
	})
	t.Run("a line with the radical of another dispatch is not proof", func(t *testing.T) {
		screen := "Read the file /tmp/herdr-soho/ws/reports/review-20260928T090000.brief.md in full\n"
		if QueuedPromptEvidence(screen, composed) {
			t.Fatalf("another dispatch's prompt was treated as proof:\n%s", screen)
		}
	})
}

// TestPromptEvidenceWholePathBoundaries keeps the whole-path entrypoint checks
// (the plain line/screen check and the box join) path-boundary aware and
// marker-anchored: a longer file name that only contains the composed path, or
// a quoted prose line that mentions or quotes the marker with a longer or the
// exact file name, is not this prompt's evidence.
func TestPromptEvidenceWholePathBoundaries(t *testing.T) {
	composed := "/tmp/herdr-soho/ws/reports/build-20260929T121817.brief.md"
	cases := []struct {
		name, screen string
		want         bool
	}{
		{"the exact path whole on a line", "Read the file " + composed + " in full and execute it\n", true},
		{"the exact path at the end of the screen", "Read the file " + composed + "\n", true},
		{"a clipped leading directory with the exact path", "Read the file …/reports/build-20260929T121817.brief.md in full\n", true},
		{"the exact path on a chrome line", "Steering: Read the file " + composed + " in full\n", true},
		{"the exact path quoted in prose is not proof", "They asked: \"Read the file " + composed + " in full\"\n", false},
		{"a longer same-prefix filename on a line", "Read the file " + composed + "-other in full and execute it\n", false},
		{"an embedded same-prefix name on a line", "Read the file /tmp/herdr-soho/ws/reports/xbuild-20260929T121817.brief.md in full\n", false},
		{"quoted prose naming a longer same-prefix file", "The note says: \"Read the file " + composed + "-final.brief.md and follow it\"\n", false},
		{"a longer same-prefix filename in the box join", "  ┃  Read the file /tmp/herdr-soho/ws/reports/build-20260929T1\n  ┃  21817.brief.md-other in full\n", false},
		{"the exact path joined back from the box", "  ┃  Read the file /tmp/herdr-soho/ws/reports/build-20260929T1\n  ┃  21817.brief.md in full\n", true},
		{"a box of prose quoting the exact path is not proof", "  ┃  They asked: \"Read the file " + composed + " in full\"\n", false},
		{"prose after an unrelated anchored marker in the box is not proof", "  ┃  Read the file /elsewhere/other.md in full\n  ┃  They asked: \"Read the file " + composed + " in full\"\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PromptEvidence(tc.screen, composed); got != tc.want {
				t.Fatalf("PromptEvidence=%v want %v for %q", got, tc.want, tc.screen)
			}
		})
	}
}
