//go:build emetos_podman

package extensions

import (
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestConnectToDatabaseOnARealPodman is the same on the user's Podman (skipped when there is none):
//
//	scripts/heavy.sh --alone -- go test -tags emetos_podman ./extensions -run TestConnectToDatabaseOnARealPodman -count=1 -v
func TestConnectToDatabaseOnARealPodman(t *testing.T) {
	connectToARealPostgres(t, containerapitest.RealPodman(t), podmanConfig)
}
