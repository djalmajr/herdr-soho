package plugin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func feedModeReply(d *TerminalKeys, mode, state int) {
	for _, r := range fmt.Sprintf("\x1b[?%d;%d$y", mode, state) {
		d.Feed(r)
	}
}

func TestMouseNegotiationNeedsCompleteSnapshot(t *testing.T) {
	dec := TerminalKeys{}
	m := newMouseNegotiation(&dec)
	m.deadline = time.Now().Add(time.Second)
	feedModeReply(&dec, 1000, 2)
	feedModeReply(&dec, 1006, 2)
	var out bytes.Buffer
	if m.poll(&out) || out.Len() != 0 {
		t.Fatal("enabled while alternate prior modes remained unknown")
	}
	feedModeReply(&dec, 1003, 1)
	for _, mode := range mouseModes {
		if _, ok := dec.DecrQM[mode]; !ok {
			feedModeReply(&dec, mode, 2)
		}
	}
	if !m.poll(&out) || !m.wheelAvailable() {
		t.Fatal("complete supported snapshot did not enable wheel")
	}
	feedModeReply(&dec, 1002, 1)
	if dec.DecrQM[1002] != 2 {
		t.Fatal("accepted a late reply after negotiation settled")
	}
	out.Reset()
	m.restore(&out)
	if out.String() != "\x1b[?1000l\x1b[?1006l\x1b[?1003h" {
		t.Fatalf("lost preexisting motion tracking: %q", out.String())
	}
}

func TestMouseNegotiationTimeoutUnsupportedAndUnsolicited(t *testing.T) {
	dec := TerminalKeys{}
	feedModeReply(&dec, 1000, 1)
	if len(dec.DecrQM) != 0 {
		t.Fatal("accepted unsolicited reply before query")
	}
	for _, missing := range []int{1002, 1006} {
		dec := TerminalKeys{}
		m := newMouseNegotiation(&dec)
		m.deadline = time.Now().Add(-time.Second)
		for _, mode := range mouseModes {
			if mode != missing {
				feedModeReply(&dec, mode, 2)
			}
		}
		var out bytes.Buffer
		m.poll(&out)
		m.restore(&out)
		if out.Len() != 0 || m.wheelAvailable() {
			t.Fatalf("missing%d changed state: %q", missing, out.String())
		}
	}
	for _, unsupported := range []int{0, 4} {
		dec := TerminalKeys{}
		m := newMouseNegotiation(&dec)
		for _, mode := range mouseModes {
			state := 2
			if mode == 1006 {
				state = unsupported
			}
			feedModeReply(&dec, mode, state)
		}
		var out bytes.Buffer
		m.poll(&out)
		m.restore(&out)
		if out.Len() != 0 {
			t.Fatalf("unsupported encoding partially enabled tracking: %q", out.String())
		}
	}
	dec = TerminalKeys{}
	m := newMouseNegotiation(&dec)
	for id := 2000; id < 2500; id++ {
		feedModeReply(&dec, id, 1)
	}
	if len(dec.DecrQM) != 0 {
		t.Fatal("unbounded unknown mode replies allocated state")
	}
	_ = m
}

type shortProtocolWriter struct {
	bytes.Buffer
	short bool
}

func (w *shortProtocolWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func (w *shortProtocolWriter) Write(p []byte) (int, error) {
	if w.short && bytes.Contains(p, []byte("1006h")) {
		n := len(p) - 1
		w.Buffer.Write(p[:n])
		return n, io.ErrShortWrite
	}
	return w.Buffer.Write(p)
}
func TestMouseNegotiationPartialWriteRestoresKnownModes(t *testing.T) {
	dec := TerminalKeys{}
	m := newMouseNegotiation(&dec)
	for _, mode := range mouseModes {
		state := 2
		if mode == 1002 {
			state = 1
		}
		feedModeReply(&dec, mode, state)
	}
	w := &shortProtocolWriter{short: true}
	m.poll(w)
	if m.wheelAvailable() {
		t.Fatal("partial encoding write reported wheel ready")
	}
	w.short = false
	w.Reset()
	m.restore(w)
	if w.String() != "\x1b[?1000l\x1b[?1006l\x1b[?1002h" {
		t.Fatalf("partial writer lost initial state: %q", w.String())
	}
}

func TestTerminalFailureNeverPopsCallerKeyboardState(t *testing.T) {
	var out bytes.Buffer
	rawError := func() (func() error, error) { return nil, errors.New("raw unavailable") }
	if PickerTerminal(true, rawError, &out, func() int { return 9 }) != 1 || strings.Contains(out.String(), keyboardPopSeq) {
		t.Fatalf("raw error popped caller keyboard state: %q", out.String())
	}
	restored := 0
	rawOK := func() (func() error, error) { return func() error { restored++; return nil }, nil }
	failed := &failBeforePushWriter{}
	if PickerTerminal(true, rawOK, failed, func() int { return 9 }) != 1 || restored != 1 || strings.Contains(failed.output.String(), keyboardPopSeq) {
		t.Fatalf("push error restoration=%d output=%q", restored, failed.output.String())
	}
}

type failBeforePushWriter struct{ output bytes.Buffer }

func (w *failBeforePushWriter) Write(p []byte) (int, error) {
	if bytes.Equal(p, []byte(keyboardPushSeq)) {
		return 0, io.ErrClosedPipe
	}
	return w.output.Write(p)
}
