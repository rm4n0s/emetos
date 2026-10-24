package app

import (
	"time"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The status bar of the window (SPEC §3.2, §10.1). The registry of its items is Window.StatusBar; the shell
// adds the items of the problems, of the notifications and of the active editor, keeps them up to date, and
// binds the commands of their pickers (statuspickers.go).
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorStatus.ts,
// src/vs/workbench/contrib/markers/browser/markers.contribution.ts (MarkersStatusBarContributions) and
// src/vs/workbench/browser/parts/notifications/notificationsStatus.ts

// The priorities of the items of the status bar (VS Code's): the problems count and the items of the editor.
const (
	problemsPriority    = 50
	selectionPriority   = 100.5
	indentationPriority = 100.4
	encodingPriority    = 100.3
	eolPriority         = 100.2
	languagePriority    = 100.1
)

// statusState is what the shell keeps of the status bar: the accessors of the items it owns.
type statusState struct {
	problems *workbench.StatusAccessor
	bell     *workbench.Bell
	// selection, indentation, encoding, eol and language are the items of the active editor; they are
	// hidden while no editor is active.
	selection, indentation, encoding, eol, language *workbench.StatusAccessor
	stops                                           []func()
	removeCommands                                  []func()
}

// editorItems lists the items of the active editor.
func (st *statusState) editorItems() []*workbench.StatusAccessor {
	return []*workbench.StatusAccessor{st.selection, st.indentation, st.encoding, st.eol, st.language}
}

// initStatus adds the items of the shell to the status bar of the window and starts to follow what they show.
func (s *shellState) initStatus(binding *widgets.WidgetsBinding) {
	win := PeekWindow(s.Context())
	st := &s.status
	win.after = func(d time.Duration, f func()) func() { return binding.Gestures().NewTimer(d, f).Cancel }
	bar := win.StatusBar

	st.problems = bar.Add("status.problems", workbench.StatusLeft, problemsPriority, s.problemsEntry())
	st.stops = append(st.stops, win.Markers.Subscribe(func(problems.Change) {
		// the markers change on any goroutine
		binding.Post(func() {
			if s.Mounted() {
				st.problems.Update(s.problemsEntry())
			}
		})
	}))
	st.bell = workbench.NewBell(bar, win.Notifications, binding.Post)

	st.selection = bar.Add("status.editor.selection", workbench.StatusRight, selectionPriority, workbench.StatusEntry{})
	st.indentation = bar.Add("status.editor.indentation", workbench.StatusRight, indentationPriority, workbench.StatusEntry{})
	st.encoding = bar.Add("status.editor.encoding", workbench.StatusRight, encodingPriority, workbench.StatusEntry{})
	st.eol = bar.Add("status.editor.eol", workbench.StatusRight, eolPriority, workbench.StatusEntry{})
	st.language = bar.Add("status.editor.mode", workbench.StatusRight, languagePriority, workbench.StatusEntry{})
	for _, it := range st.editorItems() {
		it.SetVisible(false)
	}
	s.bindStatusCommands(binding)
	s.refreshStatus()
}

// disposeStatus takes the items of the shell out of the bar and stops following.
func (s *shellState) disposeStatus() {
	st := &s.status
	if st.problems == nil {
		return
	}
	win := s.win
	for _, f := range st.stops {
		f()
	}
	for _, f := range st.removeCommands {
		f()
	}
	st.bell.Dispose()
	st.problems.Dispose()
	for _, it := range st.editorItems() {
		it.Dispose()
	}
	win.ClearStatusMessage()
	win.stopChordTimeout()
	win.after = nil
	s.status = statusState{}
}

// problemsEntry is the item of the problems: the errors and warnings of the markers, and a click toggles the
// Problems panel.
func (s *shellState) problemsEntry() workbench.StatusEntry {
	stats := PeekWindow(s.Context()).Markers.Stats()
	counts := workbench.ProblemCounts{Errors: stats.Errors, Warnings: stats.Warnings, Infos: stats.Infos}
	tooltip := workbench.ProblemsTooltip(counts)
	return workbench.StatusEntry{
		Name: "Problems", Text: workbench.ProblemsText(counts), AriaLabel: tooltip, Tooltip: tooltip,
		Command: "workbench.actions.view.toggleProblems",
	}
}

// refreshStatus makes the items of the editor show the active editor: its cursors and selections, indentation,
// encoding, line ending and language. With no editor they are hidden. It is called whenever one of them may have
// changed; an item that shows the same is not touched.
func (s *shellState) refreshStatus() {
	st := &s.status
	if st.selection == nil || s.group.grid == nil {
		return
	}
	e := s.activeEditor()
	if e == nil {
		for _, it := range st.editorItems() {
			it.SetVisible(false)
		}
		return
	}
	win := PeekWindow(s.Context())
	doc := e.doc
	tabSize, insertSpaces := doc.Indentation()
	cursors := e.view.Cursors()

	set := func(it *workbench.StatusAccessor, name, text, tooltip, cmd string) {
		it.Update(workbench.StatusEntry{Name: name, Text: text, AriaLabel: text, Tooltip: tooltip, Command: cmd})
		it.SetVisible(text != "")
	}
	set(st.selection, "Editor Selection",
		workbench.SelectionStatus(doc.Snapshot(), cursors.Selections(), cursors.Primary().Selection, tabSize, doc.LineEnding()),
		"Go to Line/Column", "workbench.action.gotoLine")
	set(st.indentation, "Editor Indentation", workbench.IndentationLabel(tabSize, insertSpaces), "Select Indentation", "changeEditorIndentation")
	set(st.encoding, "Editor Encoding", workbench.EncodingLabel(doc.Encoding()), "Select Encoding", "workbench.action.editor.changeEncoding")
	eolText := ""
	if !doc.ReadOnly() {
		eolText = workbench.EOLLabel(doc.LineEnding())
	}
	set(st.eol, "Editor End of Line", eolText, "Select End of Line Sequence", "workbench.action.editor.changeEOL")
	set(st.language, "Editor Language", win.languageName(doc.LanguageID()), "Select Language Mode", "workbench.action.editor.changeLanguageMode")
}

// languageName is the name of a language as the status bar and the language picker show it: "Go",
// "Plain Text"; the id for a language that is not registered.
func (w *Window) languageName(id string) string {
	if l, ok := w.Process.Languages.Language(id); ok && l.Name != "" {
		return l.Name
	}
	return id
}

// statusKeyLabel is the label of the key of a command for the tooltips of the status bar, "" when it has none.
func (w *Window) statusKeyLabel(commandID string) string {
	item, ok := w.Keybindings.LookupPrimaryKeybinding(commandID, w.Keys, false)
	if !ok || item.Keybinding == nil {
		return ""
	}
	return item.Keybinding.Label()
}

// statusBarOptions are the options of the status bar of the workbench.
func (s *shellState) statusBarOptions(win *Window) *workbench.StatusBarOptions {
	return &workbench.StatusBarOptions{
		Bar:        win.StatusBar,
		Keybinding: win.statusKeyLabel,
		OnError: func(id string, err error) {
			win.Process.Logger.Warn("command of the status bar failed", "command", id, "err", err)
			win.Notify.Error("Emetos", describe(err))
		},
	}
}
