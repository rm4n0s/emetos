package mediapreview

import (
	"math"
	"strconv"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The widget of the video player (SPEC §10.37 "Video"): the frame centered on black, letterboxed, and under it the bar with
// Play/Pause, the time, the seek slider, Mute, the volume slider and Loop.

// barHeight is the height of the bar, and volumeWidth the width of the volume slider.
const (
	barHeight   = 32
	volumeWidth = 80
)

// build is the Build of the tab.
func (v *videoTab) build(widgets.BuildContext) widgets.Widget {
	return &videoView{StatefulWidget: widgets.NewStatefulWidgetBase(), v: v}
}

// videoView is the widget of a video tab; its state is the tab's.
type videoView struct {
	widgets.StatefulWidget
	v *videoTab
}

// CreateState implements widgets.StatefulWidget.
func (w *videoView) CreateState() widgets.State { return &videoViewState{} }

// videoViewState is the state of the widget: the drag of the seek slider and the focus of the keys.
type videoViewState struct {
	widgets.StateOf[*videoView]
	// seeking tells that the seek slider is dragged, to seekTo (a fraction of the duration).
	seeking bool
	seekTo  float64
	focus   *widgets.FocusNode
	// asked tells that the making of the player was posted.
	asked bool
}

// InitState implements widgets.State.
func (s *videoViewState) InitState() {
	s.Widget().v.view = s
	s.focus = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "video"})
}

// DidUpdateWidget implements widgets.State.
func (s *videoViewState) DidUpdateWidget(old *videoView) {
	if old.v != s.Widget().v {
		if old.v.view == s {
			old.v.view = nil
		}
		s.Widget().v.view = s
	}
}

// Dispose implements widgets.State: a tab that is not on screen pauses. The view is let go first: the pause must not build it again.
func (s *videoViewState) Dispose() {
	v := s.Widget().v
	if v.view == s {
		v.view = nil
	}
	v.hidden()
	s.focus.Dispose()
}

// changed builds the widget again.
func (s *videoViewState) changed() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// Build implements widgets.State.
func (s *videoViewState) Build(bc widgets.BuildContext) widgets.Widget {
	v := s.Widget().v
	colors := ui.Theme(bc)
	switch {
	case v.deleted:
		return widgets.NewColoredBox(colors.EditorBackground, message(bc, "The file was deleted.", "", nil))
	case v.problem != nil:
		return widgets.NewColoredBox(colors.EditorBackground, v.x.problemView(bc, v.problem, "Playing videos", v.load))
	case v.player != nil && v.player.err != nil:
		return widgets.NewColoredBox(colors.EditorBackground, v.x.problemView(bc, v.player.err, "Playing videos", v.load))
	}
	// a Remote Work client's view plays muted: its sound would come out of the host's speakers
	if v.player != nil {
		v.player.SetForceMute(ui.OriginOf(bc) != extension.HostOrigin)
	}
	picture := widgets.NewLayoutBuilder(func(lbc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		if v.player == nil && !s.asked && !v.loading {
			s.asked = true
			dpr := widgets.MediaQueryDevicePixelRatioOf(lbc)
			binding := widgets.WidgetsBindingOf(lbc)
			w, h := c.MaxWidth*dpr, c.MaxHeight*dpr
			binding.Post(func() { // not during a layout
				s.asked = false
				if s.Mounted() {
					v.ensurePlayer(binding, w, h)
				}
			})
		}
		var child widgets.Widget = widgets.NewSizedBox(nil)
		if v.player != nil && v.player.frame != nil {
			child = widgets.NewRawImage(v.player.frame, widgets.RawImageOpts{Scale: 1, Fit: painting.BoxFitContain, FilterQuality: gui.FilterQualityLow})
		}
		return widgets.NewColoredBox(gui.NewColorFromARGB(0xff, 0, 0, 0), widgets.NewSizedBox(child,
			widgets.SizedBoxOpts{Width: foundation.Some(c.MaxWidth), Height: foundation.Some(c.MaxHeight)}))
	})
	body := widgets.NewColumn([]widgets.Widget{
		widgets.NewExpanded(widgets.NewGestureDetector(picture, widgets.GestureDetectorOpts{OnTap: func() { s.focus.RequestFocus() }})),
		s.bar(bc),
	})
	return widgets.NewFocus(body, widgets.FocusOpts{FocusNode: s.focus, Autofocus: true, OnKeyEvent: s.key})
}

// bar is the controls under the picture.
func (s *videoViewState) bar(bc widgets.BuildContext) widgets.Widget {
	v := s.Widget().v
	colors := ui.Theme(bc)
	p := v.player
	var pos, dur time.Duration
	playing, muted, loop, volume := false, false, false, 1.0
	if p != nil {
		pos, dur, playing, muted, loop, volume = p.Position(), v.info.Duration, p.playing, p.muted || p.forceMute, p.loop, p.volume
	}
	frac := 0.0
	if dur > 0 {
		frac = min(1, float64(pos)/float64(dur))
	}
	if s.seeking {
		frac = s.seekTo
		pos = time.Duration(frac * float64(dur))
	}
	playIcon, playTip := "play", "Play (Space)"
	if playing {
		playIcon, playTip = "debug-pause", "Pause (Space)"
	}
	muteIcon, muteTip := "unmute", "Mute (M)"
	if muted {
		muteIcon, muteTip = "mute", "Unmute (M)"
	}
	disabled := p == nil
	gap := widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)})
	row := widgets.NewRow([]widgets.Widget{
		ui.NewIconButton(playIcon, ui.IconButtonOpts{Tooltip: playTip, Disabled: disabled, OnPressed: v.togglePlay}),
		gap,
		widgets.NewText(formatTime(pos)+" / "+formatTime(dur), widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.Foreground)}),
		gap,
		widgets.NewExpanded(newSlider(frac, colors.Foreground, func(f float64) {
			s.SetState(func() { s.seeking, s.seekTo = true, f })
		}, func(f float64) {
			s.SetState(func() { s.seeking = false })
			if v.player != nil {
				v.player.Seek(time.Duration(f * float64(v.info.Duration)))
			}
		})),
		gap,
		ui.NewIconButton(muteIcon, ui.IconButtonOpts{Tooltip: muteTip, Disabled: disabled || (p != nil && p.forceMute), OnPressed: func() {
			if v.player != nil {
				v.player.SetMuted(!v.player.muted)
			}
		}}),
		widgets.NewSizedBox(newSlider(volume, colors.Foreground, func(f float64) {
			if v.player != nil {
				v.player.SetVolume(f)
			}
		}, nil), widgets.SizedBoxOpts{Width: foundation.Some(float64(volumeWidth))}),
		gap,
		ui.NewIconButton("sync", ui.IconButtonOpts{Tooltip: "Loop", Checked: loop, Disabled: disabled, OnPressed: func() {
			if v.player != nil {
				v.player.SetLoop(!v.player.loop)
			}
		}}),
	})
	return widgets.NewContainer(row, widgets.ContainerOpts{
		Height: foundation.Some(float64(barHeight)), Color: foundation.Some(colors.EditorBackground),
		Padding: painting.NewEdgeInsetsSymmetric(0, 8),
	})
}

// key is the keys of the player: Space plays and pauses, Left and Right seek 5 s, Up and Down change the volume, M mutes, Home and
// End go to the start and the end.
func (s *videoViewState) key(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	v := s.Widget().v
	// Space before the player is made (FFmpeg still found, the file still probed) plays when it is, as the button does
	if e.LogicalKey() == services.LogicalKeyboardKeySpace {
		v.togglePlay()
		return widgets.KeyEventResultHandled
	}
	p := v.player
	if p == nil {
		return widgets.KeyEventResultIgnored
	}
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyArrowLeft:
		p.Seek(p.Position() - seekStep)
	case services.LogicalKeyboardKeyArrowRight:
		p.Seek(p.Position() + seekStep)
	case services.LogicalKeyboardKeyArrowUp:
		p.SetVolume(p.volume + volumeStep)
	case services.LogicalKeyboardKeyArrowDown:
		p.SetVolume(p.volume - volumeStep)
	case services.LogicalKeyboardKeyKeyM:
		p.SetMuted(!p.muted)
	case services.LogicalKeyboardKeyHome:
		p.Seek(0)
	case services.LogicalKeyboardKeyEnd:
		p.Seek(v.info.Duration)
	default:
		return widgets.KeyEventResultIgnored
	}
	return widgets.KeyEventResultHandled
}

// formatTime writes a time as the player shows it: 0:42, 3:07, 1:02:03.
func formatTime(d time.Duration) string {
	total := int(math.Max(0, d.Seconds()))
	h, m, sec := total/3600, total/60%60, total%60
	two := func(n int) string {
		if n < 10 {
			return "0" + strconv.Itoa(n)
		}
		return strconv.Itoa(n)
	}
	if h > 0 {
		return strconv.Itoa(h) + ":" + two(m) + ":" + two(sec)
	}
	return strconv.Itoa(m) + ":" + two(sec)
}

// messageWithButtons is a message in the middle of the view with a row of buttons under it.
func messageWithButtons(bc widgets.BuildContext, text string, buttons []widgets.Widget) widgets.Widget {
	colors := ui.Theme(bc)
	children := []widgets.Widget{widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.Foreground), TextAlign: gui.TextAlignCenter})}
	if len(buttons) > 0 { // in rows that wrap in a narrow view
		children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(12.0)}),
			widgets.NewWrap(buttons, widgets.WrapOpts{Alignment: rendering.WrapAlignmentCenter, Spacing: 8, RunSpacing: 8}))
	}
	return widgets.NewCenter(widgets.NewPadding(painting.NewEdgeInsetsAll(16),
		widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})))
}

// slider is a horizontal slider of a value from 0 to 1: a track, the part before the thumb filled, and the thumb. onChanged is called
// while it is pressed or dragged, and onEnd when it is let go (nil for none).
type slider struct {
	widgets.StatelessWidget
	value     float64
	color     gui.Color
	onChanged func(float64)
	onEnd     func(float64)
}

func newSlider(value float64, color gui.Color, onChanged, onEnd func(float64)) *slider {
	s := &slider{value: value, color: color, onChanged: onChanged, onEnd: onEnd}
	s.StatelessWidget = widgets.NewStatelessWidgetBase(widgets.WidgetOpts{})
	return s
}

// Build implements widgets.StatelessWidget.
func (s *slider) Build(widgets.BuildContext) widgets.Widget {
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		width := c.MaxWidth
		at := func(x float64) float64 {
			if width <= 0 {
				return 0
			}
			return min(max(x/width, 0), 1)
		}
		last := s.value
		changed := func(x float64) {
			last = at(x)
			if s.onChanged != nil {
				s.onChanged(last)
			}
		}
		ended := func() {
			if s.onEnd != nil {
				s.onEnd(last)
			}
		}
		paint := widgets.NewCustomPaint(nil, widgets.CustomPaintOpts{
			Painter: &sliderPainter{CustomPainterBase: rendering.NewCustomPainterBase(nil), value: s.value, color: s.color},
			Size:    gui.Size{Width: width, Height: barHeight},
		})
		return widgets.NewGestureDetector(paint, widgets.GestureDetectorOpts{
			Behavior:               rendering.HitTestBehaviorOpaque,
			OnTapDown:              func(d gestures.TapDownDetails) { changed(d.LocalPosition.Dx) },
			OnTapUp:                func(gestures.TapUpDetails) { ended() },
			OnHorizontalDragStart:  func(d gestures.DragStartDetails) { changed(d.LocalPosition.Dx) },
			OnHorizontalDragUpdate: func(d gestures.DragUpdateDetails) { changed(d.LocalPosition.Dx) },
			OnHorizontalDragEnd:    func(gestures.DragEndDetails) { ended() },
		})
	})
}

// sliderPainter draws a slider.
type sliderPainter struct {
	rendering.CustomPainterBase
	value float64
	color gui.Color
}

// Paint implements rendering.CustomPainter.
func (p *sliderPainter) Paint(canvas *gui.Canvas, size gui.Size) {
	y := size.Height / 2
	track := gui.NewPaint()
	track.SetColor(p.color.WithOpacity(0.25))
	canvas.DrawRect(gui.NewRectFromLTWH(0, y-2, size.Width, 4), track)
	fill := gui.NewPaint()
	fill.SetColor(p.color)
	x := size.Width * p.value
	canvas.DrawRect(gui.NewRectFromLTWH(0, y-2, x, 4), fill)
	canvas.DrawCircle(gui.Offset{Dx: min(max(x, 6), size.Width-6), Dy: y}, 6, fill)
}

// ShouldRepaint implements rendering.CustomPainter.
func (p *sliderPainter) ShouldRepaint(old rendering.CustomPainter) bool {
	o, ok := old.(*sliderPainter)
	return !ok || o.value != p.value || o.color != p.color
}
