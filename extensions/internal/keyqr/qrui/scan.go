package qrui

import (
	"context"
	"image"
	"image/draw"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
	xdraw "golang.org/x/image/draw"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// The look of the preview: the picture is made at most previewWidth pixels wide (a frame of 1280x720 would be 3.6 MB of pixels
// ten times a second for a box of 448 by 252), and the box that shows it is previewBox logical pixels.
const (
	previewWidth             = 480
	previewBoxW, previewBoxH = 448.0, 252.0
	// notAKeyFor is how long "not a public key" stays after a code that is not one was last seen.
	notAKeyFor = 1500 * time.Millisecond
)

// ScanOptions are what NewScanDialog needs.
type ScanOptions struct {
	// Context ends the dialog when it ends (the context of the extension): the camera is released.
	Context context.Context
	// Source finds and opens the cameras. Nil means this computer cannot read a camera, and the dialog says so.
	Source keyqr.Source
	// Post runs a function on the UI thread (the Post of the extension API); the goroutine that reads the camera uses it.
	Post func(func())
	// Refresh builds the owner of the dialog again, so that Build is called: the owner's EditorTab.Refresh or WindowTab.Refresh.
	Refresh func()
	// Add is called, on the UI thread, with the key the user confirmed, after the dialog closed. The owner adds it to its list and
	// says what happened (a key that is in the list already, a list that is full).
	Add func(key sshlogin.AllowedKey)
	// Close is called, on the UI thread, once, when the dialog closes in any way: the owner drops it and builds itself again.
	Close func()
	// Interval is the time between two frames that are looked at; keyqr.DefaultScanInterval when zero.
	Interval time.Duration
}

// scanState is what the dialog shows.
type scanState int

const (
	// stateListing is the look for cameras.
	stateListing scanState = iota
	// stateNoCamera is the reason there is nothing to scan with.
	stateNoCamera
	// stateStarting is a camera being opened, and stateScanning one that is open and showing its picture.
	stateStarting
	stateScanning
	// stateConfirm is the key that was read, to be added or not.
	stateConfirm
	// stateFailed is a camera that could not be opened or stopped working.
	stateFailed
)

// ScanDialog is the Scan QR Code dialog. Make it with NewScanDialog on the UI thread; all its methods are for the UI thread.
type ScanDialog struct {
	opts   ScanOptions
	ctx    context.Context
	cancel context.CancelFunc

	// the UI thread only
	state      scanState
	cameras    []keyqr.CameraInfo
	selected   int
	reason     string
	picture    *ui.Picture
	showNotKey bool
	key        sshlogin.AllowedKey
	gen        int
	scanCancel context.CancelFunc
	closed     bool

	// the goroutine that reads the camera hands its results over through these: the newest picture, whether the owner was told
	// of it, and when a code that is not a key was last seen
	latest  atomic.Pointer[ui.Picture]
	pending atomic.Bool
	lastBad atomic.Int64
}

// NewScanDialog opens the dialog: it looks for the cameras off the UI thread and, when there are some, opens the first and shows
// what it sees. The camera is released when the dialog closes.
func NewScanDialog(opts ScanOptions) *ScanDialog {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	d := &ScanDialog{opts: opts, state: stateListing}
	d.ctx, d.cancel = context.WithCancel(ctx)
	if opts.Source == nil {
		d.state, d.reason = stateNoCamera, NoSourceReason
		return d
	}
	go func() {
		cams, err := opts.Source.Cameras(d.ctx)
		if d.ctx.Err() != nil {
			return
		}
		d.post(func() { d.listed(cams, err) })
	}()
	return d
}

// post runs f on the UI thread unless the dialog is closed by then.
func (d *ScanDialog) post(f func()) {
	d.opts.Post(func() {
		if !d.closed {
			f()
		}
	})
}

func (d *ScanDialog) refresh() { d.opts.Refresh() }

// NoSourceReason is what is said when the computer has no way to read a camera at all.
const NoSourceReason = "This computer has no camera that Emetos can read."

// NoCameraReason says why a look for cameras (the list and the error Source.Cameras gave) found nothing to scan with: the words
// for the user, or an empty text when there is a camera. The owners show it on the disabled Scan QR Code button.
func NoCameraReason(cams []keyqr.CameraInfo, err error) string {
	switch {
	case err != nil:
		return message(err)
	case len(cams) == 0:
		return "No camera was found on this computer."
	}
	return ""
}

// listed is the answer to the look for cameras.
func (d *ScanDialog) listed(cams []keyqr.CameraInfo, err error) {
	if reason := NoCameraReason(cams, err); reason != "" {
		d.state, d.reason = stateNoCamera, reason
		d.refresh()
		return
	}
	d.cameras = cams
	d.start(0)
}

// message is what an error says to the user.
func message(err error) string {
	if appErr, ok := errors.FromError(err); ok && appErr.Message != "" {
		return appErr.Message
	}
	return err.Error()
}

// start opens the camera i and scans with it, in place of the one that scans.
func (d *ScanDialog) start(i int) {
	d.stopScan()
	d.gen++
	gen, cam := d.gen, d.cameras[i]
	d.selected, d.state, d.picture, d.showNotKey, d.reason = i, stateStarting, nil, false, ""
	d.latest.Store(nil)
	d.pending.Store(false)
	d.lastBad.Store(0)
	ctx, cancel := context.WithCancel(d.ctx)
	d.scanCancel = cancel
	go d.run(ctx, cancel, gen, cam)
	d.refresh()
}

// stopScan tells the goroutine of the camera to let it go; it does so on its own as soon as it is told.
func (d *ScanDialog) stopScan() {
	if d.scanCancel != nil {
		d.scanCancel()
		d.scanCancel = nil
	}
}

// run is the goroutine of one camera: it opens it, scans it, and gives it back when the context ends.
func (d *ScanDialog) run(ctx context.Context, cancel context.CancelFunc, gen int, cam keyqr.CameraInfo) {
	stream, err := d.opts.Source.Open(ctx, cam.ID)
	if err != nil {
		if ctx.Err() == nil {
			d.post(func() { d.failed(gen, err) })
		}
		return
	}
	defer func() { _ = stream.Close() }() // the camera is let go whatever way the scan ended
	d.post(func() { d.opened(gen) })
	err = keyqr.Scan(ctx, stream, keyqr.ScanOptions{
		Interval: d.opts.Interval,
		OnFrame:  func(frame image.Image) { d.frame(gen, frame) },
		OnCode:   func(text string) { d.code(cancel, gen, text) },
	})
	if err != nil && ctx.Err() == nil {
		d.post(func() { d.failed(gen, err) })
	}
}

// opened is the camera being open.
func (d *ScanDialog) opened(gen int) {
	if gen == d.gen && d.state == stateStarting {
		d.state = stateScanning
		d.refresh()
	}
}

// failed is a camera that could not be opened or stopped working.
func (d *ScanDialog) failed(gen int, err error) {
	if gen != d.gen {
		return
	}
	d.stopScan()
	d.state, d.reason, d.picture = stateFailed, message(err), nil
	d.latest.Store(nil)
	d.refresh()
}

// frame is a picture of the camera, on the goroutine that reads it: it makes the picture for the preview and hands it over. The
// owner is told once for the pictures that come while it has not drawn the last, so a slow UI thread drops pictures and does
// not fall behind.
func (d *ScanDialog) frame(gen int, frame image.Image) {
	pic, err := previewPicture(frame)
	if err != nil {
		return // a frame that cannot be shown is a frame lost
	}
	d.latest.Store(pic)
	if d.pending.Swap(true) {
		return
	}
	d.post(func() {
		d.pending.Store(false)
		if gen != d.gen || (d.state != stateScanning && d.state != stateStarting) {
			return
		}
		d.picture = d.latest.Load()
		d.showNotKey = d.lastBad.Load() != 0 && time.Since(time.Unix(0, d.lastBad.Load())) < notAKeyFor
		d.refresh()
	})
}

// previewPicture is the picture of a frame as it is shown: at most previewWidth wide.
func previewPicture(frame image.Image) (*ui.Picture, error) {
	b := frame.Bounds()
	if b.Dx() <= previewWidth {
		return ui.NewPicture(frame)
	}
	small := image.NewRGBA(image.Rect(0, 0, previewWidth, max(1, b.Dy()*previewWidth/b.Dx())))
	if _, isYCbCr := frame.(*image.YCbCr); isYCbCr {
		xdraw.ApproxBiLinear.Scale(small, small.Bounds(), frame, b, draw.Src, nil)
	} else {
		xdraw.BiLinear.Scale(small, small.Bounds(), frame, b, draw.Src, nil)
	}
	return ui.NewPicture(small)
}

// code is a QR code that was read, on the goroutine of the camera. A public key ends the scan at once, so that the camera is let
// go and no more frames are looked at; any other code is remembered to be said so.
func (d *ScanDialog) code(cancel context.CancelFunc, gen int, text string) {
	key, err := keyqr.ParseKey(text)
	if err != nil {
		d.lastBad.Store(time.Now().UnixNano())
		return
	}
	cancel()
	d.post(func() {
		if gen != d.gen || d.state == stateConfirm {
			return
		}
		d.key, d.state, d.picture, d.showNotKey = key, stateConfirm, nil, false
		d.latest.Store(nil)
		d.refresh()
	})
}

// Close closes the dialog: the camera is released, the picture dropped, and the owner told. It does nothing the second time.
func (d *ScanDialog) Close() {
	if d.closed {
		return
	}
	d.closed = true
	d.stopScan()
	d.cancel()
	d.picture = nil
	d.latest.Store(nil)
	if d.opts.Close != nil {
		d.opts.Close()
	}
}

// add closes the dialog and gives the key that was confirmed to the owner.
func (d *ScanDialog) add() {
	key := d.key
	d.Close()
	if d.opts.Add != nil {
		d.opts.Add(key)
	}
}

// again goes back to the camera after a key was read, or a camera that failed.
func (d *ScanDialog) again() { d.start(d.selected) }

// State is what the dialog is showing, for the tests of the owners: "listing", "no camera", "starting", "scanning", "confirm" or
// "failed".
func (d *ScanDialog) State() string {
	return [...]string{"listing", "no camera", "starting", "scanning", "confirm", "failed"}[d.state]
}

// Key is the key that was read, valid while State is "confirm".
func (d *ScanDialog) Key() sshlogin.AllowedKey { return d.key }

// Build is the dialog, to be stacked over the content of its owner.
func (d *ScanDialog) Build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	dim := colors.DescriptionForeground
	cancel := ui.NewButton("Cancel", ui.ButtonOpts{Secondary: true, OnPressed: d.Close})
	head := title(bc, colors, "device-camera-video", colors.NotificationsInfoIconForeground, "Scan QR Code")
	var body []widgets.Widget
	switch d.state {
	case stateListing:
		body = []widgets.Widget{paragraph(bc, "Looking for cameras...", dim), vgap(14), buttons(cancel)}
	case stateNoCamera:
		body = []widgets.Widget{
			widgets.NewRow([]widgets.Widget{ui.Icon("warning", 16, colors.NotificationsWarningIconForeground), hgap(8), widgets.NewExpanded(paragraph(bc, d.reason, colors.Foreground))},
				widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}),
			vgap(14), buttons(ui.NewButton("Close", ui.ButtonOpts{OnPressed: d.Close})),
		}
	case stateFailed:
		body = []widgets.Widget{
			widgets.NewRow([]widgets.Widget{ui.Icon("error", 16, colors.ErrorForeground), hgap(8), widgets.NewExpanded(paragraph(bc, d.reason, colors.Foreground))},
				widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}),
			vgap(14), buttons(cancel, ui.NewButton("Try Again", ui.ButtonOpts{OnPressed: d.again})),
		}
	case stateConfirm:
		body = d.confirmBody(bc, colors, cancel)
	default:
		body = d.scanBody(bc, colors, cancel)
	}
	return Card(bc, column(append([]widgets.Widget{head, vgap(12)}, body...)...))
}

// scanBody is the preview with the picker and the hint.
func (d *ScanDialog) scanBody(bc widgets.BuildContext, colors ui.Colors, cancel widgets.Widget) []widgets.Widget {
	var preview widgets.Widget = widgets.NewCenter(paragraph(bc, "Starting the camera...", gui.NewColor(0xFFB0B0B0)))
	if d.picture != nil {
		preview = ui.NewImage(d.picture, ui.ImageOpts{Upscale: true})
	}
	box := widgets.NewColoredBox(gui.NewColor(0xFF000000), widgets.NewSizedBox(preview, widgets.SizedBoxOpts{Width: foundation.Some(previewBoxW), Height: foundation.Some(previewBoxH)}))
	out := []widgets.Widget{widgets.NewAlign(box, widgets.AlignOpts{Alignment: painting.AlignmentCenter})}
	if len(d.cameras) > 1 {
		options := make([]ui.DropdownOption, len(d.cameras))
		for i, c := range d.cameras {
			options[i] = ui.DropdownOption{Label: c.Name + "  " + c.ID}
		}
		out = append(out, vgap(10), widgets.NewRow([]widgets.Widget{
			paragraph(bc, "Camera", colors.DescriptionForeground), hgap(10),
			ui.NewDropdown(options, ui.DropdownOpts{Selected: d.selected, Width: foundation.Some(340.0), OnChanged: func(i int) {
				if i != d.selected {
					d.start(i)
				}
			}}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	}
	status, color := "Hold the QR code of the public key in front of the camera.", colors.DescriptionForeground
	if d.showNotKey {
		status, color = "That QR code is not a public key.", colors.NotificationsWarningIconForeground
	}
	return append(out, vgap(10), paragraph(bc, status, color), vgap(14), buttons(cancel))
}

// confirmBody is the key that was read: what the user compares before it is added.
func (d *ScanDialog) confirmBody(bc widgets.BuildContext, colors ui.Colors, cancel widgets.Widget) []widgets.Widget {
	return []widgets.Widget{
		widgets.NewText("Add this key?", widgets.TextOpts{Style: bold(ui.TextStyle(bc, 14, colors.Foreground))}),
		vgap(8),
		keyDetails(bc, colors, d.key.Type(), d.key.Fingerprint(), d.key.Comment),
		vgap(10),
		paragraph(bc, "Compare the fingerprint with the one the other computer shows. Add the key only if they are the same: whoever holds it can log in.", colors.DescriptionForeground),
		vgap(14),
		buttons(cancel, ui.NewButton("Scan Again", ui.ButtonOpts{Secondary: true, OnPressed: d.again}), ui.NewButton("Add Key", ui.ButtonOpts{OnPressed: d.add})),
	}
}
