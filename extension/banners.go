package extension

import "context"

// Banner is a bar across the top of the window tab, under the title bar: an icon, a line of text and buttons. An extension
// shows one for something that goes on in the window tab and that the user should always see and be able to end, such as the
// "Remote Work: laptop B connected" banner with its Disconnect button (SPEC §10.18).
type Banner struct {
	// Text is the message; one line, cut with an ellipsis when it is too long.
	Text string
	// Icon is the name of the Codicon before the text ("remote"); empty for none.
	Icon string
	// Actions are the buttons at the right end.
	Actions []BannerAction
}

// BannerAction is a button of a banner.
type BannerAction struct {
	// Label is the text of the button.
	Label string
	// Run is called on the UI thread when the button is pressed. A panic in it deactivates the extension.
	Run func()
}

// BannerHandle changes or removes a banner.
type BannerHandle interface {
	// Update replaces what the banner shows. It does nothing after Dispose or the end of the activation.
	Update(banner Banner)
	// Dispose takes the banner away; a banner that was not disposed goes with the activation.
	Dispose()
}

// Banners shows banners in the window tab of the scope.
type Banners interface {
	// Show adds a banner below the ones shown before it. It fails with ExtensionBannerInvalid for a banner with no text,
	// ExtensionBannersUnavailable in a scope that has no workbench to show it in and ExtensionDeactivated after the activation
	// ended. Call it on the UI thread.
	Show(banner Banner) (BannerHandle, error)
}

// FileChooserOptions say what the dialog of FileChooser().Open asks for.
type FileChooserOptions struct {
	// Title is the title of the dialog; the desktop's own when empty.
	Title string
	// AcceptLabel is the text of the button that accepts the dialog ("Add"); the desktop's own when empty.
	AcceptLabel string
	// Filters are the kinds of file the dialog offers; the first is chosen when it opens.
	Filters []FileFilter
	// Folder is the folder the dialog opens in; the desktop's own when empty.
	Folder string
	// Name is, for FileChooser().Save, the file name the dialog starts with ("result.csv"); Open ignores it.
	Name string
	// Save is set by FileChooser().Save for the host: the dialog asks for a file to write, not to read. An extension leaves it
	// alone.
	Save bool
}

// FileFilter is a kind of file a file dialog shows ("Public keys", "*.pub").
type FileFilter struct {
	// Name is what the dialog's menu shows.
	Name string
	// Patterns are globs such as "*.pub"; the dialog matches them as written, so write both cases for a name that may have
	// either.
	Patterns []string
}

// FileChooser asks the user for a file with the file dialog of the desktop.
type FileChooser interface {
	// Open shows the dialog and waits for the user: it returns the path of the file chosen, or an empty path and no error when
	// the dialog was closed without a choice. It blocks, so it is called from a goroutine of the extension and never from the
	// UI thread. It fails with ExtensionFileChooserUnavailable in a scope that has no dialog (a desktop without the file
	// chooser portal, a scope that is not on screen), ExtensionDeactivated after the activation ended, and with the error of the
	// desktop otherwise. When ctx ends the call returns its error.
	Open(ctx context.Context, opts FileChooserOptions) (string, error)
	// Save shows the dialog of the desktop that asks where to write a file (SPEC §10.21: the export of a result), with
	// opts.Name as the name it starts with. The rules are those of Open: it returns the path chosen, or an empty path and no error
	// when the dialog was closed without a choice; it blocks, so it is called from a goroutine of the extension; it fails with
	// ExtensionFileChooserUnavailable, ExtensionDeactivated and the error of the desktop. The desktop's dialog asks about
	// overwriting a file; the extension writes the file it is given.
	Save(ctx context.Context, opts FileChooserOptions) (string, error)
}
