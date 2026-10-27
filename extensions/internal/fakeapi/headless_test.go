package fakeapi

import (
	"image"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheFakeHeadlessViewKeepsTheInputAndHandsOutThePictures(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	_, err := a.Workspace().NewHeadlessView(extension.Size{Width: 0, Height: 10})
	route(t, err, "workspace.NewHeadlessView.ExtensionHeadlessSize")
	view, err := a.Workspace().NewHeadlessView(extension.Size{Width: 640, Height: 400})
	if err != nil {
		t.Fatal(err)
	}
	fake := a.HeadlessViews()[0]
	if fake.Size() != (extension.Size{Width: 640, Height: 400}) {
		t.Errorf("size %v", fake.Size())
	}
	var got []*image.RGBA
	view.OnFrame(func(img *image.RGBA) { got = append(got, img) })
	var copied []string
	view.OnCopy(func(s string) { copied = append(copied, s) })
	if !fake.Emit(image.NewRGBA(image.Rect(0, 0, 640, 400))) || len(got) != 1 {
		t.Fatal("the picture did not reach OnFrame")
	}
	if !fake.Copy("text") || len(copied) != 1 || copied[0] != "text" {
		t.Errorf("copied %v", copied)
	}
	var notes []extension.ViewNotification
	view.OnNotification(func(n extension.ViewNotification) { notes = append(notes, n) })
	if !fake.Notify(extension.ViewNotification{ID: 7, Message: "hi", Actions: []string{"Open"}}) || len(notes) != 1 || notes[0].ID != 7 {
		t.Errorf("notified %v", notes)
	}
	view.RunNotificationAction(7, 0)
	if p := fake.Presses(); len(p) != 1 || p[0] != (NotificationPress{ID: 7, Action: 0}) {
		t.Errorf("presses %v", p)
	}
	view.SendKey(extension.KeyEvent{Kind: extension.KeyDown, Text: "a"})
	view.SendPointer(extension.PointerEvent{Kind: extension.PointerDown, X: 1, Y: 2})
	view.SendScroll(extension.ScrollEvent{DY: 3})
	view.SetClipboard("clip")
	view.Refresh()
	if err := view.Resize(extension.Size{Width: 100, Height: 100}); err != nil || fake.Size().Width != 100 {
		t.Errorf("resize: %v %v", err, fake.Size())
	}
	route(t, view.Resize(extension.Size{Width: -1, Height: 1}), "HeadlessView.Resize.ExtensionHeadlessSize")
	if len(fake.Keys()) != 1 || len(fake.Pointers()) != 1 || len(fake.Scrolls()) != 1 || fake.Clipboard() != "clip" || fake.Refreshes() != 1 {
		t.Errorf("input: %v %v %v %q %d", fake.Keys(), fake.Pointers(), fake.Scrolls(), fake.Clipboard(), fake.Refreshes())
	}
	select {
	case <-view.Done():
		t.Fatal("Done is closed before Close")
	default:
	}
	view.Close()
	view.Close()
	<-view.Done()
	if !fake.Closed() || fake.Emit(image.NewRGBA(image.Rect(0, 0, 1, 1))) || fake.Copy("x") || fake.Notify(extension.ViewNotification{}) {
		t.Error("a closed view still delivers")
	}
}
