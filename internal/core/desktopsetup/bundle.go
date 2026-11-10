package desktopsetup

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// This file is the macOS counterpart of Install and Uninstall: Emetos's own application bundle, <Name>.app, in the user's
// Applications folder (~/Applications), which Launchpad, Spotlight and the Dock find. Its executable is a shell script that runs the
// program at exe with the arguments it was given, so the bundle stays small, follows the program when it is updated, and is made
// again by running the flag again after the program moved. Its icon is an icns file made of the PNG sizes of IconSizes.

// icnsTypes are the icns chunk types that hold a PNG of a size: icp4 (16), icp5 (32), icp6 (64), ic07 (128), ic08 (256) and
// ic09 (512).
var icnsTypes = map[int]string{16: "icp4", 32: "icp5", 64: "icp6", 128: "ic07", 256: "ic08", 512: "ic09"}

// BundleResult tells where InstallBundle wrote the bundle, or what UninstallBundle removed.
type BundleResult struct {
	// App is the bundle folder; empty when UninstallBundle found none.
	App string
}

// BundleFolderName is the name of the bundle's folder for the application called name: the characters that a file name cannot hold
// are left out.
func BundleFolderName(name string) string {
	clean := strings.NewReplacer("/", "", ":", "", "\x00", "").Replace(name)
	return strings.TrimSpace(clean) + ".app"
}

// InstallBundle writes the application bundle of Emetos, called name, under dir (the Applications folder of the user), replacing the
// one of an earlier call. It is built in a folder of its own beside the target and moved into place, so Launchpad never reads half a
// bundle. exe must be an absolute path.
//
// The tags are InstallExeNotAbsolute, BundleCanceled, BundleDirCreateFailed, BundleFileWriteFailed and BundleReplaceFailed.
func InstallBundle(ctx context.Context, dir, appID, name string, icons Icons, exe string) (BundleResult, error) {
	if !filepath.IsAbs(exe) {
		return BundleResult{}, errors.New("InstallExeNotAbsolute", "the program's path is not absolute", "exe", exe)
	}
	if err := ctx.Err(); err != nil {
		return BundleResult{}, errors.NewErr("BundleCanceled", err).SetMetadata("dir", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return BundleResult{}, errors.NewErr("BundleDirCreateFailed", err).SetMetadata("dir", dir)
	}
	stage, err := os.MkdirTemp(dir, ".emetos-bundle-*")
	if err != nil {
		return BundleResult{}, errors.NewErr("BundleDirCreateFailed", err).SetMetadata("dir", dir)
	}
	defer func() { _ = os.RemoveAll(stage) }() // what is left after a failure; nothing after the move
	folder := BundleFolderName(name)
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{filepath.Join(stage, folder, "Contents", "Resources", appID+".icns"), icns(icons), 0o644},
		{filepath.Join(stage, folder, "Contents", "Info.plist"), []byte(infoPlist(appID, name)), 0o644},
		{filepath.Join(stage, folder, "Contents", "MacOS", appID), []byte(launcherScript(exe)), 0o755},
	}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return BundleResult{}, errors.NewErr("BundleCanceled", err).SetMetadata("path", f.path)
		}
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return BundleResult{}, errors.NewErr("BundleDirCreateFailed", err).SetMetadata("dir", filepath.Dir(f.path))
		}
		if err := os.WriteFile(f.path, f.data, f.mode); err != nil {
			return BundleResult{}, errors.NewErr("BundleFileWriteFailed", err).SetMetadata("path", f.path)
		}
	}
	app := filepath.Join(dir, folder)
	if err := os.RemoveAll(app); err != nil {
		return BundleResult{}, errors.NewErr("BundleReplaceFailed", err).SetMetadata("path", app)
	}
	if err := os.Rename(filepath.Join(stage, folder), app); err != nil {
		return BundleResult{}, errors.NewErr("BundleReplaceFailed", err).SetMetadata("path", app)
	}
	return BundleResult{App: app}, nil
}

// UninstallBundle removes the bundle InstallBundle wrote under dir. A bundle that is not there is not an error: Result.App is then
// empty.
//
// The tags are BundleCanceled and BundleRemoveFailed.
func UninstallBundle(ctx context.Context, dir, name string) (BundleResult, error) {
	app := filepath.Join(dir, BundleFolderName(name))
	if err := ctx.Err(); err != nil {
		return BundleResult{}, errors.NewErr("BundleCanceled", err).SetMetadata("path", app)
	}
	if _, err := os.Lstat(app); err != nil {
		if os.IsNotExist(err) {
			return BundleResult{}, nil
		}
		return BundleResult{}, errors.NewErr("BundleRemoveFailed", err).SetMetadata("path", app)
	}
	if err := os.RemoveAll(app); err != nil {
		return BundleResult{}, errors.NewErr("BundleRemoveFailed", err).SetMetadata("path", app)
	}
	return BundleResult{App: app}, nil
}

// launcherScript is the executable of the bundle: it hands over to the program, with the arguments it was given.
func launcherScript(exe string) string {
	return "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", `'\''`) + "' \"$@\"\n"
}

// infoPlist is the bundle's Info.plist.
func infoPlist(appID, name string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	for _, kv := range [][2]string{
		{"CFBundleName", name}, {"CFBundleDisplayName", name}, {"CFBundleIdentifier", appID}, {"CFBundleExecutable", appID},
		{"CFBundleIconFile", appID}, {"CFBundlePackageType", "APPL"}, {"CFBundleVersion", "1"}, {"CFBundleShortVersionString", "1"},
		{"LSMinimumSystemVersion", "12.0"}, {"LSApplicationCategoryType", "public.app-category.developer-tools"},
	} {
		b.WriteString("\t<key>" + xmlEscape(kv[0]) + "</key>\n\t<string>" + xmlEscape(kv[1]) + "</string>\n")
	}
	b.WriteString("\t<key>NSHighResolutionCapable</key>\n\t<true/>\n</dict>\n</plist>\n")
	return b.String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// icns makes an icns file of the PNG sizes icons has: the type "icns", the length of the file, then a chunk (its type, its length and
// the PNG) for each size, all big-endian.
func icns(icons Icons) []byte {
	var body bytes.Buffer
	for _, size := range IconSizes {
		kind, ok := icnsTypes[size]
		if !ok {
			continue
		}
		png, ok := icons(size)
		if !ok {
			continue
		}
		body.WriteString(kind)
		_ = binary.Write(&body, binary.BigEndian, uint32(8+len(png))) // writing to a buffer does not fail
		body.Write(png)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(8+body.Len())) // the same
	out.Write(body.Bytes())
	return out.Bytes()
}
