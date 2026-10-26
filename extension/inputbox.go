package extension

// InputBoxOptions describe an input box.
type InputBoxOptions struct {
	// Prompt says what to enter; it is the hint of the input while it is empty (VS Code's prompt and placeHolder in one).
	Prompt string
	// Value is the text the input starts with; the caret is at its end.
	Value string
	// Validate, when set, checks the text on every change and when it is accepted: a message that is not empty is shown under
	// the input as an error and the text is not accepted (VS Code's validateInput). It is called on the UI thread.
	Validate func(value string) string
}

// InputBox asks the user for a line of text in the quick input of the window (SPEC §10.3), the way VS Code's showInputBox does.
type InputBox interface {
	// Show opens the input box. done is called once, on the UI thread, after the box is hidden: with the text and true when the
	// user accepted a valid text, or with an empty text and false when the box was closed without one or another quick input
	// took its place. It fails with ExtensionInputBoxUnavailable in a scope that cannot show one (a window tab of an extension
	// has none) and with ExtensionDeactivated after the activation ended; done is not called then.
	Show(opts InputBoxOptions, done func(value string, ok bool)) error
}
