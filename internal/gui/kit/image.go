package kit

import (
	"context"
	"image"
	"image/draw"
	"math"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// MaxPictureSide is the largest width or height, in pixels, of an image NewPicture takes.
const MaxPictureSide = 4096

// Picture is an image that is ready to be drawn: its pixels are copied and owned by the Picture, so the image it was made from
// may be changed or dropped. Make it once and keep it; making one copies every pixel. It is Glutter's image, and holds no
// resource that needs closing.
type Picture = gui.Image

// NewPicture copies the pixels of an image into a Picture. It may be called from any goroutine, so that a goroutine that gets
// pictures (the frames of a camera) makes the Picture before it hands it to the UI thread. It fails with PictureInvalid for a
// nil image or one with no pixels, and with PictureTooLarge for one with a side over MaxPictureSide.
func NewPicture(img image.Image) (*Picture, error) {
	if img == nil || img.Bounds().Empty() {
		return nil, errors.New("PictureInvalid", "there is no image to draw")
	}
	b := img.Bounds()
	if b.Dx() > MaxPictureSide || b.Dy() > MaxPictureSide {
		return nil, errors.New("PictureTooLarge", "the image is too large to be drawn", "width", b.Dx(), "height", b.Dy(), "max", MaxPictureSide)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok || rgba.Rect.Min != (image.Point{}) {
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	}
	pic, err := gui.DecodeImageFromPixels(context.Background(), rgba.Pix, rgba.Rect.Dx(), rgba.Rect.Dy(), gui.PixelFormatRGBA8888, gui.DecodeImageFromPixelsOpts{RowBytes: rgba.Stride})
	if err != nil {
		return nil, errors.NewErr("PictureInvalid", err)
	}
	return pic, nil
}

// ImageOpts are the options of NewImage.
type ImageOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Upscale lets the picture be drawn larger than its own size to fill the space it is given; without it a picture is only
	// scaled down to fit.
	Upscale bool
	// Pixelated draws the pixels as squares when the picture is scaled up (a QR code, a pixel-art icon); without it the pixels
	// are blended smoothly (a photograph, the picture of a camera).
	Pixelated bool
}

// Image draws a Picture scaled to fit the space it is given, keeping its proportions. The space is the box the constraints give;
// where a side is not bounded the picture keeps its own size on that side. In a box of a fixed size the picture is centered in
// it; where the box is only a limit (loose constraints) the widget is as large as the picture is drawn.
type Image struct {
	widgets.StatelessWidget
	pic  *Picture
	opts ImageOpts
}

// NewImage makes a widget that draws a picture. A nil picture draws nothing and takes no space.
func NewImage(pic *Picture, opts ...ImageOpts) *Image {
	img := &Image{pic: pic}
	if len(opts) > 0 {
		img.opts = opts[0]
	}
	img.StatelessWidget = widgets.NewStatelessWidgetBase(widgets.WidgetOpts{Key: img.opts.Key})
	return img
}

// Picture is the picture the widget draws.
func (i *Image) Picture() *Picture { return i.pic }

// ImageSize is the size a picture of w by h pixels is drawn at in a box of at most maxW by maxH logical pixels (an infinite
// side is not a limit): the largest size that keeps its proportions and fits, and without upscale never larger than the
// picture's own.
func ImageSize(w, h, maxW, maxH float64, upscale bool) (float64, float64) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	scale := math.Inf(1)
	if !math.IsInf(maxW, 1) {
		scale = min(scale, maxW/w)
	}
	if !math.IsInf(maxH, 1) {
		scale = min(scale, maxH/h)
	}
	if math.IsInf(scale, 1) || (!upscale && scale > 1) {
		scale = 1
	}
	return w * scale, h * scale
}

// Build implements widgets.StatelessWidget.
func (i *Image) Build(widgets.BuildContext) widgets.Widget {
	if i.pic == nil {
		return widgets.NewSizedBox(nil)
	}
	pic, opts := i.pic, i.opts
	quality := gui.FilterQualityLow
	if opts.Pixelated {
		quality = gui.FilterQualityNone
	}
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		w, h := ImageSize(float64(pic.Width()), float64(pic.Height()), c.MaxWidth, c.MaxHeight, opts.Upscale)
		return widgets.NewAlign(widgets.NewSizedBox(
			widgets.NewRawImage(pic, widgets.RawImageOpts{Scale: 1, Fit: painting.BoxFitFill, FilterQuality: quality}),
			widgets.SizedBoxOpts{Width: foundation.Some(w), Height: foundation.Some(h)}),
			widgets.AlignOpts{Alignment: painting.AlignmentCenter, WidthFactor: foundation.Some(1.0), HeightFactor: foundation.Some(1.0)})
	})
}
