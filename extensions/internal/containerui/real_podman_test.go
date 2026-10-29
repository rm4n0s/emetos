//go:build emetos_podman

package containerui

import (
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestTheExplorerOverTheRealPodman runs explorerSuite against the user's Podman (skipped when there is none), as the Docker one does
// (real_docker_test.go).
func TestTheExplorerOverTheRealPodman(t *testing.T) {
	explorerSuite(t, containerapitest.RealPodman(t))
}
