package app

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"time"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// File editors in the window (SPEC §10.37 "File editors", step 36.1): a file that matches a default file editor of an extension (or
// that workbench.editorAssociations sends to one) opens in a FileEditorInput, whose pane shows the extension's box; Reopen Editor
// With switches a file between the text editor and its file editors; the binary notice offers Open With... too.
//
// Ported from VS Code: src/vs/workbench/services/editor/browser/openWithEditorPicker.ts (the pick of Reopen Editor With and of the
// default editor) and src/vs/workbench/browser/parts/editor/editorCommands.ts (workbench.action.reopenWithEditor).

// editorAssociationsSetting is VS Code's setting that chooses the editor of the files a glob matches.
const editorAssociationsSetting = "workbench.editorAssociations"

// fileEditorKind is how the window handles FileEditorInputs.
func fileEditorKind() editorKind {
	return editorKind{
		typeID: editorgroup.FileEditorInputTypeID,
		restore: func(value string) (editorgroup.EditorInput, bool) {
			in, err := editorgroup.ParseFileEditorInput(value)
			return in, err == nil
		},
		newPane: func(s *shellState, input editorgroup.EditorInput) pane {
			in := input.(*editorgroup.FileEditorInput)
			win := PeekWorkspace(s.Context())
			if scope := win.Extensions(); scope != nil {
				in.SetStateFunc(func() json.RawMessage { return scope.FileEditorState(in) })
			}
			// a file outside the folder is watched while a tab shows it, so that its editor hears of its changes
			win.followFile(in.Path())
			return &fileEditorPane{s: s, input: in}
		},
	}
}

// fileEditorPane is the editor of a FileEditorInput: the box of the extension's file editor.
type fileEditorPane struct {
	paneFocus
	s     *shellState
	input *editorgroup.FileEditorInput
	// fallingBack says that the tab is being reopened in the text editor (its file editor is gone).
	fallingBack bool
}

// build implements pane.
func (p *fileEditorPane) build(bc widgets.BuildContext, group *editorgroup.Group, _ bool) widgets.Widget {
	win := PeekWorkspace(bc)
	scope := win.Extensions()
	if scope == nil {
		return p.wrap(p.s, group, widgets.NewSizedBox(nil))
	}
	body, ok, failure := scope.FileEditorBody(exthost.FileEditorRequest{
		Key: p.input, EditorID: p.input.Editor(), Path: p.input.Path(), Folder: win.Folder, State: p.input.RestoredState(),
	})
	if !ok {
		// the editor is gone (its extension was turned off, or failed): the file opens in the text editor in the tab's place
		if !p.fallingBack {
			p.fallingBack = true
			in := p.input
			widgets.WidgetsBindingOf(bc).Scheduler().AddPostFrameCallback(func(time.Duration) {
				if p.s.Mounted() {
					p.s.reopenWith(group, in, "")
				}
			})
		}
		return p.wrap(p.s, group, widgets.NewSizedBox(nil))
	}
	if failure != nil {
		in := p.input
		return p.wrap(p.s, group, editor.NewNotice("The file cannot be shown: "+describe(failure), editor.NoticeOpts{
			Action: "Open in Text Editor", OnAction: func() { p.s.reopenWith(group, in, "") },
		}))
	}
	return p.wrap(p.s, group, body)
}

// focus implements pane: the widget of the extension takes the keyboard itself.
func (p *fileEditorPane) focus() {}

// dispose implements pane.
func (p *fileEditorPane) dispose() {}

// closed is called when the tab closes for good, or is reopened in another editor: the extension is told.
func (p *fileEditorPane) closed() {
	win := PeekWorkspace(p.s.Context())
	if scope := win.Extensions(); scope != nil {
		scope.ReleaseFileEditor(p.input)
	}
	// the watch of the file stops unless a document or another tab still shows it (following is not counted)
	path := p.input.Path()
	if _, open := win.Documents.Get(document.FileURI(path)); open {
		return
	}
	for in := range p.s.group.panes {
		if other, ok := in.(*editorgroup.FileEditorInput); ok && other != p.input && other.Path() == path {
			return
		}
	}
	win.unfollowFile(path)
}

// editorAssociations is workbench.editorAssociations of the window tab.
func (w *Workspace) editorAssociations() map[string]string {
	raw := settings.Get[map[string]any](w.Settings, editorAssociationsSetting, "")
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if id, ok := v.(string); ok {
			out[k] = id
		}
	}
	return out
}

// fileEditorFor is the file editor a file opens in, or "" for the text editor.
func (w *Workspace) fileEditorFor(path string) string {
	scope := w.Extensions()
	if scope == nil {
		return ""
	}
	return scope.ResolveFileEditor(path, w.editorAssociations())
}

// showFileEditor shows a file in a file editor, as showAcquired shows a document: in the group of opts (the active group when nil),
// as the preview editor when opts ask for it, revealing the tab of the group that shows the file already in any editor.
func (s *shellState) showFileEditor(binding *widgets.WidgetsBinding, path, editorID string, opts openOptions) {
	s.SetState(func() {
		s.blocked = ""
		s.placeFileEditor(binding, path, editorID, opts)
	})
	s.refreshKeys()
}

// placeFileEditor is showFileEditor without the rebuild: the shell's InitState places the files of the command line with it.
func (s *shellState) placeFileEditor(binding *widgets.WidgetsBinding, path, editorID string, opts openOptions) {
	win := PeekWorkspace(s.Context())
	group := opts.Group
	if group == nil || s.group.grid.Group(group.ID()) != group {
		group = s.activeGroup()
	}
	{
		if opts.PreserveFocus {
			s.holdFocus(binding)
		}
		win.AddRecentFile(path)
		s.nav.hold++
		var shown editorgroup.EditorInput
		for _, e := range group.Model.Editors(editorgroup.Sequential, false) {
			if p, ok := fileResource(e); ok && p == path {
				shown = e
				break
			}
		}
		if shown != nil {
			if !opts.Inactive {
				group.Model.SetActive(shown)
			}
			if !opts.Preview {
				group.Model.Pin(shown)
			}
		} else {
			preview := opts.Preview && settings.Get[bool](win.Settings, "workbench.editor.enablePreview", "")
			group.Model.OpenEditor(editorgroup.NewFileEditorInput(path, editorID), editorgroup.OpenOptions{
				Pinned: !preview, Active: !opts.Inactive, Sticky: opts.Sticky, Index: opts.Index, HasIndex: opts.HasIndex,
			})
		}
		if !opts.Inactive {
			if err := s.group.grid.Activate(group); err != nil {
				win.Process.Logger.Error("cannot activate the group", "err", err)
			}
		}
		if opts.TakeFocus && !opts.Inactive {
			s.askFocus(binding)
		}
		s.nav.hold--
		s.syncNav()
	}
}

// reopenWith shows the file of an editor of a group in another editor at the place of its tab: editorID is a file editor, or "" for
// the text editor. The old tab closes (a file editor is told it closed).
func (s *shellState) reopenWith(group *editorgroup.Group, input editorgroup.EditorInput, editorID string) {
	path, ok := fileResource(input)
	if !ok || s.group.grid.Group(group.ID()) != group {
		return
	}
	if fe, isFE := input.(*editorgroup.FileEditorInput); isFE && fe.Editor() == editorID {
		return
	}
	index := slices.Index(group.Model.Editors(editorgroup.Sequential, false), input)
	opts := openOptions{Group: group, Index: index, HasIndex: index >= 0, TakeFocus: true, Text: editorID == ""}
	binding := widgets.WidgetsBindingOf(s.Context())
	s.closeEditors(group, []editorgroup.EditorInput{input})
	if editorID != "" {
		s.showFileEditor(binding, path, editorID, opts)
		return
	}
	s.openPathAt(binding, path, 0, 0, opts)
}

// editorPickItems are the items of the pick of Reopen Editor With for a file: the text editor first, then the file editors that match
// it, each with Active (the editor the tab shows now: current, "" for text), Default (the one the file opens in) or both.
func editorPickItems(editors []exthost.FileEditorInfo, current, dflt string, choose func(id string)) []quickinput.Item {
	describe := func(id string) string {
		switch {
		case id == current && id == dflt:
			return "Active and Default"
		case id == current:
			return "Active"
		case id == dflt:
			return "Default"
		}
		return ""
	}
	items := []quickinput.Item{{ID: exthost.TextEditorID, Label: "Text Editor", Description: describe(""), Detail: "Built-in",
		Accept: func(quickinput.KeyMods) { choose("") }}}
	for _, e := range editors {
		id := e.ID
		items = append(items, quickinput.Item{ID: id, Label: e.Name, Description: describe(id), Detail: e.Extension, Accept: func(quickinput.KeyMods) { choose(id) }})
	}
	return items
}

// associationPattern is the key Configure default editor writes for a file: "*" and its extension (VS Code's associationPattern).
func associationPattern(path string) string { return "*" + filepath.Ext(path) }

// reopenWithEditor runs workbench.action.reopenWithEditor for an editor of a group: the pick of the editors of its file.
func (s *shellState) reopenWithEditor(group *editorgroup.Group, input editorgroup.EditorInput) {
	path, ok := fileResource(input)
	if !ok {
		return
	}
	win := PeekWorkspace(s.Context())
	scope := win.Extensions()
	if scope == nil {
		return
	}
	editors := scope.FileEditorsFor(path)
	current := ""
	if fe, isFE := input.(*editorgroup.FileEditorInput); isFE {
		current = fe.Editor()
	}
	dflt := win.fileEditorFor(path)
	items := editorPickItems(editors, current, dflt, func(id string) {
		if s.Mounted() {
			s.reopenWith(group, input, id)
		}
	})
	if ext := filepath.Ext(path); ext != "" {
		pattern := associationPattern(path)
		items = append(items, quickinput.Item{Separator: true}, quickinput.Item{ID: "configureDefault", Label: "Configure default editor for '" + pattern + "'...",
			Accept: func(quickinput.KeyMods) { s.configureDefaultEditor(path, editors, current, dflt) }})
	}
	activeID := current
	if activeID == "" {
		activeID = exthost.TextEditorID
	}
	win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select editor for '" + filepath.Base(path) + "'", Items: items, ActiveID: activeID})
}

// configureDefaultEditor asks for the editor the files with the extension of path open in, and writes it in the user's
// workbench.editorAssociations.
func (s *shellState) configureDefaultEditor(path string, editors []exthost.FileEditorInfo, current, dflt string) {
	win := PeekWorkspace(s.Context())
	pattern := associationPattern(path)
	items := editorPickItems(editors, current, dflt, func(id string) {
		value := exthost.TextEditorID
		if id != "" {
			value = id
		}
		assoc := map[string]any{}
		for k, v := range settings.Get[map[string]any](win.Settings, editorAssociationsSetting, "") {
			assoc[k] = v
		}
		assoc[pattern] = value
		if err := win.Settings.Update(win.ctx, editorAssociationsSetting, assoc, settings.TargetUser); err != nil {
			s.settingsWriteFailed(err)
		}
	})
	win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select new default editor for '" + pattern + "'", Items: items, ActiveID: map[bool]string{true: exthost.TextEditorID, false: dflt}[dflt == ""]})
}

// fileEditorCommands are the handlers of the commands of the file editors.
func (s *shellState) fileEditorCommands() map[string]command.Handler {
	return map[string]command.Handler{
		"workbench.action.reopenWithEditor": func(_ command.Context, args any) error {
			group, input := s.commandTarget(args)
			if group != nil && input != nil {
				s.reopenWithEditor(group, input)
			}
			return nil
		},
	}
}

// FileEditorGroup implements exthost.EditorHost: the group that shows the file editor tab of the key (its input).
func (h extEditorHost) FileEditorGroup(key any) int {
	s, err := h.shellOrErr("find a file editor")
	if err != nil {
		return 0
	}
	in, ok := key.(editorgroup.EditorInput)
	if !ok {
		return 0
	}
	if g := s.groupHolding(in); g != nil {
		return g.ID()
	}
	return 0
}

// startupFileEditor is a file of the command line that opens in a file editor.
type startupFileEditor struct{ path, editor string }

// openBlockedWith is Open With... of the notice of a file that looks binary: the pick of the text editor (Open Anyway) and the file
// editors of the file.
func (s *shellState) openBlockedWith(path string) {
	win := PeekWorkspace(s.Context())
	scope := win.Extensions()
	if scope == nil {
		return
	}
	binding := widgets.WidgetsBindingOf(s.Context())
	// no editor shows the file yet: none is marked Active
	const noneActive = "\x00"
	items := editorPickItems(scope.FileEditorsFor(path), noneActive, win.fileEditorFor(path), func(id string) {
		if !s.Mounted() {
			return
		}
		if id == "" {
			s.openAnyway(binding, path)
			return
		}
		s.showFileEditor(binding, path, id, openOptions{TakeFocus: true})
	})
	win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select editor for '" + filepath.Base(path) + "'", Items: items})
}

// watchFileEditorEvents makes the changes of files on disk reach the file editors that show them, on the UI thread.
func (w *Workspace) watchFileEditorEvents() {
	w.stops = append(w.stops, w.OnFileEvents(func(events []workspace.Event) {
		type change struct {
			path    string
			deleted bool
		}
		// only the files a file editor shows are handed to the UI thread: a change of any other file costs no frame
		scope := w.Extensions()
		if scope == nil {
			return
		}
		var changes []change
		add := func(path string, deleted bool) {
			if scope.ShowsFile(path) {
				changes = append(changes, change{path, deleted})
			}
		}
		for _, e := range events {
			switch e.Kind {
			case workspace.EventDeleted:
				add(e.Path, true)
			case workspace.EventRenamed:
				add(e.OldPath, true)
				add(e.Path, false)
			case workspace.EventCreated, workspace.EventChanged:
				add(e.Path, false)
			}
		}
		if len(changes) == 0 {
			return
		}
		w.onUI(func() {
			now := w.Extensions() // the extensions may have been restarted meanwhile
			if now == nil {
				return
			}
			for _, c := range changes {
				now.FileEditorChanged(c.path, c.deleted)
			}
		})
	}))
}
