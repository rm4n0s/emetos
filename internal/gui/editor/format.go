package editor

import (
	"context"
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// Formatting in the editor (SPEC §9.12 "Formatting", "As built (step 12.8)"): Format Document and Format Selection ask the
// formatter of the language off the UI thread and make its edits as one undo step with the cursors following the text; the
// characters a server declares format the line after they are typed; a paste is formatted as a range. The edits a save makes
// are the window's (gui/app/saveformat.go) and use ApplyTextEdits.
//
// Ported from VS Code: src/vs/editor/contrib/format/browser/formatActions.ts, format.ts and formattingEdit.ts

// FormatOptions are the settings of the formatting that follows typing and pasting.
type FormatOptions struct {
	// OnType is editor.formatOnType and OnPaste editor.formatOnPaste.
	OnType, OnPaste bool
	// DefaultFormatter is editor.defaultFormatter: the id of the extension whose formatter is used when a language has several.
	DefaultFormatter string
}

// formatState is what a view model keeps for its formatting.
type formatState struct {
	// cancel ends the explicit request in flight and gen numbers them; a newer request replaces the older.
	cancel context.CancelFunc
	gen    int
}

// FormattingOptions are the options a formatter is given: the indentation of the document.
func (vm *ViewModel) FormattingOptions() langfeatures.FormattingOptions {
	tab, spaces := vm.doc.Indentation()
	return langfeatures.FormattingOptions{TabSize: tab, InsertSpaces: spaces}
}

// HasDocumentFormatter tells whether a formatter matches the document (the key editorHasDocumentFormattingProvider).
func (vm *ViewModel) HasDocumentFormatter() bool {
	f := vm.hoverFeatures()
	return f != nil && f.Has(vm.langDocument(), langfeatures.FeatureFormatting)
}

// HasRangeFormatter tells whether a formatter that formats ranges matches the document (the key
// editorHasDocumentSelectionFormattingProvider).
func (vm *ViewModel) HasRangeFormatter() bool {
	f := vm.hoverFeatures()
	return f != nil && f.Has(vm.langDocument(), langfeatures.FeatureRangeFormatting)
}

// formatRun starts an explicit request: the one before it, if still running, is cancelled and its answer dropped.
func (vm *ViewModel) formatRun(ask func(ctx context.Context) ([]langfeatures.TextEdit, error), minimalOf langfeatures.Document) {
	h := vm.sg.host
	if h == nil || h.Features == nil || vm.ReadOnly() {
		return
	}
	st := &vm.fm
	if st.cancel != nil {
		st.cancel()
	}
	st.gen++
	gen, version := st.gen, minimalOf.Version
	ctx, cancel := context.WithCancel(vm.hostCtx())
	st.cancel = cancel
	pos := vm.cursors.Primary().Position()
	go func() {
		edits, err := ask(ctx)
		if err == nil {
			edits = langfeatures.MinimalEdits(minimalOf.Text, edits)
		}
		vm.post(func() {
			if gen != st.gen {
				return
			}
			st.cancel()
			st.cancel = nil
			// the answer is for the text and the cursor of the request (VS Code's CodeEditorStateFlag.Value | Position)
			if vm.doc.Version() != version || vm.cursors.Primary().Position() != pos {
				return
			}
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					vm.reportError(err)
				}
				return
			}
			if len(edits) == 0 {
				return
			}
			if err := vm.ApplyTextEdits(edits, false); err != nil {
				vm.reportError(err)
				return
			}
			vm.RevealPositionInCenterIfOutsideViewport(vm.cursors.Primary().Position())
		})
	}()
}

// FormatDocument formats the whole document with the formatter of its language (editor.action.formatDocument). It says whether
// a request was made.
//
// Ported from VS Code: formatActions.ts (FormatDocumentAction), format.ts (formatDocumentWithSelectedProvider)
func (vm *ViewModel) FormatDocument() bool {
	h := vm.sg.host
	if h == nil || h.Features == nil || vm.ReadOnly() || !vm.HasDocumentFormatter() {
		return false
	}
	doc, opts, def := vm.langDocument(), vm.FormattingOptions(), vm.opts.Format.DefaultFormatter
	reg := h.Features
	vm.formatRun(func(ctx context.Context) ([]langfeatures.TextEdit, error) {
		return reg.FormatDocument(ctx, doc, opts, def)
	}, doc)
	return true
}

// FormatSelection formats the selections, each empty one as its whole line (editor.action.formatSelection). It says whether a
// request was made.
//
// Ported from VS Code: formatActions.ts (FormatSelectionAction), format.ts (formatDocumentRangesWithProvider)
func (vm *ViewModel) FormatSelection() bool {
	h := vm.sg.host
	if h == nil || h.Features == nil || vm.ReadOnly() || !vm.HasRangeFormatter() {
		return false
	}
	snap := vm.doc.Snapshot()
	var ranges []buffer.Range
	for _, s := range vm.cursors.Selections() {
		r := s.Range()
		if s.IsEmpty() {
			n, _ := snap.LineLength(r.Start.Line)
			r = buffer.Range{Start: buffer.Position{Line: r.Start.Line}, End: buffer.Position{Line: r.Start.Line, Col: n}}
		}
		ranges = append(ranges, r)
	}
	doc, opts, def := vm.langDocument(), vm.FormattingOptions(), vm.opts.Format.DefaultFormatter
	reg := h.Features
	vm.formatRun(func(ctx context.Context) ([]langfeatures.TextEdit, error) {
		return formatRanges(ctx, reg, doc, ranges, opts, def)
	}, doc)
	return true
}

// formatRanges asks the range formatter for each of the ranges; ranges that touch or overlap are one.
func formatRanges(ctx context.Context, reg *langfeatures.Registry, doc langfeatures.Document, ranges []buffer.Range, opts langfeatures.FormattingOptions, def string) ([]langfeatures.TextEdit, error) {
	var all []langfeatures.TextEdit
	for _, r := range MergeRanges(ranges) {
		edits, err := reg.FormatRange(ctx, doc, r, opts, def)
		if err != nil {
			return nil, err
		}
		all = append(all, edits...)
	}
	return all, nil
}

// MergeRanges sorts the ranges and joins the ones that overlap or touch (VS Code's formatDocumentRangesWithProvider).
func MergeRanges(ranges []buffer.Range) []buffer.Range {
	sorted := slices.Clone(ranges)
	slices.SortFunc(sorted, buffer.CompareUsingStarts)
	var out []buffer.Range
	for _, r := range sorted {
		if n := len(out); n > 0 && !out[n-1].End.IsBefore(r.Start) {
			if out[n-1].End.IsBefore(r.End) {
				out[n-1].End = r.End
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// ApplyTextEdits makes the edits of a formatter or a server in the document of the editor as one undo step, and the cursors
// follow the text they were in. With join the edits are part of the undo group that is open (the participants of a save make
// one); otherwise they are a step of their own, which typing before and after does not join. An edit that is not valid for
// the text fails with the document's error and changes nothing.
//
// Ported from VS Code: formattingEdit.ts (FormattingEdit.execute)
func (vm *ViewModel) ApplyTextEdits(edits []langfeatures.TextEdit, join bool) error {
	if len(edits) == 0 {
		return nil
	}
	if err := vm.checkEditable(); err != nil {
		return err
	}
	batch := make([]document.Edit, len(edits))
	for i, e := range edits {
		batch[i] = document.Edit{Range: e.Range, Text: e.Text}
	}
	sels := vm.cursors.Selections()
	restore := vm.trackSelections(sels)
	if !join {
		vm.doc.PushStackElement()
	}
	_, err := vm.doc.Edit(batch, document.EditOptions{Kind: document.KindOther, Before: sels, Join: join})
	moved := restore()
	if err != nil {
		return err
	}
	if !join {
		vm.doc.PushStackElement()
	}
	vm.cursors.SetSelections(moved)
	vm.history.Clear()
	vm.maxLineWidth = 0
	vm.Sync()
	vm.NotifyListeners()
	return nil
}

// ---- on type ----

// formatOnTypeAfter asks for the formatting that follows a typed character when the language declares it as a trigger
// (editor.formatOnType): only with one empty selection. The edits are dropped when the text at or above the line of the cursor
// changed meanwhile.
//
// Ported from VS Code: formatActions.ts (FormatOnType)
func (vm *ViewModel) formatOnTypeAfter(typed string) {
	h := vm.sg.host
	if typed == "" || !vm.opts.Format.OnType || h == nil || h.Features == nil || vm.ReadOnly() {
		return
	}
	sels := vm.cursors.Selections()
	if len(sels) != 1 || !sels[0].IsEmpty() {
		return
	}
	runes := []rune(typed)
	ch := string(runes[len(runes)-1])
	doc := vm.langDocument()
	reg := h.Features
	if !slices.Contains(reg.OnTypeFormattingTriggers(doc), ch) {
		return
	}
	pos := sels[0].Active
	opts := vm.FormattingOptions()
	ctx, cancel := context.WithCancel(vm.hostCtx())
	stale := false
	unsub := vm.doc.Subscribe(func(ev document.ChangeEvent) {
		for _, c := range ev.Changes {
			if c.OldRange.End.Line <= pos.Line {
				stale = true
				cancel()
				return
			}
		}
	})
	go func() {
		edits, err := reg.FormatOnType(ctx, doc, pos, ch, opts)
		if err == nil {
			edits = langfeatures.MinimalEdits(doc.Text, edits)
		}
		vm.post(func() {
			unsub()
			cancel()
			if stale || err != nil || len(edits) == 0 {
				if err != nil && !errors.Is(err, context.Canceled) && h.Log != nil {
					h.Log("format on type failed", err)
				}
				return
			}
			// a text that changed below the line since the request is still the text the edits are for
			if err := vm.ApplyTextEdits(edits, false); err != nil {
				vm.reportError(err)
			}
		})
	}()
}

// ---- on paste ----

// formatPasted formats the text a paste put in the range (editor.formatOnPaste) when a formatter that formats ranges is there.
// The edits are dropped when the text changed meanwhile.
//
// Ported from VS Code: formatActions.ts (FormatOnPaste)
func (vm *ViewModel) formatPasted(r buffer.Range) {
	h := vm.sg.host
	if !vm.opts.Format.OnPaste || h == nil || h.Features == nil || vm.ReadOnly() || len(vm.cursors.Selections()) > 1 || !vm.HasRangeFormatter() {
		return
	}
	doc, opts, def := vm.langDocument(), vm.FormattingOptions(), vm.opts.Format.DefaultFormatter
	reg := h.Features
	vm.formatRun(func(ctx context.Context) ([]langfeatures.TextEdit, error) {
		return formatRanges(ctx, reg, doc, []buffer.Range{r}, opts, def)
	}, doc)
}

// formatActions are the commands of formatting.
//
// Ported from VS Code: formatActions.ts
func formatActions(a map[string]action) {
	a["editor.action.formatDocument"] = func(vm *ViewModel, _ any) error {
		vm.keepSessions, vm.skipReveal = true, true
		vm.FormatDocument()
		return nil
	}
	a["editor.action.formatSelection"] = func(vm *ViewModel, _ any) error {
		vm.keepSessions, vm.skipReveal = true, true
		vm.FormatSelection()
		return nil
	}
	// the old command that does both
	a["editor.action.format"] = func(vm *ViewModel, args any) error {
		if vm.cursors.Primary().Selection.IsEmpty() {
			return a["editor.action.formatDocument"](vm, args)
		}
		return a["editor.action.formatSelection"](vm, args)
	}
}

// TrackCursors follows the cursors through the edits that someone else makes to the document from now on (a workspace edit of a
// server, a code action); the function it returns puts the cursors where the text took them. A document that did not change
// leaves the cursors alone.
func (vm *ViewModel) TrackCursors() (restore func()) {
	sels := vm.cursors.Selections()
	version := vm.doc.Version()
	current := vm.trackSelections(sels)
	return func() {
		moved := current()
		if vm.doc.Version() == version {
			return
		}
		vm.cursors.SetSelections(moved)
		vm.history.Clear()
		vm.maxLineWidth = 0
		vm.Sync()
		vm.NotifyListeners()
	}
}
