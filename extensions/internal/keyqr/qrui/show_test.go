package qrui

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
)

// keyLine reads the public line of a key of the tests, which keyqr keeps in testdata.
func keyLine(t testing.TB, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", name+".pub"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// showRig shows a ShowDialog in a window.
type showRig struct {
	ts     *gluttertest.Tester
	d      *ShowDialog
	closes int
}

func newShowRig(t *testing.T, th uitest.Theme, line string) *showRig {
	t.Helper()
	return newShowRigIn(t, th, line, gui.Size{Width: 640, Height: 900})
}

func newShowRigIn(t *testing.T, th uitest.Theme, line string, win gui.Size) *showRig {
	t.Helper()
	r := &showRig{}
	var rebuild func()
	d, err := NewShowDialog(ShowOptions{Line: line, Close: func() { r.closes++; rebuild() }})
	if err != nil {
		t.Fatal(err)
	}
	r.d = d
	host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		rebuild = func() { set(func() {}) }
		children := []widgets.Widget{widgets.NewColoredBox(th.Colors.EditorBackground, widgets.NewSizedBox(nil))}
		if !d.closed {
			children = append(children, d.Build(bc))
		}
		return widgets.NewStack(children, widgets.StackOpts{Fit: 1})
	})
	r.ts = uitest.Pump(t, th, win, host)
	return r
}

func TestTheDialogShowsTheQRCodeOfTheKeyWithItsDetails(t *testing.T) {
	for _, name := range []string{"ed25519", "ecdsa", "rsa4096"} {
		t.Run(name, func(t *testing.T) {
			line := keyLine(t, name)
			r := newShowRig(t, uitest.Themes()[0], line)
			key := r.d.Key()
			for _, want := range []string{"My Public Key", key.Type(), key.Fingerprint(), key.Comment, "Only the public key is shown", "never leaves this computer"} {
				if !r.ts.Exists(gluttertest.ByTextContaining(want)) {
					t.Errorf("the dialog does not show %q", want)
				}
			}
			// the picture on screen is the code of exactly this key: read back, it is the line
			pic := r.ts.Widget(gluttertest.ByType[*widgets.RawImage]()).(*widgets.RawImage).Image()
			if pic.Width() > showSide || pic.Width() != pic.Height() {
				t.Errorf("the code is %dx%d, to fit %d", pic.Width(), pic.Height(), showSide)
			}
			rgba := &image.RGBA{Pix: pic.Pixels(), Stride: 4 * pic.Width(), Rect: image.Rect(0, 0, pic.Width(), pic.Height())}
			text, err := keyqr.DecodeQR(rgba)
			if err != nil || text != line {
				t.Errorf("the code on screen reads %q, %v; want the key %q", text, err, line)
			}
			// the code is drawn at its own size: a pixel for a pixel
			if size := r.ts.Size(gluttertest.ByType[*widgets.RawImage]()); size.Width != float64(pic.Width()) {
				t.Errorf("drawn at %v", size)
			}
		})
	}
}

func TestTheShowDialogCloses(t *testing.T) {
	r := newShowRig(t, uitest.Themes()[0], keyLine(t, "ed25519"))
	r.ts.Tap(gluttertest.ByText("Close"))
	r.ts.Pump()
	if r.closes != 1 || r.d.picture != nil {
		t.Errorf("closed %d times; the picture is kept: %v", r.closes, r.d.picture != nil)
	}
	r.d.Close()
	if r.closes != 1 {
		t.Errorf("a second Close told the owner again")
	}
	if r.ts.Exists(gluttertest.ByText("My Public Key")) {
		t.Error("the dialog is still on screen")
	}
}

func TestOnlyAPublicKeyLineMakesAShowDialog(t *testing.T) {
	cases := []struct{ name, text, route string }{
		{"a private key", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----", "NewShowDialog->EncodeKey->ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"nothing", "", "NewShowDialog->EncodeKey->ParseKey->ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"a DSA key", "ssh-dss AAAAB3NzaC1kc3MAAAAA x", "NewShowDialog->EncodeKey->ParseKey->ParseAuthorizedKey.SSHLoginKey"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewShowDialog(ShowOptions{Line: tc.text})
			if d != nil {
				t.Error("a dialog was made")
			}
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure: %v", err)
			}
		})
	}
}

func TestAShowDialogWithNoOptionsDoesNotPanic(t *testing.T) {
	d, err := NewShowDialog(ShowOptions{Line: keyLine(t, "ecdsa")})
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
}

func TestTheCodeIsMadeSmallerInAWindowThatIsTooLowForIt(t *testing.T) {
	line := keyLine(t, "rsa4096")
	for _, h := range []float64{900, 700, 560, 420} {
		r := newShowRigIn(t, uitest.Themes()[0], line, gui.Size{Width: 640, Height: h})
		got := r.ts.Size(gluttertest.ByType[*widgets.RawImage]())
		t.Logf("a window %v high: the code is drawn at %vx%v", h, got.Width, got.Height)
		if got.Width != got.Height || got.Width > showSide || got.Height > h-120 {
			t.Errorf("a window %v high: the code is %vx%v", h, got.Width, got.Height)
		}
		if h >= 900 && got.Width != showSide {
			t.Errorf("in a high window the code is %v, not %d", got.Width, showSide)
		}
		if !r.ts.Exists(gluttertest.ByText("Close")) {
			t.Errorf("a window %v high: no Close button", h)
		}
	}
}
