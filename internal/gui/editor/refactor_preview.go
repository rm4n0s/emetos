package editor

import (
	"context"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// The preview of a refactoring (SPEC §9.12 "Rename", "As built (step 12.8)"): the changes of a rename or of a code action, file
// by file, each with a check box; Apply makes only the checked ones, Discard none. VS Code shows it in a view at the bottom of the
// window with a diff for the selected change; here it is a list over the editor.
//
// Ported from VS Code: src/vs/workbench/contrib/bulkEdit/browser/preview/bulkEditPane.ts and bulkEditTree.ts

// PreviewChange is one change of a file in the preview.
type PreviewChange struct {
	// Line is the line of the change, counted from 1.
	Line int
	// Before is the text of the line before the change (cut at its start), Old the text that is replaced, New the text that
	// replaces it, and After the rest of the line (cut at its end).
	Before, Old, New, After string
	// Checked says that Apply makes the change.
	Checked bool
}

// PreviewFile is a file with the changes made to it, or a file operation.
type PreviewFile struct {
	// Name is the name of the file and Dir the folder it is in, as the window writes it.
	Name, Dir string
	// Operation is "create", "rename" or "delete" for a file operation, and empty for changes to the text.
	Operation string
	// Target is the new name of a renamed file.
	Target string
	// Changes are the changes to the text, and Checked says that the file operation is made.
	Changes []PreviewChange
	Checked bool

	change int
}

// PreviewRow is a row of the preview: a file (Change is -1), or a change of a file.
type PreviewRow struct {
	File, Change int
}

// RefactorPreview is the preview that shows.
type RefactorPreview struct {
	// Label says what is changed ("Renaming 'a' to 'b'").
	Label string
	Files []*PreviewFile
	// Focus is the index of the row with the keyboard focus.
	Focus int

	edit *langfeatures.WorkspaceEdit
	done func(error)
}

// Rows are the rows of the list: each file followed by its changes.
func (p *RefactorPreview) Rows() []PreviewRow {
	var rows []PreviewRow
	for i, f := range p.Files {
		rows = append(rows, PreviewRow{File: i, Change: -1})
		for k := range f.Changes {
			rows = append(rows, PreviewRow{File: i, Change: k})
		}
	}
	return rows
}

// Checked tells whether a row is checked and, for a file, whether only some of its changes are.
func (p *RefactorPreview) Checked(r PreviewRow) (checked, partial bool) {
	f := p.Files[r.File]
	if r.Change >= 0 {
		return f.Changes[r.Change].Checked, false
	}
	if len(f.Changes) == 0 {
		return f.Checked, false
	}
	n := 0
	for _, c := range f.Changes {
		if c.Checked {
			n++
		}
	}
	return n > 0, n > 0 && n < len(f.Changes)
}

// PreviewUI is what the widget gives the view model to move the focus into the preview and back.
type PreviewUI struct {
	Focus, Return func()
}

// previewState is what a view model keeps for the preview.
type previewState struct {
	shown   *RefactorPreview
	gen     int
	cancel  context.CancelFunc
	changed *foundation.ChangeNotifier
	ui      PreviewUI
}

// SetPreviewUI gives the view model the widget's means to move the focus.
func (vm *ViewModel) SetPreviewUI(ui PreviewUI) { vm.pv.ui = ui }

func newPreviewState() previewState { return previewState{changed: foundation.NewChangeNotifier()} }

// AddPreviewListener calls fn when the preview shows, changes or goes.
func (vm *ViewModel) AddPreviewListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return vm.pv.changed.AddListener(fn)
}

// RemovePreviewListener removes a listener of AddPreviewListener.
func (vm *ViewModel) RemovePreviewListener(h foundation.ListenerHandle) {
	vm.pv.changed.RemoveListener(h)
}

// RefactorPreview is the preview that shows now, or nil.
func (vm *ViewModel) RefactorPreview() *RefactorPreview { return vm.pv.shown }

func (vm *ViewModel) previewNotify() {
	vm.pv.changed.NotifyListeners()
	vm.notifyKeys()
}

// cachedTexts remembers the text of the documents that are asked for once.
func cachedTexts(texts langfeatures.TextSource) langfeatures.TextSource {
	if texts == nil {
		return nil
	}
	cache := map[document.URI]lsp.Lines{}
	return func(ctx context.Context, uri document.URI) (lsp.Lines, error) {
		if l, ok := cache[uri]; ok {
			return l, nil
		}
		l, err := texts(ctx, uri)
		if err == nil {
			cache[uri] = l
		}
		return l, err
	}
}

// PreviewEdit shows the changes of an edit before they are made. The edit is expanded off the UI thread, with the text of the
// documents it touches; done, when set, is called with nil once the checked changes were made, and with the error when they
// could not be, or with context.Canceled when the user discarded the preview.
func (vm *ViewModel) PreviewEdit(label string, we *langfeatures.WorkspaceEdit, done func(error)) {
	h := vm.sg.host
	if h == nil || we == nil {
		return
	}
	vm.PreviewDiscard()
	st := &vm.pv
	st.gen++
	gen := st.gen
	ctx, cancel := context.WithCancel(vm.hostCtx())
	st.cancel = cancel
	texts := cachedTexts(h.Texts)
	go func() {
		exp, err := langfeatures.ExpandEdit(ctx, we, texts)
		var files []*PreviewFile
		if err == nil {
			files = previewFiles(ctx, exp, texts)
		}
		vm.post(func() {
			if gen != st.gen {
				return
			}
			st.cancel()
			st.cancel = nil
			if err != nil {
				vm.reportError(err)
				if done != nil {
					done(err)
				}
				return
			}
			if len(files) == 0 {
				return
			}
			exp.Label = label
			st.shown = &RefactorPreview{Label: label, Files: files, edit: exp, done: done}
			vm.CodeActionHide()
			vm.previewNotify()
			if st.ui.Focus != nil {
				st.ui.Focus()
			}
		})
	}()
}

// previewFiles lists the changes of an expanded edit, with the text around each.
func previewFiles(ctx context.Context, exp *langfeatures.WorkspaceEdit, texts langfeatures.TextSource) []*PreviewFile {
	var files []*PreviewFile
	byURI := map[document.URI]*PreviewFile{}
	for i, c := range exp.Changes {
		name, dir := splitPath(c.URI)
		switch c.Kind {
		case langfeatures.ChangeCreate:
			files = append(files, &PreviewFile{Name: name, Dir: dir, Operation: "create", Checked: true, change: i})
		case langfeatures.ChangeDelete:
			files = append(files, &PreviewFile{Name: name, Dir: dir, Operation: "delete", Checked: true, change: i})
		case langfeatures.ChangeRename:
			target, _ := splitPath(c.NewURI)
			files = append(files, &PreviewFile{Name: name, Dir: dir, Operation: "rename", Target: target, Checked: true, change: i})
		default:
			f := byURI[c.URI]
			if f == nil || f.change != i {
				f = &PreviewFile{Name: name, Dir: dir, change: i}
				byURI[c.URI] = f
				files = append(files, f)
			}
			var lines lsp.Lines
			if texts != nil {
				lines, _ = texts(ctx, c.URI)
			}
			for _, e := range c.Edits {
				f.Changes = append(f.Changes, previewChange(lines, e))
			}
		}
	}
	return files
}

// splitPath is the name of a file and its folder.
func splitPath(uri document.URI) (name, dir string) {
	if uri.Scheme != document.SchemeFile {
		return uri.Title(), ""
	}
	return filepath.Base(uri.Path), filepath.Dir(uri.Path)
}

// previewContext is the number of characters of the line shown before and after a change.
const previewContext = 24

// previewChange describes an edit by the line it is on.
func previewChange(lines lsp.Lines, e langfeatures.TextEdit) PreviewChange {
	c := previewChangeOf(lines, e)
	// a tab has no glyph in the font of the list: it shows as the spaces of a tab stop
	c.Before, c.Old, c.New, c.After = spaced(c.Before), spaced(c.Old), spaced(c.New), spaced(c.After)
	return c
}

// spaced writes the tabs of a text as four spaces.
func spaced(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// previewChangeOf is previewChange with the text as it is in the document.
func previewChangeOf(lines lsp.Lines, e langfeatures.TextEdit) PreviewChange {
	c := PreviewChange{Line: e.Range.Start.Line + 1, New: firstLine(e.Text), Checked: true}
	if lines == nil || e.Range.Start.Line >= lines.LineCount() {
		return c
	}
	start, _ := lines.LineContent(e.Range.Start.Line)
	col := min(e.Range.Start.Col, len(start))
	c.Before = tailOf(start[:col], previewContext)
	if e.Range.End.Line == e.Range.Start.Line {
		end := min(e.Range.End.Col, len(start))
		c.Old = start[col:max(end, col)]
		c.After = headOf(start[max(end, col):], previewContext)
		return c
	}
	c.Old = start[col:] + "…"
	if e.Range.End.Line < lines.LineCount() {
		endLine, _ := lines.LineContent(e.Range.End.Line)
		c.After = headOf(endLine[min(e.Range.End.Col, len(endLine)):], previewContext)
	}
	return c
}

// firstLine is the text up to its first line break, with an ellipsis when there is more.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i] + "…"
	}
	return s
}

// tailOf is the last n characters of s, after an ellipsis when it is longer.
func tailOf(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return "…" + string(r[len(r)-n:])
}

// headOf is the first n characters of s, before an ellipsis when it is longer.
func headOf(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// PreviewMove moves the focus to the next (1) or previous (-1) row.
func (vm *ViewModel) PreviewMove(dir int) {
	p := vm.pv.shown
	if p == nil {
		return
	}
	n := len(p.Rows())
	p.Focus = ((p.Focus+dir)%n + n) % n
	vm.previewNotify()
}

// PreviewFocus focuses a row, as a click does.
func (vm *ViewModel) PreviewFocus(i int) {
	if p := vm.pv.shown; p != nil && i >= 0 && i < len(p.Rows()) && p.Focus != i {
		p.Focus = i
		vm.previewNotify()
	}
}

// PreviewToggle checks or unchecks a row: a change, or all the changes of a file, which are checked unless all are.
func (vm *ViewModel) PreviewToggle(i int) {
	p := vm.pv.shown
	if p == nil {
		return
	}
	rows := p.Rows()
	if i < 0 || i >= len(rows) {
		return
	}
	r := rows[i]
	f := p.Files[r.File]
	switch {
	case r.Change >= 0:
		f.Changes[r.Change].Checked = !f.Changes[r.Change].Checked
	case len(f.Changes) == 0:
		f.Checked = !f.Checked
	default:
		all, partial := p.Checked(r)
		want := !all || partial
		for k := range f.Changes {
			f.Changes[k].Checked = want
		}
	}
	vm.previewNotify()
}

// PreviewToggleFocused toggles the focused row.
func (vm *ViewModel) PreviewToggleFocused() {
	if p := vm.pv.shown; p != nil {
		vm.PreviewToggle(p.Focus)
	}
}

// PreviewDiscard closes the preview and makes nothing.
func (vm *ViewModel) PreviewDiscard() {
	st := &vm.pv
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	st.gen++
	p := st.shown
	if p == nil {
		return
	}
	st.shown = nil
	vm.previewNotify()
	if st.ui.Return != nil {
		st.ui.Return()
	}
	if p.done != nil {
		p.done(context.Canceled)
	}
}

// PreviewApply makes the checked changes and closes the preview. The edit fails as a whole when a document changed since the
// preview was made (the version each change names), and the user is told.
func (vm *ViewModel) PreviewApply() {
	st := &vm.pv
	p := st.shown
	h := vm.sg.host
	if p == nil {
		return
	}
	st.gen++
	st.shown = nil
	vm.previewNotify()
	if st.ui.Return != nil {
		st.ui.Return()
	}
	byChange := map[int]*PreviewFile{}
	for _, f := range p.Files {
		byChange[f.change] = f
	}
	keep := func(change, edit int) bool {
		f := byChange[change]
		switch {
		case f == nil:
			return false
		case f.Operation != "":
			return f.Checked
		case edit < 0:
			return true
		}
		return edit < len(f.Changes) && f.Changes[edit].Checked
	}
	filtered := langfeatures.FilterEdit(p.edit, keep)
	var err error
	if len(filtered.Changes) > 0 {
		if h == nil || h.ApplyEdit == nil {
			err = context.Canceled // the editor has no means to make the changes
		} else if err = h.ApplyEdit(vm.hostCtx(), filtered); err != nil {
			vm.reportError(err)
		}
	}
	if p.done != nil {
		p.done(err)
	}
}

// previewActions are the commands of the preview.
//
// Ported from VS Code: bulkEdit.contribution.ts (refactorPreview.apply, discard and toggleCheckedState)
func previewActions(a map[string]action) {
	only := func(f func(vm *ViewModel)) action {
		return func(vm *ViewModel, _ any) error {
			vm.keepSessions, vm.skipReveal = true, true
			f(vm)
			return nil
		}
	}
	a["refactorPreview.apply"] = only(func(vm *ViewModel) { vm.PreviewApply() })
	a["refactorPreview.discard"] = only(func(vm *ViewModel) { vm.PreviewDiscard() })
	a["refactorPreview.toggleCheckedState"] = only(func(vm *ViewModel) { vm.PreviewToggleFocused() })
	a["refactorPreview.focusNext"] = only(func(vm *ViewModel) { vm.PreviewMove(1) })
	a["refactorPreview.focusPrevious"] = only(func(vm *ViewModel) { vm.PreviewMove(-1) })
}
