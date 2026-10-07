//go:build darwin

package plugin

import (
	"syscall"
	"unsafe"
)

func getPickerTermios(fd int, term *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCGETA), uintptr(unsafe.Pointer(term)))
	if errno != 0 {
		return errno
	}
	return nil
}

// pickerTerminalSize returns the actual terminal viewport size in cells
// (columns, rows) for fd, using the winsize ioctl.
func pickerTerminalSize(fd uintptr) (width, height int, err error) {
	var size struct{ Row, Col, Xpixel, Ypixel uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(0x40087468), uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0, 0, errno
	}
	return int(size.Col), int(size.Row), nil
}

// pickerTerminalWidth keeps the existing width-only contract for callers
// that only need the columns.
func pickerTerminalWidth(fd uintptr) (int, error) {
	w, _, err := pickerTerminalSize(fd)
	return w, err
}
func setPickerTermios(fd int, term *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCSETA), uintptr(unsafe.Pointer(term)))
	if errno != 0 {
		return errno
	}
	return nil
}
