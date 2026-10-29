//go:build emetos_docker

package containerui

import (
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestTheExplorerOverTheRealDocker runs explorerSuite against the user's Docker (skipped when no engine answers). It makes one
// container named emetos-test-<random> with the label dev.emetos.test=1 from the pinned image, lists only what has that label,
// and removes the container itself. Run it on its own:
//
//	scripts/heavy.sh --alone -- go test -tags emetos_docker ./extensions/... -run RealDocker -v
func TestTheExplorerOverTheRealDocker(t *testing.T) {
	explorerSuite(t, containerapitest.RealDocker(t))
}
