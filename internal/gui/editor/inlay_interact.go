package editor

import (
	"context"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// The hover and the click on an inlay hint (SPEC §9.12 "As built (step 12.9)"): the pointer resting on a part of a hint shows its
// tooltip, with the way to follow it (go to the definition a location part names, run the command it has); Ctrl+click follows it.
// A hint that the server left unresolved is resolved first.
//
// Ported from VS Code: src/vs/editor/contrib/inlayHints/browser/inlayHintsHover.ts and inlayHintsController.ts (_installLinkGesture)

// hintAnchor is the part of a hint a hover is about.
type hintAnchor struct {
	entry *inlayEntry
	part  int
	// hint is the hint as it was when the hover began, with what a resolve brought.
	hint langfeatures.InlayHint
}

// HintAt finds the inlay hint at a point of the text layer, given in the layer's own coordinates, and the label part it is on
// (-1 on its padding); false when the point is not on a hint.
func (r *RenderEditorText) HintAt(local ui.Offset) (entryID, part int, ok bool) {
	if len(r.vm.inlay.ids) == 0 || !r.vm.InlayHintsShown() {
		return 0, 0, false
	}
	g := newGeometry(r.vm, r.factory)
	row := g.proj.RowAt(local.Dy + r.top())
	if g.isZoneRow(row) {
		return 0, 0, false
	}
	inj, part, ok := g.hintAt(row, local.Dx+r.vm.ScrollLeft())
	if !ok {
		return 0, 0, false
	}
	return inj.entry.id, part, true
}

// HoverHintMoved tells the hover that the pointer rests on a part of the inlay hint with an id: after editor.hover.delay the
// tooltip of the part shows. A hover that shows for the part keeps showing.
func (vm *ViewModel) HoverHintMoved(entryID, part int) {
	h := &vm.hover
	e := vm.inlay.entries[entryID]
	if e == nil || !vm.hoverEnabled() || vm.hoverMouseBlocked() || h.focused {
		return
	}
	cur := h.shownHint
	if h.op != nil && h.op.anchor.hint != nil {
		cur = h.op.anchor.hint
	}
	if cur != nil && cur.entry == e && cur.part == part {
		vm.cancelHoverHide()
		vm.cancelHoverReact()
		return
	}
	d, ok := vm.doc.Decoration(entryID)
	if !ok {
		return
	}
	vm.cancelHoverHide()
	vm.cancelHoverReact()
	vm.hoverHideShown()
	pos := d.Range.Start
	vm.hoverStart(hoverAnchor{rng: buffer.Range{Start: pos, End: pos}, pos: pos, hint: &hintAnchor{entry: e, part: part, hint: e.hint}}, false, false)
}

// hintPartOf is the part of the hint of an anchor, or false for the padding or a part the hint does not have.
func (a *hintAnchor) partOf() (langfeatures.InlayHintPart, bool) {
	if a.part < 0 || a.part >= len(a.hint.Parts) {
		return langfeatures.InlayHintPart{}, false
	}
	return a.hint.Parts[a.part], true
}

// hintHoverParts are the parts of the hover of a hint: the tooltip of the hint and of its label part, and what a Ctrl+click on
// the part does.
func hintHoverParts(a hoverAnchor) []HoverPart {
	h := a.hint
	var contents []langfeatures.Markup
	part, hasPart := h.partOf()
	if hasPart && part.Tooltip != nil {
		contents = append(contents, *part.Tooltip)
	}
	if h.hint.Tooltip != nil {
		contents = append(contents, *h.hint.Tooltip)
	}
	if hasPart && part.Location != nil {
		contents = append(contents, langfeatures.Markup{Value: "Go to Definition (Ctrl+Click)"})
	}
	if hasPart && part.Command != nil && part.Command.Title != "" {
		contents = append(contents, langfeatures.Markup{Value: part.Command.Title + " (Ctrl+Click)"})
	}
	if len(contents) == 0 {
		return nil
	}
	return []HoverPart{{Kind: HoverPartMarkdown, Contents: contents, Owner: h.hint.Owner, Range: a.rng, order: [2]int{hoverOrdinalMarkdown, 0}}}
}

// hoverHintAsync resolves the hint of an op whose tooltip, places and commands the server left out, and shows what it brings.
func (vm *ViewModel) hoverHintAsync(op *hoverOp) {
	f := vm.hoverFeatures()
	a := op.anchor.hint
	if f == nil || !f.CanResolveInlayHint(a.hint) {
		op.asyncDone = true
		return
	}
	ctx := context.Background()
	if h := vm.sg.host; h != nil && h.Ctx != nil {
		ctx = h.Ctx
	}
	ctx, op.cancelRequests = context.WithCancel(ctx)
	doc, gen := vm.langDocument(), op.gen
	go func() {
		got, err := f.ResolveInlayHint(ctx, doc, a.hint)
		vm.post(func() {
			if vm.hover.op != op || op.gen != gen || vm.doc.Version() != doc.Version {
				return
			}
			if err != nil {
				if h := vm.sg.host; h != nil && h.Log != nil && !errors.Is(err, context.Canceled) {
					h.Log("inlay hint resolve failed", err)
				}
			} else {
				a.hint = got
				if e := vm.inlay.entries[a.entry.id]; e != nil {
					e.hint = got
				}
				op.results = append(op.results[:0], hintHoverParts(op.anchor)...)
			}
			op.asyncDone = true
			if op.state == hoverWaitingAsync || op.state == hoverWaitingAsyncLoading {
				op.state = hoverIdle
			}
			vm.hoverFire(op)
		})
	}()
}

// HintClick handles a click on a part of an inlay hint with the modifier of the links held (Ctrl): the location of the part is
// shown, or its command run. It reports whether it did something, so that the click does not move the cursors; a hint the
// server left unresolved is resolved first.
func (vm *ViewModel) HintClick(entryID, part int, m MouseModifiers) bool {
	e := vm.inlay.entries[entryID]
	if e == nil || !vm.linkModifier(m) {
		return false
	}
	h := vm.sg.host
	if h == nil {
		return false
	}
	follow := func(hint langfeatures.InlayHint) bool {
		if part < 0 || part >= len(hint.Parts) {
			return false
		}
		p := hint.Parts[part]
		switch {
		case p.Location != nil && h.Open != nil:
			h.Open(*p.Location, OpenLocation{Side: vm.linkSide(m)})
			return true
		case p.Command != nil && h.Execute != nil && p.Command.ID != "":
			ctx := h.Ctx
			if ctx == nil {
				ctx = context.Background()
			}
			if err := h.Execute(ctx, *p.Command); err != nil && h.Report != nil {
				h.Report(err)
			}
			return true
		}
		return false
	}
	if follow(e.hint) {
		return true
	}
	f := vm.hoverFeatures()
	if f == nil || !f.CanResolveInlayHint(e.hint) {
		return false
	}
	ctx := h.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	doc := vm.langDocument()
	go func() {
		got, err := f.ResolveInlayHint(ctx, doc, e.hint)
		vm.post(func() {
			if err != nil {
				if h.Log != nil && !errors.Is(err, context.Canceled) {
					h.Log("inlay hint resolve failed", err)
				}
				return
			}
			if cur := vm.inlay.entries[entryID]; cur != nil {
				cur.hint = got
			}
			follow(got)
		})
	}()
	return true
}
