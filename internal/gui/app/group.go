package app

import (
	"context"
	"os"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The editor group of the window (SPEC §10.3): the tab strip, the breadcrumbs and the editor of the
// active tab. The group's data is an editorgroup.Model; this file keeps an editor (textEditor, with a
// view model of its own) for each text input of it, and follows the model's events. Step 6.9 makes
// several groups of it.

// groupState is what the shell keeps of its editor group.
type groupState struct {
	model *editorgroup.Model
	// editors are the editors of the inputs of the model.
	editors map[editorgroup.EditorInput]*textEditor
	// stop follows the model, and stopSettings the settings.
	stop, stopSettings func()
	// removeCommands take the handlers of the group commands off the registry.
	removeCommands []func()
}

// openOptions say how a document is shown in the group.
type openOptions struct {
	// Preview opens the editor as the preview editor, which the next preview replaces, when
	// workbench.editor.enablePreview is on; a navigation (the breadcrumbs, later the explorer) asks
	// for it, the command line and the other commands do not.
	Preview bool
	// Place, when set, runs once the editor exists (to put the cursor somewhere); the editor then
	// scrolls to the cursor.
	Place func(*textEditor)
}

// groupOptions are the settings the model follows.
func (s *shellState) groupOptions() editorgroup.Options {
	win := PeekWindow(s.Context())
	return editorgroup.Options{
		OpenPositioning:             editorgroup.OpenPositioning(settings.Get[string](win.Settings, "workbench.editor.openPositioning", "")),
		FocusRecentEditorAfterClose: settings.Get[bool](win.Settings, "workbench.editor.focusRecentEditorAfterClose", ""),
	}
}

// initGroup makes the model of the group and the editors for the documents the window starts with:
// the first is the active editor, and all are kept (not previews), as VS Code opens the files of the
// command line.
func (s *shellState) initGroup(binding *widgets.WidgetsBinding, docs []*document.Document) {
	win := PeekWindow(s.Context())
	g := &s.group
	g.model = editorgroup.NewModel(1, s.groupOptions())
	g.editors = map[editorgroup.EditorInput]*textEditor{}
	g.stop = g.model.OnChange(func(e editorgroup.ChangeEvent) { s.groupChanged(binding, e) })
	g.stopSettings = win.Settings.OnChange(func(e settings.ChangeEvent) {
		if e.Affects("workbench.editor", "") || e.Affects("breadcrumbs", "") {
			binding.Post(func() {
				if !s.Mounted() {
					return
				}
				s.group.model.SetOptions(s.groupOptions())
				s.SetState(func() {})
			})
		}
	})
	for _, doc := range docs {
		win.Documents.Retain(doc)
		s.showAcquired(binding, doc, openOptions{})
	}
	// the first document is the one that is shown
	if len(docs) > 1 {
		if first := s.inputFor(docs[0]); first != nil {
			g.model.SetActive(first)
		}
	}
	s.refreshKeys()
}

// inputFor is the input of the group that shows a document, or nil.
func (s *shellState) inputFor(doc *document.Document) editorgroup.EditorInput {
	for _, e := range s.group.model.Editors(editorgroup.Sequential, false) {
		if t, ok := e.(*editorgroup.TextInput); ok && t.Document() == doc {
			return e
		}
	}
	return nil
}

// activeEditor is the editor of the active tab, or nil when the group is empty.
func (s *shellState) activeEditor() *textEditor {
	if s.group.model == nil {
		return nil
	}
	return s.group.editors[s.group.model.ActiveEditor()]
}

// inputOptions are what the inputs of the window need to describe their files.
func (s *shellState) inputOptions() editorgroup.TextInputOpts {
	win := PeekWindow(s.Context())
	opts := editorgroup.TextInputOpts{Folder: win.Folder}
	if home, err := os.UserHomeDir(); err == nil {
		opts.Home = home
	}
	return opts
}

// showAcquired opens a document, which the caller has acquired (or retained) once, in the group and
// makes it the active editor. A document that the group shows already is activated (and kept, when
// it is not opened as a preview), and the caller's use of it is released. The new editor is a
// preview editor only when asked for and workbench.editor.enablePreview is on; a document that has
// unsaved changes is kept.
func (s *shellState) showAcquired(binding *widgets.WidgetsBinding, doc *document.Document, opts openOptions) {
	win := PeekWindow(s.Context())
	preview := opts.Preview && settings.Get[bool](win.Settings, "workbench.editor.enablePreview", "") && !doc.Dirty()
	input := editorgroup.NewTextInput(doc, s.inputOptions())
	shown, isNew := s.group.model.OpenEditor(input, editorgroup.OpenOptions{Pinned: !preview, Active: true})
	if isNew {
		s.group.editors[input] = s.newTextEditor(binding, input)
	} else {
		input.Dispose()
		win.Documents.Release(doc)
	}
	if opts.Place == nil {
		return
	}
	if e := s.group.editors[shown]; e != nil {
		opts.Place(e)
		binding.Scheduler().AddPostFrameCallback(func(time.Duration) {
			if !e.disposed {
				e.view.RevealPrimary()
			}
		})
	}
}

// show is showAcquired for code that opens a document in a callback of the UI thread: it asks the
// shell to rebuild.
func (s *shellState) show(binding *widgets.WidgetsBinding, doc *document.Document, opts openOptions) {
	s.SetState(func() {
		s.blocked = ""
		s.showAcquired(binding, doc, opts)
	})
	s.refreshKeys()
}

// groupChanged follows the model: a closed editor releases its document and its view, anything
// else rebuilds the group and updates the context keys. Typing in a preview editor keeps it.
func (s *shellState) groupChanged(binding *widgets.WidgetsBinding, e editorgroup.ChangeEvent) {
	switch e.Kind {
	case editorgroup.EditorClose:
		s.releaseEditor(e.Editor)
	case editorgroup.EditorDirty:
		if e.Editor.Dirty() && !s.group.model.IsPinned(e.Editor) {
			s.group.model.Pin(e.Editor)
		}
	case editorgroup.EditorOpen, editorgroup.EditorActive, editorgroup.EditorPin, editorgroup.EditorSticky,
		editorgroup.EditorMove, editorgroup.EditorLabel, editorgroup.EditorCapabilities, editorgroup.EditorWillDispose:
	}
	s.refreshKeys()
	if s.Mounted() {
		binding.Post(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	}
}

// releaseEditor ends the editor of a closed input: its view and background work stop, the input is
// disposed and the document is released (it closes with its last user).
func (s *shellState) releaseEditor(input editorgroup.EditorInput) {
	e := s.group.editors[input]
	delete(s.group.editors, input)
	if e != nil {
		e.dispose()
		PeekWindow(s.Context()).Documents.Release(e.doc)
	}
	input.Dispose()
}

// refreshKeys sets the context keys of the group in the window: activeEditor (the type of the active
// editor), activeEditorIsPinned (it is sticky: the pinned tab with the pin) and activeEditorIsNotPreview.
func (s *shellState) refreshKeys() {
	win := PeekWindow(s.Context())
	active := s.group.model.ActiveEditor()
	win.Keys.Batch(func() {
		if active == nil {
			win.Keys.Reset("activeEditor")
			win.Keys.Set("activeEditorIsPinned", false)
			win.Keys.Set("activeEditorIsNotPreview", false)
			return
		}
		win.Keys.Set("activeEditor", active.TypeID())
		win.Keys.Set("activeEditorIsPinned", s.group.model.IsSticky(active))
		win.Keys.Set("activeEditorIsNotPreview", s.group.model.IsPinned(active))
	})
}

// disposeGroup stops following the model and the settings, ends every editor and releases the
// documents.
func (s *shellState) disposeGroup() {
	g := &s.group
	if g.model == nil {
		return
	}
	if g.stop != nil {
		g.stop()
	}
	if g.stopSettings != nil {
		g.stopSettings()
	}
	for _, remove := range g.removeCommands {
		remove()
	}
	g.removeCommands = nil
	win := PeekWindow(s.Context())
	for input, e := range g.editors {
		e.dispose()
		win.Documents.Release(e.doc)
		input.Dispose()
	}
	g.editors = nil
	g.model.Dispose()
	win.Keys.Reset("activeEditor")
	win.Keys.Reset("activeEditorIsPinned")
	win.Keys.Reset("activeEditorIsNotPreview")
}

// confirmClose says whether an editor may close.
//
// TODO(step 7.2): ask whether to save, discard or cancel when the editor has unsaved changes; until
// then a dirty editor closes without asking and its changes are lost.
func (s *shellState) confirmClose(editorgroup.EditorInput) bool { return true }

// closeEditors closes editors of the group (those that confirm). When the active editor is among them
// and others stay, one of the others becomes active first, the most recently used (or the nearest, with
// workbench.editor.focusRecentEditorAfterClose off), so that it does not pass through the editors that
// close.
func (s *shellState) closeEditors(editors []editorgroup.EditorInput) {
	m := s.group.model
	var closing []editorgroup.EditorInput
	for _, e := range editors {
		if m.Contains(e) && s.confirmClose(e) {
			closing = append(closing, e)
		}
	}
	if active := m.ActiveEditor(); active != nil && containsInput(closing, active) {
		for _, candidate := range s.survivors(closing) {
			m.SetActive(candidate)
			break
		}
	}
	for _, e := range closing {
		m.CloseEditor(e, editorgroup.CloseUnknown, true)
	}
}

// survivors are the editors that stay when some close, the one to activate first.
func (s *shellState) survivors(closing []editorgroup.EditorInput) []editorgroup.EditorInput {
	m := s.group.model
	var out []editorgroup.EditorInput
	if m.Options().FocusRecentEditorAfterClose {
		for _, e := range m.Editors(editorgroup.MostRecentlyActive, false) {
			if !containsInput(closing, e) {
				out = append(out, e)
			}
		}
		return out
	}
	// nearest on the right of the active editor, else on its left
	editors := m.Editors(editorgroup.Sequential, false)
	at := m.IndexOf(m.ActiveEditor())
	for i := at + 1; i < len(editors); i++ {
		if !containsInput(closing, editors[i]) {
			out = append(out, editors[i])
		}
	}
	for i := at - 1; i >= 0; i-- {
		if !containsInput(closing, editors[i]) {
			out = append(out, editors[i])
		}
	}
	return out
}

func containsInput(list []editorgroup.EditorInput, e editorgroup.EditorInput) bool {
	for _, x := range list {
		if x == e {
			return true
		}
	}
	return false
}

// bindGroupCommands takes over the commands of the group in the registry while the shell is on screen.
// The registry must have them (command.DefaultRegistry does): a missing one is a mistake of the
// caller and panics with the tag EditorGroupCommandMissing.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorCommands.ts and
// src/vs/workbench/browser/parts/editor/editorActions.ts
func (s *shellState) bindGroupCommands() {
	reg := PeekWindow(s.Context()).Process.Commands
	m := s.group.model
	active := func() editorgroup.EditorInput { return m.ActiveEditor() }
	// notSticky filters the editors that the commands that close many leave alone when sticky.
	notSticky := func(editors []editorgroup.EditorInput) []editorgroup.EditorInput {
		var out []editorgroup.EditorInput
		for _, e := range editors {
			if !m.IsSticky(e) {
				out = append(out, e)
			}
		}
		return out
	}
	// move activates the editor at an offset in the order of the tabs, going round
	move := func(by int) {
		editors := m.Editors(editorgroup.Sequential, false)
		if len(editors) < 2 {
			return
		}
		at := m.IndexOf(m.ActiveEditor())
		m.SetActive(editors[((at+by)%len(editors)+len(editors))%len(editors)])
	}
	// recent activates the previously used editor (the second in the most recently used order), or
	// the one used longest ago
	recent := func(oldest bool) {
		editors := m.Editors(editorgroup.MostRecentlyActive, false)
		if len(editors) < 2 {
			return
		}
		if oldest {
			m.SetActive(editors[len(editors)-1])
		} else {
			m.SetActive(editors[1])
		}
	}
	handlers := map[string]command.Handler{
		"workbench.action.closeActiveEditor": func(command.Context, any) error {
			if e := active(); e != nil {
				s.closeEditors([]editorgroup.EditorInput{e})
			}
			return nil
		},
		"workbench.action.closeEditorsInGroup": func(command.Context, any) error {
			s.closeEditors(notSticky(m.Editors(editorgroup.Sequential, false)))
			return nil
		},
		"workbench.action.closeAllEditors": func(command.Context, any) error {
			s.closeEditors(notSticky(m.Editors(editorgroup.Sequential, false)))
			return nil
		},
		"workbench.action.closeUnmodifiedEditors": func(command.Context, any) error {
			var saved []editorgroup.EditorInput
			for _, e := range notSticky(m.Editors(editorgroup.Sequential, false)) {
				if !e.Dirty() {
					saved = append(saved, e)
				}
			}
			s.closeEditors(saved)
			return nil
		},
		"workbench.action.closeOtherEditors": func(command.Context, any) error {
			keep := active()
			if keep == nil {
				return nil
			}
			m.Pin(keep)
			var others []editorgroup.EditorInput
			for _, e := range notSticky(m.Editors(editorgroup.Sequential, false)) {
				if e != keep {
					others = append(others, e)
				}
			}
			s.closeEditors(others)
			return nil
		},
		"workbench.action.closeEditorsToTheRight": func(command.Context, any) error {
			keep := active()
			if keep == nil {
				return nil
			}
			m.Pin(keep)
			editors := m.Editors(editorgroup.Sequential, false)
			s.closeEditors(notSticky(editors[m.IndexOf(keep)+1:]))
			return nil
		},
		"workbench.action.keepEditor": func(command.Context, any) error {
			if e := active(); e != nil {
				m.Pin(e)
			}
			return nil
		},
		"workbench.action.pinEditor": func(command.Context, any) error {
			if e := active(); e != nil {
				m.Stick(e)
			}
			return nil
		},
		"workbench.action.unpinEditor": func(command.Context, any) error {
			if e := active(); e != nil {
				m.Unstick(e)
			}
			return nil
		},
		"workbench.action.nextEditor":     func(command.Context, any) error { move(1); return nil },
		"workbench.action.previousEditor": func(command.Context, any) error { move(-1); return nil },
		"workbench.action.openNextRecentlyUsedEditorInGroup": func(command.Context, any) error {
			recent(false)
			return nil
		},
		"workbench.action.openPreviousRecentlyUsedEditorInGroup": func(command.Context, any) error {
			recent(true)
			return nil
		},
	}
	removes, err := setGroupHandlers(reg, handlers)
	if err != nil {
		// the commands are registered by the program itself: a missing one is a broken build
		panic(err)
	}
	s.group.removeCommands = append(s.group.removeCommands, removes...)
}

// setGroupHandlers binds handlers to commands of a registry and returns the functions that take them
// off again. It fails with the tag EditorGroupCommandMissing for a command the registry does not know
// (the handlers bound so far are taken off).
func setGroupHandlers(reg *command.Registry, handlers map[string]command.Handler) ([]func(), error) {
	var removes []func()
	for id, h := range handlers {
		remove, err := reg.SetHandler(id, h)
		if err != nil {
			for _, r := range removes {
				r()
			}
			return nil, errors.NewErr("EditorGroupCommandMissing", err).SetMetadata("command", id)
		}
		removes = append(removes, remove)
	}
	return removes, nil
}

// tabItems are the tabs of the group, in order: the label and the state of each editor. Editors that
// share a name show their folders too.
func (s *shellState) tabItems() []kit.TabItem {
	m := s.group.model
	editors := m.Editors(editorgroup.Sequential, false)
	labels := editorgroup.TabLabels(editors)
	items := make([]kit.TabItem, len(editors))
	for i, e := range editors {
		key := labels[i].Name
		if uri, ok := e.Resource(); ok {
			key = uri.String()
		}
		items[i] = kit.TabItem{
			Key: key, Name: labels[i].Name, Description: labels[i].Description, Title: labels[i].Title,
			Preview: !m.IsPinned(e), Sticky: m.IsSticky(e), Dirty: e.Dirty(),
		}
	}
	return items
}

// tabsOptions are the options of the tab strip from the settings and the model.
func (s *shellState) tabsOptions(win *Window) kit.TabsOpts {
	m := s.group.model
	opts := kit.TabsOpts{
		Active:            m.IndexOf(m.ActiveEditor()),
		HighlightModified: settings.Get[bool](win.Settings, "workbench.editor.highlightModifiedTabs", ""),
		OnActivate:        func(i int) { s.activateAt(i) },
		OnClose:           func(i int) { s.closeAt(i) },
		OnPin:             func(i int) { m.Pin(m.EditorAt(i)) },
		OnUnpin:           func(i int) { m.Unstick(m.EditorAt(i)) },
	}
	// the close button: the old setting tabCloseButton is honored with the two that replace it
	button := settings.Get[string](win.Settings, "workbench.editor.tabCloseButton", "")
	switch {
	case button == "off" || !settings.Get[bool](win.Settings, "workbench.editor.tabActionCloseVisibility", ""):
		opts.CloseButton = kit.TabCloseOff
	case button == "left" || settings.Get[string](win.Settings, "workbench.editor.tabActionLocation", "") == "left":
		opts.CloseButton = kit.TabCloseLeft
	}
	switch settings.Get[string](win.Settings, "workbench.editor.pinnedTabSizing", "") {
	case "compact":
		opts.PinnedSizing = kit.PinnedCompact
	case "shrink":
		opts.PinnedSizing = kit.PinnedShrink
	}
	if settings.Get[string](win.Settings, "workbench.editor.showTabs", "") == "single" {
		opts.Mode = kit.TabsSingle
	}
	return opts
}

// activateAt makes the editor at an index of the tabs the active one.
func (s *shellState) activateAt(index int) {
	if e := s.group.model.EditorAt(index); e != nil {
		s.group.model.SetActive(e)
	}
}

// closeAt closes the editor at an index of the tabs.
func (s *shellState) closeAt(index int) {
	if e := s.group.model.EditorAt(index); e != nil {
		s.closeEditors([]editorgroup.EditorInput{e})
	}
}

// editorArea is what the editor area shows: the tab strip, the breadcrumbs and the editor of the
// active tab, the notice of a file that cannot be shown, or the home page.
func (s *shellState) editorArea(bc widgets.BuildContext) widgets.Widget {
	win := PeekWindow(bc)
	if active := s.activeEditor(); active != nil {
		binding := widgets.WidgetsBindingOf(bc)
		text := editor.NewEditor(active.view, editor.EditorOpts{
			Commands: win.Process.Commands, Keys: win.Keys, Scopes: win.Scopes, Autofocus: true, Context: win.ctx, ClipboardMemory: win.Clipboard, FindMemory: win.Find, Zoom: win.Zoom,
		})
		// each editor has its own widget state (scroll, focus), which the key keeps apart
		var area widgets.Widget = widgets.NewKeyedSubtree(
			// a right click opens the context menu of the editor
			kit.NewContextMenuRegion(text, s.editorMenuItems, kit.ContextMenuOpts{Controller: s.editorMenu, OnRun: s.menus.Run}),
			widgets.WidgetOpts{Key: foundation.NewObjectKey(active)})
		column := []widgets.Widget{}
		if settings.Get[string](win.Settings, "workbench.editor.showTabs", "") != "none" {
			column = append(column, kit.NewTabs(s.tabItems(), s.tabsOptions(win)))
		}
		if uri := active.doc.URI(); uri.Scheme == document.SchemeFile {
			// the path of the file above the text
			column = append(column, editor.NewBreadcrumbs(win.ctx, uri.Path, editor.BreadcrumbsOptionsFromSettings(win.Settings), editor.BreadcrumbsOpts{
				OnOpen: func(path string) { s.openPath(binding, path) },
			}))
		}
		column = append(column, widgets.NewExpanded(area))
		return widgets.NewColumn(column, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	}
	return s.emptyArea(bc)
}

// emptyArea is the editor area of a group with no editor: the notice of a file that looks binary, with
// a button to open it anyway, or the home page.
func (s *shellState) emptyArea(bc widgets.BuildContext) widgets.Widget {
	if s.blocked != "" {
		return editor.NewNotice(BinaryFileNotice, editor.NoticeOpts{Action: "Open Anyway", OnAction: func() { s.openAnyway(widgets.WidgetsBindingOf(bc), s.blocked) }})
	}
	return s.homePage(bc)
}

// openFile reads a file for an editor of the group (the document is acquired once; showAcquired takes
// that use over, or releases it).
func (s *shellState) openFile(ctx context.Context, path string) (*document.Document, error) {
	return PeekWindow(s.Context()).Documents.Acquire(ctx, path, document.NewOptions())
}
