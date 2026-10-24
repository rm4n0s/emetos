package app

import (
	"context"
	"strconv"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Closing with unsaved changes (SPEC §10.3, §10.10, §10.11). Every way to close editors (the close button
// of a tab, the close commands, the Open Editors view, the editor picker, Close Folder and Open Folder) goes
// through closeConfirmed, and closing the window goes through Window.RequestClose; both ask with the same
// dialog (askUnsaved).
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorGroupView.ts (handleCloseConfirmation,
// doHandleCloseConfirmation) and src/vs/platform/dialogs (showSaveConfirm in
// src/vs/workbench/services/dialogs/browser/abstractFileDialogService.ts)

// CloseReason says why a window is asked to close.
type CloseReason int

const (
	// CloseReasonWindow is the user closing the window (its close button, Close Window).
	CloseReasonWindow CloseReason = iota
	// CloseReasonQuit is the user quitting the application (Quit, Exit).
	CloseReasonQuit
)

// closeAction is what closing a window with unsaved changes does about them.
type closeAction int

const (
	// closeAsk asks about the unsaved changes with the dialog of §10.3 (Save, Don't Save, Cancel).
	closeAsk closeAction = iota
	// closeBackup backs the unsaved documents up, without asking, and closes: they come back at the next start.
	closeBackup
)

// closeDecision tells what closing a window with unsaved changes does, from files.hotExit (SPEC §10.10):
//
//	off                    ask, always
//	onExit                 back up when the application quits or the window is the last one; ask when another window
//	                       closes while others stay
//	onExitAndWindowClose   as onExit, and back up as well when a window that has a folder closes while others stay
//
// A value the setting does not have asks. hasFolder is whether the window has a folder open, otherWindows whether
// other windows are open.
//
// Ported from VS Code: src/vs/workbench/services/workingCopy/electron-sandbox/workingCopyBackupTracker.ts
// (onWillShutdown, shouldBackup) and the description of the setting files.hotExit
func closeDecision(hotExit string, reason CloseReason, hasFolder, otherWindows bool) closeAction {
	lastOrQuit := reason == CloseReasonQuit || !otherWindows
	switch hotExit {
	case "onExit":
		if lastOrQuit {
			return closeBackup
		}
	case "onExitAndWindowClose":
		if lastOrQuit || hasFolder {
			return closeBackup
		}
	}
	return closeAsk
}

// unsavedDetail is the detail of the dialog that asks whether to save.
const unsavedDetail = "Your changes will be lost if you don't save them."

// The buttons of the dialog that asks whether to save, in the order of the spec (the first is the default).
const (
	buttonSave = iota
	buttonDontSave
	buttonCancel
)

// saveConfirmation is the text of the dialog that asks whether to save the changes of the files named: the
// message, the detail and the label of the first button. One file is named in the message; several are listed
// in the detail, ten at the most.
//
// Ported from VS Code: src/vs/workbench/services/dialogs/browser/abstractFileDialogService.ts
// (doShowSaveConfirm) and src/vs/platform/dialogs/common/dialogs.ts (getFileNamesMessage)
func saveConfirmation(names []string) (message, detail, save string) {
	if len(names) == 1 {
		return "Do you want to save the changes you made to " + names[0] + "?", unsavedDetail, "Save"
	}
	return "Do you want to save the changes to the following " + strconv.Itoa(len(names)) + " files?",
		fileNamesMessage(names) + "\n" + unsavedDetail, "Save All"
}

// closeState is what the shell keeps of closing.
type closeState struct {
	// busy is true from the moment a dialog asks about unsaved changes until the answer is carried out (the
	// saves included): a request to close anything that would ask again is ignored meanwhile.
	busy bool
	// removeCommands take the handlers of the window commands off the registry.
	removeCommands []func()
}

// initClosing makes the window ask about unsaved changes before it closes: the platform's request to close a
// window reaches the manager of the windows (Windows.bind), which asks the window, and Close Window and Quit are
// commands.
func (s *shellState) initClosing(binding *widgets.WidgetsBinding) {
	win := PeekWindow(s.Context())
	c := &s.closing
	win.closer = s.requestWindowClose
	win.exit = func() { binding.ExitApplication(ui.AppExitTypeRequired) }
	win.Process.Windows.bind(binding, nil)
	for id, reason := range map[string]CloseReason{"workbench.action.closeWindow": CloseReasonWindow, "workbench.action.quit": CloseReasonQuit} {
		remove, err := win.Commands.SetHandler(id, func(command.Context, any) error {
			win.RequestClose(reason)
			return nil
		})
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		c.removeCommands = append(c.removeCommands, remove)
	}
}

// disposeClosing stops listening and gives the commands back.
func (s *shellState) disposeClosing() {
	c := &s.closing
	if win := s.win; win != nil {
		win.closer, win.exit = nil, nil
	}
	for _, remove := range c.removeCommands {
		remove()
	}
	c.removeCommands = nil
}

// requestWindowClose is the shell's part of closing a window (Window.closeAsk): it asks the questions of closing the
// window, with the dialog of §10.3 when documents would lose changes, and tells done how they were answered. It
// closes nothing. When no document would lose changes, or files.hotExit leaves them to the backups (they are
// written first), done is told at once; the same dialog asks when the backups cannot be written. Save saves them (an
// untitled document through Save As); Don't Save and a window with nothing to lose proceed; Cancel, or a save that
// fails or is cancelled, does not. A request that comes while the dialog is open is not carried out (done gets
// false).
func (s *shellState) requestWindowClose(reason CloseReason, done func(closeResult)) {
	win := PeekWindow(s.Context())
	if s.closing.busy {
		done(closeResult{}) // the dialog is open already
		return
	}
	// with files.hotExit off nothing stays for the next start: whatever closes takes its backups with it
	off := !hotExitEnabled(win.hotExitMode())
	targets := editorgroup.AllTargets(s.group.grid)
	losses := editorgroup.PlanClose(s.group.grid, targets)
	if len(losses) == 0 {
		done(closeResult{proceed: true, discard: off})
		return
	}
	// the documents that auto save saves go first; the others are backed up (hot exit) or asked about
	s.autoSaveBeforeClose(losses, true, func() {
		losses := editorgroup.PlanClose(s.group.grid, editorgroup.AllTargets(s.group.grid))
		if len(losses) == 0 {
			done(closeResult{proceed: true, discard: off})
			return
		}
		ask := func() {
			s.raise()
			s.askUnsaved(losses, func(proceed bool) {
				// saved or given up: nothing is left to restore
				done(closeResult{proceed: proceed, discard: true})
			})
		}
		if closeDecision(win.hotExitMode(), reason, win.Folder != "", win.otherWindows()) == closeAsk {
			ask()
			return
		}
		s.backupBeforeClose(func(ok bool) {
			if !ok {
				ask() // the unsaved changes could not be kept: the user decides
				return
			}
			done(closeResult{proceed: true})
		})
	})
}

// raise brings the window to the front, for the dialog that asks in it.
func (s *shellState) raise() {
	if win := s.win; win != nil {
		win.Raise()
	}
}

// backupBeforeClose backs up every document with unsaved changes (hot exit) and tells done whether all of them
// were: the texts are read here, on the UI thread, and written on a goroutine, so the window does not wait for the
// disk; done is called on the UI thread. While it goes on, a request to close is ignored. A failure is logged.
func (s *shellState) backupBeforeClose(done func(ok bool)) {
	win := PeekWindow(s.Context())
	tracker := win.Backups()
	if tracker == nil {
		done(false) // the window keeps no backups
		return
	}
	s.closing.busy = true
	batch := tracker.Prepare(win.Documents.All())
	binding := s.binding
	s.Go(func(context.Context) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(win.ctx), backupExitTimeout)
		defer cancel()
		err := batch.Write(ctx)
		binding.Post(func() {
			s.closing.busy = false
			if err != nil {
				win.Process.Logger.Warn("the unsaved documents cannot be backed up; the user is asked", "err", err)
			}
			if s.Mounted() {
				done(err == nil)
			}
		})
	})
}

// closeEditors closes editors of a group, asking first about the changes that would be lost.
func (s *shellState) closeEditors(group *editorgroup.Group, editors []editorgroup.EditorInput) {
	s.closeConfirmed([]editorgroup.CloseTarget{{Group: group, Editors: editors}}, nil)
}

// closeConfirmed closes the editors of the targets. When some of them have changes that no other editor shows,
// and files.autoSave is onFocusChange or onWindowChange, they are saved (a failure is reported, and the document
// is asked about); the changes that are left are asked about: a dialog asks whether to save them (Save), drop them (Don't Save) or keep everything open (Cancel). done, when
// set, is told on the UI thread whether the editors closed; a request that would ask while another dialog about
// closing is open is ignored (done gets false).
func (s *shellState) closeConfirmed(targets []editorgroup.CloseTarget, done func(closed bool)) {
	finish := func(closed bool) {
		if done != nil {
			done(closed)
		}
	}
	targets = s.liveTargets(targets)
	losses := editorgroup.PlanClose(s.group.grid, targets)
	if len(losses) == 0 {
		s.closeNow(targets)
		finish(true)
		return
	}
	// with onFocusChange and onWindowChange the documents are saved instead of asked about; whatever is still
	// dirty after that (a save failed, or auto save does not apply) is asked about as before
	s.autoSaveBeforeClose(losses, false, func() {
		targets = s.liveTargets(targets)
		losses = editorgroup.PlanClose(s.group.grid, targets)
		if len(losses) == 0 {
			s.closeNow(targets)
			finish(true)
			return
		}
		s.askUnsaved(losses, func(proceed bool) {
			if proceed {
				s.closeNow(s.liveTargets(targets))
			}
			finish(proceed)
		})
	})
}

// liveTargets are the targets as the groups have them now: the inputs the groups hold (a target may name a
// matching input), only the groups that are still in the grid, and each editor once.
func (s *shellState) liveTargets(targets []editorgroup.CloseTarget) []editorgroup.CloseTarget {
	var out []editorgroup.CloseTarget
	for _, t := range targets {
		if t.Group == nil || s.group.grid.Group(t.Group.ID()) != t.Group {
			continue
		}
		live := editorgroup.CloseTarget{Group: t.Group}
		for _, e := range t.Editors {
			at := t.Group.Model.IndexOf(e)
			if at < 0 {
				continue
			}
			if held := t.Group.Model.EditorAt(at); !containsInput(live.Editors, held) {
				live.Editors = append(live.Editors, held)
			}
		}
		if len(live.Editors) > 0 {
			out = append(out, live)
		}
	}
	return out
}

// closeNow closes the editors of the targets without asking. When the active editor of a group is among its
// closing editors and others stay, one of the others becomes active first, the most recently used (or the
// nearest, with workbench.editor.focusRecentEditorAfterClose off), so that the group does not pass through the
// editors that close. A group left empty closes with workbench.editor.closeEmptyGroups.
func (s *shellState) closeNow(targets []editorgroup.CloseTarget) {
	// the editors that go next to the ones that come to the front are not the user leaving them: no auto save
	s.autosave.hold++
	defer func() {
		s.autosave.hold--
		s.autosave.last = s.activeEditor()
	}()
	for _, t := range targets {
		m := t.Group.Model
		var closing []editorgroup.EditorInput
		for _, e := range t.Editors {
			if m.Contains(e) {
				closing = append(closing, e)
			}
		}
		if active := m.ActiveEditor(); active != nil && containsInput(closing, active) {
			for _, candidate := range survivors(m, closing) {
				m.SetActive(candidate)
				break
			}
		}
		for _, e := range closing {
			m.CloseEditor(e, editorgroup.CloseUnknown, true)
		}
	}
}

// askUnsaved asks whether to save the documents of the editors in losses, in one dialog, and carries out the
// answer. done is told on the UI thread whether to go on with the close: true after Save when every document
// was saved, and after Don't Save (the documents are dropped with their last editor, which is what a revert
// does); false after Cancel, after a save that failed or was cancelled, and when a dialog about closing is open
// already. While the question and the saves are going on, busy is set.
func (s *shellState) askUnsaved(losses []editorgroup.Unsaved, done func(proceed bool)) {
	if s.closing.busy {
		done(false)
		return
	}
	win := PeekWindow(s.Context())
	s.closing.busy = true
	end := func(proceed bool) {
		s.closing.busy = false
		if s.Mounted() {
			done(proceed)
		}
	}
	var names []string
	var docs []*document.Document
	for _, u := range losses {
		names = append(names, u.Input.Name())
		if t, ok := u.Input.(*editorgroup.TextInput); ok {
			docs = append(docs, t.Document())
		}
	}
	message, detail, save := saveConfirmation(names)
	win.Dialogs.Show(kit.DialogSpec{
		Type: kit.DialogWarning, Message: message, Detail: detail,
		Buttons: []string{save, "Don't Save", "Cancel"}, CancelButton: foundation.Some(buttonCancel),
		OnAnswer: func(r kit.DialogResult) {
			switch {
			case r.Cancelled || r.Button == buttonCancel:
				end(false)
			case r.Button == buttonDontSave:
				end(true)
			default:
				s.saveInOrder(docs, end)
			}
		},
	})
}

// saveInOrder saves the documents one after the other, as Save All does: an untitled one through Save As. The
// first that fails to save, or whose Save As is cancelled, ends it with false; so does a document that has
// changed again meanwhile (VS Code vetoes the close of an editor that is still dirty).
func (s *shellState) saveInOrder(docs []*document.Document, done func(saved bool)) {
	var next func(i int)
	next = func(i int) {
		if !s.Mounted() {
			done(false)
			return
		}
		for i < len(docs) && !docs[i].Dirty() {
			i++
		}
		if i >= len(docs) {
			done(true)
			return
		}
		s.saveDocument(docs[i], saveRequest{}, func(saved bool) {
			if !saved || docs[i].Dirty() {
				done(false)
				return
			}
			next(i + 1)
		})
	}
	next(0)
}
