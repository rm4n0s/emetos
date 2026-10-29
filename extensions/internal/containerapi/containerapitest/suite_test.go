package containerapitest_test

import (
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestEngineAgainstFake runs the suite that the real-engine tests run, against the fake engine of each kind: it proves that
// the fake and the suite agree, and that the client works on what the suite asks of it.
func TestEngineAgainstFake(t *testing.T) {
	for _, kind := range []containerapi.Kind{containerapi.KindDocker, containerapi.KindPodman} {
		t.Run(string(kind), func(t *testing.T) {
			containerapitest.TestEngine(t, containerapitest.NewFakeHarness(t, kind))
		})
	}
}
