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
	// Separator makes the item a heading: only its Label is used, it is drawn at the right of the next item that is shown, with a line
	// above that item, and it cannot be chosen (VS Code's QuickPickItemKind.Separator). Its index is never given to done.
	Separator bool
	// AlwaysShow keeps the item in the list when the text typed in the filter does not match it (VS Code's alwaysShow): the entries
	// that make something new, such as "Create new branch...".
	AlwaysShow bool
}

// QuickPickOptions describe a quick pick.
type QuickPickOptions struct {
	// Title is a line of text centered above the input (VS Code's title); none when empty.
	Title string
	// Placeholder is the hint of the input.
	Placeholder string
	// Items are the rows; an item with an empty Label is not shown.
	Items []QuickPickItem
	// MatchOnDescription lets the filter look at the descriptions too.
	MatchOnDescription bool
	// IgnoreFocusOut keeps the list open when the user clicks elsewhere or the window loses the focus, as VS Code's ignoreFocusOut does:
	// only a choice and Escape end it. A question that a running program waits for (ssh's host key) sets it.
	IgnoreFocusOut bool
	// OnAccept, when set, is called on the UI thread just before done, when an item was chosen, with the text that was typed in the
	// filter at that moment (VS Code's quickPick.value). The checkout picker uses it: the text typed beside "Create new branch..." is
	// the name of the branch.
	OnAccept func(value string)
	// Origin is the screen that shows the list; the zero value is the origin of the command being handled (see InputBoxOptions.Origin).
	Origin Origin
}

// QuickPick shows a list over the editor to choose one item from, as the command palette does (SPEC §10.3).
type QuickPick interface {
	// Show opens the quick pick. done is called once, on the UI thread, after the list is hidden: with the index in
	// Options.Items of the chosen item, or -1 when it was closed without a choice or another quick input took its place. The
	// command of the item, if it has one, runs before done. It fails with ExtensionQuickPickInvalid when no item that is not a
	// separator has a label, with ExtensionQuickPickUnavailable when the scope cannot show a list (a window tab of an extension has none) and
	// with ExtensionDeactivated after the activation ended; done is not called then.
	Show(opts QuickPickOptions, done func(index int)) error
}
