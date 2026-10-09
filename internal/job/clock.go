package job

import "time"

// Clock supplies event timestamps and monotonic wait deadlines.
// Monotonic is nanoseconds from an arbitrary epoch and must not move
// backwards. Sleep is the only pause a wait may take. Now is wall time
// for the ts field and is never used to decide that a wait is over.
type Clock interface {
	Now() time.Time
	Monotonic() int64
	Sleep(time.Duration)
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) Monotonic() int64 { return int64(time.Since(monoEpoch)) }

func (systemClock) Sleep(d time.Duration) {
	if d > 0 {
		time.Sleep(d)
	}
}

// monoEpoch is captured with a monotonic reading. time.Since keeps that
// reading, so a wall-clock step backwards does not shrink Monotonic.
var monoEpoch = time.Now()

func (s *Store) clock() Clock {
	if s != nil && s.Clock != nil {
		return s.Clock
	}
	return systemClock{}
}
