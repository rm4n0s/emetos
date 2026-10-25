package app

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/glob"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// File views (SPEC §10.15 "File view", step 11.6): the micro-tabs at the right end of the breadcrumbs row of a file's editor
// tab, Text first and then the views of the extensions that select the file; the choice belongs to the editor tab and is kept
// in its saved state; workbench.editor.defaultFileViews chooses the view a file opens in; workbench.action.cycleFileView
// goes through them. The view is the extension's widget in a box, over the document of the tab, which it reads and edits
// through a docHandle.

// fileViewsFor are the file views of the extensions that select the file of an editor, in the order of the micro-tabs.
func (s *shellState) fileViewsFor(e *textEditor) []exthost.FileViewInfo {
	scope := PeekWorkspace(s.Context()).Extensions()
	if scope == nil {
		return nil
	}
	return scope.FileViewsFor(e.doc.URI().Path, e.doc.LanguageID(), PeekWorkspace(s.Context()).Folder)
}

// currentFileView is the id of the file view an editor shows, "" for Text: the choice, when the view is still one of the
// views of the file (its extension is on, has not failed, and still selects the file).
func (s *shellState) currentFileView(e *textEditor, views []exthost.FileViewInfo) string {
	if e.fileView == "" {
		return ""
	}
	for _, v := range views {
		if v.ID == e.fileView {
			return e.fileView
		}
	}
	return ""
}

// setFileView makes an editor show a file view ("" is Text).
func (s *shellState) setFileView(e *textEditor, id string) {
	if e.fileView == id {
		return
	}
	s.SetState(func() { e.fileView = id })
	s.stateChangedIfMounted()
	if id == "" {
		s.askFocus(widgets.WidgetsBindingOf(s.Context()))
	}
}

// cycleFileView shows the next file view of the active editor: Text, the views, Text again.
func (s *shellState) cycleFileView() {
	e := s.activeEditor()
	if e == nil {
		return
	}
	views := s.fileViewsFor(e)
	if len(views) == 0 {
		return
	}
	cur := s.currentFileView(e, views)
	next := views[0].ID
	if cur != "" {
		next = ""
		for i, v := range views {
			if v.ID == cur && i+1 < len(views) {
				next = views[i+1].ID
			}
		}
	}
	s.setFileView(e, next)
}

// fileViewCommands is the handler of workbench.action.cycleFileView.
func (s *shellState) fileViewCommands() map[string]command.Handler {
	return map[string]command.Handler{
		"workbench.action.cycleFileView": func(command.Context, any) error {
			s.cycleFileView()
			return nil
		},
	}
}

// applyDefaultFileView gives a new editor the view workbench.editor.defaultFileViews names for its file: the first
// pattern (in the order of the keys) that matches the name or the path relative to the folder and names a view the file
// has. Nothing else changes the view of an editor that is open.
func (s *shellState) applyDefaultFileView(e *textEditor) {
	win := PeekWorkspace(s.Context())
	m := settings.Get[map[string]any](win.Settings, "workbench.editor.defaultFileViews", "")
	if len(m) == 0 {
		return
	}
	views := s.fileViewsFor(e)
	if len(views) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	path := filepath.ToSlash(e.doc.URI().Path)
	base := filepath.Base(path)
	rel := ""
	if win.Folder != "" {
		if r, ok := strings.CutPrefix(path, filepath.ToSlash(win.Folder)+"/"); ok {
			rel = r
		}
	}
	for _, k := range keys {
		id, ok := m[k].(string)
		if !ok || !slices.ContainsFunc(views, func(v exthost.FileViewInfo) bool { return v.ID == id }) {
			continue
		}
		g := glob.Parse(k, glob.Options{})
		if g.Match(base) || (rel != "" && g.Match(rel)) {
			e.fileView = id
			return
		}
	}
}

// ---- the state of an editor: its view state and its file view ----

// editorState is the State of a text input in the saved state of the window: its view state, and the file view when it is
// not Text. A state with no file view is the view state alone, as it was before file views.
type editorState struct {
	ViewState json.RawMessage `json:"viewState,omitempty"`
	FileView  string          `json:"fileView,omitempty"`
}

// encodeEditorState writes the state of an editor.
func encodeEditorState(viewState json.RawMessage, fileView string) json.RawMessage {
	if fileView == "" {
		return viewState
	}
	data, err := json.Marshal(editorState{ViewState: viewState, FileView: fileView})
	if err != nil {
		return viewState
	}
	return data
}

// decodeEditorState reads the state of an editor; a state that is the view state alone has no file view.
func decodeEditorState(raw json.RawMessage) (viewState json.RawMessage, fileView string) {
	var st editorState
	if err := json.Unmarshal(raw, &st); err == nil && st.FileView != "" {
		return st.ViewState, st.FileView
	}
	return raw, ""
}

// ---- the micro-tabs ----

// fileViewBar is the row at the right end of the breadcrumbs: Text and the views of the file as micro-tabs.
func (s *shellState) fileViewBar(e *textEditor, views []exthost.FileViewInfo, current string) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		colors := kit.ThemeOf(bc)
		item := func(id, name string, icon int) widgets.Widget {
			active := id == current
			fg := colors.TabInactiveForeground
			if active {
				fg = colors.TabActiveForeground
			}
			row := []widgets.Widget{}
			if icon != 0 {
				row = append(row, widgets.NewIcon(fonts.CodiconIcon(icon), widgets.IconOpts{Size: foundation.Some(14.0), Color: foundation.Some(fg)}),
					widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}))
			}
			row = append(row, widgets.NewText(name, widgets.TextOpts{Style: kit.TextStyleOf(bc, 12, fg)}))
			var border painting.Border
			if active {
				border = painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.TabActiveBorderTop), Width: foundation.Some(1.0)})}
			}
			body := widgets.NewContainer(
				widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(8, 0, 8, 0), widgets.NewCenter(widgets.NewRow(row, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin}))),
				widgets.ContainerOpts{Height: foundation.Some(editor.BreadcrumbsHeight), Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Border: border})})
			tab := widgets.NewListener(body, widgets.ListenerOpts{
				Behavior:      rendering.HitTestBehaviorOpaque,
				OnPointerDown: func(*gestures.PointerDownEvent) { s.setFileView(e, id) },
			})
			return kit.NewHover(tab, kit.HoverOpts{Text: "Show " + name, Compact: true})
		}
		tabs := []widgets.Widget{item("", "Text", 0)}
		for _, v := range views {
			tabs = append(tabs, item(v.ID, v.Name, v.Icon))
		}
		return widgets.NewContainer(widgets.NewRow(tabs, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin}),
			widgets.ContainerOpts{Height: foundation.Some(editor.BreadcrumbsHeight), Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(colors.BreadcrumbBackground)})})
	})
}

// ---- the document of a file view ----

var _ extension.DocumentHandle = (*docHandle)(nil)

// docHandle is the extension.DocumentHandle of the document of an editor tab. It runs on the UI thread.
type docHandle struct {
	s       *shellState
	e       *textEditor
	cancels []func()
}

// handle is the document handle of an editor, made on first use.
func (e *textEditor) handle(s *shellState) *docHandle {
	if e.docHandle == nil {
		e.docHandle = &docHandle{s: s, e: e}
	}
	return e.docHandle
}

// release ends the subscriptions of the handle.
func (h *docHandle) release() {
	cancels := h.cancels
	h.cancels = nil
	for _, c := range cancels {
		c()
	}
}

// URI implements extension.DocumentHandle.
func (h *docHandle) URI() string { return h.e.doc.URI().String() }

// LanguageID implements extension.DocumentHandle.
func (h *docHandle) LanguageID() string { return h.e.doc.LanguageID() }

// Version implements extension.DocumentHandle.
func (h *docHandle) Version() int { return h.e.doc.Version() }

// Text implements extension.DocumentHandle.
func (h *docHandle) Text() string { return h.e.doc.Text() }

// ReadOnly implements extension.DocumentHandle.
func (h *docHandle) ReadOnly() bool { return h.e.doc.ReadOnly() }

// Folder implements extension.DocumentHandle.
func (h *docHandle) Folder() string { return PeekWorkspace(h.s.Context()).Folder }

// Edit implements extension.DocumentHandle: one batch of the document, one undo step, with the cursors of the editor as the
// selections an undo goes back to.
func (h *docHandle) Edit(edits []extension.TextEdit) error {
	batch := make([]document.Edit, len(edits))
	for i, ed := range edits {
		batch[i] = document.Edit{
			Range: buffer.Range{Start: buffer.Position{Line: ed.StartLine, Col: ed.StartColumn}, End: buffer.Position{Line: ed.EndLine, Col: ed.EndColumn}},
			Text:  ed.Text,
		}
	}
	h.e.doc.PushStackElement()
	_, err := h.e.doc.Edit(batch, document.EditOptions{Before: h.e.view.Cursors().Selections()})
	h.e.doc.PushStackElement()
	return err
}

// causeName is the name of the cause of a change, as emetos.document.changed spells it.
func causeName(c document.Cause) string {
	switch c {
	case document.CauseUndo:
		return "undo"
	case document.CauseRedo:
		return "redo"
	case document.CauseLineEnding:
		return "lineEnding"
	case document.CauseSetText:
		return "setText"
	}
	return "edit"
}

// OnChange implements extension.DocumentHandle.
func (h *docHandle) OnChange(fn func(extension.ViewDocumentChange)) (cancel func()) {
	stop := h.e.doc.Subscribe(func(ev document.ChangeEvent) {
		fn(extension.ViewDocumentChange{Version: ev.Version, Cause: causeName(ev.Cause)})
	})
	h.cancels = append(h.cancels, stop)
	return stop
}

// topLine is the first line an editor shows at the top.
func topLine(e *textEditor) int {
	p := e.view.Projection()
	line, _, _ := p.RowSpan(p.RowAt(e.view.ScrollTop()))
	return line
}

// textEditors are the editors of the document that show its text, the ones of the most recently active groups first.
func (s *shellState) textEditors(doc *document.Document) []*textEditor {
	var out []*textEditor
	for _, g := range s.group.grid.Groups(editorgroup.MostRecentlyActiveGroups) {
		if e := s.group.editors[g.Model.ActiveEditor()]; e != nil && e.doc == doc && e.fileView == "" {
			out = append(out, e)
		}
	}
	return out
}

// TopLine implements extension.DocumentHandle: the top line of the text editor of the document that was active last, or of
// the editor of the view itself when no editor shows the text.
func (h *docHandle) TopLine() int {
	if eds := h.s.textEditors(h.e.doc); len(eds) > 0 {
		return topLine(eds[0])
	}
	return topLine(h.e)
}

// OnScroll implements extension.DocumentHandle: fn is called, later, on the UI thread, when a text editor of the document
// scrolls to another top line.
func (h *docHandle) OnScroll(fn func(topLine int)) (cancel func()) {
	binding := widgets.WidgetsBindingOf(h.s.Context())
	last := -1
	var stops []func()
	for _, e := range h.s.group.editors {
		if e.doc != h.e.doc {
			continue
		}
		e := e
		handle := e.view.AddScrollListener(func() {
			if e.fileView != "" {
				return
			}
			line := topLine(e)
			if line == last {
				return
			}
			last = line
			binding.Post(func() { fn(line) })
		})
		stops = append(stops, func() { e.view.RemoveScrollListener(handle) })
	}
	stop := func() {
		for _, s := range stops {
			s()
		}
	}
	h.cancels = append(h.cancels, stop)
	return stop
}

// OpenFile implements extension.DocumentHandle.
func (h *docHandle) OpenFile(path string) error {
	if !filepath.IsAbs(path) {
		folder := h.Folder()
		if folder == "" {
			return errors.New("ExtFileViewNoFolder", "a relative path needs the folder of the workspace", "path", path)
		}
		path = filepath.Join(folder, path)
	}
	s := h.s
	s.openPath(widgets.WidgetsBindingOf(s.Context()), s.activeGroup(), filepath.Clean(path), false)
	return nil
}
