package variables

import (
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Fatalf("unexpected failure path %s, want it to contain %s", appErr.Route(), route)
	}
}

// fakeState is an extension.State in memory.
type fakeState struct {
	data map[string]json.RawMessage
	err  error
}

func newState() *fakeState { return &fakeState{data: map[string]json.RawMessage{}} }

func (s *fakeState) Get(key string) (json.RawMessage, bool) {
	v, ok := s.data[key]
	return v, ok
}

func (s *fakeState) Set(key string, value any) error {
	if s.err != nil {
		return s.err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.data[key] = raw
	return nil
}

// seq is a random source that gives the bytes 1, 2, 3, ... and then starts again.
type seq struct{ n byte }

func (s *seq) Read(p []byte) (int, error) {
	for i := range p {
		s.n++
		p[i] = s.n
	}
	return len(p), nil
}

var _ io.Reader = (*seq)(nil)

var fixedNow = func() time.Time { return time.Date(2026, 11, 3, 10, 20, 30, 456_000_000, time.UTC) }
