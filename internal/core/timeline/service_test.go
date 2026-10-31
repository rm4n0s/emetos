package timeline

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	rerrors "github.com/rm4n0s/errors"
)

type funcProvider func(ctx context.Context, uri string, o Options) (Page, error)

func (f funcProvider) Provide(ctx context.Context, uri string, o Options) (Page, error) {
	return f(ctx, uri, o)
}

func register(t *testing.T, s *Service, id string, schemes []string, p Provider) *Registration {
	t.Helper()
	r, err := s.Register("test.ext", Descriptor{ID: id, Label: id + " label", Schemes: schemes}, p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func request(t *testing.T, s *Service, source, uri string, o Options) Result {
	t.Helper()
	ch := make(chan Result, 1)
	if req := s.Request(context.Background(), source, uri, o, func(r Result) { ch <- r }); req == nil {
		t.Fatalf("no request for %s %s", source, uri)
	}
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("the provider did not answer")
		return Result{}
	}
}

func TestRegisterAndSources(t *testing.T) {
	s := New()
	if s.HasProvider() {
		t.Error("a new service has a provider")
	}
	var events []ProvidersChange
	stop := s.OnProviders(func(e ProvidersChange) { events = append(events, e) })
	a := register(t, s, "a", []string{"file"}, funcProvider(nil))
	register(t, s, "b", []string{"file"}, funcProvider(nil))
	if !s.HasProvider() {
		t.Error("HasProvider is false after Register")
	}
	if got, want := s.Sources(), []Source{{"a", "a label"}, {"b", "b label"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("sources = %v", got)
	}
	a.Dispose()
	a.Dispose() // twice is the same as once
	if got, want := s.Sources(), []Source{{"b", "b label"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("sources after Dispose = %v", got)
	}
	stop()
	register(t, s, "c", []string{"file"}, funcProvider(nil))
	want := []ProvidersChange{{Added: []string{"a"}}, {Added: []string{"b"}}, {Removed: []string{"a"}}}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("events = %+v, want %+v", events, want)
	}
}

func TestRegisterRefusesATakenID(t *testing.T) {
	s := New()
	register(t, s, "a", []string{"file"}, funcProvider(nil))
	_, err := s.Register("other.ext", Descriptor{ID: "a", Label: "x", Schemes: []string{"file"}}, funcProvider(nil))
	appErr, ok := rerrors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("Service.Register.ExtTimelineProviderTaken") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	if appErr.Metadata["owner"] != "test.ext" || appErr.Metadata["extension"] != "other.ext" {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
}

func TestRequestSortsNewestFirstAndSetsTheSource(t *testing.T) {
	s := New()
	var got struct {
		sync.Mutex
		uri  string
		opts Options
	}
	register(t, s, "git", []string{"file"}, funcProvider(func(_ context.Context, uri string, o Options) (Page, error) {
		got.Lock()
		got.uri, got.opts = uri, o
		got.Unlock()
		return Page{Items: []Item{
			{ID: "old", Timestamp: time.Unix(10, 0)}, {ID: "new", Timestamp: time.Unix(30, 0)}, {ID: "mid", Timestamp: time.Unix(20, 0)},
			{ID: "tie1", Timestamp: time.Unix(20, 0)},
		}, Cursor: "c"}, nil
	}))
	res := request(t, s, "git", "file:///a.go", Options{Cursor: "x", Limit: 5})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	var ids []string
	for _, it := range res.Page.Items {
		ids = append(ids, it.ID+"@"+it.Source)
	}
	if want := []string{"new@git", "mid@git", "tie1@git", "old@git"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("items = %v, want %v", ids, want)
	}
	if res.Page.Cursor != "c" {
		t.Errorf("cursor = %q", res.Page.Cursor)
	}
	got.Lock()
	defer got.Unlock()
	if got.uri != "file:///a.go" || got.opts != (Options{Cursor: "x", Limit: 5}) {
		t.Errorf("provider got %q %+v", got.uri, got.opts)
	}
}

func TestRequestIsNilForAnUnknownSourceOrAnotherScheme(t *testing.T) {
	s := New()
	register(t, s, "git", []string{"file", "git"}, funcProvider(func(context.Context, string, Options) (Page, error) { return Page{}, nil }))
	register(t, s, "all", []string{"*"}, funcProvider(func(context.Context, string, Options) (Page, error) { return Page{}, nil }))
	tests := []struct {
		source, uri string
		want        bool
	}{
		{"git", "file:///a", true},
		{"git", "git:/a", true},
		{"git", "untitled:Untitled-1", false},
		{"git", "no scheme", false},
		{"all", "untitled:Untitled-1", true},
		{"nobody", "file:///a", false},
	}
	for _, tt := range tests {
		req := s.Request(context.Background(), tt.source, tt.uri, Options{}, func(Result) {})
		if (req != nil) != tt.want {
			t.Errorf("Request(%s, %s) = %v, want a request %v", tt.source, tt.uri, req, tt.want)
		}
	}
}

func TestRequestGivesTheErrorAndCancelsTheContext(t *testing.T) {
	s := New()
	started := make(chan context.Context, 1)
	boom := rerrors.New("Boom", "boom")
	register(t, s, "p", []string{"file"}, funcProvider(func(ctx context.Context, _ string, o Options) (Page, error) {
		if o.Cursor == "wait" {
			started <- ctx
			<-ctx.Done()
			return Page{}, ctx.Err()
		}
		return Page{}, boom
	}))
	if res := request(t, s, "p", "file:///a", Options{}); !rerrors.Is(res.Err, boom) {
		t.Errorf("error = %v", res.Err)
	}
	done := make(chan Result, 1)
	req := s.Request(context.Background(), "p", "file:///a", Options{Cursor: "wait"}, func(r Result) { done <- r })
	ctx := <-started
	req.Cancel()
	if r := <-done; !rerrors.Is(r.Err, context.Canceled) || ctx.Err() == nil {
		t.Errorf("after Cancel: %v, context %v", r.Err, ctx.Err())
	}
}

func TestFireAndShow(t *testing.T) {
	s := New()
	r := register(t, s, "git", []string{"file"}, funcProvider(nil))
	var changes []Change
	stop := s.OnChanged(func(c Change) { changes = append(changes, c) })
	var shown []string
	stopShow := s.OnShow(func(u string) { shown = append(shown, u) })
	r.Fire("file:///a", "file:///b")
	r.Fire()
	s.Show("file:///c")
	stop()
	stopShow()
	r.Fire("file:///ignored")
	s.Show("file:///ignored")
	want := []Change{{ID: "git", URIs: []string{"file:///a", "file:///b"}}, {ID: "git"}}
	if !reflect.DeepEqual(changes, want) {
		t.Errorf("changes = %+v, want %+v", changes, want)
	}
	if !reflect.DeepEqual(shown, []string{"file:///c"}) {
		t.Errorf("shown = %v", shown)
	}
	r.Dispose()
	var after int
	s.OnChanged(func(Change) { after++ })
	r.Fire()
	if after != 0 {
		t.Error("a removed provider fired")
	}
}

func TestSortItemsTiesByTheNumbersOfTheSource(t *testing.T) {
	now := time.Unix(100, 0)
	items := []Item{{ID: "1", Source: "src2", Timestamp: now}, {ID: "2", Source: "src10", Timestamp: now}, {ID: "3", Source: "SRC1", Timestamp: now}, {ID: "4", Source: "src2", Timestamp: now}}
	SortItems(items)
	var got []string
	for _, it := range items {
		got = append(got, it.ID)
	}
	// the later source first, numbers as numbers, case ignored, equal items in their order
	if want := []string{"2", "1", "4", "3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestSchemeOf(t *testing.T) {
	tests := map[string]string{"file:///a": "file", "git:/x": "git", "emetos-virtual:/git/a": "emetos-virtual", "/plain/path": "", "": "", "a b:c": ""}
	for in, want := range tests {
		if got := SchemeOf(in); got != want {
			t.Errorf("SchemeOf(%q) = %q, want %q", in, got, want)
		}
	}
}
