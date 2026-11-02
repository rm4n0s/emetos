//go:build emetos_lldbdap

package cpp

import (
	"os/exec"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

func TestLldbDapRunsACProgramToABreakpoint(t *testing.T) {
	if _, err := exec.LookPath("lldb-dap"); err != nil {
		t.Skip("lldb-dap is not installed")
	}
	runCToABreakpoint(t, nativedebug.TypeLLDB, nil)
}
