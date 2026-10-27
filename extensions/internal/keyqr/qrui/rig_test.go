package qrui

import (
	"context"
	"image"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// the lines of the keys of the tests: the public halves of keys made for them (no private key is in the repository)
const (
	edLine = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICRlIqzvVjLOB4ouXHmzTdLheJBhMGlwdnfNJLSV0Lq3 laptop@emetos"
)

var size = gui.Size{Width: 640, Height: 560}

// scene is the picture a camera shows when nothing is held in front of it.
func scene(w, h int) *image.RGBA { return keyqr.Scene(w, h) }

// withCode is a scene with a QR code of the text held in front of it.
func withCode(t testing.TB, text string, w, h int) image.Image {
	t.Helper()
	frame, err := keyqr.SceneWithCode(text, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

// rig is a dialog shown in a window, with the UI thread played by the test.
type rig struct {
	t       *testing.T
	ts      *gluttertest.Tester
	posted  chan func()
	rebuild func()
	d       *ScanDialog
	src     keyqr.Source

	refreshes int
	added     []sshlogin.AllowedKey
	closes    int
}

// newRig shows the dialog of a Source in a theme. The dialog scans every 5 ms.
func newRig(t *testing.T, th uitest.Theme, src keyqr.Source, ctx context.Context) *rig {
	t.Helper()
	r := &rig{t: t, posted: make(chan func(), 1024), src: src}
	host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		r.rebuild = func() { set(func() {}) }
		children := []widgets.Widget{widgets.NewColoredBox(uitest.Themes()[0].Colors.EditorBackground, widgets.NewSizedBox(nil))}
		if r.d != nil && !r.d.closed {
			children = append(children, r.d.Build(bc))
		}
		return widgets.NewStack(children, widgets.StackOpts{Fit: 1})
	})
	r.ts = uitest.Pump(t, th, size, host)
	if ctx == nil {
		ctx = t.Context()
	}
	r.d = NewScanDialog(ScanOptions{
		Context: ctx, Source: src, Interval: 5 * time.Millisecond,
		Post:    func(f func()) { r.posted <- f },
		Refresh: func() { r.refreshes++; r.rebuild() },
		Add:     func(k sshlogin.AllowedKey) { r.added = append(r.added, k) },
		Close:   func() { r.closes++; r.rebuild() },
	})
	r.rebuild()
	t.Cleanup(r.d.Close)
	return r
}

// pump runs what the goroutine of the camera posted to the UI thread.
func (r *rig) pump() {
	r.t.Helper()
	for {
		select {
		case f := <-r.posted:
			f()
			r.ts.Pump()
		default:
			r.ts.Pump()
			return
		}
	}
}

// until pumps until the condition holds.
func (r *rig) until(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r.pump()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s (state %s)", what, r.d.State())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (r *rig) has(text string) bool { return r.ts.Exists(gluttertest.ByTextContaining(text)) }

func (r *rig) tap(text string) {
	r.t.Helper()
	r.ts.Tap(gluttertest.ByText(text))
	r.pump()
}

// hasPicture tells whether a picture of the camera is on screen.
func (r *rig) hasPicture() bool { return r.ts.Exists(gluttertest.ByType[*widgets.RawImage]()) }

// until released waits for the camera to be let go.
func released(src *keyqr.FakeSource, n int) func() bool {
	return func() bool { return src.Released() == n && src.Active() == 0 }
}
