package app

import (
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
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

// closed is called when the tab closes for good: the extension is told and its state is dropped. A tab that the other
// screens of the workspace show too (a kind that is shared, SPEC §10.18 "Tabs of extensions in the view") closes on them
// as well: the tab is one, whoever closes it.
func (p *extTabPane) closed() {
	win := PeekWorkspace(p.s.Context())
	scope := win.Extensions()
	if scope == nil {
		return
	}
	shared := scope.SharedTab(p.input)
	scope.TabClosed(p.input)
	if shared {
		win.hostWorkspace().closeTabOnScreens(p.input, p.s)
	}
}

// hostWorkspace is the workspace of the window tab: the workspace itself, or the one a headless view is a view of.
func (w *Workspace) hostWorkspace() *Workspace {
	if w.isView() {
		return w.viewOf
	}
	return w
}

// screenShells are the shells of the screens of the workspace that are on screen now: the window tab of the host first, then
// the views of its clients. UI thread only.
func (w *Workspace) screenShells() []*shellState {
	var out []*shellState
	if s := w.shell; s != nil && s.Mounted() {
		out = append(out, s)
	}
	for _, v := range w.views {
		if v.ws != nil && !v.ended {
			if s := v.ws.shell; s != nil && s.Mounted() {
				out = append(out, s)
			}
		}
	}
	return out
}

// closeTabOnScreens closes the tab of an input, or of its mirror, on every screen of the workspace but the one of except,
// without asking.
func (w *Workspace) closeTabOnScreens(in *editorgroup.ExtensionTabInput, except *shellState) {
	for _, s := range w.screenShells() {
		if s == except {
			continue
		}
		if g, own := s.extTab(in); g != nil {
			s.closeEditors(g, []editorgroup.EditorInput{own})
		}
	}
}

// extTab finds the tab of an input in the groups of the shell: the group that shows it and the shell's own input of it,
// which is the input itself in the window it was opened in and its mirror on another screen (the same id).
func (s *shellState) extTab(in *editorgroup.ExtensionTabInput) (*editorgroup.Group, *editorgroup.ExtensionTabInput) {
	if s.group.grid == nil {
		return nil, nil
	}
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		for _, e := range g.Model.Editors(editorgroup.Sequential, false) {
			if own, ok := e.(*editorgroup.ExtensionTabInput); ok && own.ID() == in.ID() {
				return g, own
			}
		}
	}
	return nil, nil
}

// mirrorHostTabs gives the shell of a headless view the tabs of the window of the host that are shared between screens
// (the terminals of the editor area): a mirror of each, after the editors the view starts with, in the order of the
// groups of the host. The one the host has in front in its active group is in front in the view too.
func (s *shellState) mirrorHostTabs() {
	host := s.win.hostWorkspace()
	scope, hs := host.Extensions(), host.shell
	if scope == nil || hs == nil || hs == s || hs.group.grid == nil || s.group.grid == nil {
		return
	}
	front := hs.activeGroup().Model.ActiveEditor()
	g := s.activeGroup()
	for _, hg := range hs.group.grid.Groups(editorgroup.GridAppearance) {
		for _, e := range hg.Model.Editors(editorgroup.Sequential, false) {
			in, ok := e.(*editorgroup.ExtensionTabInput)
			if !ok || in.IsMirror() || !scope.SharedTab(in) {
				continue
			}
			g.Model.OpenEditor(in.Mirror(), editorgroup.OpenOptions{Pinned: true, Active: e == front, Index: g.Model.Count(), HasIndex: true})
		}
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

// OpenTab implements exthost.EditorHost. A tab of a kind that is shared between the screens of the workspace opens on all
// of them: where it was asked from (the group is a group of that screen) as the caller says, and on the others in their
// active group without taking the focus or the place in front. The window of the host has the input itself, which is the
// one that is saved, and each view a mirror of it.
func (h extEditorHost) OpenTab(in *editorgroup.ExtensionTabInput, group int, inactive bool, origin extension.Origin) error {
	s, err := h.shellOrErr("open a tab")
	if err != nil {
		return err
	}
	scope := h.w.Extensions()
	if scope == nil || !scope.SharedTab(in) {
		return h.open(s, in, group, inactive, -1)
	}
	asker := h.screenShell(origin)
	if asker == nil {
		asker = s // the view of the client is gone: the tab opens in the window of the host
	}
	for _, screen := range h.w.screenShells() {
		own := in
		if screen != s {
			own = in.Mirror()
		}
		g, quiet := group, inactive
		if screen != asker {
			g, quiet = 0, true
		}
		if err := h.open(screen, own, g, quiet, -1); err != nil {
			return err
		}
	}
	return nil
}

// screenShell is the shell of the screen with the origin when it is on screen, nil otherwise.
func (h extEditorHost) screenShell(origin extension.Origin) *shellState {
	screen := h.w.screenOf(origin)
	if screen == nil || screen.shell == nil || !screen.shell.Mounted() {
		return nil
	}
	return screen.shell
}

// tabOn finds the tab of an input on the screen of the origin, or in the window of the host when that screen does not show
// it: the shell, the group and the shell's own input (extTab). The shell is nil when no screen shows it.
func (h extEditorHost) tabOn(in *editorgroup.ExtensionTabInput, origin extension.Origin) (*shellState, *editorgroup.Group, *editorgroup.ExtensionTabInput) {
	if s := h.screenShell(origin); s != nil {
		if g, own := s.extTab(in); g != nil {
			return s, g, own
		}
	}
	if s := h.w.shell; s != nil && s.Mounted() {
		if g, own := s.extTab(in); g != nil {
			return s, g, own
		}
	}
	return nil, nil, nil
}

// OpenTabAt implements exthost.EditorHost: the drop of a drag, which is in the window of the host. A tab of a kind that is
// shared between screens is given to the views of the clients as well, behind what they show.
func (h extEditorHost) OpenTabAt(in *editorgroup.ExtensionTabInput, group, index int) error {
	s, err := h.shellOrErr("open a tab")
	if err != nil {
		return err
	}
	if err := h.open(s, in, group, false, index); err != nil {
		return err
	}
	if scope := h.w.Extensions(); scope != nil && scope.SharedTab(in) {
		for _, screen := range h.w.screenShells() {
			if screen != s {
				if err := h.open(screen, in.Mirror(), 0, true, -1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// open shows an input in a group of a shell, at an index of its strip when the index is not negative.
func (h extEditorHost) open(s *shellState, in *editorgroup.ExtensionTabInput, group int, inactive bool, index int) error {
	win := PeekWorkspace(s.Context())
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

// CloseTab implements exthost.EditorHost: the tab closes on every screen that shows it.
func (h extEditorHost) CloseTab(in *editorgroup.ExtensionTabInput) {
	h.w.closeTabOnScreens(in, nil)
}

// RevealTab implements exthost.EditorHost.
func (h extEditorHost) RevealTab(in *editorgroup.ExtensionTabInput, focus bool, origin extension.Origin) {
	s, g, own := h.tabOn(in, origin)
	if s == nil {
		return
	}
	s.SetState(func() {
		g.Model.SetActive(own)
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
func (h extEditorHost) GroupOf(in *editorgroup.ExtensionTabInput, origin extension.Origin) int {
	if _, g, _ := h.tabOn(in, origin); g != nil {
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
	made := false
	if side {
		if err := s.group.grid.Activate(target); err != nil {
			h.w.Process.Logger.Error("cannot activate the group", "err", err)
		}
		if target, made = s.sideGroupMade(); target == nil {
			target = s.groupFor(group) // the group for the side cannot be made: the file opens where it is asked from
		}
	}
	// an open document of the file opens in the group at once; the file is read otherwise
	if doc, ok := h.w.Documents.Get(u); ok {
		h.w.Documents.Retain(doc)
		s.show(widgets.WidgetsBindingOf(s.Context()), doc, openOptions{Group: target, FileView: viewID})
		return nil
	}
	s.openPathAt(widgets.WidgetsBindingOf(s.Context()), u.Path, 0, 0, openOptions{Group: target, FileView: viewID, MadeGroup: made})
	return nil
}
