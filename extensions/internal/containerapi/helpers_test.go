package containerapi

import (
	"testing"

	"github.com/rm4n0s/errors"
)

// routeOf fails the test unless err is an *errors.Error whose route contains route (the call chain from the outermost
// function to the tag), and returns it for a look at its message and metadata. wantRoute is the same when only the route matters.
func routeOf(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want a failure at %s", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("error %T (%v) is not an *errors.Error", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Fatalf("route = %s, want %s", appErr.Route(), route)
	}
	return appErr
}

// wantRoute is routeOf without the result.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	_ = routeOf(t, err, route)
}
