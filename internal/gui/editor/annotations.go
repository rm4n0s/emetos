package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// Line annotations (SPEC §10.30 "Blame", step 24.22): a muted text after the end of a line, which an extension puts there (the Git
// extension's blame text, VS Code's `after` decoration of the blame controller), with a hover. The text is drawn by the text layer after
// the text of the last row of the line, in the font of the editor; it moves nothing (the rows, the cursors and the width of the content are
// those of the text alone), is not selectable and is clipped at the edge of the editor. The annotations are decorations of the document, so
// they follow its edits until the extension sets new ones.
//
// Ported from VS Code: extensions/git/src/blame.ts (GitBlameEditorDecoration: the `after` decoration with a margin of 50 pixels and the hover
// at the end of the line)

// annotationsOwner is the slot of the decorations of the line annotations (see ViewModel.owner).
const annotationsOwner = 13

// annotationMargin is the space between the end of the text of a line and its annotation (VS Code's margin of 50px); a second annotation
// of the same line follows the first after annotationGap.
const (
	annotationMargin = 50.0
	annotationGap    = 12.0
)

// LineAnnotation is a text after the end of a line.
type LineAnnotation struct {
	// Line is the line, counted from 0.
	Line int
	// Text is the text to draw.
	Text string
	// Color is the id of the theme color of the text; empty, or one the theme does not know, draws it in editorInlayHint.foreground.
	Color string
	// Hover is the markdown of the hover; empty for none.
	Hover string
	// Link is called with the destination of a link of the hover that names a command (command:<id>?<arguments>); other links open
	// as the links of every hover do. Nil lets such a link do nothing.
	Link func(dest string)
}

// annotationEntry is an annotation as the view model holds it.
type annotationEntry struct {
	LineAnnotation
}

// annotationState is the line annotations of a view model.
type annotationState struct {
	// ids are the decorations that carry the annotations and entries their annotations by decoration id.
	ids     []int
	entries map[int]annotationEntry
	// generation counts the changes, and cachedGen and cachedVersion say for which the lines were worked out.
	generation, cachedGen, cachedVersion int
	cached                               map[int][]annotationEntry
}

// SetLineAnnotations replaces the line annotations of the editor with the list. A line the document does not have is not kept. The
// annotations are decorations that this editor owns, so another editor of the same document is given its own list.
func (vm *ViewModel) SetLineAnnotations(list []LineAnnotation) {
	st := &vm.annot
	snap := vm.doc.Snapshot()
	count := snap.LineCount()
	add := make([]document.NewDecoration, 0, len(list))
	kept := make([]annotationEntry, 0, len(list))
	for _, an := range list {
		if an.Line < 0 || an.Line >= count || an.Text == "" {
			continue
		}
		text, err := snap.LineContent(an.Line)
		if err != nil {
			continue
		}
		at := buffer.Position{Line: an.Line, Col: len(text)}
		add = append(add, document.NewDecoration{Range: buffer.Range{Start: at, End: at}, Options: document.DecorationOptions{
			// the start stays before what is typed at the end of the line, so the annotation stays on its line when Enter is pressed there
			Stickiness: document.AlwaysGrowsWhenTypingAtEdges, ClassName: "lineAnnotation",
		}})
		kept = append(kept, annotationEntry{an})
	}
	if len(add) == 0 && len(st.ids) == 0 {
		return
	}
	if vm.hover.shownAnn != nil || vm.hover.op != nil && vm.hover.op.anchor.ann != nil {
		vm.HideHover()
	}
	st.ids = vm.doc.DeltaDecorations(vm.owner(annotationsOwner), st.ids, add)
	st.entries = make(map[int]annotationEntry, len(kept))
	for i, id := range st.ids {
		st.entries[id] = kept[i]
	}
	st.generation++
	vm.NotifyListeners()
}

// LineAnnotationCount is the number of line annotations the editor holds.
func (vm *ViewModel) LineAnnotationCount() int { return len(vm.annot.ids) }

// DisposeLineAnnotations takes the line annotations off the document.
func (vm *ViewModel) DisposeLineAnnotations() {
	vm.doc.RemoveDecorationsOf(vm.owner(annotationsOwner))
	vm.annot = annotationState{generation: vm.annot.generation + 1}
}

// annotationsByLine are the annotations by the line they are on now (their decorations moved with the edits), in the order of the list;
// it is worked out again when the text or the list changed.
func (vm *ViewModel) annotationsByLine() map[int][]annotationEntry {
	st := &vm.annot
	if len(st.ids) == 0 {
		return nil
	}
	version := vm.doc.Version()
	if st.cached != nil && st.cachedGen == st.generation && st.cachedVersion == version {
		return st.cached
	}
	byLine := map[int][]annotationEntry{}
	for _, d := range vm.doc.DecorationsOf(vm.owner(annotationsOwner)) {
		if e, ok := st.entries[d.ID]; ok {
			line := d.Range.Start.Line
			byLine[line] = append(byLine[line], e)
		}
	}
	st.cached, st.cachedGen, st.cachedVersion = byLine, st.generation, version
	return byLine
}

// LineAnnotations are the annotations of the editor on the lines they are on now, in the order of the lines.
func (vm *ViewModel) LineAnnotations() []LineAnnotation {
	by := vm.annotationsByLine()
	var out []LineAnnotation
	for line := range vm.doc.Snapshot().LineCount() {
		for _, e := range by[line] {
			a := e.LineAnnotation
			a.Line = line
			out = append(out, a)
		}
	}
	return out
}

// annotationText is the text of an annotation as it is drawn: one line, cut where it would be far wider than any editor.
func annotationText(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	if utf8.RuneCountInString(s) > 1000 {
		s = string([]rune(s)[:1000])
	}
	return s
}

// paintAnnotations draws the annotations of a row, when it is the last row of its line: each after the end of the text of the row.
func (r *RenderEditorText) paintAnnotations(canvas *ui.Canvas, g geometry, by map[int][]annotationEntry, row int, left, top float64) {
	line, _, _ := g.proj.RowSpan(row)
	list := by[line]
	if len(list) == 0 || g.isZoneRow(row) || !g.lastRowOfLine(row) {
		return
	}
	x := left + g.rowEnd(row) + annotationMargin
	for _, e := range list {
		p := r.annotationParagraph(g, e)
		if p == nil {
			continue
		}
		// the paragraph has the line height of a row of text, so the text sits on the baseline of the text of the line
		canvas.DrawParagraph(p, ui.Offset{Dx: x, Dy: top})
		x += p.LongestLine() + annotationGap
	}
}

// annotationParagraph is the paragraph of the text of an annotation in its color.
func (r *RenderEditorText) annotationParagraph(g geometry, e annotationEntry) *ui.Paragraph {
	text := annotationText(e.Text)
	if text == "" {
		return nil
	}
	style := newTextStyle(r.factory, r.vm.Options(), r.annotationColorOf(e.Color))
	return style.paragraph(r.vm.Paragraphs(), text)
}

// annotationColorOf is the color of an annotation: the theme color it names, else the one of the inlay hints.
func (r *RenderEditorText) annotationColorOf(id string) ui.Color {
	if id != "" && r.namedColor != nil {
		if c, ok := r.namedColor(id); ok {
			return c
		}
	}
	return r.annotationColor
}

// SetAnnotationColors gives the layer the colors of the annotations: fallback is the color of an annotation that names none the theme
// knows, named finds the color of a theme color id.
func (r *RenderEditorText) SetAnnotationColors(fallback ui.Color, named func(id string) (ui.Color, bool)) {
	r.namedColor = named
	if fallback == r.annotationColor {
		r.MarkNeedsPaint()
		return
	}
	r.annotationColor = fallback
	r.MarkNeedsPaint()
}

// annotationHit says which annotation a point is on.
type annotationHit struct {
	line, index int
}

// AnnotationAt finds the annotation at a point of the text layer, given in the layer's own coordinates: the line of the annotation and its
// place among those of the line; false when the point is not on the text of an annotation.
func (r *RenderEditorText) AnnotationAt(local ui.Offset) (annotationHit, bool) {
	by := r.vm.annotationsByLine()
	if len(by) == 0 {
		return annotationHit{}, false
	}
	g := newGeometry(r.vm, r.factory)
	row := g.proj.RowAt(local.Dy + r.top())
	if row < 0 || row >= g.proj.RowCount() || g.isZoneRow(row) || !g.lastRowOfLine(row) {
		return annotationHit{}, false
	}
	line, _, _ := g.proj.RowSpan(row)
	list := by[line]
	if len(list) == 0 {
		return annotationHit{}, false
	}
	if top := g.proj.RowTop(row) - r.top(); local.Dy < top || local.Dy >= top+g.proj.RowHeight(row) {
		return annotationHit{}, false
	}
	x := g.rowEnd(row) + annotationMargin - r.vm.ScrollLeft()
	for i, e := range list {
		p := r.annotationParagraph(g, e)
		if p == nil {
			continue
		}
		w := p.LongestLine()
		if local.Dx >= x && local.Dx < x+w {
			return annotationHit{line: line, index: i}, true
		}
		x += w + annotationGap
	}
	return annotationHit{}, false
}

// annotationAnchor is the annotation a hover is about.
type annotationAnchor struct {
	line, index int
	entry       annotationEntry
}

// HoverAnnotationMoved tells the hover that the pointer rests on an annotation: after editor.hover.delay the markdown of its hover shows.
// An annotation with no hover shows nothing. A hover that shows for the annotation keeps showing.
func (vm *ViewModel) HoverAnnotationMoved(hit annotationHit) {
	h := &vm.hover
	by := vm.annotationsByLine()
	if hit.index >= len(by[hit.line]) || !vm.hoverEnabled() || vm.hoverMouseBlocked() || h.focused {
		return
	}
	e := by[hit.line][hit.index]
	cur := h.shownAnn
	if h.op != nil && h.op.anchor.ann != nil {
		cur = h.op.anchor.ann
	}
	if cur != nil && cur.line == hit.line && cur.index == hit.index {
		vm.cancelHoverHide()
		vm.cancelHoverReact()
		return
	}
	vm.cancelHoverHide()
	vm.cancelHoverReact()
	vm.hoverHideShown()
	if e.Hover == "" {
		vm.HideHover()
		return
	}
	end := vm.lineEndOf(buffer.Position{Line: hit.line})
	vm.hoverStart(hoverAnchor{rng: buffer.Range{Start: end, End: end}, pos: end, ann: &annotationAnchor{line: hit.line, index: hit.index, entry: e}}, false, false)
}

// annotationHoverParts is the part of the hover of an annotation: its markdown, and the way its links are followed.
func annotationHoverParts(a hoverAnchor) []HoverPart {
	e := a.ann.entry
	if e.Hover == "" {
		return nil
	}
	return []HoverPart{{
		Kind: HoverPartMarkdown, Contents: []langfeatures.Markup{{Value: e.Hover, Markdown: true}}, Range: a.rng,
		order: [2]int{hoverOrdinalMarkdown, 0}, Links: e.Link, CommandLinks: true,
	}}
}
