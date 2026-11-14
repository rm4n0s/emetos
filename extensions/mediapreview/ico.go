package mediapreview

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"strconv"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"
)

// The ICO reader (SPEC §10.37): the directory of the file, and its largest image, which is a PNG file (read by Glutter's codec) or a
// DIB, a BMP without its file header whose height counts the colors and the AND mask (read here). Cursors (CUR, type 2) are read the
// same way.

// icoEntry is an entry of the directory of an ICO file.
type icoEntry struct {
	width, height int
	bitCount      int
	size, offset  uint32
}

// decodeICO decodes the largest image of an ICO file. It fails with ImageDecodeFailed for a file that is not an ICO file or whose
// image is damaged.
func decodeICO(ctx context.Context, path string, data []byte) (*Image, error) {
	entry, err := largestICOEntry(data)
	if err != nil {
		return nil, errors.NewErr("ImageDecodeFailed", err).SetMetadata("path", path, "format", "ico")
	}
	body := data[entry.offset : entry.offset+entry.size]
	if bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		img, err := decodeBitmap(ctx, path, body)
		if err != nil {
			return nil, err
		}
		img.Format = "ico"
		return img, nil
	}
	rgba, err := decodeDIB(body)
	if err != nil {
		return nil, errors.NewErr("ImageDecodeFailed", err).SetMetadata("path", path, "format", "ico")
	}
	w, h := rgba.Rect.Dx(), rgba.Rect.Dy()
	return &Image{Width: w, Height: h, Format: "ico", Frames: []Frame{{Image: ui.NewImageFromRGBA(rgba.Pix, w, h)}}}, nil
}

// icoError is the error of a damaged ICO file; decodeICO wraps it with its tag.
type icoError string

func (e icoError) Error() string { return string(e) }

// largestICOEntry reads the directory of an ICO file and gives its largest image (by area, then by bits per pixel).
func largestICOEntry(data []byte) (icoEntry, error) {
	if len(data) < 6 {
		return icoEntry{}, icoError("the file is too short for an ICO header")
	}
	reserved, kind, count := binary.LittleEndian.Uint16(data), binary.LittleEndian.Uint16(data[2:]), int(binary.LittleEndian.Uint16(data[4:]))
	if reserved != 0 || (kind != 1 && kind != 2) || count == 0 {
		return icoEntry{}, icoError("the file is not an ICO file")
	}
	if len(data) < 6+16*count {
		return icoEntry{}, icoError("the directory of the ICO file is cut short")
	}
	var best icoEntry
	found := false
	for i := range count {
		e := data[6+16*i:]
		entry := icoEntry{width: int(e[0]), height: int(e[1]), bitCount: int(binary.LittleEndian.Uint16(e[6:])),
			size: binary.LittleEndian.Uint32(e[8:]), offset: binary.LittleEndian.Uint32(e[12:])}
		if entry.width == 0 {
			entry.width = 256
		}
		if entry.height == 0 {
			entry.height = 256
		}
		if uint64(entry.offset)+uint64(entry.size) > uint64(len(data)) || entry.size == 0 {
			continue
		}
		area, bestArea := entry.width*entry.height, best.width*best.height
		if !found || area > bestArea || (area == bestArea && entry.bitCount > best.bitCount) {
			best, found = entry, true
		}
	}
	if !found {
		return icoEntry{}, icoError("no image of the ICO file is inside the file")
	}
	return best, nil
}

// decodeDIB decodes the DIB of an ICO entry: a BITMAPINFOHEADER, the palette for 8 bits per pixel and fewer, the colors bottom-up
// and the AND mask (1 bit per pixel, set where the image is transparent). 32-bit images have their own alpha; the mask is used when
// all of it is zero. The result is premultiplied.
func decodeDIB(b []byte) (*image.RGBA, error) {
	if len(b) < 40 {
		return nil, icoError("the bitmap header is cut short")
	}
	headerSize := int(binary.LittleEndian.Uint32(b))
	w := int(int32(binary.LittleEndian.Uint32(b[4:])))
	h := int(int32(binary.LittleEndian.Uint32(b[8:]))) / 2 // the height counts the colors and the mask
	bpp := int(binary.LittleEndian.Uint16(b[14:]))
	compression := binary.LittleEndian.Uint32(b[16:])
	colorsUsed := int(binary.LittleEndian.Uint32(b[32:]))
	if headerSize < 40 || headerSize > len(b) || w <= 0 || h <= 0 || w > 256*16 || h > 256*16 {
		return nil, icoError("the bitmap header is not valid (" + strconv.Itoa(w) + " x " + strconv.Itoa(h) + ")")
	}
	if compression != 0 && (compression != 3 || bpp != 32) {
		return nil, icoError("the bitmap is compressed (" + strconv.Itoa(int(compression)) + ")")
	}
	var palette []color.NRGBA
	pos := headerSize
	if bpp <= 8 {
		n := colorsUsed
		if n == 0 {
			n = 1 << bpp
		}
		if pos+4*n > len(b) {
			return nil, icoError("the palette of the bitmap is cut short")
		}
		for i := range n {
			p := b[pos+4*i:]
			palette = append(palette, color.NRGBA{R: p[2], G: p[1], B: p[0], A: 0xff})
		}
		pos += 4 * n
	} else if compression == 3 {
		pos = max(pos, 40+12) // the three masks of BI_BITFIELDS follow a 40-byte header
	}
	switch bpp {
	case 1, 4, 8, 24, 32:
	default:
		return nil, icoError("the bitmap has " + strconv.Itoa(bpp) + " bits per pixel")
	}
	stride := (w*bpp + 31) / 32 * 4
	maskStride := (w + 31) / 32 * 4
	if pos+stride*h > len(b) {
		return nil, icoError("the pixels of the bitmap are cut short")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	anyAlpha := false
	for y := range h {
		row := b[pos+(h-1-y)*stride:]
		for x := range w {
			var c color.NRGBA
			switch bpp {
			case 32:
				c = color.NRGBA{R: row[4*x+2], G: row[4*x+1], B: row[4*x], A: row[4*x+3]}
				anyAlpha = anyAlpha || c.A != 0
			case 24:
				c = color.NRGBA{R: row[3*x+2], G: row[3*x+1], B: row[3*x], A: 0xff}
			default:
				perByte := 8 / bpp
				v := int(row[x/perByte]>>(8-bpp*(x%perByte+1))) & (1<<bpp - 1)
				if v < len(palette) {
					c = palette[v]
				}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	maskPos := pos + stride*h
	hasMask := maskPos+maskStride*h <= len(b)
	if hasMask && (bpp != 32 || !anyAlpha) {
		for y := range h {
			row := b[maskPos+(h-1-y)*maskStride:]
			for x := range w {
				c := img.NRGBAAt(x, y)
				if row[x/8]&(0x80>>(x%8)) != 0 {
					c.A = 0
				} else {
					c.A = 0xff
				}
				img.SetNRGBA(x, y, c)
			}
		}
	} else if bpp == 32 && !anyAlpha {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff
		}
	}
	out := image.NewRGBA(img.Rect)
	draw.Draw(out, out.Rect, img, image.Point{}, draw.Src) // premultiplies
	return out, nil
}
