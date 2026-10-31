package scm

import (
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// OpenOptions say how a resource is opened (VS Code's IOpenEvent: the editor options and sideBySide).
type OpenOptions struct {
	// Preserve keeps the keyboard focus in the view; a click does, Enter and a double click do not.
	Preserve bool
	// Pinned opens the editor as a kept tab and not as the preview; a double click and Enter do.
	Pinned bool
	// Side opens it beside the active editor (Ctrl or Alt held).
	Side bool
}

// DragSession is the drag of a row into the editor area (the window's tab drag): the view tells it where the pointer is, and at
// the end the window decides which group the file goes to.
type DragSession interface {
	// Move tells where the pointer is, in window coordinates.
	Move(at ui.Offset)
	// End ends the drag; with drop set the window calls open with the group and the place it was dropped on.
	End(at ui.Offset, drop bool, open func(group, index int) error) (bool, error)
}

// Options say what the view reads from the window and what it asks the window to do. Model and Run are required.
type Options struct {
	// Model is the Source Control model of the window.
	Model *scmcore.Model
	// Post runs a function on the UI thread: the model is changed from any goroutine.
	Post func(func())
	// Run runs a command of the registry with an argument, as a button of the view does.
	Run func(commandID string, args any)
	// Entries gives the entries of a menu for the context keys of a row (extra is added to the window's); nil is no menu. The
	// inline actions and the title actions are read from it.
	Entries func(menu string, extra map[string]any) []command.MenuEntry
	// ShowMenu opens the context menu of a menu at a position of the window, in the context extra, with args as the argument of
	// the commands that have none; the rows of the group "inline" are left out.
	ShowMenu func(menu string, extra map[string]any, args []any, at ui.Offset)
	// OpenResource opens the file of a resource that has no command; a resource with a command runs the command.
	OpenResource func(uri string, o OpenOptions)
	// Icons gives the file icon theme, nil for none; Language the language id of a path.
	Icons    func() *theme.FileIconTheme
	Language func(path string) string
	// AlwaysShowActions is scm.alwaysShowActions, AlwaysShowRepositories scm.alwaysShowRepositories and AutoReveal
	// scm.autoReveal; nil is false.
	AlwaysShowActions, AlwaysShowRepositories, AutoReveal func() bool
	// BeginDrag starts the drag of a file into the editor area, and OpenIn opens the file in the group the drag ended on; nil
	// turns the drag off.
	BeginDrag func(label string) (DragSession, error)
	OpenIn    func(uri string, group, index int) error
	// Side tells whether Control or Alt is held (an open beside the active editor); nil is never.
	Side func() bool
	// Scopes and Keys give the input a scope of context keys while it has the keyboard focus (scmRepository, scmInputIsInFirstPosition,
	// scmInputIsInLastPosition, scmInputHasValidationMessage, editorHasSelection): a child of Keys that the window's key dispatcher
	// finds through Scopes. Both nil: the input has no keys of its own.
	Scopes *kit.FocusScopes
	Keys   *contextkey.Scope
	// InputLines gives scm.inputMinLineCount and scm.inputMaxLineCount; nil is 1 and 10. InputFont gives scm.inputFontFamily and
	// scm.inputFontSize; nil is the UI font at 13 px.
	InputLines func() (minLines, maxLines int)
	InputFont  func() InputFont
	// ShowActionButton is scm.showActionButton; nil is true.
	ShowActionButton func() bool
	// AcceptKeyLabel is the label of the key that runs scm.acceptInput ("Ctrl+Enter"), which the placeholder of the box names.
	AcceptKeyLabel func() string
	// ShowItems opens a menu of items below a position of the window: the dropdown of the action button. The items run their
	// commands (ID, with Data as the argument) when the host's menu runs them.
	ShowItems func(items []kit.MenuItem, at ui.Offset)
	// Reveal shows the view in the window; it is called before the input is asked to take the keyboard focus (SCMInput.Focus).
	Reveal func()
	// AfterFrame runs a function after the next frame; the list is scrolled to a revealed row then. Nil scrolls at once.
	AfterFrame func(func())
}

func (o Options) flag(f func() bool) bool { return f != nil && f() }
