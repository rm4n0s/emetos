// Package icons holds the pictures of the application icon (SPEC §10.14, §17) and embeds them in the program.
//
// The pictures are made by the test of internal/gui/kit (go test ./internal/gui/kit -run TestAppIconPNGs
// -update-icons), which draws the icon with Glutter's canvas and renders it at each of kit.AppIconSizes; the same test
// fails when a file here differs from what the drawing gives. The window and the system tray use them, and scripts/install-desktop.sh
// (step 10.2) installs them in the icon theme folders, with ../dev.emetos.Emetos.desktop; the tests of both are in this folder.
package icons
