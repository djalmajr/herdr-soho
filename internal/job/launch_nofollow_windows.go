//go:build windows

package job

import (
	"os"
	"syscall"
)

// FILE_FLAG_OPEN_REPARSE_POINT opens a reparse point (a symlink, a
// junction or any other tag) as itself instead of following it.
const fileFlagOpenReparsePoint = 0x00200000

// openNoFollow opens an existing path (OPEN_EXISTING, never a create)
// without following a reparse point (FILE_FLAG_OPEN_REPARSE_POINT): a
// reparse point or a directory at the path is refused as errLaunchFile from
// the opened handle's own attributes, before any read or lock. No
// FILE_FLAG_BACKUP_SEMANTICS is passed, so a directory does not open at
// all, and a filesystem path names no pipe, so the open cannot block on
// one. The sharing mode admits the other soho processes' handles.
// Read-only, or read-write when write is set. Not proven natively yet.
func openNoFollow(path string, write bool) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access := uint32(syscall.GENERIC_READ)
	if write {
		access |= syscall.GENERIC_WRITE
	}
	h, err := syscall.CreateFile(name, access,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL|fileFlagOpenReparsePoint, 0)
	if err != nil {
		return nil, err
	}
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		_ = syscall.CloseHandle(h)
		return nil, err
	}
	if info.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		_ = syscall.CloseHandle(h)
		return nil, errLaunchFile
	}
	return os.NewFile(uintptr(h), path), nil
}
