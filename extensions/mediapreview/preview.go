package mediapreview

import (
	"context"
	"encoding/json"
	"os"
	"slices"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/offscreen"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
)

// previewState is what a tab of the image preview saves (VS Code's webview state): no scale while the image is fitted.
type previewState struct {
	Scale   float64 `json:"scale,omitempty"`
	OffsetX float64 `json:"offsetX,omitempty"`
	OffsetY float64 `json:"offsetY,omitempty"`
}

// preview is one tab of the image preview. Its fields belong to the UI thread.
type preview struct {
	x      *Extension
	file   extension.FileEditorFile
	tab    extension.FileEditorTab
	ctx    context.Context
	cancel context.CancelFunc
	// gen counts the reads of the file, so an older read that ends late is dropped.
	gen     int
	loading bool
	img     *Image
	err     error
	deleted bool
	// fileSize is the size of the file in bytes, -1 when it is not known.
	fileSize int64
	// fit tells that the image is fitted to the view; scale is its scale otherwise, and offX and offY how far the view is scrolled
	// into the image, in pixels of the view.
	fit        bool
	scale      float64
	offX, offY float64
	// viewW and viewH are the size of the view at its last layout; 0 before it.
	viewW, viewH float64
	// frame is the frame of an animated image shown now, and plays how many times its animation ended.
	frame, plays int
	// stamp is the stamp of the file when it was last read.
	stamp fileStamp
	// view is the state of the widget on screen, told when something changes; nil while it is not built.
	view   *viewState
	closed bool
}

// open is the Open of the image preview: a tab for the file.
func (x *Extension) open(file extension.FileEditorFile, tab extension.FileEditorTab) (extension.FileEditorContent, error) {
	p := &preview{x: x, file: file, tab: tab, fit: true, scale: 1, fileSize: -1}
	var st previewState
	if s := file.State(); len(s) > 0 && json.Unmarshal(s, &st) == nil && st.Scale > 0 {
		p.fit, p.scale, p.offX, p.offY = false, clampScale(st.Scale), st.OffsetX, st.OffsetY
	}
	p.ctx, p.cancel = context.WithCancel(x.ctx)
	x.previews = append(x.previews, p)
	file.OnChange(p.changed)
	p.load()
	return extension.FileEditorContent{Build: p.build, SaveState: p.saveState, OnClose: p.close}, nil
}

// load reads the file on a goroutine of its own.
func (p *preview) load() {
	p.gen++
	gen, path, ctx := p.gen, p.file.Path(), p.ctx
	p.stamp, _ = stampOf(path)
	opts := DecodeOptions{Video: p.x.videoOf(path)}
	p.loading = true
	p.tab.SetBusy(true)
	go func() {
		img, err := Decode(ctx, path, opts)
		size := int64(-1)
		if info, statErr := os.Stat(path); statErr == nil {
			size = info.Size()
		}
		p.x.api.Post(func() { p.loaded(gen, img, err, size) })
	}()
}

// loaded takes the result of a read: a reload keeps the zoom when the size of the image did not change, else the image is fitted.
func (p *preview) loaded(gen int, img *Image, err error, size int64) {
	if p.closed || gen != p.gen {
		return
	}
	p.loading = false
	p.tab.SetBusy(false)
	old := p.img
	p.img, p.err, p.fileSize, p.deleted = img, err, size, false
	if old != nil && img != nil && (old.Width != img.Width || old.Height != img.Height) {
		p.fit, p.offX, p.offY = true, 0, 0
	}
	p.frame, p.plays = 0, 0
	p.update()
}

// changed is the file changed or deleted on disk.
func (p *preview) changed(c extension.FileEditorChange) {
	if c.Deleted {
		p.gen++ // a read under way is dropped
		p.loading, p.deleted = false, true
		p.tab.SetBusy(false)
		p.update()
		return
	}
	if s, ok := stampOf(p.file.Path()); ok && s == p.stamp && !p.deleted {
		return // the watcher's first event, or a write of the same bytes at the same time
	}
	p.load()
}

// update shows what changed: the status bar items and the widget.
func (p *preview) update() {
	p.updateStatus()
	if p.view != nil {
		p.view.changed()
	}
}

// updateStatus sets VS Code's three entries: the zoom (a click picks a level), the size of the image and the size of the file.
func (p *preview) updateStatus() {
	var items []extension.StatusItem
	if p.img != nil && !p.deleted && p.err == nil {
		items = append(items,
			extension.StatusItem{ID: "zoom", Name: "Image Zoom", Text: ZoomLabel(p.fit, p.scale), Alignment: extension.StatusRight, Priority: 102,
				OnClick: p.pickZoom},
			extension.StatusItem{ID: "size", Name: "Image Size", Text: p.img.Size(), Alignment: extension.StatusRight, Priority: 101})
	}
	if p.fileSize >= 0 && !p.deleted {
		items = append(items, extension.StatusItem{ID: "binarySize", Name: "Image Binary Size", Text: formatSize(p.fileSize),
			Alignment: extension.StatusRight, Priority: 100})
	}
	p.tab.SetStatus(items)
}

// pickZoom is the pick of the zoom entry (VS Code's _imagePreview.selectZoomLevel).
func (p *preview) pickZoom() {
	items := make([]extension.QuickPickItem, len(ZoomPickScales))
	for i, s := range ZoomPickScales {
		items[i] = extension.QuickPickItem{Label: ZoomLabel(s == 0, s)}
	}
	err := p.x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select zoom level", Items: items}, func(i int) {
		if i < 0 || p.closed {
			return
		}
		if s := ZoomPickScales[i]; s == 0 {
			p.setFit()
		} else {
			p.setScaleAt(s, p.viewW/2, p.viewH/2)
		}
	})
	if err != nil {
		p.x.api.Log().Warn("the zoom pick cannot be shown", "err", err)
	}
}

// shownScale is the scale the image is drawn at: the fitted scale while it is fitted.
func (p *preview) shownScale() float64 {
	if !p.fit || p.img == nil {
		return p.scale
	}
	if p.viewW <= 0 || p.viewH <= 0 {
		return 1
	}
	return FitScale(float64(p.img.Width), float64(p.img.Height), p.viewW, p.viewH)
}

// setFit fits the image to the view.
func (p *preview) setFit() {
	p.fit, p.offX, p.offY = true, 0, 0
	p.update()
}

// setScaleAt zooms to a scale, keeping the point (px, py) of the view over the same point of the image.
func (p *preview) setScaleAt(s, px, py float64) {
	if p.img == nil {
		return
	}
	from, to := p.shownScale(), clampScale(s)
	p.offX, p.offY = Anchor(float64(p.img.Width), float64(p.img.Height), from, to, p.viewW, p.viewH, p.offX, p.offY, px, py)
	p.fit, p.scale = false, to
	p.update()
}

// zoomIn and zoomOut are the commands, at the middle of the view; from a fitted image they start at its fitted scale (VS Code's
// firstZoom).
func (p *preview) zoomIn()  { p.setScaleAt(ZoomIn(p.shownScale()), p.viewW/2, p.viewH/2) }
func (p *preview) zoomOut() { p.setScaleAt(ZoomOut(p.shownScale()), p.viewW/2, p.viewH/2) }

// scrollBy scrolls a zoomed image by (dx, dy) pixels of the view.
func (p *preview) scrollBy(dx, dy float64) {
	if p.img == nil || p.fit {
		return
	}
	s := p.shownScale()
	_, _, p.offX, p.offY = Place(float64(p.img.Width), float64(p.img.Height), s, p.viewW, p.viewH, p.offX+dx, p.offY+dy)
	p.update()
}

// pngOf is the image in the PNG format: its first frame, or an SVG drawn at its own size.
func (p *preview) pngOf() ([]byte, error) {
	img := p.img
	if img == nil {
		return nil, errors.New("ImageNotLoaded", "the image is not loaded")
	}
	if img.SVG != nil {
		w, h := max(img.Width, 1), max(img.Height, 1)
		raster, err := img.SVG.Picture().ToImage(w, h, ui.ToImageOpts{Rasterizer: offscreen.NewRasterizer()})
		if err != nil {
			return nil, errors.NewErr("ImageCopyFailed", err).SetMetadata("path", p.file.Path())
		}
		return encodePNG(p.file.Path(), raster)
	}
	return encodePNG(p.file.Path(), img.Frames[0].Image)
}

// encodePNG writes a picture as a PNG file.
func encodePNG(path string, img *ui.Image) ([]byte, error) {
	data, err := img.ToByteData(ui.ImageByteFormatPNG)
	if err != nil {
		return nil, errors.NewErr("ImageCopyFailed", err).SetMetadata("path", path)
	}
	return data, nil
}

// copyImage is Copy: the image as PNG on the clipboard.
func (p *preview) copyImage() {
	data, err := p.pngOf()
	if err == nil {
		err = p.x.api.Clipboard().WriteImage(data)
	}
	if err != nil {
		p.x.api.Log().Warn("the image cannot be copied", "path", p.file.Path(), "err", err)
		p.x.api.Notifications().Warn("Cannot copy the image: " + describe(err))
	}
}

// describe is the message of an error for the user.
func describe(err error) string {
	if e, ok := errors.FromError(err); ok && e.Message != "" {
		return e.Message
	}
	return err.Error()
}

// saveState is the state of the tab: the zoom and the scroll.
func (p *preview) saveState() (any, error) {
	if p.fit {
		return previewState{}, nil
	}
	return previewState{Scale: p.scale, OffsetX: p.offX, OffsetY: p.offY}, nil
}

// close ends the tab: the read under way is cancelled.
func (p *preview) close() {
	p.closed = true
	p.cancel()
	p.x.previews = slices.DeleteFunc(p.x.previews, func(q *preview) bool { return q == p })
	if p.view != nil {
		p.view.stopAnimation()
	}
}
