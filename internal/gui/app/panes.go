package app

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Editors that are not text editors (SPEC §10.3 "Editor kinds"). The editor of a text input is a textEditor, which
// the shell makes for every TextInput; any other input type has an editorKind: how its input comes back from the saved
// state of the window, and how its editor, a pane, is made. A pane builds the widget that fills the group below the
// tab strip, takes the keyboard focus when its group does, and ends with its input. The settings editor is the
// first and the keyboard shortcuts editor (step 8.3) the second; the diff editor (step 9.5) and the merge editor
// (step 9.7) are registered the same way, in editorKinds.

// pane is the editor of an input that shows no text.
type pane interface {
	// build is the widget of the editor in a group; active tells that the group is the active one. The shell keeps
	// the state of the widget through a key of the pane, so it follows the editor to another group.
	build(bc widgets.BuildContext, group *editorgroup.Group, active bool) widgets.Widget
	// focus asks for the keyboard focus, and hasFocus tells whether the editor has it.
	focus()
	hasFocus() bool
	// dispose ends the pane, when its input is closed or the window is.
	dispose()
}

// editorKind says how the window handles the inputs of one type that is not text.
type editorKind struct {
	// typeID is the TypeID of the inputs of the kind.
	typeID string
	// restore makes an input again from the value its Serialize gave, or reports false when it cannot come back.
	restore func(value string) (editorgroup.EditorInput, bool)
	// newPane makes the pane that shows an input of the kind.
	newPane func(s *shellState, input editorgroup.EditorInput) pane
	// icon is the Codicon of the tab, 0 for none.
	icon int
}

// editorKinds are the kinds of editors the window has besides the text editor.
func editorKinds() []editorKind {
	return []editorKind{settingsKind(), keybindingsKind(), extensionsKind(), eventSharingKind(), extensionTabKind(), diffKind(), mergeKind()}
}

// kindOf finds the kind of a type of input.
func kindOf(typeID string) (editorKind, bool) {
	for _, k := range editorKinds() {
		if k.typeID == typeID {
			return k, true
		}
	}
	return editorKind{}, false
}

// ensurePane makes the pane of an input of a kind that has one, unless it has one already.
func (s *shellState) ensurePane(input editorgroup.EditorInput) {
	if s.group.panes[input] != nil {
		return
	}
	if k, ok := kindOf(input.TypeID()); ok && k.newPane != nil {
		s.group.panes[input] = k.newPane(s, input)
	}
}

// paneColumn is the tab strip over the pane of a group.
func (s *shellState) paneColumn(bc widgets.BuildContext, win *Workspace, group *editorgroup.Group, input editorgroup.EditorInput, p pane, active bool) widgets.Widget {
	var area widgets.Widget = widgets.NewKeyedSubtree(p.build(bc, group, active), widgets.WidgetOpts{Key: widgets.NewGlobalObjectKey[widgets.State](p)})
	area = widgets.NewListener(area, widgets.ListenerOpts{
		Behavior:      rendering.HitTestBehaviorTranslucent,
		OnPointerDown: func(*gestures.PointerDownEvent) { s.groupFocused(group) },
	})
	column := []widgets.Widget{}
	if tabs := s.tabStrip(bc, win, group); tabs != nil {
		column = append(column, tabs)
	}
	column = append(column, widgets.NewExpanded(area))
	return widgets.NewColumn(column, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// paneFocus is what a pane embeds to know whether it has the keyboard focus: wrap makes the focus that moves into
// the pane make its group the active group.
type paneFocus struct{ has bool }

func (f *paneFocus) hasFocus() bool { return f.has }

// wrap puts the widget of a pane in a focus node that only watches the focus of the widgets below it.
func (f *paneFocus) wrap(s *shellState, group *editorgroup.Group, child widgets.Widget) widgets.Widget {
	return widgets.NewFocus(child, widgets.FocusOpts{
		CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "Editor pane",
		OnFocusChange: func(has bool) {
			f.has = has
			if has {
				s.focusMoved(group)
			}
		},
	})
}

// tabStrip is the tab strip of a group in the host of the context menu of the tabs; nil while
// workbench.editor.showTabs is none.
func (s *shellState) tabStrip(bc widgets.BuildContext, win *Workspace, group *editorgroup.Group) widgets.Widget {
	if win.ShowTabs() == "none" {
		return nil
	}
	return kit.NewContextMenu(kit.NewTabs(s.tabItems(kit.ThemeOf(bc), group), s.tabsOptions(win, group)),
		kit.ContextMenuOpts{Controller: s.view(group).tabMenu, OnRun: s.menus.Run, OnHide: s.askFocusSoon})
}
