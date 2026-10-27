package ui_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// gradient is a picture whose pixels differ from each other, w by h.
func gradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(255 * x / max(w-1, 1)), G: uint8(255 * y / max(h-1, 1)), B: 128, A: 255})
		}
	}
	return img
}

// checker is a w by h image of black and white squares.
func checker(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(255 * ((x + y) % 2))})
		}
	}
	return img
}

func TestAPictureCopiesThePixelsOfAnyImage(t *testing.T) {
	sub := gradient(8, 8).SubImage(image.Rect(2, 2, 6, 5)).(*image.NRGBA) // a window that does not start at 0,0
	cases := []struct {
		name      string
		img       image.Image
		w, h      int
		firstPix  [4]byte
		wantError string
	}{
		{"an NRGBA image", gradient(4, 2), 4, 2, [4]byte{0, 0, 128, 255}, ""},
		{"a gray image", checker(3, 3), 3, 3, [4]byte{0, 0, 0, 255}, ""},
		{"a window of an image", sub, 4, 3, [4]byte{uint8(255 * 2 / 7), uint8(255 * 2 / 7), 128, 255}, ""},
		{"nil", nil, 0, 0, [4]byte{}, "PictureInvalid"},
		{"an empty image", image.NewRGBA(image.Rect(0, 0, 0, 5)), 0, 0, [4]byte{}, "PictureInvalid"},
		{"one that is too wide", image.NewGray(image.Rect(0, 0, ui.MaxPictureSide+1, 1)), 0, 0, [4]byte{}, "PictureTooLarge"},
		{"one that is too high", image.NewGray(image.Rect(0, 0, 1, ui.MaxPictureSide+1)), 0, 0, [4]byte{}, "PictureTooLarge"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pic, err := ui.NewPicture(tc.img)
			if tc.wantError != "" {
				appErr, ok := errors.FromError(err)
				if !ok {
					t.Fatalf("expected *errors.Error, got %T", err)
				}
				if !appErr.HasRoute("NewPicture." + tc.wantError) {
					t.Errorf("unexpected failure path: %s", appErr.Route())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if pic.Width() != tc.w || pic.Height() != tc.h {
				t.Errorf("size %dx%d, want %dx%d", pic.Width(), pic.Height(), tc.w, tc.h)
			}
			if got := [4]byte(pic.Pixels()[:4]); got != tc.firstPix {
				t.Errorf("first pixel %v, want %v", got, tc.firstPix)
			}
		})
	}
}

func TestAPictureDoesNotShareThePixelsWithTheImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{R: 10, A: 255})
	pic, err := ui.NewPicture(img)
	if err != nil {
		t.Fatal(err)
	}
	img.SetRGBA(0, 0, color.RGBA{R: 99, A: 255})
	if pic.Pixels()[0] != 10 {
		t.Errorf("the picture changed with the image: %d", pic.Pixels()[0])
	}
}

func TestImageSizeKeepsTheProportions(t *testing.T) {
	inf := math.Inf(1)
	cases := []struct {
		name         string
		w, h         float64
		maxW, maxH   float64
		upscale      bool
		wantW, wantH float64
	}{
		{"smaller than the box, not scaled up", 100, 50, 400, 400, false, 100, 50},
		{"smaller than the box, scaled up", 100, 50, 400, 400, true, 400, 200},
		{"wider than the box", 400, 200, 100, 100, false, 100, 50},
		{"higher than the box", 200, 400, 100, 100, false, 50, 100},
		{"unbounded sides keep the size", 100, 50, inf, inf, true, 100, 50},
		{"one side bounded", 100, 50, 50, inf, false, 50, 25},
		{"one side bounded, upscale", 100, 50, inf, 100, true, 200, 100},
		{"no pixels", 0, 50, 100, 100, true, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, h := kit.ImageSize(tc.w, tc.h, tc.maxW, tc.maxH, tc.upscale)
			if w != tc.wantW || h != tc.wantH {
				t.Errorf("got %vx%v, want %vx%v", w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestTheImageWidgetFitsItsBox(t *testing.T) {
	pic, err := ui.NewPicture(gradient(200, 100))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name         string
		box          gui.Size
		opts         ui.ImageOpts
		wantW, wantH float64
	}{
		{"scaled down to the width", gui.Size{Width: 100, Height: 100}, ui.ImageOpts{}, 100, 50},
		{"left alone in a bigger box", gui.Size{Width: 300, Height: 300}, ui.ImageOpts{}, 200, 100},
		{"scaled up on request", gui.Size{Width: 300, Height: 300}, ui.ImageOpts{Upscale: true}, 300, 150},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := uitest.Pump(t, uitest.Themes()[0], tc.box, ui.NewImage(pic, tc.opts))
			size := ts.Size(gluttertest.ByType[*widgets.RawImage]())
			if size.Width != tc.wantW || size.Height != tc.wantH {
				t.Errorf("drawn at %vx%v, want %vx%v", size.Width, size.Height, tc.wantW, tc.wantH)
			}
			if got := ts.Widget(gluttertest.ByType[*widgets.RawImage]()).(*widgets.RawImage).Image(); got != pic {
				t.Error("the widget draws another picture")
			}
		})
	}
	// no picture, nothing drawn
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 50, Height: 50}, ui.NewImage(nil))
	if ts.Exists(gluttertest.ByType[*widgets.RawImage]()) {
		t.Error("a nil picture draws something")
	}
}

func TestImageGoldens(t *testing.T) {
	photo, err := ui.NewPicture(gradient(160, 100))
	if err != nil {
		t.Fatal(err)
	}
	squares, err := ui.NewPicture(checker(8, 8))
	if err != nil {
		t.Fatal(err)
	}
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			body := widgets.NewRow([]widgets.Widget{
				widgets.NewSizedBox(ui.NewImage(photo, ui.ImageOpts{Upscale: true}), widgets.SizedBoxOpts{Width: foundation.Some(200.0), Height: foundation.Some(200.0)}),
				widgets.NewSizedBox(ui.NewImage(squares, ui.ImageOpts{Upscale: true, Pixelated: true}), widgets.SizedBoxOpts{Width: foundation.Some(160.0), Height: foundation.Some(160.0)}),
			})
			ts := uitest.Pump(t, th, gui.Size{Width: 380, Height: 220}, body)
			uitest.MatchesGolden(ts, "image_"+th.Name)
		})
	}
}
