package output

import (
	"context"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Keys of the widgets of the panel, which tests find them by.
var (
	// EditorKey is the key of the editor that shows the text of the channel.
	EditorKey = foundation.NewValueKey("output:editor")
	// ChannelsKey is the key of the dropdown of the channels.
	ChannelsKey = foundation.NewValueKey("output:channels")
	// ClearKey is the key of the button that clears the channel.
	ClearKey = foundation.NewValueKey("output:clear")
	// ScrollLockKey is the key of the button that locks and unlocks the scrolling.
	ScrollLockKey = foundation.NewValueKey("output:scroll-lock")
)

// BodyOpts are what the editor of the panel shares with the other editors of the window.
type BodyOpts struct {
	// Commands is the registry whose editor commands (copy, select all, find) the editor takes over
	// while it has the focus.
	Commands *command.Registry
	// Keys is the scope of the context keys of the window.
	Keys *contextkey.Scope
	// Scopes registers the focus node of the editor with its scope.
	Scopes *kit.FocusScopes
	// Context is the context of what the editor asks of the desktop.
	Context context.Context
	// Clipboard, Find and Zoom are what the editors of the window share.
	Clipboard *editor.ClipboardMemory
	Find      *editor.FindMemory
	Zoom      *editor.Zoom
}

// Body is the body of the panel: a read-only editor over the text of the active channel.
//
// Ported from VS Code: src/vs/workbench/contrib/output/browser/outputView.ts (OutputViewPane)
type Body struct {
	widgets.StatefulWidget
	view *View
	opts BodyOpts
}

// NewBody makes the body of the panel for a view.
func NewBody(view *View, opts ...BodyOpts) *Body {
	b := &Body{StatefulWidget: widgets.NewStatefulWidgetBase(), view: view}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	return b
}

// CreateState implements widgets.StatefulWidget.
func (b *Body) CreateState() widgets.State { return &bodyState{} }

type bodyState struct {
	widgets.StateOf[*Body]
}

// InitState goes to the end of the text, where the tab was left to the lines that came meanwhile.
func (s *bodyState) InitState() { s.Widget().view.Shown() }

// Build implements widgets.State.
func (s *bodyState) Build(widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	o := w.opts
	text := editor.NewEditor(w.view.vm, editor.EditorOpts{
		Commands: o.Commands, Keys: o.Keys, Scopes: o.Scopes, Context: o.Context,
		ClipboardMemory: o.Clipboard, FindMemory: o.Find, Zoom: o.Zoom, Focus: w.view.focus,
		OnFocus: func(has bool) {
			if o.Keys != nil {
				o.Keys.Set("inOutput", has)
			}
		},
	})
	return widgets.NewKeyedSubtree(text, widgets.WidgetOpts{Key: EditorKey})
}

// Actions is what the panel shows in its title strip while the Output is the tab: the dropdown of the
// channels, the button that clears the channel and the one that locks the scrolling.
//
// Ported from VS Code: src/vs/workbench/contrib/output/browser/outputView.ts (SwitchOutputActionViewItem) and
// output.contribution.ts (clearOutput, toggleAutoScroll)
type Actions struct {
	widgets.StatefulWidget
	view *View
}

// NewActions makes the actions of the panel for a view.
func NewActions(view *View) *Actions {
	return &Actions{StatefulWidget: widgets.NewStatefulWidgetBase(), view: view}
}

// CreateState implements widgets.StatefulWidget.
func (a *Actions) CreateState() widgets.State { return &actionsState{} }

type actionsState struct {
	widgets.StateOf[*Actions]
	stop func()
}

// InitState follows the view.
func (s *actionsState) InitState() {
	s.stop = s.Widget().view.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// Dispose stops following the view.
func (s *actionsState) Dispose() { s.stop() }

// Widths of the actions: the dropdown of the channels at most channelsWidth and at least
// channelsMinWidth wide, and what is after it (the 6 px, and the two buttons of 22 px).
const (
	channelsWidth    = 160.0
	channelsMinWidth = 60.0
	buttonsWidth     = 6 + 2*22.0
)

// Build implements widgets.State.
func (s *actionsState) Build(widgets.BuildContext) widgets.Widget {
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, cons rendering.BoxConstraints) widgets.Widget {
		return s.actions(cons.MaxWidth)
	})
}

// actions are the actions in the room there is: with little room the dropdown shrinks, and under its
// least width it is left out.
func (s *actionsState) actions(room float64) widgets.Widget {
	v := s.Widget().view
	names := v.Channels()
	options := make([]kit.DropdownOption, len(names))
	selected := -1
	for i, name := range names {
		options[i] = kit.DropdownOption{Label: name}
		if name == v.Active() {
			selected = i
		}
	}
	lockIcon, lockTip := fonts.CodiconLock, "Turn Auto Scrolling Off"
	if v.ScrollLock() {
		lockIcon, lockTip = fonts.CodiconUnlock, "Turn Auto Scrolling On"
	}
	var children []widgets.Widget
	if width := min(room-buttonsWidth, channelsWidth); width >= channelsMinWidth {
		children = append(children,
			widgets.NewSizedBox(kit.NewDropdown(options, kit.DropdownOpts{
				Key: ChannelsKey, Selected: selected, Width: foundation.Some(width),
				OnChanged: func(i int) { v.SetActive(names[i]) },
			}), widgets.SizedBoxOpts{Width: foundation.Some(width)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
	}
	children = append(children,
		kit.NewIconButton(fonts.CodiconClearAll, kit.IconButtonOpts{Key: ClearKey, Tooltip: "Clear Output", OnPressed: v.Clear}),
		kit.NewIconButton(lockIcon, kit.IconButtonOpts{Key: ScrollLockKey, Tooltip: lockTip, OnPressed: v.ToggleScrollLock}))
	return widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}
