package timelinetest

import (
	"context"
	"reflect"
	"testing"
	"time"

	rerrors "github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Harness is what an implementation gives the suite.
type Harness struct {
	// API is the API of the extension under test, in an activation that is on.
	API extension.API
	// Ask is the view asking the provider with the ID for a page of the timeline of a file, and waiting for the answer: the items as
	// the view gets them (those with no ID dropped, newest first), the cursor, and the error. ok is false when the view has no such
	// provider or the provider has no history for the scheme of the URI.
	Ask func(source, uri string, opts extension.TimelineOptions) (tl extension.Timeline, err error, ok bool)
	// Sources are the IDs of the providers the view lists, in the order they were registered.
	Sources func() []string
	// Told is what the view was told, since the last call, about changes of the timelines: the URIs of each call of Changed (none for
	// every file), with the ID of the provider.
	Told func() []Told
	// Pump lets the work posted to the UI thread run.
	Pump func()
	// Deactivate ends the activation, as closing the window tab does.
	Deactivate func()
	// Failed tells whether the implementation took the extension off because of a panic in the provider.
	Failed func() bool
}

// Told is a change the view was told of.
type Told struct {
	// ID is the provider and URIs the files; none means every file.
	ID   string
	URIs []string
}

// Run runs the suite: newHarness makes a fresh harness for each test.
func Run(t *testing.T, newHarness func(t *testing.T) *Harness) {
	t.Helper()
	tests := []struct {
		name string
		run  func(t *testing.T, h *Harness)
	}{
		{"the page of a provider reaches the view with its options", testPage},
		{"items with no ID are dropped and the others sorted newest first", testItems},
		{"the fields of an item reach the view", testFields},
		{"a scheme the provider has no history for is not asked", testSchemes},
		{"an error of the provider reaches the view", testError},
		{"Changed tells the view", testChanged},
		{"an invalid provider is refused", testInvalid},
		{"an ID that is taken is refused", testTaken},
		{"Dispose takes the provider away", testDispose},
		{"the end of the activation takes the provider away and RegisterProvider fails", testDeactivate},
		{"a provider that panics takes the extension off", testPanic},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, newHarness(t)) })
	}
}

func provider(id string, f func(ctx context.Context, uri string, o extension.TimelineOptions) (extension.Timeline, error)) extension.TimelineProvider {
	return extension.TimelineProvider{ID: id, Label: id + " label", Schemes: []string{"file"}, Provide: f}
}

func testPage(t *testing.T, h *Harness) {
	var got extension.TimelineOptions
	var gotURI string
	_, err := h.API.Timeline().RegisterProvider(provider("p", func(_ context.Context, uri string, o extension.TimelineOptions) (extension.Timeline, error) {
		got, gotURI = o, uri
		return extension.Timeline{Items: []extension.TimelineItem{{ID: "a", Label: "A", Timestamp: time.Unix(5, 0)}}, Paging: extension.TimelinePaging{Cursor: "next"}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.Sources(), []string{"p"}) {
		t.Errorf("sources = %v", h.Sources())
	}
	tl, err, ok := h.Ask("p", "file:///r/a.go", extension.TimelineOptions{Cursor: "c1", Limit: 7})
	if err != nil || !ok {
		t.Fatalf("Ask = %v, %v", err, ok)
	}
	if got != (extension.TimelineOptions{Cursor: "c1", Limit: 7}) || gotURI != "file:///r/a.go" {
		t.Errorf("the provider got %q %+v", gotURI, got)
	}
	if len(tl.Items) != 1 || tl.Items[0].ID != "a" || tl.Paging.Cursor != "next" {
		t.Errorf("page = %+v", tl)
	}
}

func testItems(t *testing.T, h *Harness) {
	_, err := h.API.Timeline().RegisterProvider(provider("p", func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{Items: []extension.TimelineItem{
			{ID: "old", Timestamp: time.Unix(1, 0)}, {Label: "no id", Timestamp: time.Unix(99, 0)}, {ID: "new", Timestamp: time.Unix(3, 0)},
		}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	tl, err, _ := h.Ask("p", "file:///r/a.go", extension.TimelineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range tl.Items {
		ids = append(ids, it.ID)
	}
	if !reflect.DeepEqual(ids, []string{"new", "old"}) {
		t.Errorf("items = %v", ids)
	}
}

func testFields(t *testing.T, h *Harness) {
	when := time.Unix(1700000000, 0)
	want := extension.TimelineItem{
		ID: "abc", Label: "Fix", Description: "Ada", Detail: "**Ada**", Timestamp: when, Icon: "git-commit", ContextValue: "git:file:commit",
		Command: &extension.Command{ID: "x.open", Title: "Open", Tooltip: "tip", Args: map[string]any{"k": "v"}}, Args: map[string]any{"ref": "abc"},
	}
	_, err := h.API.Timeline().RegisterProvider(provider("p", func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{Items: []extension.TimelineItem{want}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	tl, _, _ := h.Ask("p", "file:///r/a.go", extension.TimelineOptions{})
	if len(tl.Items) != 1 {
		t.Fatalf("items = %+v", tl.Items)
	}
	got := tl.Items[0]
	if got.ID != want.ID || got.Label != want.Label || got.Description != want.Description || got.Detail != want.Detail || !got.Timestamp.Equal(when) ||
		got.Icon != want.Icon || got.ContextValue != want.ContextValue || !reflect.DeepEqual(got.Args, want.Args) {
		t.Errorf("item = %+v, want %+v", got, want)
	}
	if got.Command == nil || got.Command.ID != "x.open" || got.Command.Title != "Open" || got.Command.Tooltip != "tip" || !reflect.DeepEqual(got.Command.Args, map[string]any{"k": "v"}) {
		t.Errorf("command = %+v", got.Command)
	}
}

func testSchemes(t *testing.T, h *Harness) {
	calls := 0
	_, err := h.API.Timeline().RegisterProvider(provider("p", func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		calls++
		return extension.Timeline{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := h.Ask("p", "untitled:Untitled-1", extension.TimelineOptions{}); ok {
		t.Error("the provider was asked about a scheme it has no history for")
	}
	if _, _, ok := h.Ask("nobody", "file:///r/a.go", extension.TimelineOptions{}); ok {
		t.Error("a provider that does not exist was asked")
	}
	if calls != 0 {
		t.Errorf("Provide was called %d times", calls)
	}
}

func testError(t *testing.T, h *Harness) {
	boom := rerrors.New("Boom", "boom")
	_, err := h.API.Timeline().RegisterProvider(provider("p", func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{}, boom
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err, _ := h.Ask("p", "file:///r/a.go", extension.TimelineOptions{}); !rerrors.Is(err, boom) {
		t.Errorf("error = %v", err)
	}
}

func testChanged(t *testing.T, h *Harness) {
	reg, err := h.API.Timeline().RegisterProvider(provider("p", func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	h.Told()
	reg.Changed("file:///r/a.go")
	reg.Changed()
	h.Pump()
	want := []Told{{ID: "p", URIs: []string{"file:///r/a.go"}}, {ID: "p"}}
	if got := h.Told(); !reflect.DeepEqual(got, want) {
		t.Errorf("told = %+v, want %+v", got, want)
	}
	reg.Dispose()
	reg.Changed()
	h.Pump()
	if got := h.Told(); len(got) != 0 {
		t.Errorf("a disposed provider told the view: %+v", got)
	}
}

func testInvalid(t *testing.T, h *Harness) {
	ok := func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{}, nil
	}
	tests := []struct {
		name string
		p    extension.TimelineProvider
	}{
		{"no ID", extension.TimelineProvider{Label: "L", Schemes: []string{"file"}, Provide: ok}},
		{"no label", extension.TimelineProvider{ID: "x", Schemes: []string{"file"}, Provide: ok}},
		{"no scheme", extension.TimelineProvider{ID: "x", Label: "L", Provide: ok}},
		{"an empty scheme", extension.TimelineProvider{ID: "x", Label: "L", Schemes: []string{""}, Provide: ok}},
		{"no function", extension.TimelineProvider{ID: "x", Label: "L", Schemes: []string{"file"}}},
	}
	for _, tt := range tests {
		_, err := h.API.Timeline().RegisterProvider(tt.p)
		mustTag(t, tt.name, err, "ExtensionTimelineInvalid")
	}
	if len(h.Sources()) != 0 {
		t.Errorf("sources = %v", h.Sources())
	}
}

func testTaken(t *testing.T, h *Harness) {
	empty := func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{}, nil
	}
	if _, err := h.API.Timeline().RegisterProvider(provider("p", empty)); err != nil {
		t.Fatal(err)
	}
	_, err := h.API.Timeline().RegisterProvider(provider("p", empty))
	mustTag(t, "the same ID", err, "ExtTimelineProviderTaken")
}

func testDispose(t *testing.T, h *Harness) {
	reg, err := h.API.Timeline().RegisterProvider(provider("p", func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	reg.Dispose()
	reg.Dispose()
	h.Pump()
	if len(h.Sources()) != 0 {
		t.Errorf("sources after Dispose = %v", h.Sources())
	}
	// the ID can be used again
	if _, err := h.API.Timeline().RegisterProvider(provider("p", func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{}, nil
	})); err != nil {
		t.Errorf("registering the ID again: %v", err)
	}
}

func testDeactivate(t *testing.T, h *Harness) {
	if _, err := h.API.Timeline().RegisterProvider(provider("p", func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{}, nil
	})); err != nil {
		t.Fatal(err)
	}
	h.Deactivate()
	h.Pump()
	if len(h.Sources()) != 0 {
		t.Errorf("sources after the activation ended = %v", h.Sources())
	}
	_, err := h.API.Timeline().RegisterProvider(provider("q", func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{}, nil
	}))
	mustTag(t, "after the end", err, "ExtensionDeactivated")
}

func testPanic(t *testing.T, h *Harness) {
	if _, err := h.API.Timeline().RegisterProvider(provider("p", func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		panic("boom")
	})); err != nil {
		t.Fatal(err)
	}
	_, err, _ := h.Ask("p", "file:///r/a.go", extension.TimelineOptions{})
	mustTag(t, "the panic", err, "ExtensionTimelinePanicked")
	h.Pump()
	if !h.Failed() {
		t.Error("the extension was not taken off")
	}
}

// mustTag checks that err is an *errors.Error with the tag.
func mustTag(t *testing.T, what string, err error, tag string) {
	t.Helper()
	appErr, ok := rerrors.FromError(err)
	if !ok {
		t.Errorf("%s: expected *errors.Error, got %T (%v)", what, err, err)
		return
	}
	if appErr.Tag != tag {
		t.Errorf("%s: tag %s, want %s (route %s)", what, appErr.Tag, tag, appErr.Route())
	}
}
