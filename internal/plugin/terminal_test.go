package plugin

import (
	"bytes"
	"errors"
	"testing"
)

func TestPickerTerminalModeRestoresAfterEscapeAndPanic(t *testing.T) {
	// JS: "main: SIGTERM goes through the same finish as Esc — raw off, children killed, no copy"
	// Mutation captured: dropping deferred raw-mode restoration leaves the fake console in raw mode after Esc or panic.
	for _, test := range []struct {
		name  string
		panic bool
	}{{name: "escape"}, {name: "panic", panic: true}} {
		t.Run(test.name, func(t *testing.T) {
			restored := false
			var screen bytes.Buffer
			setRaw := func() (func() error, error) { return func() error { restored = true; return nil }, nil }
			func() {
				defer func() { _ = recover() }()
				withPickerTerminal(true, setRaw, &screen, func() int {
					if test.panic {
						panic(errors.New("fixture panic"))
					}
					state := NewPickerState()
					state.FeedChunk("\x1b")
					state.FlushEsc()
					return 0
				})
			}()
			if !restored {
				t.Fatal("raw terminal mode was not restored")
			}
			if screen.String() != "\x1b[?25h" {
				t.Fatalf("cursor restore=%q", screen.String())
			}
		})
	}
}
