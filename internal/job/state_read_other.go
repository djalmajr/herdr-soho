//go:build !windows

package job

import "os"

// readReplacedFile is the non-Windows counterpart of the _windows open:
// the atomic replace (writeAtomic0600's rename) has no DELETE-access
// sharing-violation window there, so the plain os.ReadFile is right as is;
// it keeps following links and produces the same errors as before.
func readReplacedFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
