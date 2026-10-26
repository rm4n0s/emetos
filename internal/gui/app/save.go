package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// Saving, Save As, Save All, Revert and untitled files (SPEC §3.1, §6.3). The text of a document is written on
// a goroutine, so the UI never waits for the disk; the three steps of document.SaveJob run on the UI thread,
// on the goroutine, and on the UI thread again. A failure becomes one notification for the document, with the
// buttons that can mend it.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileCommands.ts (the save commands),
// src/vs/workbench/services/textfile/common/textFileEditorModel.ts and
// src/vs/workbench/services/textfile/browser/textFileService.ts (saveAs, the overwrite prompt)

// SaveDialogOptions are what the dialog that asks for the file to save in is told.
type SaveDialogOptions struct {
	// Name is the file name the dialog starts with, and Folder the folder it opens in.
	Name, Folder string
}

// chooseSavePath shows the save dialog of the desktop (a file chooser portal) and waits for the user. It must be
// called from a goroutine, as Glutter's dialogs wait for the user. A dialog that is closed without a choice gives
// "" and no error. The error has the tag SaveDialogFailed; when the desktop has no file chooser its cause has the
// tag FileChooserUnavailable (see dialogUnavailable).
func chooseSavePath(ctx context.Context, bc widgets.BuildContext, o SaveDialogOptions) (string, error) {
	res, err := desktop.SaveFile(ctx, bc, desktop.SaveFileOpts{
		Title: "Save As", AcceptLabel: "Save", CurrentName: o.Name, CurrentFolder: o.Folder,
	})
	if err != nil {
		return "", errors.NewErr("SaveDialogFailed", err)
	}
	if res.Cancelled || len(res.Paths) == 0 {
		return "", nil
	}
	return res.Paths[0], nil
}

// dialogUnavailable tells whether the save dialog failed because the desktop has no file chooser, which sends the
// window to its own prompt for the path.
func dialogUnavailable(err error) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "FileChooserUnavailable" {
			return true
		}
	}
	return false
}

// saveRequest says how a document is saved.
type saveRequest struct {
	// skipParticipants saves without the save participants (Save without Formatting).
	skipParticipants bool
	// reason is why the document is saved.
	reason document.SaveReason
	// overwrite saves over a read-only file, and over a file that exists in Save As.
	overwrite bool
	// ignoreConflict saves although another program changed the file.
	ignoreConflict bool
	// prepared says that the code actions and the formatter of the language features already ran for this save (saveformat.go).
	prepared bool
}

// saveRun is a save that is going on for a document: a request that comes meanwhile waits for it.
type saveRun struct {
	queued  *saveRequest
	waiters []func(bool)
}

// saveState is what the shell keeps of saving.
type saveState struct {
	// running are the documents that are being written.
	running map[*document.Document]*saveRun
	// notes are the notifications of the saves that failed, one for each document.
	notes map[*document.Document]uint64
	// asOpen are the documents that wait for the dialog of Save As (or its prompt): auto save leaves them alone.
	asOpen map[*document.Document]bool
	// removeCommands take the handlers of the save commands off the registry.
	removeCommands []func()
}

// saveFailure is the failure of a save, with what the buttons of its notification do.
type saveFailure struct {
	doc *document.Document
	err error
	// again runs the operation that failed once more, with the request changed by the button.
	again func(saveRequest)
	req   saveRequest
	// asEncoding opens the encodings to save in; nil leaves the button out.
	asEncoding func()
}

// initSave binds the commands of saving while the shell is on screen.
func (s *shellState) initSave(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	st := &s.saves
	st.running = map[*document.Document]*saveRun{}
	st.notes = map[*document.Document]uint64{}
	st.asOpen = map[*document.Document]bool{}
	save := func(skip bool) command.Handler {
		return func(_ command.Context, args any) error {
			if doc := s.targetDocument(args); doc != nil {
				s.saveDocument(doc, saveRequest{skipParticipants: skip}, nil)
			}
			return nil
		}
	}
	saveAll := func(command.Context, any) error { s.saveAll(); return nil }
	handlers := map[string]command.Handler{
		"workbench.action.files.save":                  save(false),
		"workbench.action.files.saveWithoutFormatting": save(true),
		"workbench.action.files.saveAs": func(_ command.Context, args any) error {
			if doc := s.targetDocument(args); doc != nil {
				s.saveDocumentAs(doc, saveRequest{}, nil)
			}
			return nil
		},
		"saveAll":                        saveAll,
		"workbench.action.files.saveAll": saveAll,
		"workbench.action.files.revert": func(_ command.Context, args any) error {
			if doc := s.targetDocument(args); doc != nil {
				s.revertDocument(binding, doc, nil)
			}
			return nil
		},
		"workbench.action.files.newUntitledFile": func(_ command.Context, args any) error {
			s.newUntitled(binding, args)
			return nil
		},
	}
	for id, h := range s.autoSaveCommands() {
		handlers[id] = h
	}
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		st.removeCommands = append(st.removeCommands, remove)
	}
}

// disposeSave gives the commands of saving back.
func (s *shellState) disposeSave() {
	for _, remove := range s.saves.removeCommands {
		remove()
	}
	s.saves.removeCommands = nil
}

// targetDocument is the document a save command acts on: the one of the editor its argument names (the tab that was
// clicked), else of the active editor; the modified side of a diff editor. It is nil for an editor that has no
// document.
func (s *shellState) targetDocument(args any) *document.Document {
	_, input := s.commandTarget(args)
	if input == nil {
		return nil
	}
	return editorgroup.EditedDocument(input)
}

// ---- participants ----

// editorOf is an editor that shows a document: the active one when it does, else the first of the groups.
func (s *shellState) editorOf(doc *document.Document) *textEditor {
	if e := s.activeEditor(); e != nil && e.doc == doc {
		return e
	}
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
			if e := s.group.editors[in]; e != nil && e.doc == doc {
				return e
			}
		}
	}
	return nil
}

// saveParticipants are the participants the settings turn on for the language of a document.
func (w *Workspace) saveParticipants(doc *document.Document) edit.SaveParticipants {
	lang := doc.LanguageID()
	return edit.SaveParticipants{
		TrimTrailingWhitespace: settings.Get[bool](w.Settings, "files.trimTrailingWhitespace", lang),
		InsertFinalNewline:     settings.Get[bool](w.Settings, "files.insertFinalNewline", lang),
		TrimFinalNewlines:      settings.Get[bool](w.Settings, "files.trimFinalNewlines", lang),
	}
}

// participate runs the save participants over a document before it is written: through the editor that shows it,
// so that its cursors stay where they are, else on the text alone.
func (s *shellState) participate(doc *document.Document, reason document.SaveReason) error {
	p := PeekWorkspace(s.Context()).saveParticipants(doc)
	if !p.Any() {
		return nil
	}
	if e := s.editorOf(doc); e != nil {
		return e.view.ApplySaveParticipants(p, reason == document.SaveAuto)
	}
	res := edit.NewCommands(edit.NewModel(doc.Snapshot()), edit.DefaultOptions()).SaveParticipants(nil, p, reason == document.SaveAuto)
	if len(res.Edits) == 0 {
		return nil
	}
	doc.PushStackElement()
	_, err := doc.Edit(res.Edits, document.EditOptions{Kind: res.Kind})
	doc.PushStackElement()
	return err
}

// saveOptions are the options of a document's save for a request.
func (s *shellState) saveOptions(doc *document.Document, req saveRequest) document.SaveOptions {
	win := PeekWorkspace(s.Context())
	opts := document.SaveOptions{
		Reason: req.reason, Overwrite: req.overwrite, IgnoreConflict: req.ignoreConflict,
		DetectLanguage: win.Process.Languages.Detect,
	}
	if settings.Get[string](win.Settings, "files.saveConflictResolution", doc.LanguageID()) == "overwriteFileOnDisk" {
		opts.IgnoreConflict = true
	}
	if !req.skipParticipants {
		opts.Participate = func(d *document.Document, reason document.SaveReason) error { return s.participate(d, reason) }
	}
	return opts
}

// ---- save ----

// saveDocument saves a document in its file. An untitled document goes to Save As, and a read-only one is not
// saved (a quiet message tells why). done, when set, is told whether the document was saved.
func (s *shellState) saveDocument(doc *document.Document, req saveRequest, done func(saved bool)) {
	finish := func(ok bool) {
		if done != nil {
			done(ok)
		}
	}
	win := PeekWorkspace(s.Context())
	switch {
	case doc.ReadOnly():
		win.Notify.Notify(notify.Notification{
			Severity: notify.Info, Source: "Emetos", Silent: true,
			Message: "Cannot save '" + doc.URI().Title() + "': it is read-only.",
		})
		finish(false)
		return
	case doc.URI().Scheme != document.SchemeFile:
		s.saveDocumentAs(doc, req, done)
		return
	}
	if run := s.saves.running[doc]; run != nil {
		// the write goes on: save again when it is done, so that what was typed meanwhile is saved too
		run.queued = &req
		if done != nil {
			run.waiters = append(run.waiters, done)
		}
		return
	}
	if plan := s.savePlanFor(doc, req); plan != nil {
		// the code actions and the formatter take a while: a request that comes meanwhile waits like it does for a write
		run := &saveRun{}
		if done != nil {
			run.waiters = append(run.waiters, done)
		}
		s.saves.running[doc] = run
		req.prepared = true
		plan.run(func() {
			delete(s.saves.running, doc)
			s.saveDocument(doc, req, func(ok bool) {
				for _, w := range run.waiters {
					w(ok)
				}
			})
		})
		return
	}
	job, err := doc.BeginSave(s.saveOptions(doc, req))
	if err != nil {
		s.saveFailed(saveFailure{doc: doc, err: err, req: req, again: func(r saveRequest) { s.saveDocument(doc, r, done) }, asEncoding: s.encodingChooser(doc)}, finish)
		return
	}
	run := &saveRun{}
	if done != nil {
		run.waiters = append(run.waiters, done)
	}
	s.saves.running[doc] = run
	again := func(r saveRequest) { s.saveDocument(doc, r, nil) }
	s.writeJob(job, func(stat fileio.FileStat, err error) {
		delete(s.saves.running, doc)
		waiters := run.waiters
		all := func(ok bool) {
			for _, w := range waiters {
				w(ok)
			}
		}
		if err != nil {
			s.saveFailed(saveFailure{doc: doc, err: err, req: req, again: again, asEncoding: s.encodingChooser(doc)}, all)
			return
		}
		oldURI, oldLang := doc.URI(), doc.LanguageID()
		doc.FinishSave(job, stat)
		s.saved(doc, oldURI, oldLang)
		if run.queued != nil && doc.Dirty() {
			s.saveDocument(doc, *run.queued, nil)
		}
		all(true)
	})
}

// writeJob writes a save job on a goroutine and tells the file watcher about the write at once, so that the
// event of our own write is not taken for a change by another program (SPEC §10.5); then, on the UI thread,
// it calls done with the stat and the error. Nothing is called when the shell is gone.
func (s *shellState) writeJob(job *document.SaveJob, done func(fileio.FileStat, error)) {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	s.Go(func(life context.Context) {
		stat, err := job.Write(life)
		if err == nil {
			if w := win.Watcher(); w != nil {
				w.Suppress(job.Path(), stat)
			}
		}
		binding.Post(func() {
			if err != nil {
				job.Abort() // the document is not being written any more, whether or not the shell is still there
			}
			if s.Mounted() {
				done(stat, err)
				// a look at the file that came while it was written is made now: the document has the stat of
				// our write (or, after a failure, none of it)
				s.saveSettled(job.Document())
			}
		})
	})
}

// saved is what the window does when a document was written: the failure notification goes, the file counts as
// recently opened, and after a Save As the editors of the document follow the new name, the history follows the
// new URI and the language is found again.
func (s *shellState) saved(doc *document.Document, oldURI document.URI, oldLang string) {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	s.dismissSaveNote(doc)
	uri := doc.URI()
	if uri != oldURI {
		if path, ok := filePath(uri.Scheme, uri.Path); ok {
			win.AddRecentFile(path)
		}
		if h := s.nav.history; h != nil && oldURI.Scheme == document.SchemeUntitled {
			h.Follow(func(u document.URI) (document.URI, bool) {
				if u == oldURI {
					return uri, true
				}
				return u, true
			})
		}
	}
	if doc.LanguageID() != oldLang {
		for _, e := range s.group.editors {
			if e.doc == doc {
				s.languageChanged(binding, e)
			}
		}
	}
	s.refreshKeys()
	s.refreshStatus()
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// dismissSaveNote takes the notification of a failed save of a document away.
func (s *shellState) dismissSaveNote(doc *document.Document) {
	if id, ok := s.saves.notes[doc]; ok {
		s.win.Notify.Dismiss(id)
		delete(s.saves.notes, doc)
	}
}

// saveFailed tells the user that a save failed, in one sticky notification for the document with the buttons
// that suit the failure (SPEC §6.3): Retry; Overwrite for a file that another program changed or that is
// read-only; Revert for the first; Save with Encoding for a character the encoding lacks; and Save As.
func (s *shellState) saveFailed(f saveFailure, finish func(bool)) {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	win.Process.Logger.Warn("saving failed", "uri", f.doc.URI().String(), "err", f.err, "reason", f.req.reason.String())
	s.autoSaveFailed(f.doc) // no auto save of the document until it is saved or read again
	name := f.doc.URI().Title()
	tag := ""
	if appErr, ok := errors.FromError(f.err); ok {
		tag = appErr.Tag
	}
	msg := "Failed to save '" + name + "': " + describe(f.err)
	var actions []notify.Action
	retryWith := func(label string, change func(*saveRequest)) notify.Action {
		return notify.Action{Label: label, Run: func() {
			r := f.req
			r.reason = document.SaveExplicit // the user asks now, whatever failed before
			change(&r)
			f.again(r)
		}}
	}
	switch tag {
	case "FileModifiedSince":
		msg = "Failed to save '" + name + "': The content of the file is newer. Overwrite the content of the file with your changes, or revert to the content of the file."
		actions = append(actions,
			notify.Action{Label: "Compare", Run: func() { s.compareConflict(f.doc) }},
			retryWith("Overwrite", func(r *saveRequest) { r.ignoreConflict = true }),
			notify.Action{Label: "Revert", Run: func() { s.revertDocument(binding, f.doc, nil) }})
	case "FileReadOnly":
		msg = "Failed to save '" + name + "': The file is read-only. Overwrite to make it writable and save."
		actions = append(actions, retryWith("Overwrite", func(r *saveRequest) { r.overwrite = true }))
	case "EncodingCannotRepresent":
		if f.asEncoding != nil {
			actions = append(actions, notify.Action{Label: "Save with Encoding...", Run: f.asEncoding})
		}
	default:
		actions = append(actions, retryWith("Retry", func(*saveRequest) {}))
	}
	actions = append(actions, notify.Action{Label: "Save As...", Run: func() { s.saveDocumentAs(f.doc, saveRequest{}, nil) }})
	s.dismissSaveNote(f.doc)
	s.saves.notes[f.doc] = win.Notify.Notify(notify.Notification{
		Severity: notify.Error, Source: "Emetos", Message: msg, Actions: actions, Sticky: true,
	})
	if finish != nil {
		finish(false)
	}
}

// ---- save as ----

// saveAsSuggestion is the name and the folder the dialog of Save As starts with: the file's own, or for an untitled
// document its name with the extension of its language, in the folder of the window or the home folder.
func (s *shellState) saveAsSuggestion(doc *document.Document) SaveDialogOptions {
	win := PeekWorkspace(s.Context())
	uri := doc.URI()
	if uri.Scheme == document.SchemeFile {
		return SaveDialogOptions{Name: filepath.Base(uri.Path), Folder: filepath.Dir(uri.Path)}
	}
	name := uri.Title()
	if l, ok := win.Process.Languages.Language(doc.LanguageID()); ok && doc.LanguageID() != document.DefaultLanguage && len(l.Extensions) > 0 {
		name += l.Extensions[0]
	}
	folder := win.Folder
	if folder == "" {
		folder, _ = os.UserHomeDir()
	}
	return SaveDialogOptions{Name: name, Folder: folder}
}

// saveDocumentAs asks for the file to save a document in and saves it there: the dialog of the desktop on a
// goroutine, or, where the desktop has none, a prompt for the path. A cancelled dialog changes nothing.
func (s *shellState) saveDocumentAs(doc *document.Document, req saveRequest, done func(saved bool)) {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	bc := s.Context()
	finish := func(ok bool) {
		if done != nil {
			done(ok)
		}
	}
	if doc.ReadOnly() {
		s.saveDocument(doc, req, done) // says why it cannot (a read-only file, and a virtual document)
		return
	}
	// auto save leaves the document alone while the dialog waits, and tries it again if the dialog gives nothing
	s.saves.asOpen[doc] = true
	inner := finish
	finish = func(ok bool) {
		delete(s.saves.asOpen, doc)
		if !ok {
			s.autoSaveRetry(doc)
		}
		inner(ok)
	}
	req.overwrite, req.ignoreConflict = false, false
	suggestion := s.saveAsSuggestion(doc)
	choose := win.SaveDialog
	if choose == nil {
		choose = chooseSavePath
	}
	s.Go(func(life context.Context) {
		path, err := choose(life, bc, suggestion)
		binding.Post(func() {
			if !s.Mounted() {
				return
			}
			switch {
			case err != nil && dialogUnavailable(err):
				s.promptSavePath(doc, req, suggestion, finish)
			case err != nil:
				win.Process.Logger.Warn("the save dialog failed", "err", err)
				win.Notify.Error("Emetos", "Cannot show the dialog to save a file: "+describe(err))
				finish(false)
			case path == "":
				finish(false)
			default:
				s.saveAsPath(doc, path, req, finish)
			}
		})
	})
}

// validateSavePath checks the path typed in the prompt of Save As and gives the message to show when it cannot be
// saved in, "" when it can.
func validateSavePath(value string) string {
	switch {
	case strings.TrimSpace(value) == "":
		return "Enter the path of the file to save in."
	case !filepath.IsAbs(value):
		return "The path must be absolute."
	case strings.HasSuffix(value, "/"):
		return "Enter the name of a file, not of a folder."
	}
	dir := filepath.Dir(value)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "The folder '" + dir + "' does not exist."
	}
	if info, err := os.Stat(value); err == nil && info.IsDir() {
		return "'" + filepath.Base(value) + "' is a folder: enter the name of a file."
	}
	return ""
}

// promptSavePath asks for the path in the quick input, with the suggestion in full to edit; Escape cancels.
func (s *shellState) promptSavePath(doc *document.Document, req saveRequest, suggestion SaveDialogOptions, finish func(bool)) {
	win := PeekWorkspace(s.Context())
	win.QuickInput.Prompt(quickinput.PromptOptions{
		Placeholder: "Enter the path of the file to save in",
		Value:       filepath.Join(suggestion.Folder, suggestion.Name),
		Validate:    validateSavePath,
		OnAccept: func(value string) {
			if s.Mounted() {
				s.saveAsPath(doc, value, req, finish)
			}
		},
		OnCancel: func() { finish(false) },
	})
}

// saveAsPath saves a document in a file and makes that file the document's own. A file that exists asks first
// whether to replace it.
func (s *shellState) saveAsPath(doc *document.Document, path string, req saveRequest, finish func(bool)) {
	win := PeekWorkspace(s.Context())
	path = filepath.Clean(path)
	again := func(r saveRequest) { s.saveAsPath(doc, path, r, nil) }
	if err := checkSaveAsTarget(win, doc, path); err != nil {
		s.saveFailed(saveFailure{doc: doc, err: err, req: req, again: again}, finish)
		return
	}
	if plan := s.savePlanFor(doc, req); plan != nil {
		req.prepared = true
		plan.run(func() { s.saveAsPath(doc, path, req, finish) })
		return
	}
	job, err := doc.BeginSaveAs(path, s.saveOptions(doc, req))
	if err != nil {
		s.saveFailed(saveFailure{doc: doc, err: err, req: req, again: again}, finish)
		return
	}
	s.writeJob(job, func(stat fileio.FileStat, err error) {
		if err != nil {
			if appErr, ok := errors.FromError(err); ok && appErr.Tag == "FileExists" {
				s.confirmReplace(doc, path, req, finish)
				return
			}
			s.saveFailed(saveFailure{doc: doc, err: err, req: req, again: again, asEncoding: s.encodingChooser(doc)}, finish)
			return
		}
		oldURI, oldLang := doc.URI(), doc.LanguageID()
		doc.FinishSave(job, stat)
		s.saved(doc, oldURI, oldLang)
		if finish != nil {
			finish(true)
		}
	})
}

// checkSaveAsTarget fails with the tag SaveAsTargetOpen when the file to save in is open in the window as another
// document: the window does not merge two documents of one file, so that one is closed first.
func checkSaveAsTarget(win *Workspace, doc *document.Document, path string) error {
	if other, open := win.Documents.Get(document.FileURI(path)); open && other != doc {
		return errors.New("SaveAsTargetOpen", "the file is open in another editor; close it first", "path", path)
	}
	return nil
}

// confirmReplace asks whether to replace a file that exists, as VS Code's confirmOverwrite.
func (s *shellState) confirmReplace(doc *document.Document, path string, req saveRequest, finish func(bool)) {
	name, dir := filepath.Base(path), filepath.Dir(path)
	s.confirm(confirmSpec{
		Type:    kit.DialogWarning,
		Message: "'" + name + "' already exists. Do you want to replace it?",
		Detail:  "A file or folder with the name '" + name + "' already exists in the folder '" + filepath.Base(dir) + "'. Replacing it will overwrite its current contents.",
		Primary: "Replace",
	}, func(confirmed, _ bool) {
		if !s.Mounted() {
			return
		}
		if !confirmed {
			if finish != nil {
				finish(false)
			}
			return
		}
		req.overwrite = true
		s.saveAsPath(doc, path, req, finish)
	})
}

// encodingChooser gives the function that opens the encodings to save a document in, for the button of a save that
// failed for a character the encoding lacks; nil when no editor shows the document.
func (s *shellState) encodingChooser(doc *document.Document) func() {
	if e := s.editorOf(doc); e != nil {
		binding := widgets.WidgetsBindingOf(s.Context())
		return func() {
			if !e.disposed {
				s.showSaveEncodingList(binding, e, "")
			}
		}
	}
	return nil
}

// ---- save all ----

// dirtyDocuments are the documents of the editors of all the groups that have unsaved changes, each once, in the
// order of the groups and their tabs.
func (s *shellState) dirtyDocuments() []*document.Document {
	var out []*document.Document
	seen := map[*document.Document]bool{}
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
			if doc := editorgroup.EditedDocument(in); doc != nil && !seen[doc] && doc.Dirty() {
				seen[doc] = true
				out = append(out, doc)
			}
		}
	}
	return out
}

// saveAll saves the documents with unsaved changes one after the other. An untitled one asks for its file with
// Save As; cancelling that stops the rest, as VS Code's Save All does. A file that cannot be saved is reported and
// the others go on.
func (s *shellState) saveAll() {
	docs := s.dirtyDocuments()
	var next func(i int)
	next = func(i int) {
		if i >= len(docs) || !s.Mounted() {
			return
		}
		doc := docs[i]
		if !doc.Dirty() {
			next(i + 1)
			return
		}
		untitled := doc.URI().Scheme != document.SchemeFile
		s.saveDocument(doc, saveRequest{}, func(saved bool) {
			if !saved && untitled {
				return // the dialog of an untitled document was cancelled, or its save failed: stop
			}
			next(i + 1)
		})
	}
	next(0)
}

// ---- revert ----

// revertDocument reads a document again from its file, dropping its unsaved changes. An untitled document
// becomes empty. done, when set, is told whether it was reverted.
func (s *shellState) revertDocument(binding *widgets.WidgetsBinding, doc *document.Document, done func(bool)) {
	win := PeekWorkspace(s.Context())
	finish := func(ok bool) {
		if done != nil {
			done(ok)
		}
	}
	job, err := doc.BeginRevert(fileio.ReadOptions{})
	if err != nil {
		win.Notify.Error("Emetos", "Failed to revert '"+doc.URI().Title()+"': "+describe(err))
		finish(false)
		return
	}
	s.Go(func(life context.Context) {
		res, err := job.Read(life)
		binding.Post(func() {
			if !s.Mounted() {
				return
			}
			if err != nil {
				win.Process.Logger.Warn("reverting failed", "uri", doc.URI().String(), "err", err)
				win.Notify.Error("Emetos", "Failed to revert '"+doc.URI().Title()+"': "+describe(err),
					notify.Action{Label: "Retry", Run: func() { s.revertDocument(binding, doc, done) }})
				finish(false)
				return
			}
			views := s.captureViews(doc)
			doc.FinishRevert(job, res)
			s.dismissSaveNote(doc)
			s.documentReloaded(binding, doc, views, true)
			finish(true)
		})
	})
}

// documentReloaded makes the editors of a document, whose text was replaced as a whole (a revert, a reopen with
// another encoding, a file that changed on disk), draw it again and keep their view state: the cursors inside the
// new text, the scroll position and the folds that still have their lines. views is what captureViews took before
// the text was replaced; an editor that is not in it only gets its cursors moved into the text. The window shows
// the state of the document, and the keyboard focus goes back to the editor when focus is set (the user asked for
// it; a file that changed on disk takes no focus).
func (s *shellState) documentReloaded(binding *widgets.WidgetsBinding, doc *document.Document, views []viewSnap, focus bool) {
	win := PeekWorkspace(s.Context())
	for _, other := range s.group.editors {
		if other.doc != doc || other.disposed {
			continue
		}
		other.view.DocumentChanged()
		i := slices.IndexFunc(views, func(v viewSnap) bool { return v.ed == other })
		if i >= 0 {
			if err := other.view.RestoreView(views[i].state); err != nil {
				win.Process.Logger.Debug("a fold could not be restored after the file was read again", "uri", doc.URI().String(), "err", err)
			}
			continue
		}
		snap := doc.Snapshot()
		sels := other.view.Cursors().Selections()
		for i := range sels {
			sels[i].Anchor, sels[i].Active = snap.ValidatePosition(sels[i].Anchor), snap.ValidatePosition(sels[i].Active)
		}
		other.view.Cursors().SetSelections(sels)
		other.view.SelectionsChanged()
	}
	s.refreshKeys()
	s.refreshStatus()
	if focus {
		s.askFocus(binding)
	}
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// ---- untitled files ----

// NewUntitled makes an untitled document: empty, in the language of files.defaultLanguage (the language of the
// editor that is active when it is "${activeEditorLanguage}"; plain text when it names none that exists), the
// encoding of files.encoding and the line ending of files.eol. languageID, when it names a language, wins over the
// setting. It is open in the window's documents but not shown.
func (w *Workspace) NewUntitled(languageID, activeLanguage string) *document.Document {
	lang := languageID
	if lang == "" {
		lang = settings.Get[string](w.Settings, "files.defaultLanguage", "")
		if lang == "${activeEditorLanguage}" {
			lang = activeLanguage
		}
	}
	if lang == "" || !w.Process.Languages.IsRegistered(lang) {
		lang = document.DefaultLanguage
	}
	opts := document.NewOptions()
	opts.LanguageID = lang
	if settings.Get[string](w.Settings, "files.eol", lang) == "\r\n" {
		opts.LineEnding = fileio.CRLF
	}
	doc := w.Documents.NewUntitled(opts, settings.Get[string](w.Settings, "files.encoding", lang))
	return doc
}

// newUntitled opens a new untitled file in the active group, kept (not a preview) and with the keyboard focus.
// Its argument may be {"languageId": "go"}.
func (s *shellState) newUntitled(binding *widgets.WidgetsBinding, args any) {
	win := PeekWorkspace(s.Context())
	languageID := ""
	if m, ok := args.(map[string]any); ok {
		languageID, _ = m["languageId"].(string)
	}
	active := ""
	if e := s.activeEditor(); e != nil {
		active = e.doc.LanguageID()
	}
	doc := win.NewUntitled(languageID, active)
	win.Documents.Retain(doc)
	s.show(binding, doc, openOptions{})
	s.askFocus(binding)
}

// ---- encoding ----

// showSaveEncodingList shows the encodings to save the file of an editor in ("Save with Encoding"); the one the
// document has is active. Choosing one makes the document save in it and saves it at once (an untitled document
// asks for its file).
func (s *shellState) showSaveEncodingList(binding *widgets.WidgetsBinding, e *textEditor, guessed string) {
	win := PeekWorkspace(s.Context())
	configured := settings.Get[string](win.Settings, "files.encoding", e.doc.LanguageID())
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select File Encoding to Save with",
		Items: encodingPicks(configured, guessed, false, func(enc fileio.Encoding) {
			if e.disposed {
				return
			}
			if err := e.doc.SetSaveEncoding(enc.ID); err != nil {
				win.Notify.Error("Emetos", describe(err))
				return
			}
			s.refreshStatus()
			s.saveDocument(e.doc, saveRequest{}, func(bool) { s.askFocus(binding) })
		}),
		ActiveID: e.doc.Encoding(),
	})
}

// ---- context keys ----

// saveKeys sets the context keys of saving: activeEditorIsDirty (the active editor has unsaved changes),
// activeEditorCanRevert (an editor is open and is not an untitled one) and dirtyWorkingCopies (a document of the
// window has unsaved changes).
func (s *shellState) saveKeys() (dirty, canRevert, anyDirty bool) {
	win := PeekWorkspace(s.Context())
	if active := s.activeGroup().Model.ActiveEditor(); active != nil {
		dirty = active.Dirty()
		canRevert = !active.Capabilities().Has(editorgroup.CapUntitled)
	}
	for _, d := range win.Documents.All() {
		if d.Dirty() {
			anyDirty = true
			break
		}
	}
	return dirty, canRevert, anyDirty
}
