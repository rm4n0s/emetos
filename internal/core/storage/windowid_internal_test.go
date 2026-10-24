package storage

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestEmptyWindowIDWithoutRandomness(t *testing.T) {
	_, err := newEmptyWindowID(strings.NewReader("short"))
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("newEmptyWindowID.WindowIDUnavailable") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	id, err := newEmptyWindowID(strings.NewReader("12345678"))
	if err != nil || id != "3132333435363738" {
		t.Errorf("id %q, %v", id, err)
	}
}
