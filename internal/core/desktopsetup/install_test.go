package desktopsetup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

const (
	testAppID = "dev.emetos.Emetos"
	testEntry = "[Desktop Entry]\nType=Application\nExec=emetos %F\nIcon=dev.emetos.Emetos\n\n[Desktop Action new-workspace-tab]\nExec=emetos -n\n"
	testPNG16 = "16-png-bytes"
	testPNG22 = "22-png-bytes"
)

// fakeIcons answers only the sizes given.
func fakeIcons(sizes map[int]string) Icons {
	return func(size int) ([]byte, bool) {
		data, ok := sizes[size]
		if !ok {
			return nil, false
		}
		return []byte(data), true
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func requireRoute(t *testing.T, err error, tag, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if appErr.Tag != tag {
		t.Errorf("Tag = %q, want %q", appErr.Tag, tag)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("Route() = %q, want it to contain %q", appErr.Route(), route)
	}
}

func TestInstallWritesTheDesktopFileAndTheIcons(t *testing.T) {
	dirs := storagetest.Isolate(t)
	res, err := Install(context.Background(), dirs, testAppID, []byte(testEntry), fakeIcons(map[int]string{16: testPNG16, 22: testPNG22}), "/opt/emetos/emetos")
	if err != nil {
		t.Fatal(err)
	}
	wantDesktop := filepath.Join(dirs.DataHome, "applications", testAppID+".desktop")
	wantIcon16 := filepath.Join(dirs.DataHome, "icons", "hicolor", "16x16", "apps", testAppID+".png")
	wantIcon22 := filepath.Join(dirs.DataHome, "icons", "hicolor", "22x22", "apps", testAppID+".png")
	if res.DesktopFile != wantDesktop {
		t.Errorf("DesktopFile = %q, want %q", res.DesktopFile, wantDesktop)
	}
	if len(res.IconFiles) != 2 || res.IconFiles[0] != wantIcon16 || res.IconFiles[1] != wantIcon22 {
		t.Errorf("IconFiles = %v, want [%q %q]", res.IconFiles, wantIcon16, wantIcon22)
	}

	entry := readFile(t, res.DesktopFile)
	if !strings.Contains(entry, "Exec=/opt/emetos/emetos %F\n") {
		t.Errorf("the main Exec line was not replaced: %q", entry)
	}
	if !strings.Contains(entry, "Exec=/opt/emetos/emetos -n\n") {
		t.Errorf("the action's Exec line was not replaced: %q", entry)
	}
	if strings.Contains(entry, "Exec=emetos") {
		t.Errorf("a plain Exec line is still there: %q", entry)
	}
	if got := readFile(t, wantIcon16); got != testPNG16 {
		t.Errorf("the 16px icon = %q, want %q", got, testPNG16)
	}
	if got := readFile(t, wantIcon22); got != testPNG22 {
		t.Errorf("the 22px icon = %q, want %q", got, testPNG22)
	}

	for _, p := range append([]string{res.DesktopFile}, res.IconFiles...) {
		if m := fileMode(t, p); m != 0o644 {
			t.Errorf("%s has mode %v, want 0644", p, m)
		}
	}
	for _, d := range []string{
		filepath.Join(dirs.DataHome, "applications"),
		filepath.Join(dirs.DataHome, "icons", "hicolor", "16x16", "apps"),
	} {
		if m := fileMode(t, d); m != 0o755 {
			t.Errorf("%s has mode %v, want 0755", d, m)
		}
	}
}

func TestInstallSkipsASizeIconsHasNone(t *testing.T) {
	dirs := storagetest.Isolate(t)
	res, err := Install(context.Background(), dirs, testAppID, []byte(testEntry), fakeIcons(map[int]string{16: testPNG16}), "/usr/bin/emetos")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.IconFiles) != 1 {
		t.Errorf("IconFiles = %v, want exactly the one size fakeIcons answered", res.IconFiles)
	}
}

func TestInstallTwiceReplacesTheFiles(t *testing.T) {
	dirs := storagetest.Isolate(t)
	ctx := context.Background()
	icons := fakeIcons(map[int]string{16: testPNG16})
	if _, err := Install(ctx, dirs, testAppID, []byte(testEntry), icons, "/old/place/emetos"); err != nil {
		t.Fatal(err)
	}
	res, err := Install(ctx, dirs, testAppID, []byte(testEntry), icons, "/new/place/emetos")
	if err != nil {
		t.Fatal(err)
	}
	got := readFile(t, res.DesktopFile)
	if !strings.Contains(got, "/new/place/emetos") || strings.Contains(got, "/old/") {
		t.Errorf("the entry was not replaced: %q", got)
	}
	// No temporary file is left behind.
	for _, d := range []string{
		filepath.Join(dirs.DataHome, "applications"),
		filepath.Join(dirs.DataHome, "icons", "hicolor", "16x16", "apps"),
	} {
		list, err := os.ReadDir(d)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 {
			t.Errorf("%s has %d entries, want 1: %v", d, len(list), list)
		}
	}
}

func TestInstallErrors(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name  string
		ctx   context.Context
		exe   string
		tag   string
		route string
	}{
		{"relative path", context.Background(), "emetos", "InstallExeNotAbsolute", "Install.InstallExeNotAbsolute"},
		{"a space", context.Background(), "/opt/my apps/emetos", "InstallExeUnsafe", "Install.InstallExeUnsafe"},
		{"a quote", context.Background(), `/opt/a"b/emetos`, "InstallExeUnsafe", "Install.InstallExeUnsafe"},
		{"canceled", canceled, "/usr/bin/emetos", "InstallCanceled", "Install.InstallCanceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := storagetest.Isolate(t)
			_, err := Install(tt.ctx, dirs, testAppID, []byte(testEntry), fakeIcons(nil), tt.exe)
			if err == nil {
				t.Fatal("Install succeeded")
			}
			requireRoute(t, err, tt.tag, tt.route)
		})
	}
}

func TestInstallFailsOnAnUnwritableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores file permissions")
	}
	dirs := storagetest.Isolate(t)
	apps := filepath.Join(dirs.DataHome, "applications")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(apps, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(apps, 0o755) })
	if f, err := os.CreateTemp(apps, "probe"); err == nil {
		_ = f.Close()
		t.Skip("the folder is writable despite mode 0500")
	}
	_, err := Install(context.Background(), dirs, testAppID, []byte(testEntry), fakeIcons(nil), "/usr/bin/emetos")
	if err == nil {
		t.Fatal("Install succeeded")
	}
	requireRoute(t, err, "InstallFileWriteFailed", "Install->writeAtomic.InstallFileWriteFailed")
}

func TestInstallFailsWhenAFileIsWhereAFolderShouldBe(t *testing.T) {
	dirs := storagetest.Isolate(t)
	if err := os.WriteFile(filepath.Join(dirs.DataHome, "applications"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Install(context.Background(), dirs, testAppID, []byte(testEntry), fakeIcons(nil), "/usr/bin/emetos")
	if err == nil {
		t.Fatal("Install succeeded")
	}
	requireRoute(t, err, "InstallDirCreateFailed", "Install->ensureDir.InstallDirCreateFailed")
}

func TestUninstallRemovesWhatInstallWrote(t *testing.T) {
	dirs := storagetest.Isolate(t)
	ctx := context.Background()
	installed, err := Install(ctx, dirs, testAppID, []byte(testEntry), fakeIcons(map[int]string{16: testPNG16, 22: testPNG22}), "/usr/bin/emetos")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Uninstall(ctx, dirs, testAppID)
	if err != nil {
		t.Fatal(err)
	}
	if res.DesktopFile != installed.DesktopFile {
		t.Errorf("DesktopFile = %q, want %q", res.DesktopFile, installed.DesktopFile)
	}
	if len(res.IconFiles) != 2 || res.IconFiles[0] != installed.IconFiles[0] || res.IconFiles[1] != installed.IconFiles[1] {
		t.Errorf("IconFiles = %v, want %v", res.IconFiles, installed.IconFiles)
	}
	for _, p := range append([]string{res.DesktopFile}, res.IconFiles...) {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists (stat err %v)", p, err)
		}
	}
	// The emptied hicolor size folders are cleaned up, but the icon theme's own root stays (it may
	// hold icons of other sizes, or other themes' folders).
	for _, size := range []string{"16x16", "22x22"} {
		dir := filepath.Join(dirs.DataHome, "icons", "hicolor", size)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s still exists (stat err %v)", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dirs.DataHome, "icons", "hicolor")); err != nil {
		t.Errorf("the hicolor theme folder itself was removed: %v", err)
	}
}

func TestUninstallOfNothingSucceeds(t *testing.T) {
	dirs := storagetest.Isolate(t)
	res, err := Uninstall(context.Background(), dirs, testAppID)
	if err != nil {
		t.Fatal(err)
	}
	if res.DesktopFile != "" || len(res.IconFiles) != 0 {
		t.Errorf("Result = %+v, want nothing removed", res)
	}
}

func TestUninstallKeepsAFolderThatStillHasOtherIcons(t *testing.T) {
	dirs := storagetest.Isolate(t)
	ctx := context.Background()
	if _, err := Install(ctx, dirs, testAppID, []byte(testEntry), fakeIcons(map[int]string{16: testPNG16}), "/usr/bin/emetos"); err != nil {
		t.Fatal(err)
	}
	appsDir := filepath.Join(dirs.DataHome, "icons", "hicolor", "16x16", "apps")
	other := filepath.Join(appsDir, "other-app.png")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(ctx, dirs, testAppID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("another app's icon was removed too: %v", err)
	}
	if _, err := os.Stat(appsDir); err != nil {
		t.Errorf("the still-used apps folder was removed: %v", err)
	}
}

func TestUninstallErrors(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	dirs := storagetest.Isolate(t)
	_, err := Uninstall(canceled, dirs, testAppID)
	if err == nil {
		t.Fatal("Uninstall succeeded")
	}
	requireRoute(t, err, "UninstallCanceled", "Uninstall.UninstallCanceled")
}

func TestUninstallFailsWhenTheFileCannotBeRemoved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores file permissions")
	}
	dirs := storagetest.Isolate(t)
	apps := filepath.Join(dirs.DataHome, "applications")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	desktop := filepath.Join(apps, testAppID+".desktop")
	if err := os.WriteFile(desktop, []byte(testEntry), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(apps, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(apps, 0o755) })
	if err := os.Remove(filepath.Join(apps, "probe-remove")); err == nil || !os.IsPermission(err) {
		t.Skip("the folder does not refuse removals despite mode 0500")
	}
	_, err := Uninstall(context.Background(), dirs, testAppID)
	if err == nil {
		t.Fatal("Uninstall succeeded")
	}
	requireRoute(t, err, "UninstallRemoveFailed", "Uninstall->removeIfExists.UninstallRemoveFailed")
}
