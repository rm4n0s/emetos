package mediapreview

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// rig is Media Preview activated over the fake API, with a folder for the images of the test.
type rig struct {
	t   *testing.T
	api *fakeapi.API
	x   *Extension
	dir string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	x := New().(*Extension)
	api := fakeapi.New(t, x.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: dir, ID: dir})
	if err := x.Activate(t.Context(), api); err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, api: api, x: x, dir: dir}
}

// write writes a file of the folder and gives its path.
func (r *rig) write(name string, data []byte) string {
	r.t.Helper()
	p := filepath.Join(r.dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		r.t.Fatal(err)
	}
	return p
}

// open opens the file in the image preview, with the state of a tab that comes back, and waits until it is read.
func (r *rig) open(path string, state json.RawMessage) (*fakeapi.FakeFileEditor, *preview) {
	r.t.Helper()
	fe, err := r.api.OpenFileEditor(ImageEditorID, path, state)
	if err != nil {
		r.t.Fatal(err)
	}
	p := r.x.previews[len(r.x.previews)-1]
	r.api.WaitFor("the image is read", func() bool { return !p.loading })
	return fe, p
}

// statusTexts are the texts of the tab's status bar items by id.
func statusTexts(fe *fakeapi.FakeFileEditor) map[string]string {
	out := map[string]string{}
	for _, it := range fe.Status() {
		out[it.ID] = it.Text
	}
	return out
}

// solidPNG is a PNG of w x h pixels of one color.
func solidPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return encode(t, func(b *bytes.Buffer) error { return png.Encode(b, img) })
}

func TestTheStatusBarEntries(t *testing.T) {
	r := newRig(t)
	data := pngData(t)
	fe, _ := r.open(r.write("a.png", data), nil)
	want := map[string]string{"zoom": "Whole Image", "size": "3x2", "binarySize": formatSize(int64(len(data)))}
	got := statusTexts(fe)
	for id, text := range want {
		if got[id] != text {
			t.Errorf("%s: %q, want %q", id, got[id], text)
		}
	}
	// VS Code's order on the right: the zoom, then the size, then the size of the file
	prio := map[string]float64{}
	for _, it := range fe.Status() {
		prio[it.ID] = it.Priority
		if it.Alignment != extension.StatusRight {
			t.Errorf("%s is not on the right", it.ID)
		}
	}
	if !(prio["zoom"] > prio["size"] && prio["size"] > prio["binarySize"]) {
		t.Errorf("priorities %v", prio)
	}
}

func TestTheZoomCommandsActOnTheActiveTab(t *testing.T) {
	r := newRig(t)
	fe, p := r.open(r.write("a.png", pngData(t)), nil)
	// not the active editor: nothing happens
	if err := r.api.Run(CommandZoomIn, nil); err != nil {
		t.Fatal(err)
	}
	if !p.fit {
		t.Fatal("an inactive tab was zoomed")
	}
	fe.SetActive(true)
	steps := []struct {
		command, want string
	}{
		{CommandZoomIn, "150%"},
		{CommandZoomIn, "200%"},
		{CommandZoomOut, "150%"},
		{CommandZoomOut, "100%"},
		{CommandZoomOut, "90%"},
	}
	for _, s := range steps {
		if err := r.api.Run(s.command, nil); err != nil {
			t.Fatal(err)
		}
		if got := statusTexts(fe)["zoom"]; got != s.want {
			t.Errorf("after %s: %q, want %q", s.command, got, s.want)
		}
	}
}

func TestTheZoomPick(t *testing.T) {
	r := newRig(t)
	fe, p := r.open(r.write("a.png", pngData(t)), nil)
	var zoom extension.StatusItem
	for _, it := range fe.Status() {
		if it.ID == "zoom" {
			zoom = it
		}
	}
	zoom.OnClick()
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no pick")
	}
	if got, want := pick.Labels(), []string{"1000%", "500%", "200%", "100%", "50%", "20%", "Whole Image"}; !slices.Equal(got, want) {
		t.Errorf("labels %v", got)
	}
	if pick.Options().Placeholder != "Select zoom level" {
		t.Errorf("placeholder %q", pick.Options().Placeholder)
	}
	pick.Choose(2)
	if p.fit || p.scale != 2 || statusTexts(fe)["zoom"] != "200%" {
		t.Errorf("fit %v scale %v", p.fit, p.scale)
	}
	zoom.OnClick()
	r.api.LastPick().Choose(6)
	if !p.fit || statusTexts(fe)["zoom"] != "Whole Image" {
		t.Errorf("Whole Image: fit %v", p.fit)
	}
}

func TestAReloadKeepsTheZoomUnlessTheSizeChanged(t *testing.T) {
	r := newRig(t)
	path := r.write("a.png", solidPNG(t, 4, 4, color.NRGBA{R: 255, A: 255}))
	fe, p := r.open(path, nil)
	p.setScaleAt(2, 0, 0)
	r.write("a.png", solidPNG(t, 4, 4, color.NRGBA{B: 255, A: 255}))
	fe.Change(false)
	r.api.WaitFor("the image is read again", func() bool { return !p.loading })
	if p.fit || p.scale != 2 {
		t.Errorf("same size: fit %v scale %v", p.fit, p.scale)
	}
	if got := pixel(t, p.img, 0, 0); got != (color.RGBA{B: 255, A: 255}) {
		t.Errorf("the new pixels are not read: %v", got)
	}
	r.write("a.png", solidPNG(t, 6, 3, color.NRGBA{G: 255, A: 255}))
	fe.Change(false)
	r.api.WaitFor("the image is read again", func() bool { return !p.loading })
	if !p.fit || statusTexts(fe)["size"] != "6x3" {
		t.Errorf("another size: fit %v, status %v", p.fit, statusTexts(fe))
	}
}

func TestADeletedFile(t *testing.T) {
	r := newRig(t)
	fe, p := r.open(r.write("a.png", pngData(t)), nil)
	fe.Change(true)
	if !p.deleted || len(fe.Status()) != 0 {
		t.Errorf("deleted %v, status %v", p.deleted, fe.Status())
	}
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 300}, widgets.NewBuilder(fe.Content.Build))
	if !ts.Exists(gluttertest.ByText("The file was deleted.")) {
		t.Error("the message is not shown")
	}
}

func TestTheStateOfTheTab(t *testing.T) {
	r := newRig(t)
	path := r.write("a.png", pngData(t))
	fe, p := r.open(path, nil)
	if s, err := fe.SaveState(); err != nil || string(s) != `{}` {
		t.Errorf("fitted: %s %v", s, err)
	}
	p.setScaleAt(5, 0, 0)
	s, err := fe.SaveState()
	if err != nil || string(s) != `{"scale":5}` {
		t.Errorf("zoomed: %s %v", s, err)
	}
	// a tab that comes back starts zoomed
	fe2, p2 := r.open(path, json.RawMessage(`{"scale":3,"offsetX":4}`))
	if p2.fit || p2.scale != 3 || p2.offX != 4 || statusTexts(fe2)["zoom"] != "300%" {
		t.Errorf("restored: fit %v scale %v off %v", p2.fit, p2.scale, p2.offX)
	}
	fe.Close()
	if len(r.x.previews) != 1 || r.x.previews[0] != p2 {
		t.Errorf("previews after a close: %d", len(r.x.previews))
	}
}

func TestCopy(t *testing.T) {
	r := newRig(t)
	fe, _ := r.open(r.write("a.png", pngData(t)), nil)
	fe.SetActive(true)
	svgFE, _ := r.open(r.write("a.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="30"><rect width="40" height="30" fill="red"/></svg>`)), nil)
	if err := r.api.Run(CommandCopyImage, nil); err != nil {
		t.Fatal(err)
	}
	fe.SetActive(false)
	svgFE.SetActive(true)
	if err := r.api.Run(CommandCopyImage, nil); err != nil {
		t.Fatal(err)
	}
	images := r.api.ClipboardImages()
	if len(images) != 2 {
		t.Fatalf("%d images copied", len(images))
	}
	for i, size := range []image.Point{{3, 2}, {40, 30}} {
		img, err := png.Decode(bytes.NewReader(images[i]))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Size() != size {
			t.Errorf("copy %d is %v", i, img.Bounds().Size())
		}
	}
	// the SVG is drawn: its middle is red
	img, _ := png.Decode(bytes.NewReader(images[1]))
	if r, g, b, a := img.At(20, 15).RGBA(); r>>8 != 255 || g != 0 || b != 0 || a>>8 != 255 {
		t.Errorf("the middle of the SVG is %v", img.At(20, 15))
	}
}

func TestACopyThatFails(t *testing.T) {
	r := newRig(t)
	// an SVG wider than the software renderer draws
	fe, p := r.open(r.write("wide.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40000" height="1"></svg>`)), nil)
	fe.SetActive(true)
	_, err := p.pngOf()
	route(t, err, "pngOf.ImageCopyFailed")
	if err := r.api.Run(CommandCopyImage, nil); err != nil {
		t.Fatal(err)
	}
	if notes := r.api.Notes(); len(notes) != 1 || notes[0].Severity != "warn" {
		t.Errorf("notes %+v", notes)
	}
	// nothing read yet
	_, err = (&preview{file: p.file}).pngOf()
	route(t, err, "pngOf.ImageNotLoaded")
	// a picture that was disposed
	_, img := r.open(r.write("a.png", pngData(t)), nil)
	frame := img.img.Frames[0].Image
	frame.Dispose()
	_, err = encodePNG("a.png", frame)
	route(t, err, "encodePNG.ImageCopyFailed")
}

func TestAnImageThatCannotBeShown(t *testing.T) {
	r := newRig(t)
	for _, tt := range []struct {
		name, file string
		data       []byte
		message    string
	}{
		{"not an image", "text.png", []byte("not an image"), "An error occurred while loading the image."},
		{"too large", "wide.bmp", bmpOfWidth(t, 40000), "This image is too large to show (40000 x 1)."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fe, _ := r.open(r.write(tt.file, tt.data), nil)
			if _, ok := statusTexts(fe)["zoom"]; ok {
				t.Error("the zoom entry shows for an image that is not shown")
			}
			ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 500, Height: 300}, widgets.NewBuilder(fe.Content.Build))
			if !ts.Exists(gluttertest.ByText(tt.message)) {
				t.Errorf("no %q", tt.message)
			}
			ts.Tap(gluttertest.ByText("Open file using Emetos's standard text/binary editor?"))
			ts.Pump(time.Millisecond)
			if fe.ReopenedAsText() != 1 {
				t.Errorf("reopened %d times", fe.ReopenedAsText())
			}
		})
	}
}

// viewRig is a preview on screen: a 100 x 50 image in a 400 x 300 view.
func viewRig(t *testing.T) (*rig, *fakeapi.FakeFileEditor, *preview, *gluttertest.Tester) {
	r := newRig(t)
	fe, p := r.open(r.write("a.png", solidPNG(t, 100, 50, color.NRGBA{R: 200, A: 255})), nil)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 300}, widgets.NewBuilder(fe.Content.Build))
	return r, fe, p, ts
}

func TestClickingZooms(t *testing.T) {
	_, fe, p, ts := viewRig(t)
	if p.viewW != 400 || p.viewH != 300 || p.shownScale() != 1 {
		t.Fatalf("view %vx%v, scale %v", p.viewW, p.viewH, p.shownScale())
	}
	ts.TapAt(gui.Offset{Dx: 200, Dy: 150})
	ts.Pump(time.Millisecond)
	if p.fit || p.scale != 1.5 || statusTexts(fe)["zoom"] != "150%" {
		t.Errorf("click: fit %v scale %v", p.fit, p.scale)
	}
	// the key of Zoom Out (Ctrl here, Alt on macOS)
	key := services.LogicalKeyboardKeyControlLeft
	if zoomOutIsAlt() {
		key = services.LogicalKeyboardKeyAltLeft
	}
	ts.SendKeyDownEvent(key)
	ts.TapAt(gui.Offset{Dx: 200, Dy: 150})
	ts.SendKeyUpEvent(key)
	ts.Pump(time.Millisecond)
	if p.scale != 1 {
		t.Errorf("click with the key: scale %v", p.scale)
	}
}

func TestTheWheel(t *testing.T) {
	_, _, p, ts := viewRig(t)
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyControlLeft)
	ts.ScrollAt(gui.Offset{Dx: 200, Dy: 150}, gui.Offset{Dy: -20})
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyControlLeft)
	ts.Pump(time.Millisecond)
	if !near(p.scale, 1.075) || p.fit {
		t.Errorf("Ctrl+wheel: scale %v", p.scale)
	}
	// zoomed in past the view, the wheel scrolls
	p.setScaleAt(10, 200, 150)
	ts.Pump(time.Millisecond)
	before := p.offY
	ts.ScrollAt(gui.Offset{Dx: 200, Dy: 150}, gui.Offset{Dy: 30})
	ts.Pump(time.Millisecond)
	if p.offY != before+30 {
		t.Errorf("wheel: offY %v, was %v", p.offY, before)
	}
}

func TestDraggingScrollsAZoomedImage(t *testing.T) {
	_, _, p, ts := viewRig(t)
	p.setScaleAt(10, 200, 150) // 1000 x 500 in 400 x 300
	ts.Pump(time.Millisecond)
	x, y := p.offX, p.offY
	ts.DragFrom(gui.Offset{Dx: 200, Dy: 150}, gui.Offset{Dx: 50, Dy: 40})
	ts.Pump(time.Millisecond)
	if !near(p.offX, x-50) || !near(p.offY, y-40) || p.scale != 10 {
		t.Errorf("offsets (%v, %v), were (%v, %v); scale %v", p.offX, p.offY, x, y, p.scale)
	}
}

func TestAnAnimatedGIFPlays(t *testing.T) {
	r := newRig(t)
	fe, p := r.open(r.write("a.gif", threeFrameGIF(t)), nil)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 200, Height: 100}, widgets.NewBuilder(fe.Content.Build))
	// the frames last 50, 100 and 100 ms; the loop count of 1 plays them twice, then the last frame stays. Sampled every 50 ms from 25 ms.
	ts.Pump(25 * time.Millisecond)
	var frames []int
	for range 12 {
		frames = append(frames, p.frame)
		ts.Pump(50 * time.Millisecond)
	}
	if want := []int{0, 1, 1, 2, 2, 0, 1, 1, 2, 2, 2, 2}; !slices.Equal(frames, want) {
		t.Errorf("frames %v, want %v", frames, want)
	}
}

func TestPixelsAreSquaresFromThreeTimes(t *testing.T) {
	for _, tt := range []struct {
		scale float64
		want  gui.FilterQuality
	}{{1, gui.FilterQualityLow}, {2.9, gui.FilterQualityLow}, {3, gui.FilterQualityNone}, {20, gui.FilterQualityNone}} {
		if got := filterQuality(tt.scale); got != tt.want {
			t.Errorf("scale %v: quality %v", tt.scale, got)
		}
	}
}
