package editor

import (
	"context"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// The link that Ctrl+hover draws on a word that has a definition, and the click that follows it (SPEC §9.12 "Navigation",
// "As built (step 12.7)").
//
// Ported from VS Code: src/vs/editor/contrib/gotoSymbol/browser/link/goToDefinitionAtPosition.ts and clickLinkGesture.ts

// Link is the link under the pointer: the range of the word that has a definition, underlined while the modifier is held.
type Link struct {
	Range buffer.Range
	// Count is the number of definitions found.
	Count int
}

// linkState is what a view model knows of the link.
type linkState struct {
	// pointer is where the pointer rests, when it is over the text, and held says that the modifier of the link is held.
	pointer    buffer.Position
	hasPointer bool
	held       bool
	// word is the word a request is for, or the link shows on.
	word   buffer.Range
	shown  *Link
	gen    int
	cancel context.CancelFunc
}

func newLinkState() linkState { return linkState{} }

// Link is the link that shows, or nil.
func (vm *ViewModel) Link() *Link { return vm.link.shown }

// linkModifier tells whether the key that makes a link is held: Ctrl, or Alt when editor.multiCursorModifier is ctrlCmd (the
// other key makes the cursors).
//
// Ported from VS Code: clickLinkGesture.ts (ClickLinkOptions, triggerModifier)
func (vm *ViewModel) linkModifier(m MouseModifiers) bool {
	if vm.opts.MultiCursorModifier == "ctrlCmd" {
		return m.Alt
	}
	return m.Ctrl
}

// linkSide tells whether the key that opens a link beside is held too: Alt, or Ctrl with editor.multiCursorModifier ctrlCmd.
func (vm *ViewModel) linkSide(m MouseModifiers) bool {
	if vm.opts.MultiCursorModifier == "ctrlCmd" {
		return m.Ctrl
	}
	return m.Alt
}

// LinkPointerMoved tells the editor that the pointer rests on the text at a position, with the modifiers held. With the
// modifier of the link held over a word, the providers are asked for its definition and the word is underlined when they find
// one; without it the link goes. The request is cancelled when the pointer goes to another word.
func (vm *ViewModel) LinkPointerMoved(pos buffer.Position, m MouseModifiers) {
	l := &vm.link
	l.pointer, l.hasPointer, l.held = pos, true, vm.linkModifier(m)
	if !l.held {
		vm.LinkClear()
		return
	}
	vm.linkUpdate()
}

// LinkModifierChanged tells the editor that the key of the link went down or up while the pointer rested.
func (vm *ViewModel) LinkModifierChanged(held bool) {
	l := &vm.link
	l.held = held
	if !held {
		vm.LinkClear()
		return
	}
	if l.hasPointer {
		vm.linkUpdate()
	}
}

// LinkPointerLeft tells the editor that the pointer left the text.
func (vm *ViewModel) LinkPointerLeft() {
	vm.link.hasPointer = false
	vm.LinkClear()
}

// LinkClear takes the link away and cancels the request for it.
func (vm *ViewModel) LinkClear() {
	l := &vm.link
	l.gen++
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.word = buffer.Range{}
	if l.shown != nil {
		l.shown = nil
		vm.sel.NotifyListeners()
		vm.NotifyListeners() // the pointer over the text is not a link any more
	}
}

// linkWordAt is the word at a position, or false.
func (vm *ViewModel) linkWordAt(pos buffer.Position) (buffer.Range, bool) {
	line, err := vm.doc.Snapshot().LineContent(pos.Line)
	if err != nil || pos.Col >= len(line) {
		return buffer.Range{}, false
	}
	s, e, ok := vm.wordFunc()(line, pos.Col)
	if !ok || pos.Col < s || pos.Col >= e {
		return buffer.Range{}, false
	}
	return buffer.Range{Start: buffer.Position{Line: pos.Line, Col: s}, End: buffer.Position{Line: pos.Line, Col: e}}, true
}

// linkUpdate looks for the link under the pointer.
func (vm *ViewModel) linkUpdate() {
	l := &vm.link
	f := vm.hoverFeatures()
	doc := vm.langDocument()
	word, ok := vm.linkWordAt(l.pointer)
	if f == nil || !ok || !f.Has(doc, langfeatures.FeatureDefinition) {
		vm.LinkClear()
		return
	}
	if word == l.word && (l.shown != nil || l.cancel != nil) {
		return // the pointer moves inside the word: the link, or the request for it, stays
	}
	vm.LinkClear()
	l.word = word
	ctx := context.Background()
	if h := vm.sg.host; h != nil && h.Ctx != nil {
		ctx = h.Ctx
	}
	ctx, l.cancel = context.WithCancel(ctx)
	gen, version, pos := l.gen, doc.Version, l.pointer
	go func() {
		links, _ := f.Locations(ctx, doc, pos, langfeatures.NavDefinition)
		vm.post(func() {
			if vm.link.gen != gen || vm.doc.Version() != version || len(links) == 0 {
				return
			}
			vm.link.cancel = nil
			rng := word
			if o := links[0].Origin; o != nil {
				rng = vm.doc.Snapshot().ValidateRange(*o)
			}
			vm.link.shown = &Link{Range: rng, Count: len(links)}
			vm.sel.NotifyListeners()
			vm.NotifyListeners()
		})
	}()
}

// LinkClick handles a press of the primary button with the modifier of the link held: it puts the cursor at the position and
// goes to the definition of the word there (the peek view with editor.definitionLinkOpensInPeek; beside with the second key). It
// says whether it handled the press: it does not when the language has no definition provider or the press is not on a word,
// and the press is an ordinary one then.
//
// Ported from VS Code: goToDefinitionAtPosition.ts (GotoDefinitionAtPositionEditorContribution.gotoDefinition)
func (vm *ViewModel) LinkClick(pos buffer.Position, m MouseModifiers) bool {
	if !vm.linkModifier(m) {
		return false
	}
	f := vm.hoverFeatures()
	if f == nil || !f.Has(vm.langDocument(), langfeatures.FeatureDefinition) {
		return false
	}
	if _, ok := vm.linkWordAt(pos); !ok {
		return false
	}
	vm.LinkClear()
	vm.HideHover()
	vm.cursors.SetSelections([]buffer.Selection{buffer.NewCursor(pos)})
	vm.SelectionsChanged()
	return vm.GoTo(GoDefinition, GoConfig{
		Peek: vm.opts.GotoLocation.DefinitionLinkOpensInPeek && !vm.embedded, Side: vm.linkSide(m), Run: func(id string) error { return vm.RunCommand(id, nil) },
	})
}

// paintLink underlines the word of the link.
func (v overlayView) paintLink() {
	l := v.geo.vm.Link()
	if l == nil || v.colors.Link.Alpha() == 0 {
		return
	}
	paint := fill(v.colors.Link)
	for _, seg := range v.segments(Diagnostic{Range: l.Range}) {
		r := v.rowRect(seg.row, seg.x1, seg.x2)
		v.canvas.DrawRect(ui.NewRectFromLTRB(r.Left, r.Bottom-3, r.Right, r.Bottom-2), paint)
	}
}
