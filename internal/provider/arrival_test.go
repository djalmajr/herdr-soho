package provider

import (
	"testing"
)

func TestQueuedPromptEvidenceRules(t *testing.T) {
	// A literal slash path on every host: the screen shows the path as it was sent.
	composed := "/tmp/herdr-soho/ws/reports/build-20260929T121817.brief.md"
	t.Run("rule (a): a line carrying the radical of the composed file is proof", func(t *testing.T) {
		screen := "Read the file …/reports/build-20260929T121817.brief.md in full and execute it\n"
		if !QueuedPromptEvidence(screen, composed) {
			t.Fatalf("screen %q did not prove the queued prompt via the radical", screen)
		}
	})
	t.Run("rule (b): a truncated path of at least 8 characters inside the composed path is proof", func(t *testing.T) {
		for _, screen := range []string{
			"Read the file /tmp/herdr-soho/ws/reports/build-20260929T1…\n",
			"Read the file \"/tmp/herdr-soho/ws/…\n",
			// Cursor's follow-ups box cuts the path at both ends.
			"  follow-ups: Read the file '.../herdr-soho/ws/rep…'\n",
		} {
			if !QueuedPromptEvidence(screen, composed) {
				t.Fatalf("screen %q did not prove the queued prompt via the truncated path", screen)
			}
		}
	})
	t.Run("rule (c): a queue chrome line starting with the arrow or Steering: is proof", func(t *testing.T) {
		for _, screen := range []string{
			"\t↳ Read the file in full and execute it\n",
			"Steering: Read the file …\n",
		} {
			if !QueuedPromptEvidence(screen, composed) {
				t.Fatalf("screen %q did not prove the queued prompt via the queue chrome", screen)
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
	t.Run("a truncated path shorter than 8 characters is not proof", func(t *testing.T) {
		screen := "Read the file /tmp/h…\n"
		if QueuedPromptEvidence(screen, composed) {
			t.Fatalf("short truncated path was treated as proof:\n%s", screen)
		}
	})
}
