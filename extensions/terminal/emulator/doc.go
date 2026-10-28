// Package emulator is Emetos's own terminal emulator (SPEC §10.25 "The emulator", D27): the part of a terminal that turns the
// bytes a program writes into a screen of cells, and the keys and mouse events of the user into the bytes the program reads.
// It has no user interface and imports nothing of Glutter or of Emetos.
//
// # Sources
//
// It is written from the published descriptions of the terminal and from nothing else: ECMA-48 (5th edition), the DEC
// VT100, VT220 and VT510 manuals and Paul Williams' state machine of the DEC parser (vt100.net), and "XTerm Control
// Sequences" (the documentation of the X program xterm, called ctlseqs in the comments). It uses nothing of xterm.js, of
// VS Code's terminal code, or of the code of any other terminal (the user, 2026-10-28: "dont use xterm.js !!!"). Every
// handler names the document and the section it follows, for example "ctlseqs: CSI Ps J" or "ECMA-48 8.3.39".
//
// # Use
//
// An Emulator is owned by one goroutine, the one that reads the program's output: it calls Write for every chunk it reads
// and Resize when the window changes. The user interface calls Snapshot once per frame, from another goroutine: the two
// meet in a short lock, and Snapshot copies only the rows that changed since the last call into a Frame it reuses, so an
// idle terminal costs nothing. Everything the program asks for (device status and attribute replies, the title, the
// working folder, the bell) goes out through the Callbacks, which are called by the goroutine that called Write after the
// lock is released.
//
// # Speed
//
// Runs of printable ASCII are written to the cells in one loop with no state machine step per byte; UTF-8 is decoded in
// place; the parameters of a sequence are parsed into a fixed array, so a sequence allocates nothing. A cell is 8 bytes
// (the code point, an index into a table of styles shared by the buffer, and flags). The lines of a buffer live in a ring
// of pages, so scrolling and trimming the scrollback move no cells. Only the rows that changed are copied for the UI.
//
// # Memory
//
// The scrollback has a limit in lines (Options.Scrollback); a line is at most MaxCols cells wide; a line that reflow
// joins is at most MaxLogicalLine cells long; an OSC string is at most MaxOSCLength bytes; a sequence keeps at most
// MaxParams parameters; the table of styles has at most 65535 entries and the table of hyperlinks 65535, and each is
// compacted before it is given up.
package emulator
