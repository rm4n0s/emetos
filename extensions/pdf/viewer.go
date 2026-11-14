package pdf

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

// The tab of the viewer (SPEC §10.37 "PDF"): Poppler is found and pdfinfo run off the UI thread when the tab opens; the pages are laid
// out when the size of the view is known, and the pages in view (and one before and after) are drawn by pdftoppm, at most two at once,
// into a cache of drawn pages.

// The drawing of pages: at most maxRenders at once, and zoomSettle after the last zoom step before the pages are drawn again at the
// new scale (the old pictures are scaled meanwhile).
const (
	maxRenders = 2
	zoomSettle = 200 * time.Millisecond
)

// The steps of the keys: an arrow scrolls lineStep, Page Up and Page Down the view less pageOverlap.
const (
	lineStep    = 40.0
	pageOverlap = 40.0
)

// fileStamp is what tells that a file changed: its size and the time it was written.
type fileStamp struct {
	size int64
	mod  time.Time
}

// stampOf is the stamp of the file at path; false when it cannot be read.
func stampOf(path string) (fileStamp, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, false
	}
	return fileStamp{size: info.Size(), mod: info.ModTime()}, true
}

// viewerState is what a tab saves: the page (from 1), how far down it the view starts (a fraction of its height), and the zoom.
type viewerState struct {
	Page   int     `json:"page,omitempty"`
	Offset float64 `json:"offset,omitempty"`
	Zoom   string  `json:"zoom,omitempty"`
}

// renderJob is a page being drawn.
type renderJob struct {
	dpi    float64
	cancel context.CancelFunc
}

// viewer is one tab of the viewer. Its fields belong to the UI thread.
type viewer struct {
	x      *Extension
	file   extension.FileEditorFile
	tab    extension.FileEditorTab
	ctx    context.Context
	cancel context.CancelFunc
	// gen counts the reads of the file (open, Check Again, a change on disk, a password).
	gen     int
	loading bool
	progs   poppler.Programs
	pages   []poppler.Page
	problem error
	deleted bool
	// locked tells that the document asks for its password; wrongPassword that the one given is not it. The password is kept only
	// here, never saved.
	locked        bool
	wrongPassword bool
	password      string
	stamp         fileStamp
	// The view: the zoom and its scale, the size of the view, the device pixel ratio, the layout and the scroll position.
	zoom         zoom
	scale        float64
	viewW, viewH float64
	dpr          float64
	lay          layout
	left, top    float64
	// restore is the place to go to when the pages and the view are known: the saved one, or the place before a reload.
	restore *place
	cache   *pageCache
	jobs    map[int]*renderJob
	// failed are the pages pdftoppm could not draw, with its message; they are not drawn again until the file is read again.
	failed map[int]string
	// settling tells that a zoom step was made less than zoomSettle ago; stopSettle stops its timer.
	settling   bool
	stopSettle func()
	scroll     *pagesScroll
	view       *viewerViewState
	pageBox    *ui.InputBoxController
	text       textState
	closed     bool
}

// open is the Open of the viewer.
func (x *Extension) open(file extension.FileEditorFile, tab extension.FileEditorTab) (extension.FileEditorContent, error) {
	v := &viewer{x: x, file: file, tab: tab, dpr: 1, cache: newPageCache(cacheLimit), jobs: map[int]*renderJob{}, failed: map[int]string{},
		pageBox: ui.NewInputBoxController(""), text: newTextState()}
	v.scroll = newPagesScroll(v)
	if s := file.State(); len(s) > 0 {
		var st viewerState
		if err := json.Unmarshal(s, &st); err != nil {
			x.api.Log().Debug("the state of a PDF tab cannot be read", "path", file.Path(), "err", err)
		} else {
			v.zoom = parseZoom(st.Zoom)
			if st.Page > 0 {
				v.restore = &place{page: st.Page - 1, offset: min(max(st.Offset, 0), 1)}
			}
		}
	}
	v.ctx, v.cancel = context.WithCancel(x.ctx)
	x.viewers = append(x.viewers, v)
	file.OnChange(v.changed)
	v.load()
	return extension.FileEditorContent{Build: v.build, SaveState: v.saveState, OnClose: v.close}, nil
}

// load finds Poppler and reads the pages on a goroutine of its own.
func (v *viewer) load() {
	v.gen++
	gen, path, ctx, password := v.gen, v.file.Path(), v.ctx, v.password
	v.stamp, _ = stampOf(path)
	setting, _ := extension.SettingValue[string](v.x.api.Settings(), poppler.PathSetting)
	find, info := v.x.find, v.x.info
	v.loading = true
	v.tab.SetBusy(true)
	go func() {
		progs, err := find(ctx, setting)
		var doc poppler.Document
		if err == nil {
			doc, err = info(ctx, progs, path, password)
		}
		v.x.api.Post(func() { v.loaded(gen, progs, doc, err) })
	}()
}

// loaded takes what load found. A document that asks for its password shows the password box; the pages of a reload keep the place.
func (v *viewer) loaded(gen int, progs poppler.Programs, doc poppler.Document, err error) {
	if v.closed || gen != v.gen {
		return
	}
	v.loading = false
	v.tab.SetBusy(false)
	v.stopJobs()
	v.cache.clear()
	v.failed = map[int]string{}
	v.progs, v.problem, v.deleted, v.locked, v.wrongPassword = progs, nil, false, false, false
	if e, ok := errors.FromError(err); ok && (e.Tag == "PDFPasswordNeeded" || e.Tag == "PDFPasswordWrong") {
		v.locked, v.wrongPassword, v.pages = true, e.Tag == "PDFPasswordWrong", nil
		v.refresh()
		return
	}
	if err != nil {
		v.problem, v.pages = err, nil
		v.refresh()
		return
	}
	if len(v.pages) > 0 && v.restore == nil {
		p := v.lay.placeAt(v.top)
		v.restore = &p
	}
	v.pages = doc.Pages
	v.relayout()
	v.resetText()
	v.refresh()
}

// submitPassword opens the document with a password.
func (v *viewer) submitPassword(password string) {
	if password == "" {
		return
	}
	v.password = password
	v.load()
	v.refresh()
}

// changed is the file changed or deleted on disk. A change that leaves the size and the time of the file as they were (the watcher's
// first event) reads nothing.
func (v *viewer) changed(c extension.FileEditorChange) {
	if c.Deleted {
		v.gen++
		v.loading, v.deleted = false, true
		v.tab.SetBusy(false)
		v.stopJobs()
		v.refresh()
		return
	}
	if s, ok := stampOf(v.file.Path()); ok && s == v.stamp && !v.deleted && !v.loading {
		return
	}
	v.load()
}

// saveState is the SaveState of the tab.
func (v *viewer) saveState() (any, error) {
	st := viewerState{Zoom: v.zoom.String()}
	switch {
	case v.restore != nil:
		st.Page, st.Offset = v.restore.page+1, v.restore.offset
	case len(v.pages) > 0:
		p := v.lay.placeAt(v.top)
		st.Page, st.Offset = p.page+1, p.offset
	}
	if st.Zoom == "fitWidth" {
		st.Zoom = ""
	}
	return st, nil
}

// close is the OnClose of the tab: its processes end at once.
func (v *viewer) close() {
	v.closed = true
	v.cancel()
	v.stopJobs()
	if v.stopSettle != nil {
		v.stopSettle()
	}
	v.pageBox.Dispose()
	v.text.findBox.Dispose()
	for i, o := range v.x.viewers {
		if o == v {
			v.x.viewers = append(v.x.viewers[:i], v.x.viewers[i+1:]...)
			break
		}
	}
}

// refresh builds the view again.
func (v *viewer) refresh() {
	if v.view != nil {
		v.view.changed()
	}
}

// ---- the layout and the scroll position ----

// relayout lays the pages out at the scale of the zoom for the view, keeping the place at the top of the view (or going to the place
// to restore).
func (v *viewer) relayout() {
	if len(v.pages) == 0 || v.viewW <= 0 || v.viewH <= 0 {
		return
	}
	at := v.lay.placeAt(v.top)
	if len(v.lay.rects) != len(v.pages) {
		at = place{}
	}
	if v.restore != nil {
		at = *v.restore
		v.restore = nil
	}
	cur := min(max(at.page, 0), len(v.pages)-1)
	centerX := 0.5
	if v.lay.width > 0 {
		centerX = (v.left + v.viewW/2) / v.lay.width
	}
	v.scale = scaleFor(v.zoom, v.pages, cur, v.viewW, v.viewH)
	v.lay = layoutPages(v.pages, v.scale, v.viewW)
	v.left = centerX*v.lay.width - v.viewW/2
	v.top = v.lay.heightOf(at)
	v.clamp()
}

// clamp keeps the scroll position within the content.
func (v *viewer) clamp() {
	v.left = min(max(v.left, 0), max(v.lay.width-v.viewW, 0))
	v.top = min(max(v.top, 0), max(v.lay.height-v.viewH, 0))
}

// setViewport is the size of the view and its device pixel ratio, from the layout of the view; true when they changed.
func (v *viewer) setViewport(w, h, dpr float64) bool {
	if w == v.viewW && h == v.viewH && dpr == v.dpr {
		return false
	}
	v.viewW, v.viewH, v.dpr = w, h, dpr
	v.relayout()
	return true
}

// scrollTo moves the view to a position, and draws the pages that come into view.
func (v *viewer) scrollTo(left, top float64) {
	v.left, v.top = left, top
	v.clamp()
	v.moved()
}

// moved tells the scrollbars and the page box that the view moved, builds it again and draws what it shows.
func (v *viewer) moved() {
	v.scroll.NotifyListeners()
	v.syncPageBox()
	v.refresh()
	v.schedule()
}

// currentPage is the page the view shows the most of (from 0), or -1 without pages.
func (v *viewer) currentPage() int {
	if len(v.lay.rects) == 0 {
		return -1
	}
	return v.lay.current(v.top, v.top+v.viewH)
}

// goToPage scrolls to the top of a page (from 0); a page beyond the first or the last goes to it.
func (v *viewer) goToPage(i int) {
	if len(v.lay.rects) == 0 {
		return
	}
	i = min(max(i, 0), len(v.lay.rects)-1)
	v.top = v.lay.heightOf(place{page: i})
	v.clamp()
	v.moved()
}

// syncPageBox writes the current page in the page box while it does not have the focus.
func (v *viewer) syncPageBox() {
	if v.pageBox.HasFocus() {
		return
	}
	text := ""
	if cur := v.currentPage(); cur >= 0 {
		text = strconv.Itoa(cur + 1)
	}
	if v.pageBox.Value() != text {
		v.pageBox.SetValue(text)
	}
}

// focusPageBox is Go to Page: the page box takes the focus with its text selected.
func (v *viewer) focusPageBox() {
	if v.view != nil {
		v.pageBox.Focus()
		v.pageBox.SelectAll()
	}
}

// submitPage goes to the page typed in the page box, and gives the focus back to the pages.
func (v *viewer) submitPage(text string) {
	if n, ok := pageNumber(text, len(v.pages)); ok {
		v.goToPage(n - 1)
	}
	if v.view != nil {
		v.view.focus.RequestFocus()
	}
	v.syncPageBox()
}

// ---- the zoom ----

// setZoom zooms, keeping the place at the top of the view; the pages are drawn again at the new scale zoomSettle after the last step.
func (v *viewer) setZoom(z zoom) {
	v.zoom = z
	v.relayout()
	v.settle()
	v.moved()
}

// zoomIn is Zoom In: the next level above the scale shown.
func (v *viewer) zoomIn() {
	if z, ok := zoomIn(v.shownScale()); ok {
		v.setZoom(z)
	}
}

// zoomOut is Zoom Out: the next level below the scale shown.
func (v *viewer) zoomOut() {
	if z, ok := zoomOut(v.shownScale()); ok {
		v.setZoom(z)
	}
}

// shownScale is the scale the pages are shown at (that of 100% before they are laid out).
func (v *viewer) shownScale() float64 {
	if v.scale == 0 {
		return scaleFor(v.zoom, v.pages, 0, v.viewW, v.viewH)
	}
	return v.scale
}

// settle holds the drawing of pages for zoomSettle (again, at each step).
func (v *viewer) settle() {
	if v.view == nil || v.view.afterFunc == nil {
		return
	}
	if v.stopSettle != nil {
		v.stopSettle()
	}
	v.settling = true
	v.stopSettle = v.view.afterFunc(zoomSettle, func() {
		v.settling, v.stopSettle = false, nil
		v.schedule()
	})
}

// ---- drawing the pages ----

// dpiFor is the resolution a page is drawn at for the scale and the device pixel ratio, kept so that its larger side fits a picture.
func (v *viewer) dpiFor(i int) float64 {
	dpi := v.scale * 72 * v.dpr
	p := v.pages[i]
	if side := max(p.Width, p.Height) * dpi / 72; side > ui.MaxPictureSide {
		dpi = float64(ui.MaxPictureSide) * 72 / max(p.Width, p.Height)
	}
	return dpi
}

// wanted are the pages to draw, in their order: those in view, then one before and one after.
func (v *viewer) wanted() []int {
	first, last := v.lay.visible(v.top, v.top+v.viewH)
	if first < 0 {
		return nil
	}
	var out []int
	for i := first; i <= last; i++ {
		out = append(out, i)
	}
	if first > 0 {
		out = append(out, first-1)
	}
	if last+1 < len(v.pages) {
		out = append(out, last+1)
	}
	return out
}

// schedule draws the wanted pages that are not drawn at their resolution, at most maxRenders at once, and stops the drawing of pages
// that left the view or whose resolution changed.
func (v *viewer) schedule() {
	if v.closed || v.deleted || v.loading || len(v.pages) == 0 || len(v.lay.rects) != len(v.pages) || v.settling {
		return
	}
	want := v.wanted()
	for p, j := range v.jobs {
		keep := false
		for _, w := range want {
			keep = keep || (w == p && sameDPI(j.dpi, v.dpiFor(p)))
		}
		if !keep {
			j.cancel()
			delete(v.jobs, p)
		}
	}
	for _, p := range want {
		if len(v.jobs) >= maxRenders {
			break
		}
		dpi := v.dpiFor(p)
		if _, failed := v.failed[p]; failed || v.jobs[p] != nil || v.cache.get(p, dpi) != nil {
			continue
		}
		v.start(p, dpi)
	}
}

// start draws a page on a goroutine of its own.
func (v *viewer) start(page int, dpi float64) {
	ctx, cancel := context.WithCancel(v.ctx)
	job := &renderJob{dpi: dpi, cancel: cancel}
	v.jobs[page] = job
	gen, path, password, progs, render := v.gen, v.file.Path(), v.password, v.progs, v.x.render
	go func() {
		img, err := render(ctx, progs, path, password, page+1, dpi)
		var pic *gui.Image
		if err == nil {
			pic, err = ui.NewPicture(img)
		}
		v.x.api.Post(func() { v.rendered(gen, page, job, pic, err) })
	}()
}

// rendered takes a drawn page, and draws the next.
func (v *viewer) rendered(gen, page int, job *renderJob, pic *gui.Image, err error) {
	job.cancel()
	if v.closed || gen != v.gen || v.jobs[page] != job {
		return // stopped
	}
	delete(v.jobs, page)
	if err != nil {
		v.x.api.Log().Warn("a page of a PDF cannot be drawn", "path", v.file.Path(), "page", page+1, "err", err)
		v.failed[page] = messageOf(err)
	} else {
		v.cache.put(page, job.dpi, pic)
	}
	v.refresh()
	v.schedule()
}

// stopJobs stops the drawing of every page.
func (v *viewer) stopJobs() {
	for p, j := range v.jobs {
		j.cancel()
		delete(v.jobs, p)
	}
}

// ---- messages ----

// problemText is the message of what keeps the document from being shown, the instructions to link, and whether Poppler is what is
// missing (its buttons are shown then).
func problemText(err error) (text, docs string, needsPoppler bool) {
	e, ok := errors.FromError(err)
	if !ok {
		return err.Error(), "", false
	}
	switch e.Tag {
	case "PopplerNotFound", "PopplerPathInvalid", "PopplerProgramMissing", "PopplerTooOld", "PopplerVersionFailed":
		return "Showing PDF files needs Poppler. " + strings.TrimSuffix(e.Message, ".") + ".", installDocs, true
	case "PDFDamaged":
		msg, _ := e.Metadata["message"].(string)
		if msg == "" {
			return "This file cannot be opened as a PDF.", "", false
		}
		return "This file cannot be opened as a PDF: " + strings.TrimSuffix(msg, ".") + ".", "", false
	}
	return e.Message, "", false
}
