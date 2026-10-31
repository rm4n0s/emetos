package app

import (
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
	"github.com/rm4n0s/emetos/internal/gui/diffeditor"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/quickdiffpeek"
)

// The peek of a change in the window (SPEC §10.30 "The peek", step 24.12): the editor of a file gets the controller of internal/gui/
// quickdiffpeek, which the window feeds with the model of the changes, and for which it makes the small diff editor and the buttons of
// the title bar from the menu of the extensions.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/quickDiffWidget.ts (QuickDiffWidget.showChange, _fillBody, updateActions)

// startQuickDiffPeek gives the editor the peek of its changes. It returns the function that closes the peek and lets go of it.
func (s *shellState) startQuickDiffPeek(binding *widgets.WidgetsBinding, e *textEditor, model *quickdiff.Model) func() {
	win := PeekWorkspace(s.Context())
	peek := quickdiffpeek.New(quickdiffpeek.Config{
		Editor: e.view,
		Model:  model,
		Name:   func() string { return e.doc.URI().Title() },
		NewBody: func(spec quickdiffpeek.BodySpec) (quickdiffpeek.Body, error) {
			return s.newPeekBody(binding, e, spec)
		},
		Actions:     func(sh quickdiffpeek.Shown) []quickdiffpeek.Action { return s.peekActions(win, e, sh) },
		FocusEditor: func() { e.focus.Request() },
		OnError:     func(err error) { win.Process.Logger.Warn("the peek of a change failed", "err", err) },
	})
	e.view.SetQuickDiffPeeker(peek)
	return func() {
		peek.Dispose()
		e.view.SetQuickDiffPeeker(nil)
	}
}

// peekChanges are the changes of a provider as the commands of the peek get them.
func peekChanges(changes []quickdiff.Change) []extension.LineChange {
	out := make([]extension.LineChange, len(changes))
	for i, c := range changes {
		out[i] = extension.LineChange{
			OriginalStartLineNumber: c.OriginalStart, OriginalEndLineNumber: c.OriginalEnd,
			ModifiedStartLineNumber: c.ModifiedStart, ModifiedEndLineNumber: c.ModifiedEnd,
		}
	}
	return out
}

// peekActions are the buttons of the title bar: the items of the menu of the peek whose condition holds for the change, last first (VS
// Code reverses the order of the menu), each running its command with the file, the changes of the provider and the place of the shown one.
func (s *shellState) peekActions(win *Workspace, e *textEditor, sh quickdiffpeek.Shown) []quickdiffpeek.Action {
	origin := document.URI{}
	if u, err := document.ParseURI(sh.Original); err == nil {
		origin = u
	}
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: map[string]any{
		"originalResource": sh.Original, "originalResourceScheme": win.resourceScheme(origin),
		"originalResourceSchemes": []any{win.resourceScheme(origin)},
	}}
	args := extension.QuickDiffArgs(quickdiff.URIOf(e.doc.URI()), peekChanges(sh.Changes), sh.Index)
	var out []quickdiffpeek.Action
	for _, item := range win.Menus.Items(command.SCMChangeContext, ctx) {
		code, ok := fonts.CodiconByName(item.Icon)
		if item.Separator || item.Command == "" || item.Disabled || !ok {
			continue
		}
		id := item.Command
		out = append(out, quickdiffpeek.Action{Icon: code, Tooltip: item.Title, Run: func() { s.runTitleAction(win, id, args) }})
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// peekBody is the small diff editor of a peek: the original of a provider and the document of the editor, in the inline view.
type peekBody struct {
	s      *shellState
	sides  [2]*textEditor
	inputs [2]*editorgroup.TextInput
	docs   [2]*document.Document
	model  *diffeditor.Model
	// focused says that the editor of the modified text has the keyboard.
	focused  bool
	disposed bool
	// detached says that the original document is the peek's alone, in no set of the window.
	detached bool
	// want is the change to bring into view when the body can.
	want *quickdiff.Change
	// handles are the listeners that place it.
	modelHandle   foundation.ListenerHandle
	metricsHandle foundation.ListenerHandle
}

// newPeekBody makes the small diff editor for the original of a provider. The modified side is the document of the outer editor, so the
// changes typed in the peek are the editor's, and the diff follows them.
//
// Ported from VS Code: quickDiffWidget.ts (QuickDiffWidget._fillBody: the options of the EmbeddedDiffEditorWidget)
func (s *shellState) newPeekBody(binding *widgets.WidgetsBinding, outer *textEditor, spec quickdiffpeek.BodySpec) (quickdiffpeek.Body, error) {
	win := PeekWorkspace(s.Context())
	u, err := document.ParseURI(spec.Original)
	if err != nil {
		return quickdiffpeek.Body{}, err
	}
	content := document.VirtualContent{Text: strings.ReplaceAll(spec.Text, "\r\n", "\n"), Label: spec.Label, Language: outer.doc.LanguageID()}
	// a document an extension provides is shared with the editors that show it (the diff of the staged file); any other original, a
	// file the provider names, is a read-only document of the peek alone
	var original *document.Document
	detached := u.Scheme != document.SchemeVirtual
	if detached {
		original = document.NewVirtual(u, content)
	} else {
		original = win.Documents.AdoptVirtual(u, content)
	}
	b := &peekBody{s: s, docs: [2]*document.Document{original, outer.doc}, detached: detached}
	for i, side := range []diffeditor.Side{diffeditor.Original, diffeditor.Modified} {
		if i == 1 || !detached {
			win.Documents.Retain(b.docs[i])
		}
		b.inputs[i] = editorgroup.NewTextInput(b.docs[i], s.inputOptions())
		b.sides[i] = s.newTextEditorWith(binding, b.inputs[i], func(o editor.Options) editor.Options {
			o = diffeditor.AdjustOptions(o, side)
			o.GlyphMargin = false
			return o
		})
		b.sides[i].view.SetEmbedded(true)
		b.sides[i].view.SetQuickDiffPeeker(spec.Peeker)
	}
	b.model = diffeditor.NewModel(diffeditor.ModelOpts{
		Original: b.sides[0].view, Modified: b.sides[1].view, Settings: win.Settings, Embedded: true,
		Post: binding.Post, Context: win.ctx,
		AfterTimer:    func(d time.Duration, fn func()) func() { return binding.Gestures().NewTimer(d, fn).Cancel },
		OnError:       func(err error) { win.Process.Logger.Warn("the diff of the peek cannot be computed", "err", err) },
		OriginalFocus: b.sides[0].focus, ModifiedFocus: b.sides[1].focus,
	})
	b.model.Start()
	b.modelHandle = b.model.AddListener(b.place)
	// the metrics change while the editor is laid out: the scroll that places the change waits for the frame to end
	b.metricsHandle = b.sides[1].view.AddMetricsListener(func() { binding.Post(b.place) })
	opts := func(ed *textEditor, side diffeditor.Side) editor.EditorOpts {
		return editor.EditorOpts{
			Commands: win.Commands, Keys: win.Keys, Scopes: win.Scopes,
			ClipboardMemory: win.Clipboard, FindMemory: win.Find, Zoom: win.Zoom, InputMode: win.InputMode, Focus: ed.focus, Context: win.ctx,
			OnFocus: func(has bool) {
				if side == diffeditor.Modified {
					b.focused = has
					outer.view.QuickDiffPeekChanged()
				}
			},
		}
	}
	return quickdiffpeek.Body{
		Widget:   diffeditor.NewWidget(b.model, diffeditor.WidgetOpts{Original: opts(b.sides[0], diffeditor.Original), Modified: opts(b.sides[1], diffeditor.Modified)}),
		Reveal:   b.reveal,
		Focus:    func() { b.sides[1].focus.Request() },
		HasFocus: func() bool { return b.focused },
		Dispose:  b.dispose,
	}, nil
}

// reveal brings the change into the middle of the small diff editor, once the editor knows its size and the diff has been computed (the
// lines of the deleted code are zones that move the rows).
//
// Ported from VS Code: quickDiffWidget.ts (QuickDiffWidget.revealChange)
func (b *peekBody) reveal(c quickdiff.Change) {
	b.want = &c
	b.place()
}

// place reveals the change the body was asked for when it can be done, and keeps asking until then.
func (b *peekBody) place() {
	if b.want == nil || b.disposed {
		return
	}
	vm := b.sides[1].view
	if _, height := vm.ViewportSize(); height <= 0 || !b.model.Computed() {
		return
	}
	c := *b.want
	b.want = nil
	start, end := c.ModifiedStart, c.ModifiedEnd
	switch {
	case end == 0: // a deletion
		end = start + 1
	case c.OriginalEnd > 0: // a modification
		start, end = start-1, end+1
	}
	vm.RevealPositionInCenter(buffer.Position{Line: max((start+end)/2-1, 0)})
}

// dispose lets go of the editors and the documents, as the diff pane does.
func (b *peekBody) dispose() {
	if b.disposed {
		return
	}
	b.disposed = true
	win := b.s.win
	b.model.RemoveListener(b.modelHandle)
	b.sides[1].view.RemoveMetricsListener(b.metricsHandle)
	b.model.Dispose()
	for i, ed := range b.sides {
		delete(b.s.group.focused, ed)
		ed.view.SetQuickDiffPeeker(nil)
		ed.dispose()
		if i == 0 && b.detached {
			b.s.autoSaveUntrack(ed.doc)
			b.s.externalUntrack(ed.doc)
		} else if win.Documents.Release(ed.doc) {
			b.s.autoSaveUntrack(ed.doc)
			b.s.externalUntrack(ed.doc)
		}
		b.inputs[i].Dispose()
	}
}
