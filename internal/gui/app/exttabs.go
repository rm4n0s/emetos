package app

import (
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The editor tabs of extensions in the window (SPEC §10.15 "Editor tab", step 11.6): the kind of editor of an
// ExtensionTabInput, the pane that shows the box of the extension, and the exthost.EditorHost the window gives the scope to
// open, close and reveal the tabs and to show a file in a file view.

// extensionTabKind is how the window handles ExtensionTabInputs. The icon of a tab is the one its kind declares
// (tabIcon).
func extensionTabKind() editorKind {
	return editorKind{
		typeID: editorgroup.ExtensionTabInputTypeID,
		restore: func(value string) (editorgroup.EditorInput, bool) {
			in, err := editorgroup.ParseExtensionTabInput(value)
			return in, err == nil
		},
		newPane: func(s *shellState, input editorgroup.EditorInput) pane {
			in := input.(*editorgroup.ExtensionTabInput)
			if scope := PeekWorkspace(s.Context()).Extensions(); scope != nil {
				scope.RegisterTab(in)
			}
			return &extTabPane{s: s, input: in}
		},
	}
}

// extTabPane is the editor of an ExtensionTabInput: the box of the extension.
type extTabPane struct {
	paneFocus
	s     *shellState
	input *editorgroup.ExtensionTabInput
}

// build implements pane.
func (p *extTabPane) build(bc widgets.BuildContext, group *editorgroup.Group, _ bool) widgets.Widget {
	win := PeekWorkspace(bc)
	scope := win.Extensions()
	if scope == nil {
		return p.wrap(p.s, group, widgets.NewSizedBox(nil))
	}
	return p.wrap(p.s, group, scope.TabBody(p.input))
}

// focus implements pane: the widget of the extension takes the keyboard itself.
func (p *extTabPane) focus() {}

// dispose implements pane.
func (p *extTabPane) dispose() {}

// closed is called when the tab closes for good: the extension is told and its state is dropped.
func (p *extTabPane) closed() {
	if scope := PeekWorkspace(p.s.Context()).Extensions(); scope != nil {
		scope.TabClosed(p.input)
	}
}

// extTabIcon is the icon of the tab of an extension, with the busy mark in its place.
func extTabIcon(colors kit.Colors, in *editorgroup.ExtensionTabInput, active bool) widgets.Widget {
	color := colors.TabInactiveForeground
	if active {
		color = colors.TabActiveForeground
	}
	if c, ok := ansiColorByID(colors, in.IconColor()); ok {
		color = c
	}
	code := 0
	if in.Busy() {
		code = fonts.CodiconLoading
	} else if c, ok := fonts.CodiconByName(in.Icon()); ok {
		code = c
	}
	if code == 0 {
		return nil
	}
	return widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(color)})
}

// ansiNames are the ids of the sixteen ANSI colors of the terminal, in the order of kit.Colors.TerminalAnsi.
var ansiNames = [16]string{
	"terminal.ansiBlack", "terminal.ansiRed", "terminal.ansiGreen", "terminal.ansiYellow", "terminal.ansiBlue", "terminal.ansiMagenta",
	"terminal.ansiCyan", "terminal.ansiWhite", "terminal.ansiBrightBlack", "terminal.ansiBrightRed", "terminal.ansiBrightGreen",
	"terminal.ansiBrightYellow", "terminal.ansiBrightBlue", "terminal.ansiBrightMagenta", "terminal.ansiBrightCyan", "terminal.ansiBrightWhite",
}

// ansiColorByID is the color of the theme with the id of one of the terminal's ANSI colors, which the icon of the tab of an
// extension may have (EditorTab.SetIcon); false for any other id.
func ansiColorByID(colors kit.Colors, id string) (ui.Color, bool) {
	if id == "" {
		return ui.Color{}, false
	}
	for i, name := range ansiNames {
		if name == id {
			return colors.TerminalAnsi[i], true
		}
	}
	return ui.Color{}, false
}

// extEditorHost is the exthost.EditorHost of a workspace.
type extEditorHost struct{ w *Workspace }

// shellOrErr is the shell of the window tab when it is on screen.
func (h extEditorHost) shellOrErr(what string) (*shellState, error) {
	s := h.w.shell
	if s == nil || !s.Mounted() {
		return nil, errors.New("ExtEditorsUnavailable", "the window tab is not on screen", "what", what)
	}
	return s, nil
}

// groupFor finds the group with the id, or the active group.
func (s *shellState) groupFor(id int) *editorgroup.Group {
	if id > 0 {
		if g := s.group.grid.Group(id); g != nil {
			return g
		}
	}
	return s.activeGroup()
}

// OpenTab implements exthost.EditorHost.
func (h extEditorHost) OpenTab(in *editorgroup.ExtensionTabInput, group int, inactive bool) error {
	return h.open(in, group, inactive, -1)
}

// OpenTabAt implements exthost.EditorHost.
func (h extEditorHost) OpenTabAt(in *editorgroup.ExtensionTabInput, group, index int) error {
	return h.open(in, group, false, index)
}

// open shows an input in a group, at an index of its strip when the index is not negative.
func (h extEditorHost) open(in *editorgroup.ExtensionTabInput, group int, inactive bool, index int) error {
	s, err := h.shellOrErr("open a tab")
	if err != nil {
		return err
	}
	win := h.w
	g := s.groupFor(group)
	s.SetState(func() {
		s.nav.hold++
		g.Model.OpenEditor(in, editorgroup.OpenOptions{Pinned: true, Active: !inactive, Index: max(index, 0), HasIndex: index >= 0})
		s.nav.hold--
		s.syncNav()
		if !inactive {
			if err := s.group.grid.Activate(g); err != nil {
				win.Process.Logger.Error("cannot activate the group", "err", err)
			}
		}
	})
	s.refreshKeys()
	return nil
}

// BeginDrag implements exthost.EditorHost: the drag of a tab that is not one yet, shown like the drag of a tab.
func (h extEditorHost) BeginDrag(label string) (exthost.DragSession, error) {
	s, err := h.shellOrErr("drag a tab")
	if err != nil {
		return nil, err
	}
	if s.group.drag != nil {
		return nil, errors.New("ExtEditorsDragBusy", "another drag is going on", "label", label)
	}
	s.group.drag = &tabDrag{label: label, hovered: -1, external: true}
	s.group.dragFeed.NotifyListeners()
	return &extDragSession{s: s}, nil
}

// extDragSession is the exthost.DragSession of a drag an extension began.
type extDragSession struct{ s *shellState }

// Move implements exthost.DragSession.
func (x *extDragSession) Move(at ui.Offset) { x.s.dragMove(at) }

// End implements exthost.DragSession.
func (x *extDragSession) End(at ui.Offset, drop bool, open func(group, index int) error) (bool, error) {
	s := x.s
	d := s.group.drag
	if d == nil || !d.external {
		return false, nil
	}
	var target dropTarget
	if drop {
		d.at = at
		s.dragUpdate(false)
		target = d.target
	}
	s.stopDrag()
	if !drop {
		return false, nil
	}
	switch target.kind {
	case dropStrip:
		return openedOr(open(target.group.ID(), target.index))
	case dropArea:
		g := target.group
		if dir, split := target.zone.Direction(); split {
			nu, err := s.group.grid.AddGroup(g, dir)
			if err != nil {
				return false, err
			}
			g = nu
			return openedOr(open(g.ID(), -1))
		}
		return openedOr(open(g.ID(), -1))
	case dropNone:
	}
	return false, nil
}

// openedOr is the answer of End for the result of opening the tab.
func openedOr(err error) (bool, error) { return err == nil, err }

// groupHolding is the group that shows the input, or nil.
func (s *shellState) groupHolding(input editorgroup.EditorInput) *editorgroup.Group {
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		if g.Model.ContainsStrict(input) {
			return g
		}
	}
	return nil
}

// CloseTab implements exthost.EditorHost.
func (h extEditorHost) CloseTab(in *editorgroup.ExtensionTabInput) {
	s, err := h.shellOrErr("close a tab")
	if err != nil {
		return
	}
	if g := s.groupHolding(in); g != nil {
		s.closeEditors(g, []editorgroup.EditorInput{in})
	}
}

// RevealTab implements exthost.EditorHost.
func (h extEditorHost) RevealTab(in *editorgroup.ExtensionTabInput, focus bool) {
	s, err := h.shellOrErr("reveal a tab")
	if err != nil {
		return
	}
	g := s.groupHolding(in)
	if g == nil {
		return
	}
	s.SetState(func() {
		g.Model.SetActive(in)
		if err := s.group.grid.Activate(g); err != nil {
			h.w.Process.Logger.Error("cannot activate the group", "err", err)
		}
	})
	s.refreshKeys()
	if focus {
		s.askFocus(widgets.WidgetsBindingOf(s.Context()))
	}
}

// GroupOf implements exthost.EditorHost.
func (h extEditorHost) GroupOf(in *editorgroup.ExtensionTabInput) int {
	s, err := h.shellOrErr("find a tab")
	if err != nil {
		return 0
	}
	if g := s.groupHolding(in); g != nil {
		return g.ID()
	}
	return 0
}

// OpenFileView implements exthost.EditorHost: the file opens in the active group, or in the group on its side, kept, in the
// file view. A file whose view is not available opens in Text.
func (h extEditorHost) OpenFileView(uri, viewID string, side bool, group int) error {
	s, err := h.shellOrErr("open a file view")
	if err != nil {
		return err
	}
	if uri == "" {
		// the document of the active text editor of the group
		e := s.group.editors[s.groupFor(group).Model.ActiveEditor()]
		if e == nil {
			return errors.New("ExtEditorsNoDocument", "the group has no text editor to show in the view", "group", group)
		}
		uri = e.doc.URI().String()
	}
	u, err := document.ParseURI(uri)
	if err != nil {
		return err
	}
	if u.Scheme != document.SchemeFile {
		return errors.New("ExtEditorsFileOnly", "only a file can be opened in a file view", "uri", uri)
	}
	target := s.groupFor(group)
	if side {
		if err := s.group.grid.Activate(target); err != nil {
			h.w.Process.Logger.Error("cannot activate the group", "err", err)
		}
		if target = s.sideGroup(); target == nil {
			target = s.groupFor(group) // the group for the side cannot be made: the file opens where it is asked from
		}
	}
	// an open document of the file opens in the group at once; the file is read otherwise
	if doc, ok := h.w.Documents.Get(u); ok {
		h.w.Documents.Retain(doc)
		s.show(widgets.WidgetsBindingOf(s.Context()), doc, openOptions{Group: target, FileView: viewID})
		return nil
	}
	s.openPathAt(widgets.WidgetsBindingOf(s.Context()), u.Path, 0, 0, openOptions{Group: target, FileView: viewID})
	return nil
}
