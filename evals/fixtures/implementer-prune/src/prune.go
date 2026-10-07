// Package src is the worker-facing source of the implementer-prune fixture.
// The distributed implementation is deliberately unfinished, mirroring the
// legacy src/prune.mjs stub: the worker under evaluation implements
// PruneBackups, not receives the answer.
package src

import (
	"errors"
	"time"
)

// Options selects one prune request.
type Options struct {
	// Dir names the directory that holds the backup files.
	Dir string
	// Keep is the number of newest backups to retain; it must hold a safe
	// integer greater than or equal to 1.
	Keep float64
	// Now is the caller's current wall clock; it must be a nonzero time.
	Now time.Time
}

var errNotImplemented = errors.New("not implemented")

// PruneBackups removes backups older than the newest Keep backups and
// returns the number of backup files removed. The distributed fixture ships
// this stub deliberately unfinished.
func PruneBackups(o Options) (int, error) {
	return 0, errNotImplemented
}
