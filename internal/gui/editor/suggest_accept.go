package editor

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/snippet"
)

// Accepting an item of the suggest widget (SPEC §9.12 "Completion"): its text replaces the word, as a snippet, with its
// additional edits and its command; the whole is one undo step.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestController.ts (_insertSuggestion, getOverwriteInfo,
// LineSuffix)

// triggerSuggestCommand is the command an item can ask for to open the widget again.
const triggerSuggestCommand = "editor.action.triggerSuggest"

// insertOptions are the flags of an acceptance.
type insertOptions struct {
	// alternative uses the other of insert and replace than editor.suggest.insertMode says (Shift+Enter).
	alternative bool
	// noAfterUndoStop leaves the undo group open, as the acceptance by a commit character does: the character typed next
	// joins it.
	noAfterUndoStop bool
}

// lineSuffix follows the text right of the cursor from the moment the widget was asked for, so that the characters that
// appear there while the user types (the closing bracket the editor typed) are replaced with the word.
type lineSuffix struct {
	line, col int
	marker    int
	hasMarker bool
}

// newLineSuffix notes the character right of the cursor: a marker follows it when there is one.
func (vm *ViewModel) newLineSuffix() lineSuffix {
	pos := vm.cursors.Primary().Position()
	ls := lineSuffix{line: pos.Line, col: pos.Col}
	line, _ := vm.doc.Snapshot().LineContent(pos.Line)
	if pos.Col < len(line) {
		_, w := utf8.DecodeRuneInString(line[pos.Col:])
		next := pos.Col + w
		ls.marker = vm.doc.AddDecoration(vm.owner(suggestOwner), buffer.Range{Start: pos, End: buffer.Position{Line: pos.Line, Col: next}},
			document.DecorationOptions{Stickiness: document.NeverGrowsWhenTypingAtEdges})
		ls.hasMarker = true
	}
	return ls
}

func (l *lineSuffix) dispose(vm *ViewModel) {
	if l.hasMarker {
		vm.doc.RemoveDecorations(l.marker)
		l.hasMarker = false
	}
}

// delta is the number of bytes between the cursor and the text that was right of it when the widget was asked for.
func (l *lineSuffix) delta(vm *ViewModel, cursor buffer.Position) int {
	if cursor.Line != l.line {
		return 0
	}
	if l.hasMarker {
		if d, ok := vm.doc.Decoration(l.marker); ok && d.Range.Start.Line == cursor.Line {
			return d.Range.Start.Col - cursor.Col
		}
		return 0
	}
	line, _ := vm.doc.Snapshot().LineContent(cursor.Line)
	return len(line) - cursor.Col
}

// overwriteInfo is how many bytes before and after each cursor the item replaces: the word the item was asked for, and what
// was typed since, before the cursor; up to the end of the insert or the replace range, and the characters that appeared there,
// after it.
func (vm *ViewModel) overwriteInfo(it *langfeatures.SuggestItem, alternative bool) (before, after int) {
	replace := vm.opts.Suggest.InsertMode == "replace"
	if alternative {
		replace = !replace
	}
	cursor := vm.cursors.Primary().Position()
	before = it.Position.Col - it.EditStart
	end := it.InsertEnd
	if replace {
		end = it.ReplaceEnd
	}
	after = end - it.Position.Col
	before += cursor.Col - it.Position.Col
	if s := vm.sg.session; s != nil {
		after += s.suffix.delta(vm, cursor)
	}
	return max(before, 0), max(after, 0)
}

// pendingEdits are the additional edits of an accepted item that were not known when it was accepted: the provider is asked
// for them, and they are made when the answer comes if the text did not change before them meanwhile.
type pendingEdits struct {
	cancel context.CancelFunc
	// earliest is the first place of the text that changed since the item was accepted.
	earliest   buffer.Position
	hasChanged bool
	unsub      func()
	// closeAfter tells that the group of undo is closed when the edits are made.
	closeAfter bool
}

// suggestInsert accepts an item: the first undo stop, the additional edits, the text, the second undo stop and the command.
func (vm *ViewModel) suggestInsert(it *langfeatures.SuggestItem, o insertOptions) {
	sg := &vm.sg
	h := sg.host
	if h == nil || it == nil {
		vm.suggestCancel()
		return
	}
	if err := vm.checkEditable(); err != nil {
		vm.suggestCancel()
		vm.reportSuggest(err)
		return
	}
	before, after := vm.overwriteInfo(it, o.alternative)
	cursor := vm.cursors.Primary().Position()
	c := vm.currentLineCtx()
	h.Shared.Memory.Memorize(langfeatures.MemoryMode(vm.opts.Suggest.SuggestSelection), vm.doc.LanguageID(), c.word, it)
	sg.lastAccepted = it

	vm.doc.PushStackElement() // an undo stop before the additional edits
	closeAfter := !o.noAfterUndoStop
	var plan *pendingEdits
	switch {
	case it.AdditionalEdits != nil:
		vm.suggestCancel()
		if err := vm.applyAdditionalEdits(it.AdditionalEdits, shiftEdits(it, cursor)); err != nil {
			vm.reportSuggest(err)
		}
	case !it.Resolved && h.Features != nil && h.Features.CanResolveCompletion(it.CompletionItem):
		plan = &pendingEdits{closeAfter: closeAfter}
		closeAfter = false
	}
	text := it.EffectiveInsertText()
	if !it.Snippet {
		text = snippet.Escape(text)
	}
	vm.suggestCancel()
	opts := snippet.DefaultInsertOptions()
	opts.OverwriteBefore, opts.OverwriteAfter = before, after
	opts.AdjustWhitespace = !it.KeepWhitespace
	opts.UndoStopBefore, opts.UndoStopAfter, opts.JoinUndo = false, false, true
	if err := vm.insertCompletionText(text, opts); err != nil {
		vm.reportSuggest(err)
	}
	if plan != nil {
		vm.resolveForEdits(it, plan)
	}
	if closeAfter {
		vm.doc.PushStackElement()
	}
	vm.NotifyListeners()
	if cmd := it.Command; cmd != nil {
		if cmd.ID == triggerSuggestCommand {
			vm.suggestTrigger(triggerOptions{auto: true, retrigger: true})
		} else if h.Execute != nil {
			ctx := h.Ctx
			if ctx == nil {
				ctx = context.Background()
			}
			if err := h.Execute(ctx, *cmd); err != nil {
				vm.reportSuggest(err)
			}
		}
	}
}

// insertCompletionText inserts the text as a snippet at the cursors, reading the clipboard first when the text uses it.
func (vm *ViewModel) insertCompletionText(text string, opts snippet.InsertOptions) error {
	if !strings.Contains(text, "CLIPBOARD") || vm.clip == nil {
		return vm.insertSnippetNoRun(text, opts)
	}
	version, sels := vm.doc.Version(), vm.cursors.Selections()
	vm.clip.clipboard.Read(func(clip string, err error) {
		if err != nil || vm.doc.Version() != version || !buffer.SelectionsEqual(vm.cursors.Selections(), sels) {
			return
		}
		opts.ClipboardText = clip
		if err := vm.insertSnippetNoRun(text, opts); err != nil {
			vm.reportSuggest(err)
		}
		vm.SelectionsChanged()
		vm.RevealPrimary()
	})
	return nil
}

// insertSnippetNoRun is insertSnippet without the sessions of Run: the caller tells the view about the cursors.
func (vm *ViewModel) insertSnippetNoRun(text string, opts snippet.InsertOptions) error {
	vm.dismissSnippetPicker(false)
	opts.Overtyped = vm.overtypedBy
	err := vm.snippets().Insert(text, opts)
	vm.history.Clear()
	vm.maxLineWidth = 0
	vm.Sync()
	return err
}

// shiftEdits moves the additional edits that come after the place the item was asked at by the characters typed since, as VS
// Code does: an import above is not moved, an edit on the line after the cursor is.
func shiftEdits(it *langfeatures.SuggestItem, cursor buffer.Position) func(langfeatures.TextEdit) langfeatures.TextEdit {
	delta := cursor.Col - it.Position.Col
	return func(e langfeatures.TextEdit) langfeatures.TextEdit {
		if e.Range.Start.Line == it.Position.Line && e.Range.Start.Col > it.Position.Col {
			e.Range.Start.Col += delta
			if e.Range.Start.Line == e.Range.End.Line {
				e.Range.End.Col += delta
			}
		}
		return e
	}
}

// applyAdditionalEdits makes the edits as part of the open undo group; the cursors follow the text they were in.
func (vm *ViewModel) applyAdditionalEdits(edits []langfeatures.TextEdit, adjust func(langfeatures.TextEdit) langfeatures.TextEdit) error {
	if len(edits) == 0 {
		return nil
	}
	batch := make([]document.Edit, len(edits))
	for i, e := range edits {
		if adjust != nil {
			e = adjust(e)
		}
		batch[i] = document.Edit{Range: e.Range, Text: e.Text}
	}
	sels := vm.cursors.Selections()
	restore := vm.trackSelections(sels)
	_, err := vm.doc.Edit(batch, document.EditOptions{Join: true, Before: sels})
	moved := restore()
	if err != nil {
		return err
	}
	vm.cursors.SetSelections(moved)
	vm.maxLineWidth = 0
	vm.Sync()
	return nil
}

// trackSelections follows the selections through the edits that come next; it returns the function that gives where they are.
func (vm *ViewModel) trackSelections(sels []buffer.Selection) (current func() []buffer.Selection) {
	ids := make([][2]int, len(sels))
	opts := document.DecorationOptions{Stickiness: document.NeverGrowsWhenTypingAtEdges}
	for i, s := range sels {
		ids[i][0] = vm.doc.AddDecoration(vm.owner(trackOwner), buffer.Range{Start: s.Anchor, End: s.Anchor}, opts)
		ids[i][1] = vm.doc.AddDecoration(vm.owner(trackOwner), buffer.Range{Start: s.Active, End: s.Active}, opts)
	}
	return func() []buffer.Selection {
		out := make([]buffer.Selection, len(sels))
		for i, s := range sels {
			out[i] = s
			if d, ok := vm.doc.Decoration(ids[i][0]); ok {
				out[i].Anchor = d.Range.Start
			}
			if d, ok := vm.doc.Decoration(ids[i][1]); ok {
				out[i].Active = d.Range.Start
			}
			vm.doc.RemoveDecorations(ids[i][0], ids[i][1])
		}
		return out
	}
}

// resolveForEdits asks for the details of an item that was accepted before it had its additional edits (an import), and makes
// them when they come. They are not made when the text changed before them since, or when the document is gone.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestController.ts (_insertSuggestion, the async edits)
func (vm *ViewModel) resolveForEdits(it *langfeatures.SuggestItem, plan *pendingEdits) {
	h := vm.sg.host
	base := h.Ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	plan.cancel = cancel
	vm.sg.pending = plan
	plan.unsub = vm.doc.Subscribe(func(ev document.ChangeEvent) {
		for _, ch := range ev.Changes {
			end := ch.OldRange.End
			if !plan.hasChanged || end.IsBefore(plan.earliest) {
				plan.earliest, plan.hasChanged = end, true
			}
		}
	})
	doc := vm.langDocument()
	item := it.CompletionItem
	go func() {
		merged, err := h.Features.ResolveCompletion(ctx, doc, item)
		vm.post(func() { vm.finishPendingEdits(ctx, plan, it, merged, err) })
	}()
}

// finishPendingEdits makes the additional edits that a resolve brought.
func (vm *ViewModel) finishPendingEdits(ctx context.Context, plan *pendingEdits, it *langfeatures.SuggestItem, merged langfeatures.CompletionItem, err error) {
	if vm.sg.pending == plan {
		vm.sg.pending = nil
	}
	plan.unsub()
	cancelled := ctx.Err() != nil
	plan.cancel()
	if err != nil || cancelled || len(merged.AdditionalEdits) == 0 {
		if plan.closeAfter || plan.hasChanged {
			vm.doc.PushStackElement()
		}
		return
	}
	it.CompletionItem = merged
	it.Resolved = true
	if plan.hasChanged {
		for _, e := range merged.AdditionalEdits {
			if plan.earliest.IsBefore(e.Range.Start) {
				vm.doc.PushStackElement()
				return // the text changed before the edit: its places are not those of the text any more
			}
		}
		vm.doc.PushStackElement() // the edits are a step of their own: the user typed in between
	}
	if err := vm.applyAdditionalEdits(merged.AdditionalEdits, nil); err != nil {
		vm.reportSuggest(err)
	}
	vm.doc.PushStackElement()
	vm.NotifyListeners()
}

// reportSuggest shows a failure of the widget.
func (vm *ViewModel) reportSuggest(err error) {
	if err == nil || vm.sg.host == nil || vm.sg.host.Report == nil {
		return
	}
	vm.sg.host.Report(err)
}
