// Package realinput types and clicks the way a keyboard and a mouse do: it is a Wayland client that makes one virtual
// keyboard (zwp_virtual_keyboard_manager_v1) and one virtual pointer (zwlr_virtual_pointer_manager_v1) on a compositor,
// so that Emetos receives the keys through wl_keyboard and Glutter's key and text input path, and the pointer through
// wl_pointer. The fake platform of gluttertest skips both paths, which is how the bug of 2026-10-27 (an input box that
// kept only its first key) was missed by every widget test (SPEC §15.1 "Real input", step 17b.1).
//
// The package uses the standard library only (and github.com/rm4n0s/errors). A Client talks to the compositor named by
// WAYLAND_DISPLAY; it makes the keyboard once, with a US xkb keymap in a sealed file, and keeps it for its whole life,
// because a compositor that loses its last keyboard sends the no_keymap keymap to the clients, and a client then drops
// keys. Every key and every pointer event is followed by a round trip (wl_display.sync), so that when a method returns
// the compositor has passed the event on.
//
// Positions are window coordinates (logical pixels from the top left of the window's content) once SetWindow has told
// the client where the window is; sway's IPC (SwayWindowOf, SwayLayout) gives that. sway.go speaks i3's IPC protocol over
// $SWAYSOCK directly, so no swaymsg process is needed.
//
// The display tests that use it live under the emetos_display build tag and run under scripts/headless-wayland.sh;
// scripts/realinput.sh runs the same scenarios in a nested sway window on the user's desktop. A client must only be
// pointed at a compositor of its own (a headless or nested sway): never at the user's session.
package realinput
