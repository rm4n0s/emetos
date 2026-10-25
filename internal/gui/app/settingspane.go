package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/jsonc"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/settingsui"
)

// The settings editor in the window (SPEC §11.1, step 8.1): the kind of editor of a SettingsInput, the commands
// that open it, and what its buttons do (opening settings.json in a text editor).
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/preferences.contribution.ts and
// src/vs/workbench/services/preferences/browser/preferencesService.ts (openSettings, openWorkspaceSettings)

// settingsKind is how the window handles SettingsInputs.
func settingsKind() editorKind {
	return editorKind{
		typeID: editorgroup.SettingsInputTypeID,
		icon:   fonts.CodiconSettingsGear,
		restore: func(value string) (editorgroup.EditorInput, bool) {
			in, err := editorgroup.ParseSettingsInput(value)
			if err != nil {
				return nil, false
			}
			return in, true
		},
		newPane: func(s *shellState, input editorgroup.EditorInput) pane {
			return &settingsPane{s: s, input: input.(*editorgroup.SettingsInput), ctl: settingsui.NewController()}
		},
	}
}

// settingsPane is the editor of a SettingsInput: the settings editor widget, with the view state kept in the input.
type settingsPane struct {
	paneFocus
	s     *shellState
	input *editorgroup.SettingsInput
	ctl   *settingsui.Controller
}

// build implements pane.
func (p *settingsPane) build(bc widgets.BuildContext, group *editorgroup.Group, _ bool) widgets.Widget {
	win := PeekWindow(bc)
	// the code spans of the descriptions are in the font of the editor
	code, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().MonospaceFont()
	if first, _, _ := strings.Cut(settings.Get[string](win.Settings, "editor.fontFamily", ""), ","); strings.Trim(strings.TrimSpace(first), `"'`) != "" {
		code = strings.Trim(strings.TrimSpace(first), `"'`)
	}
	return p.wrap(p.s, group, settingsui.NewEditor(settingsui.Options{
		Model: win.SettingsModel(), Context: win.ctx, State: p.input.ViewState(),
		OnState: p.noteState, HasFolder: win.Folder != "",
		OnOpenJSON: p.s.runOpenJSON, OnError: p.s.settingsWriteFailed, CopyText: p.s.copyText,
		Languages: win.resolveLanguage, CodeFont: code, Controller: p.ctl,
	}))
}

// noteState keeps the view of the editor in its input, which the state of the window saves.
func (p *settingsPane) noteState(st editorgroup.SettingsViewState) {
	p.input.SetViewState(st)
	p.s.stateChangedIfMounted()
}

// focus implements pane: the search box takes the focus, as VS Code's settings editor does.
func (p *settingsPane) focus() { p.ctl.FocusSearch() }

// dispose implements pane.
func (p *settingsPane) dispose() {}

// SettingsModel is the model of the settings editor of the window, over its store.
func (w *Window) SettingsModel() *settings.UIModel {
	if w.uiModel == nil {
		w.uiModel = settings.NewUIModel(w.Settings)
	}
	return w.uiModel
}

// settingsWriteFailed reports a change of a setting that could not be written.
func (s *shellState) settingsWriteFailed(err error) {
	win := PeekWindow(s.Context())
	win.Process.Logger.Warn("cannot write the setting", "err", err)
	win.Notify.Warn("Emetos", "Cannot save the setting: "+describe(err))
}

// openSettingsEditor shows the settings editor: the one a group has is revealed (it keeps the tab and the place it
// had unless the arguments ask for another), else a new one opens in the active group, kept and not a preview.
// target switches the tab ("" keeps it); query, when not empty, fills the search box.
func (s *shellState) openSettingsEditor(target editorgroup.SettingsTarget, query string) {
	var input *editorgroup.SettingsInput
	var group *editorgroup.Group
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		for _, e := range g.Model.Editors(editorgroup.Sequential, false) {
			if si, ok := e.(*editorgroup.SettingsInput); ok && input == nil {
				input, group = si, g
			}
		}
	}
	win := PeekWindow(s.Context())
	if target == editorgroup.SettingsWorkspace && win.Folder == "" {
		target = ""
	}
	if input == nil {
		st := editorgroup.SettingsViewState{Target: target, Query: query}
		input, group = editorgroup.NewSettingsInput(st), s.activeGroup()
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
	if p := s.group.panes[input]; p != nil {
		if sp, ok := p.(*settingsPane); ok {
			if target != "" {
				sp.ctl.SetTarget(target)
			}
			if query != "" {
				sp.ctl.SetQuery(query)
			}
			sp.ctl.FocusSearch()
		}
	}
	s.refreshKeys()
}

// settingsCommands are the handlers of the commands that open the settings editor and the settings files. The
// argument of Settings, Open Settings (UI), Open User Settings and Open Workspace Settings is the search text (a
// string, or {"query": "..."}), as VS Code's, and {"revealSetting": {"key": "editor.fontSize"}} searches for that
// setting; with workbench.settings.editor set to json they open settings.json instead. Open User Settings (JSON) and
// Open Workspace Settings (JSON) open the files, with the cursor on {"revealSetting": {"key": ...}} when the file has
// it, and Open Default Settings (JSON) opens the read-only document of every default (SPEC §7.1, §11.1).
func (s *shellState) settingsCommands() map[string]command.Handler {
	open := func(target editorgroup.SettingsTarget) command.Handler {
		return func(_ command.Context, args any) error {
			win := PeekWindow(s.Context())
			opts := settingsArgsOf(args)
			if settings.Get[string](win.Settings, "workbench.settings.editor", "") == "json" {
				t := settings.TargetUser
				if target == editorgroup.SettingsWorkspace {
					t = settings.TargetWorkspace
				}
				s.openSettingsJSON(t, opts.reveal)
				return nil
			}
			query := opts.query
			if query == "" && opts.reveal != "" {
				query = settings.IDFilter + opts.reveal
			}
			s.openSettingsEditor(target, query)
			return nil
		}
	}
	file := func(target settings.Target) command.Handler {
		return func(_ command.Context, args any) error {
			s.openSettingsJSON(target, settingsArgsOf(args).reveal)
			return nil
		}
	}
	return map[string]command.Handler{
		"workbench.action.openSettings":              open(editorgroup.SettingsUser),
		"workbench.action.openSettings2":             open(editorgroup.SettingsUser),
		"workbench.action.openGlobalSettings":        open(editorgroup.SettingsUser),
		"workbench.action.openWorkspaceSettings":     open(editorgroup.SettingsWorkspace),
		"workbench.action.openSettingsJson":          file(settings.TargetUser),
		"workbench.action.openWorkspaceSettingsFile": file(settings.TargetWorkspace),
		"workbench.action.openRawDefaultSettings": func(command.Context, any) error {
			return s.openDefaultSettings()
		},
	}
}

// settingsArgs are the arguments of the commands that open settings (VS Code's IOpenSettingsActionOptions, the part
// Emetos uses).
type settingsArgs struct {
	// query is the search text, reveal the key of {"revealSetting": {"key"}}.
	query, reveal string
}

// settingsArgsOf reads the argument of a command that opens settings: a string is the search text, an object may
// have a "query" and a "revealSetting" with a "key". Anything else is ignored, as VS Code's sanitizeOpenSettingsArgs
// does.
func settingsArgsOf(args any) settingsArgs {
	switch a := args.(type) {
	case string:
		return settingsArgs{query: a}
	case map[string]any:
		var out settingsArgs
		if q, ok := a["query"].(string); ok {
			out.query = q
		}
		if r, ok := a["revealSetting"].(map[string]any); ok {
			if key, ok := r["key"].(string); ok {
				out.reveal = key
			}
		}
		return out
	}
	return settingsArgs{}
}

// runOpenJSON is the button and the link of the settings editor: they run the command for the file of the tab, as
// VS Code's do, so that a key bound to the command or a handler that replaces it takes part.
func (s *shellState) runOpenJSON(target settings.Target, key string) {
	win := PeekWindow(s.Context())
	id := "workbench.action.openSettingsJson"
	if target == settings.TargetWorkspace {
		id = "workbench.action.openWorkspaceSettingsFile"
	}
	var args any
	if key != "" {
		args = map[string]any{"revealSetting": map[string]any{"key": key}}
	}
	if err := win.Commands.Run(command.Context{Ctx: win.ctx, Keys: win.Keys, Source: command.SourceAPI}, id, args); err != nil {
		win.Process.Logger.Warn("cannot open the settings file", "err", err)
		win.Notify.Error("Emetos", "Cannot open the settings file: "+describe(err))
	}
}

// openDefaultSettings shows the read-only document of the default settings (made again from the registry each time).
func (s *shellState) openDefaultSettings() error {
	win := PeekWindow(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	doc, err := win.Documents.AcquireVirtual(win.ctx, document.VirtualURI(DefaultSettingsPath))
	if err != nil {
		return err
	}
	s.show(binding, doc, openOptions{})
	return nil
}

// resolveLanguage finds a language by its id or its name for the filter @lang: of the settings editor.
func (w *Window) resolveLanguage(name string) (id, display string, ok bool) {
	id, ok = w.Process.Languages.ByName(strings.TrimSpace(name))
	if !ok {
		return "", "", false
	}
	display = id
	if l, found := w.Process.Languages.Language(id); found && l.Name != "" {
		display = l.Name
	}
	return id, display, true
}

// ---- settings.json ----

// ensureSettingsFile makes the file of settings that does not exist yet, with an empty object, so that an editor
// can open it. It fails with the tag SettingsFileCreateFailed.
func ensureSettingsFile(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errors.NewErr("SettingsFileCreateFailed", err).SetMetadata("path", path)
	}
	if _, err := fileio.Write(ctx, path, "{\n}\n", fileio.WriteOptions{Encoding: fileio.UTF8, LineEnding: fileio.LF}); err != nil {
		return err
	}
	return nil
}

// settingsKeyPosition is the line and the column, counted from 1, of the name of a setting in the text of a settings
// file, or 0, 0 when it is not there. A setting written as nested objects ("editor": {"tabSize": 2}) is found too.
func settingsKeyPosition(text, key string) (line, column int) {
	root, _ := jsonc.ParseTree(text, jsonc.ParseOptions{AllowEmptyContent: true})
	if root == nil {
		return 0, 0
	}
	node := jsonc.FindNodeAtLocation(root, jsonc.Path{key})
	if node == nil {
		path := jsonc.Path{}
		for _, part := range strings.Split(key, ".") {
			path = append(path, part)
		}
		node = jsonc.FindNodeAtLocation(root, path)
	}
	if node == nil || node.Parent == nil || node.Parent.Type != jsonc.PropertyNode {
		return 0, 0
	}
	offset := node.Parent.Offset
	before := text[:offset]
	line = strings.Count(before, "\n") + 1
	column = offset - strings.LastIndex(before, "\n")
	return line, column
}

// openSettingsJSON opens the settings.json of a target in a text editor (the file is made when it does not exist),
// with the cursor on the name of a setting when key is not empty and the file has it.
func (s *shellState) openSettingsJSON(target settings.Target, key string) {
	win := PeekWindow(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	user, workspace := win.Settings.Paths()
	path := user
	if target == settings.TargetWorkspace {
		path = workspace
	}
	if path == "" {
		win.Notify.Warn("Emetos", "Open a folder to edit the settings of the workspace.")
		return
	}
	s.Go(func(life context.Context) {
		err := ensureSettingsFile(life, path)
		line, column := 0, 0
		if err == nil && key != "" {
			// the document that is open has the text the cursor goes in, which may not be what the file has
			text, open := win.settingsDocs.OpenText(path)
			if !open {
				if res, rerr := fileio.Read(life, path, fileio.ReadOptions{}); rerr == nil {
					text = res.Text
				}
			}
			line, column = settingsKeyPosition(text, key)
		}
		binding.Post(func() {
			if !s.Mounted() {
				return
			}
			if err != nil {
				win.Process.Logger.Warn("cannot make the settings file", "err", err)
				win.Notify.Error("Emetos", "Cannot open "+path+": "+describe(err))
				return
			}
			s.openPathAt(binding, path, line, column, openOptions{})
		})
	})
}
