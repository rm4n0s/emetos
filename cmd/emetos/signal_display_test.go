//go:build emetos_display

package main

import (
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// TestRealInputSignalEndsAsAQuit sends SIGINT and SIGTERM to an editor with unsaved changes: it ends by itself, with status 0 and
// no SIGKILL, without a dialog, and the next start restores the text from the backups the quit made (SPEC §10.3, decision of
// 2026-10-31).
func TestRealInputSignalEndsAsAQuit(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"SIGINT", syscall.SIGINT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := display.Start(t, display.Options{Args: []string{".", "app.go"}, Project: projectFiles})
			a.Press("ctrl+End")
			a.Type("// unsaved by " + tc.name)
			a.Eventually("the document to be dirty", func() (bool, string) {
				ed := a.Editor()
				return ed.Dirty && strings.Contains(ed.Text, "// unsaved by "+tc.name), ed.Text
			})
			if status := a.Signal(tc.sig, 10*time.Second); status != 0 {
				t.Errorf("exit status after %v = %d, want 0\n%s", tc.sig, status, a.Log())
			}
			a.Restart()
			a.Eventually("the unsaved text to come back", func() (bool, string) {
				ed := a.Editor()
				return ed.Dirty && strings.HasSuffix(strings.TrimSpace(ed.Text), "// unsaved by "+tc.name), ed.Text
			})
		})
	}
}
