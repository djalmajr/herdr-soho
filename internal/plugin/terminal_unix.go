//go:build darwin || linux

package plugin

import (
	"os"
	"syscall"
)

func setPickerRaw(file *os.File) (func() error, error) {
	fd := int(file.Fd())
	var old syscall.Termios
	if err := getPickerTermios(fd, &old); err != nil {
		return nil, err
	}
	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := setPickerTermios(fd, &raw); err != nil {
		return nil, err
	}
	return func() error { return setPickerTermios(fd, &old) }, nil
}
