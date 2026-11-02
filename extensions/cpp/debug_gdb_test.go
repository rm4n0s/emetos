//go:build emetos_gdb

package cpp

import (
	"os/exec"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

func TestGdbRunsACProgramToABreakpoint(t *testing.T) {
	if _, err := exec.LookPath("gdb"); err != nil {
		t.Skip("gdb is not installed")
	}
	runCToABreakpoint(t, nativedebug.TypeGDB, gdbSettings)
}
