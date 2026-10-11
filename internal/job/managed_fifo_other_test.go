//go:build !unix

package job

import "errors"

func makeTestFIFO(path string) error { return errors.New("no FIFO on this system") }

const testHasFIFO = false
