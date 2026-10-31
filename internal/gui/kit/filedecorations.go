package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/decorations"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// File decorations in the views (SPEC §10.30 "File decorations and the Explorer", step 24.6): the Explorer, Open Editors, the editor
// tabs, the breadcrumbs, the Search results and the Source Control view draw the colors, badges and tooltips of the decorations
// service for the files they show. The window gives its subtree the service with a FileDecorations widget; a view asks for the look of
// a file with FileLookOf and draws it with the methods of FileLook. The labels of VS Code are the model (src/vs/workbench/browser/labels.ts
// and base/browser/ui/iconLabel/iconlabel.css).

// FileDecorations is the inherited widget that gives its subtree the file decorations service; the widgets that asked for a look are
// built again when Version has changed (the window rebuilds it when the service announces a change).
type FileDecorations struct {
	widgets.InheritedWidget
	svc     *decorations.Service
	version uint64
}

// NewFileDecorations makes a FileDecorations for the child over the service, at the service's version now.
func NewFileDecorations(svc *decorations.Service, child widgets.Widget) *FileDecorations {
	f := &FileDecorations{InheritedWidget: widgets.NewInheritedWidgetBase(child), svc: svc}
	if svc != nil {
		f.version = svc.Version()
	}
	return f
}

// UpdateShouldNotify implements widgets.InheritedWidgetInterface: the dependents are built again when the service or its version is
// another.
func (f *FileDecorations) UpdateShouldNotify(old widgets.Widget) bool {
	o := old.(*FileDecorations)
	return f.svc != o.svc || f.version != o.version
}

// FileDecorationsOf is the service of the nearest FileDecorations above bc, or nil, and rebuilds the caller when it changes.
func FileDecorationsOf(bc widgets.BuildContext) *decorations.Service {
	if f, ok := widgets.DependOn[*FileDecorations](bc); ok {
		return f.svc
	}
	return nil
}

// FileLookOpts say which parts of a decoration a view draws: explorer.decorations.colors and .badges, workbench.editor.decorations.colors
// and .badges, search.decorations.colors and .badges. The tooltip and the strike-through are drawn whatever they are.
type FileLookOpts struct {
	// Colors draws the name in the color of the decoration.
	Colors bool
	// Badges draws the badge, the icon or the dot of a folder.
	Badges bool
}

// FileLook is a decoration as a view draws it.
type FileLook struct {
	// Color is the color of the name; HasColor is false when the name keeps its own (also when Colors is off).
	Color    ui.Color
	HasColor bool
	// BadgeColor is the color of the badge: the color of the decoration whatever Colors says (VS Code's badge has its own CSS rule);
	// HasBadgeColor is false when the decoration names no color the theme knows.
	BadgeColor    ui.Color
	HasBadgeColor bool
	// Badge is the text at the end of the row, Icon the name of a Codicon in its place, and Bubble the small dot of a folder that
	// only holds decorated files; at most one of them is drawn, and none when Badges is off.
	Badge, Icon string
	Bubble      bool
	// Tooltip is the hover of the row.
	Tooltip string
	// Strikethrough draws the name struck through.
	Strikethrough bool
}

// ForRow is the look as the row draws it: in a selected row of a list that has the keyboard focus the colors of the selection win
// over the colors of the decoration, for the name and for the badge (VS Code's `.monaco-list:focus .selected .monaco-icon-label,
// .monaco-icon-label::after { color: inherit !important }`).
func (l FileLook) ForRow(st RowState) FileLook {
	if st.Selected && st.Active {
		l.HasColor, l.HasBadgeColor = false, false
	}
	return l
}

// RowState is st with the foreground the decoration gives the name; the label of the row is drawn in it.
func (l FileLook) RowState(st RowState) RowState {
	st.Foreground = l.NameColor(st.Foreground)
	return st
}

// NewDecoratedLabel is NewHighlightedLabel in the color of the look and struck through when the look says so.
func NewDecoratedLabel(bc widgets.BuildContext, text string, st RowState, look FileLook) widgets.Widget {
	return newHighlightedLabel(bc, text, look.ForRow(st).RowState(st), look.Strikethrough)
}

// HasBadge says that there is something to draw at the end of the row.
func (l FileLook) HasBadge() bool { return l.Badge != "" || l.Icon != "" || l.Bubble }

// ResolveFileLook turns a decoration of the service into what a view draws, with the colors of the theme.
func (c Colors) ResolveFileLook(d decorations.Decoration, opts FileLookOpts) FileLook {
	l := FileLook{Tooltip: d.Tooltip, Strikethrough: d.Strikethrough}
	for _, id := range d.Colors {
		if col, ok := c.Named(id); ok {
			l.BadgeColor, l.HasBadgeColor = col, true
			break
		}
	}
	if opts.Colors {
		l.Color, l.HasColor = l.BadgeColor, l.HasBadgeColor
	}
	if opts.Badges {
		l.Badge, l.Icon, l.Bubble = d.Badge, d.Icon, d.Bubble
	}
	return l
}

// FileLookOf is the look of the file at a URI (file:///path) from the service above bc; includeChildren is for a folder, which takes
// on the marks of the files below it. ok is false when the file has no decoration or there is no service above bc.
func FileLookOf(bc widgets.BuildContext, uri string, includeChildren bool, opts FileLookOpts) (FileLook, bool) {
	svc := FileDecorationsOf(bc)
	if svc == nil || uri == "" {
		return FileLook{}, false
	}
	d, ok := svc.Get(uri, includeChildren)
	if !ok {
		return FileLook{}, false
	}
	return ThemeOf(bc).ResolveFileLook(d, opts), true
}

// NameColor is the color of the name: the decoration's, or fallback.
func (l FileLook) NameColor(fallback ui.Color) ui.Color {
	if l.HasColor {
		return l.Color
	}
	return fallback
}

// NameStyle is style with the color of the decoration and the strike-through, if it has them.
func (l FileLook) NameStyle(style *painting.TextStyle) *painting.TextStyle {
	opts := painting.TextStyleOpts{}
	if l.HasColor {
		opts.Color = foundation.Some(l.Color)
	}
	if l.Strikethrough {
		opts.Decoration = foundation.Some(ui.TextDecorationLineThrough)
	}
	if !l.HasColor && !l.Strikethrough {
		return style
	}
	return style.CopyWith(opts)
}

// The metrics of the badge of a label (iconlabel.css `.monaco-icon-label::after` and decorationsService.ts).
const (
	badgeFontScale   = 0.9
	badgeOpacity     = 0.75
	badgeMarginLeft  = 5.0
	badgeMarginRight = 16.0
	badgeDotSize     = 14.0
	badgeDotOpacity  = 0.4
	badgeDotRight    = 14.0
	badgeIconSize    = 16.0
	badgeIconRight   = 14.0
)

// BadgeWidget draws the badge of the look at the end of a row: the text in 90% of fontSize, bold, at 75% opacity; the icon; or the dot
// at 40% opacity. fallback is the color of the row, used when the decoration names none (or when colors is false: the badge of a
// selected row follows the selection). The widget is nil when there is nothing to draw. The hover shows the tooltip.
func (l FileLook) BadgeWidget(bc widgets.BuildContext, fontSize float64, fallback ui.Color) widgets.Widget {
	if !l.HasBadge() {
		return nil
	}
	color := fallback
	if l.HasBadgeColor {
		color = l.BadgeColor
	}
	var w widgets.Widget
	switch {
	case l.Icon != "":
		code, ok := fonts.CodiconByName(l.Icon)
		if !ok {
			return nil
		}
		w = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(badgeMarginLeft, 0, badgeIconRight, 0),
			widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(badgeIconSize), Color: foundation.Some(color)}))
	case l.Bubble:
		w = widgets.NewOpacity(badgeDotOpacity, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, badgeDotRight, 0),
			widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconCircleSmallFilled), widgets.IconOpts{Size: foundation.Some(badgeDotSize), Color: foundation.Some(color)})))
	default:
		style := TextStyleOf(bc, fontSize*badgeFontScale, color).Merge(painting.NewTextStyle(painting.TextStyleOpts{FontWeight: ui.FontWeightW600}))
		w = widgets.NewOpacity(badgeOpacity, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(badgeMarginLeft, 0, badgeMarginRight, 0),
			widgets.NewText(l.Badge, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), SoftWrap: foundation.Some(false)})))
	}
	return w
}

// WithTooltip wraps a widget in the hover of the tooltip of the look; it is w itself when the look has none.
func (l FileLook) WithTooltip(w widgets.Widget) widgets.Widget {
	if l.Tooltip == "" {
		return w
	}
	return NewHover(w, HoverOpts{Text: l.Tooltip, Compact: true})
}

// FileDecorationsHost keeps a FileDecorations above its child up to date with the service: it builds the child again only for the
// widgets that asked for a look.
type FileDecorationsHost struct {
	widgets.StatefulWidget
	svc   *decorations.Service
	child widgets.Widget
}

// NewFileDecorationsHost makes the host of the service over the child. The child widget is the same value at each build of the host
// (the rebuild is of the inherited widget only), so a change of the decorations builds nothing but the widgets that asked for one.
func NewFileDecorationsHost(svc *decorations.Service, child widgets.Widget) *FileDecorationsHost {
	return &FileDecorationsHost{StatefulWidget: widgets.NewStatefulWidgetBase(), svc: svc, child: child}
}

// CreateState implements widgets.StatefulWidget.
func (h *FileDecorationsHost) CreateState() widgets.State { return &fileDecorationsHostState{} }

type fileDecorationsHostState struct {
	widgets.StateOf[*FileDecorationsHost]
	stop func()
}

// InitState follows the service.
func (s *fileDecorationsHostState) InitState() { s.follow() }

func (s *fileDecorationsHostState) follow() {
	svc := s.Widget().svc
	if svc == nil {
		return
	}
	binding := widgets.WidgetsBindingOf(s.Context())
	s.stop = svc.OnChange(func(decorations.Change) {
		binding.Post(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	})
}

// DidUpdateWidget follows another service.
func (s *fileDecorationsHostState) DidUpdateWidget(old *FileDecorationsHost) {
	if old.svc != s.Widget().svc {
		if s.stop != nil {
			s.stop()
		}
		s.follow()
	}
}

// Dispose stops following the service.
func (s *fileDecorationsHostState) Dispose() {
	if s.stop != nil {
		s.stop()
	}
}

// Build implements widgets.State.
func (s *fileDecorationsHostState) Build(widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	return NewFileDecorations(w.svc, w.child)
}

// FixedFileDecoration is a decoration that a test gives a file by its URI (NewFixedFileDecorations).
type FixedFileDecoration struct {
	// Letter, Tooltip and Color are the badge, the hover and the theme color id; Bubble shows the mark in the folders above, and Weight
	// orders the marks of a folder (the highest first).
	Letter, Tooltip, Color string
	Bubble                 bool
	Weight                 int
	// Strikethrough strikes the name through.
	Strikethrough bool
}

// NewFixedFileDecorations is a FileDecorations whose service answers from a fixed table of URIs, for the tests of views (and of the views of
// extensions, through extension/ui) that draw decorations.
func NewFixedFileDecorations(files map[string]FixedFileDecoration, child widgets.Widget) *FileDecorations {
	svc := decorations.New(decorations.Options{})
	reg, err := svc.Register("fixed", decorations.ProviderFunc(func(uri string) (decorations.Data, bool) {
		f, ok := files[uri]
		return decorations.Data{Weight: f.Weight, Color: f.Color, Letter: f.Letter, Tooltip: f.Tooltip, Strikethrough: f.Strikethrough, Bubble: f.Bubble}, ok
	}))
	if err != nil {
		// a nil provider is the only failure and this one is not nil
		panic(err)
	}
	// the files are known at once, so that the folders above them show the marks that bubble up
	uris := make([]string, 0, len(files))
	for uri := range files {
		uris = append(uris, uri)
	}
	reg.Fire(uris...)
	return NewFileDecorations(svc, child)
}
