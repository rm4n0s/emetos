package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/mergeeditor"
)

// The merge editor in the window (SPEC §9.11, step 9.7): the kind of editor of a MergeInput, its pane, the commands
// of the merge editor, the notification that offers it for a file with conflict markers, and the ways to open it
// (that notification, git.openMergeEditor, _open.mergeEditor and `emetos --merge`). The pane makes a text editor for
// each of the three views and gives the view models to the merge editor, which lines the inputs up, outlines the
// conflicts and keeps the three scrolled together.
//
// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/view/mergeEditor.ts,
// src/vs/workbench/contrib/mergeEditor/browser/commands/commands.ts, extensions/git/src/commands.ts (openMergeEditor)
// and src/vs/workbench/contrib/mergeEditor/browser/mergeEditorSerializer.ts

const (
	// MergeConflictsNotice is what the notification says about a file that has conflict markers.
	MergeConflictsNotice = "This file has merge conflicts."
	// MergeConflictsButton is the button of that notification (the title of VS Code's git.openMergeEditor).
	MergeConflictsButton = "Resolve in Merge Editor"
	// MergeNoConflictsNotice is what git.openMergeEditor says about a file without conflict markers.
	MergeNoConflictsNotice = "This file has no merge conflicts."
	// MergeNotCompletedNotice is the message of `--merge --wait` when the merge editor was closed. (Step 9.8 makes
	// Complete Merge end it with exit code 0.)
	MergeNotCompletedNotice = "The merge was not completed."
	// mergeNoticeLimit is the largest text that is looked at for conflict markers when a file opens.
	mergeNoticeLimit = 2 << 20
	// mergeDiffLimit is how long the diffs of a merge may take; past it the ranges are coarse (a whole block).
	mergeDiffLimit = 5 * time.Second
)

// mergeKind is how the window handles MergeInputs. An input comes back through restoreMerge, which needs the
// documents that PrepareRestore opened, so the kind has no restore of its own.
func mergeKind() editorKind {
	return editorKind{
		typeID: editorgroup.MergeInputTypeID,
		icon:   fonts.CodiconGitMerge,
		newPane: func(s *shellState, input editorgroup.EditorInput) pane {
			return newMergePane(s, input.(*editorgroup.MergeInput))
		},
	}
}

// mergePrepared is a merge that is ready to be shown: its texts and its model, which the goroutine that read the
// files made, and whether the result document is replaced by the model's result (a merge that is started) or kept as it is
// (one that was restored with its unsaved changes).
type mergePrepared struct {
	inputs  diff.MergeInputs
	model   *diff.MergeModel
	initial mergeeditor.Initial
}

// mergeState is what the shell keeps of the merge editors.
type mergeState struct {
	// prepared are the merges that are about to be shown, by the input that will show them.
	prepared map[*editorgroup.MergeInput]*mergePrepared
	// number gives the virtual documents of the inputs their URIs.
	number int
}

// ---- preparing a merge ----

// markersMerge makes the merge of a text with conflict markers: the inputs and the base come from the markers (SPEC §9.11
// (a)). It runs on any goroutine. The error is MergeMarkersNotFound for a text without a complete conflict.
func markersMerge(ctx context.Context, text string) (*mergePrepared, error) {
	inputs, err := diff.MergeInputsFromMarkerText(text)
	if err != nil {
		return nil, err
	}
	model, err := diff.NewMergeModel(ctx, inputs, diff.MergeOptions{MaxComputationTime: mergeDiffLimit})
	if err != nil {
		return nil, err
	}
	return &mergePrepared{inputs: inputs, model: model}, nil
}

// filesMerge makes the merge of three files (SPEC §9.11 (b)): the base, input 1 (incoming) and input 2 (current), the
// headers of the inputs labelled by their file names unless labels are given. It runs on any goroutine. The errors are
// those of reading a file.
func filesMerge(ctx context.Context, base, in1, in2, label1, label2 string) (*mergePrepared, error) {
	var texts [3][]string
	for i, path := range []string{base, in1, in2} {
		res, err := fileio.Read(ctx, path, fileio.ReadOptions{})
		if err != nil {
			return nil, err
		}
		texts[i], _ = diff.SplitLines(res.Text)
	}
	inputs := diff.MergeInputsFromFiles(texts[0], texts[1], texts[2])
	inputs.Input1Label, inputs.Input2Label = label1, label2
	if inputs.Input1Label == "" {
		inputs.Input1Label = filepath.Base(in1)
	}
	if inputs.Input2Label == "" {
		inputs.Input2Label = filepath.Base(in2)
	}
	inputs.BaseLabel = filepath.Base(base)
	model, err := diff.NewMergeModel(ctx, inputs, diff.MergeOptions{MaxComputationTime: mergeDiffLimit})
	if err != nil {
		return nil, err
	}
	return &mergePrepared{inputs: inputs, model: model}, nil
}

// prepareSaved makes the merge of a saved merge editor again, for the document that PrepareRestore opened: a marker file is
// read from the disk (the document may have its unsaved result), the files of --merge are read. The merge keeps the
// text of the document.
func prepareSaved(ctx context.Context, source editorgroup.MergeSource) (*mergePrepared, error) {
	var prep *mergePrepared
	var err error
	switch source.Kind {
	case editorgroup.MergeFiles:
		prep, err = filesMerge(ctx, source.Base, source.Input1, source.Input2, "", "")
	default:
		var res fileio.ReadResult
		if res, err = fileio.Read(ctx, source.Result, fileio.ReadOptions{}); err == nil {
			prep, err = markersMerge(ctx, res.Text)
		}
	}
	if err != nil {
		return nil, err
	}
	prep.initial = mergeeditor.InitialSync
	return prep, nil
}

// ---- the pane ----

// mergePane is the editor of a MergeInput: a text editor for each of the incoming side, the current side and the
// result, and the merge editor over them. It counts a use of the result document for as long as it lives.
type mergePane struct {
	paneFocus
	s     *shellState
	input *editorgroup.MergeInput
	// sides are the text editors of the incoming side, the current side and the result.
	sides   [3]*textEditor
	inputs  [3]*editorgroup.TextInput
	session *mergeeditor.Session
	model   *mergeeditor.Model
	// failed is why the pane has no merge editor; the pane then says so.
	failed   error
	disposed bool
}

// newMergePane makes the pane of an input from the merge that was prepared for it (or, for an input that was not, from the
// files again, keeping the text of the result). A merge that cannot be shown leaves a pane that says why.
func newMergePane(s *shellState, input *editorgroup.MergeInput) pane {
	win := PeekWindow(s.Context())
	prep := s.merge.prepared[input]
	delete(s.merge.prepared, input)
	if prep == nil {
		var err error
		if prep, err = prepareSaved(win.ctx, input.Source()); err != nil {
			win.Process.Logger.Warn("the merge editor cannot show its merge", "err", err)
			return &mergePane{s: s, input: input, failed: err}
		}
	}
	return buildMergePane(s, input, prep)
}

// buildMergeSession makes the session of a merge over the result document of the input: the document is replaced by the
// model's result when the merge is a new one.
func buildMergeSession(s *shellState, input *editorgroup.MergeInput, prep *mergePrepared) (*mergeeditor.Session, error) {
	win := PeekWindow(s.Context())
	return mergeeditor.NewSession(win.ctx, mergeeditor.SessionOpts{
		Result: input.Result(), Inputs: prep.inputs, Model: prep.model, Initial: prep.initial,
		OnError: func(err error) { win.Process.Logger.Warn("the merge editor lost track of the result", "err", err) },
	})
}

// buildMergePane makes the editors and the model of a merge.
func buildMergePane(s *shellState, input *editorgroup.MergeInput, prep *mergePrepared) pane {
	win := PeekWindow(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	p := &mergePane{s: s, input: input}
	session, err := buildMergeSession(s, input, prep)
	if err != nil {
		win.Process.Logger.Warn("the merge editor cannot take the result", "err", err)
		p.failed = err
		return p
	}
	p.session = session
	result := input.Result()
	win.Documents.Retain(result)
	s.merge.number++
	lang := result.LanguageID()
	virtual := func(side, label string, lines []string) *document.Document {
		return document.NewVirtual(document.VirtualURI(fmt.Sprintf("/merge/%d/%s", s.merge.number, side)),
			document.VirtualContent{Text: diff.JoinLines(lines, "\n"), Label: label, Language: lang})
	}
	docs := [3]*document.Document{
		virtual("incoming", prep.inputs.Input1Label, prep.inputs.Input1),
		virtual("current", prep.inputs.Input2Label, prep.inputs.Input2),
		result,
	}
	for i := range docs {
		side := mergeeditor.Side(i)
		p.inputs[i] = editorgroup.NewTextInput(docs[i], s.inputOptions())
		p.sides[i] = s.newTextEditorWith(binding, p.inputs[i], func(o editor.Options) editor.Options { return mergeeditor.AdjustOptions(o, side) })
	}
	p.model = mergeeditor.NewModel(mergeeditor.ModelOpts{
		Session:  session,
		Incoming: p.sides[0].view, Current: p.sides[1].view, Result: p.sides[2].view,
		Focus:         [3]*editor.EditorFocus{p.sides[0].focus, p.sides[1].focus, p.sides[2].focus},
		IncomingLabel: prep.inputs.Input1Label, CurrentLabel: prep.inputs.Input2Label, ResultLabel: s.displayPath(result),
		Post:    binding.Post,
		OnError: func(err error) { win.Process.Logger.Warn("the merge editor failed", "err", err) },
	})
	return p
}

// displayPath is the path of a file for a label: relative to the folder of the window when it is inside it, else with the home
// folder written "~".
func (s *shellState) displayPath(doc *document.Document) string {
	return mergeDisplayPath(doc.URI(), s.inputOptions())
}

// mergeDisplayPath is the label of the result of a merge: the folder the description of the tab gives, and the name.
func mergeDisplayPath(uri document.URI, opts editorgroup.TextInputOpts) string {
	if uri.Scheme != document.SchemeFile {
		return uri.Title()
	}
	if opts.Folder != "" {
		if rel, err := filepath.Rel(opts.Folder, uri.Path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	if opts.Home != "" {
		if rel, err := filepath.Rel(opts.Home, uri.Path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "~" + string(filepath.Separator) + rel
		}
	}
	return uri.Path
}

// Model is the model of the merge editor of the pane; nil for a pane that failed.
func (p *mergePane) Model() *mergeeditor.Model { return p.model }

// build implements pane.
func (p *mergePane) build(bc widgets.BuildContext, group *editorgroup.Group, active bool) widgets.Widget {
	if p.model == nil {
		return p.wrap(p.s, group, mergeFailedWidget(bc, p.failed))
	}
	win := PeekWindow(bc)
	opts := func(ed *textEditor, autofocus bool) editor.EditorOpts {
		return editor.EditorOpts{
			Commands: win.Commands, Keys: win.Keys, Scopes: win.Scopes, Autofocus: autofocus, Context: win.ctx,
			ClipboardMemory: win.Clipboard, FindMemory: win.Find, Zoom: win.Zoom, Focus: ed.focus,
			OnFocus: func(has bool) { p.s.editorFocus(group, ed, has) },
		}
	}
	return p.wrap(p.s, group, mergeeditor.NewWidget(p.model, mergeeditor.WidgetOpts{
		Incoming: opts(p.sides[0], false), Current: opts(p.sides[1], false), Result: opts(p.sides[2], active),
	}))
}

// focus implements pane: the result takes the focus, as VS Code's merge editor does.
func (p *mergePane) focus() {
	if p.model != nil {
		p.model.FocusSide(mergeeditor.Result)
	}
}

// dispose implements pane: the merge editor ends, the editors end and the result document is let go.
func (p *mergePane) dispose() {
	if p.disposed {
		return
	}
	p.disposed = true
	win := p.s.win
	if p.model != nil {
		p.model.Dispose()
	}
	if p.session != nil {
		p.session.Dispose()
	}
	for i, ed := range p.sides {
		if ed == nil {
			continue
		}
		delete(p.s.group.focused, ed)
		ed.dispose()
		if win.Documents.Release(ed.doc) {
			p.s.autoSaveUntrack(ed.doc)
			p.s.externalUntrack(ed.doc)
		}
		p.inputs[i].Dispose()
	}
	p.s.launchMergeClosed(p.input)
}

// mergeFailedWidget says that a merge could not be shown.
func mergeFailedWidget(bc widgets.BuildContext, err error) widgets.Widget {
	message := "The merge could not be shown."
	if err != nil {
		message += " " + describe(err)
	}
	colors := kit.ThemeOf(bc)
	return widgets.NewColoredBox(colors.EditorBackground, widgets.NewCenter(
		widgets.NewText(message, widgets.TextOpts{Style: uiTextStyle(bc, 13, colors.Foreground), MaxLines: foundation.Some(3)})))
}

// ---- opening ----

// mergePanes are the merge editors on screen: the one of the active group when its active tab is a merge editor.
func (s *shellState) activeMergePane() *mergePane {
	if s.group.grid == nil {
		return nil
	}
	p, _ := s.group.panes[s.activeGroup().Model.ActiveEditor()].(*mergePane)
	if p == nil || p.model == nil {
		return nil
	}
	return p
}

// mergeKeys are the values of isMergeEditor (the active editor is a merge editor) and mergeEditorResultUri (its result).
func (s *shellState) mergeKeys() (active bool, resultURI string) {
	if s.group.grid == nil {
		return false, ""
	}
	if m, ok := s.activeGroup().Model.ActiveEditor().(*editorgroup.MergeInput); ok {
		return true, m.Result().URI().String()
	}
	return false, ""
}

// openMerge shows a merge in a group (the active one by default), as an editor that is kept, and makes it the active
// editor. The caller has acquired the result document once; the use is given back here, the pane counts its own. A merge of
// the same result that the group shows already is activated and the prepared merge is dropped. It returns the input of
// the tab that shows the merge.
func (s *shellState) openMerge(prep *mergePrepared, source editorgroup.MergeSource, result *document.Document, group *editorgroup.Group) *editorgroup.MergeInput {
	win := PeekWindow(s.Context())
	if group == nil {
		group = s.activeGroup()
	}
	defer win.Documents.Release(result)
	input := editorgroup.NewMergeInput(result, source, s.inputOptions())
	if s.merge.prepared == nil {
		s.merge.prepared = map[*editorgroup.MergeInput]*mergePrepared{}
	}
	s.merge.prepared[input] = prep
	shown := input
	s.SetState(func() {
		s.nav.hold++
		opened, isNew := group.Model.OpenEditor(input, editorgroup.OpenOptions{Pinned: true, Active: true})
		s.nav.hold--
		if !isNew {
			delete(s.merge.prepared, input)
			if m, ok := opened.(*editorgroup.MergeInput); ok {
				shown = m
			}
			input.Dispose()
		}
		s.syncNav()
		if err := s.group.grid.Activate(group); err != nil {
			win.Process.Logger.Error("cannot activate the group", "err", err)
		}
	})
	s.refreshKeys()
	return shown
}

// restoreMerge makes the input of a saved merge editor from the document that PrepareRestore opened for its result
// (openSavedEditors); false when the merge cannot be made again (a file of it is gone, or the marker file lost its
// markers: its document then opens as a text editor when it has unsaved changes). The document is marked as used.
func (s *shellState) restoreMerge(r *restoredWindow, value string, used map[string]bool) (editorgroup.EditorInput, bool) {
	source, uri, err := editorgroup.ParseMergeInput(value)
	if err != nil {
		s.win.Process.Logger.Warn("a saved merge editor cannot be restored", "value", value, "err", describe(err))
		return nil, false
	}
	doc := r.docs[uri.String()]
	if doc == nil {
		return nil, false
	}
	prep, err := prepareSaved(s.win.ctx, source)
	if err != nil {
		s.win.Process.Logger.Warn("a saved merge editor cannot be made again", "result", source.Result, "err", describe(err))
		return nil, false
	}
	used[uri.String()] = true
	input := editorgroup.NewMergeInput(doc, source, s.inputOptions())
	if s.merge.prepared == nil {
		s.merge.prepared = map[*editorgroup.MergeInput]*mergePrepared{}
	}
	s.merge.prepared[input] = prep
	return input, true
}

// hasConflictMarkers tells whether a document is a file whose text has a complete conflict (the cheap test first). It looks
// at texts up to mergeNoticeLimit only.
func hasConflictMarkers(doc *document.Document) bool {
	if doc.URI().Scheme != document.SchemeFile || doc.LargeFile() || doc.Snapshot().Len() > mergeNoticeLimit {
		return false
	}
	text := doc.Text()
	if !diff.ContainsConflictMarkers(text) {
		return false
	}
	lines, _ := diff.SplitLines(text)
	return len(diff.ParseConflictMarkers(lines)) > 0
}

// noticeMergeConflicts offers the merge editor for a document that has conflict markers when a text editor of it opens: an
// information notification with the button "Resolve in Merge Editor". A restored editor and one that comes back from
// the closed ones do not ask again (the openOptions say so).
func (s *shellState) noticeMergeConflicts(doc *document.Document) {
	if !hasConflictMarkers(doc) {
		return
	}
	win := PeekWindow(s.Context())
	name := doc.URI().Title()
	win.Notify.Notify(notify.Notification{
		Severity: notify.Info, Source: "Emetos", Message: MergeConflictsNotice + " (" + name + ")",
		Actions: []notify.Action{{Label: MergeConflictsButton, Run: func() {
			if s.Mounted() {
				s.resolveInMergeEditor(doc)
			}
		}}},
	})
}

// resolveInMergeEditor opens the merge editor for a file with conflict markers in the active group: the inputs and the base are
// made from the markers on a goroutine (the diffs), and the result is the document of the file itself, whose text the
// merge editor replaces with the merged text as one undoable edit when it opens. A document that has no conflict, is not a
// file or is read-only is reported.
func (s *shellState) resolveInMergeEditor(doc *document.Document) {
	win := PeekWindow(s.Context())
	// a merge editor that shows the file already is the answer (its result has no markers any more)
	if group, input := s.mergeEditorOf(doc); input != nil {
		group.Model.SetActive(input)
		if err := s.group.grid.Activate(group); err != nil {
			win.Process.Logger.Error("cannot activate the group", "err", err)
		}
		s.refreshKeys()
		return
	}
	switch {
	case doc.URI().Scheme != document.SchemeFile || doc.ReadOnly():
		win.Notify.Error("Emetos", "Only a file that can be edited can be resolved in the merge editor.")
		return
	case !hasConflictMarkers(doc):
		win.Notify.Info("Emetos", MergeNoConflictsNotice)
		return
	}
	text := doc.Text()
	win.Documents.Retain(doc)
	s.Go(func(life context.Context) {
		prep, err := markersMerge(life, text)
		s.binding.Post(func() {
			if !s.Mounted() {
				win.Documents.Release(doc)
				return
			}
			if err != nil {
				win.Documents.Release(doc)
				win.Notify.Error("Emetos", "Cannot open the merge editor: "+describe(err))
				return
			}
			s.openMerge(prep, editorgroup.MergeSource{Kind: editorgroup.MergeMarkers, Result: doc.URI().Path}, doc, nil)
		})
	})
}

// mergeEditorOf finds the group that has a merge editor whose result is a document, and its input.
func (s *shellState) mergeEditorOf(doc *document.Document) (*editorgroup.Group, *editorgroup.MergeInput) {
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
			if m, ok := in.(*editorgroup.MergeInput); ok && m.Result() == doc {
				return g, m
			}
		}
	}
	return nil, nil
}

// openMergeFiles opens the merge editor of `--merge`: the four files are read in the background, the result file is
// created when it does not exist, and the merge is shown in the active group; done is called on the UI thread with the
// input of the tab, or with the error. Nothing is shown when a file cannot be read.
func (s *shellState) openMergeFiles(in1, in2, base, result, label1, label2 string, done func(*editorgroup.MergeInput, error)) {
	win := PeekWindow(s.Context())
	s.Go(func(life context.Context) {
		prep, err := filesMerge(life, base, in1, in2, label1, label2)
		var doc *document.Document
		missing := false
		if err == nil {
			if _, statErr := os.Stat(result); statErr == nil {
				doc, err = win.Documents.Acquire(life, result, document.NewOptions())
			} else if os.IsNotExist(statErr) {
				missing = true
			} else {
				err = errors.NewErr("MergeResultUnreadable", statErr).SetMetadata("path", result)
			}
		}
		s.binding.Post(func() {
			if !s.Mounted() {
				if doc != nil {
					win.Documents.Release(doc)
				}
				return
			}
			if err != nil {
				done(nil, err)
				return
			}
			if missing {
				// a result that is not there yet starts empty and is created when the merge is completed
				opts := document.NewOptions()
				opts.LanguageID = win.Process.Languages.Detect(result, "")
				doc, _ = win.Documents.OpenBlank(document.FileURI(result), opts)
				win.Documents.Retain(doc)
			}
			source := editorgroup.MergeSource{Kind: editorgroup.MergeFiles, Input1: in1, Input2: in2, Base: base, Result: result}
			done(s.openMerge(prep, source, doc, nil), nil)
		})
	})
}

// ---- commands ----

// mergeCommands are the commands of the merge editor: they act on the merge editor of the active group.
func (s *shellState) mergeCommands() map[string]command.Handler {
	goTo := func(next bool) command.Handler {
		return func(command.Context, any) error {
			if p := s.activeMergePane(); p != nil {
				p.model.GoToConflict(next)
			}
			return nil
		}
	}
	return map[string]command.Handler{
		"merge.goToNextUnhandledConflict":     goTo(true),
		"merge.goToPreviousUnhandledConflict": goTo(false),
		"git.openMergeEditor": func(_ command.Context, args any) error {
			return s.openMergeEditorCommand(args)
		},
		"_open.mergeEditor": func(_ command.Context, args any) error {
			return s.openMergeEditorWith(args)
		},
	}
}

// openMergeEditorCommand is git.openMergeEditor ("Resolve in Merge Editor"): it opens the merge editor for the file of the
// editor the command names (a tab, as the commands of the tabs do) or, without one, the active text editor, and says so
// when there is none or its file has no conflict.
func (s *shellState) openMergeEditorCommand(args any) error {
	win := PeekWindow(s.Context())
	doc := s.targetDocument(args)
	if doc == nil {
		win.Notify.Info("Emetos", MergeNoConflictsNotice)
		return nil
	}
	s.resolveInMergeEditor(doc)
	return nil
}

// mergeSide is a side of the arguments of _open.mergeEditor: a path, or an object with the path as "uri" and the text of the
// header as "description".
type mergeSide struct{ path, label string }

// parseMergeSide reads a file of the arguments of _open.mergeEditor: a path or file:// URI, or {"uri", "description"}.
func parseMergeSide(v any) (mergeSide, bool) {
	var side mergeSide
	switch x := v.(type) {
	case string:
		side.path = x
	case map[string]any:
		side.path, _ = x["uri"].(string)
		side.label, _ = x["description"].(string)
	}
	side.path = strings.TrimPrefix(side.path, "file://")
	return side, filepath.IsAbs(side.path)
}

// openMergeEditorWith is _open.mergeEditor (VS Code's internal command): {"base", "input1", "input2", "output"}, each an
// absolute path or file:// URI, the inputs also {"uri", "description"}; it opens the merge editor as `emetos --merge`
// does. A wrong argument has the tag MergeOpenInvalid, a file that is none MergeOpenSideInvalid.
func (s *shellState) openMergeEditorWith(args any) error {
	m, ok := args.(map[string]any)
	if !ok {
		return errors.New("MergeOpenInvalid", `_open.mergeEditor needs {"base", "input1", "input2", "output"}`, "arguments", args)
	}
	var sides [4]mergeSide
	for i, key := range []string{"input1", "input2", "base", "output"} {
		side, ok := parseMergeSide(m[key])
		if !ok {
			return errors.New("MergeOpenSideInvalid", `_open.mergeEditor needs an absolute path or file:// URI for "`+key+`"`, "arguments", args)
		}
		sides[i] = side
	}
	win := PeekWindow(s.Context())
	s.openMergeFiles(sides[0].path, sides[1].path, sides[2].path, sides[3].path, sides[0].label, sides[1].label, func(_ *editorgroup.MergeInput, err error) {
		if err != nil {
			win.Notify.Error("Emetos", "Cannot open the merge editor: "+describe(err))
		}
	})
	return nil
}
