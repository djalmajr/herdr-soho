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

func pickerTerminalWidth(fd uintptr) (int, error) {
	var size struct{ Row, Col, Xpixel, Ypixel uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(0x40087468), uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0, errno
	}
	return int(size.Col), nil
}
func setPickerTermios(fd int, term *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCSETA), uintptr(unsafe.Pointer(term)))
	if errno != 0 {
		return errno
	}
	return nil
}
