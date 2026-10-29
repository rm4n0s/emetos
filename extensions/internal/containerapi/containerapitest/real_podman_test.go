//go:build emetos_podman

package containerapitest_test

import (
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestRealPodman runs the engine suite against the user's Podman (build tag emetos_podman; its socket, or a podman system
// service of Emetos's own on a private socket; skipped when there is no Podman). It makes only containers, networks, volumes
// and pods named emetos-test-<random> with the label dev.emetos.test=1, pulls only the pinned images of images.go, and fails
// when something labelled is left. Run it on its own:
//
//	scripts/heavy.sh --alone -- go test -tags emetos_podman ./extensions/internal/containerapi/containerapitest -run TestRealPodman -v
func TestRealPodman(t *testing.T) {
	containerapitest.TestEngine(t, containerapitest.RealPodman(t))
}
