package qrui

import (
	"image"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
)

// The goldens of the dialogs: the preview of a camera, the note that a code is not a key, the confirmation, no camera, the picker
// and the QR code of a key. Look at them with the eyes when they change: the picture must be the scene, the code must be square.

func TestScanDialogGoldens(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			t.Run("scanning", func(t *testing.T) {
				src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Integrated Camera", Delay: 5 * time.Millisecond, Frames: []image.Image{scene(1280, 720)}})
				r := newRig(t, th, src, nil)
				r.until("the picture", func() bool { return r.d.State() == "scanning" && r.hasPicture() })
				uitest.MatchesGolden(r.ts, "scan_scanning_"+th.Name)
			})
			t.Run("confirm", func(t *testing.T) {
				src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Integrated Camera", Delay: 2 * time.Millisecond, Frames: []image.Image{withCode(t, edLine, 640, 480)}})
				r := newRig(t, th, src, nil)
				r.until("the confirmation", func() bool { return r.d.State() == "confirm" })
				r.pump()
				uitest.MatchesGolden(r.ts, "scan_confirm_"+th.Name)
			})
		})
	}
	th := uitest.Themes()[0]
	t.Run("not a key", func(t *testing.T) {
		src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Integrated Camera", Delay: 5 * time.Millisecond, Frames: []image.Image{withCode(t, "https://example.com/pair?code=7", 640, 480)}})
		r := newRig(t, th, src, nil)
		r.until("the note", func() bool { return r.d.State() == "scanning" && r.has("not a public key") && r.hasPicture() })
		uitest.MatchesGolden(r.ts, "scan_not_a_key_"+th.Name)
	})
	t.Run("two cameras", func(t *testing.T) {
		src := keyqr.NewFakeSource(
			keyqr.FakeCamera{Name: "Integrated Camera", Delay: 5 * time.Millisecond, Frames: []image.Image{scene(640, 480)}},
			keyqr.FakeCamera{Name: "USB Camera", Delay: 5 * time.Millisecond, Frames: []image.Image{scene(640, 480)}})
		r := newRig(t, th, src, nil)
		r.until("the picture", func() bool { return r.d.State() == "scanning" && r.hasPicture() })
		uitest.MatchesGolden(r.ts, "scan_two_cameras_"+th.Name)
	})
	t.Run("no camera", func(t *testing.T) {
		r := newRig(t, th, keyqr.NewFakeSource(), nil)
		r.until("the reason", func() bool { return r.d.State() == "no camera" })
		uitest.MatchesGolden(r.ts, "scan_no_camera_"+th.Name)
	})
}

func TestShowDialogGoldens(t *testing.T) {
	for _, th := range uitest.Themes() {
		for _, name := range []string{"ed25519", "rsa4096"} {
			if th.Name == "light" && name == "rsa4096" {
				continue // the code is the same on any ground, and the dark one shows the largest code
			}
			t.Run(name+"_"+th.Name, func(t *testing.T) {
				r := newShowRig(t, th, keyLine(t, name))
				uitest.MatchesGolden(r.ts, "show_"+name+"_"+th.Name)
			})
		}
	}
}
