package storage

import (
	"context"
	"os"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestSharingEncodeFailure(t *testing.T) {
	s := NewSharingStore(Paths{StateHome: t.TempDir()}, SharingStoreOptions{})
	s.marshal = func(any, string, string) ([]byte, error) { return nil, os.ErrInvalid }
	err := s.AddLink(context.Background(), SharingLink{From: "/a", To: "/b"})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("SharingStore.save.SharingEncodeFailed") {
		t.Fatalf("unexpected failure: %v", err)
	}
	if len(s.Snapshot().Links) != 0 {
		t.Error("a link that could not be encoded must not be kept")
	}
}
