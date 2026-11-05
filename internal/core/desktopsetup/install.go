// Package desktopsetup writes and removes Emetos's own desktop entry and icon theme files, so that
// application menus and launchers show it (SPEC §16 "Desktop entry and icon"): the same files
// scripts/install-desktop.sh writes or removes, for the --install-desktop and --uninstall-desktop
// flags of cmd/emetos (SPEC §10.11), so that a user without a shell, or on a launcher whose PATH
// lacks go install's folder, can still add Emetos to their menu, or remove it, with the program
// itself.
package desktopsetup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/storage"
)

// IconSizes are the icon sizes Install writes and Uninstall removes: the sizes assets/icons embeds,
// which are kit.AppIconSizes (internal/gui/kit, which desktopsetup, a core package, never imports).
var IconSizes = []int{16, 22, 24, 32, 48, 64, 128, 256, 512}

// Icons gives the PNG bytes of Emetos's own icon at size, and whether there is one. It is
// assets/icons.PNG, taken as a parameter so this package stays free of that package's embedded
// data and is easy to test with fakes.
type Icons func(size int) (data []byte, ok bool)

// Result tells which of Emetos's files Install wrote, or Uninstall removed.
type Result struct {
	// DesktopFile is the desktop entry under dirs.DataHome/applications. Empty when Uninstall found
	// none there.
	DesktopFile string
	// IconFiles are the icon files under dirs.DataHome/icons/hicolor, one per size in IconSizes that
	// Install had a picture for, or that Uninstall found there.
	IconFiles []string
}

// execLinePrefix is the start of a line of the desktop entry that names the program to run:
// "Exec=emetos " starts both the main entry and its "New Workspace Tab" action.
const execLinePrefix = "Exec=emetos "

// safeExe is the set of characters Install accepts unescaped in exe (a path with a space or a
// character the Exec key of a desktop entry treats as special would need quoting rules that are
// not worth carrying here; scripts/install-desktop.sh applies the same restriction).
var safeExe = regexp.MustCompile(`^/[A-Za-z0-9_./+,:@=-]*$`)

// Install writes Emetos's own desktop entry to <dirs.DataHome>/applications/<appID>.desktop, and its
// icon to <dirs.DataHome>/icons/hicolor/<N>x<N>/apps/<appID>.png for each size in IconSizes that
// icons has a picture for. template is the desktop entry as committed (assets.DesktopEntry()); every
// line that starts with "Exec=emetos " has that program replaced by exe, the absolute path of the
// running program. Missing folders are made with mode 0755 and every file gets mode 0644, each
// written through a temporary file renamed over the target, so a second call replaces the files of
// an earlier one. Install runs no command (scripts/install-desktop.sh also refreshes
// update-desktop-database and gtk-update-icon-cache when they are installed; a desktop environment
// picks up a new or changed file under $XDG_DATA_HOME on its own, a little later, without them).
//
// The tags are InstallExeNotAbsolute, InstallExeUnsafe, InstallCanceled, InstallDirCreateFailed and
// InstallFileWriteFailed.
func Install(ctx context.Context, dirs storage.Paths, appID string, template []byte, icons Icons, exe string) (Result, error) {
	if !filepath.IsAbs(exe) {
		return Result{}, errors.New("InstallExeNotAbsolute", "the program's path is not absolute", "exe", exe)
	}
	if !safeExe.MatchString(exe) {
		return Result{}, errors.New("InstallExeUnsafe",
			"the program's path must be made of letters, digits and _ . / + , : @ = - only", "exe", exe)
	}
	entry := bytes.ReplaceAll(template, []byte(execLinePrefix), []byte("Exec="+exe+" "))

	type file struct {
		path string
		data []byte
	}
	files := []file{{path: filepath.Join(dirs.DataHome, "applications", appID+".desktop"), data: entry}}
	for _, size := range IconSizes {
		png, ok := icons(size)
		if !ok {
			continue
		}
		dir := strconv.Itoa(size) + "x" + strconv.Itoa(size)
		files = append(files, file{path: filepath.Join(dirs.DataHome, "icons", "hicolor", dir, "apps", appID+".png"), data: png})
	}

	var res Result
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return Result{}, errors.NewErr("InstallCanceled", err).SetMetadata("path", f.path)
		}
		if err := ensureDir(filepath.Dir(f.path), 0o755); err != nil {
			return Result{}, err
		}
		if err := writeAtomic(f.path, f.data, 0o644); err != nil {
			return Result{}, err
		}
		if i == 0 {
			res.DesktopFile = f.path
		} else {
			res.IconFiles = append(res.IconFiles, f.path)
		}
	}
	return res, nil
}

// Uninstall removes the files Install (or scripts/install-desktop.sh) wrote: the same
// <dirs.DataHome>/applications/<appID>.desktop and <dirs.DataHome>/icons/hicolor/<N>x<N>/apps/<appID>.png
// for each size in IconSizes. A file that is not there is not an error, and Result lists only the
// ones that existed and were removed. An icons/hicolor/<N>x<N> or its apps folder that Uninstall
// leaves empty is removed too, so a hicolor icon theme with other icons in it stays; that cleanup
// never fails Uninstall.
//
// The tags are UninstallCanceled and UninstallRemoveFailed.
func Uninstall(ctx context.Context, dirs storage.Paths, appID string) (Result, error) {
	var res Result
	desktopPath := filepath.Join(dirs.DataHome, "applications", appID+".desktop")
	if err := ctx.Err(); err != nil {
		return Result{}, errors.NewErr("UninstallCanceled", err).SetMetadata("path", desktopPath)
	}
	removed, err := removeIfExists(desktopPath)
	if err != nil {
		return Result{}, err
	}
	if removed {
		res.DesktopFile = desktopPath
	}

	for _, size := range IconSizes {
		if err := ctx.Err(); err != nil {
			return Result{}, errors.NewErr("UninstallCanceled", err).SetMetadata("path", desktopPath)
		}
		dim := strconv.Itoa(size) + "x" + strconv.Itoa(size)
		appsDir := filepath.Join(dirs.DataHome, "icons", "hicolor", dim, "apps")
		iconPath := filepath.Join(appsDir, appID+".png")
		removed, err := removeIfExists(iconPath)
		if err != nil {
			return Result{}, err
		}
		if removed {
			res.IconFiles = append(res.IconFiles, iconPath)
		}
		// A folder left empty by this removal is cleaned up; one that still holds other icons, or
		// never existed, is left alone, and either way Uninstall still succeeds.
		_ = os.Remove(appsDir)
		_ = os.Remove(filepath.Join(dirs.DataHome, "icons", "hicolor", dim))
	}
	return res, nil
}

// removeIfExists removes path and reports whether it was there. A missing file is not an error.
func removeIfExists(path string) (bool, error) {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.NewErr("UninstallRemoveFailed", err).SetMetadata("path", path)
	}
	return true, nil
}

// ensureDir makes dir, and every missing parent, with mode.
func ensureDir(dir string, mode os.FileMode) error {
	if err := os.MkdirAll(dir, mode); err != nil {
		return errors.NewErr("InstallDirCreateFailed", err).SetMetadata("dir", dir)
	}
	return nil
}

// writeAtomic writes data to path with mode, through a temporary file in the same folder renamed
// over the target, so a reader never sees a partial file and a second call replaces the first.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".emetos-setup-*")
	if err != nil {
		return errors.NewErr("InstallFileWriteFailed", err).SetMetadata("path", path)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return errors.NewErr("InstallFileWriteFailed", err).SetMetadata("path", path)
	}
	if err := tmp.Close(); err != nil {
		return errors.NewErr("InstallFileWriteFailed", err).SetMetadata("path", path)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return errors.NewErr("InstallFileWriteFailed", err).SetMetadata("path", path)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.NewErr("InstallFileWriteFailed", err).SetMetadata("path", path)
	}
	ok = true
	return nil
}
