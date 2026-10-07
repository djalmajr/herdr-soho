//go:build windows

package install

import (
	"fmt"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// winUserPathStore is the production seam for the Windows user PATH (HKCU
// Environment Path): raw advapi32 calls only — RegOpenKeyExW and
// RegQueryValueExW from the stdlib syscall package and RegSetValueExW via
// a LazyDLL proc. No PowerShell, no REG shell-out, no new module. The
// value's registry kind (REG_SZ/REG_EXPAND_SZ) and every existing entry
// are preserved by the caller (UpdateUserPathValue); this store moves the
// whole value in and out of the registry.
//
// The raw calls go through the package seams rawQueryValueEx and
// rawSetValueExW; the Windows-tagged native tests replace them with
// recorders and drive the value functions with injected fake handles, so
// no test ever opens, reads or writes the real registry.
type winUserPathStore struct{}

var regSetValueExWProc = syscall.NewLazyDLL("advapi32.dll").NewProc("RegSetValueExW")

// rawQueryValueEx is the seam over advapi32!RegQueryValueExW. The
// production body is the stdlib syscall wrapper; native tests replace it.
var rawQueryValueEx = func(h syscall.Handle, name16 *uint16, valtype *uint32, buf *byte, size *uint32) error {
	return syscall.RegQueryValueEx(h, name16, nil, valtype, buf, size)
}

// rawSetValueExW is the seam over advapi32!RegSetValueExW. The production
// body supplies the EXACT six arguments — hKey, lpValueName, Reserved,
// dwType, lpData and cbData in BYTES of UTF-16 data — and returns the LSTATUS
// the function reports, which is the authoritative result. Native tests
// replace it with a recorder of the exact arguments.
var rawSetValueExW = func(h syscall.Handle, name16 *uint16, reserved uint32, regType uint32, data16 *uint16, cbData uintptr) (lstatus uintptr, err error) {
	r1, _, e := regSetValueExWProc.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(name16)),
		uintptr(reserved),
		uintptr(regType),
		uintptr(unsafe.Pointer(data16)),
		uintptr(cbData),
	)
	return r1, e
}

// DefaultUserPathStore returns the real HKCU user-PATH store on Windows.
func DefaultUserPathStore() (UserPathStore, error) {
	return winUserPathStore{}, nil
}

// openEnvKey opens the HKCU Environment key with the given access.
func (s winUserPathStore) openEnvKey(access uint32) (syscall.Handle, error) {
	name16, err := syscall.UTF16FromString("Environment")
	if err != nil {
		return 0, fmt.Errorf("install: user PATH: %w", err)
	}
	var handle syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, &name16[0], 0, access, &handle); err != nil {
		return 0, fmt.Errorf("install: user PATH: opening HKCU Environment: %w", err)
	}
	return handle, nil
}

func (s winUserPathStore) Read() (string, string, error) {
	handle, err := s.openEnvKey(syscall.KEY_READ)
	if err != nil {
		return "", "", err
	}
	defer func() {
		_ = syscall.RegCloseKey(handle)
	}()
	return readUserPathValue(handle)
}

func (s winUserPathStore) Write(value, kind string) error {
	handle, err := s.openEnvKey(syscall.KEY_READ | syscall.KEY_WRITE)
	if err != nil {
		return err
	}
	defer func() {
		_ = syscall.RegCloseKey(handle)
	}()
	return writeUserPathValue(handle, value, kind)
}

// readUserPathValue reads the Path value from an already-open key (two
// RegQueryValueExW passes: size, then data). It never opens the key
// itself, which lets the native tests drive it with an injected fake
// handle and a replaced rawQueryValueEx seam.
func readUserPathValue(handle syscall.Handle) (string, string, error) {
	path16, err := syscall.UTF16FromString("Path")
	if err != nil {
		return "", "", fmt.Errorf("install: user PATH: %w", err)
	}
	var varType uint32
	var size uint32
	if err := rawQueryValueEx(handle, &path16[0], &varType, nil, &size); err != nil {
		if err == syscall.ERROR_FILE_NOT_FOUND {
			// No user PATH value yet: start from an empty REG_SZ value.
			return "", UserPathREGSZ, nil
		}
		return "", "", fmt.Errorf("install: user PATH: querying Path: %w", err)
	}
	if varType != syscall.REG_SZ && varType != syscall.REG_EXPAND_SZ {
		return "", "", fmt.Errorf("install: user PATH: unsupported registry type %d for Path", varType)
	}
	kind := UserPathREGSZ
	if varType == syscall.REG_EXPAND_SZ {
		kind = UserPathREGExpandSZ
	}
	if size == 0 {
		return "", kind, nil
	}
	buf := make([]byte, size)
	if err := rawQueryValueEx(handle, &path16[0], &varType, &buf[0], &size); err != nil {
		return "", "", fmt.Errorf("install: user PATH: reading Path: %w", err)
	}
	return utf16BytesToString(buf), kind, nil
}

// writeUserPathValue writes the Path value into an already-open key with a
// single RegSetValueExW. syscall.UTF16FromString already NUL-terminates
// the data, so no extra NUL is appended and cbData is the exact UTF-16
// byte length. A nonzero LSTATUS is the authoritative failure.
func writeUserPathValue(handle syscall.Handle, value, kind string) error {
	path16, err := syscall.UTF16FromString("Path")
	if err != nil {
		return fmt.Errorf("install: user PATH: %w", err)
	}
	value16, err := syscall.UTF16FromString(value)
	if err != nil {
		return fmt.Errorf("install: user PATH: encoding value: %w", err)
	}
	var regType uint32 = syscall.REG_SZ
	if kind == UserPathREGExpandSZ {
		regType = syscall.REG_EXPAND_SZ
	}
	lstatus, _ := rawSetValueExW(handle, &path16[0], 0, regType, &value16[0], uintptr(len(value16))*2)
	if lstatus != 0 {
		return fmt.Errorf("install: user PATH: setting Path: LSTATUS 0x%08X", uint32(lstatus))
	}
	return nil
}

// utf16BytesToString decodes NUL-terminated UTF-16LE registry data.
// unicode/utf16 replaces unpaired surrogates with U+FFFD.
func utf16BytesToString(buf []byte) string {
	if len(buf)%2 != 0 {
		buf = buf[:len(buf)-1]
	}
	units := make([]uint16, len(buf)/2)
	for i := range units {
		units[i] = uint16(buf[2*i]) | uint16(buf[2*i+1])<<8
		if units[i] == 0 {
			units = units[:i]
			break
		}
	}
	return string(utf16.Decode(units))
}
