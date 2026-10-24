package app

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/views/explorer"
)

// The file operations of the Explorer in the window (SPEC §10.5): new file and folder, rename, delete, cut,
// copy and paste, drag and drop, undo and redo, and the editors that follow them. The operations themselves are
// core/workspace's FileOps; this file asks the user, runs them off the UI thread, and shows what happened.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts,
// src/vs/workbench/contrib/files/browser/fileCommands.ts,
// src/vs/workbench/contrib/files/browser/fileImportExport.ts,
// src/vs/workbench/contrib/files/browser/views/explorerViewer.ts (FileDragAndDrop) and
// src/vs/workbench/services/editor/browser/editorService.ts (handleMovedFile, handleDeletedFile)

// fileClip is the internal clipboard of the Explorer: the paths copied or cut.
type fileClip struct {
	paths []string
	cut   bool
}

// maxConfirmFiles is the number of names a confirmation lists (VS Code's MAX_CONFIRM_FILES).
const maxConfirmFiles = 10

// fileNamesMessage lists the names of files for the detail of a confirmation.
//
// Ported from VS Code: src/vs/platform/dialogs/common/dialogs.ts (getFileNamesMessage)
func fileNamesMessage(paths []string) string {
	var lines []string
	for i, p := range paths {
		if i == maxConfirmFiles {
			break
		}
		lines = append(lines, filepath.Base(p))
	}
	if extra := len(paths) - maxConfirmFiles; extra == 1 {
		lines = append(lines, "...1 additional file not shown")
	} else if extra > 1 {
		lines = append(lines, "..."+strconv.Itoa(extra)+" additional files not shown")
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

// confirmSpec describes a question that is answered Yes or No.
type confirmSpec struct {
	Type     kit.DialogType
	Message  string
	Detail   string
	Primary  string
	Checkbox string
}

// confirm asks a question in a dialog (VS Code's `dialogService.confirm`): the primary button confirms, Cancel
// and Escape do not.
func (s *shellState) confirm(spec confirmSpec, answer func(confirmed, checked bool)) {
	win := PeekWindow(s.Context())
	win.Dialogs.Show(kit.DialogSpec{
		Type: spec.Type, Message: spec.Message, Detail: strings.TrimRight(spec.Detail, "\n"),
		Buttons: []string{spec.Primary, "Cancel"}, CancelButton: foundation.Some(1),
		CheckboxLabel: spec.Checkbox,
		OnAnswer: func(r kit.DialogResult) {
			answer(!r.Cancelled && r.Button == 0, r.Checked)
		},
	})
}

// updateSetting writes a setting of the user, the way the "Do not ask me again" box does.
func (s *shellState) updateSetting(key string, value any) {
	win := PeekWindow(s.Context())
	if err := win.Settings.Update(win.ctx, key, value, settings.TargetUser); err != nil {
		win.Process.Logger.Warn("cannot write the setting", "key", key, "err", err)
		win.Process.Notify.Warn("Emetos", "Cannot save the setting "+key+": "+describe(err))
	}
}

// opsRun is what an operation reports.
type opsRun struct {
	res workspace.OpResult
	err error
}

// runFileOp runs a file operation on a goroutine, applies its events to the tree, and then, on the UI thread, makes the
// editors follow it and calls done with the outcome. Nothing runs when the shell is gone.
func (s *shellState) runFileOp(binding *widgets.WidgetsBinding, work func(ctx context.Context) (workspace.OpResult, error), done func(opsRun)) {
	win := PeekWindow(s.Context())
	s.Go(func(life context.Context) {
		res, err := work(life)
		if ws := win.currentWorkspace(); ws != nil && !res.Empty() {
			ws.Apply(life, res.Events())
		}
		binding.Post(func() {
			if !s.Mounted() {
				return
			}
			if !res.Empty() {
				s.followFileOp(binding, res)
			}
			if done != nil {
				done(opsRun{res: res, err: err})
			}
		})
	})
}

// reportOpError tells the user that an operation failed, with a button that tries again when retry is set.
func (s *shellState) reportOpError(err error, retry func()) {
	win := PeekWindow(s.Context())
	win.Process.Logger.Warn("file operation failed", "err", err)
	var actions []notify.Action
	if retry != nil {
		actions = append(actions, notify.Action{Label: "Retry", Run: retry})
	}
	win.Process.Notify.Error("Emetos", describe(err), actions...)
}

// ---- the editors follow the operations ----

// followFileOp makes the open editors follow an operation (VS Code's `handleMovedFile` and `handleDeletedFile`): a
// document whose file moved gets the new path (its tab, breadcrumbs and title follow, its text and unsaved
// changes stay, and its language is found again when the extension changed), and the editors of a file that was
// deleted close unless they have unsaved changes.
func (s *shellState) followFileOp(binding *widgets.WidgetsBinding, res workspace.OpResult) {
	win := PeekWindow(s.Context())
	// the recently opened files of the quick open follow the moves and lose the files that were deleted
	for _, p := range win.Recent.List() {
		switch path, gone := res.Follow(p); {
		case gone:
			win.Recent.Remove(p)
		case path != p:
			win.Recent.Rename(p, path)
		}
	}
	// the locations and the closed editors of the history follow too
	s.followNavigation(res.Follow)
	for _, doc := range win.Documents.All() {
		uri := doc.URI()
		if uri.Scheme != document.SchemeFile {
			continue
		}
		path, gone := res.Follow(uri.Path)
		switch {
		case gone:
			s.closeCleanEditors(doc)
		case path != uri.Path:
			s.renameDocument(binding, doc, path)
		}
	}
	s.refreshKeys()
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// closeCleanEditors closes the editors of a document in every group, unless the document has unsaved changes.
func (s *shellState) closeCleanEditors(doc *document.Document) {
	if doc.Dirty() {
		return
	}
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		var closing []editorgroup.EditorInput
		for _, e := range g.Model.Editors(editorgroup.Sequential, false) {
			if t, ok := e.(*editorgroup.TextInput); ok && t.Document() == doc {
				closing = append(closing, e)
			}
		}
		if len(closing) > 0 {
			s.closeEditors(g, closing)
		}
	}
}

// renameDocument gives a document the path its file moved to, and finds its language again when the language it
// has is the one its old name gave.
func (s *shellState) renameDocument(binding *widgets.WidgetsBinding, doc *document.Document, newPath string) {
	win := PeekWindow(s.Context())
	oldPath := doc.URI().Path
	firstLine, _, _ := strings.Cut(doc.Snapshot().String(), "\n")
	autoDetected := doc.LanguageID() == win.Process.Languages.Detect(oldPath, firstLine)
	if !win.Documents.Rename(doc, newPath) {
		win.Process.Logger.Debug("a document could not follow its file", "from", oldPath, "to", newPath)
		return
	}
	lang := win.Process.Languages.Detect(newPath, firstLine)
	if !autoDetected || lang == doc.LanguageID() {
		return
	}
	doc.SetLanguageID(lang)
	for _, e := range s.group.editors {
		if e.doc == doc {
			s.languageChanged(binding, e)
		}
	}
}

// externalFileEvents handles the events of the watcher on the UI thread. VS Code keeps the editor of a file that
// another program deleted or renamed open, unless workbench.editor.closeOnFileDelete is on; the editors with
// unsaved changes never close. The changes of our own operations were applied already, and are harmless here.
func (s *shellState) externalFileEvents(events []workspace.Event) {
	win := PeekWindow(s.Context())
	var removed []string
	for _, e := range events {
		switch e.Kind {
		case workspace.EventDeleted:
			removed = append(removed, e.Path)
		case workspace.EventRenamed:
			removed = append(removed, e.OldPath)
		default:
		}
	}
	// the history forgets the files that are gone (VS Code removes them when the watcher says they were deleted)
	s.forgetDeleted(removed)
	if !settings.Get[bool](win.Settings, "workbench.editor.closeOnFileDelete", "") {
		return
	}
	for _, e := range events {
		var gone string
		switch e.Kind {
		case workspace.EventDeleted:
			gone = e.Path
		case workspace.EventRenamed:
			gone = e.OldPath
		default:
			continue
		}
		for _, doc := range win.Documents.All() {
			uri := doc.URI()
			if uri.Scheme != document.SchemeFile || (uri.Path != gone && !strings.HasPrefix(uri.Path, gone+string(filepath.Separator))) {
				continue
			}
			if exists(uri.Path) {
				continue // the file is there: a faulty event, or a file renamed back
			}
			s.closeCleanEditors(doc)
		}
	}
}

// ---- commands ----

// bindFileOpsCommands takes over the commands of the file operations while the shell is on screen.
func (s *shellState) bindFileOpsCommands(binding *widgets.WidgetsBinding) {
	win := PeekWindow(s.Context())
	ex := &s.explorer
	run := func(f func()) command.Handler {
		return func(command.Context, any) error { f(); return nil }
	}
	newFile := run(func() { s.newInExplorer(binding, false) })
	newFolder := run(func() { s.newInExplorer(binding, true) })
	handlers := map[string]command.Handler{
		"explorer.newFile": newFile, "workbench.files.action.createFileFromExplorer": newFile,
		"explorer.newFolder": newFolder, "workbench.files.action.createFolderFromExplorer": newFolder,
		"renameFile":              run(func() { s.renameInExplorer() }),
		"moveFileToTrash":         run(func() { s.deleteInExplorer(binding, true) }),
		"deleteFile":              run(func() { s.deleteInExplorer(binding, false) }),
		"filesExplorer.cut":       run(func() { s.copyInExplorer(true) }),
		"filesExplorer.copy":      run(func() { s.copyInExplorer(false) }),
		"filesExplorer.paste":     run(func() { s.pasteInExplorer(binding) }),
		"filesExplorer.cancelCut": run(func() { s.clearFileClip() }),
		"explorer.openToSide":     func(_ command.Context, args any) error { s.openToSide(binding, args); return nil },
		"revealFileInOS":          func(_ command.Context, args any) error { s.revealInOS(binding, args); return nil },
	}
	for id, h := range handlers {
		remove, err := win.Process.Commands.SetHandler(id, h)
		if err != nil {
			panic(err) // the commands are registered by the program itself: a missing one is a broken build
		}
		ex.removeCommands = append(ex.removeCommands, remove)
	}
}

// bindUndoRedo takes undo and redo over for the file operations while the tree has the keyboard focus, and gives them
// back when it loses it.
func (s *shellState) bindUndoRedo(binding *widgets.WidgetsBinding, bind bool) {
	win := PeekWindow(s.Context())
	ex := &s.explorer
	for _, f := range ex.removeUndo {
		f()
	}
	ex.removeUndo = nil
	if !bind {
		return
	}
	for id, h := range map[string]command.Handler{
		"undo": func(command.Context, any) error { s.undoInExplorer(binding, false); return nil },
		"redo": func(command.Context, any) error { s.undoInExplorer(binding, true); return nil },
	} {
		remove, err := win.Process.Commands.SetHandler(id, h)
		if err != nil {
			panic(err)
		}
		ex.removeUndo = append(ex.removeUndo, remove)
	}
}

// targetFolder is the folder a new item, a paste or a drop goes into: the folder in the context, the folder
// of a file in the context, or the folder of the window.
func (s *shellState) targetFolder() (*workspace.Item, bool) {
	ex := &s.explorer
	if ex.view == nil {
		return nil, false
	}
	ctx := ex.view.Context(false)
	if len(ctx) == 0 {
		return ex.view.Workspace().Root(), true
	}
	if ctx[0].IsDir() {
		return ctx[0], true
	}
	if p := ctx[0].Parent(); p != nil {
		return p, true
	}
	return ex.view.Workspace().Root(), true
}

// newInExplorer shows the Explorer and opens the inline input of a new file or folder in the folder of the context.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (openExplorerAndCreate)
func (s *shellState) newInExplorer(binding *widgets.WidgetsBinding, folder bool) {
	win := PeekWindow(s.Context())
	if s.explorer.view == nil {
		win.Process.Notify.Info("Emetos", "You have not yet opened a folder.")
		return
	}
	s.showExplorer(binding, false)
	target, ok := s.targetFolder()
	if !ok {
		return
	}
	s.explorer.view.StartNew(target.Path(), folder)
}

// renameInExplorer opens the inline input on the focused item.
func (s *shellState) renameInExplorer() {
	ex := &s.explorer
	if ex.view == nil {
		return
	}
	if ctx := ex.view.Context(false); len(ctx) > 0 && !ctx[0].IsRoot() {
		ex.view.StartRename(ctx[0].Path())
	}
}

// editCommitted is what the user typed into the inline input: the item is created or renamed.
func (s *shellState) editCommitted(binding *widgets.WidgetsBinding, r explorer.EditResult) {
	win := PeekWindow(s.Context())
	ops := win.FileOps
	switch r.Kind {
	case explorer.EditNewFile, explorer.EditNewFolder:
		isFolder := r.Kind == explorer.EditNewFolder
		var attempt func()
		attempt = func() {
			s.runFileOp(binding, func(ctx context.Context) (workspace.OpResult, error) {
				if isFolder {
					return ops.CreateFolder(ctx, r.Parent, r.Name)
				}
				return ops.CreateFile(ctx, r.Parent, r.Name)
			}, func(o opsRun) {
				if o.err != nil {
					s.reportOpError(o.err, attempt)
					return
				}
				target := o.res.Targets[0]
				if strings.HasSuffix(r.Name, "/") {
					isFolder = true
				}
				if isFolder {
					s.selectInExplorer(target, true)
					return
				}
				s.selectInExplorer(target, false)
				s.openPath(binding, nil, target, false)
			})
		}
		attempt()
	case explorer.EditRename:
		s.runFileOp(binding, func(ctx context.Context) (workspace.OpResult, error) {
			return ops.Rename(ctx, r.Path, r.Name)
		}, func(o opsRun) {
			if o.err != nil {
				s.reportOpError(o.err, nil)
				return
			}
			if len(o.res.Moves) > 0 {
				s.selectInExplorer(o.res.Moves[0].New, true)
			}
		})
	}
}

// selectInExplorer selects a path in the tree once the tree shows it; the tree keeps the keyboard focus when it had it.
func (s *shellState) selectInExplorer(path string, focus bool) {
	ex := &s.explorer
	if ex.view == nil {
		return
	}
	ex.view.Reveal(path, explorer.RevealSelect, func(found bool) {
		if found && focus {
			ex.view.Focus()
		}
	})
}

// ---- delete ----

// itemsUnder lists the paths that are the items of a context without the ones inside another of them.
func distinctPaths(items []*workspace.Item) []string {
	var paths []string
	for _, it := range items {
		paths = append(paths, it.Path())
	}
	slices.Sort(paths)
	var out []string
	for _, p := range paths {
		if len(out) > 0 && strings.HasPrefix(p, out[len(out)-1]+string(filepath.Separator)) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// deleteInExplorer deletes the items of the context: to the trash, or for good when useTrash is false or files.enableTrash is off.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (deleteFiles)
func (s *shellState) deleteInExplorer(binding *widgets.WidgetsBinding, useTrash bool) {
	ex := &s.explorer
	if ex.view == nil {
		return
	}
	var items []*workspace.Item
	for _, it := range ex.view.Context(true) {
		if !it.IsRoot() {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return
	}
	win := PeekWindow(s.Context())
	if !settings.Get[bool](win.Settings, "files.enableTrash", "") {
		useTrash = false
	}
	s.deleteItems(binding, items, useTrash, false)
}

// deleteItems asks what has to be asked and deletes.
func (s *shellState) deleteItems(binding *widgets.WidgetsBinding, items []*workspace.Item, useTrash, skipConfirm bool) {
	win := PeekWindow(s.Context())
	paths := distinctPaths(items)
	byPath := map[string]*workspace.Item{}
	for _, it := range items {
		byPath[it.Path()] = it
	}
	var distinct []*workspace.Item
	for _, p := range paths {
		distinct = append(distinct, byPath[p])
	}
	primary := "Delete"
	if useTrash {
		primary = "Move to Trash"
	}

	// documents with unsaved changes below the items
	dirty := 0
	for _, doc := range win.Documents.All() {
		if !doc.Dirty() || doc.URI().Scheme != document.SchemeFile {
			continue
		}
		for _, p := range paths {
			if doc.URI().Path == p || strings.HasPrefix(doc.URI().Path, p+string(filepath.Separator)) {
				dirty++
				break
			}
		}
	}
	proceed := func(skip bool) {
		s.deleteConfirmed(binding, distinct, paths, useTrash, skip)
	}
	if dirty > 0 {
		var msg string
		switch {
		case len(distinct) > 1:
			msg = "You are deleting files with unsaved changes. Do you want to continue?"
		case distinct[0].IsDir() && dirty == 1:
			msg = "You are deleting a folder " + distinct[0].Name() + " with unsaved changes in 1 file. Do you want to continue?"
		case distinct[0].IsDir():
			msg = "You are deleting a folder " + distinct[0].Name() + " with unsaved changes in " + strconv.Itoa(dirty) + " files. Do you want to continue?"
		default:
			msg = "You are deleting " + distinct[0].Name() + " with unsaved changes. Do you want to continue?"
		}
		s.confirm(confirmSpec{Type: kit.DialogWarning, Message: msg, Detail: "Your changes will be lost if you don't save them.", Primary: primary},
			func(ok, _ bool) {
				if ok {
					proceed(true)
				}
			})
		return
	}
	proceed(skipConfirm)
}

func hasFolder(items []*workspace.Item) (anyDir, anyFile bool) {
	for _, it := range items {
		if it.IsDir() {
			anyDir = true
		} else {
			anyFile = true
		}
	}
	return anyDir, anyFile
}

// deleteMessage is the question of a delete: the title and the list of names.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (getMoveToTrashMessage, getDeleteMessage)
func deleteMessage(items []*workspace.Item, permanently bool) (message, detail string) {
	verb := "delete"
	if permanently {
		verb = "permanently delete"
	}
	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.Path()
	}
	anyDir, anyFile := hasFolder(items)
	n := strconv.Itoa(len(items))
	switch {
	case anyDir && anyFile:
		return "Are you sure you want to " + verb + " the following " + n + " files/directories and their contents?", fileNamesMessage(paths)
	case len(items) > 1 && anyDir:
		return "Are you sure you want to " + verb + " the following " + n + " directories and their contents?", fileNamesMessage(paths)
	case len(items) > 1:
		return "Are you sure you want to " + verb + " the following " + n + " files?", fileNamesMessage(paths)
	case anyDir && !items[0].IsSymlink():
		return "Are you sure you want to " + verb + " '" + items[0].Name() + "' and its contents?", ""
	}
	return "Are you sure you want to " + verb + " '" + items[0].Name() + "'?", ""
}

// deleteConfirmed asks to confirm (explorer.confirmDelete) and deletes.
func (s *shellState) deleteConfirmed(binding *widgets.WidgetsBinding, items []*workspace.Item, paths []string, useTrash, skipConfirm bool) {
	win := PeekWindow(s.Context())
	// folders cannot be restored after a permanent delete, and nothing can: it is not undoable here
	irreversible := "This action is irreversible!"
	if skipConfirm || !settings.Get[bool](win.Settings, "explorer.confirmDelete", "") {
		s.doDelete(binding, items, paths, useTrash)
		return
	}
	if useTrash {
		msg, detail := deleteMessage(items, false)
		if detail != "" {
			detail += "\n"
		}
		if len(items) > 1 {
			detail += "You can restore these files from the Trash."
		} else {
			detail += "You can restore this file from the Trash."
		}
		s.confirm(confirmSpec{Message: msg, Detail: detail, Primary: "Move to Trash", Checkbox: "Do not ask me again"},
			func(ok, checked bool) {
				if checked && ok {
					s.updateSetting("explorer.confirmDelete", false)
				}
				if ok {
					s.doDelete(binding, items, paths, true)
				}
			})
		return
	}
	msg, detail := deleteMessage(items, true)
	if detail != "" {
		detail += "\n"
	}
	detail += irreversible
	s.confirm(confirmSpec{Type: kit.DialogWarning, Message: msg, Detail: detail, Primary: "Delete"},
		func(ok, _ bool) {
			if ok {
				s.doDelete(binding, items, paths, false)
			}
		})
}

// doDelete deletes the paths. A trash that cannot take them asks whether to delete for good.
func (s *shellState) doDelete(binding *widgets.WidgetsBinding, items []*workspace.Item, paths []string, useTrash bool) {
	win := PeekWindow(s.Context())
	ops := win.FileOps
	s.runFileOp(binding, func(ctx context.Context) (workspace.OpResult, error) {
		if useTrash {
			return ops.Trash(ctx, paths)
		}
		return ops.Delete(ctx, paths)
	}, func(o opsRun) {
		if o.err == nil {
			return
		}
		win.Process.Logger.Warn("delete failed", "err", o.err)
		if useTrash {
			s.confirm(confirmSpec{
				Type: kit.DialogWarning, Message: "Failed to delete using the Trash. Do you want to permanently delete instead?",
				Detail: "This action is irreversible!", Primary: "Delete Permanently",
			}, func(ok, _ bool) {
				if ok {
					s.doDelete(binding, items, paths, false)
				}
			})
			return
		}
		s.reportOpError(o.err, func() { s.doDelete(binding, items, paths, false) })
	})
}

// ---- cut, copy, paste ----

// fileURIs are the file:// URIs of paths, for the clipboard.
func fileURIs(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = (&url.URL{Scheme: "file", Path: p}).String()
	}
	return out
}

// copyInExplorer remembers the items of the context as copied or cut, shows the cut ones dimmed, and puts their files
// on the system clipboard too (text/uri-list, and their paths as text), so a file manager can paste them.
func (s *shellState) copyInExplorer(cut bool) {
	ex := &s.explorer
	win := PeekWindow(s.Context())
	if ex.view == nil {
		return
	}
	var items []*workspace.Item
	for _, it := range ex.view.Context(true) {
		if !it.IsRoot() {
			items = append(items, it)
		}
	}
	paths := distinctPaths(items)
	if len(paths) == 0 {
		return
	}
	ex.clip = fileClip{paths: paths, cut: cut}
	if cut {
		ex.view.SetCut(paths)
	} else {
		ex.view.SetCut(nil)
	}
	win.Keys.Set("fileCopied", true)
	binding := widgets.WidgetsBindingOf(s.Context())
	data := services.ClipboardData{Text: strings.Join(paths, "\n"), URIs: fileURIs(paths)}
	binding.Clipboard().SetData(win.ctx, data, func(err error) {
		if err != nil {
			win.Process.Logger.Warn("cannot copy the files to the clipboard", "err", err)
		}
	})
}

// clearFileClip forgets a cut (Escape).
func (s *shellState) clearFileClip() {
	ex := &s.explorer
	if ex.view != nil {
		ex.view.SetCut(nil)
	}
	if ex.clip.cut {
		ex.clip = fileClip{}
		PeekWindow(s.Context()).Keys.Set("fileCopied", false)
	}
}

// readClipboardFiles reads the files on the system clipboard; the function is called on the UI thread with none when the
// clipboard has no files.
func (s *shellState) readClipboardFiles(binding *widgets.WidgetsBinding, done func(paths []string)) {
	win := PeekWindow(s.Context())
	binding.Clipboard().GetData(win.ctx, services.ClipboardKURIList, func(data *services.ClipboardData, err error) {
		var paths []string
		if err == nil && data != nil {
			for _, raw := range data.URIs {
				if u, perr := url.Parse(raw); perr == nil && u.Scheme == "file" && (u.Host == "" || u.Host == "localhost") && u.Path != "" {
					paths = append(paths, filepath.Clean(u.Path))
				}
			}
		}
		done(paths)
	})
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// pasteInExplorer pastes the files on the clipboard into the folder of the context: the files the Explorer cut are moved, the
// others are copied (the files of a file manager too).
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (pasteFileHandler)
func (s *shellState) pasteInExplorer(binding *widgets.WidgetsBinding) {
	ex := &s.explorer
	if ex.view == nil || ex.view.Editing() {
		return
	}
	s.readClipboardFiles(binding, func(system []string) {
		if !s.Mounted() || ex.view == nil {
			return
		}
		var paths []string
		cut, native := false, false
		switch {
		case len(system) > 0 && len(ex.clip.paths) > 0 && sameSet(system, ex.clip.paths):
			paths, cut = ex.clip.paths, ex.clip.cut
		case len(system) > 0:
			paths, native = system, true
		default:
			paths, cut = ex.clip.paths, ex.clip.cut
		}
		if len(paths) == 0 {
			return
		}
		target, ok := s.targetFolder()
		if !ok {
			return
		}
		do := func() { s.pasteInto(binding, paths, target.Path(), cut) }
		win := PeekWindow(s.Context())
		if native && settings.Get[bool](win.Settings, "explorer.confirmPasteNative", "") {
			msg := "Are you sure you want to paste '" + filepath.Base(paths[0]) + "'?"
			detail := ""
			if len(paths) > 1 {
				msg = "Are you sure you want to paste the following " + strconv.Itoa(len(paths)) + " items?"
				detail = fileNamesMessage(paths)
			}
			s.confirm(confirmSpec{Message: msg, Detail: detail, Primary: "Paste", Checkbox: "Do not ask me again"},
				func(ok, checked bool) {
					if checked && ok {
						s.updateSetting("explorer.confirmPasteNative", false)
					}
					if ok {
						do()
					}
				})
			return
		}
		do()
	})
}

// naming is explorer.incrementalNaming.
func (s *shellState) naming() workspace.IncrementalNaming {
	win := PeekWindow(s.Context())
	return workspace.IncrementalNaming(settings.Get[string](win.Settings, "explorer.incrementalNaming", ""))
}

// overwriteConfirm is the question asked before an item replaces another of the same name.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileImportExport.ts (getMultipleFilesOverwriteConfirm)
func overwriteConfirm(conflicts []string) confirmSpec {
	if len(conflicts) > 1 {
		return confirmSpec{
			Type:    kit.DialogWarning,
			Message: "The following " + strconv.Itoa(len(conflicts)) + " files and/or folders already exist in the destination folder. Do you want to replace them?",
			Detail:  fileNamesMessage(conflicts) + "\nThis action is irreversible!", Primary: "Replace",
		}
	}
	return confirmSpec{
		Type:    kit.DialogWarning,
		Message: "A file or folder with the name '" + filepath.Base(conflicts[0]) + "' already exists in the destination folder. Do you want to replace it?",
		Detail:  "This action is irreversible!", Primary: "Replace",
	}
}

// pasteInto moves or copies paths into a folder, asking before it replaces anything. A cut ends with the paste.
func (s *shellState) pasteInto(binding *widgets.WidgetsBinding, paths []string, folder string, move bool) {
	win := PeekWindow(s.Context())
	ops := win.FileOps
	// the folder of the paste must not be one of the items, or below one of them
	for _, p := range paths {
		if folder == p || strings.HasPrefix(folder, p+string(filepath.Separator)) {
			if move || folder != p {
				win.Process.Notify.Error("Emetos", "File to paste is an ancestor of the destination folder")
				return
			}
		}
	}
	naming := s.naming()
	finish := func(o opsRun) {
		if move && o.err == nil {
			s.clearFileClip()
		}
		if o.err != nil {
			s.reportOpError(o.err, nil)
			return
		}
		s.showResults(binding, o.res)
	}
	if move {
		// an item that is in the folder already stays
		var srcs []string
		for _, p := range paths {
			if filepath.Dir(p) != folder {
				srcs = append(srcs, p)
			}
		}
		if len(srcs) == 0 {
			s.clearFileClip()
			return
		}
		s.moveWithConfirm(binding, srcs, folder, finish)
		return
	}
	if naming == workspace.NamingDisabled {
		if conflicts := ops.Conflicts(folder, paths); len(conflicts) > 0 {
			s.confirm(overwriteConfirm(conflicts), func(ok, _ bool) {
				if ok {
					s.runFileOp(binding, func(ctx context.Context) (workspace.OpResult, error) {
						return ops.Copy(ctx, folder, paths, workspace.CopyOptions{Naming: naming, Overwrite: true})
					}, finish)
				}
			})
			return
		}
	}
	s.runFileOp(binding, func(ctx context.Context) (workspace.OpResult, error) {
		return ops.Copy(ctx, folder, paths, workspace.CopyOptions{Naming: naming})
	}, finish)
}

// moveWithConfirm moves paths into a folder, asking before it replaces an item of the same name.
func (s *shellState) moveWithConfirm(binding *widgets.WidgetsBinding, srcs []string, folder string, done func(opsRun)) {
	ops := PeekWindow(s.Context()).FileOps
	move := func(overwrite bool) {
		s.runFileOp(binding, func(ctx context.Context) (workspace.OpResult, error) {
			return ops.Move(ctx, folder, srcs, workspace.MoveOptions{Overwrite: overwrite})
		}, done)
	}
	if conflicts := ops.Conflicts(folder, srcs); len(conflicts) > 0 {
		s.confirm(overwriteConfirm(conflicts), func(ok, _ bool) {
			if ok {
				move(true)
			}
		})
		return
	}
	move(false)
}

// showResults selects the first item an operation made in the tree, and opens it when it is the only one and a file.
func (s *shellState) showResults(binding *widgets.WidgetsBinding, res workspace.OpResult) {
	if len(res.Targets) == 0 {
		return
	}
	first := res.Targets[0]
	s.selectInExplorer(first, false)
	if len(res.Targets) == 1 && !isDir(first) {
		s.openPath(binding, nil, first, false)
	}
}

// ---- drag and drop ----

// dropItems moves or copies items of the tree that were dropped on a folder of the tree.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerViewer.ts (handleExplorerDrop)
func (s *shellState) dropItems(binding *widgets.WidgetsBinding, sources []string, folder string, copying bool) {
	win := PeekWindow(s.Context())
	ops := win.FileOps
	finish := func(o opsRun) {
		if o.err != nil {
			s.reportOpError(o.err, nil)
			return
		}
		if copying {
			// the copies of files open in editors
			for _, p := range o.res.Targets {
				if !isDir(p) {
					s.openPath(binding, nil, p, false)
				}
			}
			return
		}
		s.showResults(binding, o.res)
	}
	act := func() {
		if copying {
			naming := s.naming()
			run := func(overwrite bool) {
				s.runFileOp(binding, func(ctx context.Context) (workspace.OpResult, error) {
					return ops.Copy(ctx, folder, sources, workspace.CopyOptions{Naming: naming, Overwrite: overwrite})
				}, finish)
			}
			if naming == workspace.NamingDisabled {
				if conflicts := ops.Conflicts(folder, sources); len(conflicts) > 0 {
					s.confirm(overwriteConfirm(conflicts), func(ok, _ bool) {
						if ok {
							run(true)
						}
					})
					return
				}
			}
			run(false)
			return
		}
		s.moveWithConfirm(binding, sources, folder, finish)
	}
	if copying || !settings.Get[bool](win.Settings, "explorer.confirmDragAndDrop", "") {
		act()
		return
	}
	target := filepath.Base(folder)
	msg := "Are you sure you want to move '" + filepath.Base(sources[0]) + "' into '" + target + "'?"
	detail := ""
	if len(sources) > 1 {
		msg = "Are you sure you want to move the following " + strconv.Itoa(len(sources)) + " files into '" + target + "'?"
		detail = fileNamesMessage(sources)
	}
	s.confirm(confirmSpec{Message: msg, Detail: detail, Primary: "Move", Checkbox: "Do not ask me again"},
		func(ok, checked bool) {
			if checked && ok {
				s.updateSetting("explorer.confirmDragAndDrop", false)
			}
			if ok {
				act()
			}
		})
}

// importFiles copies files that were dropped from another application into a folder of the tree; a file whose name is
// taken is replaced after the user agreed.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileImportExport.ts (ExternalFileImport.importResources)
func (s *shellState) importFiles(binding *widgets.WidgetsBinding, paths []string, folder string) {
	win := PeekWindow(s.Context())
	ops := win.FileOps
	var existing []string
	missing := 0
	for _, p := range paths {
		if exists(p) {
			existing = append(existing, p)
		} else {
			missing++
		}
	}
	if missing > 0 {
		msg := "The dropped file could not be accessed for import."
		if missing > 1 {
			msg = "Some or all of the dropped files could not be accessed for import."
		}
		win.Process.Notify.Error("Emetos", msg)
	}
	run := func(srcs []string) {
		if len(srcs) == 0 {
			return
		}
		s.runFileOp(binding, func(ctx context.Context) (workspace.OpResult, error) {
			return ops.Copy(ctx, folder, srcs, workspace.CopyOptions{Naming: workspace.NamingDisabled, Overwrite: true})
		}, func(o opsRun) {
			if o.err != nil {
				s.reportOpError(o.err, nil)
				return
			}
			if len(o.res.Targets) == 0 {
				return
			}
			s.selectInExplorer(o.res.Targets[0], false)
			if settings.Get[bool](win.Settings, "explorer.autoOpenDroppedFile", "") && len(o.res.Targets) == 1 && !isDir(o.res.Targets[0]) {
				s.openPath(binding, nil, o.res.Targets[0], false)
			}
		})
	}
	conflicts := ops.Conflicts(folder, existing)
	if len(conflicts) == 0 {
		run(existing)
		return
	}
	s.confirm(overwriteConfirm(conflicts), func(ok, _ bool) {
		if ok {
			run(existing)
			return
		}
		// the files that would replace others are left out
		var rest []string
		for _, p := range existing {
			if !slices.Contains(conflicts, p) {
				rest = append(rest, p)
			}
		}
		run(rest)
	})
}

// ---- undo and redo ----

// undoInExplorer takes back (or does again) the last file operation. Taking back what removes files asks first, as
// explorer.confirmUndo says.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/files.contribution.ts (UndoCommand, RedoCommand
// implementations) and src/vs/platform/undoRedo/common/undoRedoService.ts (confirmBeforeUndo)
func (s *shellState) undoInExplorer(binding *widgets.WidgetsBinding, redo bool) {
	win := PeekWindow(s.Context())
	ops := win.FileOps
	if !settings.Get[bool](win.Settings, "explorer.enableUndo", "") {
		return
	}
	run := func() {
		s.runFileOp(binding, func(ctx context.Context) (workspace.OpResult, error) {
			if redo {
				return ops.Redo(ctx)
			}
			return ops.Undo(ctx)
		}, func(o opsRun) {
			if o.err != nil {
				s.reportOpError(o.err, nil)
				return
			}
			if len(o.res.Targets) > 0 {
				s.selectInExplorer(o.res.Targets[0], false)
			}
		})
	}
	if redo {
		if ops.CanRedo() {
			run()
		}
		return
	}
	label, prompt, ok := ops.NextUndo()
	if !ok {
		return
	}
	level := settings.Get[string](win.Settings, "explorer.confirmUndo", "")
	ask := prompt == workspace.PromptAlways ||
		(prompt == workspace.PromptDefault && (level == "default" || level == "verbose")) ||
		(prompt == workspace.PromptVerbose && level == "verbose")
	if !ask {
		run()
		return
	}
	s.confirm(confirmSpec{Message: "Would you like to undo '" + label + "'?", Primary: "Yes"}, func(ok, _ bool) {
		if ok {
			run()
		}
	})
}

// ---- open to the side, reveal in the file manager ----

// commandPaths are the files a command works on: the paths in its argument (a path, a list of paths, or the
// context of a tab), else the items of the Explorer when the folder view has the keyboard focus, else the file of the
// active editor.
func (s *shellState) commandPaths(args any) []string {
	switch v := args.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []string:
		return v
	case []any:
		var out []string
		for _, x := range v {
			if p, ok := x.(string); ok {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	case map[string]any:
		if _, e := s.commandTarget(args); e != nil {
			if p, ok := fileResource(e); ok {
				return []string{p}
			}
		}
		return nil
	}
	ex := &s.explorer
	if ex.view != nil && ex.focus[command.ViewExplorerFolders] {
		if items := ex.view.Context(true); len(items) > 0 {
			var out []string
			for _, it := range items {
				out = append(out, it.Path())
			}
			return out
		}
	}
	if e := s.activeGroup().Model.ActiveEditor(); e != nil {
		if p, ok := fileResource(e); ok {
			return []string{p}
		}
	}
	return nil
}

// openToSide opens the files of the Explorer in the group on the side of the active group (the side
// workbench.editor.openSideBySideDirection says), making the group when there is none.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileCommands.ts (OPEN_TO_SIDE_COMMAND_ID)
func (s *shellState) openToSide(binding *widgets.WidgetsBinding, args any) {
	ex := &s.explorer
	var paths []string
	if ex.view != nil {
		for _, it := range ex.view.Context(true) {
			if !it.IsDir() {
				paths = append(paths, it.Path())
			}
		}
	}
	if len(paths) == 0 {
		for _, p := range s.commandPaths(args) {
			if !isDir(p) {
				paths = append(paths, p)
			}
		}
	}
	if len(paths) == 0 {
		return
	}
	side := s.sideGroup()
	if side == nil {
		return
	}
	for _, p := range paths {
		s.openPath(binding, side, p, false)
	}
}

// sideGroup is the group on the side of the active group (the side workbench.editor.openSideBySideDirection
// says), made when there is none; nil when it cannot be made.
func (s *shellState) sideGroup() *editorgroup.Group {
	dir := s.sideBySideDirection()
	grid := s.group.grid
	side := grid.FindByDirection(s.activeGroup(), dir, false)
	if side == nil {
		var err error
		if side, err = grid.AddGroup(s.activeGroup(), dir); err != nil {
			PeekWindow(s.Context()).Process.Logger.Warn("cannot make the group for open to the side", "err", err)
			return nil
		}
	}
	return side
}

// revealInOS shows the folder of a file in the file manager of the desktop (an OpenURI portal or xdg-open, on a
// goroutine).
//
// Ported from VS Code: src/vs/workbench/contrib/files/electron-browser/fileActions.contribution.ts
func (s *shellState) revealInOS(binding *widgets.WidgetsBinding, args any) {
	win := PeekWindow(s.Context())
	paths := s.commandPaths(args)
	if len(paths) == 0 {
		return
	}
	bc := s.Context()
	open := win.OpenURI
	if open == nil {
		open = desktop.OpenURI
	}
	var folders []string
	for _, p := range paths {
		if dir := filepath.Dir(p); !slices.Contains(folders, dir) {
			folders = append(folders, dir)
		}
	}
	s.Go(func(life context.Context) {
		for _, dir := range folders {
			if err := open(life, bc, (&url.URL{Scheme: "file", Path: dir}).String()); err != nil {
				binding.Post(func() {
					win.Process.Logger.Warn("cannot show the folder in the file manager", "path", dir, "err", err)
					win.Process.Notify.Error("Emetos", "Cannot show the folder in the file manager: "+describe(err))
				})
				return
			}
		}
	})
}

// ---- copy path ----

// pathSeparator is the separator copied paths use (explorer.copyPathSeparator, explorer.copyRelativePathSeparator):
// "auto" is the slash of Linux.
func (s *shellState) pathSeparator(relative bool) string {
	win := PeekWindow(s.Context())
	key := "explorer.copyPathSeparator"
	if relative {
		key = "explorer.copyRelativePathSeparator"
	}
	if sep := settings.Get[string](win.Settings, key, ""); sep == "\\" {
		return "\\"
	}
	return "/"
}

// copyPaths puts the paths of files on the clipboard, one per line, relative to the folder of the window when asked.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileCommands.ts (resourcesToClipboard)
func (s *shellState) copyPaths(paths []string, relative bool) {
	if len(paths) == 0 {
		return
	}
	win := PeekWindow(s.Context())
	sep := s.pathSeparator(relative)
	lines := make([]string, len(paths))
	for i, p := range paths {
		text := pathText(p, win.Folder, relative)
		if sep != "/" {
			text = strings.ReplaceAll(text, "/", sep)
		}
		lines[i] = text
	}
	s.copyText(strings.Join(lines, "\n"))
}

// ---- the context menu ----

// explorerMenuContext is the context the context menu of the Explorer is evaluated in: the keys of the window with
// those of the item that was pressed (the folder of the window for the empty part of the tree).
func (s *shellState) explorerMenuContext(path string) contextkey.Context {
	win := PeekWindow(s.Context())
	ex := &s.explorer
	isRoot := path == ""
	folder := isRoot || isDir(path)
	trash := settings.Get[bool](win.Settings, "files.enableTrash", "")
	extra := map[string]any{
		"explorerResourceIsFolder":        folder,
		"explorerResourceIsRoot":          isRoot,
		"explorerResourceReadonly":        false,
		"explorerResourceMoveableToTrash": !isRoot && trash,
		"explorerResourceCut":             ex.view != nil && len(ex.view.CutPaths()) > 0,
		"fileCopied":                      len(ex.clip.paths) > 0,
		"resourceScheme":                  "file",
		"isFileSystemResource":            true,
	}
	return menuContext{base: win.Keys, settings: win.Settings, extra: extra}
}

// explorerMenuItems are the rows of the context menu of the Explorer for a row (the empty part of the tree when path is
// empty).
func (s *shellState) explorerMenuItems(path string) []kit.MenuItem {
	items := s.menus.Items(command.ExplorerContext, s.explorerMenuContext(path))
	return items
}

// openExplorerMenu shows the context menu of the Explorer at a position of the window.
func (s *shellState) openExplorerMenu(path string, at ui.Offset) {
	s.explorerMenu.Show(at, s.explorerMenuItems(path))
}

// exists tells whether a path is there (a link counts, whatever it points at).
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// isDir tells whether a path is a folder, or a link to one.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
