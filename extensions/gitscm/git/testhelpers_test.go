package git

import (
	"testing"

	"github.com/rm4n0s/errors"
)

// wantRoute fails the test unless err is an *errors.Error whose route has the given tail ("Func.Tag" or "Type.Method->Func.Tag").
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	_ = routeOf(t, err, route)
}

// routeOf is wantRoute that also returns the error, for a test that reads its tag, message or metadata.
func routeOf(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
		return nil
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
	return appErr
}
