package fakeas

import (
	"net"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestStartFailsWhenItCannotListen(t *testing.T) {
	_, err := Start(Options{Listen: func() (net.Listener, error) { return nil, errors.New("FakeNoPort", "no port") }})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Start.FakeASListenFailed") {
		t.Fatalf("err = %v", err)
	}
}
