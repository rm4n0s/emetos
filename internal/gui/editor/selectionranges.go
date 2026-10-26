package editor

import (
	"context"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// Selection ranges (SPEC §9.12 "As built (step 12.9)"): Expand Selection (Shift+Alt+Right) steps through the ranges the language
// server gives around the cursors (textDocument/selectionRange) together with the word, bracket, string and line ranges the
// editor knows. The server is asked once, when the expansion starts; Shrink Selection goes back through the same steps.
//
// Ported from VS Code: src/vs/editor/contrib/smartSelect/browser/smartSelect.ts (SmartSelectController, provideSelectionRanges)

// selectionRangesTimeout is how long Expand Selection waits for the server before it goes on with the ranges it knows.
const selectionRangesTimeout = 2 * time.Second

// selectionRangeState is the request for the ranges that starts an expansion.
type selectionRangeState struct {
	gen    int
	cancel context.CancelFunc
}

// startsServerExpansion asks the selection range providers for the ranges around the cursors, when there is one for the
// document, and finishes the command when they answer: the command that starts an expansion returns at once and the expansion
// starts then, with the ranges of the providers among the others. It reports whether it is asking; an answer for a text that
// changed or cursors that moved meanwhile is dropped.
func (vm *ViewModel) startsServerExpansion() bool {
	f := vm.hoverFeatures()
	doc := vm.langDocument()
	if f == nil || vm.embedded || !f.Has(doc, langfeatures.FeatureSelectionRanges) {
		return false
	}
	st := &vm.selr
	if st.cancel != nil {
		st.cancel()
	}
	st.gen++
	gen := st.gen
	sels := vm.cursors.Selections()
	snap := vm.doc.Snapshot()
	positions := make([]buffer.Position, len(sels))
	for i, s := range sels {
		positions[i] = snap.ValidatePosition(s.Active)
	}
	base := context.Background()
	if h := vm.sg.host; h != nil && h.Ctx != nil {
		base = h.Ctx
	}
	ctx, cancel := context.WithTimeout(base, selectionRangesTimeout)
	st.cancel = cancel
	go func() {
		set, ok, errs := f.SelectionRanges(ctx, doc, positions)
		vm.post(func() {
			cancel()
			if vm.selr.gen != gen || vm.doc.Version() != doc.Version || !buffer.SelectionsEqual(vm.cursors.Selections(), sels) {
				return
			}
			vm.selr.cancel = nil
			if h := vm.sg.host; h != nil && h.Log != nil {
				for _, e := range errs {
					if !errors.Is(e.Err, context.Canceled) && !errors.Is(e.Err, context.DeadlineExceeded) {
						h.Log("selection range provider "+e.Owner+" failed", e.Err)
					}
				}
			}
			providers := vm.selectionProviders()
			if ok {
				providers = append([]edit.SelectionRangeProvider{set}, providers...)
			}
			_ = vm.Run(func(vm *ViewModel, _ any) error {
				vm.sessionOwner = sessionExpansion
				vm.expansion = vm.commands().NewExpansion(vm.cursors.Selections(), edit.DefaultSelectionRangesOptions(), providers...)
				if vm.expansion != nil {
					vm.setSelections(vm.expansion.Expand())
				}
				return nil
			}, nil)
		})
	}()
	return true
}

// SelectionRangesCancel ends the request for the ranges of an expansion that is starting.
func (vm *ViewModel) SelectionRangesCancel() {
	st := &vm.selr
	st.gen++
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
}
