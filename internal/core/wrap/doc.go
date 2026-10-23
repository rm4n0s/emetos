// Package wrap computes where word wrap breaks a line into rows (SPEC §9.2): the algorithm of
// VS Code's monospace line breaks computer, which counts columns, for lines whose characters have
// the width of one or two columns, and the helpers the editor uses for the other lines, which it
// breaks by laying the text out. It has no dependency on the UI.
//
// Ported from VS Code: src/vs/editor/common/viewModel/monospaceLineBreaksComputer.ts
package wrap
