package plugin

import "testing"

func TestUnifiedFilterCyclesRepeat(t *testing.T) {
	s := unifiedTestState()
	for lap := 0; lap < 3; lap++ {
		for _, want := range []string{sessionScopeAgents, sessionScopeTerminals, sessionScopeAll} {
			s.ApplyKey("f")
			if s.ViewState.Scope != want {
				t.Fatalf("scope lap %d: got %q want %q", lap, s.ViewState.Scope, want)
			}
		}
		for _, want := range []string{sessionStatusWorking, sessionStatusBlocked, sessionStatusIdle, sessionStatusDone, sessionStatusUnknown, sessionStatusAll} {
			s.ApplyKey("t")
			if s.ViewState.Status != want {
				t.Fatalf("status lap %d: got %q want %q", lap, s.ViewState.Status, want)
			}
		}
	}
}

func TestUnifiedControlsAnchorToDrawnPinDuringRefresh(t *testing.T) {
	for _, key := range []string{"f", "t", "p", "w", "s", "v"} {
		t.Run(key, func(t *testing.T) {
			s := unifiedTestState()
			// Both fixtures survive their requested filter; the working peer
			// supplies the first status-cycle case.
			pinned := "local/a2"
			if key == "t" {
				pinned = "local/a1"
			}
			for i, e := range s.visible() {
				if pickerString(e["ref"]) == pinned {
					s.Selected = i
				}
			}
			s.refreshing, s.selectedRef = true, pinned
			s.ViewState.SearchFocused = true
			s.ApplyKey("e")
			s.ApplyKey("tab")
			drawn := s.pinnedIndex()
			if drawn < 0 {
				t.Fatalf("fixture pin %s disappeared", pinned)
			}
			// Deliberately emulate a numeric cursor left behind by search clamping.
			s.Selected = (drawn + 1) % len(s.visible())
			s.ApplyKey(key)
			if s.selectedRef != pinned || s.Selected != s.pinnedIndex() {
				t.Fatalf("%s changed drawn pin: ref=%q Selected=%d drawn=%d want=%q", key, s.selectedRef, s.Selected, s.pinnedIndex(), pinned)
			}
		})
	}
}

func TestUnifiedMalformedSGRTerminatesAtFinal(t *testing.T) {
	s := unifiedTestState()
	s.ViewState.SearchFocused = true
	s.FeedChunk("\x1b[<65;4;7zab")
	if s.Query != "ab" {
		t.Fatalf("following literal text lost: got %q want ab", s.Query)
	}
}
