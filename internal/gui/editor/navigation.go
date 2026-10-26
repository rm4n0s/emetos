package editor

import (
	"context"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// Go to definition and its kin (SPEC §9.12 "Navigation", "As built (step 12.7)"): the commands ask the providers for the
// locations of the symbol at the cursor, off the UI thread; one result is a jump, several open the peek view (editor.gotoLocation.*),
// and the alternative command runs when the cursor is on the only result.
//
// Ported from VS Code: src/vs/editor/contrib/gotoSymbol/browser/goToCommands.ts (SymbolNavigationAction),
// src/vs/editor/contrib/gotoSymbol/browser/referencesModel.ts and
// src/vs/editor/contrib/gotoSymbol/browser/goToSymbol.ts

// GoKind is the kind of location a navigation command looks for.
type GoKind int

// The kinds of navigation, in the order of the settings editor.gotoLocation.multiple* and alternative*Command.
const (
	GoDefinition GoKind = iota
	GoDeclaration
	GoTypeDefinition
	GoImplementation
	GoReferences
	goKinds
)

// nav is the kind of location the providers are asked for, for the kinds that are not references.
func (k GoKind) nav() langfeatures.NavKind {
	switch k {
	case GoDeclaration:
		return langfeatures.NavDeclaration
	case GoTypeDefinition:
		return langfeatures.NavTypeDefinition
	case GoImplementation:
		return langfeatures.NavImplementation
	}
	return langfeatures.NavDefinition
}

// Feature is the feature the providers must offer.
func (k GoKind) Feature() langfeatures.Feature {
	switch k {
	case GoDeclaration:
		return langfeatures.FeatureDeclaration
	case GoTypeDefinition:
		return langfeatures.FeatureTypeDefinition
	case GoImplementation:
		return langfeatures.FeatureImplementation
	case GoReferences:
		return langfeatures.FeatureReferences
	}
	return langfeatures.FeatureDefinition
}

// noResult is the message of a navigation that found nothing.
func (k GoKind) noResult(word string) string {
	what := [goKinds]string{"definition", "declaration", "type definition", "implementation", "references"}[k]
	if word == "" {
		return "No " + what + " found"
	}
	return "No " + what + " found for '" + word + "'"
}

// GotoLocationOptions are the settings editor.gotoLocation.* and editor.definitionLinkOpensInPeek.
type GotoLocationOptions struct {
	// Multiple is what a navigation does when it finds several locations, by GoKind: "peek" (also when empty), "gotoAndPeek" or
	// "goto".
	Multiple [goKinds]string
	// Alternative is the command that runs when the only location found is the one the cursor is on, by GoKind; empty for none.
	Alternative [goKinds]string
	// DefinitionLinkOpensInPeek makes the mouse gesture (Ctrl+click) open the peek view.
	DefinitionLinkOpensInPeek bool
}

// GoConfig says how a navigation command behaves.
type GoConfig struct {
	// Peek opens the peek view whatever the number of results is (the Peek commands).
	Peek bool
	// Side opens the location in the group beside the active one.
	Side bool
	// Mute leaves out the message of a navigation that found nothing.
	Mute bool
	// Run runs a command of the editor by id, for the alternative command of a navigation (a command is run by the actions
	// that call GoTo, which can reach the table of commands); without it there is no alternative command.
	Run func(id string) error
}

// NavTarget is a place a navigation found.
type NavTarget struct {
	URI document.URI
	// Range is the range to select: the name of the symbol for a link, else the range of the location.
	Range buffer.Range
}

// OpenLocation says how the window shows a location.
type OpenLocation struct {
	// Side opens it in the group beside the active one.
	Side bool
	// Pinned keeps the editor instead of replacing the preview editor.
	Pinned bool
	// After is called on the UI thread with the view model of the editor that shows the location, once it does.
	After func(target *ViewModel)
}

// goState is the navigation that is being asked for.
type goState struct {
	op  *goOp
	gen int
	// active holds the alternative commands that are running, so that one does not run itself again.
	active map[string]bool
}

type goOp struct {
	gen     int
	kind    GoKind
	cfg     GoConfig
	version int
	pos     buffer.Position
	word    string
	cancel  context.CancelFunc
}

// HasNavigation tells whether a provider offers the kind of navigation for the document (the keys editorHasDefinitionProvider
// and its kin).
func (vm *ViewModel) HasNavigation(k GoKind) bool {
	f := vm.hoverFeatures()
	return f != nil && f.Has(vm.langDocument(), k.Feature())
}

// GoTo asks the providers for the locations of the symbol at the cursor and does what the settings say with them. It says
// whether a request was made.
func (vm *ViewModel) GoTo(kind GoKind, cfg GoConfig) bool {
	f := vm.hoverFeatures()
	doc := vm.langDocument()
	if f == nil || !f.Has(doc, kind.Feature()) {
		return false
	}
	vm.CancelGoTo()
	g := &vm.nav
	g.gen++
	ctx := context.Background()
	if h := vm.sg.host; h != nil && h.Ctx != nil {
		ctx = h.Ctx
	}
	ctx, cancel := context.WithCancel(ctx)
	pos := vm.cursors.Primary().Position()
	line, _ := vm.doc.Snapshot().LineContent(pos.Line)
	word := ""
	if s, e, ok := vm.wordFunc()(line, min(pos.Col, len(line))); ok {
		word = line[s:e]
	}
	op := &goOp{gen: g.gen, kind: kind, cfg: cfg, version: doc.Version, pos: pos, word: word, cancel: cancel}
	g.op = op
	go func() {
		targets, errs := goRequest(ctx, f, doc, pos, kind)
		vm.post(func() { vm.goResult(op, targets, errs) })
	}()
	return true
}

// CancelGoTo ends the navigation that is being asked for; its answer is dropped.
func (vm *ViewModel) CancelGoTo() {
	if op := vm.nav.op; op != nil {
		op.cancel()
		vm.nav.op = nil
	}
}

// goRequest asks the providers for the locations.
func goRequest(ctx context.Context, f *langfeatures.Registry, doc langfeatures.Document, pos buffer.Position, kind GoKind) ([]NavTarget, []langfeatures.ProviderError) {
	var out []NavTarget
	var errs []langfeatures.ProviderError
	if kind == GoReferences {
		var locs []langfeatures.Location
		locs, errs = f.References(ctx, doc, pos, true)
		for _, l := range locs {
			out = append(out, NavTarget{URI: l.URI, Range: l.Range})
		}
		return out, errs
	}
	var links []langfeatures.LocationLink
	links, errs = f.Locations(ctx, doc, pos, kind.nav())
	for _, l := range links {
		r := l.TargetSelection
		if r == (buffer.Range{}) {
			r = l.Target.Range
		}
		out = append(out, NavTarget{URI: l.Target.URI, Range: r})
	}
	return out, errs
}

// sortTargets orders locations by file and position, as VS Code's ReferencesModel does, and says where the first location of
// the providers went.
func sortTargets(targets []NavTarget) (sorted []NavTarget, first int) {
	type item struct {
		t     NavTarget
		first bool
	}
	items := make([]item, len(targets))
	for i, t := range targets {
		items[i] = item{t: t, first: i == 0}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].t, items[j].t
		if c := strings.Compare(a.URI.String(), b.URI.String()); c != 0 {
			return c < 0
		}
		if a.Range.Start != b.Range.Start {
			return a.Range.Start.IsBefore(b.Range.Start)
		}
		return a.Range.End.IsBefore(b.Range.End)
	})
	sorted = make([]NavTarget, len(items))
	for i, it := range items {
		sorted[i] = it.t
		if it.first {
			first = i
		}
	}
	return sorted, first
}

// goResult takes the answer of a navigation. An answer of a navigation that was replaced or cancelled, for a document that
// changed, or for a cursor that moved, is dropped (VS Code's EditorStateCancellationTokenSource with the flags value and
// position).
func (vm *ViewModel) goResult(op *goOp, targets []NavTarget, errs []langfeatures.ProviderError) {
	if vm.nav.op != op {
		return
	}
	vm.nav.op = nil
	op.cancel()
	if vm.doc.Version() != op.version || vm.cursors.Primary().Position() != op.pos {
		return
	}
	if h := vm.sg.host; h != nil {
		for _, e := range errs {
			if h.Log != nil && !errors.Is(e.Err, context.Canceled) {
				h.Log("navigation provider "+e.Owner+" failed", e.Err)
			}
		}
	}
	vm.goHandle(op.kind, op.cfg, op.word, op.pos, targets)
}

// goHandle does what a navigation that found targets asks for.
//
// Ported from VS Code: goToCommands.ts (SymbolNavigationAction.runEditorCommand, _onResult)
func (vm *ViewModel) goHandle(kind GoKind, cfg GoConfig, word string, pos buffer.Position, targets []NavTarget) {
	if len(targets) == 0 {
		if !cfg.Mute {
			vm.ShowMessage(kind.noResult(word))
		}
		return
	}
	sorted, first := sortTargets(targets)
	opts := vm.opts.GotoLocation
	// the cursor is on the only result: the alternative command is what the user wants
	if alt := opts.Alternative[kind]; alt != "" && len(sorted) == 1 && !vm.nav.active[alt] && vm.targetAt(sorted, pos) {
		if cfg.Run != nil {
			if vm.nav.active == nil {
				vm.nav.active = map[string]bool{}
			}
			cmd := commandOfKind(kind)
			vm.nav.active[cmd] = true
			defer delete(vm.nav.active, cmd)
			if err := cfg.Run(alt); err != nil {
				vm.reportError(err)
			}
			return
		}
	}
	multiple := opts.Multiple[kind]
	if !vm.embedded && (cfg.Peek || ((multiple == "" || multiple == "peek") && len(sorted) > 1)) {
		vm.OpenPeek(kind, sorted)
		return
	}
	next := sorted[first]
	peek := len(sorted) > 1 && multiple == "gotoAndPeek" && !vm.embedded
	var after func(*ViewModel)
	if peek {
		after = func(target *ViewModel) { target.OpenPeek(kind, sorted) }
	}
	vm.OpenTarget(next, OpenLocation{Side: cfg.Side, After: after})
}

// commandOfKind is the id of the command that runs a kind (to stop one from running itself as its alternative).
func commandOfKind(k GoKind) string {
	return [goKinds]string{
		"editor.action.revealDefinition", "editor.action.revealDeclaration", "editor.action.goToTypeDefinition",
		"editor.action.goToImplementation", "editor.action.goToReferences",
	}[k]
}

// targetAt tells whether a position of this document is in one of the targets.
func (vm *ViewModel) targetAt(targets []NavTarget, pos buffer.Position) bool {
	for _, t := range targets {
		if t.URI == vm.doc.URI() && t.Range.ContainsPosition(pos) {
			return true
		}
	}
	return false
}

// OpenTarget shows a location: the window opens its file (a preview editor, or the group beside) and puts the cursor at the
// start of the range, which is a jump of the navigation history. Without a window the location of this document is gone to
// here.
func (vm *ViewModel) OpenTarget(t NavTarget, o OpenLocation) {
	if h := vm.sg.host; h != nil && h.Open != nil {
		h.Open(langfeatures.Location{URI: t.URI, Range: t.Range}, o)
		return
	}
	if t.URI == vm.doc.URI() {
		vm.GoToPosition(t.Range.Start)
		if o.After != nil {
			o.After(vm)
		}
	}
}

// reportError shows a failure to the user.
func (vm *ViewModel) reportError(err error) {
	if h := vm.sg.host; h != nil && h.Report != nil {
		h.Report(err)
	}
}

// ShowMessage shows a message next to the cursor until it moves (VS Code's MessageController): "No definition found for
// 'x'". It is the hover of a command, with the text as its only part.
func (vm *ViewModel) ShowMessage(text string) {
	p := vm.cursors.Primary().Position()
	rng := buffer.Range{Start: p, End: p}
	if line, err := vm.doc.Snapshot().LineContent(p.Line); err == nil {
		if s, e, ok := vm.wordFunc()(line, min(p.Col, len(line))); ok {
			rng = buffer.Range{Start: buffer.Position{Line: p.Line, Col: s}, End: buffer.Position{Line: p.Line, Col: e}}
		}
	}
	vm.HideHover()
	h := &vm.hover
	h.shown = &Hover{
		Range: rng, Pinned: true, Complete: true,
		Parts: []HoverPart{{Kind: HoverPartMarkdown, Contents: []langfeatures.Markup{{Value: text}}, Range: rng}},
	}
	h.selections = vm.cursors.Selections()
	vm.hoverListen()
	h.changed.NotifyListeners()
}

// navigationActions are the commands of go to definition and its kin, and of the peek view.
//
// Ported from VS Code: goToCommands.ts and peek/referencesController.ts (the commands)
func navigationActions(a map[string]action) {
	only := func(f func(vm *ViewModel) error) action {
		return func(vm *ViewModel, _ any) error {
			vm.keepSessions, vm.skipReveal = true, true
			return f(vm)
		}
	}
	// the alternative command is one of the table: reaching it from GoTo directly would make the table refer to itself
	goto_ := func(kind GoKind, cfg GoConfig) action {
		return only(func(vm *ViewModel) error {
			cfg.Run = func(id string) error {
				if act, ok := a[id]; ok {
					return vm.Run(act, nil)
				}
				return nil // the setting names the commands that can be (settings.Validate), all of the editor
			}
			vm.GoTo(kind, cfg)
			return nil
		})
	}
	a["editor.action.revealDefinition"] = goto_(GoDefinition, GoConfig{})
	a["editor.action.revealDefinitionAside"] = goto_(GoDefinition, GoConfig{Side: true})
	a["editor.action.peekDefinition"] = goto_(GoDefinition, GoConfig{Peek: true})
	a["editor.action.revealDeclaration"] = goto_(GoDeclaration, GoConfig{})
	a["editor.action.peekDeclaration"] = goto_(GoDeclaration, GoConfig{Peek: true})
	a["editor.action.goToTypeDefinition"] = goto_(GoTypeDefinition, GoConfig{})
	a["editor.action.peekTypeDefinition"] = goto_(GoTypeDefinition, GoConfig{Peek: true})
	a["editor.action.goToImplementation"] = goto_(GoImplementation, GoConfig{})
	a["editor.action.peekImplementation"] = goto_(GoImplementation, GoConfig{Peek: true})
	a["editor.action.goToReferences"] = goto_(GoReferences, GoConfig{})
	a["editor.action.referenceSearch.trigger"] = goto_(GoReferences, GoConfig{Peek: true})

	a["closeReferenceSearch"] = only(func(vm *ViewModel) error { vm.PeekClose(); return nil })
	a["goToNextReference"] = only(func(vm *ViewModel) error { vm.PeekMove(1); return nil })
	a["goToPreviousReference"] = only(func(vm *ViewModel) error { vm.PeekMove(-1); return nil })
	a["selectNextReference"] = a["goToNextReference"]
	a["selectPreviousReference"] = a["goToPreviousReference"]
	a["openReference"] = only(func(vm *ViewModel) error { vm.PeekOpenSelected(false, true); return nil })
	a["openReferenceToSide"] = only(func(vm *ViewModel) error { vm.PeekOpenSelected(true, true); return nil })
}
