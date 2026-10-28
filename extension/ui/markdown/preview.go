package markdown

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The numbers of the preview of a document.
const (
	// topPad and bottomPad are the space above the first block and below the last (the padding of the page).
	topPad, bottomPad = 16.0, 40.0
	// previewDelay is how long a change of the text waits before the preview shows it: the delay of VS Code's preview
	// (`#delay` of preview.ts); the first text is shown at once.
	previewDelay = 300 * time.Millisecond
	// syncParseLimit is the size of a text, in bytes, that is parsed on the UI thread: parsing it takes less than the hand-off
	// to another goroutine.
	syncParseLimit = 16 << 10
	// cacheMargin is how far beyond the viewport blocks are built, in logical pixels (Flutter's default cache extent is 250).
	// Glutter 1.9.2 skips the shapes outside the clip before it makes their mask (its step 29.13), so a block built off screen
	// costs much less than before; the margin stays at Flutter's default, which a jump and a scroll by a wheel step both need.
	cacheMargin = 250.0
	// coverFactor shrinks the estimates of the blocks that were never measured when the window is chosen, so that an estimate
	// that is too high does not leave a gap at the bottom of the viewport.
	coverFactor = 0.8
	// maxCoverRetries is how many times in a row a frame that leaves a gap asks for another build.
	maxCoverRetries = 4
)

// preview is the widget of NewView for the page of a document.
type preview struct {
	widgets.StatefulWidget
	src  string
	opts Options
}

// CreateState implements widgets.StatefulWidget.
func (p *preview) CreateState() widgets.State { return &previewState{} }

// document is a parsed text.
type document struct {
	src    string
	blocks []Block
	keys   []blockKey
	byKey  map[blockKey]int
}

// newDocument parses a text. It touches no state, so it runs on any goroutine.
func newDocument(src string) *document {
	d := &document{src: src, blocks: Parse(src)}
	d.keys = blockKeys(d.blocks)
	d.byKey = make(map[blockKey]int, len(d.keys))
	for i, k := range d.keys {
		d.byKey[k] = i
	}
	return d
}

// lineIndex is the index of the last block that starts at or before the line, or -1.
func (d *document) lineIndex(line int) int {
	i := sort.Search(len(d.blocks), func(i int) bool { return d.blocks[i].Span().StartLine > line })
	return i - 1
}

// builtBlock is the widget of a block that is near the viewport, and the recognizers of its links.
type builtBlock struct {
	w    widgets.Widget
	taps []gestures.GestureRecognizer
}

// previewState is the state of the preview of a document. It is not a ListView: it was made when a SliverList laid out every
// item between the first one it has and the offset it is given, so a jump (the scroll bar, a line) into a document of thousands
// of blocks built and measured all the blocks on the way. Glutter 1.9.2 has ListViewOpts.EstimateItemOffsets for that (its step
// 29.20), and the preview could be written on it, but it is kept: a jump chooses the blocks to build from estimates and
// positions them from an anchor. The blocks are rows of a list that knows their heights as far as they were laid out, and
// estimates for the others; the part of it near the viewport is built, and positioned from an anchor, a block whose place is
// known, so that the content does not move when a block above it turns out taller or shorter than it was estimated (an image
// that has loaded).
type previewState struct {
	widgets.StateOf[*preview]
	binding *widgets.WidgetsBinding
	ctl     *Controller

	// the text
	doc      *document
	latest   string
	parsing  bool
	throttle *gestures.Timer

	// the look
	r          *renderer
	dirty      bool // the theme changed: make the renderer again
	tokens     *tokenCache
	images     *ImageCache
	ownsImages bool
	built      map[blockKey]*builtBlock

	// the list: the height of every block plus the gap below it, the heights that were measured, the place of the viewport as an
	// anchor block and the distance from the top of that block to the top of the viewport.
	est      estimator
	heights  *heightIndex
	measured map[blockKey]float64
	vw, vh   float64
	anchor   int
	offset   float64

	model         *previewModel
	afterPending  bool
	changed       bool
	coverRetries  int
	notCovered    bool
	layoutVersion int
	builds        int
}

// previewModel is the ScrollModel of the preview: the scrollbars and the wheel move the list through it.
type previewModel struct {
	foundation.ChangeNotifier
	s *previewState
}

// Metrics implements kit.ScrollModel.
func (m *previewModel) Metrics() kit.ScrollMetrics { return m.s.metrics() }

// ScrollTo implements kit.ScrollModel.
func (m *previewModel) ScrollTo(_, top float64) { m.s.scrollTo(top) }

// InitState makes the caches and starts parsing.
func (s *previewState) InitState() {
	v := s.Widget()
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.tokens = newTokenCache()
	s.images = v.opts.Images
	if s.images == nil {
		s.images, s.ownsImages = NewImageCache(0), true
	}
	s.images.attach(s.binding.Post)
	s.built = map[blockKey]*builtBlock{}
	s.measured = map[blockKey]float64{}
	s.model = &previewModel{ChangeNotifier: *foundation.NewChangeNotifier(), s: s}
	s.dirty = true
	s.attachController(v.opts.Controller)
	s.latest = v.src
	s.schedule(false)
}

// DidChangeDependencies makes the renderer again: the theme or the pixel ratio changed.
func (s *previewState) DidChangeDependencies() { s.dirty = true }

// DidUpdateWidget follows a new text and a new controller.
func (s *previewState) DidUpdateWidget(old *preview) {
	v := s.Widget()
	if v.opts.Controller != old.opts.Controller {
		if old.opts.Controller != nil && old.opts.Controller.view == controlled(s) {
			old.opts.Controller.view = nil
		}
		s.attachController(v.opts.Controller)
	}
	if v.src != s.latest {
		s.latest = v.src
		s.schedule(true)
	}
}

func (s *previewState) attachController(c *Controller) {
	if c == nil {
		c = NewController()
	}
	s.ctl = c
	c.view = s
}

// Dispose releases what the state holds.
func (s *previewState) Dispose() {
	if s.throttle != nil {
		s.throttle.Cancel()
	}
	if s.ctl.view == controlled(s) {
		s.ctl.view = nil
	}
	for _, b := range s.built {
		releaseTaps(b.taps)
	}
	if s.ownsImages {
		s.images.Close()
	}
}

func releaseTaps(taps []gestures.GestureRecognizer) {
	for _, t := range taps {
		t.Dispose()
	}
}

// ---- the text ----

// wanted tells whether the text shown is not the latest one.
func (s *previewState) wanted() bool { return s.doc == nil || s.doc.src != s.latest }

// schedule parses the latest text: at once for the first one, and for a change when the delay of the last one is over (VS
// Code's preview takes the text at the end of the delay, whatever happened in it). notify says that the state is mounted and
// is to build again.
func (s *previewState) schedule(notify bool) {
	if !s.wanted() || s.parsing || s.throttle != nil {
		return
	}
	if s.doc == nil {
		s.startParse(notify)
		return
	}
	s.throttle = s.binding.Gestures().NewTimer(previewDelay, func() {
		s.throttle = nil
		if s.Mounted() && s.wanted() && !s.parsing {
			s.startParse(true)
		}
	})
}

// startParse parses the latest text: a small one here (notify says that the state is mounted and builds again), a big one on a
// goroutine of its own, whose result is shown when it comes.
func (s *previewState) startParse(notify bool) {
	src := s.latest
	if len(src) <= syncParseLimit {
		s.apply(newDocument(src), notify)
		return
	}
	s.parsing = true
	post := s.binding.Post
	s.Go(func(context.Context) {
		d := newDocument(src)
		post(func() {
			s.parsing = false
			if !s.Mounted() {
				return
			}
			if d.src == s.latest {
				s.apply(d, true)
			}
			s.schedule(true)
		})
	})
}

// apply shows a parsed text: the heights that were measured for blocks that are still there are kept, and the viewport stays
// at the block it was at.
func (s *previewState) apply(d *document, notify bool) {
	old := s.doc
	s.doc = d
	if old != nil && s.anchor < len(old.keys) {
		if i, ok := d.byKey[old.keys[s.anchor]]; ok {
			s.anchor = i
		} else {
			s.anchor = max(d.lineIndex(old.blocks[s.anchor].Span().StartLine), 0)
		}
	} else {
		s.anchor, s.offset = 0, -topPad // the top of the page
	}
	s.anchor = min(s.anchor, max(len(d.blocks)-1, 0))
	keep := make(map[blockKey]float64, len(d.keys))
	for _, k := range d.keys {
		if h, ok := s.measured[k]; ok {
			keep[k] = h
		}
	}
	s.measured = keep
	for k, b := range s.built {
		if _, ok := d.byKey[k]; !ok {
			releaseTaps(b.taps)
			delete(s.built, k)
		}
	}
	s.rebuildHeights()
	s.layoutVersion++
	if s.ctl.hasPending {
		s.ctl.ScrollToLine(s.ctl.pending)
	}
	if notify && s.Mounted() {
		s.SetState(func() {})
	}
	s.changed = true
	s.scheduleAfterLayout()
}

// rebuildHeights makes the table of heights from the measurements and the estimates.
func (s *previewState) rebuildHeights() {
	if s.doc == nil {
		s.heights = nil
		return
	}
	units := make([]float64, len(s.doc.blocks))
	for i, b := range s.doc.blocks {
		h, ok := s.measured[s.doc.keys[i]]
		if !ok {
			h = s.est.blockHeight(b)
		}
		units[i] = h + blockGap
	}
	s.heights = newHeightIndex(units)
}

// ---- the viewport ----

// contentHeight is the height of everything: the padding of the page and the blocks with the gaps between them.
func (s *previewState) contentHeight() float64 {
	if s.heights == nil || s.heights.Len() == 0 {
		return 0
	}
	return topPad + s.heights.Total() - blockGap + bottomPad
}

// position is how far the content is scrolled (the distance from its top to the top of the viewport).
func (s *previewState) position() float64 {
	if s.heights == nil || s.heights.Len() == 0 {
		return 0
	}
	return max(topPad+s.heights.Prefix(s.anchor)+s.offset, 0)
}

func (s *previewState) metrics() kit.ScrollMetrics {
	return kit.ScrollMetrics{ViewportWidth: s.vw, ViewportHeight: s.vh, ContentWidth: s.vw, ContentHeight: s.contentHeight(), Top: s.position()}
}

// anchorAt gives the anchor block and the offset for a scroll position.
func (s *previewState) anchorAt(top float64) (int, float64) {
	x := top - topPad
	a := s.heights.Find(x)
	return a, x - s.heights.Prefix(a)
}

// scrollTo moves the viewport to a scroll position.
func (s *previewState) scrollTo(top float64) {
	if s.heights == nil || s.heights.Len() == 0 {
		return
	}
	top = min(max(top, 0), max(s.contentHeight()-s.vh, 0))
	a, o := s.anchorAt(top)
	if a == s.anchor && o == s.offset {
		return
	}
	s.anchor, s.offset = a, o
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// scrollToLine implements controlled.
func (s *previewState) scrollToLine(line int) bool {
	if s.doc == nil || s.heights == nil || len(s.doc.blocks) == 0 {
		return false
	}
	i := s.doc.lineIndex(line)
	if i < 0 {
		s.anchor, s.offset = 0, -topPad
	} else {
		s.anchor, s.offset = i, 0
	}
	if s.Mounted() {
		s.SetState(func() {})
	}
	return true
}

// topLine implements controlled: the start of the last block that starts at or before the line under the top edge.
func (s *previewState) topLine() int {
	if s.doc == nil || s.heights == nil || len(s.doc.blocks) == 0 {
		return 0
	}
	x := s.position() + 1 - topPad
	if x < 0 {
		return 0
	}
	return s.doc.blocks[s.heights.Find(x)].Span().StartLine
}

// stats implements controlled.
func (s *previewState) stats() Stats {
	st := Stats{Built: len(s.built), Builds: s.builds, Measured: len(s.measured), Parsing: s.parsing || s.throttle != nil || s.wanted(), ScrollTop: s.position(), ContentHeight: s.contentHeight(), TopLine: s.topLine()}
	if s.doc != nil {
		st.Blocks = len(s.doc.blocks)
	}
	st.ImageBytes, st.Images = s.images.Used(), s.images.Len()
	return st
}

// MarkdownStats implements Inspector.
func (s *previewState) MarkdownStats() Stats { return s.stats() }

// ---- build ----

// Build implements widgets.State.
func (s *previewState) Build(bc widgets.BuildContext) widgets.Widget {
	if s.dirty || s.r == nil {
		s.makeRenderer(bc)
	}
	body := widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget { return s.window(c) })
	scroll := kit.NewScrollableElement(body, kit.ScrollableElementOpts{
		Model: s.model, HandleMouseWheel: foundation.Some(true), Handle: &s.ctl.scroll,
	})
	return widgets.NewColoredBox(s.r.colors.EditorBackground, scroll)
}

// makeRenderer makes the renderer for the theme now; the widgets built with the old one are dropped.
func (s *previewState) makeRenderer(bc widgets.BuildContext) {
	s.r = newRenderer(bc, s.Widget().opts, s.tokens, s.images)
	if s.est.width > 0 {
		s.r.fit.maxWidth = math.Min(maxImageWidth, s.est.width)
	}
	s.dirty = false
	for k, b := range s.built {
		releaseTaps(b.taps)
		delete(s.built, k)
	}
}

// resize takes the size of the viewport; a new width makes the measurements and the estimates of the blocks obsolete.
func (s *previewState) resize(w, h float64) {
	s.vw, s.vh = w, h
	width := max(w-2*pagePadding, 40)
	if s.heights != nil && math.Abs(width-s.est.width) <= 0.5 {
		return
	}
	s.est.width = width
	s.r.fit.maxWidth = math.Min(maxImageWidth, width)
	clear(s.measured)
	for k, b := range s.built {
		releaseTaps(b.taps)
		delete(s.built, k)
	}
	s.rebuildHeights()
}

// window builds the part of the list near the viewport: it is called while the list is laid out, with the size it was given.
func (s *previewState) window(c rendering.BoxConstraints) widgets.Widget {
	if s.doc == nil || len(s.doc.blocks) == 0 {
		s.vw, s.vh = c.MaxWidth, c.MaxHeight
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(c.MaxWidth), Height: foundation.Some(c.MaxHeight)})
	}
	s.resize(c.MaxWidth, c.MaxHeight)
	n := len(s.doc.blocks)
	// the anchor is the block at the top of the viewport
	a, o := s.anchorAt(s.position())
	s.anchor, s.offset = min(a, n-1), o
	first, last := s.pickWindow()
	ids := make([]blockKey, 0, last-first+1)
	children := make([]widgets.Widget, 0, last-first+1)
	inWindow := make(map[blockKey]bool, last-first+1)
	for i := first; i <= last; i++ {
		key := s.doc.keys[i]
		ids = append(ids, key)
		inWindow[key] = true
		children = append(children, widgets.NewLayoutId(key, s.blockWidget(i)))
	}
	for k, b := range s.built {
		if !inWindow[k] {
			releaseTaps(b.taps)
			delete(s.built, k)
		}
	}
	version := s.layoutVersion
	plan := &blockPlan{
		ids: ids, first: first, last: last, anchor: s.anchor, count: n, offset: s.offset,
		report: func(heights []float64, shift float64, covered bool) {
			s.reported(version, first, heights, shift, covered)
		},
	}
	return widgets.NewCustomMultiChildLayout(&blockLayout{MultiChildLayoutDelegateBase: rendering.NewMultiChildLayoutDelegateBase(nil), plan: plan}, children)
}

// pickWindow chooses the blocks to build: from the anchor up and down until the viewport and the margin around it are covered
// by the heights as far as they are known.
func (s *previewState) pickWindow() (first, last int) {
	n := len(s.doc.blocks)
	cover := func(i int) float64 {
		h := s.heights.Get(i)
		if _, ok := s.measured[s.doc.keys[i]]; ok {
			return h
		}
		return h * coverFactor
	}
	y := -s.offset
	first = s.anchor
	for first > 0 && y > -cacheMargin {
		first--
		y -= cover(first)
	}
	bottom := -s.offset + cover(s.anchor)
	last = s.anchor
	for last < n-1 && bottom < s.vh+cacheMargin {
		last++
		bottom += cover(last)
	}
	return first, last
}

// blockWidget is the widget of a block: the one built before when there is one.
func (s *previewState) blockWidget(i int) widgets.Widget {
	key := s.doc.keys[i]
	if b, ok := s.built[key]; ok {
		return b.w
	}
	s.builds++
	b := &builtBlock{}
	r := *s.r
	r.addTap = func(t gestures.GestureRecognizer) { b.taps = append(b.taps, t) }
	b.w = r.block(s.doc.blocks[i], true)
	s.built[key] = b
	return b.w
}

// ---- layout ----

// blockPlan is what the layout of the window needs to know, and how it tells the state what it found.
type blockPlan struct {
	ids                        []blockKey
	first, last, anchor, count int
	offset                     float64
	report                     func(heights []float64, shift float64, covered bool)
}

// blockLayout positions the blocks of the window: the anchor block where the offset says, the ones before it above, the ones
// after it below, each at the height it has when laid out. The first block is not above the top of the page, and the last
// is not above the bottom of the viewport (the estimates of the blocks that were never laid out may be wrong).
type blockLayout struct {
	rendering.MultiChildLayoutDelegateBase
	plan *blockPlan
}

// PerformLayout implements rendering.MultiChildLayoutDelegate.
func (d *blockLayout) PerformLayout(size ui.Size) {
	p := d.plan
	width := math.Max(size.Width-2*pagePadding, 1)
	c := rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinWidth: foundation.Some(width), MaxWidth: foundation.Some(width)})
	heights := make([]float64, len(p.ids))
	for k, id := range p.ids {
		heights[k] = d.LayoutChild(id, c).Height
	}
	at := p.anchor - p.first
	ys := make([]float64, len(p.ids))
	ys[at] = -p.offset
	for k := at - 1; k >= 0; k-- {
		ys[k] = ys[k+1] - blockGap - heights[k]
	}
	for k := at + 1; k < len(ys); k++ {
		ys[k] = ys[k-1] + heights[k-1] + blockGap
	}
	shift := 0.0
	lastBottom := ys[len(ys)-1] + heights[len(ys)-1] + bottomPad
	switch {
	case p.first == 0 && ys[0] > topPad:
		shift = topPad - ys[0]
	case p.last == p.count-1 && lastBottom < size.Height:
		shift = size.Height - lastBottom
		if p.first == 0 {
			shift = math.Min(shift, topPad-ys[0])
		}
		shift = math.Max(shift, 0)
	}
	for k, id := range p.ids {
		d.PositionChild(id, ui.Offset{Dx: pagePadding, Dy: ys[k] + shift})
	}
	covered := (p.first == 0 || ys[0]+shift <= 0) && (p.last == p.count-1 || lastBottom-bottomPad+shift >= size.Height)
	p.report(heights, shift, covered)
}

// ShouldRelayout implements rendering.MultiChildLayoutDelegate.
func (d *blockLayout) ShouldRelayout(rendering.MultiChildLayoutDelegate) bool { return true }

// reported takes what a layout of the window found: the heights of its blocks, how far it moved the content to keep it within
// the page, and whether the blocks cover the viewport.
func (s *previewState) reported(version, first int, heights []float64, shift float64, covered bool) {
	if version != s.layoutVersion || s.doc == nil {
		return
	}
	for k, h := range heights {
		i := first + k
		if i >= len(s.doc.keys) {
			break
		}
		key := s.doc.keys[i]
		if prev, ok := s.measured[key]; !ok || prev != h {
			s.measured[key] = h
			s.heights.Set(i, h+blockGap)
			s.changed = true
		}
	}
	if shift != 0 {
		s.offset -= shift
		s.changed = true
	}
	s.notCovered = !covered
	s.scheduleAfterLayout()
}

// scheduleAfterLayout asks for the work that cannot be done while the list is laid out.
func (s *previewState) scheduleAfterLayout() {
	if s.afterPending || !s.Mounted() {
		return
	}
	s.afterPending = true
	scheduler := s.binding.Scheduler()
	scheduler.AddPostFrameCallback(func(time.Duration) { s.afterLayout() })
}

// afterLayout tells the scrollbars about new heights, and builds again when the blocks left a gap in the viewport.
func (s *previewState) afterLayout() {
	s.afterPending = false
	if !s.Mounted() {
		return
	}
	if s.changed {
		s.changed = false
		s.model.NotifyListeners()
	}
	if s.notCovered && s.coverRetries < maxCoverRetries {
		s.coverRetries++
		s.notCovered = false
		s.SetState(func() {})
		return
	}
	if !s.notCovered {
		s.coverRetries = 0
	}
}
