package editor

import (
	"context"
	"slices"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Code lenses (SPEC §9.12 "As built (step 12.9)"): commands the language server shows above lines. A line that has lenses gets a
// view zone of one row above it (§9.1), with the titles of its lenses separated by " | ". The zones follow the edits of the
// document; the lenses are asked for again a little after an edit, and resolved (their commands asked for) when their line is
// on the screen. A click on a title runs the command.
//
// Ported from VS Code: src/vs/editor/contrib/codelens/browser/codelensController.ts, codelensWidget.ts and codelens.ts

// lensItem is a lens of a row.
type lensItem struct {
	lens langfeatures.CodeLens
	// resolving says that the command is being asked for.
	resolving bool
	// failed says that the provider could not resolve the lens: it is not asked again.
	failed bool
}

// lensRow is the zone of a line and the lenses on it.
type lensRow struct {
	zone ZoneID
	// line is the line the zone was put above (it follows the edits: lensLine is the line now).
	line  int
	items []*lensItem
}

// lensState is what a view model knows of the code lenses of its document.
type lensState struct {
	rows []*lensRow
	// version is the version of the document the lenses were asked for.
	version int
	// gen numbers the requests for the lenses, resolveGen those for their commands.
	gen, resolveGen int
	cancel          context.CancelFunc
	cancelResolve   context.CancelFunc
	cancelWait      func()
	cancelResolveIn func()
	deb             *featureDebounce
	unsub           func()
	scrollOn        bool
	scrollH         foundation.ListenerHandle
}

func newLensState() lensState {
	return lensState{deb: newFeatureDebounce(250*time.Millisecond, 2500*time.Millisecond), version: -1}
}

// CodeLensCount is how many lens rows (lines with lenses) the editor shows.
func (vm *ViewModel) CodeLensCount() int { return len(vm.lens.rows) }

// lensWanted tells whether lenses should be asked for: editor.codeLens is on and a provider offers them.
func (vm *ViewModel) lensWanted() bool {
	f := vm.hoverFeatures()
	return f != nil && vm.opts.Lens.CodeLens && !vm.embedded && !vm.doc.LargeFile() && f.Has(vm.langDocument(), langfeatures.FeatureCodeLens)
}

// lensHostChanged starts or stops following the document when the host is set or what the providers offer changed.
func (vm *ViewModel) lensHostChanged() {
	l := &vm.lens
	if vm.hoverFeatures() == nil {
		vm.lensStop()
		return
	}
	if l.unsub == nil {
		l.unsub = vm.doc.Subscribe(func(document.ChangeEvent) { vm.lensDocumentChanged() })
	}
	if !l.scrollOn {
		l.scrollH, l.scrollOn = vm.AddScrollListener(vm.lensScrolled), true
	}
	vm.lensSchedule(0)
}

// lensStop stops following the document and the scroll and takes the lenses away.
func (vm *ViewModel) lensStop() {
	l := &vm.lens
	if l.unsub != nil {
		l.unsub()
		l.unsub = nil
	}
	if l.scrollOn {
		vm.RemoveScrollListener(l.scrollH)
		l.scrollOn = false
	}
	vm.lensCancel()
	vm.lensRemoveRows()
}

// DisposeCodeLenses stops following the document and takes the lenses away.
func (vm *ViewModel) DisposeCodeLenses() { vm.lensStop() }

// lensCancel ends the request in flight and the waits.
func (vm *ViewModel) lensCancel() {
	l := &vm.lens
	l.gen++
	l.resolveGen++
	for _, c := range []*func(){&l.cancelWait, &l.cancelResolveIn} {
		if *c != nil {
			(*c)()
			*c = nil
		}
	}
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	if l.cancelResolve != nil {
		l.cancelResolve()
		l.cancelResolve = nil
	}
}

// lensRemoveRows removes the zones of the lenses.
func (vm *ViewModel) lensRemoveRows() {
	l := &vm.lens
	if len(l.rows) == 0 {
		return
	}
	_ = vm.ChangeZones(func(z ZoneChanges) error {
		for _, r := range l.rows {
			if err := z.Remove(r.zone); err != nil {
				return err
			}
		}
		return nil
	})
	l.rows = nil
}

// lensDocumentChanged follows an edit: the lenses are asked for again after the delay the answers took, and the commands that
// are being resolved are for lenses that may be gone.
func (vm *ViewModel) lensDocumentChanged() {
	l := &vm.lens
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.gen++
	vm.lensCancelResolve()
	if vm.lensWanted() {
		vm.lensSchedule(l.deb.delay())
	}
}

// lensScrolled resolves the lenses that came into view.
func (vm *ViewModel) lensScrolled() {
	vm.lensResolveSoon()
}

// lensOptionsChanged follows a change of the settings: the lenses go with editor.codeLens, and come back.
func (vm *ViewModel) lensOptionsChanged() {
	if vm.hoverFeatures() == nil {
		return
	}
	if !vm.opts.Lens.CodeLens {
		vm.lensCancel()
		vm.lensRemoveRows()
		return
	}
	if len(vm.lens.rows) == 0 {
		vm.lensSchedule(0)
		return
	}
	vm.lensRefreshWidgets()
}

// lensSchedule asks for the lenses after a delay (at once for 0), replacing a request that is waiting.
func (vm *ViewModel) lensSchedule(delay time.Duration) {
	l := &vm.lens
	if l.cancelWait != nil {
		l.cancelWait()
		l.cancelWait = nil
	}
	if delay <= 0 || vm.afterTimer == nil {
		vm.lensRequest()
		return
	}
	l.cancelWait = vm.afterTimer(delay, func() {
		l.cancelWait = nil
		vm.lensRequest()
	})
}

// lensRequest asks the providers for the lenses of the document, off the UI thread.
func (vm *ViewModel) lensRequest() {
	l := &vm.lens
	f := vm.hoverFeatures()
	if f == nil {
		return
	}
	doc := vm.langDocument()
	if !vm.lensWanted() {
		vm.lensRemoveRows()
		return
	}
	if l.cancel != nil {
		l.cancel()
	}
	l.gen++
	gen := l.gen
	base := context.Background()
	if h := vm.sg.host; h != nil && h.Ctx != nil {
		base = h.Ctx
	}
	ctx, cancel := context.WithCancel(base)
	l.cancel = cancel
	started := time.Now()
	go func() {
		lenses, errs := f.CodeLenses(ctx, doc)
		vm.post(func() {
			if vm.lens.gen != gen || vm.doc.Version() != doc.Version {
				return
			}
			vm.lens.cancel = nil
			cancel()
			vm.lens.deb.update(time.Since(started))
			if h := vm.sg.host; h != nil && h.Log != nil {
				for _, e := range errs {
					if !errors.Is(e.Err, context.Canceled) {
						h.Log("code lens provider "+e.Owner+" failed", e.Err)
					}
				}
			}
			if len(errs) > 0 && len(lenses) == 0 {
				return // a failure keeps what is shown
			}
			vm.lensSet(lenses, doc.Version)
		})
	}()
}

// lensSet takes the lenses: a row (and its zone) for each line that has some. The zones are replaced in one change.
func (vm *ViewModel) lensSet(lenses []langfeatures.CodeLens, version int) {
	l := &vm.lens
	old := l.rows
	l.rows, l.version = nil, version
	snap := vm.doc.Snapshot()
	_ = vm.ChangeZones(func(z ZoneChanges) error {
		for _, r := range old {
			if err := z.Remove(r.zone); err != nil {
				return err
			}
		}
		for _, lens := range lenses {
			line := snap.ValidatePosition(lens.Range.Start).Line
			item := &lensItem{lens: lens}
			if n := len(l.rows); n > 0 && l.rows[n-1].line == line {
				l.rows[n-1].items = append(l.rows[n-1].items, item)
				continue
			}
			// the zone comes after the line above: AfterLine is the number of lines above it
			id, err := z.Add(ZoneSpec{AfterLine: line, Rows: 1})
			if err != nil {
				return err
			}
			l.rows = append(l.rows, &lensRow{zone: id, line: line, items: []*lensItem{item}})
		}
		return nil
	})
	vm.lensRefreshWidgets()
	vm.lensResolveSoon()
}

// lensRefreshWidgets gives the zones their widgets again, for lenses that were resolved or for a change of the look.
func (vm *ViewModel) lensRefreshWidgets() {
	l := &vm.lens
	if len(l.rows) == 0 {
		return
	}
	_ = vm.ChangeZones(func(z ZoneChanges) error {
		for _, r := range l.rows {
			row := r
			if err := z.Change(row.zone, func(s *ZoneSpec) { s.Widget = newLensRowWidget(vm, row) }); err != nil {
				return err
			}
		}
		return nil
	})
}

// lensCancelResolve ends the resolving that is going on.
func (vm *ViewModel) lensCancelResolve() {
	l := &vm.lens
	l.resolveGen++
	if l.cancelResolve != nil {
		l.cancelResolve()
		l.cancelResolve = nil
	}
	if l.cancelResolveIn != nil {
		l.cancelResolveIn()
		l.cancelResolveIn = nil
	}
}

// lensResolveSoon resolves the lenses on the screen after a pause: a scroll does not ask for a command per line it passes.
func (vm *ViewModel) lensResolveSoon() {
	l := &vm.lens
	if l.cancelResolveIn != nil {
		l.cancelResolveIn()
		l.cancelResolveIn = nil
	}
	if vm.afterTimer == nil {
		vm.lensResolve()
		return
	}
	l.cancelResolveIn = vm.afterTimer(lensResolveDelay, func() {
		l.cancelResolveIn = nil
		vm.lensResolve()
	})
}

// lensResolveDelay is how long a scroll must rest before the lenses on the screen are resolved (VS Code adapts this to the
// time the providers take, from 250 ms).
const lensResolveDelay = 250 * time.Millisecond

// lensResolve asks the providers for the commands of the lenses that are on the screen and have none.
//
// Ported from VS Code: codelensController.ts (_resolveCodeLensesInViewport)
func (vm *ViewModel) lensResolve() {
	l := &vm.lens
	f := vm.hoverFeatures()
	if f == nil || len(l.rows) == 0 {
		return
	}
	proj := vm.Projection()
	top := vm.ScrollTop()
	_, height := vm.ViewportSize()
	firstLine, _, _ := proj.RowSpan(proj.RowAt(top))
	lastLine, _, _ := proj.RowSpan(proj.RowAt(top + height))
	var todo []*lensItem
	for _, r := range l.rows {
		line := vm.lensLine(r)
		if line < firstLine-1 || line > lastLine+1 {
			continue
		}
		for _, it := range r.items {
			if !it.lens.Resolved() && !it.resolving && !it.failed && f.CanResolveCodeLens(it.lens) {
				todo = append(todo, it)
			}
		}
	}
	if len(todo) == 0 {
		return
	}
	doc := vm.langDocument()
	l.resolveGen++
	gen := l.resolveGen
	base := context.Background()
	if h := vm.sg.host; h != nil && h.Ctx != nil {
		base = h.Ctx
	}
	ctx, cancel := context.WithCancel(base)
	if l.cancelResolve != nil {
		l.cancelResolve()
	}
	l.cancelResolve = cancel
	for _, it := range todo {
		it.resolving = true
		lens := it.lens
		go func() {
			got, err := f.ResolveCodeLens(ctx, doc, lens)
			vm.post(func() {
				it.resolving = false
				if vm.lens.resolveGen != gen {
					return // the text changed or the lenses were replaced
				}
				if err != nil {
					it.failed = true
					if h := vm.sg.host; h != nil && h.Log != nil && !errors.Is(err, context.Canceled) {
						h.Log("code lens resolve failed", err)
					}
					return
				}
				it.lens = got
				it.failed = !got.Resolved()
				vm.lensRefreshWidgets()
			})
		}()
	}
}

// lensLine is the line a row's zone is above now (the zone follows the edits).
func (vm *ViewModel) lensLine(r *lensRow) int {
	if spec, ok := vm.Zone(r.zone); ok {
		return spec.AfterLine
	}
	return r.line
}

// lensExecute runs the command of a lens on the UI thread: a command of the language server goes to it, any other is a command
// of the window.
func (vm *ViewModel) lensExecute(it *lensItem) {
	h := vm.sg.host
	if h == nil || h.Execute == nil || it.lens.Command == nil || it.lens.Command.ID == "" {
		return
	}
	ctx := h.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := h.Execute(ctx, *it.lens.Command); err != nil && h.Report != nil {
		h.Report(err)
	}
}

// CodeLensTitles are the titles of the resolved lenses of the rows, in order, one list per row (for a test and for the
// accessibility of the editor).
func (vm *ViewModel) CodeLensTitles() [][]string {
	out := make([][]string, 0, len(vm.lens.rows))
	for _, r := range vm.lens.rows {
		var titles []string
		for _, it := range r.items {
			if it.lens.Command != nil {
				titles = append(titles, it.lens.Command.Title)
			}
		}
		out = append(out, titles)
	}
	return out
}

// CodeLensRunAt runs the command of the lens that has a title in the row of the zone, as a click on it does; false when there is
// none.
func (vm *ViewModel) CodeLensRunAt(zone ZoneID, index int) bool {
	for _, r := range vm.lens.rows {
		if r.zone != zone {
			continue
		}
		var shown []*lensItem
		for _, it := range r.items {
			if it.lens.Command != nil && it.lens.Command.Title != "" {
				shown = append(shown, it)
			}
		}
		if index < 0 || index >= len(shown) {
			return false
		}
		vm.lensExecute(shown[index])
		return true
	}
	return false
}

// CodeLensZones are the view zones of the lens rows, in order.
func (vm *ViewModel) CodeLensZones() []ZoneID {
	out := make([]ZoneID, len(vm.lens.rows))
	for i, r := range vm.lens.rows {
		out[i] = r.zone
	}
	return slices.Clone(out)
}

// lensRowWidget is the widget of the zone of a row of lenses: their titles in a row, indented as the line below.
type lensRowWidget struct {
	widgets.StatelessWidget
	vm  *ViewModel
	row *lensRow
}

func newLensRowWidget(vm *ViewModel, row *lensRow) widgets.Widget {
	return &lensRowWidget{StatelessWidget: widgets.NewStatelessWidgetBase(), vm: vm, row: row}
}

// Build implements widgets.StatelessWidget.
func (w *lensRowWidget) Build(bc widgets.BuildContext) widgets.Widget {
	vm := w.vm
	colors := kit.ThemeOf(bc)
	opts := vm.Options()
	size := opts.Lens.CodeLensFontSize
	if size <= 0 {
		size = opts.FontSize * 0.9
	}
	family, fallback := "", []string(nil)
	if f := FontFamilies(opts.Lens.CodeLensFontFamily); len(f) > 0 {
		family, fallback = f[0], append(f[1:], opts.FontFamilies...)
	} else if len(opts.FontFamilies) > 0 {
		family, fallback = opts.FontFamilies[0], opts.FontFamilies[1:]
	}
	style := painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: family, FontFamilyFallback: fallback, FontSize: foundation.Some(size),
		Height: foundation.Some(opts.LineHeight / size), Color: foundation.Some(colors.CodeLensForeground),
	})
	var children []widgets.Widget
	for _, it := range w.row.items {
		if it.lens.Command == nil || it.lens.Command.Title == "" {
			continue
		}
		if len(children) > 0 {
			children = append(children, widgets.NewText(" | ", widgets.TextOpts{Style: style}))
		}
		title := it.lens.Command.Title
		item := it
		children = append(children, widgets.NewMouseRegion(
			widgets.NewGestureDetector(widgets.NewText(title, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1)}), widgets.GestureDetectorOpts{
				Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { vm.lensExecute(item) },
			}), widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click}))
	}
	indent := vm.lensIndent(bc, w.row)
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(indent, 0, 0, 0),
		widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
}

// lensIndent is how far from the left edge of the text the titles start: the first character of the line below that is not
// white space, as VS Code puts them.
func (vm *ViewModel) lensIndent(bc widgets.BuildContext, r *lensRow) float64 {
	line := vm.lensLine(r)
	text, err := vm.doc.Snapshot().LineContent(line)
	if err != nil {
		return 0
	}
	col := 0
	for col < len(text) && (text[col] == ' ' || text[col] == '\t') {
		col++
	}
	geo := newGeometry(vm, textFactory(bc))
	return geo.x(lineRow(vm, line), col)
}
