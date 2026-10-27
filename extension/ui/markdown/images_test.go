package markdown

import (
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The decoding and the cache of the images of a document (step 17b.2).

// writeImage writes an image file of w by h pixels in the format of the extension.
func writeImage(t testing.TB, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), 90, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	switch filepath.Ext(path) {
	case ".jpg":
		err = jpeg.Encode(f, img, nil)
	case ".gif":
		err = gif.Encode(f, img, nil)
	default:
		err = png.Encode(f, img)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeImageKeepsOnlyThePixelsThatAreDrawn(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name         string
		file         string
		w, h         int
		fit          imageFit
		wantW, wantH float64 // logical size
		wantPx       int     // pixels across
	}{
		{"scaled to the box, at a ratio of 1", "a.png", 1200, 800, imageFit{maxWidth: 600, maxHeight: 600, ratio: 1}, 600, 400, 600},
		{"the ratio gives more pixels", "b.png", 1200, 800, imageFit{maxWidth: 600, maxHeight: 600, ratio: 2}, 600, 400, 1200},
		{"never more pixels than the file has", "c.png", 300, 200, imageFit{maxWidth: 600, maxHeight: 600, ratio: 2}, 300, 200, 300},
		{"a tall image is limited by the height", "d.png", 400, 1600, imageFit{maxWidth: 800, maxHeight: 600, ratio: 1}, 150, 600, 150},
		{"a small image keeps its size", "e.png", 100, 50, imageFit{maxWidth: 800, maxHeight: 600, ratio: 1}, 100, 50, 100},
		{"jpeg", "f.jpg", 640, 480, imageFit{maxWidth: 320, maxHeight: 600, ratio: 1}, 320, 240, 320},
		{"gif", "g.gif", 640, 480, imageFit{maxWidth: 320, maxHeight: 600, ratio: 1}, 320, 240, 320},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.file)
			writeImage(t, path, tt.w, tt.h)
			res, err := decodeImage(context.Background(), path, tt.fit)
			if err != nil {
				t.Fatal(err)
			}
			if res.width != tt.wantW || res.height != tt.wantH {
				t.Errorf("shown %v x %v logical pixels, want %v x %v", res.width, res.height, tt.wantW, tt.wantH)
			}
			if got := res.pic.Width(); got != tt.wantPx {
				t.Errorf("holds %d pixels across, want %d", got, tt.wantPx)
			}
			// the proportions of the file are kept
			if got, want := float64(res.pic.Height())/float64(res.pic.Width()), float64(tt.h)/float64(tt.w); got < want*0.99 || got > want*1.01 {
				t.Errorf("the pixels are %d x %d: the proportions changed (%v, want %v)", res.pic.Width(), res.pic.Height(), got, want)
			}
		})
	}
}

func TestDecodeImageErrorRoutes(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.png")
	if err := os.WriteFile(garbage, []byte("this is not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	huge := filepath.Join(dir, "huge.png")
	writeImage(t, huge, 8, 8)
	// a PNG header that says 20000 x 20000 pixels: the decoder reads the header only
	if err := os.WriteFile(huge, hugeHeader(), 0o600); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	fit := imageFit{maxWidth: 100, maxHeight: 100, ratio: 1}
	tests := []struct {
		name  string
		ctx   context.Context
		path  string
		route string
	}{
		{"no such file", context.Background(), filepath.Join(dir, "missing.png"), "decodeImage.ImageOpenFailed"},
		{"not an image", context.Background(), garbage, "decodeImage.ImageDecodeFailed"},
		{"too many pixels", context.Background(), huge, "decodeImage.ImageTooLarge"},
		{"cancelled", cancelled, garbage, "decodeImage.ImageRequestCancelled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeImage(tt.ctx, tt.path, fit)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("err = %v, want an *errors.Error", err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("route %s, want %s", appErr.Route(), tt.route)
			}
		})
	}
}

// hugeHeader is the start of a PNG file that says the image is 20000 x 20000 pixels.
func hugeHeader() []byte {
	ihdr := []byte{'I', 'H', 'D', 'R', 0, 0, 0x4e, 0x20, 0, 0, 0x4e, 0x20, 8, 6, 0, 0, 0}
	out := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13}
	out = append(out, ihdr...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(ihdr))
}

func TestFileStampErrorRoute(t *testing.T) {
	_, _, err := fileStamp(filepath.Join(t.TempDir(), "missing.png"))
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("fileStamp.ImageStatFailed") {
		t.Errorf("err = %v", err)
	}
}

// fakeCache is a cache whose decoding is a function of the test and whose posted work runs when the test says.
type fakeCache struct {
	*ImageCache
	posted chan func()
	calls  chan string
}

func newFakeCache(t testing.TB, budget int, decode func(path string) (imageResult, error)) *fakeCache {
	t.Helper()
	f := &fakeCache{ImageCache: NewImageCache(budget), posted: make(chan func(), 100), calls: make(chan string, 100)}
	t.Cleanup(f.Close)
	f.attach(func(fn func()) { f.posted <- fn })
	f.decode = func(_ context.Context, path string, _ imageFit) (imageResult, error) {
		f.calls <- path
		return decode(path)
	}
	f.stamp = func(string) (time.Time, int64, error) { return time.Unix(100, 0), 10, nil }
	return f
}

// run runs what the cache posted until the decode of path is done and its result delivered.
func (f *fakeCache) run(t testing.TB) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for f.pending() > 0 {
		select {
		case fn := <-f.posted:
			fn()
		case <-deadline:
			t.Fatal("a decoding did not finish")
		}
	}
}

// picture is a result of w by h pixels.
func picture(t testing.TB, w, h int) imageResult {
	t.Helper()
	pic, err := kit.NewPicture(image.NewRGBA(image.Rect(0, 0, w, h)))
	if err != nil {
		t.Fatal(err)
	}
	return imageResult{pic: pic, width: float64(w), height: float64(h)}
}

func TestImageCacheBudgetAndLeastRecentlyUsedEviction(t *testing.T) {
	// 10 x 10 pixels is 400 bytes: the budget holds three
	f := newFakeCache(t, 1200, func(string) (imageResult, error) { return picture(t, 10, 10), nil })
	ctx := context.Background()
	load := func(path string) {
		req := imageRequest{path: path, fit: imageFit{maxWidth: 100, maxHeight: 100, ratio: 1}}
		key, _, ok := f.get(ctx, req)
		if ok {
			return
		}
		f.load(key, req, func(imageResult) {})
		f.run(t)
	}
	has := func(path string) bool {
		_, _, ok := f.get(ctx, imageRequest{path: path, fit: imageFit{maxWidth: 100, maxHeight: 100, ratio: 1}})
		return ok
	}
	for _, p := range []string{"a", "b", "c"} {
		load(p)
	}
	if f.Used() != 1200 || f.Len() != 3 {
		t.Fatalf("used %d in %d entries, want 1200 in 3", f.Used(), f.Len())
	}
	has("a") // a is the most recently used now; b is the least
	load("d")
	if f.Used() > f.Budget() || f.Len() != 3 {
		t.Errorf("after a fourth image: used %d (budget %d) in %d entries", f.Used(), f.Budget(), f.Len())
	}
	if !has("a") || has("b") || !has("c") || !has("d") {
		t.Errorf("b was the least recently used and goes: a %v b %v c %v d %v", has("a"), has("b"), has("c"), has("d"))
	}
	// one image over the budget on its own is kept (a view is about to draw it), and everything else goes
	big := newFakeCache(t, 300, func(string) (imageResult, error) { return picture(t, 10, 10), nil })
	req := imageRequest{path: "big", fit: imageFit{maxWidth: 100, maxHeight: 100, ratio: 1}}
	key, _, _ := big.get(ctx, req)
	big.load(key, req, func(imageResult) {})
	big.run(t)
	if big.Len() != 1 || big.Used() != 400 {
		t.Errorf("an image over the budget: %d entries, %d bytes", big.Len(), big.Used())
	}
}

func TestImageCacheKeysAreThePathTheFileAndTheSize(t *testing.T) {
	f := newFakeCache(t, 0, func(string) (imageResult, error) { return picture(t, 4, 4), nil })
	mod := time.Unix(100, 0)
	size := int64(10)
	f.stamp = func(string) (time.Time, int64, error) { return mod, size, nil }
	ctx := context.Background()
	fit := imageFit{maxWidth: 100, maxHeight: 100, ratio: 1}
	load := func(path string, fit imageFit) bool {
		req := imageRequest{path: path, fit: fit}
		key, _, ok := f.get(ctx, req)
		if !ok {
			f.load(key, req, func(imageResult) {})
			f.run(t)
		}
		return ok
	}
	if load("a", fit) {
		t.Fatal("the first request was in the cache")
	}
	if !load("a", fit) {
		t.Error("the same path, file and size is a hit")
	}
	if load("a", imageFit{maxWidth: 50, maxHeight: 100, ratio: 1}) {
		t.Error("another width is another entry")
	}
	if load("a", imageFit{maxWidth: 100, maxHeight: 100, ratio: 2}) {
		t.Error("another pixel ratio is another entry")
	}
	mod = time.Unix(200, 0)
	if load("a", fit) {
		t.Error("another modification time is another entry")
	}
	size = 11
	if load("a", fit) {
		t.Error("another file size is another entry")
	}
	if load("b", fit) {
		t.Error("another path is another entry")
	}
}

func TestImageCacheKeepsFailuresAndSharesALoad(t *testing.T) {
	release := make(chan struct{})
	f := newFakeCache(t, 0, func(path string) (imageResult, error) {
		<-release
		return imageResult{}, errors.New("TestDecodeFailed", "no", "path", path)
	})
	ctx := context.Background()
	req := imageRequest{path: "bad", fit: imageFit{maxWidth: 100, maxHeight: 100, ratio: 1}}
	key, _, _ := f.get(ctx, req)
	var got []imageResult
	f.load(key, req, func(r imageResult) { got = append(got, r) })
	f.load(key, req, func(r imageResult) { got = append(got, r) })
	close(release)
	f.run(t)
	if len(got) != 2 || got[0].err == nil || got[1].err == nil {
		t.Fatalf("both views get the failure: %+v", got)
	}
	if n := len(f.calls); n != 1 {
		t.Errorf("the image was decoded %d times for two views", n)
	}
	// the failure is an entry: the next request does not decode again
	if _, res, ok := f.get(ctx, req); !ok || res.err == nil || f.Used() != 0 {
		t.Errorf("the failure is not kept: %v %+v used %d", ok, res, f.Used())
	}
}

func TestImageCacheDoesNotDecodeWhatNobodyWaitsFor(t *testing.T) {
	// two decodings run at a time: the third waits for a slot, and its view is gone by then
	hold := make(chan struct{})
	f := newFakeCache(t, 0, func(string) (imageResult, error) {
		<-hold
		return picture(t, 4, 4), nil
	})
	fit := imageFit{maxWidth: 100, maxHeight: 100, ratio: 1}
	cancels := map[string]func(){}
	for _, p := range []string{"one", "two", "three"} {
		req := imageRequest{path: p, fit: fit}
		key, _, _ := f.get(context.Background(), req)
		cancels[p] = f.load(key, req, func(imageResult) {})
	}
	started := map[string]bool{}
	for len(started) < 2 {
		select {
		case fn := <-f.posted:
			fn() // the UI thread answers whether a view waits
		case p := <-f.calls:
			started[p] = true
		case <-time.After(10 * time.Second):
			t.Fatal("the first two decodings did not start")
		}
	}
	var waiting string
	for p := range cancels {
		if !started[p] {
			waiting = p
		}
	}
	cancels[waiting]() // the view of the one that waits for a slot left
	close(hold)
	f.run(t)
	select {
	case p := <-f.calls:
		t.Errorf("%s was decoded though no view waited for it", p)
	default:
	}
	if f.Len() != 2 {
		t.Errorf("%d entries, want the two that were wanted", f.Len())
	}
}

func TestImageCacheRequestErrorRoutes(t *testing.T) {
	c := NewImageCache(0)
	t.Cleanup(c.Close)
	c.stamp = func(string) (time.Time, int64, error) { return time.Time{}, 0, errors.New("TestStamp", "no") }
	_, res, ok := c.get(context.Background(), imageRequest{path: "x"})
	if !ok || res.err == nil {
		t.Errorf("a file with no stamp is a failure that shows at once: %v %+v", ok, res)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, res, ok = c.get(cancelled, imageRequest{path: "x"})
	appErr, isErr := errors.FromError(res.err)
	if !ok || !isErr || !appErr.HasRoute("keyOf.ImageRequestCancelled") {
		t.Errorf("a cancelled view: %v %v", ok, res.err)
	}
}
