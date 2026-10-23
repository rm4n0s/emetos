package editor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Metrics of the notice, as VS Code's binary file editor shows its message.
const (
	noticeMaxWidth   = 480.0
	noticeFontSize   = 13.0
	noticeGap        = 16.0
	noticeButtonPadV = 4.0
	noticeButtonPadH = 14.0
	noticeRadius     = 2.0
)

// NoticeOpts configure a Notice.
type NoticeOpts struct {
	// Action is the label of the button below the message ("Open Anyway"); without it there is
	// no button.
	Action string
	// OnAction is called when the button is pressed (a click, or Enter or Space while the notice has
	// the focus).
	OnAction func()
}

// Notice is what the editor area shows in place of an editor when a file is not shown, with the
// reason and, optionally, a button that does the thing the user can do about it: a binary file
// shows "The file is not displayed in the editor because it is either binary or uses an
// unsupported text encoding." and "Open Anyway".
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/binaryEditor.ts
type Notice struct {
	widgets.StatefulWidget
	message string
	opts    NoticeOpts
}

// NewNotice makes a notice that shows a message.
func NewNotice(message string, opts ...NoticeOpts) *Notice {
	n := &Notice{StatefulWidget: widgets.NewStatefulWidgetBase(), message: message}
	if len(opts) > 0 {
		n.opts = opts[0]
	}
	return n
}

// Message is the text of the notice.
func (n *Notice) Message() string { return n.message }

// CreateState implements widgets.StatefulWidget.
func (n *Notice) CreateState() widgets.State { return &noticeState{} }

type noticeState struct {
	widgets.StateOf[*Notice]
	node  *widgets.FocusNode
	hover bool
}

// InitState makes the focus node.
func (s *noticeState) InitState() {
	s.node = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Notice"})
}

// Dispose releases the focus node.
func (s *noticeState) Dispose() { s.node.Dispose() }

func (s *noticeState) act() {
	if f := s.Widget().opts.OnAction; f != nil {
		f()
	}
}

func (s *noticeState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if _, ok := e.(*services.KeyDownEvent); !ok || s.Widget().opts.Action == "" {
		return widgets.KeyEventResultIgnored
	}
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter, services.LogicalKeyboardKeySpace:
		s.act()
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

func (s *noticeState) setHover(h bool) {
	if s.hover != h && s.Mounted() {
		s.SetState(func() { s.hover = h })
	}
}

// Build implements widgets.State.
func (s *noticeState) Build(bc widgets.BuildContext) widgets.Widget {
	n := s.Widget()
	colors := kit.ThemeOf(bc)
	children := []widgets.Widget{
		widgets.NewText(n.message, widgets.TextOpts{
			Style:     kit.TextStyleOf(bc, noticeFontSize, colors.DescriptionForeground),
			TextAlign: ui.TextAlignCenter,
		}),
	}
	if n.opts.Action != "" {
		bg := colors.ButtonBackground
		if s.hover {
			bg = colors.ButtonHoverBackground
		}
		button := widgets.NewMouseRegion(
			widgets.NewGestureDetector(
				widgets.NewContainer(
					widgets.NewPadding(painting.NewEdgeInsetsSymmetric(noticeButtonPadV, noticeButtonPadH),
						widgets.NewText(n.opts.Action, widgets.TextOpts{Style: kit.TextStyleOf(bc, noticeFontSize, colors.ButtonForeground)})),
					widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
						Color:        foundation.Some(bg),
						BorderRadius: painting.NewBorderRadiusCircular(noticeRadius),
					})}),
				widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: s.act}),
			widgets.MouseRegionOpts{
				Cursor:  services.SystemMouseCursors.Click,
				OnEnter: func(*gestures.PointerEnterEvent) { s.setHover(true) },
				OnExit:  func(*gestures.PointerExitEvent) { s.setHover(false) },
			})
		children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(noticeGap)}), button)
	}
	body := widgets.NewConstrainedBox(
		rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(noticeMaxWidth)}),
		widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	return widgets.NewFocus(
		widgets.NewContainer(widgets.NewCenter(body), widgets.ContainerOpts{Color: foundation.Some(colors.EditorBackground)}),
		widgets.FocusOpts{FocusNode: s.node, Autofocus: true, OnKeyEvent: s.handleKey})
}
