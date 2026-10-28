package extension

import (
	"encoding/json"

	"github.com/rm4n0s/glutter/widgets"
)

// ViewContainerSpec declares a view container of the side bar: an icon in the activity bar, and the views it holds. The
// extension's container shows when the user clicks the icon, or runs the command ShowViewCommand(ID).
type ViewContainerSpec struct {
	// ID identifies the container among the containers of Emetos. It starts with the id of the extension and a dot
	// (acme.build.container).
	ID string
	// Title is the name in the title of the side bar and the tooltip of the icon.
	Title string
	// Icon is the name of the Codicon of the activity bar ("checklist", "list-tree").
	Icon string
	// Order orders the icons of the activity bar, the lowest first; the built-in Explorer is 0. Zero is put after
	// everything the editor has.
	Order int
}

// ViewSpec declares a view of the side bar: a section with a header and a body in a container.
type ViewSpec struct {
	// ID identifies the view. It starts with the id of the extension and a dot.
	ID string
	// Name is the title in the header of the view.
	Name string
	// Container is the id of the container: one the extension declares, or one of Emetos (workbench.view.explorer).
	Container string
	// When is a when-clause under which the view is shown; empty is always. It reads the context keys of the window and
	// the ones the extension sets with Context().
	When string
	// Order orders the views of a container, the lowest first.
	Order int
	// Collapsed starts the view with its body folded away.
	Collapsed bool
}

// PanelViewSpec declares a view of the panel: a tab beside Problems and Output with one body. It moves and maximizes
// with the panel (the panel's position is the user's, and so is its size). Emetos adds the command
// ToggleViewCommand(ID) "Toggle <Title>" and an entry for it in the View menu.
type PanelViewSpec struct {
	// ID identifies the view and its tab. It starts with the id of the extension and a dot.
	ID string
	// Title is the name on the tab, shown in capitals.
	Title string
	// Icon is the name of the Codicon of the view in lists; it may be empty.
	Icon string
	// Order orders the tabs, the lowest first; Problems is 0 and Output 1.
	Order int
}

// ViewActionSpec declares a title action of a view: an icon button at the right end of its header (a side bar view's
// header, the title strip of the panel for a panel view), shown while the pointer is over the view or the focus is in it
// for a side bar view and always for the tab that is open in the panel.
type ViewActionSpec struct {
	// View is the id of a view of the extension.
	View string
	// Command is the id of the command the button runs.
	Command string
	// Icon is the name of the Codicon of the button.
	Icon string
	// Tooltip is the text of the hover; the title of the command when empty.
	Tooltip string
	// When is a when-clause under which the button is shown; empty is always.
	When string
	// Order orders the buttons of a view, the lowest first.
	Order float64
}

// ShowViewCommand is the id of the command that shows a side bar view container and its views: workbench.view.extension.
// followed by the id of the container. Emetos adds it for every container an extension declares, with the title "Show
// <Title>" in the command palette.
func ShowViewCommand(containerID string) string { return "workbench.view.extension." + containerID }

// ToggleViewCommand is the id of the command that shows a panel view with the focus, or hides the panel when the view
// has it: workbench.panel.extension. followed by the id of the view and .toggle.
func ToggleViewCommand(viewID string) string {
	return "workbench.panel.extension." + viewID + ".toggle"
}

// ViewContent is what an extension puts in a view it declared. All the functions run on the UI thread, and a panic in
// any of them, in Build or in the widgets that Build makes with the helpers of package ui, deactivates the extension in
// the scope: the view shows "<Name> failed" with a Show Log button (SPEC §10.15 "Lifetime").
type ViewContent struct {
	// Build makes the widget of the view. It is called when the view is shown and again after Refresh. The widget is put
	// in a box: it gets the constraints of the view, cannot paint outside them, and the text style and colors of the theme
	// are those of the side bar or the panel.
	Build func(bc widgets.BuildContext) widgets.Widget
	// SaveState returns the state the extension wants kept for the view: a value that encodes to JSON, or nil for none.
	// It is called when the view goes off screen (another tab of the panel, a container of the side bar, a closed panel)
	// and when Emetos saves the state of the workspace, so the state also survives a restart.
	SaveState func() (any, error)
	// RestoreState gives the view the JSON of the state that SaveState returned, before the first Build after the view
	// was provided and whenever the box is made again. It is not called when nothing was saved.
	RestoreState func(state json.RawMessage) error
	// AllowCommand, when set, is asked while the keyboard focus is in the view and a key press is bound to a command: with
	// true the window runs the command as it always does, with false the window leaves the key to the focused widget of the
	// view, which is how a terminal gets Ctrl+W or Ctrl+P unless the command is one that skips the shell (SPEC §10.25 "Commands,
	// keys and context keys"). A key bound to no command, and a key that starts a chord, are not asked about. It is called on
	// the UI thread; a panic in it deactivates the extension.
	AllowCommand func(commandID string) bool
}

// ViewHandle controls a view that was provided.
type ViewHandle interface {
	// Refresh builds the view again, later, on the UI thread: call it when the data the view shows changed.
	Refresh()
	// SetBadge puts a count on the tab of a panel view, in the pill the Problems tab has; 0 takes it away. tooltip is
	// the hover of the pill. It does nothing for a side bar view.
	SetBadge(count int, tooltip string)
	// Reveal shows the view: the container of a side bar view is opened (and the side bar shown), the tab of a panel
	// view is selected (and the panel shown). With focus the keyboard focus goes into it.
	Reveal(focus bool)
	// Hide hides the panel when the view is a panel view and its tab is the one that is open (it does nothing otherwise), and
	// gives the keyboard focus to the editor area: the last terminal ending uses it. The view stays declared.
	Hide()
	// Dispose takes the content away; the view stays declared and shows nothing.
	Dispose()
}

// Views gives the views the manifest declares their content.
type Views interface {
	// Provide sets the content of a view of the manifest (Contributes.Views or Contributes.PanelViews). It fails with
	// ExtensionViewNotDeclared when the manifest does not declare the view, with ExtensionViewProvided when the activation
	// provided it already, and with ExtensionViewContentInvalid when Build is nil. The state the view saved last time is
	// restored when it is shown.
	Provide(viewID string, content ViewContent) (ViewHandle, error)
}

// ContextKeys sets the context keys of the extension, which the `when` of its views, its title actions, its commands
// and its keybindings read.
type ContextKeys interface {
	// Set gives the key a value: a bool, a string or a number. The key starts with the id of the extension and a dot
	// (emetos.tasks.running), else it fails with ExtensionContextKeyForeign. The keys the activation set go when it ends.
	Set(key string, value any) error
	// Reset takes the key away.
	Reset(key string)
}
