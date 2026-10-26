package extension

// QuickPickItem is a row of a quick pick.
type QuickPickItem struct {
	// Label is the main text; the filter matches it. A codicon may be written $(name).
	Label string
	// Description is shown after the label in a dimmer color; Detail is shown on a second line below it.
	Description, Detail string
	// Command, when it is not empty, is the id of a command that is run (with Args) when the item is chosen; the done function
	// of Show still gets the index. Use it for a command of the editor, such as workbench.action.openSettings.
	Command string
	Args    any
}

// QuickPickOptions describe a quick pick.
type QuickPickOptions struct {
	// Placeholder is the hint of the input.
	Placeholder string
	// Items are the rows; an item with an empty Label is not shown.
	Items []QuickPickItem
	// MatchOnDescription lets the filter look at the descriptions too.
	MatchOnDescription bool
}

// QuickPick shows a list over the editor to choose one item from, as the command palette does (SPEC §10.3).
type QuickPick interface {
	// Show opens the quick pick. done is called once, on the UI thread, after the list is hidden: with the index in
	// Options.Items of the chosen item, or -1 when it was closed without a choice or another quick input took its place. The
	// command of the item, if it has one, runs before done. It fails with ExtensionQuickPickInvalid when no item has a
	// label, with ExtensionQuickPickUnavailable when the scope cannot show a list (a window tab of an extension has none) and
	// with ExtensionDeactivated after the activation ended; done is not called then.
	Show(opts QuickPickOptions, done func(index int)) error
}
