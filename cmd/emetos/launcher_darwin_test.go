//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
	"github.com/rm4n0s/errors"
)

// TestRunInstallAndUninstallLauncher is the end-to-end test of runInstallLauncher and runUninstallLauncher themselves (the real
// implementations, not fakes): install writes Emetos.app under ~/Applications of an isolated $HOME, naming the test binary
// (os.Executable) as the program, and uninstall removes exactly that.
func TestRunInstallAndUninstallLauncher(t *testing.T) {
	storagetest.Isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	te := newTestEnv(t, t.TempDir())
	te.installLauncher, te.uninstallLauncher = runInstallLauncher, runUninstallLauncher

	if got := run([]string{"--install-launcher"}, te.env); got != 0 {
		t.Fatalf("install: status = %d, stderr %q", got, te.stderr.String())
	}
	bundle := filepath.Join(home, "Applications", "Emetos.app")
	if !strings.Contains(te.stdout.String(), "Wrote "+bundle) {
		t.Errorf("stdout = %q, want it to say it wrote %s", te.stdout.String(), bundle)
	}
	script, err := os.ReadFile(filepath.Join(bundle, "Contents", "MacOS", "dev.emetos.Emetos"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "exec '/") {
		t.Errorf("the launcher does not run a program by its path: %q", script)
	}
	if _, err := os.Stat(filepath.Join(bundle, "Contents", "Resources", "dev.emetos.Emetos.icns")); err != nil {
		t.Errorf("the icon was not written: %v", err)
	}

	te.stdout.Reset()
	if got := run([]string{"--uninstall-launcher"}, te.env); got != 0 {
		t.Fatalf("uninstall: status = %d, stderr %q", got, te.stderr.String())
	}
	if !strings.Contains(te.stdout.String(), "Removed "+bundle) {
		t.Errorf("stdout = %q, want it to say it removed %s", te.stdout.String(), bundle)
	}
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Errorf("%s still exists (stat err %v)", bundle, err)
	}
	te.stdout.Reset()
	if got := run([]string{"--uninstall-launcher"}, te.env); got != 0 || !strings.Contains(te.stdout.String(), "Nothing to remove") {
		t.Errorf("a second uninstall: status %d, stdout %q", got, te.stdout.String())
	}
}

func TestApplicationsDirNeedsAnAbsoluteHome(t *testing.T) {
	t.Setenv("HOME", "relative")
	_, err := applicationsDir()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("applicationsDir.LauncherHomeUnknown") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
