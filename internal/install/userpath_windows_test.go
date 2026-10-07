//go:build windows

package install

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// These native tests drive the raw registry call seams with injected fake
// handles and replaced raw-query/raw-set functions: they prove the exact
// RegSetValueExW ABI (six arguments, cbData in bytes, numeric type, exact
// UTF-16 data) and the full Unicode decode WITHOUT ever opening, reading
// or writing the real HKCU registry.

// fakeHandle is an injected registry handle pointing at nothing real.
const fakeHandle syscall.Handle = 0x0000ABCD

// unsafeSlice16 exposes n uint-16 units starting at p (test-only ABI
// record helper).
func unsafeSlice16(p *uint16, n int) []uint16 {
	if n <= 0 {
		return nil
	}
	return unsafe.Slice(p, n)
}

func utf16UnitsBytes(units []uint16) []byte {
	b := make([]byte, len(units)*2)
	for i, u := range units {
		b[2*i] = byte(u)
		b[2*i+1] = byte(u >> 8)
	}
	return b
}

// nameUnits reads a NUL-terminated UTF-16 name from a raw pointer. The
// legacy unsafe.Add strides in bytes, so the offset is 2*i — sizeof
// (uint16) — per index; the scan stops at the NUL (the production name
// is "Path" + NUL), so only in-bounds units are ever dereferenced.
func nameUnits(p *uint16) []uint16 {
	var out []uint16
	for i := 0; i < 64; i++ {
		u := *(*uint16)(unsafe.Add(unsafe.Pointer(p), 2*i))
		if u == 0 {
			break
		}
		out = append(out, u)
	}
	return out
}

// TestWriteRawCallSuppliesSixExactArguments proves the production write
// path supplies RegSetValueExW with exactly the six arguments — hKey,
// lpValueName, Reserved, dwType, lpData, cbData — the numeric
// REG_EXPAND_SZ type, and the exact UTF-16 byte length as cbData
// (UTF16FromString already NUL-terminates; no double NUL).
func TestWriteRawCallSuppliesSixExactArguments(t *testing.T) {
	type rawSetCall struct {
		handle   syscall.Handle
		name     []uint16
		reserved uint32
		regType  uint32
		data     []uint16
		cbData   uintptr
	}
	var call *rawSetCall
	oldSet := rawSetValueExW
	rawSetValueExW = func(h syscall.Handle, name16 *uint16, reserved uint32, regType uint32, data16 *uint16, cbData uintptr) (uintptr, error) {
		dataLen := int(cbData) / 2
		call = &rawSetCall{handle: h, name: nameUnits(name16), reserved: reserved, regType: regType, data: make([]uint16, dataLen), cbData: cbData}
		copy(call.data, unsafeSlice16(data16, dataLen))
		return 0, nil
	}
	t.Cleanup(func() { rawSetValueExW = oldSet })

	value := `C:\tools;D:\bin`
	if err := writeUserPathValue(fakeHandle, value, UserPathREGExpandSZ); err != nil {
		t.Fatalf("write: %v", err)
	}
	if call == nil {
		t.Fatal("raw RegSetValueExW was never called")
	}
	if call.handle != fakeHandle {
		t.Fatalf("hKey = %v, want the injected fake handle", call.handle)
	}
	if !reflect.DeepEqual(call.name, []uint16{'P', 'a', 't', 'h'}) {
		t.Fatalf("lpValueName = %v, want \"Path\"", call.name)
	}
	if call.reserved != 0 {
		t.Fatalf("Reserved = %d, want 0", call.reserved)
	}
	if call.regType != syscall.REG_EXPAND_SZ {
		t.Fatalf("dwType = %d, want the numeric REG_EXPAND_SZ (%d)", call.regType, syscall.REG_EXPAND_SZ)
	}
	expectedData, err := syscall.UTF16FromString(value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(utf16UnitsBytes(call.data), utf16UnitsBytes(expectedData)) {
		t.Fatalf("lpData = %v, want the exact UTF-16 of %q", call.data, value)
	}
	if call.cbData != uintptr(len(expectedData))*2 {
		t.Fatalf("cbData = %d, want the exact UTF-16 byte length %d", call.cbData, len(expectedData)*2)
	}
	if call.data[len(call.data)-1] != 0 {
		t.Fatalf("lpData is not NUL-terminated: %v", call.data)
	}
	if len(call.data) >= 2 && call.data[len(call.data)-2] == 0 {
		t.Fatalf("double NUL terminator in lpData: %v", call.data)
	}
}

// TestWriteRawCallREGSZType proves the REG_SZ kind maps to the numeric
// REG_SZ type.
func TestWriteRawCallREGSZType(t *testing.T) {
	var regType uint32
	var called bool
	oldSet := rawSetValueExW
	rawSetValueExW = func(h syscall.Handle, name16 *uint16, reserved uint32, rt uint32, data16 *uint16, cbData uintptr) (uintptr, error) {
		called = true
		regType = rt
		return 0, nil
	}
	t.Cleanup(func() { rawSetValueExW = oldSet })
	if err := writeUserPathValue(fakeHandle, `C:\bin`, UserPathREGSZ); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !called || regType != syscall.REG_SZ {
		t.Fatalf("dwType = %d (called=%v), want the numeric REG_SZ (%d)", regType, called, syscall.REG_SZ)
	}
}

// TestWriteRawCallNonBMPData proves non-BMP values cross the ABI as their
// UTF-16 surrogate pairs with the correct cbData.
func TestWriteRawCallNonBMPData(t *testing.T) {
	var callData []uint16
	var callCBData uintptr
	oldSet := rawSetValueExW
	rawSetValueExW = func(h syscall.Handle, name16 *uint16, reserved uint32, regType uint32, data16 *uint16, cbData uintptr) (uintptr, error) {
		callData = unsafeSlice16(data16, int(cbData)/2)
		callCBData = cbData
		return 0, nil
	}
	t.Cleanup(func() { rawSetValueExW = oldSet })

	value := `C:\` + "\U0001D11E" + `\bin` // U+1D11E: surrogate pair D834 DD1E
	if err := writeUserPathValue(fakeHandle, value, UserPathREGSZ); err != nil {
		t.Fatalf("write: %v", err)
	}
	expectedData, err := syscall.UTF16FromString(value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(utf16UnitsBytes(callData), utf16UnitsBytes(expectedData)) {
		t.Fatalf("lpData = %v, want the exact UTF-16 of %q", callData, value)
	}
	found := false
	for i := 0; i+1 < len(callData); i++ {
		if callData[i] == 0xD834 && callData[i+1] == 0xDD1E {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("surrogate pair D834 DD1E missing from the encoded data: %v", callData)
	}
	if callCBData != uintptr(len(expectedData))*2 {
		t.Fatalf("cbData = %d, want %d", callCBData, len(expectedData)*2)
	}
}

// TestWriteRawCallLSTATUSFailure proves the returned LSTATUS is the
// authoritative error (a last-error-free failure still reports clearly).
func TestWriteRawCallLSTATUSFailure(t *testing.T) {
	oldSet := rawSetValueExW
	rawSetValueExW = func(h syscall.Handle, name16 *uint16, reserved uint32, regType uint32, data16 *uint16, cbData uintptr) (uintptr, error) {
		// E_ACCESS_DENIED as LSTATUS, with no related last-error state.
		return 0x80070005, nil
	}
	t.Cleanup(func() { rawSetValueExW = oldSet })
	err := writeUserPathValue(fakeHandle, `C:\bin`, UserPathREGSZ)
	if err == nil || !strings.Contains(err.Error(), "LSTATUS 0x80070005") {
		t.Fatalf("err = %v, want the authoritative LSTATUS surfaced", err)
	}
}

// TestReadRawCallFakeHandle proves the read path (two raw-query passes
// over an injected fake handle) decodes full Unicode including surrogate
// pairs and maps the numeric type to the seam kind.
func TestReadRawCallFakeHandle(t *testing.T) {
	value := "C:\\bin;\U0001D11E\\dir"
	units, err := syscall.UTF16FromString(value)
	if err != nil {
		t.Fatal(err)
	}
	raw := utf16UnitsBytes(units)
	var passes int
	oldQuery := rawQueryValueEx
	rawQueryValueEx = func(h syscall.Handle, name16 *uint16, valtype *uint32, buf *byte, size *uint32) error {
		if h != fakeHandle {
			return errors.New("unexpected handle")
		}
		if !reflect.DeepEqual(nameUnits(name16), []uint16{'P', 'a', 't', 'h'}) {
			return errors.New("wrong value name")
		}
		passes++
		*valtype = syscall.REG_EXPAND_SZ
		if buf == nil {
			*size = uint32(len(raw))
			return nil
		}
		if int(*size) < len(raw) {
			return errors.New("buffer too small")
		}
		copy(unsafe.Slice(buf, *size), raw)
		return nil
	}
	t.Cleanup(func() { rawQueryValueEx = oldQuery })

	got, kind, err := readUserPathValue(fakeHandle)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != value {
		t.Fatalf("value = %q, want %q (full Unicode, surrogate pairs)", got, value)
	}
	if kind != UserPathREGExpandSZ {
		t.Fatalf("kind = %q, want %s", kind, UserPathREGExpandSZ)
	}
	if passes != 2 {
		t.Fatalf("raw query passes = %d, want 2 (size + data)", passes)
	}
}

// TestReadRawCallNotFoundIsEmptyREGSZ proves a missing Path value is read
// as an empty REG_SZ value (the write then creates it).
func TestReadRawCallNotFoundIsEmptyREGSZ(t *testing.T) {
	oldQuery := rawQueryValueEx
	rawQueryValueEx = func(h syscall.Handle, name16 *uint16, valtype *uint32, buf *byte, size *uint32) error {
		return syscall.ERROR_FILE_NOT_FOUND
	}
	t.Cleanup(func() { rawQueryValueEx = oldQuery })
	got, kind, err := readUserPathValue(fakeHandle)
	if err != nil || got != "" || kind != UserPathREGSZ {
		t.Fatalf("got %q kind %q err %v, want empty REG_SZ without error", got, kind, err)
	}
}

// TestReadRawCallRejectsUnsupportedType proves types other than
// REG_SZ/REG_EXPAND_SZ are refused with the numeric type named.
func TestReadRawCallRejectsUnsupportedType(t *testing.T) {
	oldQuery := rawQueryValueEx
	rawQueryValueEx = func(h syscall.Handle, name16 *uint16, valtype *uint32, buf *byte, size *uint32) error {
		*valtype = 7 // REG_MULTI_SZ
		*size = 0
		return nil
	}
	t.Cleanup(func() { rawQueryValueEx = oldQuery })
	if _, _, err := readUserPathValue(fakeHandle); err == nil || !strings.Contains(err.Error(), "unsupported registry type 7") {
		t.Fatalf("err = %v, want the unsupported type refusal", err)
	}
}

// TestUTF16BytesToStringFullUnicode proves the pure decode: BMP, surrogate
// pairs (non-BMP, encoded little-endian: D834 is 34 D8, DD1E is 1E DD),
// NUL termination, odd-byte trim, and the documented unicode/utf16
// behavior for an unpaired surrogate (U+FFFD, with no preservation or
// data-loss promise).
func TestUTF16BytesToStringFullUnicode(t *testing.T) {
	// 'A' + U+1D11E (D834 DD1E, little-endian) + NUL
	if got := utf16BytesToString([]byte{0x41, 0x00, 0x34, 0xD8, 0x1E, 0xDD, 0x00, 0x00}); got != "A\U0001D11E" {
		t.Fatalf("decoded %q, want \"A\" + U+1D11E", got)
	}
	// odd trailing byte is trimmed, content unchanged
	if got := utf16BytesToString([]byte{0x41, 0x00, 0x34, 0xD8, 0x1E, 0xDD, 0x00, 0x00, 0x7F}); got != "A\U0001D11E" {
		t.Fatalf("decoded %q with odd byte", got)
	}
	// an unpaired high surrogate (little-endian 34 D8) becomes the Unicode
	// replacement character, the documented unicode/utf16 behavior
	if got := utf16BytesToString([]byte{0x34, 0xD8, 0x00, 0x00}); got != "\uFFFD" {
		t.Fatalf("decoded %q, want U+FFFD (unicode/utf16 behavior)", got)
	}
}
