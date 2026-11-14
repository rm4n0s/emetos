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

	"github.com/rm4n0s/emetos/extensions/internal/ffmpeg"

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

// DecodeOptions are the optional parts of Decode.
type DecodeOptions struct {
	// Video finds FFmpeg and probes the file, for the formats only FFmpeg reads (AVIF, HEIC, HEIF): the error of finding FFmpeg (or of
	// the probe) is the error of Decode. Nil leaves those formats unreadable.
	Video func(ctx context.Context) (ffmpeg.Info, media, error)
}

// ffmpegFormats are the formats read through one frame of FFmpeg.
var ffmpegFormats = map[string]string{".avif": "avif", ".heic": "heic", ".heif": "heif"}

// Decode reads and decodes the image at path; it runs off the UI thread. It fails with ImageFileTooLarge for a file over MaxFileSize,
// ImageTooLarge for an image over Glutter's pixel limit, ImageReadFailed when the file cannot be read and ImageDecodeFailed when it is
// not an image of its format; an AVIF, HEIC or HEIF image fails with the error of finding FFmpeg when it is missing, and
// ImageNeedsFFmpeg when no Video is given.
func Decode(ctx context.Context, path string, opts ...DecodeOptions) (*Image, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, errors.NewErr("ImageReadFailed", err).SetMetadata("path", path)
	}
	if info.Size() > MaxFileSize {
		return nil, errors.New("ImageFileTooLarge", "This image file is too large to show ("+formatSize(info.Size())+").", "path", path, "size", info.Size())
	}
	ext := strings.ToLower(filepath.Ext(path))
	if format, ok := ffmpegFormats[ext]; ok {
		var o DecodeOptions
		if len(opts) > 0 {
			o = opts[0]
		}
		if o.Video == nil {
			return nil, errors.New("ImageNeedsFFmpeg", "Opening "+strings.ToUpper(format)+" images needs FFmpeg.", "path", path)
		}
		return decodeWithFFmpeg(ctx, path, format, o.Video)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.NewErr("ImageReadFailed", err).SetMetadata("path", path)
	}
	switch ext {
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

// decodeWithFFmpeg decodes one frame of the image through FFmpeg, at its own size, premultiplied.
func decodeWithFFmpeg(ctx context.Context, path, format string, video func(context.Context) (ffmpeg.Info, media, error)) (*Image, error) {
	info, m, err := video(ctx)
	if err != nil {
		return nil, err
	}
	w, h := info.Width, info.Height
	if !info.HasVideo || w <= 0 || h <= 0 {
		return nil, errors.New("ImageDecodeFailed", "FFmpeg finds no picture in the file", "path", path, "format", format)
	}
	if w > maxSide || h > maxSide || w*h > maxPixels {
		return nil, errors.New("ImageTooLarge", "This image is too large to show ("+strconv.Itoa(w)+" x "+strconv.Itoa(h)+").",
			"path", path, "width", w, "height", h, "format", format)
	}
	src, err := m.Frames(ctx, ffmpeg.FrameOptions{Width: w, Height: h, Rate: 1})
	if err != nil {
		return nil, err
	}
	buf := make([]byte, src.FrameSize())
	ok, err := src.Next(buf)
	if cerr := src.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("ImageDecodeFailed", "FFmpeg gave no picture of the file", "path", path, "format", format)
	}
	// FFmpeg's RGBA has straight alpha; Glutter's images are premultiplied
	for i := 0; i+3 < len(buf); i += 4 {
		if a := uint32(buf[i+3]); a < 255 {
			buf[i] = byte(uint32(buf[i]) * a / 255)
			buf[i+1] = byte(uint32(buf[i+1]) * a / 255)
			buf[i+2] = byte(uint32(buf[i+2]) * a / 255)
		}
	}
	return &Image{Width: w, Height: h, Format: format, Frames: []Frame{{Image: ui.NewImageFromRGBA(buf, w, h)}}}, nil
}
