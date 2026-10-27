package keyqr

import (
	"image"
	"image/color"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/makiuchi-d/gozxing/qrcode/decoder"
	"github.com/rm4n0s/errors"
	xdraw "golang.org/x/image/draw"

	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// MaxQRBytes is how many bytes a QR code holds at most (version 40, error correction L, byte mode).
const MaxQRBytes = 2953

// ParseKey reads the text of a QR code as the line of a public key (the contents of an id_ed25519.pub: type, base64 and an
// optional comment), under the rules of the login (sshlogin.ParseAuthorizedKey): no options, no certificates, RSA of at least
// 2048 bits. Text that is not such a line is refused with the tags of that function, SSHLoginKeyLineInvalid and
// SSHLoginKeyUnsupported; the caller says "not a public key".
func ParseKey(text string) (sshlogin.AllowedKey, error) {
	return sshlogin.ParseAuthorizedKey(text)
}

// decodeScales are the sizes DecodeQR looks at a picture in, as fractions of its own: the detector of the QR code finds the
// same code at one size and not at another (the modules of a large code fall between pixels), so a picture where nothing was
// found is looked at again, smaller.
var decodeScales = [...][2]int{{1, 1}, {2, 3}, {1, 2}}

// DecodeQR reads the QR code in an image and returns its text. It looks in the whole image and tries harder than a quick
// scan, in any orientation, so a code that is turned, small or a little blurred is found; when nothing is found it looks again
// at the image reduced to two thirds and to a half. It fails with KeyQRNoCode when there is no readable code (the normal
// answer for most frames of a camera). It takes time in proportion to the size of the image (20 ms for noise of 1920x1080, the
// largest frame) and has no way to be interrupted: the caller bounds the size.
func DecodeQR(img image.Image) (string, error) {
	lum, w, h, stride := luminance(img)
	if w == 0 || h == 0 {
		return "", errors.New("KeyQRNoCode", "the image is empty")
	}
	var last error
	for i, sc := range decodeScales {
		pix, pw, ph, ps := lum, w, h, stride
		if i > 0 {
			if w*sc[0]/sc[1] < 120 || h*sc[0]/sc[1] < 120 {
				break // too small to hold a code any more
			}
			pix, pw, ph, ps = reduced(lum, w, h, stride, sc[0], sc[1])
		}
		text, err := decodeLuminance(pix, pw, ph, ps)
		if err == nil {
			return text, nil
		}
		last = err
	}
	return "", last
}

// reduced scales the brightness of a picture by num/den (smaller) with a bilinear filter.
func reduced(pix []byte, w, h, stride, num, den int) ([]byte, int, int, int) {
	src := &image.Gray{Pix: pix, Stride: stride, Rect: image.Rect(0, 0, w, h)}
	dst := image.NewGray(image.Rect(0, 0, w*num/den, h*num/den))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst.Pix, dst.Rect.Dx(), dst.Rect.Dy(), dst.Stride
}

// decodeLuminance looks for a QR code in the brightness of a picture.
func decodeLuminance(lum []byte, w, h, stride int) (string, error) {
	src, err := gozxing.NewPlanarYUVLuminanceSource(lum, stride, h, 0, 0, w, h, false)
	if err != nil {
		return "", errors.NewErr("KeyQRNoCode", err)
	}
	bitmap, err := gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(src))
	if err != nil {
		return "", errors.NewErr("KeyQRNoCode", err)
	}
	hints := map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER:       true,
		gozxing.DecodeHintType_POSSIBLE_FORMATS: []gozxing.BarcodeFormat{gozxing.BarcodeFormat_QR_CODE},
	}
	result, err := qrcode.NewQRCodeReader().Decode(bitmap, hints)
	if err != nil {
		return "", errors.NewErr("KeyQRNoCode", err)
	}
	return result.GetText(), nil
}

// luminance gives the brightness of every pixel of an image as bytes, row after row, and the width, the height and the number
// of bytes between two rows. The image of a camera (YCbCr, the Y plane is the brightness) and a gray image are used as they are.
func luminance(img image.Image) (pix []byte, w, h, stride int) {
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, 0, 0, 0
	}
	switch im := img.(type) {
	case *image.YCbCr:
		return im.Y[im.YOffset(b.Min.X, b.Min.Y):], w, h, im.YStride
	case *image.Gray:
		return im.Pix[im.PixOffset(b.Min.X, b.Min.Y):], w, h, im.Stride
	}
	pix = make([]byte, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = color.GrayModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.Gray).Y
		}
	}
	return pix, w, h, w
}

// EncodeQR makes the image of a QR code that holds the text: black modules on white with the quiet zone of 4 modules the
// standard asks for, each module a square of whole pixels so that the edges stay sharp, as large as keeps the side of the image
// within maxSide pixels (a code of more modules than maxSide has a pixel for each). The error correction is level M (it survives 15% of damage) and, when the text does not fit at that level, L
// (7%). It fails with KeyQREncodeFailed for empty text and for text that does not fit in a QR code at level L (MaxQRBytes).
func EncodeQR(text string, maxSide int) (*image.Gray, error) {
	img, _, _, err := encodeQR(text, maxSide)
	return img, err
}

// encodeQR is EncodeQR that also says the error correction level it used and the number of modules a side has (the quiet zone
// included), which the tests check.
func encodeQR(text string, maxSide int) (*image.Gray, decoder.ErrorCorrectionLevel, int, error) {
	var matrix *gozxing.BitMatrix
	var err error
	var used decoder.ErrorCorrectionLevel
	hints := map[gozxing.EncodeHintType]interface{}{gozxing.EncodeHintType_ERROR_CORRECTION: decoder.ErrorCorrectionLevel_M}
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 { // a comment in another script: say it is UTF-8 (plain ASCII needs no mark, and a mark costs bits)
			hints[gozxing.EncodeHintType_CHARACTER_SET] = "UTF-8"
			break
		}
	}
	for _, level := range []decoder.ErrorCorrectionLevel{decoder.ErrorCorrectionLevel_M, decoder.ErrorCorrectionLevel_L} {
		// a width of 0 asks for the matrix of modules with its quiet zone, one pixel for a module
		hints[gozxing.EncodeHintType_ERROR_CORRECTION] = level
		matrix, err = qrcode.NewQRCodeWriter().Encode(text, gozxing.BarcodeFormat_QR_CODE, 0, 0, hints)
		if err == nil {
			used = level
			break
		}
	}
	if err != nil {
		return nil, 0, 0, errors.NewErr("KeyQREncodeFailed", err).SetMetadata("bytes", len(text))
	}
	modules := matrix.GetWidth()
	scale := max(1, maxSide/modules)
	img := image.NewGray(image.Rect(0, 0, modules*scale, modules*scale))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	for my := 0; my < modules; my++ {
		for mx := 0; mx < modules; mx++ {
			if !matrix.Get(mx, my) {
				continue
			}
			for y := my * scale; y < (my+1)*scale; y++ {
				row := img.Pix[y*img.Stride+mx*scale : y*img.Stride+(mx+1)*scale]
				for i := range row {
					row[i] = 0
				}
			}
		}
	}
	return img, used, modules, nil
}

// EncodeKey makes the QR code of a public key: the line of the key (type, base64 and the comment when it has one) as
// sshlogin.AllowedKey.Line writes it, which is what the other computer's Scan QR Code reads. Only a public key can be given:
// it takes the line of a key and parses it under the rules of the login first, so text that is not a public key (a private key
// among it) is refused with the tags of ParseKey and never drawn. It fails with KeyQREncodeFailed like EncodeQR.
func EncodeKey(line string, maxSide int) (*image.Gray, sshlogin.AllowedKey, error) {
	key, err := ParseKey(line)
	if err != nil {
		return nil, sshlogin.AllowedKey{}, err
	}
	img, err := EncodeQR(key.Line(), maxSide)
	if err != nil {
		return nil, sshlogin.AllowedKey{}, err
	}
	return img, key, nil
}
