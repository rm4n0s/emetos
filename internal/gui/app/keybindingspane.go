package app

import (
	"context"
	"strings"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/keybindingsui"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The keybindings editor in the window (SPEC §11.3, step 8.3): the kind of editor of a KeybindingsInput, the
// commands that open it and the files of the keybindings, and the commands it runs on its rows.
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/preferences.contribution.ts and
// src/vs/workbench/services/preferences/browser/preferencesService.ts (openGlobalKeybindingSettings,
// openDefaultKeybindingsFile)

const (
	// DefaultKeybindingsPath is the path of the URI of the default keybindings document,
	// emetos-virtual:/defaultKeybindings.json.
	DefaultKeybindingsPath = "/defaultKeybindings.json"
	// DefaultKeybindingsLabel is the name of the default keybindings document in its tab.
	DefaultKeybindingsLabel = "Default Keyboard Shortcuts"
)

// keybindingsKind is how the window handles KeybindingsInputs.
func keybindingsKind() editorKind {
	return editorKind{
		typeID: editorgroup.KeybindingsInputTypeID,
		icon:   fonts.CodiconRecordKeys,
		restore: func(value string) (editorgroup.EditorInput, bool) {
			in, err := editorgroup.ParseKeybindingsInput(value)
			if err != nil {
				return nil, false
			}
			return in, true
		},
		newPane: func(s *shellState, input editorgroup.EditorInput) pane {
			return &keybindingsPane{s: s, input: input.(*editorgroup.KeybindingsInput), ctl: keybindingsui.NewController()}
		},
	}
}

// keybindingsPane is the editor of a KeybindingsInput: the keybindings editor widget, with the view state kept in
// the input.
type keybindingsPane struct {
	paneFocus
	s     *shellState
	input *editorgroup.KeybindingsInput
	ctl   *keybindingsui.Controller
}

// build implements pane.
func (p *keybindingsPane) build(bc widgets.BuildContext, group *editorgroup.Group, _ bool) widgets.Widget {
	win := PeekWorkspace(bc)
	code, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().MonospaceFont()
	if first, _, _ := strings.Cut(settings.Get[string](win.Settings, "editor.fontFamily", ""), ","); strings.Trim(strings.TrimSpace(first), `"'`) != "" {
		code = strings.Trim(strings.TrimSpace(first), `"'`)
	}
	return p.wrap(p.s, group, keybindingsui.NewEditor(keybindingsui.Options{
		Model:     win.KeybindingsModel,
		Subscribe: win.userKeys.subscribe,
		Actions:   p.s.keybindingActions(),
		State:     p.input.ViewState(),
		OnState:   p.noteState,
		OnOpenJSON: func() {
			if err := win.runCommand("workbench.action.openGlobalKeybindingsFile", nil); err != nil {
				win.Process.Logger.Warn("cannot open keybindings.json", "err", err)
				win.Notify.Error("Emetos", "Cannot open keybindings.json: "+describe(err))
			}
		},
		OnError: p.s.keybindingEditFailed, CopyText: p.s.copyText,
		Record: win.startRecording, KeyLabel: win.keyLabelOf, Dialogs: win.Dialogs,
		Keys: win.Keys, Scopes: win.Scopes, CodeFont: code, Controller: p.ctl,
	}))
}

// noteState keeps the view of the editor in its input, which the state of the window saves.
func (p *keybindingsPane) noteState(st editorgroup.KeybindingsViewState) {
	p.input.SetViewState(st)
	p.s.stateChangedIfMounted()
}

// focus implements pane: the row that was selected takes the focus, else the search box, as VS Code's editor does.
func (p *keybindingsPane) focus() {
	if _, ok := p.ctl.Selected(); ok {
		p.ctl.FocusRows()
		return
	}
	p.ctl.FocusSearch()
}

// dispose implements pane.
func (p *keybindingsPane) dispose() {}

// keybindingActions are the edits of the keybindings editor to keybindings.json.
func (s *shellState) keybindingActions() keybindingsui.Actions {
	win := PeekWorkspace(s.Context())
	edit := func(op func(text string, e keybinding.Editing) (string, error)) error {
		return win.userKeys.Edit(win.ctx, op)
	}
	return keybindingsui.Actions{
		Define: func(row keybinding.Row, add bool, key string, overwrite []keybinding.Row) error {
			return edit(func(text string, e keybinding.Editing) (string, error) {
				var err error
				// the new keybinding first: it does not move the entries that the others are found by
				if add {
					text, err = e.Add(text, row.Command, key, row.When, row.Args)
				} else {
					text, err = e.Edit(text, row, key, row.When)
				}
				if err != nil || len(overwrite) == 0 {
					return text, err
				}
				return e.Overwrite(text, overwrite)
			})
		},
		Remove: func(row keybinding.Row) error {
			return edit(func(t string, e keybinding.Editing) (string, error) { return e.Remove(t, row) })
		},
		Reset: func(row keybinding.Row) error {
			return edit(func(t string, e keybinding.Editing) (string, error) { return e.Reset(t, row) })
		},
		SetWhen: func(row keybinding.Row, when string) error {
			return edit(func(t string, e keybinding.Editing) (string, error) { return e.UpdateWhen(t, row, when) })
		},
	}
}

// keybindingEditFailed reports an edit of keybindings.json that failed.
func (s *shellState) keybindingEditFailed(err error) {
	win := PeekWorkspace(s.Context())
	win.Process.Logger.Warn("cannot change the keybinding", "err", err)
	win.Notify.Error("Emetos", "Cannot change the keybinding: "+describe(err)+" Open keybindings.json and check it for errors.")
}

// keybindingsPanes are the keybindings editors of the window, the one that has the focus first, then the one of
// the active group.
func (s *shellState) keybindingsPane() *keybindingsPane {
	var found []*keybindingsPane
	for _, p := range s.group.panes {
		if kp, ok := p.(*keybindingsPane); ok {
			found = append(found, kp)
		}
	}
	for _, kp := range found {
		if kp.hasFocus() {
			return kp
		}
	}
	if active := s.activeGroup().Model.ActiveEditor(); active != nil {
		if kp, ok := s.group.panes[active].(*keybindingsPane); ok {
			return kp
		}
	}
	if len(found) > 0 {
		return found[0]
	}
	return nil
}

// openKeybindingsEditor shows the keybindings editor: the one a group has is revealed (it keeps what it shows
// unless a query is given), else a new one opens in the active group, kept and not a preview.
func (s *shellState) openKeybindingsEditor(query string) {
	var input *editorgroup.KeybindingsInput
	var group *editorgroup.Group
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		for _, e := range g.Model.Editors(editorgroup.Sequential, false) {
			if ki, ok := e.(*editorgroup.KeybindingsInput); ok && input == nil {
				input, group = ki, g
			}
		}
	}
	win := PeekWorkspace(s.Context())
	// the search box takes the focus when the editor is built, some frames away; the keys typed until then go into it
	// (kit.TypeAhead), not into the editor that has the focus now
	typeAhead := func(input *editorgroup.KeybindingsInput) {
		kit.StartTypeAheadLater(widgets.WidgetsBindingOf(s.Context()), func() (*widgets.TextEditingController, *widgets.FocusNode) {
			if kp, ok := s.group.panes[input].(*keybindingsPane); ok {
				return kp.ctl.SearchInput()
			}
			return nil, nil
		}, func(text string) {
			if kp, ok := s.group.panes[input].(*keybindingsPane); ok {
				kp.ctl.SearchTyped(text)
			}
		})
	}
	if input == nil {
		input, group = editorgroup.NewKeybindingsInput(editorgroup.KeybindingsViewState{Query: query}), s.activeGroup()
		typeAhead(input)
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
	typeAhead(input)
	s.SetState(func() {
		group.Model.SetActive(input)
		if err := s.group.grid.Activate(group); err != nil {
			win.Process.Logger.Error("cannot activate the group", "err", err)
		}
	})
	if kp, ok := s.group.panes[input].(*keybindingsPane); ok {
		if query != "" {
			kp.ctl.SetQuery(query)
		}
		kp.ctl.FocusSearch()
	}
	s.refreshKeys()
}

// keybindingsCommands are the handlers of the commands of the keybindings editor. Open Keyboard Shortcuts takes the
// search text as its argument (a string, or {"query": "..."}), as VS Code's does. Open Keyboard Shortcuts (JSON)
// opens keybindings.json (made with an empty array when it does not exist) and Open Default Keyboard Shortcuts
// (JSON) the read-only document of the built-in keybindings. The keybindings.editor.* commands act on the editor
// that has the focus; their keys are bound while it has (SPEC §11.3).
func (s *shellState) keybindingsCommands() map[string]command.Handler {
	onPane := func(f func(p *keybindingsPane)) command.Handler {
		return func(command.Context, any) error {
			if p := s.keybindingsPane(); p != nil {
				f(p)
			}
			return nil
		}
	}
	return map[string]command.Handler{
		"workbench.action.openGlobalKeybindings": func(_ command.Context, args any) error {
			s.openKeybindingsEditor(keybindingsQueryOf(args))
			return nil
		},
		"workbench.action.openGlobalKeybindingsFile": func(command.Context, any) error {
			s.openKeybindingsJSON()
			return nil
		},
		"workbench.action.openDefaultKeybindingsFile": func(command.Context, any) error {
			return s.openDefaultKeybindings()
		},
		"keybindings.editor.searchKeybindings":          onPane(func(p *keybindingsPane) { p.ctl.FocusSearch() }),
		"keybindings.editor.clearSearchResults":         onPane(func(p *keybindingsPane) { p.ctl.ClearSearch() }),
		"keybindings.editor.recordSearchKeys":           onPane(func(p *keybindingsPane) { p.ctl.RecordSearchKeys() }),
		"keybindings.editor.focusKeybindings":           onPane(func(p *keybindingsPane) { p.ctl.FocusRows() }),
		"keybindings.editor.defineKeybinding":           onPane(func(p *keybindingsPane) { p.ctl.Define(false) }),
		"keybindings.editor.addKeybinding":              onPane(func(p *keybindingsPane) { p.ctl.Define(true) }),
		"keybindings.editor.defineWhenExpression":       onPane(func(p *keybindingsPane) { p.ctl.DefineWhen() }),
		"keybindings.editor.removeKeybinding":           onPane(func(p *keybindingsPane) { p.ctl.Remove() }),
		"keybindings.editor.resetKeybinding":            onPane(func(p *keybindingsPane) { p.ctl.Reset() }),
		"keybindings.editor.showConflicts":              onPane(func(p *keybindingsPane) { p.ctl.ShowSame() }),
		"keybindings.editor.copyKeybindingEntry":        onPane(func(p *keybindingsPane) { p.ctl.Copy("entry") }),
		"keybindings.editor.copyCommandKeybindingEntry": onPane(func(p *keybindingsPane) { p.ctl.Copy("command") }),
		"keybindings.editor.copyCommandTitle":           onPane(func(p *keybindingsPane) { p.ctl.Copy("title") }),
	}
}

// keybindingsQueryOf reads the argument of Open Keyboard Shortcuts: a string is the search text, an object may have
// a "query". Anything else is ignored.
func keybindingsQueryOf(args any) string {
	switch a := args.(type) {
	case string:
		return a
	case map[string]any:
		if q, ok := a["query"].(string); ok {
			return q
		}
	}
	return ""
}

// openKeybindingsJSON opens keybindings.json in a text editor; the file is made when it does not exist.
func (s *shellState) openKeybindingsJSON() {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	k := win.userKeys
	s.Go(func(life context.Context) {
		err := k.ensure(life)
		binding.Post(func() {
			if !s.Mounted() {
				return
			}
			if err != nil {
				win.Process.Logger.Warn("cannot make the keybindings file", "err", err)
				win.Notify.Error("Emetos", "Cannot open "+k.filePath()+": "+describe(err))
				return
			}
			s.openPathAt(binding, k.filePath(), 0, 0, openOptions{})
		})
	})
}

// openDefaultKeybindings shows the read-only document of the built-in keybindings (made again from the keymap each
// time).
func (s *shellState) openDefaultKeybindings() error {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	doc, err := win.Documents.AcquireVirtual(win.ctx, document.VirtualURI(DefaultKeybindingsPath))
	if err != nil {
		return err
	}
	s.show(binding, doc, openOptions{})
	return nil
}
