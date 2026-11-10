package desktopsetup

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func bundleIcons(size int) ([]byte, bool) {
	if size == 48 || size == 22 { // sizes that icns has no chunk for, or that are missing
		return []byte("png48"), true
	}
	if size == 512 {
		return nil, false
	}
	return []byte("png" + strings.Repeat("x", size/16)), true
}

func TestInstallBundle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Applications")
	res, err := InstallBundle(context.Background(), dir, "dev.x.emetos", "Emetos", bundleIcons, "/Users/ann/my apps/it's/emetos")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "Emetos.app"); res.App != want {
		t.Errorf("bundle at %s, want %s", res.App, want)
	}
	script, err := os.ReadFile(filepath.Join(res.App, "Contents", "MacOS", "dev.x.emetos"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "#!/bin/sh\nexec '/Users/ann/my apps/it'\\''s/emetos' \"$@\"\n"; string(script) != want {
		t.Errorf("launcher %q, want %q", script, want)
	}
	if st, err := os.Stat(filepath.Join(res.App, "Contents", "MacOS", "dev.x.emetos")); err != nil || st.Mode().Perm() != 0o755 {
		t.Errorf("launcher mode %v (%v), want 0755", st, err)
	}
	plist, err := os.ReadFile(filepath.Join(res.App, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<key>CFBundleIdentifier</key>\n\t<string>dev.x.emetos</string>", "<key>CFBundleExecutable</key>\n\t<string>dev.x.emetos</string>", "<key>CFBundleIconFile</key>\n\t<string>dev.x.emetos</string>"} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("Info.plist lacks %q", want)
		}
	}
	icon, err := os.ReadFile(filepath.Join(res.App, "Contents", "Resources", "dev.x.emetos.icns"))
	if err != nil {
		t.Fatal(err)
	}
	if string(icon[:4]) != "icns" || binary.BigEndian.Uint32(icon[4:8]) != uint32(len(icon)) {
		t.Fatalf("icns header %q", icon[:8])
	}
	// the chunks: 16, 32, 64, 128 and 256 are there, 512 is missing
	var kinds []string
	for rest := icon[8:]; len(rest) > 0; {
		n := binary.BigEndian.Uint32(rest[4:8])
		kinds = append(kinds, string(rest[:4]))
		rest = rest[n:]
	}
	if got := strings.Join(kinds, ","); got != "icp4,icp5,icp6,ic07,ic08" {
		t.Errorf("chunks %s", got)
	}
	// a second call replaces the bundle and leaves nothing else in the folder
	if _, err := InstallBundle(context.Background(), dir, "dev.x.emetos", "Emetos", bundleIcons, "/bin/emetos2"); err != nil {
		t.Fatal(err)
	}
	if left, err := os.ReadDir(dir); err != nil || len(left) != 1 {
		t.Errorf("folder holds %v (%v)", left, err)
	}
	script, _ = os.ReadFile(filepath.Join(res.App, "Contents", "MacOS", "dev.x.emetos"))
	if !strings.Contains(string(script), "/bin/emetos2") {
		t.Errorf("launcher after the second call %q", script)
	}
}

func TestUninstallBundle(t *testing.T) {
	dir := t.TempDir()
	res, err := UninstallBundle(context.Background(), dir, "Emetos")
	if err != nil || res.App != "" {
		t.Fatalf("nothing to remove: %+v, %v", res, err)
	}
	if _, err := InstallBundle(context.Background(), dir, "dev.x.emetos", "Emetos", bundleIcons, "/bin/emetos"); err != nil {
		t.Fatal(err)
	}
	res, err = UninstallBundle(context.Background(), dir, "Emetos")
	if err != nil || res.App != filepath.Join(dir, "Emetos.app") {
		t.Fatalf("removed: %+v, %v", res, err)
	}
	if _, err := os.Stat(res.App); !os.IsNotExist(err) {
		t.Errorf("the bundle is still there: %v", err)
	}
}

func TestBundleFailures(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	fileInTheWay := func(t *testing.T) string {
		t.Helper()
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(file, "Applications")
	}
	readOnly := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		return dir
	}
	tests := []struct {
		name      string
		run       func(t *testing.T) error
		wantRoute string
		notRoot   bool
	}{
		{"exe not absolute", func(t *testing.T) error {
			_, err := InstallBundle(context.Background(), t.TempDir(), "a", "A", bundleIcons, "emetos")
			return err
		}, "InstallBundle.InstallExeNotAbsolute", false},
		{"canceled", func(t *testing.T) error {
			_, err := InstallBundle(canceled, t.TempDir(), "a", "A", bundleIcons, "/bin/emetos")
			return err
		}, "InstallBundle.BundleCanceled", false},
		{"the folder cannot be made", func(t *testing.T) error {
			_, err := InstallBundle(context.Background(), fileInTheWay(t), "a", "A", bundleIcons, "/bin/emetos")
			return err
		}, "InstallBundle.BundleDirCreateFailed", false},
		{"the staging folder cannot be made", func(t *testing.T) error {
			_, err := InstallBundle(context.Background(), readOnly(t), "a", "A", bundleIcons, "/bin/emetos")
			return err
		}, "InstallBundle.BundleDirCreateFailed", true},
		{"a file cannot be written", func(t *testing.T) error {
			// a file name of 258 bytes is longer than the file systems allow, and the folders of the bundle are made without trouble
			_, err := InstallBundle(context.Background(), t.TempDir(), strings.Repeat("a", 252), "A", bundleIcons, "/bin/emetos")
			return err
		}, "InstallBundle.BundleFileWriteFailed", false},
		{"the old bundle cannot be replaced", func(t *testing.T) error {
			dir := t.TempDir()
			old := filepath.Join(dir, "A.app", "keep")
			if err := os.MkdirAll(old, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(old, "x"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(old, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(old, 0o700) })
			_, err := InstallBundle(context.Background(), dir, "a", "A", bundleIcons, "/bin/emetos")
			return err
		}, "InstallBundle.BundleReplaceFailed", true},
		{"uninstall canceled", func(t *testing.T) error {
			_, err := UninstallBundle(canceled, t.TempDir(), "A")
			return err
		}, "UninstallBundle.BundleCanceled", false},
		{"uninstall cannot look", func(t *testing.T) error {
			_, err := UninstallBundle(context.Background(), fileInTheWay(t), "A")
			return err
		}, "UninstallBundle.BundleRemoveFailed", false},
		{"uninstall cannot remove", func(t *testing.T) error {
			dir := t.TempDir()
			old := filepath.Join(dir, "A.app", "keep")
			if err := os.MkdirAll(old, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(old, "x"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(old, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(old, 0o700) })
			_, err := UninstallBundle(context.Background(), dir, "A")
			return err
		}, "UninstallBundle.BundleRemoveFailed", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.notRoot && os.Geteuid() == 0 {
				t.Skip("root ignores the permissions")
			}
			err := tt.run(t)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.wantRoute) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
