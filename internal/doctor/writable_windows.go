//go:build windows

package doctor

import "os"

var writableAccess = func(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	// libuv's fs__access (Node's accessSync W_OK) grants write to any
	// directory: directories cannot be read-only on Windows, even with
	// FILE_ATTRIBUTE_READONLY set. The attribute only blocks files.
	if !info.IsDir() && info.Mode().Perm()&0o200 == 0 {
		return os.ErrPermission
	}
	return nil
}

func isWritable(path string) bool { return writableAccess(path) == nil }
