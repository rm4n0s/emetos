package diff

import "time"

// timeout tells whether a computation may go on (VS Code's ITimeout).
type timeout interface {
	isValid() bool
}

// infiniteTimeout never expires.
type infiniteTimeout struct{}

func (infiniteTimeout) isValid() bool { return true }

// dateTimeout expires a duration after its creation, once and for all (VS
// Code's DateTimeout).
type dateTimeout struct {
	start time.Time
	limit time.Duration
	now   func() time.Time
	valid bool
}

// newDateTimeout returns a timeout of the duration d (positive) measured with
// now.
func newDateTimeout(d time.Duration, now func() time.Time) *dateTimeout {
	return &dateTimeout{start: now(), limit: d, now: now, valid: true}
}

func (t *dateTimeout) isValid() bool {
	if t.valid && t.now().Sub(t.start) >= t.limit {
		t.valid = false
	}
	return t.valid
}
