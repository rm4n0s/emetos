//go:build emetos_lldbdap

package rust

import (
	"os/exec"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

func TestLldbDapRunsARustProgramToABreakpoint(t *testing.T) {
	if _, err := exec.LookPath("lldb-dap"); err != nil {
		t.Skip("lldb-dap is not installed")
	}
	runRustToABreakpoint(t, nativedebug.TypeLLDB, nil, false)
}

func TestLldbDapRunsARustTestToABreakpoint(t *testing.T) {
	if _, err := exec.LookPath("lldb-dap"); err != nil {
		t.Skip("lldb-dap is not installed")
	}
	runRustToABreakpoint(t, nativedebug.TypeLLDB, nil, true)
}
