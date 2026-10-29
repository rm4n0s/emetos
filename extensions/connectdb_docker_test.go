//go:build emetos_docker

package extensions

import (
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestConnectToDatabaseOnARealDocker uses Connect to Database of the Docker extension on a labelled postgres container and connects
// through the form it opens. Run it alone, on its own:
//
//	scripts/heavy.sh --alone -- go test -tags emetos_docker ./extensions -run TestConnectToDatabaseOnARealDocker -count=1 -v
func TestConnectToDatabaseOnARealDocker(t *testing.T) {
	connectToARealPostgres(t, containerapitest.RealDocker(t), dockerConfig)
}
