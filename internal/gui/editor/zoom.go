package editor

import (
	"github.com/rm4n0s/glutter/foundation"
)

const (
	// MinZoomLevel is the lowest zoom level of the editor font.
	MinZoomLevel = -5.0
	// MaxZoomLevel is the highest zoom level of the editor font.
	MaxZoomLevel = 20.0
	// zoomStep is how much one level changes the font size and the line height.
	zoomStep = 0.1
)

// Zoom is the zoom level of the font of the editors of a window: every level makes the font and
// the lines a tenth larger (or smaller). It is shared, so that the commands and the mouse wheel
// zoom all the editors of the window together, as VS Code's EditorZoom does.
//
// Ported from VS Code: src/vs/editor/common/config/editorZoom.ts
type Zoom struct {
	notifier *foundation.ChangeNotifier
	level    float64
}

// NewZoom makes a zoom at level 0.
func NewZoom() *Zoom { return &Zoom{notifier: foundation.NewChangeNotifier()} }

// Level is the zoom level, from MinZoomLevel to MaxZoomLevel.
func (z *Zoom) Level() float64 { return z.level }

// Set changes the zoom level, within its bounds, and tells the listeners when it changed.
func (z *Zoom) Set(level float64) {
	level = min(max(level, MinZoomLevel), MaxZoomLevel)
	if level == z.level {
		return
	}
	z.level = level
	z.notifier.NotifyListeners()
}

// AddListener calls fn when the level changes.
func (z *Zoom) AddListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return z.notifier.AddListener(fn)
}

// RemoveListener removes a listener.
func (z *Zoom) RemoveListener(h foundation.ListenerHandle) { z.notifier.RemoveListener(h) }

// zoomed is the options at a zoom level: the font and the lines are 1 + level/10 times their
// size.
//
// Ported from VS Code: src/vs/editor/common/config/fontInfo.ts (BareFontInfo._create)
func zoomed(opts Options, level float64) Options {
	if level == 0 {
		return opts
	}
	k := 1 + level*zoomStep
	opts.FontSize *= k
	opts.LineHeight *= k
	return opts
}

// Zoom is the zoom of the editor, the one it shares with the other editors of its window when
// the widget was given one.
func (vm *ViewModel) Zoom() *Zoom { return vm.zoom }

// SetZoom makes the editor follow a zoom, usually the one of its window.
func (vm *ViewModel) SetZoom(z *Zoom) {
	if z == nil || z == vm.zoom {
		return
	}
	vm.zoom.RemoveListener(vm.zoomHandle)
	vm.zoom = z
	vm.zoomHandle = z.AddListener(vm.zoomChanged)
	vm.zoomChanged()
}

// zoomChanged applies the zoom level to the options.
func (vm *ViewModel) zoomChanged() { vm.applyOptions() }
