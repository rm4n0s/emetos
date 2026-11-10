//go:build !darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

// TestRunInstallAndUninstallLauncher is the end-to-end test of runInstallLauncher and
// runUninstallLauncher themselves (the real implementations, not fakes): install writes Emetos's own
// desktop entry and icons under an isolated $XDG_DATA_HOME, naming the test binary (os.Executable) as
// the program, and uninstall removes exactly what it wrote.
func TestRunInstallAndUninstallLauncher(t *testing.T) {
	paths := storagetest.Isolate(t)
	te := newTestEnv(t, t.TempDir())
	te.installLauncher, te.uninstallLauncher = runInstallLauncher, runUninstallLauncher

	if got := run([]string{"--install-launcher"}, te.env); got != 0 {
		t.Fatalf("install: status = %d, stderr %q", got, te.stderr.String())
	}
	desktopFile := filepath.Join(paths.DataHome, "applications", "dev.emetos.Emetos.desktop")
	if !strings.Contains(te.stdout.String(), "Wrote "+desktopFile) {
		t.Errorf("stdout = %q, want it to say it wrote %s", te.stdout.String(), desktopFile)
	}
	data, err := os.ReadFile(desktopFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Exec=emetos ") {
		t.Errorf("the entry still names the plain program: %q", data)
	}
	iconFile := filepath.Join(paths.DataHome, "icons", "hicolor", "16x16", "apps", "dev.emetos.Emetos.png")
	if _, err := os.Stat(iconFile); err != nil {
		t.Errorf("the 16px icon was not written: %v", err)
	}

	te.stdout.Reset()
	if got := run([]string{"--uninstall-launcher"}, te.env); got != 0 {
		t.Fatalf("uninstall: status = %d, stderr %q", got, te.stderr.String())
	}
	if !strings.Contains(te.stdout.String(), "Removed "+desktopFile) {
		t.Errorf("stdout = %q, want it to say it removed %s", te.stdout.String(), desktopFile)
	}
	if _, err := os.Stat(desktopFile); !os.IsNotExist(err) {
		t.Errorf("%s still exists (stat err %v)", desktopFile, err)
	}
	if _, err := os.Stat(iconFile); !os.IsNotExist(err) {
		t.Errorf("%s still exists (stat err %v)", iconFile, err)
	}
}
