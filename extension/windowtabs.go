package extension

import (
	"encoding/json"

	"github.com/rm4n0s/glutter/widgets"
)

// WindowTabKindSpec declares a kind of window tab: a tab in the strip of the window beside the workspaces, with a widget
// of the extension in place of a workbench (SPEC §10.15 "Window tab", §10.14). Emetos lists every kind in the menu of the
// "+" button, opens a tab of it when the entry is chosen, and brings it back when Emetos restarts, with the state the
// extension saved. The extension gives the kind its content with WindowTabs().Provide, in the scope of the tab.
type WindowTabKindSpec struct {
	// ID identifies the kind. It starts with the id of the extension and a dot (emetos.scratchpad.tab).
	ID string
	// Title is the title of a tab that has not set one, and the name the Extensions page and the log call the tab.
	Title string
	// Icon is the name of the Codicon in the tab of the strip ("note", "remote"); may be empty (the icon "window").
	Icon string
	// PlusMenuLabel is the text of the entry of the "+" menu; the title when empty. By the convention of the menu the
	// label ends with "..." when choosing it asks for more before the work is done.
	PlusMenuLabel string
}

// WindowTab is the window tab an extension is shown in: the one tab of the scope it was activated in. Its methods run on
// the UI thread.
type WindowTab interface {
	// ID is the id of the tab, kept across restarts: the scope of the tab is named "<extension id>:<id>" in the links
	// between window tabs (ScopeInfo.ID).
	ID() string
	// Kind is the id of the kind of the tab.
	Kind() string
	// SetTitle sets the name on the tab of the strip (the title of the kind when none was set); SetHint the text of its
	// tooltip (the title when empty). Both are kept across restarts.
	SetTitle(title string)
	SetHint(hint string)
	// StateChanged says that what SaveState returns changed, so that Emetos writes it soon (a second at most), and not
	// only when the tab goes off screen or Emetos quits.
	StateChanged()
	// Refresh builds the widget of the tab again.
	Refresh()
	// CaptureKeys says that the content takes the keyboard: while it is true and the content has the keyboard focus, the
	// shortcuts of the window tab do not act, except the ones that act on the window itself (switching, opening and closing
	// the tabs, quitting, full screen), so every other key reaches the content as a key event. A tab that shows another
	// computer's screen sets it; it is false when a tab opens.
	CaptureKeys(capture bool)
	// Close closes the tab, without asking: an extension keeps its data in the state it saves.
	Close()
}

// WindowTabContent is what an extension puts in its window tab. All the functions run on the UI thread, and a panic in any
// of them, or in a widget Build makes, deactivates the extension in the scope: the tab shows "<Name> failed" with a Show
// Log button (SPEC §10.15 "Lifetime").
type WindowTabContent struct {
	// Build makes the widget of the tab; like the widget of a view it is built inside a box that gives it the whole area
	// below the title bar as its constraints, clips what it paints, keeps the keyboard focus and catches a panic.
	Build func(bc widgets.BuildContext) widgets.Widget
	// SaveState returns the state of the tab that is kept in the state of the window (a value that encodes to JSON, nil
	// for none). It is called when the tab goes off screen, when the extension is deactivated and when the window state is
	// written. The state is given to the kind's Create when the tab comes back.
	SaveState func() (any, error)
	// OnClose is called when the tab is closed for good (not when the extension is deactivated or Emetos quits); the
	// state of the tab is dropped after it.
	OnClose func()
}

// WindowTabKind is how an extension makes the content of its window tab.
type WindowTabKind struct {
	// Create makes the content of the tab, with the state SaveState returned in the session before (nil for a tab that was
	// just opened). The title and the hint may be set on tab at once. It is called once for each activation, on the UI
	// thread.
	Create func(tab WindowTab, state json.RawMessage) (WindowTabContent, error)
}

// WindowTabs gives the extension the content of the window tabs of the kinds its manifest declares.
type WindowTabs interface {
	// Provide sets how the tab of a kind the manifest declares is made, and makes the content of the tab of this scope
	// (the window tab the extension was activated for, with its saved state). It fails with
	// ExtensionWindowTabKindNotDeclared when the manifest does not declare the kind, ExtensionWindowTabKindInvalid when
	// Create is nil, ExtensionWindowTabKindProvided when the activation provided it already and
	// ExtensionWindowTabUnavailable in a scope that is not a window tab of this kind.
	Provide(kindID string, kind WindowTabKind) error
}
