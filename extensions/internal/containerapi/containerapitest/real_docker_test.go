//go:build emetos_docker

package containerapitest_test

import (
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestRealDocker runs the engine suite against the user's Docker (build tag emetos_docker; skipped when no engine answers).
// It makes only containers, networks and volumes named emetos-test-<random> with the label dev.emetos.test=1, pulls only the
// pinned images of images.go, and fails when something labelled is left. Run it on its own:
//
//	scripts/heavy.sh --alone -- go test -tags emetos_docker ./extensions/internal/containerapi/containerapitest -run TestRealDocker -v
func TestRealDocker(t *testing.T) {
	containerapitest.TestEngine(t, containerapitest.RealDocker(t))
}
