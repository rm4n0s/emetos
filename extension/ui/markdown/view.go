package markdown

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Options tune a View. They are read when the view is made; a view that is given other options later keeps the first ones,
// except for its Controller.
type Options struct {
	// Tokenize tokenizes the text of a code block for the highlighting of its language (the id or the name of the info string
	// of the fence); false leaves the block plain. Nil highlights nothing. It is called on the UI thread, for the blocks that
	// are built, and its result is kept per block.
	Tokenize func(language, text string) ([][]extension.Token, bool)
	// OnLink is called with the destination of a link that was clicked.
	OnLink func(dest string)
	// ResolveImage gives the path of a local file for the destination of an image, or "" when the image is not local. Nil
	// shows no image; the alt text is shown instead.
	ResolveImage func(dest string) string
	// Images decodes and keeps the images of the views of a window. Nil gives the view a cache of its own with
	// DefaultImageBudget.
	Images *ImageCache
	// Controller, when set, scrolls the view to a line of the source.
	Controller *Controller
	// Compact lays the text out for a small pane next to the editor, such as the details of the suggest widget: 13 px text,
	// small margins and the colors of the suggest widget.
	Compact bool
	// Hover lays the text out for the box of a hover or of the parameter hints: the blocks one under the other at their natural
	// height (no scrolling, no page background), 13 px text with a 19 px line in the colors of the editor hover, 8 px between
	// the blocks, and code blocks without a background of their own.
	Hover bool
	// Flow lays the text out as one part of a longer page that something else scrolls, such as a message in a conversation: the
	// blocks one under the other at their natural height (no scrolling, no page background), 13 px text with the colors of the
	// editor, 8 px between the blocks, and code blocks on their own background.
	Flow bool
}

// small tells whether the look is one of the small ones (Hover, Flow, Compact), whose text is short: they are built whole, in
// a column, on the UI thread. The page of a document (the Markdown preview) is a virtual list.
func (o Options) small() bool { return o.Hover || o.Flow || o.Compact }

// NewView makes the widget that shows the Markdown text src. The page of a document is a list that builds only the blocks near
// the viewport (SPEC §10.15 "As built (step 17b.2)"): the text is parsed off the UI thread, a change of it is shown after a
// delay as in VS Code and keeps the widgets of the blocks that did not change, and the view keeps its scroll position when it is
// built again with another text. It scrolls with the Controller of the options.
func NewView(src string, opts Options) widgets.Widget {
	if opts.small() {
		return &view{StatefulWidget: widgets.NewStatefulWidgetBase(), src: src, opts: opts}
	}
	return &preview{StatefulWidget: widgets.NewStatefulWidgetBase(), src: src, opts: opts}
}

// newRenderer makes the renderer of a look of the options in the theme of bc.
func newRenderer(bc widgets.BuildContext, opts Options, tokens *tokenCache, images *ImageCache) *renderer {
	r := &renderer{colors: kit.ThemeOf(bc), opts: opts, tokens: tokens, images: images}
	size, lineHeight := bodySize, bodyLineHeight
	switch {
	case opts.Hover:
		r.colors.EditorForeground, r.colors.EditorBackground = r.colors.EditorHoverWidgetForeground, r.colors.EditorHoverWidgetBackground
		size, lineHeight = hoverSize, hoverLineHeight
	case opts.Flow:
		size, lineHeight = flowSize, flowLineHeight
	case opts.Compact:
		// a pane of the suggest widget: its colors stand where the editor's would
		r.colors.EditorForeground, r.colors.EditorBackground = r.colors.EditorSuggestWidgetForeground, r.colors.EditorSuggestWidgetBackground
		size, lineHeight = compactSize, compactLineHeight
	}
	r.base = kit.TextStyleOf(bc, size, r.colors.EditorForeground).CopyWith(painting.TextStyleOpts{Height: foundation.Some(lineHeight)})
	r.mono = widgets.WidgetsBindingOf(bc).PlatformDispatcher()
	family, _ := r.mono.MonospaceFont()
	if family == "" {
		family = "monospace" // the platform does not say: the generic family (SPEC D14)
	}
	r.monoFamily = family
	r.look = kit.TokenLookerOf(bc)
	r.binding = widgets.WidgetsBindingOf(bc).Gestures()
	ratio, ok := widgets.MediaQueryMaybeDevicePixelRatioOf(bc)
	if !ok || ratio <= 0 {
		ratio = 1
	}
	r.fit = imageFit{maxWidth: maxImageWidth, maxHeight: maxImageHeight, ratio: ratio}
	return r
}

// view is the widget of NewView for the small looks: the blocks of the text one under the other.
type view struct {
	widgets.StatefulWidget
	src  string
	opts Options
}

// CreateState implements widgets.StatefulWidget.
func (v *view) CreateState() widgets.State { return &viewState{} }

// viewState is the state of a small View: the blocks of the text, the highlighting of the code blocks and the recognizers of
// the links.
type viewState struct {
	widgets.StateOf[*view]
	src    string
	blocks []Block
	parsed bool
	tokens *tokenCache
	images *ImageCache
	// taps are the recognizers of the links of the last build.
	taps []gestures.GestureRecognizer
}

// InitState makes the caches.
func (s *viewState) InitState() {
	s.tokens = newTokenCache()
	s.images = s.Widget().opts.Images
	if s.images == nil {
		s.images = NewImageCache(0)
	}
}

// Dispose releases the recognizers.
func (s *viewState) Dispose() { s.releaseTaps() }

func (s *viewState) releaseTaps() {
	for _, t := range s.taps {
		t.Dispose()
	}
	s.taps = nil
}

// Build implements widgets.State.
func (s *viewState) Build(bc widgets.BuildContext) widgets.Widget {
	v := s.Widget()
	if !s.parsed || s.src != v.src {
		s.src, s.blocks, s.parsed = v.src, Parse(v.src), true
	}
	s.releaseTaps()
	r := newRenderer(bc, v.opts, s.tokens, s.images)
	r.addTap = func(t gestures.GestureRecognizer) { s.taps = append(s.taps, t) }
	children := make([]widgets.Widget, 0, len(s.blocks))
	for i, b := range s.blocks {
		children = append(children, r.block(b, i == 0))
	}
	if v.opts.Hover || v.opts.Flow {
		return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
	}
	column := widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	padding := painting.NewEdgeInsetsFromLTRB(compactPadding, compactPadding, compactPadding, compactPadding)
	body := widgets.NewPadding(padding, column)
	scroll := kit.NewScrollableElement(body, kit.ScrollableElementOpts{Axes: kit.ScrollAxisVertical})
	return widgets.NewColoredBox(r.colors.EditorBackground, scroll)
}
