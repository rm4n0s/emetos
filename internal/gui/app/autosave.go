package app

import (
	"slices"
	"time"

	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// Auto save (files.autoSave, SPEC §3.1 "Saving"). The rules are those of document.AutoSave; this file gives it
// what the window knows (the settings, the folder, the markers, the clock of the UI thread), feeds it the events
// of the window (the focus of the editors and of the window, the active editor) and does the saving through the
// same path as Ctrl+S (save.go), silently: an auto save opens no dialog, and a failure is the one notification
// of the document, after which the document is not auto saved until it is saved or read again.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorAutoSave.ts,
// src/vs/workbench/contrib/files/browser/fileActions.ts (ToggleAutoSaveAction) and
// src/vs/workbench/services/filesConfiguration/common/filesConfigurationService.ts (toggleAutoSave)

// autoSaveKeys are the settings that change when and whether a document is saved by itself.
var autoSaveKeys = []string{"files.autoSave", "files.autoSaveDelay", "files.autoSaveWorkspaceFilesOnly", "files.autoSaveWhenNoErrors"}

// autoSaveState is what the shell keeps of auto save.
type autoSaveState struct {
	ctl *document.AutoSave
	// last is the editor that was the active one when the active editor last changed: the one that "lost the
	// focus" when another becomes active.
	last *textEditor
	// hold is above 0 while editors are closed, which activate other editors on the way without any of them
	// having lost the focus.
	hold int
	// observer hears of the window losing the keyboard focus.
	observer *autoSaveObserver
	// stops end the following of the settings and the markers.
	stops []func()
	// badMode is the value of files.autoSave that was reported as unknown last.
	badMode string
}

// autoSaveHost gives the controller what the window knows.
type autoSaveHost struct {
	s   *shellState
	win *Window
}

// Config implements document.AutoSaveHost: the settings of the language of the document. A files.autoSave
// that is none of the four values is logged once and means off.
func (h *autoSaveHost) Config(doc *document.Document) document.AutoSaveConfig {
	lang := doc.LanguageID()
	value := settings.Get[string](h.win.Settings, "files.autoSave", lang)
	mode, ok := document.ParseAutoSaveMode(value)
	if !ok && h.s.autosave.badMode != value {
		h.s.autosave.badMode = value
		h.win.Process.Logger.Warn("unknown value of files.autoSave; auto save is off", "value", value)
	}
	return document.AutoSaveConfig{
		Mode:               mode,
		Delay:              document.AutoSaveDelay(settings.Get[float64](h.win.Settings, "files.autoSaveDelay", lang)),
		WorkspaceFilesOnly: settings.Get[bool](h.win.Settings, "files.autoSaveWorkspaceFilesOnly", lang),
		WhenNoErrors:       settings.Get[bool](h.win.Settings, "files.autoSaveWhenNoErrors", lang),
	}
}

// InWorkspace implements document.AutoSaveHost.
func (h *autoSaveHost) InWorkspace(doc *document.Document) bool {
	uri := doc.URI()
	return uri.Scheme == document.SchemeFile && insideFolder(h.win, uri.Path)
}

// HasErrors implements document.AutoSaveHost: the markers have an error for the file.
func (h *autoSaveHost) HasErrors(doc *document.Document) bool {
	uri := doc.URI()
	if uri.Scheme != document.SchemeFile {
		return false
	}
	return len(h.win.Markers.Read(problems.ReadFilter{Resource: uri.Path, Severities: problems.Severities(problems.SeverityError), Take: 1})) > 0
}

// Save implements document.AutoSaveHost: the save of Ctrl+S with the reason of the auto save. A document whose
// Save As is waiting for its dialog is left alone (the controller is told to try later if the dialog is
// cancelled), and one that cannot be written is not asked for a file.
func (h *autoSaveHost) Save(doc *document.Document, reason document.SaveReason) {
	s := h.s
	if !s.Mounted() || s.saves.asOpen[doc] || doc.ReadOnly() || doc.URI().Scheme != document.SchemeFile {
		return
	}
	h.win.Process.Logger.Debug("auto save", "uri", doc.URI().String(), "reason", reason.String())
	s.saveDocument(doc, saveRequest{reason: reason}, nil)
}

// autoSaveObserver tells the controller when the window loses the keyboard focus.
type autoSaveObserver struct {
	widgets.WidgetsBindingObserverBase
	s *shellState
}

// DidChangeViewFocus implements widgets.WidgetsBindingObserver. With several windows the event says which one lost
// the focus; a window the manager does not host (a test of one window) takes every event.
func (o *autoSaveObserver) DidChangeViewFocus(e ui.ViewFocusEvent) {
	if e.State != ui.ViewFocusStateUnfocused || !o.s.Mounted() {
		return
	}
	if win := o.s.win; win != nil && win.managed() && win.entry.view() != e.ViewID {
		return
	}
	o.s.autosave.ctl.WindowFocusLost()
}

// initAutoSave makes the controller of auto save, before the editors are made (they are tracked as they
// appear), and follows the settings, the markers and the focus of the window.
func (s *shellState) initAutoSave(binding *widgets.WidgetsBinding) {
	win := PeekWindow(s.Context())
	st := &s.autosave
	clock := document.AutoSaveClockFunc(func(d time.Duration, f func()) func() {
		if win.after == nil {
			return func() {}
		}
		return win.after(d, func() {
			if s.Mounted() {
				f()
			}
		})
	})
	st.ctl = document.NewAutoSave(clock, &autoSaveHost{s: s, win: win})
	st.stops = append(st.stops,
		win.Settings.OnChange(func(e settings.ChangeEvent) {
			if slices.ContainsFunc(autoSaveKeys, func(k string) bool { return e.Affects(k, "") }) {
				// the change may come from any goroutine
				binding.Post(func() {
					if s.Mounted() {
						st.ctl.ConfigChanged()
					}
				})
			}
		}),
		win.Markers.Subscribe(func(problems.Change) {
			binding.Post(func() {
				if s.Mounted() {
					st.ctl.MarkersChanged()
				}
			})
		}))
	st.observer = &autoSaveObserver{s: s}
	binding.AddObserver(st.observer)
}

// disposeAutoSave stops following and drops every timer.
func (s *shellState) disposeAutoSave(binding *widgets.WidgetsBinding) {
	st := &s.autosave
	if st.ctl == nil {
		return
	}
	for _, stop := range st.stops {
		stop()
	}
	st.stops = nil
	if st.observer != nil {
		binding.RemoveObserver(st.observer)
		st.observer = nil
	}
	st.ctl.Dispose()
	st.ctl, st.last = nil, nil
}

// autoSaveTrack makes the controller follow the document of a new editor.
func (s *shellState) autoSaveTrack(doc *document.Document) {
	if s.autosave.ctl != nil {
		s.autosave.ctl.Track(doc)
	}
}

// autoSaveUntrack lets the controller go of a document that was closed.
func (s *shellState) autoSaveUntrack(doc *document.Document) {
	if s.autosave.ctl != nil {
		s.autosave.ctl.Untrack(doc)
	}
}

// autoSaveFailed tells the controller that a save failed, so that the document is not tried again by itself.
func (s *shellState) autoSaveFailed(doc *document.Document) {
	if s.autosave.ctl != nil {
		s.autosave.ctl.SaveFailed(doc)
	}
}

// autoSaveRetry asks the controller to try a document again later, after a save that was left alone.
func (s *shellState) autoSaveRetry(doc *document.Document) {
	if s.autosave.ctl != nil && s.Mounted() {
		s.autosave.ctl.Reschedule(doc)
	}
}

// editorBlurred follows an editor losing the keyboard focus (to another editor, the Explorer, the quick input,
// anything): the document is saved with onFocusChange. It is decided after the frame, because an editor that
// goes away (its tab closed, the window closing) loses its focus on the way, which is not the user leaving it:
// an editor that is gone by then, or has the focus again, does not count.
func (s *shellState) editorBlurred(binding *widgets.WidgetsBinding, ed *textEditor) {
	if s.autosave.ctl == nil {
		return
	}
	binding.Post(func() {
		if !s.Mounted() || s.autosave.ctl == nil || ed.disposed || s.group.focused[ed] || s.autosave.hold > 0 {
			return
		}
		s.autosave.ctl.FocusLost(ed.doc)
	})
}

// activeEditorChanged follows the active editor of the window changing, which counts as the previous one losing
// the focus (VS Code's onDidActiveEditorChange). An editor that closed has not lost anything: it asked, or
// saved, on the way out.
func (s *shellState) activeEditorChanged() {
	st := &s.autosave
	if st.ctl == nil {
		return
	}
	cur := s.activeEditor()
	prev := st.last
	st.last = cur
	if prev == nil || prev == cur || prev.disposed || st.hold > 0 {
		return
	}
	st.ctl.FocusLost(prev.doc)
}

// toggleAutoSave is Toggle Auto Save: any mode but off becomes off, and off becomes afterDelay. The value goes
// to the workspace settings when the workspace sets files.autoSave, else to the user's.
func (s *shellState) toggleAutoSave() {
	win := PeekWindow(s.Context())
	value := "afterDelay"
	if mode, _ := document.ParseAutoSaveMode(settings.Get[string](win.Settings, "files.autoSave", "")); mode != document.AutoSaveOff {
		value = "off"
	}
	target := settings.TargetUser
	if win.Folder != "" && win.Settings.Inspect("files.autoSave", "").Workspace.Ok {
		target = settings.TargetWorkspace
	}
	if err := win.Settings.Update(win.ctx, "files.autoSave", value, target); err != nil {
		win.Process.Logger.Warn("cannot write the setting", "key", "files.autoSave", "err", err)
		win.Notify.Warn("Emetos", "Cannot save the setting files.autoSave: "+describe(err))
	}
}

// ---- closing ----

// autoSavePlan is a document that is saved by itself before the question of closing.
type autoSavePlan struct {
	doc    *document.Document
	reason document.SaveReason
	// skipParticipants saves without the save participants (a window that closes does not wait for them).
	skipParticipants bool
}

// autoSaveBeforeClose saves the documents of the editors that would lose their changes which auto save saves
// instead of asking, then calls next. With shutdown (the window closes) every mode saves, with the reason auto and
// no save participants, as VS Code's backup tracker does; closing editors saves with onFocusChange and onWindowChange
// only (a dialog would take the focus; afterDelay asks like off). A document that cannot be auto saved, or whose
// save fails, stays dirty: next is expected to plan the close again and ask about what is left. While the saves
// are going on no other close is carried out.
func (s *shellState) autoSaveBeforeClose(losses []editorgroup.Unsaved, shutdown bool, next func()) {
	ctl := s.autosave.ctl
	if ctl == nil || s.closing.busy {
		next()
		return
	}
	var plans []autoSavePlan
	for _, u := range losses {
		t, ok := u.Input.(*editorgroup.TextInput)
		if !ok || slices.ContainsFunc(plans, func(p autoSavePlan) bool { return p.doc == t.Document() }) {
			continue
		}
		doc := t.Document()
		if shutdown {
			if ctl.OnShutdown(doc) {
				plans = append(plans, autoSavePlan{doc: doc, reason: document.SaveAuto, skipParticipants: true})
			}
		} else if reason, ok := ctl.OnClose(doc); ok {
			plans = append(plans, autoSavePlan{doc: doc, reason: reason})
		}
	}
	if len(plans) == 0 {
		next()
		return
	}
	s.closing.busy = true
	var run func(i int)
	run = func(i int) {
		if !s.Mounted() {
			s.closing.busy = false
			return
		}
		if i >= len(plans) {
			s.closing.busy = false
			next()
			return
		}
		p := plans[i]
		if !p.doc.Dirty() {
			run(i + 1)
			return
		}
		s.saveDocument(p.doc, saveRequest{reason: p.reason, skipParticipants: p.skipParticipants}, func(bool) { run(i + 1) })
	}
	run(0)
}

// ---- commands ----

// autoSaveCommands are the handlers of the command of auto save.
func (s *shellState) autoSaveCommands() map[string]command.Handler {
	return map[string]command.Handler{
		"workbench.action.toggleAutoSave": func(command.Context, any) error { s.toggleAutoSave(); return nil },
	}
}
