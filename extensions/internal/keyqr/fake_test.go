package keyqr

import (
	"testing"
)

func TestTheSceneIsTheSameEveryTimeAndAFrameWithACodeIsRead(t *testing.T) {
	a, b := Scene(640, 480), Scene(640, 480)
	if string(a.Pix) != string(b.Pix) {
		t.Error("the scene changes between calls")
	}
	if got := Scene(1, 1).Bounds().Dx(); got != 16 {
		t.Errorf("the smallest scene is %d wide", got)
	}
	if got := Scene(99999, 99999).Bounds(); got.Dx() != MaxFrameWidth || got.Dy() != MaxFrameHeight {
		t.Errorf("the largest scene is %v", got)
	}
	if _, err := DecodeQR(a); err == nil {
		t.Error("a code was found in the empty scene")
	}
	line := testLines(t)["ed25519"]
	for _, size := range [][2]int{{640, 480}, {1280, 720}} {
		frame, err := SceneWithCode(line, size[0], size[1])
		if err != nil {
			t.Fatal(err)
		}
		if text, err := DecodeQR(frame); err != nil || text != line {
			t.Errorf("%dx%d: read %q, %v", size[0], size[1], text, err)
		}
	}
	_, err := SceneWithCode("", 640, 480)
	assertRoute(t, err, "SceneWithCode->EncodeQR->encodeQR.KeyQREncodeFailed")
}
