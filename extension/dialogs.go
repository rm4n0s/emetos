package extension

// DialogType is what a dialog is about; it sets the icon of the dialog.
type DialogType int

// The types of a dialog.
const (
	// DialogQuestion shows the information icon, as VS Code does for a question.
	DialogQuestion DialogType = iota
	// DialogInfo shows the information icon.
	DialogInfo
	// DialogWarning shows the warning icon.
	DialogWarning
	// DialogError shows the error icon.
	DialogError
)

// DialogButton is a button of a dialog.
type DialogButton struct {
	// Label is the text of the button.
	Label string
	// Cancel makes the button the one Escape and the close button of the dialog stand for: closing the dialog answers with it.
	Cancel bool
}

// DialogSpec describes a modal dialog (VS Code's showMessage with modal: true).
type DialogSpec struct {
	// Type sets the icon.
	Type DialogType
	// Message is the main text; with a Detail it is the title above it.
	Message string
	// Detail is the text under the message; its line breaks are kept.
	Detail string
	// Buttons are the buttons; there must be at least one. The first is the default button: it has the focus when the dialog opens
	// and Enter presses it.
	Buttons []DialogButton
	// Origin is the screen that shows the dialog; the zero value is the origin of the command being handled (see InputBoxOptions.Origin).
	Origin Origin
}

// DialogResult is the answer to a dialog.
type DialogResult struct {
	// Button is the index in DialogSpec.Buttons of the button that was pressed, or of the cancel button when the dialog was closed
	// with Escape; -1 when it was closed and has no cancel button.
	Button int
	// Cancelled says the dialog was closed without pressing a button.
	Cancelled bool
}

// Dialogs asks the user a question in a modal dialog of the window tab of the scope, which blocks the window tab until it is
// answered (SPEC §10.30 "Trusting a repository").
type Dialogs interface {
	// Ask shows the dialog and calls done once, on the UI thread, with the answer. The window shows one dialog at a time: a
	// dialog asked while another is open waits for its turn, in the order asked. It returns at once. It fails with
	// ExtensionDialogInvalid for a spec with no button or with two cancel buttons, ExtensionDialogsUnavailable in a scope that
	// cannot show a dialog (a window tab that is not on screen) and ExtensionDeactivated after the activation ended. done is
	// not called when the extension was deactivated before the answer; a panic in it deactivates the extension.
	Ask(spec DialogSpec, done func(DialogResult)) error
}
