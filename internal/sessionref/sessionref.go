package sessionref

import (
	"regexp"
	"strings"
)

const LocalMachine = "local"

var (
	panePattern    = regexp.MustCompile(`^w[0-9A-Za-z]+:p[0-9A-Za-z]+$`)
	machinePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

type Ref struct {
	Machine string
	PaneID  string
}

// ParseRef parses [machine/]pane references and returns nil for all other values.
func ParseRef(value any) *Ref {
	input, ok := value.(string)
	if !ok {
		return nil
	}
	input = strings.TrimFunc(input, isJSWhitespace)
	slash := strings.IndexByte(input, '/')
	machine, paneID := LocalMachine, input
	if slash >= 0 {
		machine, paneID = input[:slash], input[slash+1:]
	}
	if !machinePattern.MatchString(machine) || !panePattern.MatchString(paneID) {
		return nil
	}
	return &Ref{Machine: machine, PaneID: paneID}
}

// FormatRef renders a reference in canonical machine/pane form.
func FormatRef(ref Ref) string {
	if ref.Machine == "" {
		ref.Machine = LocalMachine
	}
	return ref.Machine + "/" + ref.PaneID
}

// HerdrMachineArgs returns the leading Herdr arguments for a remote machine.
func HerdrMachineArgs(machine string) []string {
	if machine == "" || machine == LocalMachine {
		return []string{}
	}
	return []string{"--machine", machine}
}

func isJSWhitespace(r rune) bool {
	return r >= 0x9 && r <= 0xd || r == 0x20 || r == 0xa0 || r == 0x1680 ||
		r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
}
