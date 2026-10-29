package redis

import (
	"context"
	"os"
	"testing"

	goredis "github.com/redis/go-redis/v9"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// voidLogger drops the lines go-redis prints on its own when a connection cannot be made; the tests of failures make many.
type voidLogger struct{}

func (voidLogger) Printf(context.Context, string, ...interface{}) {}

func TestMain(m *testing.M) {
	goredis.SetLogger(voidLogger{})
	os.Exit(m.Run())
}

// wantRoute checks that err is a tagged error whose route contains want ("BuildSpec.RedisModeInvalid").
func wantRoute(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want the route %s", want)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

// asTagged returns the tagged error.
func asTagged(err error) (*errors.Error, bool) { return errors.FromError(err) }

// extension_CheckManifest checks a manifest the way the host does.
func extension_CheckManifest(m extension.Manifest) error { return extension.CheckManifest(m) }

// errBoom is a plain error that is not tagged.
type errBoom struct{}

func (errBoom) Error() string { return "boom" }
