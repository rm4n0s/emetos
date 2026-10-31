package decorationstest

import (
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Seen is what a view would draw for a file.
type Seen struct {
	// Badge, Icon, Tooltip and Color are the text, the Codicon, the hover and the first color id.
	Badge, Icon, Tooltip, Color string
}

// Harness is what an implementation gives the suite.
type Harness struct {
	// API is the API of the extension under test, in an activation that is on.
	API extension.API
	// Ask is a view asking for the decoration of a file; ok is false when it gets none.
	Ask func(uri string) (seen Seen, ok bool)
	// Told tells whether the views were told, since the last call, that the decoration of the file may have changed.
	Told func(uri string) bool
	// Pump lets the work posted to the UI thread run.
	Pump func()
	// Deactivate ends the activation, as closing the window tab does.
	Deactivate func()
	// Failed tells whether the implementation took the extension off because of a panic in the provider.
	Failed func() bool
}

// provider is a FileDecorationProvider over a map.
type provider struct {
	m map[string]extension.FileDecoration
	f func(uri string) (extension.FileDecoration, bool)
}

func (p *provider) Decoration(uri string) (extension.FileDecoration, bool) {
	if p.f != nil {
		return p.f(uri)
	}
	d, ok := p.m[uri]
	return d, ok
}

// Run runs the suite: newHarness makes a fresh harness for each test.
func Run(t *testing.T, newHarness func(t *testing.T) *Harness) {
	t.Helper()
	tests := []struct {
		name string
		run  func(t *testing.T, h *Harness)
	}{
		{"the answer of a provider reaches the view", testAnswer},
		{"a file the provider says nothing about has none", testNothing},
		{"Fire tells the views, and the new answer is asked for", testFire},
		{"an invalid decoration is dropped", testInvalid},
		{"a nil provider is refused", testNil},
		{"Dispose takes the provider away", testDispose},
		{"the end of the activation takes the provider away and Register fails", testDeactivate},
		{"a provider that panics takes the extension off", testPanic},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, newHarness(t)) })
	}
}

func testAnswer(t *testing.T, h *Harness) {
	p := &provider{m: map[string]extension.FileDecoration{
		"file:///r/a.go": {Badge: "M", Tooltip: "Modified", Color: "gitDecoration.modifiedResourceForeground"},
		"file:///r/b.go": {Icon: "lock-small", Color: "x"},
	}}
	if _, err := h.API.Decorations().Register(p); err != nil {
		t.Fatal(err)
	}
	got, ok := h.Ask("file:///r/a.go")
	if want := (Seen{Badge: "M", Tooltip: "Modified", Color: "gitDecoration.modifiedResourceForeground"}); !ok || got != want {
		t.Errorf("a.go: %+v, %v; want %+v", got, ok, want)
	}
	got, ok = h.Ask("file:///r/b.go")
	if want := (Seen{Icon: "lock-small", Color: "x"}); !ok || got != want {
		t.Errorf("b.go: %+v, %v; want %+v", got, ok, want)
	}
}

func testNothing(t *testing.T, h *Harness) {
	if _, err := h.API.Decorations().Register(&provider{m: map[string]extension.FileDecoration{}}); err != nil {
		t.Fatal(err)
	}
	if got, ok := h.Ask("file:///r/none.go"); ok {
		t.Errorf("a decoration for a file nothing is known about: %+v", got)
	}
}

func testFire(t *testing.T, h *Harness) {
	p := &provider{m: map[string]extension.FileDecoration{}}
	reg, err := h.API.Decorations().Register(p)
	if err != nil {
		t.Fatal(err)
	}
	uri := "file:///r/a.go"
	if _, ok := h.Ask(uri); ok {
		t.Fatal("decorated before the provider knows the file")
	}
	h.Told(uri) // forget what was told so far
	p.m[uri] = extension.FileDecoration{Badge: "U", Color: "c"}
	reg.Fire(uri)
	h.Pump()
	if !h.Told(uri) {
		t.Error("the views were not told")
	}
	if got, ok := h.Ask(uri); !ok || got.Badge != "U" {
		t.Errorf("after Fire: %+v, %v; want the badge U", got, ok)
	}
	delete(p.m, uri)
	reg.Fire(uri)
	h.Pump()
	if got, ok := h.Ask(uri); ok {
		t.Errorf("after the file lost its decoration: %+v", got)
	}
}

func testInvalid(t *testing.T, h *Harness) {
	p := &provider{m: map[string]extension.FileDecoration{
		"file:///r/long.go":  {Badge: "ABC", Color: "c"},
		"file:///r/empty.go": {},
		"file:///r/arrow.go": {Badge: "↓A"},
	}}
	if _, err := h.API.Decorations().Register(p); err != nil {
		t.Fatal(err)
	}
	if got, ok := h.Ask("file:///r/long.go"); ok {
		t.Errorf("a badge of three characters was kept: %+v", got)
	}
	if got, ok := h.Ask("file:///r/empty.go"); ok {
		t.Errorf("an empty decoration was kept: %+v", got)
	}
	if got, ok := h.Ask("file:///r/arrow.go"); !ok || got.Badge != "↓A" {
		t.Errorf("a badge of two characters: %+v, %v", got, ok)
	}
}

func testNil(t *testing.T, h *Harness) {
	_, err := h.API.Decorations().Register(nil)
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "ExtensionDecorationsInvalid" {
		t.Errorf("err = %v; want ExtensionDecorationsInvalid", err)
	}
}

func testDispose(t *testing.T, h *Harness) {
	p := &provider{m: map[string]extension.FileDecoration{"file:///r/a.go": {Badge: "M"}}}
	reg, err := h.API.Decorations().Register(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Ask("file:///r/a.go"); !ok {
		t.Fatal("no decoration")
	}
	reg.Dispose()
	reg.Dispose()
	reg.Fire("file:///r/a.go")
	h.Pump()
	if got, ok := h.Ask("file:///r/a.go"); ok {
		t.Errorf("a decoration of a disposed provider: %+v", got)
	}
}

func testDeactivate(t *testing.T, h *Harness) {
	p := &provider{m: map[string]extension.FileDecoration{"file:///r/a.go": {Badge: "M"}}}
	if _, err := h.API.Decorations().Register(p); err != nil {
		t.Fatal(err)
	}
	h.Deactivate()
	h.Pump()
	if got, ok := h.Ask("file:///r/a.go"); ok {
		t.Errorf("a decoration after the activation ended: %+v", got)
	}
	_, err := h.API.Decorations().Register(p)
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "ExtensionDeactivated" {
		t.Errorf("err = %v; want ExtensionDeactivated", err)
	}
}

func testPanic(t *testing.T, h *Harness) {
	p := &provider{f: func(string) (extension.FileDecoration, bool) { panic("boom") }}
	if _, err := h.API.Decorations().Register(p); err != nil {
		t.Fatal(err)
	}
	if got, ok := h.Ask("file:///r/a.go"); ok {
		t.Errorf("a decoration from a provider that panicked: %+v", got)
	}
	h.Pump()
	if !h.Failed() {
		t.Error("the extension is still on")
	}
}
