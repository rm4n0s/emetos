package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/errors"
)

// devRig is an editor over a document of lines, with the dev commands registered as a debug
// build has them, and the registry to run them in.
func devRig(t *testing.T, lines int) (*mouseRig, *command.Registry) {
	t.Helper()
	opts := goOptions()
	vm := NewViewModel(linesDoc(lines), opts, nil)
	ts := testutilNew(t)
	registry := command.DefaultRegistry()
	for _, id := range []string{"dev.type", "dev.scroll"} {
		if err := registry.Register(command.Meta{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	pumpEditorWith(t, ts, vm, EditorOpts{Autofocus: true, Commands: registry})
	ts.Pump(time.Millisecond)
	return &mouseRig{t: t, ts: ts, vm: vm, lineH: opts.LineHeight}, registry
}

func TestDevTypeTypesACharacterEveryFrame(t *testing.T) {
	r, registry := devRig(t, 5)
	if err := registry.Run(command.Context{}, "dev.type", "abc"); err != nil {
		t.Fatal(err)
	}
	if got := r.vm.Document().Text()[:1]; got != "l" {
		t.Fatalf("nothing is typed before the first frame: %q", r.vm.Document().Text())
	}
	r.ts.Pump(time.Millisecond)
	if got := r.vm.Document().Text()[:2]; got != "al" {
		t.Errorf("after one frame: %q", r.vm.Document().Text())
	}
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	if got := r.vm.Document().Text()[:4]; got != "abcl" {
		t.Errorf("after three frames: %q", r.vm.Document().Text())
	}
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	if got := r.vm.Document().Text()[:4]; got != "abcl" {
		t.Errorf("it stops when the text is typed: %q", r.vm.Document().Text())
	}
	// The map form is the same.
	if err := registry.Run(command.Context{}, "dev.type", map[string]any{"text": "é"}); err != nil {
		t.Fatal(err)
	}
	r.ts.Pump(time.Millisecond)
	if got := r.vm.Document().Text()[:6]; got != "abcél" {
		t.Errorf("a character that is not ASCII: %q", r.vm.Document().Text())
	}
	wantRoute(t, registry.Run(command.Context{}, "dev.type", 3.0), "DevTypeInvalid")
	noErrors(t, r.ts)
}

func TestDevScrollScrollsRowsEveryFrame(t *testing.T) {
	r, registry := devRig(t, 500)
	if err := registry.Run(command.Context{}, "dev.scroll", 10.0); err != nil {
		t.Fatal(err)
	}
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	if got := r.vm.ScrollTop(); got != 10*19 {
		t.Errorf("one frame of 10 rows: %v", got)
	}
	if err := registry.Run(command.Context{}, "dev.scroll", map[string]any{"rows": 5.0, "frames": 4.0}); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		r.ts.Pump(time.Millisecond)
	}
	if got := r.vm.ScrollTop(); got != (10+4*5)*19 {
		t.Errorf("four frames of 5 rows: %v", got)
	}
	if err := registry.Run(command.Context{}, "dev.scroll", -100.0); err != nil {
		t.Fatal(err)
	}
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	if got := r.vm.ScrollTop(); got != 0 {
		t.Errorf("scrolling up stops at the top: %v", got)
	}
	for _, bad := range []any{"x", map[string]any{"frames": 2.0}, nil} {
		wantRoute(t, registry.Run(command.Context{}, "dev.scroll", bad), "DevScrollInvalid")
	}
	noErrors(t, r.ts)
}

func TestDevCommandsNeedAnEditorOnScreen(t *testing.T) {
	actions := map[string]action{}
	devActions(actions)
	vm := NewViewModel(linesDoc(1), goOptions(), nil)
	for _, tc := range []struct {
		name string
		args any
	}{
		{"dev.type", "abc"},
		{"dev.scroll", 3.0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantRoute(t, actions[tc.name](vm, tc.args), "DevEditorNotShown")
		})
	}
}

// wantRoute checks that err is an error of the package that ends in tag.
func wantRoute(t *testing.T, err error, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(tag) {
		t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tag)
	}
}
