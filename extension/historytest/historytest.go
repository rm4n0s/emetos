package historytest

import (
	"context"
	"reflect"
	"testing"
	"time"

	rerrors "github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// View is the provider as the Graph sees it.
type View struct {
	// Items, Refs and CommonAncestor ask the provider as the Graph does: off the UI thread, with the items as the Graph gets them (an
	// item with no ID is dropped).
	Items          func(ctx context.Context, o extension.HistoryOptions) ([]extension.HistoryItem, error)
	Refs           func(ctx context.Context, ids []string) ([]extension.HistoryItemRef, error)
	CommonAncestor func(ctx context.Context, refs []string) (string, error)
	// OpenCommand is the command a row is opened with.
	OpenCommand string
	// Current, Remote and Base are the references the extension set last, nil for none.
	Current, Remote, Base *extension.HistoryItemRef
	// Changes are the changes of references the Graph was told, in order.
	Changes []extension.HistoryRefsChange
}

// Harness is what an implementation gives the suite.
type Harness struct {
	// API is the API of the extension under test, in an activation that is on.
	API extension.API
	// Ask is the Graph looking for the history provider of the source control with the handle; nil when it has none.
	Ask func(handle string) *View
	// Pump lets the work posted to the UI thread run.
	Pump func()
	// Deactivate ends the activation, as closing the window tab does.
	Deactivate func()
	// Failed tells whether the implementation took the extension off because of a panic in the provider.
	Failed func() bool
}

// Run runs the suite: newHarness makes a fresh harness for each test.
func Run(t *testing.T, newHarness func(t *testing.T) *Harness) {
	t.Helper()
	tests := []struct {
		name string
		run  func(t *testing.T, h *Harness)
	}{
		{"the options of a page reach the provider and its items the Graph", testItems},
		{"an item with no ID is dropped", testDropped},
		{"the references and the common ancestor are asked of the provider", testRefs},
		{"the current references and their changes reach the Graph", testCurrent},
		{"an invalid provider is refused", testInvalid},
		{"a second provider is refused and a disposed one can be set again", testDuplicate},
		{"disposing the registration or the source control takes the provider away", testDispose},
		{"the end of the activation takes the provider away and SetHistoryProvider fails", testDeactivate},
		{"an error of the provider reaches the Graph", testError},
		{"a provider that panics takes the extension off", testPanic},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, newHarness(t)) })
	}
}

func wantTag(t *testing.T, err error, tag string) {
	t.Helper()
	appErr, ok := rerrors.FromError(err)
	if !ok {
		t.Fatalf("expected an *errors.Error with the tag %s, got %T (%v)", tag, err, err)
	}
	if appErr.Tag != tag {
		t.Errorf("tag = %s (%s), want %s", appErr.Tag, appErr.Route(), tag)
	}
}

// provider is a provider whose functions answer with what the test gives.
func provider(items func(extension.HistoryOptions) []extension.HistoryItem) extension.HistoryProvider {
	return extension.HistoryProvider{
		OpenCommand: "git.viewCommit",
		Items: func(_ context.Context, o extension.HistoryOptions) ([]extension.HistoryItem, error) {
			if items == nil {
				return nil, nil
			}
			return items(o), nil
		},
		Refs: func(_ context.Context, ids []string) ([]extension.HistoryItemRef, error) {
			return []extension.HistoryItemRef{{ID: "refs/heads/main", Name: "main", Revision: "a1", Category: "branches", Icon: "git-branch"}}, nil
		},
		CommonAncestor: func(_ context.Context, refs []string) (string, error) { return "base:" + refs[0], nil },
	}
}

func source(t *testing.T, h *Harness, root string) extension.SourceControl {
	t.Helper()
	sc, err := h.API.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: root})
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func testItems(t *testing.T, h *Harness) {
	var got extension.HistoryOptions
	sc := source(t, h, "file:///work/repo")
	when := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	want := extension.HistoryItem{
		ID: "a1", ParentIDs: []string{"b2", "c3"}, Subject: "Fix the loader", Message: "Fix the loader\n\nMore.", DisplayID: "a1", Author: "Ada Example", AuthorEmail: "ada@example.com",
		Timestamp: when, Statistics: &extension.HistoryStatistics{Files: 2, Insertions: 5, Deletions: 1},
		References: []extension.HistoryItemRef{{ID: "refs/heads/main", Name: "main", Revision: "a1", Category: "branches", Icon: "git-branch"}},
	}
	if _, err := sc.SetHistoryProvider(provider(func(o extension.HistoryOptions) []extension.HistoryItem {
		got = o
		return []extension.HistoryItem{want}
	})); err != nil {
		t.Fatal(err)
	}
	v := h.Ask(sc.Handle())
	if v == nil {
		t.Fatal("the Graph finds no provider")
	}
	if v.OpenCommand != "git.viewCommit" {
		t.Errorf("OpenCommand = %q", v.OpenCommand)
	}
	opts := extension.HistoryOptions{Skip: 50, Limit: 25, RefIDs: []string{"a1", "refs/heads/dev"}, FilterText: "loader"}
	items, err := v.Items(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, opts) {
		t.Errorf("options = %+v, want %+v", got, opts)
	}
	if !reflect.DeepEqual(items, []extension.HistoryItem{want}) {
		t.Errorf("items = %+v\nwant   %+v", items, []extension.HistoryItem{want})
	}
}

func testDropped(t *testing.T, h *Harness) {
	sc := source(t, h, "file:///work/repo")
	if _, err := sc.SetHistoryProvider(provider(func(extension.HistoryOptions) []extension.HistoryItem {
		return []extension.HistoryItem{{ID: "a1", Subject: "One"}, {Subject: "No ID"}, {ID: "b2", Subject: "Two"}}
	})); err != nil {
		t.Fatal(err)
	}
	items, err := h.Ask(sc.Handle()).Items(context.Background(), extension.HistoryOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "a1" || items[1].ID != "b2" {
		t.Errorf("items = %+v, want a1 and b2", items)
	}
}

func testRefs(t *testing.T, h *Harness) {
	sc := source(t, h, "file:///work/repo")
	if _, err := sc.SetHistoryProvider(provider(nil)); err != nil {
		t.Fatal(err)
	}
	v := h.Ask(sc.Handle())
	refs, err := v.Refs(context.Background(), nil)
	if err != nil || len(refs) != 1 || refs[0].Name != "main" || refs[0].Category != "branches" {
		t.Errorf("Refs = %+v, %v", refs, err)
	}
	id, err := v.CommonAncestor(context.Background(), []string{"main", "origin/main"})
	if err != nil || id != "base:main" {
		t.Errorf("CommonAncestor = %q, %v", id, err)
	}
}

func testCurrent(t *testing.T, h *Harness) {
	sc := source(t, h, "file:///work/repo")
	reg, err := sc.SetHistoryProvider(provider(nil))
	if err != nil {
		t.Fatal(err)
	}
	cur := &extension.HistoryItemRef{ID: "refs/heads/main", Name: "main", Revision: "a1", Icon: "target"}
	rem := &extension.HistoryItemRef{ID: "refs/remotes/origin/main", Name: "origin/main", Revision: "b2", Icon: "cloud"}
	reg.SetCurrentRefs(cur, rem, nil)
	h.Pump()
	v := h.Ask(sc.Handle())
	if !reflect.DeepEqual(v.Current, cur) || !reflect.DeepEqual(v.Remote, rem) || v.Base != nil {
		t.Errorf("refs = %+v %+v %+v", v.Current, v.Remote, v.Base)
	}
	change := extension.HistoryRefsChange{Added: []extension.HistoryItemRef{*rem}, Silent: true}
	reg.RefsChanged(change)
	h.Pump()
	if got := h.Ask(sc.Handle()).Changes; len(got) != 1 || !reflect.DeepEqual(got[0], change) {
		t.Errorf("changes = %+v, want %+v", got, change)
	}
	reg.SetCurrentRefs(nil, nil, nil)
	h.Pump()
	if v := h.Ask(sc.Handle()); v.Current != nil || v.Remote != nil {
		t.Errorf("the references were not cleared: %+v %+v", v.Current, v.Remote)
	}
}

func testInvalid(t *testing.T, h *Harness) {
	sc := source(t, h, "file:///work/repo")
	for name, p := range map[string]extension.HistoryProvider{
		"no Items":          {Refs: provider(nil).Refs, CommonAncestor: provider(nil).CommonAncestor},
		"no Refs":           {Items: provider(nil).Items, CommonAncestor: provider(nil).CommonAncestor},
		"no CommonAncestor": {Items: provider(nil).Items, Refs: provider(nil).Refs},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := sc.SetHistoryProvider(p)
			wantTag(t, err, "ExtensionHistoryInvalid")
		})
	}
	if h.Ask(sc.Handle()) != nil {
		t.Error("an invalid provider was set")
	}
}

func testDuplicate(t *testing.T, h *Harness) {
	sc := source(t, h, "file:///work/repo")
	reg, err := sc.SetHistoryProvider(provider(nil))
	if err != nil {
		t.Fatal(err)
	}
	_, err = sc.SetHistoryProvider(provider(nil))
	wantTag(t, err, "ExtensionHistoryDuplicate")
	reg.Dispose()
	h.Pump()
	if h.Ask(sc.Handle()) != nil {
		t.Error("the provider is still there after Dispose")
	}
	if _, err := sc.SetHistoryProvider(provider(nil)); err != nil {
		t.Errorf("a provider cannot be set after the old one was disposed: %v", err)
	}
}

func testDispose(t *testing.T, h *Harness) {
	sc := source(t, h, "file:///work/repo")
	if _, err := sc.SetHistoryProvider(provider(nil)); err != nil {
		t.Fatal(err)
	}
	sc.Dispose()
	h.Pump()
	if h.Ask(sc.Handle()) != nil {
		t.Error("the provider outlived its source control")
	}
	_, err := sc.SetHistoryProvider(provider(nil))
	wantTag(t, err, "ExtensionSCMDisposed")
}

func testDeactivate(t *testing.T, h *Harness) {
	sc := source(t, h, "file:///work/repo")
	reg, err := sc.SetHistoryProvider(provider(nil))
	if err != nil {
		t.Fatal(err)
	}
	h.Deactivate()
	h.Pump()
	if h.Ask(sc.Handle()) != nil {
		t.Error("the provider outlived the activation")
	}
	reg.SetCurrentRefs(&extension.HistoryItemRef{ID: "x"}, nil, nil) // does nothing
	reg.Dispose()
	_, err = sc.SetHistoryProvider(provider(nil))
	wantTag(t, err, "ExtensionDeactivated")
}

func testError(t *testing.T, h *Harness) {
	sc := source(t, h, "file:///work/repo")
	boom := &rerrors.Error{Tag: "HistoryTestFailure", Message: "git failed"}
	p := provider(nil)
	p.Items = func(context.Context, extension.HistoryOptions) ([]extension.HistoryItem, error) { return nil, boom }
	if _, err := sc.SetHistoryProvider(p); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Ask(sc.Handle()).Items(context.Background(), extension.HistoryOptions{Limit: 1}); err == nil {
		t.Error("the error of the provider did not reach the Graph")
	}
	if h.Failed() {
		t.Error("an error took the extension off")
	}
}

func testPanic(t *testing.T, h *Harness) {
	sc := source(t, h, "file:///work/repo")
	p := provider(nil)
	p.Items = func(context.Context, extension.HistoryOptions) ([]extension.HistoryItem, error) { panic("boom") }
	if _, err := sc.SetHistoryProvider(p); err != nil {
		t.Fatal(err)
	}
	_, err := h.Ask(sc.Handle()).Items(context.Background(), extension.HistoryOptions{Limit: 1})
	wantTag(t, err, "ExtensionHistoryPanicked")
	h.Pump()
	if !h.Failed() {
		t.Error("the extension is still on after its provider panicked")
	}
}
