//go:build emetos_gdb

package rust

import (
	"os/exec"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

func TestGdbRunsARustProgramToABreakpoint(t *testing.T) {
	if _, err := exec.LookPath("gdb"); err != nil {
		t.Skip("gdb is not installed")
	}
	runRustToABreakpoint(t, nativedebug.TypeGDB, gdbSettings, false)
}

func TestGdbRunsARustTestToABreakpoint(t *testing.T) {
	if _, err := exec.LookPath("gdb"); err != nil {
		t.Skip("gdb is not installed")
	}
	runRustToABreakpoint(t, nativedebug.TypeGDB, gdbSettings, true)
}
