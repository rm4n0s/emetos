package diffeditor

import (
	"context"
	"time"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/diffEditorViewModel.ts (the recomputation of the
// diff), diffEditorWidget.ts (goToDiff, switchSide, the scroll sync of the two editors) and
// components/diffEditorViewZones/diffEditorViewZones.ts (the filler zones)

// DebounceDelay is how long the diff waits after the last edit of either text before it is computed again
// (VS Code's diff editor waits 200 ms).
const DebounceDelay = 200 * time.Millisecond

// ModelOpts are the options of NewModel.
type ModelOpts struct {
	// Original and Modified are the view models of the two editors, which the model puts the filler zones and
	// the marks of the changes on, and keeps scrolled together.
	Original, Modified *editor.ViewModel
	// Settings is read for diffEditor.ignoreTrimWhitespace and diffEditor.maxComputationTime, which the model
	// follows live; without it the defaults are used (white space ignored, 5 s).
	Settings *settings.Store
	// Post hands a function to the UI thread (the binding's Post); it is how the result of the computation, which
	// runs on a goroutine, arrives. Required.
	Post func(func())
	// AfterTimer calls fn on the UI thread after d and gives the function that cancels the call (the binding's
	// gestures.NewTimer); without it an edit recomputes the diff at once.
	AfterTimer func(d time.Duration, fn func()) (cancel func())
	// Now is the clock of the time limit of the diff (diff.Options.Now); nil means time.Now.
	Now func() time.Time
	// Context ends the computations when it is done; without it context.Background().
	Context context.Context
	// OnError is told about a computation that failed (the cancelled ones are not reported).
	OnError func(error)
	// OriginalFocus and ModifiedFocus give the keyboard focus to the editors (the widget hands them to the
	// editors it builds); without them the model has its own.
	OriginalFocus, ModifiedFocus *editor.EditorFocus
}

// zoneSet is the filler zones the model has put in one editor.
type zoneSet struct {
	ids []editor.ZoneID
}

// Model is the state of a diff editor: the diff of the two texts, kept up to date as they change, the filler
// zones that line the two editors up, the marks that color the changes, the scroll position the two editors share
// and the navigation between the changes. It works on the UI thread only; the diff is computed on a goroutine
// with a context that the next edit cancels, and the result comes back through ModelOpts.Post.
type Model struct {
	foundation.ChangeNotifier
	opts   ModelOpts
	vms    [2]*editor.ViewModel
	focus  [2]*editor.EditorFocus
	colors Colors

	changes    []diff.LineRangeMapping
	computed   bool
	hitTimeout bool
	applied    int
	computing  bool

	// gen counts the computations that were started; a result of an older one is dropped.
	gen         int
	cancel      context.CancelFunc
	cancelTimer func()

	zones [2]zoneSet
	// mode is the view the editors are laid out for now, width the width of the widget (0 until it was laid
	// out), inline the zones of the deleted lines of the inline view and inlineSig what they were made for.
	mode       Mode
	width      float64
	inline     []inlineZone
	inlineSig  inlineSig
	modeQueued bool
	handles    [2]foundation.ListenerHandle
	scrolls    [2]foundation.ListenerHandle
	metrics    [2]foundation.ListenerHandle
	stops      []func()
	// syncing is set while one editor scrolls the other, so that the scroll does not come back.
	syncing bool
	// leader is the side that scrolled last; the other follows it when the sizes change.
	leader        Side
	realignQueued bool
	disposed      bool
}

// NewModel makes the model of a diff editor. It computes nothing until Start.
func NewModel(opts ModelOpts) *Model {
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	m := &Model{ChangeNotifier: *foundation.NewChangeNotifier(), opts: opts, leader: Modified}
	m.vms = [2]*editor.ViewModel{opts.Original, opts.Modified}
	m.focus = [2]*editor.EditorFocus{opts.OriginalFocus, opts.ModifiedFocus}
	for i := range m.focus {
		if m.focus[i] == nil {
			m.focus[i] = &editor.EditorFocus{}
		}
	}
	for i := range m.vms {
		side := Side(i)
		vm := m.vms[i]
		m.stops = append(m.stops, vm.Document().Subscribe(func(document.ChangeEvent) { m.textChanged() }))
		m.handles[i] = vm.AddListener(m.viewChanged)
		m.scrolls[i] = vm.AddScrollListener(func() { m.scrolled(side) })
		m.metrics[i] = vm.AddMetricsListener(func() { m.metricsChanged() })
	}
	if opts.Settings != nil {
		m.stops = append(m.stops, opts.Settings.OnChange(func(e settings.ChangeEvent) {
			if e.Affects("diffEditor.ignoreTrimWhitespace", "") || e.Affects("diffEditor.maxComputationTime", "") {
				opts.Post(func() {
					if !m.disposed {
						m.Recompute()
					}
				})
			}
			if e.Affects("diffEditor.renderSideBySide", "") || e.Affects("diffEditor.useInlineViewWhenSpaceIsLimited", "") ||
				e.Affects("diffEditor.renderSideBySideInlineBreakpoint", "") {
				m.queueMode()
			}
		}))
	}
	return m
}

// View is the view model of a side.
func (m *Model) View(s Side) *editor.ViewModel { return m.vms[s] }

// Focus gives the keyboard focus to the editor of a side while it is on screen.
func (m *Model) Focus(s Side) *editor.EditorFocus { return m.focus[s] }

// FocusedSide tells which editor has the keyboard focus; false when neither does.
func (m *Model) FocusedSide() (Side, bool) {
	switch {
	case m.focus[Modified].HasFocus():
		return Modified, true
	case m.focus[Original].HasFocus():
		return Original, true
	}
	return Original, false
}

// Changes are the changes of the diff as last computed: lines counted from 1, the end excluded, with the
// character changes of each. They describe the texts as they were when the computation started; while a new
// computation is pending they may be a little behind the texts.
func (m *Model) Changes() []diff.LineRangeMapping { return m.changes }

// Computed tells whether a diff has been computed.
func (m *Model) Computed() bool { return m.computed }

// Computing tells whether a computation is running or waiting for the edits to stop.
func (m *Model) Computing() bool { return m.computing }

// NoChanges tells whether the texts are the same: the diff is computed and has no change.
func (m *Model) NoChanges() bool { return m.computed && len(m.changes) == 0 }

// HitTimeout tells whether the last computation ran out of time (diffEditor.maxComputationTime): the changes are
// then coarse, whole blocks, not an error.
func (m *Model) HitTimeout() bool { return m.hitTimeout }

// Applied is how many results of the computation the model has taken in. A result that was cancelled or
// outdated does not count.
func (m *Model) Applied() int { return m.applied }

// SetColors sets the colors of the changes; the marks are made again when they differ from the ones in use.
func (m *Model) SetColors(c Colors) {
	if c == m.colors {
		return
	}
	m.colors = c
	if m.computed {
		m.opts.Post(func() {
			if !m.disposed {
				m.applyMarks()
				if err := m.align(); err != nil && m.opts.OnError != nil {
					m.opts.OnError(err)
				}
				m.NotifyListeners()
			}
		})
	}
}

// Colors are the colors of the changes the model uses.
func (m *Model) Colors() Colors { return m.colors }

// Start computes the diff for the first time.
func (m *Model) Start() { m.Recompute() }

// textChanged follows an edit of either text: the running computation is cancelled and a new one starts when the
// edits stop for DebounceDelay.
func (m *Model) textChanged() {
	if m.disposed {
		return
	}
	m.stopComputation()
	m.computing = true
	if m.opts.AfterTimer == nil {
		m.start()
		return
	}
	m.cancelTimer = m.opts.AfterTimer(DebounceDelay, func() {
		m.cancelTimer = nil
		if !m.disposed {
			m.start()
		}
	})
}

// stopComputation cancels the computation that is running and the wait for the next one.
func (m *Model) stopComputation() {
	m.gen++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.cancelTimer != nil {
		m.cancelTimer()
		m.cancelTimer = nil
	}
	m.computing = false
}

// Recompute computes the diff now, without waiting for the edits to stop (a setting changed, or the editor opened).
func (m *Model) Recompute() {
	if m.disposed {
		return
	}
	m.stopComputation()
	m.start()
}

// diffOptions reads the settings the diff depends on.
func (m *Model) diffOptions() diff.Options {
	o := diff.Options{IgnoreTrimWhitespace: true, MaxComputationTime: 5 * time.Second, Now: m.opts.Now}
	if s := m.opts.Settings; s != nil {
		lang := m.vms[Modified].Document().LanguageID()
		o.IgnoreTrimWhitespace = settings.Get[bool](s, "diffEditor.ignoreTrimWhitespace", lang)
		o.MaxComputationTime = timeLimit(settings.Get[float64](s, "diffEditor.maxComputationTime", lang))
	}
	return o
}

// timeLimit is the time limit a setting in milliseconds stands for: zero and less is no limit, as in VS Code.
func timeLimit(ms float64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms * float64(time.Millisecond))
}

// start runs the computation on a goroutine for the texts as they are now.
func (m *Model) start() {
	m.gen++
	gen := m.gen
	ctx, cancel := context.WithCancel(m.opts.Context)
	m.cancel = cancel
	m.computing = true
	opts := m.diffOptions()
	orig, mod := m.vms[Original].Document().Snapshot(), m.vms[Modified].Document().Snapshot()
	origVersion, modVersion := m.vms[Original].Document().Version(), m.vms[Modified].Document().Version()
	post := m.opts.Post
	go func() {
		res, err := diff.Compute(ctx, snapshotLines(orig), snapshotLines(mod), opts)
		if ctx.Err() != nil {
			return // cancelled: another computation is on its way
		}
		post(func() { m.finish(gen, origVersion, modVersion, res, err) })
	}()
}

// snapshotLines are the lines of a snapshot, without their line breaks.
func snapshotLines(s buffer.Snapshot) []string {
	lines := make([]string, 0, s.LineCount())
	for _, line := range s.Lines(0) {
		lines = append(lines, line)
	}
	return lines
}

// finish takes in the result of a computation, unless it was replaced meanwhile.
func (m *Model) finish(gen, origVersion, modVersion int, res diff.LinesDiff, err error) {
	if m.disposed || gen != m.gen {
		return
	}
	m.cancel = nil
	m.computing = false
	if err != nil {
		if m.opts.OnError != nil {
			m.opts.OnError(err)
		}
		m.NotifyListeners()
		return
	}
	if m.vms[Original].Document().Version() != origVersion || m.vms[Modified].Document().Version() != modVersion {
		return // an edit came in; its recomputation is waiting
	}
	m.changes, m.hitTimeout, m.computed = res.Changes, res.HitTimeout, true
	m.applied++
	m.applyMarks()
	if err := m.align(); err != nil && m.opts.OnError != nil {
		m.opts.OnError(err)
	}
	m.NotifyListeners()
}

// viewChanged follows a change of the layout of an editor: when lines wrap, the rows of a line depend on the
// width and on the breaks that are known, and the filler has to follow them.
func (m *Model) viewChanged() {
	if m.disposed || !m.computed || m.mode == Inline || (!m.vms[Original].Wrapping() && !m.vms[Modified].Wrapping() && len(m.zones[0].ids)+len(m.zones[1].ids) == 0) {
		return
	}
	m.queueAlign()
}

// queueAlign aligns the editors again after the layout that is going on; several requests are one.
func (m *Model) queueAlign() {
	if m.realignQueued {
		return
	}
	m.realignQueued = true
	m.opts.Post(func() {
		m.realignQueued = false
		if m.disposed {
			return
		}
		if err := m.align(); err != nil && m.opts.OnError != nil {
			m.opts.OnError(err)
		}
		m.NotifyListeners()
	})
}

// extras are the wrapped lines of an editor: its lines that take more than one row.
func extras(vm *editor.ViewModel) []LineExtra {
	if !vm.Wrapping() {
		return nil
	}
	proj := vm.Projection()
	lines := vm.Document().Snapshot().LineCount()
	var out []LineExtra
	for line := 0; line < lines; line++ {
		if _, count := proj.LineRows(line); count > 1 {
			out = append(out, LineExtra{Line: line + 1, Rows: count - 1})
		}
	}
	return out
}

// Alignments are the stretches of the two texts that must be as tall on both sides, for the diff as it is and the
// wrapping of the lines as it is now.
func (m *Model) Alignments() []Alignment {
	orig := m.vms[Original].Document().Snapshot()
	return ComputeAlignment(m.changes, AlignOptions{
		Original: extras(m.vms[Original]), Modified: extras(m.vms[Modified]), InnerHunkAlignment: true,
		OriginalLineLength: func(line int) (int, bool) {
			n, err := orig.LineLength(line - 1)
			return n, err == nil
		},
	})
}

// align puts the filler zones in the editors: the side with fewer rows in a stretch gets the difference after
// it. Zones that are the same as the ones in the editor are left alone, so aligning again changes nothing.
func (m *Model) align() error {
	if !m.computed {
		return nil
	}
	if m.mode == Inline {
		errOrig := m.setZones(Original, nil)
		errMod := m.setZones(Modified, nil)
		errInline := m.setInlineZones()
		return firstError(errOrig, errMod, errInline)
	}
	errInline := m.clearInlineZones()
	orig, mod := Zones(m.Alignments())
	errOrig := m.setZones(Original, orig)
	errMod := m.setZones(Modified, mod)
	return firstError(errInline, errOrig, errMod)
}

// firstError is the first of some errors that is not nil.
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// setZones makes the zones of an editor the ones given.
func (m *Model) setZones(s Side, want []Zone) error {
	vm, set := m.vms[s], &m.zones[s]
	have := make([]Zone, 0, len(set.ids))
	for _, id := range set.ids {
		if spec, ok := vm.Zone(id); ok {
			have = append(have, Zone{AfterLine: spec.AfterLine, Rows: spec.Rows})
		}
	}
	if len(have) == len(set.ids) && sameZones(have, want) {
		return nil
	}
	var ids []editor.ZoneID
	err := vm.ChangeZones(func(c editor.ZoneChanges) error {
		for _, id := range set.ids {
			if err := c.Remove(id); err != nil {
				return err
			}
		}
		set.ids = nil
		for _, z := range want {
			id, err := c.Add(editor.ZoneSpec{AfterLine: z.AfterLine, Rows: z.Rows, ShowInHiddenAreas: true, Widget: newFiller()})
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	set.ids = ids
	return err
}

func sameZones(a, b []Zone) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ZonesOf are the filler zones an editor has now, in order.
func (m *Model) ZonesOf(s Side) []Zone {
	var out []Zone
	for _, z := range m.vms[s].Zones() {
		for _, id := range m.zones[s].ids {
			if id == z.ID {
				out = append(out, Zone{AfterLine: z.AfterLine, Rows: z.Rows})
			}
		}
	}
	return out
}

// applyMarks colors the changed lines and characters of both editors.
func (m *Model) applyMarks() {
	for _, s := range []Side{Original, Modified} {
		if m.mode == Inline && s == Original {
			// the original editor is not on screen: its lines are drawn in the zones of the modified one
			m.vms[s].SetBackgroundMarks(nil)
			continue
		}
		m.vms[s].SetBackgroundMarks(Marks(m.changes, s, m.colors, m.vms[s].Document().Snapshot()))
	}
}

// Marks are the background marks of the changes for one side: the lines of every change that has lines on the
// side, with the margin along them, and the characters of its character changes.
func Marks(changes []diff.LineRangeMapping, s Side, c Colors, snap buffer.Snapshot) []editor.BackgroundMark {
	line, gutter, text, border := c.InsertedLine, c.InsertedGutter, c.InsertedText, c.InsertedTextBorder
	if s == Original {
		line, gutter, text, border = c.RemovedLine, c.RemovedGutter, c.RemovedText, c.RemovedTextBorder
	}
	lines := snap.LineCount()
	var out []editor.BackgroundMark
	for _, ch := range changes {
		lr := ch.Modified
		if s == Original {
			lr = ch.Original
		}
		if !lr.IsEmpty() && lr.StartLine >= 1 && lr.EndLine-1 <= lines {
			last := lr.EndLine - 2
			n, _ := snap.LineLength(last)
			out = append(out, editor.BackgroundMark{
				Range:     buffer.Range{Start: buffer.Position{Line: lr.StartLine - 1}, End: buffer.Position{Line: last, Col: n}},
				WholeLine: true, Background: line, Gutter: gutter,
			})
		}
		for _, in := range ch.Inner {
			r := in.Modified
			if s == Original {
				r = in.Original
			}
			if r.Start == r.End || r.Start.Line < 1 || r.End.Line > lines {
				continue
			}
			out = append(out, editor.BackgroundMark{
				Range: buffer.Range{
					Start: buffer.Position{Line: r.Start.Line - 1, Col: r.Start.Column - 1},
					End:   buffer.Position{Line: r.End.Line - 1, Col: r.End.Column - 1},
				},
				Background: text, Border: border,
			})
		}
	}
	return out
}

// scrolled keeps the other editor where this one is.
func (m *Model) scrolled(s Side) {
	if m.disposed || m.syncing || m.mode == Inline {
		return
	}
	m.leader = s
	m.follow(s)
}

// follow scrolls the other editor to where the editor of a side is, both ways.
func (m *Model) follow(s Side) {
	from, to := m.vms[s], m.vms[s.Other()]
	m.syncing = true
	defer func() { m.syncing = false }()
	if from.ScrollTop() != to.ScrollTop() {
		to.ScrollTo(from.ScrollTop())
	}
	if from.ScrollLeft() != to.ScrollLeft() {
		to.SetScrollLeft(from.ScrollLeft())
	}
}

// metricsChanged follows a change of the sizes of an editor (the filler changed what scrolls): the editor that
// was scrolled last keeps the lead.
func (m *Model) metricsChanged() {
	if m.disposed || m.syncing || m.mode == Inline {
		return
	}
	m.follow(m.leader)
}

// GoToChange moves the cursor of the modified editor to the start of the next or the previous change, wrapping
// around, and shows it in the middle of the editor (VS Code's goToDiff). It reports false when there are no
// changes.
func (m *Model) GoToChange(next bool) bool {
	mod := m.vms[Modified]
	snap := mod.Document().Snapshot()
	i, ok := ChangeToGo(m.changes, mod.Cursors().Primary().Position().Line+1, snap.LineCount(), next)
	if !ok {
		return false
	}
	pos := buffer.Position{Line: m.changes[i].Modified.StartLine - 1}
	if pos.Line >= snap.LineCount() {
		pos.Line = snap.LineCount() - 1
		pos.Col, _ = snap.LineLength(pos.Line)
	}
	mod.GoToPosition(pos)
	return true
}

// MapToOtherSide gives the selection of the editor of a side as it is on the other side (VS Code's mapToOtherSide):
// its ends are translated through the changes.
func (m *Model) MapToOtherSide(from Side) buffer.Selection {
	sel := m.vms[from].Cursors().Primary().Selection
	a := TranslatePosition(m.changes, from, sel.Start())
	b := TranslatePosition(m.changes, from, sel.End())
	return buffer.Selection{Anchor: a.Start, Active: b.End}
}

// SwitchSide moves the keyboard focus to the other editor, with the selection mapped through the changes (VS Code's
// diffEditor.switchSide). The side that has the focus is the modified one when neither has it.
func (m *Model) SwitchSide() {
	if m.mode == Inline {
		return // one editor shows both texts
	}
	from := Modified
	if s, ok := m.FocusedSide(); ok {
		from = s
	}
	to := from.Other()
	sel := m.MapToOtherSide(from)
	dst := m.vms[to]
	snap := dst.Document().Snapshot()
	sel = buffer.Selection{Anchor: snap.ValidatePosition(sel.Anchor), Active: snap.ValidatePosition(sel.Active)}
	dst.Cursors().SetSelections([]buffer.Selection{sel})
	dst.SelectionsChanged()
	dst.RevealPrimary()
	m.focus[to].Request()
}

// FocusSide gives the keyboard focus to the editor of a side.
//
// The inline view has one editor, the modified one, which every side stands for.
func (m *Model) FocusSide(s Side) bool {
	if m.mode == Inline {
		s = Modified
	}
	return m.focus[s].Request()
}

// Dispose stops the computation, the timers and the listeners and takes the zones and the marks off the editors.
func (m *Model) Dispose() {
	if m.disposed {
		return
	}
	m.stopComputation()
	m.disposed = true
	for _, stop := range m.stops {
		stop()
	}
	m.stops = nil
	for i, vm := range m.vms {
		vm.RemoveListener(m.handles[i])
		vm.RemoveScrollListener(m.scrolls[i])
		vm.RemoveMetricsListener(m.metrics[i])
		vm.DisposeMarks()
		ids := m.zones[i].ids
		if i == int(Modified) {
			for _, z := range m.inline {
				ids = append(ids, z.id)
			}
			m.inline = nil
		}
		if err := vm.ChangeZones(func(c editor.ZoneChanges) error {
			var first error
			for _, id := range ids {
				if err := c.Remove(id); err != nil && first == nil {
					first = err
				}
			}
			return first
		}); err != nil && m.opts.OnError != nil {
			m.opts.OnError(err)
		}
		m.zones[i].ids = nil
	}
}
