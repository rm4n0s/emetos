package editor

import (
	"context"
	"path/filepath"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of VS Code's breadcrumbs bar and its picker.
const (
	// BreadcrumbsHeight is the height of the bar.
	BreadcrumbsHeight = 22.0
	breadcrumbsFont   = 13.0
	breadcrumbsInset  = 8.0
	breadcrumbsPad    = 3.0
	pickerWidth       = 280.0
	pickerRows        = 12
)

// BreadcrumbsOpts are the options of NewBreadcrumbs.
type BreadcrumbsOpts struct {
	// Root is the folder that is open: a file inside it is shown relative to it. Empty when there
	// is no folder.
	Root string
	// OnOpen is called with the path of a file the user chose in a picker.
	OnOpen func(path string)
	// ReadDir lists a folder for a picker; the default is ReadSiblings. The workbench replaces it
	// with one that applies files.exclude.
	ReadDir func(ctx context.Context, dir string) ([]BreadcrumbEntry, error)
}

// Breadcrumbs is the bar above an editor with the path of its file. Each segment opens a picker
// of its siblings: a folder in the picker is entered, a file is opened through OnOpen. The folders
// are read in the background with the ctx the bar is made with. The symbol part of the bar comes
// with area 3.
type Breadcrumbs struct {
	widgets.StatefulWidget
	ctx     context.Context
	file    string
	options BreadcrumbsOptions
	opts    BreadcrumbsOpts
}

// NewBreadcrumbs makes the bar for the file at an absolute path ("" for a document without one,
// which has no bar). It is empty, with no height, when breadcrumbs.enabled is off or the path has
// no segments. The ctx stops the reading of folders when it is cancelled.
func NewBreadcrumbs(ctx context.Context, file string, options BreadcrumbsOptions, opts ...BreadcrumbsOpts) *Breadcrumbs {
	b := &Breadcrumbs{StatefulWidget: widgets.NewStatefulWidgetBase(), ctx: ctx, file: file, options: options}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	return b
}

// Items are the segments the bar shows.
func (b *Breadcrumbs) Items() []BreadcrumbItem {
	if !b.options.Enabled {
		return nil
	}
	return BreadcrumbItems(b.file, b.opts.Root, b.options.FilePath)
}

// CreateState implements widgets.StatefulWidget.
func (b *Breadcrumbs) CreateState() widgets.State { return &breadcrumbsState{} }

// picker is what the open picker shows.
type picker struct {
	// start is the folder the picker opened on; a picker that went deeper offers ".." to come back.
	start string
	// dir is the folder it lists.
	dir string
	// focus is the name of the entry that has the focus at first.
	focus string
	// rows are the entries it lists, with ".." first when it is below its start.
	rows   []BreadcrumbEntry
	loaded bool
}

type breadcrumbsState struct {
	widgets.StateOf[*Breadcrumbs]
	binding     *widgets.WidgetsBinding
	controllers []*widgets.OverlayPortalController
	// hover is the segment under the pointer, open the one whose picker is open; -1 for none.
	hover, open int
	picker      picker
	// gen counts the reads of folders; the answer of an old read is dropped.
	gen    int
	cancel context.CancelFunc
}

// InitState prepares the state.
func (s *breadcrumbsState) InitState() {
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.hover, s.open = -1, -1
}

// Dispose stops the read that is running.
func (s *breadcrumbsState) Dispose() {
	if s.cancel != nil {
		s.cancel()
	}
}

// DidUpdateWidget closes the picker when the file is another.
func (s *breadcrumbsState) DidUpdateWidget(old *Breadcrumbs) {
	if old.file != s.Widget().file && s.open >= 0 {
		// the portals cannot be hidden while the tree is built: the builder of the picker shows
		// nothing for a segment that is not open, and the next close hides them
		s.reset()
	}
}

func (s *breadcrumbsState) readDir() func(context.Context, string) ([]BreadcrumbEntry, error) {
	if f := s.Widget().opts.ReadDir; f != nil {
		return f
	}
	return ReadSiblings
}

// toggle opens the picker of a segment, or closes it when it is the open one.
func (s *breadcrumbsState) toggle(items []BreadcrumbItem, i int) {
	if s.open == i {
		s.close()
		return
	}
	s.close()
	s.open = i
	dir := filepath.Dir(items[i].Path)
	s.picker = picker{start: dir, dir: dir, focus: items[i].Name}
	s.load(dir)
	s.SetState(func() {})
}

// load reads a folder in the background; when it is read the picker shows it.
func (s *breadcrumbsState) load(dir string) {
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(s.Widget().ctx)
	s.cancel = cancel
	s.gen++
	gen, open, read := s.gen, s.open, s.readDir()
	go func() {
		entries, err := read(ctx, dir)
		s.binding.Post(func() {
			if !s.Mounted() || gen != s.gen {
				return
			}
			if err != nil {
				// the folder cannot be read: the picker says so in a row that cannot be chosen
				entries = []BreadcrumbEntry{{Name: "Cannot read " + filepath.Base(dir)}}
			}
			s.picker.dir = dir
			s.picker.rows = s.rowsOf(dir, entries)
			s.picker.loaded = true
			s.SetState(func() {})
			if open < len(s.controllers) {
				s.controllers[open].Show()
			}
		})
	}()
}

// rowsOf are the rows of a folder's entries: ".." first when the folder is below the one the
// picker opened on. A row of an error has no path and is disabled by the list.
func (s *breadcrumbsState) rowsOf(dir string, entries []BreadcrumbEntry) []BreadcrumbEntry {
	var rows []BreadcrumbEntry
	if dir != s.picker.start {
		rows = append(rows, BreadcrumbEntry{Name: "..", Path: filepath.Dir(dir), Dir: true})
	}
	return append(rows, entries...)
}

// reset forgets the open picker and stops the read that is running.
func (s *breadcrumbsState) reset() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.gen++
	s.open = -1
	s.picker = picker{}
}

// close closes the picker; it is called from an event, never while the tree is built.
func (s *breadcrumbsState) close() {
	s.reset()
	for _, c := range s.controllers {
		if c.IsShowing() {
			c.Hide()
		}
	}
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// choose handles the row of the picker that the user chose.
func (s *breadcrumbsState) choose(index int) {
	if index < 0 || index >= len(s.picker.rows) {
		return
	}
	row := s.picker.rows[index]
	if row.Path == "" {
		return
	}
	if row.Dir {
		s.picker.focus = ""
		s.load(row.Path)
		return
	}
	s.close()
	if f := s.Widget().opts.OnOpen; f != nil {
		f(row.Path)
	}
}

// Build implements widgets.State.
func (s *breadcrumbsState) Build(bc widgets.BuildContext) widgets.Widget {
	b := s.Widget()
	items := b.Items()
	if len(items) == 0 {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(0.0)})
	}
	for len(s.controllers) < len(items) {
		s.controllers = append(s.controllers, widgets.NewOverlayPortalController())
	}
	colors := kit.ThemeOf(bc)
	row := []widgets.Widget{widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(breadcrumbsInset)})}
	for i := range items {
		if i > 0 {
			row = append(row, widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconChevronRight), widgets.IconOpts{
				Size: foundation.Some(16.0), Color: foundation.Some(colors.BreadcrumbForeground),
			}))
		}
		row = append(row, widgets.NewFlexible(s.item(bc, colors, items, i)))
	}
	return widgets.NewContainer(
		widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.ContainerOpts{Height: foundation.Some(BreadcrumbsHeight), Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color: foundation.Some(colors.BreadcrumbBackground),
		})})
}

// item is one segment: its icon and name, the click that opens its picker, and the picker.
func (s *breadcrumbsState) item(bc widgets.BuildContext, colors kit.Colors, items []BreadcrumbItem, i int) widgets.Widget {
	it := items[i]
	fg := colors.BreadcrumbForeground
	switch {
	case s.open == i:
		fg = colors.BreadcrumbActiveSelectionForeground
	case s.hover == i:
		fg = colors.BreadcrumbFocusForeground
	}
	var parts []widgets.Widget
	if s.Widget().options.Icons {
		code := fonts.CodiconSymbolFile
		if it.Dir {
			code = fonts.CodiconFolder
		}
		parts = append(parts, widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(fg)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(2.0)}))
	}
	parts = append(parts, widgets.NewFlexible(widgets.NewText(it.Name, widgets.TextOpts{
		Style: kit.TextStyleOf(bc, breadcrumbsFont, fg), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
	})))
	body := widgets.NewMouseRegion(
		widgets.NewGestureDetector(
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, breadcrumbsPad), widgets.NewRow(parts, widgets.RowOpts{
				MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter,
			})),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { s.toggle(items, i) }}),
		widgets.MouseRegionOpts{
			Cursor:  services.SystemMouseCursors.Click,
			OnEnter: func(*gestures.PointerEnterEvent) { s.setHover(i) },
			OnExit:  func(*gestures.PointerExitEvent) { s.setHover(-1) },
		})
	// a click on another segment is not a click outside the open picker: the segment handles it
	inGroup := widgets.NewTapRegion(body, widgets.TapRegionOpts{GroupID: s})
	return widgets.NewOverlayPortalOverlayChildLayoutBuilder(s.controllers[i], s.pickerBuilder(i), inGroup)
}

func (s *breadcrumbsState) setHover(i int) {
	if s.hover != i && s.Mounted() {
		s.SetState(func() { s.hover = i })
	}
}

// pickerBuilder builds the picker of segment i below the segment.
func (s *breadcrumbsState) pickerBuilder(i int) widgets.OverlayChildLayoutBuilder {
	return func(bc widgets.BuildContext, info widgets.OverlayChildLayoutInfo) widgets.Widget {
		if s.open != i || !s.picker.loaded {
			return widgets.NewSizedBox(nil)
		}
		origin := info.ChildPaintTransform.TransformPoint(ui.Offset{})
		left := max(min(origin.Dx, info.OverlaySize.Width-pickerWidth), 0)
		return widgets.NewStack([]widgets.Widget{
			widgets.NewPositioned(s.pickerList(kit.ThemeOf(bc)), widgets.PositionedOpts{
				Left: foundation.Some(left), Top: foundation.Some(origin.Dy + info.ChildSize.Height),
			}),
		}, widgets.StackOpts{Fit: rendering.StackFitExpand})
	}
}

// pickerList is the list of the open picker.
func (s *breadcrumbsState) pickerList(colors kit.Colors) widgets.Widget {
	p := s.picker
	items := make([]kit.ListItem, len(p.rows))
	focused := foundation.None[int]()
	for i, r := range p.rows {
		code := fonts.CodiconSymbolFile
		switch {
		case r.Name == "..":
			code = fonts.CodiconArrowUp
		case r.Dir:
			code = fonts.CodiconFolder
		}
		items[i] = kit.ListItem{Label: r.Name, Icon: code, Disabled: r.Path == ""}
		if p.focus != "" && r.Name == p.focus && !focused.IsSome() {
			focused = foundation.Some(i)
		}
	}
	return kit.NewListOverlay(items, kit.ListOverlayOpts{
		Key: foundation.NewValueKey(p.dir), TapGroup: s,
		Filterable: true, Rows: pickerRows, Width: foundation.Some(pickerWidth), Focused: focused,
		Background: foundation.Some(colors.BreadcrumbPickerBackground),
		OnSelect:   s.choose,
		OnDismiss:  s.close,
	})
}
