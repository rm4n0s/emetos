package app

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/diffeditor"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The diff editor in the window (SPEC §9.11, step 9.5): the kind of editor of a DiffInput, its pane, the commands
// of the compare editor and dev.openDiff. The pane makes a text editor (a view model with its tokens, bracket pairs
// and guides) for each side and gives the two view models to the diff editor, which lines them up and colors the
// changes.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/textDiffEditor.ts,
// src/vs/workbench/browser/parts/editor/diffEditorCommands.ts and
// src/vs/workbench/browser/parts/editor/editorGroupView.ts (the context keys of the compare editor)

// diffKind is how the window handles DiffInputs. An input comes back through restoreDiff, which needs the documents
// that PrepareRestore opened, so the kind has no restore of its own.
func diffKind() editorKind {
	return editorKind{
		typeID: editorgroup.DiffInputTypeID,
		icon:   fonts.CodiconDiff,
		newPane: func(s *shellState, input editorgroup.EditorInput) pane {
			return newDiffPane(s, input.(*editorgroup.DiffInput))
		},
	}
}

// diffPane is the editor of a DiffInput: a text editor for each side and the diff editor over them. It counts a
// use of each document for as long as it lives, as the editors of a text input do.
type diffPane struct {
	paneFocus
	s     *shellState
	input *editorgroup.DiffInput
	// sides are the text editors of the original and the modified document.
	sides [2]*textEditor
	model *diffeditor.Model
	// inputs are the inputs the text editors were made over; they are in no group.
	inputs   [2]*editorgroup.TextInput
	disposed bool
}

// newDiffPane makes the pane of an input and starts the first diff.
func newDiffPane(s *shellState, input *editorgroup.DiffInput) *diffPane {
	win := PeekWindow(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	p := &diffPane{s: s, input: input}
	docs := [2]*document.Document{input.Original(), input.Modified()}
	for i, side := range []diffeditor.Side{diffeditor.Original, diffeditor.Modified} {
		win.Documents.Retain(docs[i])
		p.inputs[i] = editorgroup.NewTextInput(docs[i], s.inputOptions())
		p.sides[i] = s.newTextEditorWith(binding, p.inputs[i], func(o editor.Options) editor.Options { return diffeditor.AdjustOptions(o, side) })
	}
	p.model = diffeditor.NewModel(diffeditor.ModelOpts{
		Original: p.sides[0].view, Modified: p.sides[1].view, Settings: win.Settings,
		Post: binding.Post, Context: win.ctx,
		AfterTimer:    func(d time.Duration, fn func()) func() { return binding.Gestures().NewTimer(d, fn).Cancel },
		OnError:       func(err error) { win.Process.Logger.Warn("the diff cannot be computed", "err", err) },
		OriginalFocus: p.sides[0].focus, ModifiedFocus: p.sides[1].focus,
	})
	p.model.Start()
	return p
}

// Model is the model of the diff editor of the pane.
func (p *diffPane) Model() *diffeditor.Model { return p.model }

// build implements pane.
func (p *diffPane) build(bc widgets.BuildContext, group *editorgroup.Group, active bool) widgets.Widget {
	win := PeekWindow(bc)
	opts := func(ed *textEditor, autofocus bool) editor.EditorOpts {
		return editor.EditorOpts{
			Commands: win.Commands, Keys: win.Keys, Scopes: win.Scopes, Autofocus: autofocus, Context: win.ctx,
			ClipboardMemory: win.Clipboard, FindMemory: win.Find, Zoom: win.Zoom, Focus: ed.focus,
			OnFocus: func(has bool) { p.s.editorFocus(group, ed, has) },
		}
	}
	return p.wrap(p.s, group, diffeditor.NewWidget(p.model, diffeditor.WidgetOpts{
		Original: opts(p.sides[0], false), Modified: opts(p.sides[1], active),
	}))
}

// focus implements pane: the modified editor takes the focus, as VS Code's compare editor does.
func (p *diffPane) focus() { p.model.FocusSide(diffeditor.Modified) }

// dispose implements pane: the diff stops, the editors end and the documents are let go.
func (p *diffPane) dispose() {
	if p.disposed {
		return
	}
	p.disposed = true
	win := p.s.win
	p.model.Dispose()
	for i, ed := range p.sides {
		delete(p.s.group.focused, ed)
		ed.dispose()
		if win.Documents.Release(ed.doc) {
			p.s.autoSaveUntrack(ed.doc)
			p.s.externalUntrack(ed.doc)
		}
		p.inputs[i].Dispose()
	}
}

// diffPanes are the diff editors on screen: the one of the active group first, then those of the other groups
// whose active tab is a diff.
func (s *shellState) diffPanes() []*diffPane {
	var out []*diffPane
	add := func(g *editorgroup.Group) {
		if p, ok := s.group.panes[g.Model.ActiveEditor()].(*diffPane); ok {
			out = append(out, p)
		}
	}
	active := s.activeGroup()
	add(active)
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		if g != active {
			add(g)
		}
	}
	return out
}

// diffKeys are the values of textCompareEditorVisible (a group shows a diff editor) and textCompareEditorActive (the
// active group does).
func (s *shellState) diffKeys() (visible, active bool) {
	if s.group.grid == nil {
		return false, false
	}
	if _, ok := s.group.panes[s.activeGroup().Model.ActiveEditor()].(*diffPane); ok {
		return true, true
	}
	return len(s.diffPanes()) > 0, false
}

// diffCommands are the commands of the compare editor: they act on the diff editor of the active group, or else on
// one that another group shows (VS Code looks for a visible compare editor the same way). With none on screen
// they do nothing.
func (s *shellState) diffCommands() map[string]command.Handler {
	on := func(f func(p *diffPane)) command.Handler {
		return func(command.Context, any) error {
			if panes := s.diffPanes(); len(panes) > 0 {
				f(panes[0])
			}
			return nil
		}
	}
	change := func(next bool) command.Handler {
		return on(func(p *diffPane) {
			if p.model.GoToChange(next) {
				p.model.FocusSide(diffeditor.Modified)
			}
		})
	}
	return map[string]command.Handler{
		"workbench.action.compareEditor.nextChange":         change(true),
		"workbench.action.compareEditor.previousChange":     change(false),
		"workbench.action.compareEditor.focusPrimarySide":   on(func(p *diffPane) { p.model.FocusSide(diffeditor.Modified) }),
		"workbench.action.compareEditor.focusSecondarySide": on(func(p *diffPane) { p.model.FocusSide(diffeditor.Original) }),
		"workbench.action.compareEditor.focusOtherSide": on(func(p *diffPane) {
			side, _ := p.model.FocusedSide()
			p.model.FocusSide(side.Other())
		}),
		"diffEditor.switchSide": on(func(p *diffPane) { p.model.SwitchSide() }),
	}
}

// openDiff shows a comparison of two documents in a group (the active one by default), as an editor that is kept,
// and makes it the active editor. The caller has acquired each document once; the use is given back here, the pane
// counts its own. A comparison that the group shows already is activated.
func (s *shellState) openDiff(orig, mod *document.Document, group *editorgroup.Group) {
	win := PeekWindow(s.Context())
	if group == nil {
		group = s.activeGroup()
	}
	defer win.Documents.Release(orig)
	defer win.Documents.Release(mod)
	input := editorgroup.NewDiffInput(orig, mod, s.inputOptions())
	s.SetState(func() {
		s.nav.hold++
		_, isNew := group.Model.OpenEditor(input, editorgroup.OpenOptions{Pinned: true, Active: true})
		s.nav.hold--
		if !isNew {
			input.Dispose()
		}
		s.syncNav()
		if err := s.group.grid.Activate(group); err != nil {
			win.Process.Logger.Error("cannot activate the group", "err", err)
		}
	})
	s.refreshKeys()
}

// restoreDiff makes the input of a saved diff editor from the documents that PrepareRestore opened (openSavedEditors);
// false when one of them could not be opened. The documents it uses are marked as used.
func (s *shellState) restoreDiff(r *restoredWindow, value string, used map[string]bool) (editorgroup.EditorInput, bool) {
	orig, mod, err := editorgroup.ParseDiffInput(value)
	if err != nil {
		s.win.Process.Logger.Warn("a saved diff editor cannot be restored", "value", value, "err", describe(err))
		return nil, false
	}
	od, md := r.docs[orig.String()], r.docs[mod.String()]
	if od == nil || md == nil {
		return nil, false
	}
	used[orig.String()], used[mod.String()] = true, true
	return editorgroup.NewDiffInput(od, md, s.inputOptions()), true
}

// diffArguments are the two sides of a diff that dev.openDiff names.
type diffArguments struct {
	// Original and Modified are what each side shows: an absolute path, a path relative to the folder of the window,
	// a file:// URI, an emetos-virtual: URI or an untitled: URI of a document that is open.
	Original, Modified string
}

// parseOpenDiff reads the arguments of dev.openDiff: {"original": ..., "modified": ...}.
func parseOpenDiff(args any) (diffArguments, error) {
	m, ok := args.(map[string]any)
	if !ok {
		return diffArguments{}, errors.New("DevOpenDiffInvalid", `dev.openDiff needs {"original": ..., "modified": ...}`, "arguments", args)
	}
	var a diffArguments
	a.Original, _ = m["original"].(string)
	a.Modified, _ = m["modified"].(string)
	if a.Original == "" || a.Modified == "" {
		return diffArguments{}, errors.New("DevOpenDiffInvalid", `dev.openDiff needs the paths or URIs "original" and "modified"`, "arguments", args)
	}
	return a, nil
}

// bindDevOpenDiff gives dev.openDiff (SPEC §15.2) its handler.
func (s *shellState) bindDevOpenDiff() {
	if !DevCommandsEnabled() {
		return
	}
	win := PeekWindow(s.Context())
	remove, err := win.Commands.SetHandler("dev.openDiff", func(c command.Context, args any) error {
		return s.devOpenDiff(c.Ctx, args)
	})
	if err != nil {
		win.Process.Logger.Error("cannot bind dev.openDiff", "err", err)
		return
	}
	s.removeOpenDiff = remove
}

// devOpenDiff opens a comparison of two files, or of a file and generated content, in the active group.
func (s *shellState) devOpenDiff(ctx context.Context, args any) error {
	a, err := parseOpenDiff(args)
	if err != nil {
		return err
	}
	orig, err := s.acquireSide(ctx, a.Original)
	if err != nil {
		return err
	}
	mod, err := s.acquireSide(ctx, a.Modified)
	if err != nil {
		PeekWindow(s.Context()).Documents.Release(orig)
		return err
	}
	s.openDiff(orig, mod, nil)
	return nil
}

// acquireSide opens the document of a side of a diff and counts a use of it: a virtual document is generated by its
// provider, an untitled one must be open, a file is read (a relative path is relative to the folder of the window,
// and fails when no folder is open).
func (s *shellState) acquireSide(ctx context.Context, spec string) (*document.Document, error) {
	win := PeekWindow(s.Context())
	switch {
	case strings.HasPrefix(spec, document.SchemeVirtual+":"):
		uri, err := document.ParseURI(spec)
		if err != nil {
			return nil, err
		}
		return win.Documents.AcquireVirtual(ctx, uri)
	case strings.HasPrefix(spec, document.SchemeUntitled+":"):
		uri, err := document.ParseURI(spec)
		if err != nil {
			return nil, err
		}
		doc, ok := win.Documents.Get(uri)
		if !ok {
			return nil, errors.New("DevOpenDiffSideMissing", "an untitled document that dev.openDiff names is not open", "uri", spec)
		}
		win.Documents.Retain(doc)
		return doc, nil
	}
	path := strings.TrimPrefix(spec, "file://")
	if !filepath.IsAbs(path) {
		if win.Folder == "" {
			return nil, errors.New("DevOpenDiffInvalid", "a relative path of dev.openDiff needs a folder open in the window", "path", spec)
		}
		path = filepath.Join(win.Folder, path)
	}
	return win.Documents.Acquire(ctx, filepath.Clean(path), document.NewOptions())
}
