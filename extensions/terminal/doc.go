// Package terminal is the integrated terminal (SPEC §10.25, D27, milestone M17d): VS Code's terminal as an extension of
// Emetos, with Emetos's own emulator (package emulator) and none of xterm.js. This step (17d.2) is the panel: shells in
// pseudo-terminals, drawn on a grid with a glyph atlas, with VS Code's commands, keys, settings and colors, several terminals
// with a tabs list and splits.
//
// # Structure
//
//   - A Terminal is the model and is not tied to the panel: it owns the process (a pseudo-terminal), the emulator and the
//     goroutines that read, parse and write, and tells whoever shows it that it changed. The panel shows it today; the editor
//     area (step 17d.7) and the events (step 17d.4) read the same model.
//   - A Group is a tab of the tabs list: one or more terminals side by side (a split).
//   - The widget (view.go, render.go, atlas.go) draws one terminal and turns the keyboard, the pointer and the input method
//     into bytes for it.
//   - The extension (terminal.go) declares the view, the commands, the keys, the settings and the context keys, and keeps the
//     terminals of one window tab.
//
// # Nothing of xterm.js
//
// The user asked, on 2026-10-28, that xterm.js not be used ("dont use xterm.js !!!"). Nothing here is read, ported or copied
// from it, or from the code of any other terminal. The ids, the keys and the defaults of VS Code's terminal come from VS
// Code's own TypeScript outside the folders that build on xterm.js.
package terminal
