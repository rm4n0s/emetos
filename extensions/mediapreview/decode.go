package mediapreview

import (
	"bytes"
	"context"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/svg"
	"github.com/rm4n0s/glutter/ui"

	// TIFF registers itself with the image package, so Glutter's codec reads it like the formats it knows (SPEC §10.37).
	_ "golang.org/x/image/tiff"
)

// MaxFileSize is the largest file the preview reads (SPEC §10.37): a larger one is not decoded.
const MaxFileSize = 256 << 20

// maxPixels and maxSide are the limits of Glutter's codec (ui.InstantiateImageCodec): a larger image is not decoded.
const (
	maxPixels = 1 << 26
	maxSide   = 1 << 15
)

// Image is a decoded image: its frames (one for a still image, every frame of an animated GIF), or an SVG document, which is drawn
// at the size it is shown so it stays sharp at every zoom.
type Image struct {
	// Width and Height are the size of the image in pixels (an SVG's own size, rounded).
	Width, Height int
	// Frames are the pictures of a bitmap image, with the time each is shown; nil for an SVG.
	Frames []Frame
	// Repeat is how many times an animation repeats after the first time: -1 forever (Glutter's RepetitionCount).
	Repeat int
	// SVG is the document of an SVG image, and Source its bytes, which Glutter's SvgPicture draws.
	SVG    *svg.Document
	Source []byte
	// Format is the format's name, for the errors: png, jpeg, gif, bmp, webp, tiff, ico, svg.
	Format string
}

// Frame is one picture of an image.
type Frame struct {
	Image    *ui.Image
	Duration time.Duration
}

// Size is the size of the image as text, as VS Code's status bar entry writes it: 1920x1080.
func (img *Image) Size() string { return strconv.Itoa(img.Width) + "x" + strconv.Itoa(img.Height) }

// Decode reads and decodes the image at path; it runs off the UI thread. It fails with ImageFileTooLarge for a file over MaxFileSize,
// ImageTooLarge for an image over Glutter's pixel limit, ImageReadFailed when the file cannot be read and ImageDecodeFailed when it is
// not an image of its format.
func Decode(ctx context.Context, path string) (*Image, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, errors.NewErr("ImageReadFailed", err).SetMetadata("path", path)
	}
	if info.Size() > MaxFileSize {
		return nil, errors.New("ImageFileTooLarge", "This image file is too large to show ("+formatSize(info.Size())+").", "path", path, "size", info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.NewErr("ImageReadFailed", err).SetMetadata("path", path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".svg":
		doc, err := svg.Parse(data)
		if err != nil {
			return nil, errors.NewErr("ImageDecodeFailed", err).SetMetadata("path", path, "format", "svg")
		}
		size := doc.Size()
		return &Image{Width: int(size.Width + 0.5), Height: int(size.Height + 0.5), SVG: doc, Source: data, Format: "svg"}, nil
	case ".ico":
		return decodeICO(ctx, path, data)
	}
	return decodeBitmap(ctx, path, data)
}

// decodeBitmap decodes the formats of Glutter's codec, and TIFF.
func decodeBitmap(ctx context.Context, path string, data []byte) (*Image, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.NewErr("ImageDecodeFailed", err).SetMetadata("path", path)
	}
	if cfg.Width > maxSide || cfg.Height > maxSide || cfg.Width*cfg.Height > maxPixels {
		return nil, errors.New("ImageTooLarge", "This image is too large to show ("+strconv.Itoa(cfg.Width)+" x "+strconv.Itoa(cfg.Height)+").",
			"path", path, "width", cfg.Width, "height", cfg.Height, "format", format)
	}
	codec, err := ui.InstantiateImageCodec(ctx, data)
	if err != nil {
		return nil, errors.NewErr("ImageDecodeFailed", err).SetMetadata("path", path, "format", format)
	}
	defer codec.Dispose()
	img := &Image{Width: cfg.Width, Height: cfg.Height, Repeat: codec.RepetitionCount(), Format: format}
	for range codec.FrameCount() {
		f, err := codec.GetNextFrame(ctx)
		if err != nil {
			return nil, errors.NewErr("ImageDecodeFailed", err).SetMetadata("path", path, "format", format)
		}
		img.Frames = append(img.Frames, Frame{Image: f.Image, Duration: f.Duration})
	}
	return img, nil
}
