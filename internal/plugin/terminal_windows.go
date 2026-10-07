//go:build windows

package plugin

import (
	"os"
	"syscall"
	"unsafe"
)

var kernel32Picker = syscall.NewLazyDLL("kernel32.dll")
var getConsoleModePicker = kernel32Picker.NewProc("GetConsoleMode")
var setConsoleModePicker = kernel32Picker.NewProc("SetConsoleMode")
var getConsoleScreenBufferInfoPicker = kernel32Picker.NewProc("GetConsoleScreenBufferInfo")

func setPickerRaw(file *os.File) (func() error, error) {
	handle := syscall.Handle(file.Fd())
	var old uint32
	ok, _, err := getConsoleModePicker.Call(uintptr(handle), uintptr(unsafe.Pointer(&old)))
	if ok == 0 {
		return nil, err
	}
	const enableProcessedInput = 0x0001
	const enableLineInput = 0x0002
	const enableEchoInput = 0x0004
	// Keep enhanced keyboard sequences intact when reading the ConPTY input.
	const enableVirtualTerminalInput = 0x0200
	raw := old&^(enableProcessedInput|enableLineInput|enableEchoInput) | enableVirtualTerminalInput
	ok, _, err = setConsoleModePicker.Call(uintptr(handle), uintptr(raw))
	if ok == 0 {
		return nil, err
	}
	return func() error {
		ok, _, err := setConsoleModePicker.Call(uintptr(handle), uintptr(old))
		if ok == 0 {
			return err
		}
		return nil
	}, nil
}

// pickerTerminalSize returns the actual terminal viewport size in cells
// (columns, rows) for handle, measured on the screen buffer window (the
// visible rectangle), not the full scrollback buffer size.
func pickerTerminalSize(handle uintptr) (width, height int, err error) {
	type coord struct{ X, Y int16 }
	type rect struct{ Left, Top, Right, Bottom int16 }
	type info struct {
		Size              coord
		CursorPosition    coord
		Attributes        uint16
		Window            rect
		MaximumWindowSize coord
	}
	var screen info
	ok, _, err := getConsoleScreenBufferInfoPicker.Call(handle, uintptr(unsafe.Pointer(&screen)))
	if ok == 0 {
		return 0, 0, err
	}
	return int(screen.Window.Right - screen.Window.Left + 1), int(screen.Window.Bottom - screen.Window.Top + 1), nil
}

// pickerTerminalWidth keeps the existing width-only contract for callers
// that only need the columns.
func pickerTerminalWidth(handle uintptr) (int, error) {
	w, _, err := pickerTerminalSize(handle)
	return w, err
}
