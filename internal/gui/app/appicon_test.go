package app

import (
	"bytes"
	"image/png"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/assets/icons"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// opaquePixels counts the pixels of a window icon picture that are not transparent, and the ones that are white (the
// mark).
func opaquePixels(img ui.WindowIconImage) (opaque, white int) {
	for i := 0; i < len(img.RGBA); i += 4 {
		if img.RGBA[i+3] > 0 {
			opaque++
		}
		if img.RGBA[i+3] == 255 && img.RGBA[i] > 240 && img.RGBA[i+1] > 240 && img.RGBA[i+2] > 240 {
			white++
		}
	}
	return opaque, white
}

func TestTheIconsOfTheTrayAndTheWindowAreTheEmbeddedPictures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		make  func() (ui.WindowIcon, error)
		sizes []int
	}{
		{"tray", trayIcon, []int{16, 22, 24, 32, 48}},
		{"window", windowIcon, []int{16, 32, 48, 64, 128, 256}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			icon, err := tc.make()
			if err != nil {
				t.Fatal(err)
			}
			if err := icon.Validate(); err != nil {
				t.Fatal(err)
			}
			var sizes []int
			for _, img := range icon.Images {
				sizes = append(sizes, img.Size)
				opaque, white := opaquePixels(img)
				if opaque < img.Size*img.Size/2 || white == 0 {
					t.Errorf("the picture of %d px has %d opaque and %d white pixels", img.Size, opaque, white)
				}
			}
			if !slices.Equal(sizes, tc.sizes) {
				t.Errorf("sizes %v, want %v", sizes, tc.sizes)
			}
		})
	}
}

func TestEveryPictureOfTheIconIsThere(t *testing.T) {
	for _, size := range kit.AppIconSizes {
		data, ok := icons.PNG(size)
		if !ok {
			t.Errorf("no picture of %d px", size)
			continue
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Errorf("%d px: %v", size, err)
			continue
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Errorf("the picture of %d px is %dx%d", size, b.Dx(), b.Dy())
		}
	}
	if _, ok := icons.PNG(17); ok {
		t.Error("there is a picture of 17 px")
	}
}

// TestTheTrayCanScaleTheBiggestPictureToo shows the picture of the largest size is one Glutter's TrayIconFromRGBA takes,
// should a desktop ever need the tray's pictures made from one.
func TestTheTrayCanScaleTheBiggestPictureToo(t *testing.T) {
	icon, err := iconFromPNGs(icons.PNG, []int{512})
	if err != nil {
		t.Fatal(err)
	}
	scaled, err := desktop.TrayIconFromRGBA(512, icon.Images[0].RGBA)
	if err != nil {
		t.Fatal(err)
	}
	if len(scaled.Images) != len(trayIconSizes) {
		t.Errorf("%d pictures, want %d", len(scaled.Images), len(trayIconSizes))
	}
	for _, img := range scaled.Images {
		if opaque, white := opaquePixels(img); opaque == 0 || white == 0 {
			t.Errorf("the scaled picture of %d px is empty (%d opaque, %d white)", img.Size, opaque, white)
		}
	}
}

func TestTheStartupWindowHasTheIcon(t *testing.T) {
	win := newTestWorkspace(t, "")
	cfg := startupWindowConfig(win, nil)
	if err := cfg.Icon.Validate(); err != nil || len(cfg.Icon.Images) != len(windowIconSizes) {
		t.Errorf("the window icon has %d pictures (%v)", len(cfg.Icon.Images), err)
	}
}

func TestAnIconThatCannotBeMade(t *testing.T) {
	good, _ := icons.PNG(16)
	other, _ := icons.PNG(32)
	for _, tc := range []struct {
		name  string
		get   func(int) ([]byte, bool)
		route string
	}{
		{"a size with no picture", func(int) ([]byte, bool) { return nil, false }, "iconFromPNGs.AppIconMissing"},
		{"a picture that is not a PNG", func(int) ([]byte, bool) { return []byte("not a picture"), true }, "iconFromPNGs.AppIconInvalid"},
		{"a picture of another size", func(int) ([]byte, bool) { return other, true }, "iconFromPNGs.AppIconInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := iconFromPNGs(tc.get, []int{16})
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	// the picture of the right size is fine
	icon, err := iconFromPNGs(func(int) ([]byte, bool) { return good, true }, []int{16})
	if err != nil || icon.Images[0].Size != 16 {
		t.Fatalf("%v %v", icon, err)
	}
}
