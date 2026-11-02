package debug_test

import (
	"os"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
)

// TestMain builds the fake adapter program once for the tests that start it.
func TestMain(m *testing.M) { os.Exit(daptest.Main(m)) }
