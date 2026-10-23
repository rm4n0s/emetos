package editor

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// action is what a command does to the editor that has the focus.
type action func(vm *ViewModel, args any) error

// actions are the editor's commands by id (SPEC §11.2): the cursor movement, selection and
// editing commands of milestone 1. Each calls the functions of core/edit with the view model's
// text, cursors and layout and applies the result. The table is read only.
var actions = buildActions()

// BindCommands attaches the handlers of the editor commands to a registry for a view model,
// and returns the function that takes them off again. An editor binds its commands while it
// has the focus, so that the keys of a window run them on the editor in front of the user
// (handlers stack: SPEC §11.2). A command the registry does not know is skipped: the registry
// of the process lists them all (a test checks it).
func BindCommands(r *command.Registry, vm *ViewModel) (unbind func()) {
	var removers []func()
	for id, act := range actions {
		act := act
		remove, err := r.SetHandler(id, func(_ command.Context, args any) error { return vm.Run(act, args) })
		if err != nil {
			continue
		}
		removers = append(removers, remove)
	}
	return func() {
		for _, remove := range removers {
			remove()
		}
		removers = nil
	}
}

// CommandIDs are the ids of the commands the editor implements.
func CommandIDs() []string {
	ids := make([]string, 0, len(actions))
	for id := range actions {
		ids = append(ids, id)
	}
	return ids
}

// RunCommand runs a command of the editor on a view model, for callers that have no registry
// (the tests, the dev commands). It fails with the tag EditorCommandUnknown for an id that
// is not an editor command.
func (vm *ViewModel) RunCommand(id string, args any) error {
	act, ok := actions[id]
	if !ok {
		return errors.New("EditorCommandUnknown", "not a command of the editor", "id", id)
	}
	return vm.Run(act, args)
}

// Run does an action: it forgets the sessions of the commands that go on with each other (the
// column selection, the occurrences, the expansion of the selection) when something else moved
// the cursors since, runs it, and shows the cursors again.
func (vm *ViewModel) Run(act action, args any) error {
	vm.dropStaleSessions()
	vm.sessionOwner = sessionNone
	vm.keepSessions, vm.skipReveal = false, false
	err := act(vm, args)
	// A command that is not part of a session ends it, unless it only reads (a copy).
	if !vm.keepSessions {
		if vm.sessionOwner != sessionColumns {
			vm.columns = nil
		}
		if vm.sessionOwner != sessionOccurrences {
			vm.occurrences = nil
		}
		if vm.sessionOwner != sessionExpansion {
			vm.expansion = nil
		}
	}
	vm.sessionSelections = vm.cursors.Selections()
	vm.SelectionsChanged()
	if !vm.noReveal && !vm.skipReveal {
		vm.RevealPrimary()
	}
	return err
}

// The sessions of the commands that go on with each other.
const (
	sessionNone = iota
	sessionColumns
	sessionOccurrences
	sessionExpansion
)

// dropStaleSessions ends the sessions when the selections are not the ones the last command
// left.
func (vm *ViewModel) dropStaleSessions() {
	if buffer.SelectionsEqual(vm.cursors.Selections(), vm.sessionSelections) {
		return
	}
	vm.columns, vm.occurrences, vm.expansion = nil, nil, nil
}

// editOptions are the options of the editing functions: the tab size and the indentation of
// the document.
func (vm *ViewModel) editOptions() edit.Options {
	o := edit.DefaultOptions()
	o.TabSize = vm.opts.TabSize
	_, o.InsertSpaces = vm.doc.Indentation()
	o.AutoIndent = vm.autoIndent()
	o.AutoIndentOnPaste = vm.opts.AutoIndentOnPaste
	o.AutoIndentOnPasteWithinString = vm.opts.AutoIndentOnPasteWithinString
	o.Indenter, o.Tokens = vm.indenter, indentTokens{vm}
	o.AutoClosingBrackets = parseStrategy(vm.opts.AutoClosingBrackets)
	o.AutoClosingComments = parseStrategy(vm.opts.AutoClosingComments)
	o.AutoClosingQuotes = parseStrategy(vm.opts.AutoClosingQuotes)
	o.AutoClosingDelete, _ = autoclose.ParseBehavior(vm.opts.AutoClosingDelete)
	o.AutoClosingOvertype, _ = autoclose.ParseBehavior(vm.opts.AutoClosingOvertype)
	o.AutoSurround, _ = autoclose.ParseSurround(vm.opts.AutoSurround)
	o.Comments = vm.comments
	o.CommentInsertSpace, o.CommentIgnoreEmptyLines = vm.opts.CommentsInsertSpace, vm.opts.CommentsIgnoreEmptyLines
	o.AutoClosing = vm.autoClosing
	o.AutoClosed = vm.autoClosed.Characters()
	if ix := vm.bracketIndex; ix != nil && ix.Enabled() {
		o.UnmatchedClosing = ix.HasUnmatchedClosingAfter
	}
	return o
}

// parseStrategy reads a value of editor.autoClosingBrackets, Comments or Quotes; a value that is
// none of them is "languageDefined".
func parseStrategy(s string) autoclose.Strategy {
	strategy, _ := autoclose.ParseStrategy(s)
	return strategy
}

func (vm *ViewModel) commands() edit.Commands {
	return edit.NewCommands(edit.NewModel(vm.doc.Snapshot()), vm.editOptions())
}

func (vm *ViewModel) mover() edit.Mover {
	return edit.NewMover(edit.NewModel(vm.doc.Snapshot()), vm.editOptions(), vm.Layout())
}

// apply applies the result of an editing function: the edits go into the document as one
// undo step of the result's kind, with the selections before and after, and the cursors
// follow. A result without edits only moves the cursors.
func (vm *ViewModel) apply(res edit.Result) error {
	before := vm.cursors.Selections()
	if len(res.Edits) > 0 {
		after := res.Selections
		_, err := vm.doc.Edit(res.Edits, document.EditOptions{
			Kind: res.Kind, Before: before,
			After: func([]document.Edit) []buffer.Selection { return after },
		})
		if err != nil {
			return err
		}
		vm.history.Clear()
		vm.maxLineWidth = 0
		vm.Sync()
	}
	if len(res.Selections) > 0 {
		vm.cursors.SetSelections(res.Selections)
	}
	// The cursors were validated against the pairs tracked before: the pairs of the result are
	// tracked from now on.
	vm.autoClosed.Add(res.AutoClosed)
	if len(res.Edits) > 0 {
		vm.NotifyListeners()
	}
	if res.FollowUp != nil {
		if next, ok := res.FollowUp(vm.commands(), vm.cursors.Selections()); ok {
			return vm.apply(next)
		}
	}
	return nil
}

// moveCursors moves every cursor, remembering where they were for cursor undo.
func (vm *ViewModel) moveCursors(fn func(mv edit.Mover, c edit.Cursor) edit.Cursor) {
	vm.history.Record(vm.cursors.Selections())
	mv := vm.mover()
	vm.cursors.Map(func(c edit.Cursor) edit.Cursor { return fn(mv, c) })
}

// setSelections replaces the cursors by selections, remembering where they were.
func (vm *ViewModel) setSelections(sels []buffer.Selection) {
	vm.history.Record(vm.cursors.Selections())
	vm.cursors.SetSelections(sels)
}

// lineEndOf is the position at the end of the line of a position.
func (vm *ViewModel) lineEndOf(p buffer.Position) buffer.Position {
	line, _ := vm.doc.Snapshot().LineContent(p.Line)
	return buffer.Position{Line: p.Line, Col: len(line)}
}

func buildActions() map[string]action {
	a := map[string]action{}

	// Moving and selecting: every movement has a Select twin that extends the selection.
	move := func(id string, f func(vm *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor) {
		for _, sel := range []bool{false, true} {
			name := id
			if sel {
				name += "Select"
			}
			sel := sel
			a[name] = func(vm *ViewModel, _ any) error {
				vm.moveCursors(func(mv edit.Mover, c edit.Cursor) edit.Cursor { return f(vm, mv, c, sel) })
				return nil
			}
		}
	}
	move("cursorLeft", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor { return mv.Left(c, sel) })
	move("cursorRight", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor { return mv.Right(c, sel) })
	move("cursorUp", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor { return mv.Up(c, sel) })
	move("cursorDown", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor { return mv.Down(c, sel) })
	move("cursorPageUp", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor { return mv.PageUp(c, sel) })
	move("cursorPageDown", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor { return mv.PageDown(c, sel) })
	move("cursorHome", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor { return mv.Home(c, sel) })
	move("cursorEnd", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor { return mv.End(c, sel) })
	move("cursorTop", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor { return mv.DocStart(c, sel) })
	move("cursorBottom", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor { return mv.DocEnd(c, sel) })
	move("cursorLineStart", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor {
		return mv.MoveTo(c, buffer.Position{Line: c.Position().Line}, sel)
	})
	move("cursorLineEnd", func(vm *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor {
		return mv.MoveTo(c, vm.lineEndOf(c.Position()), sel)
	})

	// Words: the stops of left and right movement of each kind.
	words := map[string]edit.WordNavigation{
		"cursorWordStart": edit.WordStart, "cursorWordEnd": edit.WordEnd,
		"cursorWord": edit.WordStartFast, "cursorWordAccessibility": edit.WordAccessibility,
	}
	for prefix, nav := range words {
		prefix, nav := prefix, nav
		move(prefix+"Left", func(vm *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor {
			return mv.WordLeft(c, sel, nav, vm.cursors.Len() > 1)
		})
		move(prefix+"Right", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor {
			return mv.WordRight(c, sel, nav)
		})
	}
	move("cursorWordPartLeft", func(vm *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor {
		return mv.WordPartLeft(c, sel, vm.cursors.Len() > 1)
	})
	move("cursorWordPartRight", func(_ *ViewModel, mv edit.Mover, c edit.Cursor, sel bool) edit.Cursor {
		return mv.WordPartRight(c, sel)
	})

	// Column selection: the rectangle goes on from where the last column command left it.
	column := func(id string, f func(mv edit.Mover, d edit.ColumnSelectData) edit.ColumnSelection) {
		a[id] = func(vm *ViewModel, _ any) error {
			vm.sessionOwner = sessionColumns
			mv := vm.mover()
			if vm.columns == nil {
				d := mv.ColumnSelectDataOf(vm.cursors.Primary())
				vm.columns = &d
			}
			res := f(mv, *vm.columns)
			vm.history.Record(vm.cursors.Selections())
			vm.cursors.Set(res.Cursors)
			vm.columns = &res.Data
			return nil
		}
	}
	column("cursorColumnSelectLeft", func(mv edit.Mover, d edit.ColumnSelectData) edit.ColumnSelection { return mv.ColumnSelectLeft(d) })
	column("cursorColumnSelectRight", func(mv edit.Mover, d edit.ColumnSelectData) edit.ColumnSelection { return mv.ColumnSelectRight(d) })
	column("cursorColumnSelectUp", func(mv edit.Mover, d edit.ColumnSelectData) edit.ColumnSelection { return mv.ColumnSelectUp(d, false) })
	column("cursorColumnSelectPageUp", func(mv edit.Mover, d edit.ColumnSelectData) edit.ColumnSelection { return mv.ColumnSelectUp(d, true) })
	column("cursorColumnSelectDown", func(mv edit.Mover, d edit.ColumnSelectData) edit.ColumnSelection {
		return mv.ColumnSelectDown(d, false)
	})
	column("cursorColumnSelectPageDown", func(mv edit.Mover, d edit.ColumnSelectData) edit.ColumnSelection { return mv.ColumnSelectDown(d, true) })

	// Selections and cursors.
	a["cancelSelection"] = func(vm *ViewModel, _ any) error {
		vm.setSelections([]buffer.Selection{buffer.NewCursor(vm.cursors.Primary().Position())})
		return nil
	}
	a["removeSecondaryCursors"] = func(vm *ViewModel, _ any) error {
		vm.history.Record(vm.cursors.Selections())
		vm.cursors.CancelSecondary()
		return nil
	}
	a["expandLineSelection"] = func(vm *ViewModel, _ any) error {
		vm.moveCursors(func(mv edit.Mover, c edit.Cursor) edit.Cursor { return mv.ExpandLine(c) })
		return nil
	}
	a["editor.action.fontZoomIn"] = func(vm *ViewModel, _ any) error { vm.zoom.Set(vm.zoom.Level() + 1); return nil }
	a["editor.action.fontZoomOut"] = func(vm *ViewModel, _ any) error { vm.zoom.Set(vm.zoom.Level() - 1); return nil }
	a["editor.action.fontZoomReset"] = func(vm *ViewModel, _ any) error { vm.zoom.Set(0); return nil }
	a["editor.action.clipboardCopyAction"] = func(vm *ViewModel, _ any) error { return vm.copySelection() }
	a["editor.action.clipboardCutAction"] = func(vm *ViewModel, _ any) error { return vm.cutSelection() }
	a["editor.action.clipboardPasteAction"] = func(vm *ViewModel, _ any) error { return vm.pasteClipboard() }
	a["editor.action.selectAll"] = func(vm *ViewModel, _ any) error {
		vm.history.Record(vm.cursors.Selections())
		vm.cursors.Set([]edit.Cursor{vm.mover().SelectAll()})
		return nil
	}
	a["cursorUndo"] = func(vm *ViewModel, _ any) error {
		if sels, ok := vm.history.Undo(vm.cursors.Selections()); ok {
			vm.cursors.SetSelections(sels)
		}
		return nil
	}
	a["cursorRedo"] = func(vm *ViewModel, _ any) error {
		if sels, ok := vm.history.Redo(vm.cursors.Selections()); ok {
			vm.cursors.SetSelections(sels)
		}
		return nil
	}

	// Typing and deleting.
	a["type"] = func(vm *ViewModel, args any) error {
		text, ok := textArgument(args)
		if !ok || text == "" {
			return nil
		}
		return vm.apply(vm.commands().Type(vm.cursors.Selections(), text))
	}
	a["lineBreakInsert"] = func(vm *ViewModel, _ any) error { return vm.apply(vm.lineBreakInsert()) }
	a["tab"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.Tab(s) })
	a["outdent"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.Outdent(s) })
	a["deleteLeft"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteLeft(s) })
	a["deleteRight"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteRight(s) })
	a["deleteWordStartLeft"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteWordStartLeft(s) })
	a["deleteWordEndLeft"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteWordEndLeft(s) })
	a["deleteWordLeft"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteWordLeft(s) })
	a["deleteWordStartRight"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteWordStartRight(s) })
	a["deleteWordEndRight"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteWordEndRight(s) })
	a["deleteWordRight"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteWordRight(s) })
	a["deleteWordPartLeft"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteWordPartLeft(s) })
	a["deleteWordPartRight"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteWordPartRight(s) })
	a["deleteInsideWord"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteInsideWord(s, false) })
	a["undo"] = func(vm *ViewModel, _ any) error { return vm.undoRedo(vm.doc.Undo) }
	a["redo"] = func(vm *ViewModel, _ any) error { return vm.undoRedo(vm.doc.Redo) }

	// Lines.
	a["editor.action.copyLinesUpAction"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.CopyLines(s, false) })
	a["editor.action.copyLinesDownAction"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.CopyLines(s, true) })
	a["editor.action.moveLinesUpAction"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.MoveLines(s, false) })
	a["editor.action.moveLinesDownAction"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.MoveLines(s, true) })
	a["editor.action.sortLinesAscending"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.SortLines(s, false) })
	a["editor.action.sortLinesDescending"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.SortLines(s, true) })
	a["editor.action.trimTrailingWhitespace"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.TrimTrailingWhitespace(s, false) })
	a["editor.action.deleteLines"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.DeleteLines(s) })
	a["editor.action.indentLines"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.Indent(s) })
	a["editor.action.outdentLines"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.Outdent(s) })
	a["editor.action.insertLineBefore"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.InsertLineBefore(s) })
	a["editor.action.insertLineAfter"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.InsertLineAfter(s) })
	a["editor.action.joinLines"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.JoinLines(s) })
	a["editor.action.transpose"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.Transpose(s) })
	a["editor.action.transformToUppercase"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.TransformCase(s, edit.CaseUpper) })
	a["editor.action.transformToLowercase"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.TransformCase(s, edit.CaseLower) })
	a["editor.action.transformToTitlecase"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.TransformCase(s, edit.CaseTitle) })

	// Several cursors.
	a["editor.action.insertCursorAbove"] = func(vm *ViewModel, _ any) error {
		vm.history.Record(vm.cursors.Selections())
		vm.cursors.Set(vm.mover().AddCursorAbove(vm.cursors.All()))
		return nil
	}
	a["editor.action.insertCursorBelow"] = func(vm *ViewModel, _ any) error {
		vm.history.Record(vm.cursors.Selections())
		vm.cursors.Set(vm.mover().AddCursorBelow(vm.cursors.All()))
		return nil
	}
	a["editor.action.insertCursorAtEndOfEachLineSelected"] = func(vm *ViewModel, _ any) error {
		vm.setSelections(vm.commands().CursorsAtLineEnds(vm.cursors.Selections()))
		return nil
	}
	occurrence := func(f func(c edit.Commands, sess *edit.OccurrenceSession, sels []buffer.Selection, find edit.SearchOptions) (*edit.OccurrenceSession, []buffer.Selection, bool)) action {
		return func(vm *ViewModel, _ any) error {
			vm.sessionOwner = sessionOccurrences
			sess, sels, ok := f(vm.commands(), vm.occurrences, vm.cursors.Selections(), vm.find)
			vm.occurrences = sess
			if ok {
				vm.setSelections(sels)
			}
			return nil
		}
	}
	a["editor.action.addSelectionToNextFindMatch"] = occurrence(func(c edit.Commands, s *edit.OccurrenceSession, sels []buffer.Selection, f edit.SearchOptions) (*edit.OccurrenceSession, []buffer.Selection, bool) {
		return c.AddNextOccurrence(s, sels, f)
	})
	a["editor.action.addSelectionToPreviousFindMatch"] = occurrence(func(c edit.Commands, s *edit.OccurrenceSession, sels []buffer.Selection, f edit.SearchOptions) (*edit.OccurrenceSession, []buffer.Selection, bool) {
		return c.AddPreviousOccurrence(s, sels, f)
	})
	a["editor.action.moveSelectionToNextFindMatch"] = occurrence(func(c edit.Commands, s *edit.OccurrenceSession, sels []buffer.Selection, f edit.SearchOptions) (*edit.OccurrenceSession, []buffer.Selection, bool) {
		return c.MoveToNextOccurrence(s, sels, f)
	})
	a["editor.action.moveSelectionToPreviousFindMatch"] = occurrence(func(c edit.Commands, s *edit.OccurrenceSession, sels []buffer.Selection, f edit.SearchOptions) (*edit.OccurrenceSession, []buffer.Selection, bool) {
		return c.MoveToPreviousOccurrence(s, sels, f)
	})
	a["editor.action.selectHighlights"] = func(vm *ViewModel, _ any) error {
		vm.sessionOwner = sessionOccurrences
		sess, sels := vm.commands().SelectAllOccurrences(vm.occurrences, vm.cursors.Selections(), vm.find, nil)
		vm.occurrences = sess
		if len(sels) > 0 {
			vm.setSelections(sels)
		}
		return nil
	}
	a["editor.action.smartSelect.expand"] = func(vm *ViewModel, _ any) error {
		vm.sessionOwner = sessionExpansion
		if vm.expansion == nil {
			vm.expansion = vm.commands().NewExpansion(vm.cursors.Selections(), edit.DefaultSelectionRangesOptions(), vm.selectionProviders()...)
		}
		if vm.expansion != nil {
			vm.setSelections(vm.expansion.Expand())
		}
		return nil
	}
	a["editor.action.smartSelect.shrink"] = func(vm *ViewModel, _ any) error {
		vm.sessionOwner = sessionExpansion
		if vm.expansion != nil {
			vm.setSelections(vm.expansion.Shrink())
		}
		return nil
	}
	a["editor.action.jumpToBracket"] = func(vm *ViewModel, _ any) error {
		if ix := vm.bracketIndex; ix != nil && ix.Enabled() {
			vm.setSelections(edit.JumpToBracket(ix, vm.cursors.Selections()))
		}
		return nil
	}
	a["editor.action.selectToBracket"] = func(vm *ViewModel, args any) error {
		ix := vm.bracketIndex
		if ix == nil || !ix.Enabled() {
			return nil
		}
		selectBrackets := true
		if m, ok := args.(map[string]any); ok {
			if v, ok := m["selectBrackets"].(bool); ok && !v {
				selectBrackets = false
			}
		}
		if sels, ok := edit.SelectToBracket(ix, vm.cursors.Selections(), selectBrackets); ok {
			vm.setSelections(sels)
		}
		return nil
	}
	a["editor.action.removeBrackets"] = func(vm *ViewModel, _ any) error {
		ix := vm.bracketIndex
		if ix == nil || !ix.Enabled() {
			return nil
		}
		return vm.apply(vm.commands().RemoveBrackets(ix, vm.cursors.Selections()))
	}
	a["editor.action.indentationToSpaces"] = convertIndentation(true)
	a["editor.action.indentationToTabs"] = convertIndentation(false)
	a["editor.action.indentUsingSpaces"] = indentUsing(true, false)
	a["editor.action.indentUsingTabs"] = indentUsing(false, false)
	a["editor.action.changeTabDisplaySize"] = indentUsing(true, true)
	a["editor.action.commentLine"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.ToggleLineComment(s) })
	a["editor.action.addCommentLine"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.AddLineComment(s) })
	a["editor.action.removeCommentLine"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.RemoveLineComment(s) })
	a["editor.action.blockComment"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.ToggleBlockComment(s) })
	a["editor.action.reindentlines"] = func(vm *ViewModel, _ any) error { return vm.reindentLines() }
	a["editor.action.reindentselectedlines"] = editing(func(c edit.Commands, s []buffer.Selection) edit.Result { return c.ReindentSelected(s) })
	a["editor.action.detectIndentation"] = func(vm *ViewModel, _ any) error {
		vm.detectIndentation()
		return nil
	}
	devActions(a)
	return a
}

// editing makes the action of a function of core/edit that returns edits.
func editing(f func(c edit.Commands, sels []buffer.Selection) edit.Result) action {
	return func(vm *ViewModel, _ any) error {
		return vm.apply(f(vm.commands(), vm.cursors.Selections()))
	}
}

// textArgument reads the text of the arguments of `type`: {"text": "..."} or a string.
func textArgument(args any) (string, bool) {
	switch v := args.(type) {
	case string:
		return v, true
	case map[string]any:
		s, ok := v["text"].(string)
		return s, ok
	}
	return "", false
}

// undoRedo undoes or redoes a step of the document and puts the cursors back where it says.
func (vm *ViewModel) undoRedo(f func() (document.HistoryResult, error)) error {
	res, err := f()
	if err != nil {
		return err
	}
	if !res.Done {
		return nil
	}
	if len(res.Selections) > 0 {
		vm.cursors.SetSelections(res.Selections)
	}
	vm.history.Clear()
	vm.maxLineWidth = 0
	vm.Sync()
	vm.NotifyListeners()
	return nil
}

// lineBreakInsert breaks the line at every cursor, keeping its indentation for the new line,
// and leaves the cursor before the break (Insert Line Break).
//
// Ported from VS Code: src/vs/editor/browser/coreCommands.ts (LineBreakInsert)
func (vm *ViewModel) lineBreakInsert() edit.Result {
	return vm.commands().LineBreakInsert(vm.cursors.Selections())
}
