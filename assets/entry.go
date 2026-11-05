// Package assets holds files embedded into the emetos binary that are not icons (assets/icons holds
// those, in a package of its own because go:embed cannot reach a folder above its package): Emetos's
// own desktop entry.
package assets

import _ "embed"

// desktopEntry is the committed dev.emetos.Emetos.desktop (SPEC §16 "Desktop entry and icon"), the
// same file scripts/install-desktop.sh and the --setup-desktop flag of cmd/emetos install.
//
//go:embed dev.emetos.Emetos.desktop
var desktopEntry []byte

// DesktopEntry is the source of dev.emetos.Emetos.desktop. Every call returns a new copy, so a caller
// may change it freely (desktopsetup.Setup replaces its "Exec=emetos " lines with the running
// program's own path).
func DesktopEntry() []byte {
	b := make([]byte, len(desktopEntry))
	copy(b, desktopEntry)
	return b
}
