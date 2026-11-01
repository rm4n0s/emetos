package dap_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/errors"
)

// TestMain builds the fake adapter program once for the tests of the package.
func TestMain(m *testing.M) { os.Exit(daptest.Main(m)) }

const wait = 20 * time.Second

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	t.Cleanup(cancel)
	return ctx
}

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	_ = wantRouteError(t, err, route)
}

// wantRouteError is wantRoute and gives the error back, for its metadata.
func wantRouteError(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want it to contain %s", appErr.Route(), route)
	}
	return appErr
}

// transports are the ways the fake adapter is reached; the tests of the client run over each.
var transports = []struct {
	name string
	tr   daptest.Transport
}{
	{"in process", daptest.InProcess},
	{"stdio", daptest.Stdio},
	{"tcp", daptest.TCP},
	{"unix socket", daptest.Unix},
	{"program that prints its port", daptest.Announce},
}
