package editor

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// The peek view (SPEC §9.12 "Navigation", "As built (step 12.7)"): an inline zone under the line of the cursor that shows the
// locations a navigation found, grouped by file, and an editor over the selected one, without opening a tab. This file is the
// model, driven by the commands and the mouse; peek_widget.go draws it.
//
// Ported from VS Code: src/vs/editor/contrib/gotoSymbol/browser/peek/referencesController.ts,
// src/vs/editor/contrib/gotoSymbol/browser/peek/referencesWidget.ts and
// src/vs/editor/contrib/gotoSymbol/browser/referencesModel.ts

// peekMatchOwner is the slot of the decorations that mark the references in the editor of the peek view.
const peekMatchOwner = 10

// classPeekMatch is the class of the decoration of a reference in the editor of the peek view.
const classPeekMatch = "peekMatch"

// peekBeforeRunes is how much of the text before a reference the row of the result list keeps.
const peekBeforeRunes = 14

// peekMaxRows is the height of the peek view in rows of the editor (VS Code's heightInLines).
const peekMaxRows = 18

// PeekRef is a location of the peek view.
type PeekRef struct {
	URI   document.URI
	Range buffer.Range
	// Before, Match and After are the text of the line of the range, once it is read: the text before the range with its
	// leading blanks cut, the text in it and the text after; Loaded says that they are known.
	Before, Match, After string
	Loaded               bool
}

// PeekFile is the locations of one file: Count references from Refs[First].
type PeekFile struct {
	URI  document.URI
	Name string
	// Dir is the folder of the file.
	Dir          string
	First, Count int
}

// PeekView is what the peek view shows.
type PeekView struct {
	Kind GoKind
	// Title is the name of the file of the selected location, Dir its folder and Meta the number of locations ("3 references").
	Title, Dir, Meta string
	Files            []PeekFile
	Refs             []PeekRef
	// Selected is the index of the selected location in Refs.
	Selected int
	// Current is the editor over the selected location, and CurrentURI its file; Current is nil while the editor is being made.
	Current    *ViewModel
	CurrentURI document.URI
	// Loading says that the editor is being made; Failed is the reason it could not be.
	Loading bool
	Failed  string
	// Rows is the height of the zone in rows of the editor.
	Rows int
}

// peekEmbedded is an editor of the peek view.
type peekEmbedded struct {
	vm      *ViewModel
	release func()
	loading bool
	err     error
}

// peekModel is the state of the peek view of a view model.
type peekModel struct {
	open bool
	// session numbers the peek views that were opened, so that the reading of one's previews is not taken by the next.
	session  int
	view     PeekView
	zone     ZoneID
	gen      int
	embedded map[document.URI]*peekEmbedded
	cancel   context.CancelFunc
	changed  *foundation.ChangeNotifier
}

func newPeekModel() peekModel { return peekModel{changed: foundation.NewChangeNotifier()} }

// PeekVisible tells whether the peek view is open (the key referenceSearchVisible).
func (vm *ViewModel) PeekVisible() bool { return vm.peek.open }

// Peek is what the peek view shows, or nil when it is closed. It is the model's own: do not change it.
func (vm *ViewModel) Peek() *PeekView {
	if !vm.peek.open {
		return nil
	}
	return &vm.peek.view
}

// AddPeekListener calls fn when the peek view opens, closes or changes.
func (vm *ViewModel) AddPeekListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return vm.peek.changed.AddListener(fn)
}

// RemovePeekListener removes a listener of AddPeekListener.
func (vm *ViewModel) RemovePeekListener(h foundation.ListenerHandle) {
	vm.peek.changed.RemoveListener(h)
}

// OpenPeek opens the peek view over the locations of a navigation (sorted, as sortTargets leaves them) at the line of the
// cursor, with the location nearest to the cursor selected. A peek that is open closes first.
func (vm *ViewModel) OpenPeek(kind GoKind, targets []NavTarget) {
	if len(targets) == 0 || vm.embedded {
		return
	}
	vm.PeekClose()
	p := &vm.peek
	p.open = true
	p.gen++
	p.session++
	p.embedded = map[document.URI]*peekEmbedded{}
	view := PeekView{Kind: kind, Refs: make([]PeekRef, len(targets)), Rows: vm.peekRows()}
	for i, t := range targets {
		view.Refs[i] = PeekRef{URI: t.URI, Range: t.Range}
		if n := len(view.Files); n == 0 || view.Files[n-1].URI != t.URI {
			view.Files = append(view.Files, PeekFile{URI: t.URI, Name: filepath.Base(uriPath(t.URI)), Dir: vm.peekDir(filepath.Dir(uriPath(t.URI))), First: i})
		}
		view.Files[len(view.Files)-1].Count++
	}
	view.Selected = nearestRef(view.Refs, vm.doc.URI(), vm.cursors.Primary().Position())
	p.view = view
	pos := vm.cursors.Primary().Selection.Range().Start
	id, err := vm.AddZone(ZoneSpec{AfterLine: pos.Line + 1, Rows: view.Rows, Widget: newPeekWidget(vm)})
	if err != nil {
		vm.reportError(err)
		p.open = false
		return
	}
	p.zone = id
	vm.peekReveal(pos)
	vm.peekLoadPreviews()
	vm.peekShowSelected()
	p.changed.NotifyListeners()
}

// peekDir is the folder of a file as the peek view shows it: the host's label (relative to the folder of the window), else the
// folder.
func (vm *ViewModel) peekDir(dir string) string {
	if h := vm.sg.host; h != nil && h.DirLabel != nil {
		return h.DirLabel(dir)
	}
	return dir
}

// uriPath is the path of a document the peek view shows.
func uriPath(u document.URI) string {
	if u.Path != "" {
		return u.Path
	}
	return u.String()
}

// peekRows is the height of the zone: 18 rows, fewer in a small editor.
func (vm *ViewModel) peekRows() int {
	rows := peekMaxRows
	if vm.opts.LineHeight > 0 && vm.viewport > 0 {
		rows = min(rows, max(int(vm.viewport/vm.opts.LineHeight)-3, 6))
	}
	return rows
}

// nearestRef is the location nearest to a position: the file that shares the longest prefix with the URI, then the line and
// column nearest.
//
// Ported from VS Code: referencesModel.ts (ReferencesModel.nearestReference)
func nearestRef(refs []PeekRef, uri document.URI, pos buffer.Position) int {
	best, bestPrefix, bestDist := 0, -1, 0
	for i, r := range refs {
		prefix := commonPrefix(r.URI.String(), uri.String())
		dist := absInt(r.Range.Start.Line-pos.Line)*100 + absInt(r.Range.Start.Col-pos.Col)
		if prefix > bestPrefix || prefix == bestPrefix && dist < bestDist {
			best, bestPrefix, bestDist = i, prefix, dist
		}
	}
	return best
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// peekReveal scrolls so that the line of the cursor and the whole zone are in view, if they fit.
func (vm *ViewModel) peekReveal(pos buffer.Position) {
	if vm.scroller == nil || vm.viewport <= 0 {
		return
	}
	proj := vm.Projection()
	first, count := proj.ZoneRows(vm.peek.zone)
	if count == 0 {
		return
	}
	row := vm.RowOfPosition(pos)
	lh := proj.RowHeight(first)
	bottom := proj.RowTop(first) + float64(count)*lh
	if proj.RowTop(row) >= vm.scrollTop && bottom <= vm.scrollTop+vm.viewport {
		return
	}
	top := min(bottom-vm.viewport, proj.RowTop(row)-lh)
	vm.scroller(max(top, 0))
}

// PeekClose closes the peek view and lets go of its editors.
func (vm *ViewModel) PeekClose() {
	p := &vm.peek
	if !p.open {
		return
	}
	p.open = false
	p.gen++
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	if err := vm.RemoveZone(p.zone); err != nil {
		vm.reportError(err)
	}
	for uri, e := range p.embedded {
		vm.peekRelease(e)
		delete(p.embedded, uri)
	}
	p.view = PeekView{}
	p.changed.NotifyListeners()
}

// peekRelease lets go of an editor of the peek view.
func (vm *ViewModel) peekRelease(e *peekEmbedded) {
	if e.vm != nil {
		e.vm.PeekClearMatches()
	}
	if e.release != nil {
		e.release()
		e.release = nil
	}
	e.vm = nil
}

// PeekSelect selects a location of the peek view and shows it in the editor.
func (vm *ViewModel) PeekSelect(i int) {
	p := &vm.peek
	if !p.open || i < 0 || i >= len(p.view.Refs) || i == p.view.Selected && p.view.Current != nil {
		return
	}
	p.view.Selected = i
	p.gen++
	vm.peekShowSelected()
	p.changed.NotifyListeners()
}

// PeekMove selects the next (1) or the previous (-1) location, going round at the ends.
func (vm *ViewModel) PeekMove(delta int) {
	p := &vm.peek
	if !p.open {
		return
	}
	n := len(p.view.Refs)
	vm.PeekSelect(((p.view.Selected+delta)%n + n) % n)
}

// PeekOpenSelected closes the peek view and shows the selected location in an editor of the window (a preview editor unless
// pinned; in the group beside with side). VS Code keeps the peek view open at the new place; here it closes (a decision of
// SPEC §9.12 "As built (step 12.7)").
func (vm *ViewModel) PeekOpenSelected(side, pinned bool) {
	p := &vm.peek
	if !p.open {
		return
	}
	ref := p.view.Refs[p.view.Selected]
	vm.PeekClose()
	vm.OpenTarget(NavTarget{URI: ref.URI, Range: buffer.Range{Start: ref.Range.Start, End: ref.Range.Start}}, OpenLocation{Side: side, Pinned: pinned})
}

// peekShowSelected makes the editor over the selected location: the one already made for its file, else a new one.
func (vm *ViewModel) peekShowSelected() {
	p := &vm.peek
	ref := p.view.Refs[p.view.Selected]
	view := &p.view
	view.Title, view.Dir = filepath.Base(uriPath(ref.URI)), vm.peekDir(filepath.Dir(uriPath(ref.URI)))
	view.Meta = strconv.Itoa(len(view.Refs)) + " references"
	if len(view.Refs) == 1 {
		view.Meta = "1 reference"
	}
	view.CurrentURI = ref.URI
	// the editors of the other files go: only the one on screen is kept
	for uri, e := range p.embedded {
		if uri != ref.URI && !e.loading {
			vm.peekRelease(e)
			delete(p.embedded, uri)
		}
	}
	if e := p.embedded[ref.URI]; e != nil {
		view.Current, view.Loading, view.Failed = e.vm, e.loading, ""
		if e.err != nil {
			view.Failed = e.err.Error()
		}
		if e.vm != nil {
			vm.peekPlace(e.vm, ref.URI)
		}
		return
	}
	h := vm.sg.host
	if h == nil || h.Embedded == nil {
		view.Current, view.Loading = nil, false
		return
	}
	e := &peekEmbedded{loading: true}
	p.embedded[ref.URI] = e
	view.Current, view.Loading, view.Failed = nil, true, ""
	ctx := context.Background()
	if h.Ctx != nil {
		ctx = h.Ctx
	}
	h.Embedded(ctx, ref.URI, func(evm *ViewModel, release func(), err error) {
		if !p.open || p.embedded[ref.URI] != e {
			// the peek closed, or moved on, while the editor was made
			if release != nil {
				release()
			}
			return
		}
		e.loading, e.vm, e.release, e.err = false, evm, release, err
		if err != nil {
			if h.Log != nil {
				h.Log("the peek view cannot show "+ref.URI.String(), err)
			}
		}
		if p.view.CurrentURI == ref.URI {
			p.view.Loading, p.view.Current = false, evm
			if err != nil {
				p.view.Failed = err.Error()
			}
			if evm != nil {
				evm.embedded = true
				vm.peekPlace(evm, ref.URI)
			}
			p.changed.NotifyListeners()
		}
	})
}

// peekPlace puts the editor of the peek view at the selected location: the cursor at its start, the references of the file
// marked, and the location in the middle of the editor once it is laid out.
func (vm *ViewModel) peekPlace(emb *ViewModel, uri document.URI) {
	view := &vm.peek.view
	ref := view.Refs[view.Selected]
	snap := emb.doc.Snapshot()
	at := snap.ValidatePosition(ref.Range.Start)
	emb.cursors.SetSelections([]buffer.Selection{buffer.NewCursor(at)})
	var marks []buffer.Range
	for _, r := range view.Refs {
		if r.URI == uri {
			marks = append(marks, snap.ValidateRange(r.Range))
		}
	}
	emb.peekMarks(marks)
	emb.SelectionsChanged()
	reveal := func() bool {
		if emb.viewport <= 0 || emb.scroller == nil {
			return false
		}
		emb.RevealPositionInCenter(at)
		return true
	}
	if reveal() {
		return
	}
	var handle foundation.ListenerHandle
	handle = emb.AddMetricsListener(func() {
		if reveal() {
			emb.RemoveMetricsListener(handle)
		}
	})
}

// peekMarks marks the references of the file in the editor of the peek view.
func (vm *ViewModel) peekMarks(ranges []buffer.Range) {
	vm.setHighlights(peekMatchOwner, &vm.hl.peekIDs, ranges)
}

// PeekClearMatches takes the marks of the references away.
func (vm *ViewModel) PeekClearMatches() { vm.peekMarks(nil) }

// SetEmbedded marks the view model as the editor of a peek view: a navigation in it does not open another peek view. The window
// sets it when it makes the editor.
func (vm *ViewModel) SetEmbedded(on bool) { vm.embedded = on }

// IsEmbedded tells whether the view model is the editor of a peek view.
func (vm *ViewModel) IsEmbedded() bool { return vm.embedded }

// peekLoadPreviews reads the text of the lines of the locations, off the UI thread, for the rows of the results.
func (vm *ViewModel) peekLoadPreviews() {
	p := &vm.peek
	h := vm.sg.host
	session := p.session
	refs := make([]PeekRef, len(p.view.Refs))
	copy(refs, p.view.Refs)
	var texts langfeatures.TextSource
	if h != nil {
		texts = h.Texts
	}
	self := vm.doc.Snapshot()
	selfURI := vm.doc.URI()
	ctx := context.Background()
	if h != nil && h.Ctx != nil {
		ctx = h.Ctx
	}
	ctx, p.cancel = context.WithCancel(ctx)
	go func() {
		done := map[document.URI]bool{}
		for i := range refs {
			uri := refs[i].URI
			if done[uri] {
				continue
			}
			done[uri] = true
			var lines lsp.Lines
			switch {
			case uri == selfURI:
				lines = self
			case texts != nil:
				l, err := texts(ctx, uri)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					continue
				}
				lines = l
			default:
				continue
			}
			previews := map[int]PeekRef{}
			for j := range refs {
				if refs[j].URI == uri {
					r := refs[j]
					fillPreview(&r, lines)
					previews[j] = r
				}
			}
			vm.post(func() {
				if !p.open || p.session != session {
					return
				}
				for j, r := range previews {
					if j < len(p.view.Refs) && p.view.Refs[j].URI == r.URI && p.view.Refs[j].Range == r.Range {
						p.view.Refs[j] = r
					}
				}
				p.changed.NotifyListeners()
			})
		}
	}()
}

// fillPreview reads the text around a reference.
//
// Ported from VS Code: referencesModel.ts (FilePreview.preview)
func fillPreview(r *PeekRef, lines lsp.Lines) {
	line, err := lines.LineContent(r.Range.Start.Line)
	if err != nil {
		return
	}
	start := min(r.Range.Start.Col, len(line))
	end := len(line)
	if r.Range.End.Line == r.Range.Start.Line {
		end = min(max(r.Range.End.Col, start), len(line))
	}
	r.Before = strings.TrimLeft(line[:start], " \t")
	// a long start of the line would push the match out of the row: its last characters stay, after an ellipsis (VS Code lets
	// the row's own ellipsis cut the end, which hides the match as well)
	if runes := []rune(r.Before); len(runes) > peekBeforeRunes {
		r.Before = "…" + string(runes[len(runes)-peekBeforeRunes:])
	}
	r.Match, r.After, r.Loaded = line[start:end], line[end:], true
}
