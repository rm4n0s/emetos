package keyqr

import (
	"bytes"
	"image"
	"image/jpeg"

	"github.com/rm4n0s/errors"
)

// The limits of a frame.
const (
	// MaxFrameWidth is the widest frame a camera may give, in pixels, and MaxFrameHeight the highest: a larger frame is refused.
	MaxFrameWidth  = 1920
	MaxFrameHeight = 1080
	// MaxFrameBytes is the largest buffer of a camera the layer maps, and the largest compressed frame it decodes.
	MaxFrameBytes = 16 << 20
)

// DecodeMJPEG turns one frame of an MJPEG camera into an image. Many cameras leave the Huffman tables out of their frames (the
// JPEG of a Motion-JPEG stream does not need them, and every decoder is expected to use the standard ones); they are put in
// first. The size of the frame is read from its header before the frame is decoded, so a frame that is too large costs no
// memory. It fails with KeyQRFrameInvalid for data that is not a JPEG, and with KeyQRFrameTooLarge for one larger than
// MaxFrameWidth by MaxFrameHeight.
func DecodeMJPEG(data []byte) (image.Image, error) {
	if len(data) == 0 || len(data) > MaxFrameBytes {
		return nil, errors.New("KeyQRFrameInvalid", "the frame is empty or larger than any camera frame", "bytes", len(data))
	}
	data = withHuffmanTables(data)
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.NewErr("KeyQRFrameInvalid", err)
	}
	if cfg.Width > MaxFrameWidth || cfg.Height > MaxFrameHeight || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errors.New("KeyQRFrameTooLarge", "the frame is larger than 1920x1080", "width", cfg.Width, "height", cfg.Height)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.NewErr("KeyQRFrameInvalid", err)
	}
	return img, nil
}

// YUYVToImage converts a frame in the packed 4:2:2 format YUYV (V4L2_PIX_FMT_YUYV: Y0 U Y1 V for every two pixels) into an
// image. stride is the number of bytes between two rows (the camera may pad them); the width must be even. The image has its
// own memory, so the buffer of the camera can be given back at once. It fails with KeyQRFrameInvalid for a size or data that do
// not fit, and with KeyQRFrameTooLarge for a frame larger than MaxFrameWidth by MaxFrameHeight.
func YUYVToImage(data []byte, width, height, stride int) (*image.YCbCr, error) {
	switch {
	case width <= 0 || height <= 0 || width%2 != 0 || stride < 2*width:
		return nil, errors.New("KeyQRFrameInvalid", "the size of a YUYV frame is not valid", "width", width, "height", height, "stride", stride)
	case width > MaxFrameWidth || height > MaxFrameHeight:
		return nil, errors.New("KeyQRFrameTooLarge", "the frame is larger than 1920x1080", "width", width, "height", height)
	case len(data) < stride*(height-1)+2*width:
		return nil, errors.New("KeyQRFrameInvalid", "the YUYV frame is shorter than its size", "bytes", len(data), "width", width, "height", height, "stride", stride)
	}
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio422)
	for y := 0; y < height; y++ {
		row := data[y*stride : y*stride+2*width]
		ys := img.Y[y*img.YStride : y*img.YStride+width]
		cb := img.Cb[y*img.CStride : y*img.CStride+width/2]
		cr := img.Cr[y*img.CStride : y*img.CStride+width/2]
		for x := 0; x < width/2; x++ {
			ys[2*x], cb[x], ys[2*x+1], cr[x] = row[4*x], row[4*x+1], row[4*x+2], row[4*x+3]
		}
	}
	return img, nil
}

// withHuffmanTables returns the JPEG with the standard Huffman tables inserted before its first scan when it has none; a JPEG
// that has a table, or that cannot be read as segments, is returned as it is (the decoder says what is wrong with it). The
// data is not changed.
func withHuffmanTables(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return data
	}
	i := 2
	for i+3 < len(data) {
		if data[i] != 0xFF {
			return data
		}
		marker := data[i+1]
		switch {
		case marker == 0xFF: // fill byte
			i++
			continue
		case marker == 0xC4: // DHT: the tables are there
			return data
		case marker == 0xDA: // SOS: the scan comes and there was no table
			out := make([]byte, 0, len(data)+len(standardDHT))
			out = append(out, data[:i]...)
			out = append(out, standardDHT...)
			return append(out, data[i:]...)
		case marker == 0x01 || (marker >= 0xD0 && marker <= 0xD9): // markers with no segment
			i += 2
			continue
		}
		n := int(data[i+2])<<8 | int(data[i+3])
		if n < 2 {
			return data
		}
		i += 2 + n
	}
	return data
}

// standardDHT is the DHT segment of the four Huffman tables of the JPEG standard (ITU T.81 annex K.3), the ones every Motion-JPEG
// frame without tables is coded with. The tables are those of Go's image/jpeg encoder (BSD-3-Clause, The Go Authors).
var standardDHT = buildStandardDHT()

func buildStandardDHT() []byte {
	type table struct {
		class byte // Tc and Th: class in the high nibble, destination in the low one
		count [16]byte
		value []byte
	}
	lumDC := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	tables := []table{
		{0x00, [16]byte{0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0}, lumDC},
		{0x10, [16]byte{0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0, 0, 1, 125}, []byte{
			0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12, 0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07,
			0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xa1, 0x08, 0x23, 0x42, 0xb1, 0xc1, 0x15, 0x52, 0xd1, 0xf0,
			0x24, 0x33, 0x62, 0x72, 0x82, 0x09, 0x0a, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x25, 0x26, 0x27, 0x28,
			0x29, 0x2a, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
			0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
			0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89,
			0x8a, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
			0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3, 0xc4, 0xc5,
			0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xe1, 0xe2,
			0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
			0xf9, 0xfa,
		}},
		{0x01, [16]byte{0, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0}, lumDC},
		{0x11, [16]byte{0, 2, 1, 2, 4, 4, 3, 4, 7, 5, 4, 4, 0, 1, 2, 119}, []byte{
			0x00, 0x01, 0x02, 0x03, 0x11, 0x04, 0x05, 0x21, 0x31, 0x06, 0x12, 0x41, 0x51, 0x07, 0x61, 0x71,
			0x13, 0x22, 0x32, 0x81, 0x08, 0x14, 0x42, 0x91, 0xa1, 0xb1, 0xc1, 0x09, 0x23, 0x33, 0x52, 0xf0,
			0x15, 0x62, 0x72, 0xd1, 0x0a, 0x16, 0x24, 0x34, 0xe1, 0x25, 0xf1, 0x17, 0x18, 0x19, 0x1a, 0x26,
			0x27, 0x28, 0x29, 0x2a, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
			0x49, 0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
			0x69, 0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
			0x88, 0x89, 0x8a, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5,
			0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3,
			0xc4, 0xc5, 0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda,
			0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
			0xf9, 0xfa,
		}},
	}
	body := []byte{}
	for _, t := range tables {
		body = append(body, t.class)
		body = append(body, t.count[:]...)
		body = append(body, t.value...)
	}
	n := len(body) + 2
	return append([]byte{0xFF, 0xC4, byte(n >> 8), byte(n)}, body...)
}
