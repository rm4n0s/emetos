package exthost

import (
	"os"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
)

// TestMain builds the fake debug adapter program once for the tests of the debug host (debug_test.go).
func TestMain(m *testing.M) { os.Exit(daptest.Main(m)) }
