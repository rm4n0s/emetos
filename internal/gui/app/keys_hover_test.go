package app

import (
	"context"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// keysHover answers every hover request with a text; keysSignature every signature help request with two signatures.
type keysHover struct{}

func (keysHover) ProvideHover(context.Context, langfeatures.Document, langfeatures.Position) (*langfeatures.Hover, error) {
	return &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "the hover"}}}, nil
}

type keysSignature struct{ signatures int }

func (k keysSignature) ProvideSignatureHelp(context.Context, langfeatures.Document, langfeatures.Position, langfeatures.SignatureContext) (*langfeatures.SignatureHelp, error) {
	h := &langfeatures.SignatureHelp{}
	for range k.signatures {
		h.Signatures = append(h.Signatures, langfeatures.Signature{Label: "f(a)", Parameters: []langfeatures.Parameter{{Label: "a"}}})
	}
	return h, nil
}

// providersHost is a host of the editor with the hover and signature help providers of these tests.
func providersHost(a *keyApp, signatures int) *editor.SuggestHost {
	reg := langfeatures.NewRegistry()
	reg.RegisterHover("keys", langfeatures.Selector{}, keysHover{})
	reg.RegisterSignatureHelp("keys", langfeatures.Selector{}, keysSignature{signatures: signatures})
	return &editor.SuggestHost{Ctx: context.Background(), Features: reg, Shared: editor.NewSuggestShared()}
}

// hoverState is the state of an editor with a hover on screen: the pointer's after its delay, or the command's, with or
// without the keys of the box.
func hoverState(a *keyApp, pinned, focused bool) func() {
	return func() {
		a.leaveFind()
		a.endSnippet()
		a.focusEditor()
		a.vm.SetSuggest(providersHost(a, 1))
		a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 2)})
		pos := buffer.Position{Line: 1, Col: 2}
		if pinned {
			a.vm.ShowHoverAt(pos)
		} else {
			a.vm.HoverPointerMoved(pos)
		}
		deadline := time.Now().Add(5 * time.Second)
		for a.vm.Hover() == nil && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
			a.ts.Pump(100 * time.Millisecond)
			a.ts.FlushPosted()
		}
		if a.vm.Hover() == nil {
			panic("the hover did not show")
		}
		if focused {
			a.vm.FocusHover()
		}
	}
}

// signatureState is the state of an editor with the parameter hints on screen, with one signature or with several.
func signatureState(a *keyApp, signatures int) func() {
	return func() {
		a.leaveFind()
		a.endSnippet()
		a.focusEditor()
		a.vm.SetSuggest(providersHost(a, signatures))
		a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 2)})
		if err := a.vm.RunCommand("editor.action.triggerParameterHints", nil); err != nil {
			panic(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for a.vm.SignatureHints() == nil && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
			a.ts.FlushPosted()
		}
		if a.vm.SignatureHints() == nil {
			panic("the parameter hints did not show")
		}
	}
}
