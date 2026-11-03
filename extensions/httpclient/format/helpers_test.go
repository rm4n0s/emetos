package format

import (
	"testing"

	"github.com/rm4n0s/errors"
)

// wantRoute checks that err is an *errors.Error whose route contains route.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	_ = routeErr(t, err, route)
}

func ptr[T any](v T) *T { return &v }

// routeErr is wantRoute that returns the error, for a test that looks at its metadata.
func routeErr(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Fatalf("unexpected failure path %s, want it to contain %s", appErr.Route(), route)
	}
	return appErr
}
