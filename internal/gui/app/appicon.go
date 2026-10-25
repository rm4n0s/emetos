package app

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/assets/icons"
)

// The application icon in the places that take pictures (SPEC §10.14, §17): the window and the system tray. The
// pictures are the PNG files of assets/icons, which the test of internal/gui/kit renders from the drawing of
// kit.AppIconPainter, embedded in the program.

// The sizes of the pictures of each use. The tray gets the five sizes that Glutter's TrayIconFromRGBA makes (the
// desktop picks the one that fits its panel); the window gets the sizes a task bar, an Alt+Tab list and a title need.
var (
	trayIconSizes   = []int{16, 22, 24, 32, 48}
	windowIconSizes = []int{16, 32, 48, 64, 128, 256}
)

// iconFromPNGs makes a window icon from the pictures of the sizes; get gives the PNG file of a size. It fails with the
// tag AppIconMissing for a size that has no picture and AppIconInvalid for a picture that is not a PNG of that size.
func iconFromPNGs(get func(size int) ([]byte, bool), sizes []int) (ui.WindowIcon, error) {
	var icon ui.WindowIcon
	for _, size := range sizes {
		data, ok := get(size)
		if !ok {
			return ui.WindowIcon{}, errors.New("AppIconMissing", "the application icon has no picture of this size", "size", size)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return ui.WindowIcon{}, errors.NewErr("AppIconInvalid", err).SetMetadata("size", size)
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			return ui.WindowIcon{}, errors.New("AppIconInvalid", "the picture of the application icon has another size",
				"size", size, "width", b.Dx(), "height", b.Dy())
		}
		// the pixels are not premultiplied by the alpha, as the pictures of a window icon are
		rgba := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
		icon.Images = append(icon.Images, ui.WindowIconImage{Size: size, Scale: 1, RGBA: rgba.Pix})
	}
	return icon, nil
}

// trayIcon is the application icon for the system tray.
func trayIcon() (ui.WindowIcon, error) { return iconFromPNGs(icons.PNG, trayIconSizes) }

// windowIcon is the application icon for the window.
func windowIcon() (ui.WindowIcon, error) { return iconFromPNGs(icons.PNG, windowIconSizes) }
