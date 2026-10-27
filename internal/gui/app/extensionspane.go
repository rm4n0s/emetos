package app

import (
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/extensionsui"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The page "Extensions" in the window (SPEC §10.15, step 11.1): the kind of editor of an ExtensionsInput, the command
// that opens it, and what its Enable and Disable buttons do (write extensions.disabled).
//
// Ported from VS Code: src/vs/workbench/contrib/extensions/browser/extensions.contribution.ts (the id and the title of the
// command Show Installed Extensions); the page itself is Emetos's own, since VS Code's extensions are installed from a
// marketplace.

// extensionsKind is how the window handles ExtensionsInputs.
func extensionsKind() editorKind {
	return editorKind{
		typeID: editorgroup.ExtensionsInputTypeID,
		icon:   fonts.CodiconExtensions,
		restore: func(string) (editorgroup.EditorInput, bool) {
			return editorgroup.NewExtensionsInput(), true
		},
		newPane: func(s *shellState, input editorgroup.EditorInput) pane {
			return &extensionsPane{s: s, input: input.(*editorgroup.ExtensionsInput)}
		},
	}
}

// extensionsPane is the editor of an ExtensionsInput: the page of the extensions.
type extensionsPane struct {
	paneFocus
	s     *shellState
	input *editorgroup.ExtensionsInput
}

// build implements pane.
func (p *extensionsPane) build(bc widgets.BuildContext, group *editorgroup.Group, _ bool) widgets.Widget {
	win := PeekWorkspace(bc)
	return p.wrap(p.s, group, extensionsui.NewPage(extensionsui.Options{
		Rows:            func() []exthost.Row { return win.Process.Extensions.Rows(win.disabledExtensions()) },
		Problems:        win.Process.Extensions.Problems,
		Subscribe:       win.Process.Extensions.OnChange,
		OnManageSharing: p.s.openEventSharingPage,
		OnToggle: func(id string, enable bool) {
			if err := win.setExtensionEnabled(id, enable); err != nil {
				win.Process.Logger.Warn("cannot write the setting", "err", err)
				win.Notify.Warn("Emetos", "Cannot save the setting: "+describe(err))
			}
		},
	}))
}

// focus implements pane: the page has nothing that takes the keyboard, so the pane keeps the focus of its group.
func (p *extensionsPane) focus() {}

// dispose implements pane.
func (p *extensionsPane) dispose() {}

// openExtensionsPage shows the page: the one a group has is revealed, else a new one opens in the active group, kept
// and not a preview.
func (s *shellState) openExtensionsPage() {
	s.openSingletonPage(func(e editorgroup.EditorInput) bool { _, ok := e.(*editorgroup.ExtensionsInput); return ok },
		func() editorgroup.EditorInput { return editorgroup.NewExtensionsInput() })
}

// openSingletonPage shows a page of which a window has one: the editor that is, if a group has one, is revealed, else a
// new one opens in the active group, kept and not a preview.
func (s *shellState) openSingletonPage(is func(editorgroup.EditorInput) bool, create func() editorgroup.EditorInput) {
	var input editorgroup.EditorInput
	var group *editorgroup.Group
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		for _, e := range g.Model.Editors(editorgroup.Sequential, false) {
			if is(e) && input == nil {
				input, group = e, g
			}
		}
	}
	win := PeekWorkspace(s.Context())
	if input == nil {
		input, group = create(), s.activeGroup()
		s.SetState(func() {
			s.nav.hold++
			group.Model.OpenEditor(input, editorgroup.OpenOptions{Pinned: true, Active: true})
			s.nav.hold--
			s.syncNav()
			if err := s.group.grid.Activate(group); err != nil {
				win.Process.Logger.Error("cannot activate the group", "err", err)
			}
		})
		s.refreshKeys()
		return
	}
	s.SetState(func() {
		group.Model.SetActive(input)
		if err := s.group.grid.Activate(group); err != nil {
			win.Process.Logger.Error("cannot activate the group", "err", err)
		}
	})
	s.refreshKeys()
}

// extensionsCommands are the handlers of the commands of the page.
func (s *shellState) extensionsCommands() map[string]command.Handler {
	return map[string]command.Handler{
		"workbench.extensions.action.showInstalledExtensions": func(command.Context, any) error {
			s.openExtensionsPage()
			return nil
		},
		"emetos.extensions.manageEventSharing": func(command.Context, any) error {
			s.openEventSharingPage()
			return nil
		},
	}
}
