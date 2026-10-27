package keyqr

import (
	"image"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/makiuchi-d/gozxing/qrcode/decoder"
	"github.com/rm4n0s/errors"
)

func TestEveryKindOfKeyRoundTripsThroughItsQRCode(t *testing.T) {
	// each key: the line is encoded, the picture decoded, and the text parsed as the same key
	wantLevel := map[string]decoder.ErrorCorrectionLevel{"ed25519": decoder.ErrorCorrectionLevel_M, "ecdsa": decoder.ErrorCorrectionLevel_M, "rsa-4096": decoder.ErrorCorrectionLevel_M}
	for name, line := range testLines(t) {
		t.Run(name, func(t *testing.T) {
			key, err := ParseKey(line)
			if err != nil {
				t.Fatal(err)
			}
			img, level, modules, err := encodeQR(key.Line(), 400)
			if err != nil {
				t.Fatalf("the key does not fit in a QR code: %v", err)
			}
			t.Logf("%s: a line of %d bytes is %d modules a side (with the quiet zone), level %v, %d pixels", name, len(key.Line()), modules, level, img.Bounds().Dx())
			if level != wantLevel[name] {
				t.Errorf("error correction level %v, want %v", level, wantLevel[name])
			}
			if side := img.Bounds().Dx(); side > 400 || side <= 400-modules || side != img.Bounds().Dy() || side%modules != 0 {
				t.Errorf("the picture is %v: not a square of whole pixels for a module, the largest of at most 400", img.Bounds())
			}
			// the quiet zone: 4 modules of white all around
			scale := img.Bounds().Dx() / modules
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					if (x < 4*scale || y < 4*scale || x >= (modules-4)*scale || y >= (modules-4)*scale) && img.GrayAt(x, y).Y != 255 {
						t.Fatalf("pixel %d,%d of the quiet zone is not white", x, y)
					}
				}
			}
			text, err := DecodeQR(img)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseKey(text)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(key.Key) || got.Comment != key.Comment || got.Fingerprint() != key.Fingerprint() {
				t.Errorf("the key that came out is %q, not %q", got.Line(), key.Line())
			}
		})
	}
}

func TestEncodeKeyHoldsTheCommentAndTheCanonicalLine(t *testing.T) {
	line := testLines(t)["ed25519"]
	// spaces and a newline around the line, and a comment in another script
	img, key, err := EncodeKey("  "+line+"\n", 200)
	if err != nil {
		t.Fatal(err)
	}
	text, err := DecodeQR(img)
	if err != nil || text != line {
		t.Fatalf("decoded %q, %v; want %q", text, err, line)
	}
	if key.Comment != "laptop@emetos" {
		t.Errorf("comment %q", key.Comment)
	}
	greek := strings.Replace(line, "laptop@emetos", "φορητό", 1)
	img, _, err = EncodeKey(greek, 200)
	if err != nil {
		t.Fatal(err)
	}
	if text, err = DecodeQR(img); err != nil || text != greek {
		t.Errorf("decoded %q, %v; want %q", text, err, greek)
	}
}

// condition is how a code is seen: the code is 60% to 70% of the height of the frame.
type condition struct {
	name string
	make func(code *image.Gray, w, h int, seed int64) *image.Gray
}

// ratio is the code in a frame, at a distance that changes with the seed: from 60% to 70% of the height of the frame.
func ratio(code *image.Gray, w, h int, seed int64) *image.Gray {
	return frameWith(scaledTo(code, h*(58+2*int(seed))/100), w, h, 140)
}

var conditions = []condition{
	{"plain", func(c *image.Gray, w, h int, s int64) *image.Gray { return ratio(c, w, h, s) }},
	{"rotated 5 degrees", func(c *image.Gray, w, h int, s int64) *image.Gray { return rotated(ratio(c, w, h, s), 5) }},
	{"rotated 15 degrees", func(c *image.Gray, w, h int, s int64) *image.Gray { return rotated(ratio(c, w, h, s), 15) }},
	{"rotated -30 degrees", func(c *image.Gray, w, h int, s int64) *image.Gray { return rotated(ratio(c, w, h, s), -30) }},
	{"rotated 45 degrees", func(c *image.Gray, w, h int, s int64) *image.Gray { return rotated(ratio(c, w, h, s), 45) }},
	{"blurred", func(c *image.Gray, w, h int, s int64) *image.Gray { return blurred(ratio(c, w, h, s), 1) }},
	{"noise 20", func(c *image.Gray, w, h int, s int64) *image.Gray { return noisy(ratio(c, w, h, s), 20, s) }},
	{"noise 40", func(c *image.Gray, w, h int, s int64) *image.Gray { return noisy(ratio(c, w, h, s), 40, s) }},
	{"rotated 7, blurred, noise 15", func(c *image.Gray, w, h int, s int64) *image.Gray {
		return noisy(blurred(rotated(ratio(c, w, h, s), 7), 1), 15, s)
	}},
}

// minimumRead is how many of the 6 seeded variants of a condition must be read. The short lines (Ed25519 and ECDSA, 49 and 61
// modules a side) are read nearly always, whatever the condition; an RSA 4096 line (113 modules) is at the limit of what a
// 640x480 camera resolves (a module of 3 pixels) and of the detector of the library, which finds the same code at one size and
// not at the next, so it is asked for less. A live camera gives a new frame every 100 ms, so a code that is read in one frame of
// three is read in under half a second. The numbers are what the library does now, with some room.
func minimumRead(size, key, condition string) int {
	if key != "rsa-4096" {
		switch condition {
		case "rotated 45 degrees":
			return 4
		case "rotated 7, blurred, noise 15":
			return 5
		}
		return 5
	}
	rsa := map[string]map[string]int{
		"1280x720": {"plain": 4, "rotated 5 degrees": 5, "rotated 15 degrees": 5, "rotated -30 degrees": 5, "rotated 45 degrees": 1,
			"blurred": 3, "noise 20": 3, "noise 40": 4, "rotated 7, blurred, noise 15": 0},
		"640x480": {"plain": 4, "rotated 5 degrees": 1, "rotated 15 degrees": 2, "rotated -30 degrees": 1, "rotated 45 degrees": 0,
			"blurred": 1, "noise 20": 3, "noise 40": 2, "rotated 7, blurred, noise 15": 0},
	}
	return rsa[size][condition]
}

func TestACodeIsReadUnderTheConditionsOfACamera(t *testing.T) {
	const variants = 6
	for _, size := range []struct {
		name string
		w, h int
	}{{"1280x720", 1280, 720}, {"640x480", 640, 480}} {
		for name, line := range testLines(t) {
			img, _, err := EncodeKey(line, 400)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range conditions {
				t.Run(size.name+"/"+name+"/"+c.name, func(t *testing.T) {
					t.Parallel() // the pictures are made and read by pure functions
					ok := 0
					for seed := int64(1); seed <= variants; seed++ {
						text, err := DecodeQR(c.make(img, size.w, size.h, seed))
						if err == nil && text == line {
							ok++
						}
					}
					t.Logf("read %d of %d", ok, variants)
					if want := minimumRead(size.name, name, c.name); ok < want {
						t.Errorf("read %d of %d, want at least %d", ok, variants, want)
					}
				})
			}
		}
	}
}

func TestACodeScaledByHalfOrTwiceIsStillRead(t *testing.T) {
	line := testLines(t)["ed25519"]
	img, _, err := EncodeKey(line, 800)
	if err != nil {
		t.Fatal(err)
	}
	for _, side := range []int{700, 350, 175, 120} {
		text, err := DecodeQR(scaledTo(img, side))
		if err != nil || text != line {
			t.Errorf("a code of %d pixels: %q, %v", side, text, err)
		}
	}
}

func TestTextThatIsNotAKeyIsRefusedWithItsTag(t *testing.T) {
	good := testLines(t)["ed25519"]
	fields := strings.Fields(good)
	cases := []struct {
		name, text, route string
	}{
		{"a link", "https://example.com/pair?code=1234", "ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"empty", "", "ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"a comment", "# " + good, "ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"two lines", good + "\n" + good, "ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"options on the line", `command="rm -rf /" ` + good, "ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"a private key", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----", "ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"base64 that is not a key", fields[0] + " AAAAAAAA", "ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"a WiFi code", "WIFI:T:WPA;S:home;P:secret;;", "ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"a certificate", "ssh-ed25519-cert-v01@openssh.com AAAAIHNzaC1lZDI1NTE5LWNlcnQtdjAxQG9wZW5zc2guY29tAAAA", "ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"a line that is too long", "ssh-ed25519 " + strings.Repeat("A", 9000), "ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseKey(tc.text)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	// a key of a type the login does not take, and an RSA key that is too short, are refused with the other tag
	_, err := ParseKey("ssh-dss AAAAB3NzaC1kc3MAAACBAPY8ZOHY2yFSJA6XYC9HRwNHxaehvx5wOJ0rzZdzoSOXxbETW6ToHv8D1UJ/z+zHo9Fiko5XybZnDIaBDHtblQ+Yp7StxyltHnXF1YLfKD1G4T6JYrdHYI14Om1eg9e4NnCRleaqoZPF3UGfZia6bXrGTQf3gJq2e7Yisk/gF+1VAAAAFQDb4wVe+YImWBu7sGmjkUPXCdQfHQAAAIAAoyWEKlUp8uczbSw45Itm2smN5IN1kgxnkpmAmH0Xk1D0r3lqkQLxXrMpnwFjmFsuNTWMuYVqaD1HZ4/iAgzmVlfx0uJcfDvgJtdyNrfhV1vOuDDLSkXZh2gdOUjjXPFPnFaMdCYY0NrC5oIKQ3QpSzvN+Zh5jhhoYvDhU8T5YQAAAIEAv7p0mG+jh7zCDSLdOwVR0XjL2UZ3JF4BlFSz1S5/jiKeQ3kyNHMJbZTGFVl4D6CBhaAYBfTFIvg/jYPuU30MIgIzDAv2XIsvLWgcvj6a0jN3EC0YxgH2w/yvqN0BPkmaJa0wkzFfCzzqW8yXdMZR9r0ELi8jGsH+8UEnAyk= x")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("ParseKey->ParseAuthorizedKey.SSHLoginKey") {
		t.Errorf("a DSA key: %v", err)
	}
}

func TestOnlyAPublicKeyIsEverDrawn(t *testing.T) {
	cases := []struct{ name, text, route string }{
		{"a private key", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----", "EncodeKey->ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"anything else", "hello", "EncodeKey->ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img, _, err := EncodeKey(tc.text, 200)
			if img != nil {
				t.Error("a picture was made")
			}
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure: %v", err)
			}
		})
	}
}

func TestATextThatDoesNotFitInAQRCodeIsRefused(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"nothing", ""},
		{"more than the largest code holds", strings.Repeat("a", MaxQRBytes+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := EncodeQR(tc.text, 100)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute("EncodeQR->encodeQR.KeyQREncodeFailed") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	// the largest text that does fit is a code of level L
	img, level, _, err := encodeQR(strings.Repeat("a", MaxQRBytes), 100)
	if err != nil || img == nil || level != decoder.ErrorCorrectionLevel_L {
		t.Errorf("the largest text: level %v, %v", level, err)
	}
}

func TestAPictureWithNoCodeIsNoCode(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	noise := image.NewGray(image.Rect(0, 0, 300, 200))
	for i := range noise.Pix {
		noise.Pix[i] = uint8(rng.Intn(256))
	}
	cases := []struct {
		name string
		img  image.Image
	}{
		{"blank", image.NewGray(image.Rect(0, 0, 640, 480))},
		{"noise", noise},
		{"too small to hold a code", image.NewGray(image.Rect(0, 0, 8, 8))},
		{"no pixels", image.NewGray(image.Rect(0, 0, 0, 0))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeQR(tc.img)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute("DecodeQR") || appErr.Tag != "KeyQRNoCode" {
				t.Errorf("unexpected failure: %s", appErr.Route())
			}
		})
	}
}

func TestDecodingTheLargestFrameIsBounded(t *testing.T) {
	// noise is the worst case for a detector: it finds finder patterns everywhere and tries them all
	rng := rand.New(rand.NewSource(5))
	frame := image.NewGray(image.Rect(0, 0, MaxFrameWidth, MaxFrameHeight))
	for i := range frame.Pix {
		frame.Pix[i] = uint8(rng.Intn(256))
	}
	start := time.Now()
	if _, err := DecodeQR(frame); err == nil {
		t.Fatal("a code was found in noise")
	}
	took := time.Since(start)
	t.Logf("noise of 1920x1080: %v", took)
	if took > 3*time.Second {
		t.Errorf("a frame of noise took %v", took)
	}
}

func TestADecodedImageMayStartAwayFromTheOrigin(t *testing.T) {
	line := testLines(t)["ed25519"]
	img, _, err := EncodeKey(line, 300)
	if err != nil {
		t.Fatal(err)
	}
	// the window of a larger picture: its pixels do not start at 0,0
	big := frameWith(img, 900, 700, 255)
	win := big.SubImage(image.Rect(100, 100, 800, 600))
	if text, err := DecodeQR(win); err != nil || text != line {
		t.Errorf("a window of a picture: %q, %v", text, err)
	}
	// and a color image, which takes the slow way through At
	rgba := image.NewRGBA(img.Bounds())
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			v := img.GrayAt(x, y).Y
			rgba.Pix[y*rgba.Stride+4*x], rgba.Pix[y*rgba.Stride+4*x+1], rgba.Pix[y*rgba.Stride+4*x+2], rgba.Pix[y*rgba.Stride+4*x+3] = v, v, v, 255
		}
	}
	if text, err := DecodeQR(rgba); err != nil || text != line {
		t.Errorf("an RGBA picture: %q, %v", text, err)
	}
}
