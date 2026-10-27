package exthost

import (
	"context"
	"image"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// stubView is the extension.HeadlessView the scope of a test hands out.
type stubView struct {
	size   extension.Size
	closed int
	done   chan struct{}
}

func (v *stubView) Size() extension.Size               { return v.size }
func (v *stubView) OnFrame(func(*image.RGBA))          {}
func (v *stubView) Refresh()                           {}
func (v *stubView) Resize(extension.Size) error        { return nil }
func (v *stubView) SendKey(extension.KeyEvent)         {}
func (v *stubView) SendPointer(extension.PointerEvent) {}
func (v *stubView) SendScroll(extension.ScrollEvent)   {}
func (v *stubView) SetClipboard(string)                {}
func (v *stubView) OnCopy(func(string))                {}
func (v *stubView) Done() <-chan struct{}              { return v.done }
func (v *stubView) Close()                             { v.closed++ }

func TestNewHeadlessViewChecksTheSizeAndAsksTheWindow(t *testing.T) {
	var made []*stubView
	var tb *tab
	var api extension.API
	f, _ := factory(manifest("ext"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f)
	tb = r.newTab("one")
	tb.scope.opts.NewHeadlessView = func(size extension.Size) (extension.HeadlessView, error) {
		v := &stubView{size: size, done: make(chan struct{})}
		made = append(made, v)
		return v, nil
	}
	tb.scope.Start()
	tb.pump()
	sizes := []struct {
		name string
		size extension.Size
		ok   bool
	}{
		{"typical", extension.Size{Width: 1280, Height: 800}, true},
		{"the largest area", extension.Size{Width: 3840, Height: 2160}, true},
		{"the longest side", extension.Size{Width: extension.MaxHeadlessSide, Height: 1}, true},
		{"no width", extension.Size{Width: 0, Height: 800}, false},
		{"a negative height", extension.Size{Width: 800, Height: -1}, false},
		{"a side too long", extension.Size{Width: extension.MaxHeadlessSide + 1, Height: 1}, false},
		{"an area too large", extension.Size{Width: 3840, Height: 2161}, false},
	}
	for _, tt := range sizes {
		t.Run(tt.name, func(t *testing.T) {
			before := len(made)
			v, err := api.Workspace().NewHeadlessView(tt.size)
			if tt.ok {
				if err != nil || v == nil || len(made) != before+1 || v.Size() != tt.size {
					t.Fatalf("got %v, %v", v, err)
				}
				return
			}
			route(t, err, "workspaceAPI.NewHeadlessView.ExtensionHeadlessSize")
			if len(made) != before {
				t.Error("the window was asked for a view of a size that is not allowed")
			}
		})
	}
	// what the window refuses comes back as it is
	refusal := errors.New("ExtensionHeadlessUnavailable", "not on screen")
	tb.scope.opts.NewHeadlessView = func(extension.Size) (extension.HeadlessView, error) { return nil, refusal }
	if _, err := api.Workspace().NewHeadlessView(extension.Size{Width: 10, Height: 10}); err != error(refusal) {
		t.Errorf("the window's error was changed: %v", err)
	}
	// a scope with no window to ask
	tb.scope.opts.NewHeadlessView = nil
	_, err := api.Workspace().NewHeadlessView(extension.Size{Width: 10, Height: 10})
	route(t, err, "workspaceAPI.NewHeadlessView.ExtensionHeadlessUnavailable")
	// the views go with the activation, and a deactivated extension gets none
	tb.scope.Close()
	for i, v := range made {
		if v.closed != 1 {
			t.Errorf("view %d was closed %d times when the extension was deactivated", i, v.closed)
		}
	}
	_, err = api.Workspace().NewHeadlessView(extension.Size{Width: 10, Height: 10})
	route(t, err, "workspaceAPI.NewHeadlessView.ExtensionDeactivated")
}
