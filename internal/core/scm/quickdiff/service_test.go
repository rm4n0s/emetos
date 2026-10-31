package quickdiff

import (
	"context"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
)

// fakeTexts reads the originals from a map; a URI that is not in it fails.
type fakeTexts struct {
	mu    sync.Mutex
	texts map[string]string
	reads int
}

func (f *fakeTexts) read(_ context.Context, uri string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	text, ok := f.texts[uri]
	if !ok {
		return "", errors.New("TestNoSuchOriginal", "no such original", "uri", uri)
	}
	return text, nil
}

func (f *fakeTexts) set(uri, text string) {
	f.mu.Lock()
	f.texts[uri] = text
	f.mu.Unlock()
}

func (f *fakeTexts) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

func newTexts() *fakeTexts { return &fakeTexts{texts: map[string]string{}} }

// original answers every file with the document "orig:" + the file's name.
func original(_ context.Context, uri string) (string, error) {
	return "orig:" + uri[len("file:///repo/"):], nil
}

func TestServiceOriginals(t *testing.T) {
	texts := newTexts()
	texts.set("orig:a.txt", "primary\n")
	texts.set("orig:sub/b.txt", "primary b\n")
	var logged []string
	svc := NewService(Options{Texts: texts.read, Log: func(msg string, _ ...any) { logged = append(logged, msg) }})
	svc.Add(Provider{Owner: "x", Label: "Secondary", Kind: KindSecondary, RootURI: "file:///repo", Original: func(ctx context.Context, uri string) (string, error) {
		if uri == "file:///repo/a.txt" {
			return "orig:a.txt", nil
		}
		return "", nil
	}})
	svc.Add(Provider{Owner: "x", Label: "Primary", Kind: KindPrimary, RootURI: "file:///repo", Original: original})
	svc.Add(Provider{Owner: "x", Label: "Elsewhere", Kind: KindPrimary, RootURI: "file:///other", Original: original})
	svc.Add(Provider{Owner: "x", Label: "Failing", Kind: KindPrimary, RootURI: "file:///repo", Original: func(context.Context, string) (string, error) {
		return "", errors.New("TestProviderFailed", "it fails")
	}})

	tests := []struct {
		name string
		uri  string
		want []string // the labels, in order
	}{
		{"both providers of the root, the primary first", "file:///repo/a.txt", []string{"Primary", "Secondary"}},
		{"a provider that has no original for the file is left out", "file:///repo/sub/b.txt", []string{"Primary"}},
		{"a file whose original cannot be read gets none", "file:///repo/none.txt", nil},
		{"a file outside every root", "file:///elsewhere/a.txt", nil},
		{"a folder with the name of the root as a prefix is not below it", "file:///repository/a.txt", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.Originals(context.Background(), tt.uri)
			if err != nil {
				t.Fatal(err)
			}
			var labels []string
			for _, o := range got {
				labels = append(labels, o.Label)
			}
			if len(labels) != len(tt.want) {
				t.Fatalf("originals = %v, want %v", labels, tt.want)
			}
			for i := range labels {
				if labels[i] != tt.want[i] {
					t.Errorf("originals = %v, want %v", labels, tt.want)
				}
			}
		})
	}
	if len(logged) == 0 {
		t.Error("a provider that failed was not logged")
	}

	text, ok, err := svc.PrimaryOriginal(context.Background(), "file:///repo/a.txt")
	if err != nil || !ok || text != "primary\n" {
		t.Errorf("PrimaryOriginal = %q, %v, %v; want the primary text", text, ok, err)
	}
	if _, ok, _ := svc.PrimaryOriginal(context.Background(), "file:///elsewhere/a.txt"); ok {
		t.Error("PrimaryOriginal found an original for a file no provider holds")
	}
}

func TestServiceStopsWithTheContext(t *testing.T) {
	svc := NewService(Options{Texts: newTexts().read})
	svc.Add(Provider{Label: "P", RootURI: "file:///repo", Original: original})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Originals(ctx, "file:///repo/a.txt"); !errors.Is(err, context.Canceled) {
		t.Errorf("Originals error = %v, want context.Canceled", err)
	}
	if _, _, err := svc.PrimaryOriginal(ctx, "file:///repo/a.txt"); !errors.Is(err, context.Canceled) {
		t.Errorf("PrimaryOriginal error = %v, want context.Canceled", err)
	}
}

func TestServiceEvents(t *testing.T) {
	svc := NewService(Options{Texts: newTexts().read})
	var events []Event
	stop := svc.Subscribe(func(ev Event) { events = append(events, ev) })
	_, remove := svc.Add(Provider{Label: "P", RootURI: "file:///repo", Original: original})
	svc.Refresh("file:///repo/sub")
	svc.Refresh("")
	remove()
	remove() // the second does nothing
	stop()
	svc.Refresh("")
	want := []Event{{Root: "file:///repo"}, {Root: "file:///repo/sub"}, {Root: ""}, {Root: "file:///repo"}}
	if len(events) != len(want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, events[i], want[i])
		}
	}
	if svc.Count() != 0 {
		t.Errorf("%d providers left", svc.Count())
	}
	for _, tt := range []struct {
		ev   Event
		uri  string
		want bool
	}{
		{Event{}, "file:///a", true},
		{Event{Root: "file:///repo"}, "file:///repo", true},
		{Event{Root: "file:///repo"}, "file:///repo/x/y", true},
		{Event{Root: "file:///repo/"}, "file:///repo/x", true},
		{Event{Root: "file:///repo"}, "file:///repo2/x", false},
		{Event{Root: "file:///repo/sub"}, "file:///repo/x", false},
	} {
		if got := tt.ev.Covers(tt.uri); got != tt.want {
			t.Errorf("%+v.Covers(%q) = %v, want %v", tt.ev, tt.uri, got, tt.want)
		}
	}
}
