package app

import (
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
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
	s, err := h.shellOrErr("open a tab")
	if err != nil {
		return err
	}
	win := h.w
	g := s.groupFor(group)
	s.SetState(func() {
		s.nav.hold++
		g.Model.OpenEditor(in, editorgroup.OpenOptions{Pinned: true, Active: !inactive})
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
