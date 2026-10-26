package app

import (
	"context"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
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

// keysNav answers every navigation request with a place in the document and every reference request with two.
type keysNav struct{ uri document.URI }

func (k keysNav) ProvideLocations(context.Context, langfeatures.Document, langfeatures.Position, langfeatures.NavKind) ([]langfeatures.LocationLink, error) {
	return []langfeatures.LocationLink{{Target: langfeatures.Location{URI: k.uri, Range: buffer.Range{Start: buffer.Position{Line: 2}, End: buffer.Position{Line: 2, Col: 3}}}}}, nil
}

func (k keysNav) ProvideReferences(context.Context, langfeatures.Document, langfeatures.Position, bool) ([]langfeatures.Location, error) {
	return []langfeatures.Location{
		{URI: k.uri, Range: buffer.Range{Start: buffer.Position{Line: 0}, End: buffer.Position{Line: 0, Col: 3}}},
		{URI: k.uri, Range: buffer.Range{Start: buffer.Position{Line: 2}, End: buffer.Position{Line: 2, Col: 3}}},
	}, nil
}

// navigationHost is a host of the editor with the navigation providers of these tests (they match any language).
func navigationHost(a *keyApp) *editor.SuggestHost {
	reg := langfeatures.NewRegistry()
	nav := keysNav{uri: a.vm.Document().URI()}
	reg.RegisterNavigation("keys", langfeatures.Selector{}, nav)
	reg.RegisterReferences("keys", langfeatures.Selector{}, nav)
	return &editor.SuggestHost{Ctx: context.Background(), Features: reg, Shared: editor.NewSuggestShared()}
}

// navigationState is the state of an editor whose language has the navigation providers (the keys editorHasDefinitionProvider
// and its kin), for the keys of go to definition and its kin.
func navigationState(a *keyApp) func() {
	return func() {
		a.leaveFind()
		a.endSnippet()
		a.focusEditor()
		a.vm.SetSuggest(navigationHost(a))
		a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 2)})
	}
}

// peekState is the state of an editor with a peek view of two references open, for the keys that go through it.
func peekState(a *keyApp) func() {
	return func() {
		a.leaveFind()
		a.endSnippet()
		a.focusEditor()
		a.vm.SetSuggest(navigationHost(a))
		a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 2)})
		uri := a.vm.Document().URI()
		a.vm.OpenPeek(editor.GoReferences, []editor.NavTarget{
			{URI: uri, Range: buffer.Range{Start: buffer.Position{Line: 0}, End: buffer.Position{Line: 0, Col: 3}}},
			{URI: uri, Range: buffer.Range{Start: buffer.Position{Line: 2}, End: buffer.Position{Line: 2, Col: 3}}},
		})
	}
}
