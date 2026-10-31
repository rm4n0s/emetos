package extension

// The File Decorations API (SPEC §10.30 "File decorations and the Explorer", step 24.6; after vscode.window.registerFileDecorationProvider,
// FileDecoration and FileDecorationProvider of vscode.d.ts and extHostDecorations.ts). An extension tells the workbench what it knows about
// a file, and the Explorer, Open Editors, the editor tabs, the breadcrumbs, the Search results and the Source Control view draw it: the
// name in a color, a badge of one or two characters at the end of the row, a tooltip.
//
// Everything here may be called from any goroutine except where a comment says that Emetos calls the extension (the provider is called
// on the UI thread).

// FileDecoration is what an extension says about a file (VS Code's FileDecoration).
type FileDecoration struct {
	// Badge is a short text drawn at the end of the row: one or two characters ("M", "U", "↓A"). A longer one makes the decoration
	// invalid. Icon wins over it when both are set.
	Badge string
	// Icon is the name of a Codicon drawn instead of the badge ("lock-small").
	Icon string
	// Tooltip is the hover of the row; the tooltips of several providers are joined with " • ".
	Tooltip string
	// Color is the id of the theme color the name and the badge are drawn in ("gitDecoration.modifiedResourceForeground"). A
	// color the theme does not know leaves the color of the name as it is.
	Color string
	// Propagate shows the decoration in the folders above the file too: a folder with such a file is drawn in the color with a small
	// dot (VS Code's propagate).
	Propagate bool
	// Priority orders the decorations several providers give for a file, and the colors that a folder takes from the files below
	// it: the higher goes first. 0 is the default. VS Code gives every extension the same weight; Git uses it to put a conflict
	// (4) before an ignored file (3), a modified file (2) and the others (1).
	Priority int
}

// IsEmpty says that the decoration says nothing (no color, badge, icon or tooltip); the host drops it, as VS Code does.
func (d FileDecoration) IsEmpty() bool {
	return d.Color == "" && d.Badge == "" && d.Icon == "" && d.Tooltip == ""
}

// ValidBadge says whether a badge is allowed: empty, or at most two characters.
func ValidBadge(badge string) bool {
	n := 0
	for range badge {
		n++
	}
	return n <= 2
}

// FileDecorationProvider answers for the files (VS Code's FileDecorationProvider, without the promise: the answer is from memory).
type FileDecorationProvider interface {
	// Decoration is the decoration of the file at uri (file:///path), ok false for none. It is called on the UI thread, for the
	// files the views show and for those the extension fired, and its answer is kept until Fire says it may have changed: answer
	// from memory, fast. An answer that takes over a millisecond is logged, and a provider that does so too often is not asked for
	// new files any more. A panic fails the extension. An invalid decoration (a longer badge, or an empty one) is logged and dropped.
	Decoration(uri string) (decoration FileDecoration, ok bool)
}

// FileDecorations is a registered provider.
type FileDecorations interface {
	// Fire says that the decorations of the files may have changed: they are asked again, and the views that show them (and the
	// folders above them) are drawn again if the answer is another. With no URI everything the provider said is forgotten. It
	// may be called from any goroutine; the questions are asked on the UI thread.
	Fire(uris ...string)
	// Dispose removes the provider and its decorations. It happens by itself when the activation ends.
	Dispose()
}

// Decorations registers the file decoration providers of an extension.
type Decorations interface {
	// Register adds a provider. It fails with ExtensionDecorationsInvalid for a nil provider, ExtensionDecorationsUnavailable in a scope
	// that has no decorations service and ExtensionDeactivated after the activation ended.
	Register(provider FileDecorationProvider) (FileDecorations, error)
}
