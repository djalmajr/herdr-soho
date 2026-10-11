//go:build linux

package platform

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

// procExec reads /proc/<pid>/exe (the resolved executable) and
// /proc/<pid>/cmdline (NUL-separated arguments, read bounded).
func procExec(pid int) (string, []string, error) {
	dir := "/proc/" + strconv.Itoa(pid)
	exe, err := os.Readlink(dir + "/exe")
	if err != nil {
		return "", nil, err
	}
	f, err := os.Open(dir + "/cmdline")
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, procExecMaxBytes+1))
	if err != nil {
		return "", nil, err
	}
	if len(data) == 0 || len(data) > procExecMaxBytes || data[len(data)-1] != 0 {
		return "", nil, errors.New("platform: malformed process arguments")
	}
	return exe, strings.Split(string(data[:len(data)-1]), "\x00"), nil
}
