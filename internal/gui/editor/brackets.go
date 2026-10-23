package editor

import (
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// maxBracketMatchCursors is how many cursors still get their brackets highlighted: with more,
// none do (VS Code's limit).
const maxBracketMatchCursors = 100

// BracketColors are the colors of the brackets (SPEC §8.5): one for each nesting level, which
// repeat past the sixth, and one for the brackets that have no partner. A transparent color
// leaves the bracket in the color of its token.
type BracketColors struct {
	Levels     [6]ui.Color
	Unexpected ui.Color
}

// SetBrackets gives the view model the bracket pairs of its document, which the commands
// of the brackets, the highlight of the matching bracket and the colors of the brackets use;
// nil takes them away. The view model does not own them. The index is kept up to date by
// whoever made it (brackets.Attach); the view model repaints with the document and the tokens.
func (vm *ViewModel) SetBrackets(ix *brackets.Index) {
	vm.bracketIndex = ix
	vm.NotifyListeners()
}

// Brackets are the bracket pairs of the document, or nil.
func (vm *ViewModel) Brackets() *brackets.Index { return vm.bracketIndex }

// selectionProviders are the providers of the steps of Expand Selection besides the words.
func (vm *ViewModel) selectionProviders() []edit.SelectionRangeProvider {
	var out []edit.SelectionRangeProvider
	if vm.bracketIndex != nil && vm.bracketIndex.Enabled() {
		out = append(out, edit.NewBracketSelectionRanges(vm.bracketIndex))
	}
	if tok := vm.tok.tokenizer; tok != nil && tok.Active() {
		out = append(out, edit.NewStringSelectionRanges(tok))
	}
	return out
}

// matchedBrackets are the brackets to highlight (editor.matchBrackets): while the editor has
// the focus, for each cursor that has no selection, the two brackets of the pair it is at; with
// "always", the brackets of the pair around it when it is at none.
//
// Ported from VS Code: src/vs/editor/contrib/bracketMatching/browser/bracketMatching.ts (_recomputeBrackets)
func (vm *ViewModel) matchedBrackets() []buffer.Range {
	ix := vm.bracketIndex
	mode := vm.opts.MatchBrackets
	if ix == nil || !ix.Enabled() || mode == "never" || !vm.focused {
		return nil
	}
	sels := vm.cursors.Selections()
	if len(sels) > maxBracketMatchCursors {
		return nil
	}
	var out []buffer.Range
	for _, s := range sels {
		if !s.IsEmpty() {
			continue
		}
		open, closing, ok := ix.Match(s.Active)
		if !ok && mode == "always" {
			open, closing, ok = ix.Enclosing(s.Active)
		}
		if ok {
			out = append(out, open, closing)
		}
	}
	return out
}

// bracketLook is the color a bracket is drawn in, or false for a bracket that keeps its token's.
func bracketLook(colors BracketColors, b brackets.BracketInfo, independent bool) (ui.Color, bool) {
	var c ui.Color
	switch {
	case b.Invalid || b.Level < 0:
		c = colors.Unexpected
	case independent:
		c = colors.Levels[b.EqualLevel%len(colors.Levels)]
	default:
		c = colors.Levels[b.Level%len(colors.Levels)]
	}
	return c, c.Alpha() > 0
}

// bracketSpans are the brackets of the bytes [start, end) of a line, as spans in the color of
// their level, with offsets in the row; the base spans give their font style.
func (vm *ViewModel) bracketSpans(colors BracketColors, line, start, end int) []span {
	ix := vm.bracketIndex
	if ix == nil || !ix.Enabled() || !vm.opts.BracketPairColorization {
		return nil
	}
	r := buffer.Range{Start: buffer.Position{Line: line, Col: start}, End: buffer.Position{Line: line, Col: end}}
	var out []span
	for _, b := range ix.Brackets(r, true) {
		// The query also gives the brackets that only touch the row.
		if b.Range.Start.Line != line || b.Range.Start.Col < start || b.Range.End.Col > end {
			continue
		}
		if c, ok := bracketLook(colors, b, vm.opts.IndependentColorPool); ok {
			out = append(out, span{start: b.Range.Start.Col - start, end: b.Range.End.Col - start, color: c})
		}
	}
	return out
}

// overlaySpans draws the over spans (sorted, apart) on top of the base spans: where they meet,
// the color is the over span's and the font style the base span's.
func overlaySpans(base, over []span) []span {
	if len(over) == 0 {
		return base
	}
	var out []span
	// cut adds the base spans between from and to.
	cut := func(from, to int) {
		for _, b := range base {
			s, e := max(b.start, from), min(b.end, to)
			if s < e {
				out = append(out, span{start: s, end: e, color: b.color, style: b.style})
			}
		}
	}
	at := 0
	for _, o := range over {
		cut(at, o.start)
		look := o
		for _, b := range base {
			if b.start <= o.start && o.start < b.end {
				look.style = b.style
				break
			}
		}
		out = append(out, look)
		at = o.end
	}
	last := at
	for _, b := range base {
		last = max(last, b.end)
	}
	cut(at, last)
	return out
}
