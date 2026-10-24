package document

import (
	"math"
	"time"
)

// Auto save (files.autoSave, SPEC §3.1 "Saving"). An AutoSave decides when the dirty documents of a window are
// saved by themselves; the window does the saving (AutoSaveHost.Save), so the controller does no I/O and has
// no goroutines. Everything on it is called on the one goroutine that uses the documents (the UI thread), and
// the clock calls its functions there too.
//
// The four modes are those of VS Code's files.autoSave:
//
//   - afterDelay saves a document files.autoSaveDelay after its last edit (the timer starts again with every
//     edit);
//   - onFocusChange saves a document when its editor loses the focus or another editor becomes active, and when
//     the window loses the focus;
//   - onWindowChange saves the dirty documents when the window loses the focus;
//   - off saves nothing.
//
// An untitled or virtual document, a read-only one, one outside the workspace folder with
// files.autoSaveWorkspaceFilesOnly, and one with errors in its markers with files.autoSaveWhenNoErrors are not
// saved (the last waits until the errors are gone). A document whose save failed is paused: VS Code's text
// file model ignores every auto save while it is in a conflict or error state, until a save succeeds or the
// file is read again (revert, reload).
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorAutoSave.ts,
// src/vs/workbench/services/filesConfiguration/common/filesConfigurationService.ts (getAutoSaveMode,
// computeAutoSaveConfiguration) and src/vs/workbench/services/textfile/common/textFileEditorModel.ts (save:
// the auto save of a model in conflict or error mode is ignored)

// AutoSaveMode is the value of files.autoSave.
type AutoSaveMode int

const (
	// AutoSaveOff never saves by itself (files.autoSave "off").
	AutoSaveOff AutoSaveMode = iota
	// AutoSaveAfterDelay saves a document after files.autoSaveDelay without edits.
	AutoSaveAfterDelay
	// AutoSaveOnFocusChange saves when the editor loses the focus or the window does.
	AutoSaveOnFocusChange
	// AutoSaveOnWindowChange saves when the window loses the focus.
	AutoSaveOnWindowChange
)

// String is the value of the setting.
func (m AutoSaveMode) String() string {
	switch m {
	case AutoSaveAfterDelay:
		return "afterDelay"
	case AutoSaveOnFocusChange:
		return "onFocusChange"
	case AutoSaveOnWindowChange:
		return "onWindowChange"
	}
	return "off"
}

// ParseAutoSaveMode reads a value of files.autoSave. A value that is none of the four reports false and gives
// AutoSaveOff, as VS Code treats an unknown value as no auto save.
func ParseAutoSaveMode(s string) (AutoSaveMode, bool) {
	switch s {
	case "off":
		return AutoSaveOff, true
	case "afterDelay":
		return AutoSaveAfterDelay, true
	case "onFocusChange":
		return AutoSaveOnFocusChange, true
	case "onWindowChange":
		return AutoSaveOnWindowChange, true
	}
	return AutoSaveOff, false
}

// DefaultAutoSaveDelay is the files.autoSaveDelay VS Code uses when the setting is not a number of 0 or more.
const DefaultAutoSaveDelay = time.Second

// AutoSaveDelay is the delay files.autoSaveDelay (milliseconds) stands for: as VS Code, a number of 0 or more
// is used as it is and anything else (negative, not a number) is the default of one second.
func AutoSaveDelay(milliseconds float64) time.Duration {
	if math.IsNaN(milliseconds) || milliseconds < 0 || milliseconds > float64(math.MaxInt64/int64(time.Millisecond)) {
		return DefaultAutoSaveDelay
	}
	return time.Duration(milliseconds * float64(time.Millisecond))
}

// AutoSaveConfig is what files.autoSave and its three companions say for one document.
type AutoSaveConfig struct {
	// Mode is files.autoSave.
	Mode AutoSaveMode
	// Delay is files.autoSaveDelay, for AutoSaveAfterDelay (see AutoSaveDelay).
	Delay time.Duration
	// WorkspaceFilesOnly is files.autoSaveWorkspaceFilesOnly: only documents inside the folder are saved.
	WorkspaceFilesOnly bool
	// WhenNoErrors is files.autoSaveWhenNoErrors: a document with errors in its markers waits until it has none.
	WhenNoErrors bool
}

// AutoSaveClock starts timers.
type AutoSaveClock interface {
	// AfterFunc calls f once after d, on the goroutine that uses the AutoSave, and returns the function that
	// cancels it.
	AfterFunc(d time.Duration, f func()) (stop func())
}

// AutoSaveClockFunc makes a function an AutoSaveClock.
type AutoSaveClockFunc func(d time.Duration, f func()) (stop func())

// AfterFunc implements AutoSaveClock.
func (c AutoSaveClockFunc) AfterFunc(d time.Duration, f func()) (stop func()) { return c(d, f) }

// AutoSaveHost is what an AutoSave asks of its window.
type AutoSaveHost interface {
	// Config is the auto save configuration for a document (the settings of its language).
	Config(doc *Document) AutoSaveConfig
	// InWorkspace tells whether the file of a document is inside the folder of the window.
	InWorkspace(doc *Document) bool
	// HasErrors tells whether the markers have an error for the document.
	HasErrors(doc *Document) bool
	// Save saves the document in its file, silently (no dialog), for the reason. It returns at once; the write
	// goes on in the background, and the host tells SaveFailed when it fails. A save that is going on already
	// for the document is the host's to join.
	Save(doc *Document, reason SaveReason)
}

// AutoSaveBlock is why a document is not saved by itself now (VS Code's AutoSaveDisabledReason, with the
// cases that its resource checks cover).
type AutoSaveBlock int

const (
	// AutoSaveNotBlocked means the document may be saved.
	AutoSaveNotBlocked AutoSaveBlock = iota
	// AutoSaveBlockedBySettings means the mode is off, or is not one that the event asks for.
	AutoSaveBlockedBySettings
	// AutoSaveBlockedNoFile means the document has no file: it is untitled or virtual.
	AutoSaveBlockedNoFile
	// AutoSaveBlockedReadOnly means the document is read-only.
	AutoSaveBlockedReadOnly
	// AutoSaveBlockedOutOfWorkspace means files.autoSaveWorkspaceFilesOnly is on and the file is outside the folder.
	AutoSaveBlockedOutOfWorkspace
	// AutoSaveBlockedByErrors means files.autoSaveWhenNoErrors is on and the document has errors.
	AutoSaveBlockedByErrors
	// AutoSaveBlockedPaused means a save of the document failed and has not been mended: no auto save until a save
	// succeeds or the file is read again.
	AutoSaveBlockedPaused
)

// AutoSave is the controller of the auto save of the documents of one window.
type AutoSave struct {
	clock AutoSaveClock
	host  AutoSaveHost
	docs  map[*Document]*autoSaveDoc
	// order is the tracked documents in the order they were added, so that saving all is deterministic.
	order []*Document
}

// autoSaveDoc is what is kept of a tracked document.
type autoSaveDoc struct {
	cfg    AutoSaveConfig
	stop   func()
	unsubs []func()
	paused bool
	// waiting is set while a save is held back by errors in the markers: the reason it was asked for.
	waiting    bool
	waitReason SaveReason
}

// NewAutoSave makes a controller that follows documents once Track is called for them.
func NewAutoSave(clock AutoSaveClock, host AutoSaveHost) *AutoSave {
	return &AutoSave{clock: clock, host: host, docs: map[*Document]*autoSaveDoc{}}
}

// Track makes the controller follow a document: its edits start and restart the timer of afterDelay, its saves
// stop it. A dirty document is scheduled at once. Tracking a document twice does nothing.
func (a *AutoSave) Track(doc *Document) {
	if _, ok := a.docs[doc]; ok {
		return
	}
	st := &autoSaveDoc{cfg: a.host.Config(doc)}
	a.docs[doc] = st
	a.order = append(a.order, doc)
	st.unsubs = []func(){
		doc.Subscribe(func(ev ChangeEvent) { a.changed(doc, ev) }),
		doc.OnSaved(func(ev SavedEvent) { a.saved(doc, ev) }),
	}
	if doc.Dirty() {
		a.schedule(doc)
	}
}

// Untrack stops following a document (it was closed) and cancels what is pending for it.
func (a *AutoSave) Untrack(doc *Document) {
	st, ok := a.docs[doc]
	if !ok {
		return
	}
	a.cancel(st)
	for _, u := range st.unsubs {
		u()
	}
	delete(a.docs, doc)
	for i, d := range a.order {
		if d == doc {
			a.order = append(a.order[:i:i], a.order[i+1:]...)
			break
		}
	}
}

// Dispose untracks every document.
func (a *AutoSave) Dispose() {
	for _, doc := range append([]*Document(nil), a.order...) {
		a.Untrack(doc)
	}
}

// Tracked tells whether the controller follows a document.
func (a *AutoSave) Tracked(doc *Document) bool { _, ok := a.docs[doc]; return ok }

// Paused tells whether a failed save keeps the document from being saved by itself.
func (a *AutoSave) Paused(doc *Document) bool {
	st := a.docs[doc]
	return st != nil && st.paused
}

// cancel stops the timer of a document and forgets that it waits for the errors to go.
func (a *AutoSave) cancel(st *autoSaveDoc) {
	if st.stop != nil {
		st.stop()
		st.stop = nil
	}
	st.waiting = false
}

// changed follows an edit: a dirty document starts its timer again, one that is clean has none. A text that is
// read again (revert, reload) mends a document that was paused.
func (a *AutoSave) changed(doc *Document, ev ChangeEvent) {
	st := a.docs[doc]
	if st == nil {
		return
	}
	if ev.Cause == CauseSetText {
		st.paused = false
	}
	if doc.Dirty() {
		a.schedule(doc)
	} else {
		a.cancel(st)
	}
}

// saved follows a save: a document that was written is no longer in an error state, and when it is clean it
// has no timer.
func (a *AutoSave) saved(doc *Document, ev SavedEvent) {
	st := a.docs[doc]
	if st == nil {
		return
	}
	st.paused = false
	if ev.Clean {
		a.cancel(st)
	}
}

// SaveFailed tells that a save of the document failed, whoever asked for it: its auto save pauses until a save
// succeeds or the file is read again, so that one broken file is not tried on every key.
func (a *AutoSave) SaveFailed(doc *Document) {
	if st := a.docs[doc]; st != nil {
		st.paused = true
		a.cancel(st)
	}
}

// Reschedule starts the timer of a dirty document again, for a caller that dropped a save of it (a Save As
// dialog was open) and wants it tried later.
func (a *AutoSave) Reschedule(doc *Document) {
	if doc.Dirty() {
		a.schedule(doc)
	}
}

// schedule (re)starts the timer of afterDelay for a dirty document.
func (a *AutoSave) schedule(doc *Document) {
	st := a.docs[doc]
	if st == nil {
		return
	}
	a.cancel(st)
	if doc.uri.Scheme != SchemeFile || st.paused {
		return
	}
	cfg := a.host.Config(doc)
	if cfg.Mode != AutoSaveAfterDelay {
		return
	}
	st.stop = a.clock.AfterFunc(cfg.Delay, func() { a.fire(doc, st) })
}

// fire is the timer of a document ending: the document is saved if it is still dirty and may be.
func (a *AutoSave) fire(doc *Document, st *autoSaveDoc) {
	if a.docs[doc] != st {
		return
	}
	st.stop = nil
	if doc.Dirty() {
		a.trigger(doc, st, SaveAuto)
	}
}

// reasonAllowed tells whether a mode saves for a reason (VS Code's getAutoSaveMode with a save reason).
func reasonAllowed(mode AutoSaveMode, reason SaveReason) bool {
	switch mode {
	case AutoSaveAfterDelay:
		return reason == SaveAuto
	case AutoSaveOnFocusChange:
		return reason == SaveFocusChange || reason == SaveWindowChange
	case AutoSaveOnWindowChange:
		return reason == SaveWindowChange
	}
	return false
}

// check tells why a document is not saved by itself, or AutoSaveNotBlocked. With anyReason, the mode only has to
// be on (a close, VS Code's getAutoSaveMode without a reason); else it must be one that saves for the reason.
func (a *AutoSave) check(doc *Document, reason SaveReason, anyReason bool) AutoSaveBlock {
	switch {
	case doc.uri.Scheme != SchemeFile:
		return AutoSaveBlockedNoFile
	case doc.readOnly:
		return AutoSaveBlockedReadOnly
	}
	cfg := a.host.Config(doc)
	if cfg.Mode == AutoSaveOff || (!anyReason && !reasonAllowed(cfg.Mode, reason)) {
		return AutoSaveBlockedBySettings
	}
	if st := a.docs[doc]; st != nil && st.paused {
		return AutoSaveBlockedPaused
	}
	if cfg.WorkspaceFilesOnly && !a.host.InWorkspace(doc) {
		return AutoSaveBlockedOutOfWorkspace
	}
	if cfg.WhenNoErrors && a.host.HasErrors(doc) {
		return AutoSaveBlockedByErrors
	}
	return AutoSaveNotBlocked
}

// Check tells why the document is not saved by itself for a reason (AutoSaveNotBlocked when it is).
func (a *AutoSave) Check(doc *Document, reason SaveReason) AutoSaveBlock {
	return a.check(doc, reason, false)
}

// trigger saves a document for a reason when it may be, or waits for the errors to go when they are what blocks.
func (a *AutoSave) trigger(doc *Document, st *autoSaveDoc, reason SaveReason) {
	switch a.check(doc, reason, false) {
	case AutoSaveNotBlocked:
		st.waiting = false
		a.host.Save(doc, reason)
	case AutoSaveBlockedByErrors:
		st.waiting, st.waitReason = true, reason
	}
}

// FocusLost tells that the editor of a document lost the focus, or that another editor became the active one:
// with onFocusChange the document is saved.
func (a *AutoSave) FocusLost(doc *Document) {
	if st := a.docs[doc]; st != nil && doc.Dirty() {
		a.trigger(doc, st, SaveFocusChange)
	}
}

// WindowFocusLost tells that the window lost the focus: with onFocusChange or onWindowChange every dirty
// document that may be is saved.
func (a *AutoSave) WindowFocusLost() { a.saveAll(SaveWindowChange) }

// saveAll triggers a reason for every dirty document.
func (a *AutoSave) saveAll(reason SaveReason) {
	for _, doc := range append([]*Document(nil), a.order...) {
		if st := a.docs[doc]; st != nil && doc.Dirty() {
			a.trigger(doc, st, reason)
		}
	}
}

// MarkersChanged tells that the markers changed: a document that waited for its errors to go is saved when it
// has none now.
func (a *AutoSave) MarkersChanged() {
	for _, doc := range append([]*Document(nil), a.order...) {
		st := a.docs[doc]
		if st == nil || !st.waiting || !doc.Dirty() {
			continue
		}
		if a.check(doc, st.waitReason, false) == AutoSaveNotBlocked {
			reason := st.waitReason
			a.cancel(st)
			a.host.Save(doc, reason)
		}
	}
}

// ConfigChanged tells that the settings may have changed. A document whose configuration changed loses its
// timer and, when auto save is on for it, is saved at once, as VS Code saves every dirty document when the
// configuration of auto save changes ("Toggle Auto Save" on a window with unsaved changes saves them); switching
// auto save off cancels what was pending.
func (a *AutoSave) ConfigChanged() {
	for _, doc := range append([]*Document(nil), a.order...) {
		st := a.docs[doc]
		if st == nil {
			continue
		}
		cfg := a.host.Config(doc)
		if cfg == st.cfg {
			continue
		}
		st.cfg = cfg
		a.cancel(st)
		if !doc.Dirty() || cfg.Mode == AutoSaveOff {
			continue
		}
		switch cfg.Mode {
		case AutoSaveAfterDelay:
			a.trigger(doc, st, SaveAuto)
		case AutoSaveOnFocusChange:
			a.trigger(doc, st, SaveFocusChange)
		case AutoSaveOnWindowChange:
			a.trigger(doc, st, SaveWindowChange)
		}
	}
}

// OnClose tells how a document is saved before its editor closes, and whether it is at all: with onFocusChange
// the editor saves instead of asking (the reason is SaveFocusChange), and with onWindowChange too (SaveWindowChange;
// VS Code does so on Linux and Windows, where a dialog takes the focus from the window). afterDelay asks like any
// other mode. A document that may not be auto saved (see Check) is asked about.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorGroupView.ts (doHandleCloseConfirmation)
func (a *AutoSave) OnClose(doc *Document) (SaveReason, bool) {
	var reason SaveReason
	switch a.host.Config(doc).Mode {
	case AutoSaveOnFocusChange:
		reason = SaveFocusChange
	case AutoSaveOnWindowChange:
		reason = SaveWindowChange
	default:
		return SaveExplicit, false
	}
	if a.check(doc, reason, true) != AutoSaveNotBlocked {
		return SaveExplicit, false
	}
	return reason, true
}

// OnShutdown tells whether a dirty document is saved by itself when the window closes, whatever the mode, as
// VS Code's backup tracker does before it asks or backs up (the reason is SaveAuto and the save participants are
// skipped, for speed).
//
// Ported from VS Code: src/vs/workbench/services/workingCopy/electron-browser/workingCopyBackupTracker.ts
// (onBeforeShutdownWithModified)
func (a *AutoSave) OnShutdown(doc *Document) bool {
	return a.check(doc, SaveAuto, true) == AutoSaveNotBlocked
}
