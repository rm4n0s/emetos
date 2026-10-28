package theme

// registerTerminalColors registers the colors of the integrated terminal (step 17d.2, SPEC §10.25): its background and
// foreground, the sixteen ANSI colors, the cursor, the selection, the find matches, the borders and the colors of the command
// decorations of shell integration (step 17d.5). The overview ruler colors of VS Code are not registered: the terminal has no
// overview ruler.
//
// Ported from VS Code: src/vs/workbench/contrib/terminal/common/terminalColorRegistry.ts
func registerTerminalColors(r *ColorRegistry) {
	r.MustRegister(
		ColorContribution{ID: "terminal.background", Defaults: AllThemes(nil),
			Description: "The background color of the terminal, this allows coloring the terminal differently to the panel."},
		ColorContribution{ID: "terminal.foreground", Defaults: dflt(Hex("#CCCCCC"), Hex("#333333"), Hex("#FFFFFF"), Hex("#292929")),
			Description: "The foreground color of the terminal."},
		ColorContribution{ID: "terminalCursor.foreground", Defaults: AllThemes(nil),
			Description: "The foreground color of the terminal cursor."},
		ColorContribution{ID: "terminalCursor.background", Defaults: AllThemes(nil),
			Description: "The background color of the terminal cursor. Allows customizing the color of a character overlapped by a block cursor."},
		ColorContribution{ID: "terminal.selectionBackground", Defaults: AllThemes(Ref("editor.selectionBackground")),
			Description: "The selection background color of the terminal."},
		ColorContribution{ID: "terminal.inactiveSelectionBackground",
			Defaults: dflt(Transparent(Ref("terminal.selectionBackground"), 0.5), Transparent(Ref("terminal.selectionBackground"), 0.5),
				Transparent(Ref("terminal.selectionBackground"), 0.7), Transparent(Ref("terminal.selectionBackground"), 0.5)),
			Description: "The selection background color of the terminal when it does not have focus."},
		ColorContribution{ID: "terminal.selectionForeground", Defaults: dflt(nil, nil, Hex("#000000"), Hex("#ffffff")),
			Description: "The selection foreground color of the terminal. When this is null the selection foreground will be retained and have the minimum contrast ratio feature applied."},
		ColorContribution{ID: "terminalCommandDecoration.defaultBackground", Defaults: dflt(Hex("#ffffff40"), Hex("#00000040"), Hex("#ffffff80"), Hex("#00000040")),
			Description: "The default terminal command decoration background color."},
		ColorContribution{ID: "terminalCommandDecoration.successBackground", Defaults: dflt(Hex("#1B81A8"), Hex("#2090D3"), Hex("#1B81A8"), Hex("#007100")),
			Description: "The terminal command decoration background color for successful commands."},
		ColorContribution{ID: "terminalCommandDecoration.errorBackground", Defaults: dflt(Hex("#F14C4C"), Hex("#E51400"), Hex("#F14C4C"), Hex("#B5200D")),
			Description: "The terminal command decoration background color for error commands."},
		ColorContribution{ID: "terminal.border", Defaults: AllThemes(Ref("panel.border")),
			Description: "The color of the border that separates split panes within the terminal. This defaults to panel.border."},
		ColorContribution{ID: "terminal.findMatchBackground", NeedsTransparency: true, Defaults: dflt(Ref("editor.findMatchBackground"), Ref("editor.findMatchBackground"), nil, Hex("#0F4A85")),
			Description: "Color of the current search match in the terminal. The color must not be opaque so as not to hide underlying terminal content."},
		ColorContribution{ID: "terminal.hoverHighlightBackground",
			Defaults:    dflt(Transparent(Hex("#264f7840"), 0.5), Transparent(Hex("#ADD6FF26"), 0.5), Transparent(Hex("#ADD6FF26"), 0.5), nil),
			Description: "Highlight below the word for which a hover is shown. The color must not be opaque so as not to hide underlying decorations."},
		ColorContribution{ID: "terminal.findMatchBorder", Defaults: dflt(nil, nil, Hex("#f38518"), Hex("#0F4A85")),
			Description: "Border color of the current search match in the terminal."},
		ColorContribution{ID: "terminal.findMatchHighlightBackground", NeedsTransparency: true,
			Defaults:    dflt(Ref("editor.findMatchHighlightBackground"), Ref("editor.findMatchHighlightBackground"), nil, nil),
			Description: "Color of the other search matches in the terminal. The color must not be opaque so as not to hide underlying terminal content."},
		ColorContribution{ID: "terminal.findMatchHighlightBorder", Defaults: dflt(nil, nil, Hex("#f38518"), Hex("#0F4A85")),
			Description: "Border color of the other search matches in the terminal."},
		ColorContribution{ID: "terminal.dropBackground", NeedsTransparency: true, Defaults: AllThemes(Ref("editorGroup.dropBackground")),
			Description: "Background color when dragging on top of terminals. The color should have transparency so that the terminal contents can still shine through."},
		ColorContribution{ID: "terminal.tab.activeBorder", Defaults: AllThemes(Ref("tab.activeBorder")),
			Description: "Border on the side of the terminal tab in the panel. This defaults to tab.activeBorder."},
		ColorContribution{ID: "terminal.initialHintForeground", Defaults: dflt(Hex("#ffffff56"), Hex("#0007"), nil, nil),
			Description: "Foreground color of the terminal initial hint."},
	)
	for _, c := range terminalAnsiColors {
		r.MustRegister(ColorContribution{ID: c.id, Defaults: dflt(Hex(c.dark), Hex(c.light), Hex(c.hcDark), Hex(c.hcLight)),
			Description: "'" + c.id[len("terminal.ansi"):] + "' ANSI color in the terminal."})
	}
}

// terminalAnsiColors are the sixteen ANSI colors of VS Code's terminal in the order of the palette: the ids and their defaults
// for the dark, light, high contrast dark and high contrast light themes.
var terminalAnsiColors = [16]struct{ id, dark, light, hcDark, hcLight string }{
	{"terminal.ansiBlack", "#000000", "#000000", "#000000", "#292929"},
	{"terminal.ansiRed", "#cd3131", "#cd3131", "#cd0000", "#cd3131"},
	{"terminal.ansiGreen", "#0DBC79", "#107C10", "#00cd00", "#136C13"},
	{"terminal.ansiYellow", "#e5e510", "#949800", "#cdcd00", "#949800"},
	{"terminal.ansiBlue", "#2472c8", "#0451a5", "#0000ee", "#0451a5"},
	{"terminal.ansiMagenta", "#bc3fbc", "#bc05bc", "#cd00cd", "#bc05bc"},
	{"terminal.ansiCyan", "#11a8cd", "#0598bc", "#00cdcd", "#0598bc"},
	{"terminal.ansiWhite", "#e5e5e5", "#555555", "#e5e5e5", "#555555"},
	{"terminal.ansiBrightBlack", "#666666", "#666666", "#7f7f7f", "#666666"},
	{"terminal.ansiBrightRed", "#f14c4c", "#f14c4c", "#ff0000", "#cd3131"},
	{"terminal.ansiBrightGreen", "#23d18b", "#14CE14", "#00ff00", "#00bc00"},
	{"terminal.ansiBrightYellow", "#f5f543", "#b5ba00", "#ffff00", "#b5ba00"},
	{"terminal.ansiBrightBlue", "#3b8eea", "#3b8eea", "#5c5cff", "#0451a5"},
	{"terminal.ansiBrightMagenta", "#d670d6", "#d670d6", "#ff00ff", "#bc05bc"},
	{"terminal.ansiBrightCyan", "#29b8db", "#29b8db", "#00ffff", "#0598bc"},
	{"terminal.ansiBrightWhite", "#e5e5e5", "#a5a5a5", "#ffffff", "#a5a5a5"},
}

// TerminalANSIColorIDs are the ids of the sixteen ANSI colors of the terminal, in the order of the palette.
func TerminalANSIColorIDs() [16]string {
	var out [16]string
	for i, c := range terminalAnsiColors {
		out[i] = c.id
	}
	return out
}
