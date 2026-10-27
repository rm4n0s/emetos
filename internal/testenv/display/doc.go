// Package display is the harness of the display tests (build tag emetos_display, SPEC §15.1 "Real input"): it starts a debug
// build of Emetos isolated from the user's folders under a compositor, types and clicks into its window through
// internal/testenv/realinput, reads its state through the read-only dev commands over the dev server (SPEC §15.2), takes
// screenshots with grim, and stops everything when the test ends.
//
// Run the tests that use it inside a headless compositor, which gives them WAYLAND_DISPLAY, XDG_RUNTIME_DIR and SWAYSOCK:
//
//	scripts/headless-wayland.sh -- go test -race -tags emetos_display ./cmd/emetos
//
// scripts/realinput.sh --desktop runs them in a nested sway window on the desktop. Without a sway compositor in the
// environment the tests are skipped.
package display
