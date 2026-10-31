package decorations

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// table is a Provider over a map of URIs; Calls counts the questions.
type table struct {
	mu    sync.Mutex
	data  map[string]Data
	calls int
	order *[]string
	label string
}

func (t *table) Decoration(uri string) (Data, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	if t.order != nil {
		*t.order = append(*t.order, t.label)
	}
	d, ok := t.data[uri]
	return d, ok
}

func (t *table) set(uri string, d Data) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.data == nil {
		t.data = map[string]Data{}
	}
	t.data[uri] = d
}

func (t *table) remove(uri string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.data, uri)
}

func mustRegister(t *testing.T, s *Service, label string, p Provider) *Registration {
	t.Helper()
	r, err := s.Register(label, p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// recorder collects the changes of a service.
type recorder struct {
	mu      sync.Mutex
	changes []Change
}

func (r *recorder) listen(s *Service) func() {
	return s.OnChange(func(c Change) {
		r.mu.Lock()
		r.changes = append(r.changes, c)
		r.mu.Unlock()
	})
}

func (r *recorder) last() (Change, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.changes) == 0 {
		return Change{}, false
	}
	return r.changes[len(r.changes)-1], true
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.changes)
}

// The cases of VS Code's decorationsService.test.ts that apply (see IMPLEMENTED.md for the count): a provider answers at once here, so the
// cases about promises are the Fire path (evented result) or skipped.

// Ported from VS Code: src/vs/workbench/services/decorations/test/browser/decorationsService.test.ts
func TestPortedDecorationsService(t *testing.T) {
	t.Run("Async provider, async/evented result: a provider that learns later fires the URI", func(t *testing.T) {
		s := New(Options{})
		uri := "foo:bar"
		p := &table{}
		reg := mustRegister(t, s, "Test", p)
		rec := &recorder{}
		defer rec.listen(s)()

		if _, ok := s.Get(uri, false); ok {
			t.Fatal("a decoration before the provider knows one")
		}
		if p.calls != 1 {
			t.Fatalf("calls = %d, want 1", p.calls)
		}
		p.set(uri, Data{Color: "someBlue", Tooltip: "T", Strikethrough: true})
		reg.Fire(uri)
		c, ok := rec.last()
		if !ok || !c.Affects(uri) {
			t.Fatalf("no event that affects %s", uri)
		}
		d, ok := s.Get(uri, false)
		if !ok || d.Tooltip != "T" || !d.Strikethrough {
			t.Errorf("decoration = %+v, %v", d, ok)
		}
		if p.calls != 2 {
			t.Errorf("calls = %d, want 2 (one for the question, one for Fire)", p.calls)
		}
		reg.Dispose()
	})

	t.Run("Sync provider, sync result", func(t *testing.T) {
		s := New(Options{})
		uri := "foo:bar"
		p := &table{data: map[string]Data{uri: {Color: "someBlue", Tooltip: "Z"}}}
		reg := mustRegister(t, s, "Test", p)
		d, ok := s.Get(uri, false)
		if !ok || d.Tooltip != "Z" {
			t.Fatalf("decoration = %+v, %v", d, ok)
		}
		if d.Strikethrough {
			t.Error("strikethrough, want none")
		}
		s.Get(uri, false)
		if p.calls != 1 {
			t.Errorf("calls = %d, want 1 (the answer is kept)", p.calls)
		}
		reg.Dispose()
	})

	t.Run("Classifies text, icon, and bubbled badges", func(t *testing.T) {
		s := New(Options{})
		textURI, iconURI, parent, child := "file:///text.txt", "file:///icon.txt", "file:///folder/", "file:///folder/child.txt"
		p := &table{data: map[string]Data{
			textURI: {Letter: "M"},
			iconURI: {Icon: "lock-small"},
			child:   {Letter: "M", Bubble: true},
		}}
		reg := mustRegister(t, s, "Badge types", p)
		text, _ := s.Get(textURI, false)
		icon, _ := s.Get(iconURI, false)
		s.Get(child, false)
		bubble, ok := s.Get(parent, true)
		if !ok {
			t.Fatal("no decoration for the folder")
		}
		got := [3]bool{text.IsTextBadge(), icon.IsTextBadge(), bubble.IsTextBadge()}
		if got != [3]bool{true, false, false} {
			t.Errorf("text badges = %v, want [true false false]", got)
		}
		reg.Dispose()
	})

	t.Run("Falls back to lower-weight decoration color when a theme color is undefined", func(t *testing.T) {
		// VS Code builds a CSS chain var(--high, var(--low)); the service hands the chain over and the view uses the first color the
		// theme knows (internal/gui/kit/filedecorations_test.go checks that part).
		s := New(Options{})
		uri, parent := "foo:/folder/file", "foo:/folder/"
		high := &table{data: map[string]Data{uri: {Color: "highPriorityColor", Weight: 10, Letter: "H", Bubble: true}}}
		low := &table{data: map[string]Data{uri: {Color: "lowPriorityColor", Weight: 5, Letter: "L", Bubble: true}}}
		a := mustRegister(t, s, "High priority", high)
		b := mustRegister(t, s, "Low priority", low)
		want := []string{"highPriorityColor", "lowPriorityColor"}
		file, _ := s.Get(uri, false)
		folder, _ := s.Get(parent, true)
		if strings.Join(file.Colors, ",") != strings.Join(want, ",") {
			t.Errorf("file colors = %v, want %v", file.Colors, want)
		}
		if strings.Join(folder.Colors, ",") != strings.Join(want, ",") {
			t.Errorf("folder colors = %v, want %v", folder.Colors, want)
		}
		if file.Badge != "H, L" {
			t.Errorf("badge = %q, want %q", file.Badge, "H, L")
		}
		a.Dispose()
		b.Dispose()
	})

	t.Run("Clear decorations on provider dispose", func(t *testing.T) {
		s := New(Options{})
		uri := "foo:bar"
		p := &table{data: map[string]Data{uri: {Color: "someBlue", Tooltip: "J"}}}
		reg := mustRegister(t, s, "Test", p)
		if d, _ := s.Get(uri, false); d.Tooltip != "J" {
			t.Fatalf("tooltip = %q, want J", d.Tooltip)
		}
		if p.calls != 1 {
			t.Fatalf("calls = %d, want 1", p.calls)
		}
		var seen bool
		stop := s.OnChange(func(c Change) {
			if !c.Affects(uri) {
				t.Error("the event does not affect the URI")
			}
			if _, ok := s.Get(uri, false); ok {
				t.Error("the decoration is still there in the event")
			}
			if p.calls != 1 {
				t.Errorf("calls = %d, want 1", p.calls)
			}
			seen = true
		})
		defer stop()
		reg.Dispose()
		if !seen {
			t.Error("no event")
		}
	})

	t.Run("No default bubbling", func(t *testing.T) {
		s := New(Options{})
		child := "file:///some/path/some/file.txt"
		folder := "file:///some/path/"
		prov := ProviderFunc(func(uri string) (Data, bool) {
			if strings.Contains(uri, ".txt") {
				return Data{Tooltip: ".txt", Weight: 17}, true
			}
			return Data{}, false
		})
		reg := mustRegister(t, s, "Test", prov)
		d, _ := s.Get(child, false)
		if d.Tooltip != ".txt" {
			t.Errorf("tooltip = %q, want .txt", d.Tooltip)
		}
		if _, ok := s.Get(folder, true); ok {
			t.Error("a folder decoration without bubbling")
		}
		reg.Dispose()

		reg = mustRegister(t, s, "Test", ProviderFunc(func(uri string) (Data, bool) {
			if strings.Contains(uri, ".txt") {
				return Data{Tooltip: ".txt.bubble", Weight: 71, Bubble: true}, true
			}
			return Data{}, false
		}))
		d, _ = s.Get(child, false)
		if d.Tooltip != ".txt.bubble" {
			t.Errorf("tooltip = %q, want .txt.bubble", d.Tooltip)
		}
		f, ok := s.Get(folder, true)
		if !ok || f.Tooltip == "" {
			t.Errorf("folder = %+v, %v; want a tooltip", f, ok)
		}
		reg.Dispose()
	})

	t.Run("Decorations not bubbling... #48745", func(t *testing.T) {
		s := New(Options{})
		reg := mustRegister(t, s, "Test", ProviderFunc(func(uri string) (Data, bool) {
			if strings.HasSuffix(uri, "hello") {
				return Data{Tooltip: "FOO", Weight: 17, Bubble: true}, true
			}
			return Data{}, false // VS Code returns a promise that never resolves for these
		}))
		if _, ok := s.Get("a:b/", true); ok {
			t.Error("a decoration for the folder before any file is known")
		}
		if d, ok := s.Get("a:b/c.hello", false); !ok || d.Tooltip == "" {
			t.Errorf("file = %+v, %v", d, ok)
		}
		if _, ok := s.Get("a:b/", true); !ok {
			t.Error("no decoration for the folder after the file is known")
		}
		reg.Dispose()
	})

	t.Run("Folder decorations don't go away when file with problems is deleted #61919 (part1)", func(t *testing.T) {
		s := New(Options{})
		gone := false
		reg := mustRegister(t, s, "Test", ProviderFunc(func(uri string) (Data, bool) {
			if !gone && strings.HasSuffix(uri, "file.ts") {
				return Data{Tooltip: "FOO", Weight: 17, Bubble: true}, true
			}
			return Data{}, false
		}))
		uri, folder := "foo:/folder/file.ts", "foo:/folder/"
		if d, _ := s.Get(uri, true); d.Tooltip != "FOO" {
			t.Errorf("tooltip = %q, want FOO", d.Tooltip)
		}
		if d, _ := s.Get(folder, true); d.Tooltip == "" {
			t.Error("the folder has no tooltip (emphasized items)")
		}
		gone = true
		reg.Fire(uri)
		if _, ok := s.Get(uri, true); ok {
			t.Error("the file is still decorated")
		}
		if _, ok := s.Get(folder, true); ok {
			t.Error("the folder is still decorated")
		}
		reg.Dispose()
	})

	t.Run("Folder decorations don't go away when file with problems is deleted #61919 (part2)", func(t *testing.T) {
		s := New(Options{})
		gone := false
		reg := mustRegister(t, s, "Test", ProviderFunc(func(uri string) (Data, bool) {
			if !gone && strings.HasSuffix(uri, "file.ts") {
				return Data{Tooltip: "FOO", Weight: 17, Bubble: true}, true
			}
			return Data{}, false
		}))
		uri, folder := "foo:/folder/file.ts", "foo:/folder/"
		s.Get(uri, true)
		s.Get(folder, true)
		var hit bool
		stop := s.OnChange(func(c Change) {
			if !c.Affects(uri) || !c.Affects(folder) {
				t.Errorf("the event affects the file %v and the folder %v, want both", c.Affects(uri), c.Affects(folder))
			}
			hit = true
		})
		defer stop()
		gone = true
		reg.Fire(uri)
		if !hit {
			t.Error("no event")
		}
		reg.Dispose()
	})

	t.Run("FileDecorationProvider intermittently fails #133210: the newest provider is asked first", func(t *testing.T) {
		s := New(Options{})
		var order []string
		a := mustRegister(t, s, "Provider-1", &table{order: &order, label: "Provider-1"})
		b := mustRegister(t, s, "Provider-2", &table{order: &order, label: "Provider-2"})
		s.Get("test://me/path", false)
		if strings.Join(order, ",") != "Provider-2,Provider-1" {
			t.Errorf("order = %v, want Provider-2, Provider-1", order)
		}
		a.Dispose()
		b.Dispose()
	})
}

func TestMerge(t *testing.T) {
	tests := []struct {
		name string
		data []Data
		want Decoration
	}{
		{"one answer", []Data{{Color: "c", Letter: "M", Tooltip: "Modified"}}, Decoration{Colors: []string{"c"}, Badge: "M", Tooltip: "Modified"}},
		{"letters joined, weight orders them", []Data{{Weight: 1, Letter: "M", Color: "m"}, {Weight: 4, Letter: "!", Color: "x"}}, Decoration{Colors: []string{"x", "m"}, Badge: "!, M"}},
		{"tooltips joined with a bullet, repeats and blanks dropped", []Data{{Tooltip: "A"}, {Tooltip: "  "}, {Tooltip: "B"}, {Tooltip: "A"}}, Decoration{Tooltip: "A • B"}},
		{"equal weights keep the newest provider first", []Data{{Letter: "N"}, {Letter: "O"}}, Decoration{Badge: "N, O"}},
		{"an icon wins over the letters after it and drops those before", []Data{{Letter: "A"}, {Icon: "lock"}, {Letter: "B"}}, Decoration{Icon: "lock"}},
		{"strikethrough from any", []Data{{Letter: "D"}, {Strikethrough: true}}, Decoration{Badge: "D", Strikethrough: true}},
		{"colors repeated once", []Data{{Color: "a"}, {Color: "a"}, {Color: "b"}}, Decoration{Colors: []string{"a", "b"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Options{})
			uri := "file:///x"
			for i := len(tc.data) - 1; i >= 0; i-- { // the last registered is asked first
				d := tc.data[i]
				mustRegister(t, s, "p", ProviderFunc(func(string) (Data, bool) { return d, true }))
			}
			got, ok := s.Get(uri, false)
			if !ok {
				t.Fatal("no decoration")
			}
			if got.Badge != tc.want.Badge || got.Icon != tc.want.Icon || got.Tooltip != tc.want.Tooltip || got.Strikethrough != tc.want.Strikethrough ||
				strings.Join(got.Colors, ",") != strings.Join(tc.want.Colors, ",") || got.Bubble {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBubbling(t *testing.T) {
	t.Run("a folder with changed children is the dot with the color of the heaviest", func(t *testing.T) {
		s := New(Options{})
		p := &table{}
		reg := mustRegister(t, s, "git", p)
		p.set("file:///r/a/x.go", Data{Weight: 12, Color: "modified", Letter: "M", Bubble: true})
		p.set("file:///r/a/b/y.go", Data{Weight: 14, Color: "conflict", Letter: "!", Bubble: true})
		reg.Fire("file:///r/a/x.go", "file:///r/a/b/y.go")
		for _, folder := range []string{"file:///r", "file:///r/a", "file:///r/a/"} {
			d, ok := s.Get(folder, true)
			if !ok || !d.Bubble || d.Badge != "" || d.Tooltip != BubbleTooltip || len(d.Colors) != 2 || d.Colors[0] != "conflict" {
				t.Errorf("%s: %+v, %v", folder, d, ok)
			}
		}
		d, ok := s.Get("file:///r/a/b", true)
		if !ok || len(d.Colors) != 1 || d.Colors[0] != "conflict" {
			t.Errorf("file:///r/a/b: %+v, %v", d, ok)
		}
		if _, ok := s.Get("file:///r/c", true); ok {
			t.Error("a sibling folder is decorated")
		}
		if _, ok := s.Get("file:///r/a", false); ok {
			t.Error("a folder asked without its children is decorated")
		}
	})

	t.Run("a deleted file that does not bubble leaves the folder alone, and the folder follows the file when it is committed", func(t *testing.T) {
		s := New(Options{})
		p := &table{}
		reg := mustRegister(t, s, "git", p)
		p.set("file:///r/d/gone.go", Data{Color: "deleted", Letter: "D"})
		p.set("file:///r/d/new.go", Data{Color: "added", Letter: "A", Bubble: true})
		reg.Fire("file:///r/d/gone.go", "file:///r/d/new.go")
		d, _ := s.Get("file:///r/d", true)
		if !d.Bubble || len(d.Colors) != 1 || d.Colors[0] != "added" {
			t.Errorf("folder = %+v, want the added color only", d)
		}
		p.remove("file:///r/d/new.go")
		reg.Fire("file:///r/d/new.go")
		if d, ok := s.Get("file:///r/d", true); ok {
			t.Errorf("folder = %+v after the file was committed, want none", d)
		}
	})

	t.Run("a folder with its own answer and bubbled marks shows the dot", func(t *testing.T) {
		s := New(Options{})
		p := &table{}
		reg := mustRegister(t, s, "git", p)
		p.set("file:///r/sub", Data{Color: "submodule", Letter: "S", Tooltip: "Submodule"})
		p.set("file:///r/sub/f.go", Data{Color: "modified", Letter: "M", Bubble: true})
		reg.Fire("file:///r/sub", "file:///r/sub/f.go")
		d, _ := s.Get("file:///r/sub", true)
		if !d.Bubble || d.Tooltip != BubbleTooltip || len(d.Colors) != 2 {
			t.Errorf("folder = %+v", d)
		}
		d, _ = s.Get("file:///r/sub", false)
		if d.Bubble || d.Badge != "S" || d.Tooltip != "Submodule" {
			t.Errorf("folder without children = %+v", d)
		}
	})

	t.Run("a file found by a question bubbles to the folder and says so", func(t *testing.T) {
		s := New(Options{})
		p := &table{data: map[string]Data{"file:///r/q/f.go": {Color: "c", Bubble: true}}}
		mustRegister(t, s, "git", p)
		rec := &recorder{}
		defer rec.listen(s)()
		if _, ok := s.Get("file:///r/q", true); ok {
			t.Fatal("decorated before the file is known")
		}
		s.Get("file:///r/q/f.go", false)
		c, ok := rec.last()
		if !ok || !c.Affects("file:///r/q") || !c.Affects("file:///r") {
			t.Errorf("event = %+v, %v; want one that affects the folders above the file", c, ok)
		}
		n := rec.count()
		s.Get("file:///r/q/f.go", false)
		if rec.count() != n {
			t.Error("a second question announced a change again")
		}
		if _, ok := s.Get("file:///r/q", true); !ok {
			t.Error("the folder is not decorated")
		}
	})
}

func TestFire(t *testing.T) {
	t.Run("only what changed is announced, with the folders above it", func(t *testing.T) {
		s := New(Options{})
		p := &table{}
		reg := mustRegister(t, s, "git", p)
		rec := &recorder{}
		defer rec.listen(s)()
		p.set("file:///r/a/x.go", Data{Letter: "M"})
		reg.Fire("file:///r/a/x.go", "file:///r/a/y.go")
		c, ok := rec.last()
		if !ok {
			t.Fatal("no event")
		}
		for uri, want := range map[string]bool{"file:///r/a/x.go": true, "file:///r/a": true, "file:///r": true, "file:///r/a/y.go": false, "file:///r/b": false, "file:///other": false} {
			if c.Affects(uri) != want {
				t.Errorf("Affects(%s) = %v, want %v", uri, !want, want)
			}
		}
		n := rec.count()
		reg.Fire("file:///r/a/x.go", "file:///r/a/y.go")
		if rec.count() != n {
			t.Error("firing URIs whose answer did not change announced a change")
		}
	})

	t.Run("no URI forgets everything the provider said and tells where", func(t *testing.T) {
		s := New(Options{})
		p := &table{data: map[string]Data{"file:///r/a": {Letter: "M"}}}
		reg := mustRegister(t, s, "git", p)
		s.Get("file:///r/a", false)
		s.Get("file:///r/b", false)
		rec := &recorder{}
		defer rec.listen(s)()
		p.remove("file:///r/a")
		reg.Fire()
		c, _ := rec.last()
		if !c.Affects("file:///r/a") || c.Affects("file:///r/b") {
			t.Errorf("the change should affect a and not b")
		}
		if _, ok := s.Get("file:///r/a", false); ok {
			t.Error("still decorated")
		}
		if s.Len() != 1 {
			t.Errorf("Len = %d, want 1 (the answer for a, asked again)", s.Len())
		}
	})

	t.Run("a disposed registration does nothing", func(t *testing.T) {
		s := New(Options{})
		p := &table{data: map[string]Data{"file:///a": {Letter: "M"}}}
		reg := mustRegister(t, s, "git", p)
		reg.Dispose()
		reg.Dispose()
		reg.Fire("file:///a")
		if _, ok := s.Get("file:///a", false); ok {
			t.Error("decorated by a disposed provider")
		}
		if s.Len() != 0 {
			t.Errorf("Len = %d, want 0", s.Len())
		}
	})
}

func TestRegisterNilFails(t *testing.T) {
	s := New(Options{})
	_, err := s.Register("none", nil)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("Service.Register.DecorationsProviderMissing") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestPathsOfURIs(t *testing.T) {
	tests := []struct {
		uri, root string
		segs      string
	}{
		{"file:///a/b/c", "file://", "a/b/c"},
		{"file:///a/b/c/", "file://", "a/b/c"},
		{"file://host/a", "file://host", "a"},
		{"foo:bar", "foo://", "bar"},
		{"foo:/folder/file.ts", "foo://", "folder/file.ts"},
		{"git:/a/b?ref=1", "git://", "a/b/?ref=1"},
		{"file:///", "file://", ""},
	}
	for _, tc := range tests {
		root, segs := splitURI(tc.uri)
		if root != tc.root || strings.Join(segs, "/") != tc.segs {
			t.Errorf("splitURI(%q) = %q, %q; want %q, %q", tc.uri, root, strings.Join(segs, "/"), tc.root, tc.segs)
		}
	}
	if got := FileURI("/home/a b/c.go"); got != "file:///home/a b/c.go" {
		t.Errorf("FileURI = %q", got)
	}
	// the escaped and the plain spelling of a path are one place
	for _, pair := range [][2]string{{"file:///home/a%20b/c.go", "file:///home/a b/c.go"}, {"file:///100%/x", "file:///100%/x"}, {"file:///h/%E2%86%93", "file:///h/↓"}} {
		ra, sa := splitURI(pair[0])
		rb, sb := splitURI(pair[1])
		if keyOf(ra, sa) != keyOf(rb, sb) {
			t.Errorf("%q and %q are different places", pair[0], pair[1])
		}
	}
}

func TestCacheIsBounded(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	s := New(Options{MaxEntries: 100, Logger: log})
	p := &table{}
	for i := 0; i < 60; i++ {
		p.set(FileURI("/r/bubble/"+string(rune('a'+i%26))+string(rune('a'+i/26))), Data{Letter: "M", Bubble: true})
	}
	reg := mustRegister(t, s, "git", p)
	// 500 questions about files that have nothing: the "nothing" answers make room for each other
	for i := 0; i < 500; i++ {
		s.Get(FileURI("/r/plain/"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+string(rune('a'+i/676))), false)
		if s.Len() > 100 {
			t.Fatalf("Len = %d, want at most 100", s.Len())
		}
	}
	// the 60 answers that carry a decoration are kept and bubble
	var uris []string
	for u := range p.data {
		uris = append(uris, u)
	}
	reg.Fire(uris...)
	if d, ok := s.Get(FileURI("/r/bubble"), true); !ok || !d.Bubble {
		t.Errorf("folder = %+v, %v; want the dot", d, ok)
	}
	if s.Len() > 100 {
		t.Errorf("Len = %d, want at most 100", s.Len())
	}
	// more decorations than the cache holds: the rest is not kept, and still answered
	for i := 0; i < 100; i++ {
		p.set(FileURI("/r/more/"+string(rune('a'+i%26))+string(rune('a'+i/26))), Data{Letter: "A"})
	}
	var more []string
	for u := range p.data {
		more = append(more, u)
	}
	reg.Fire(more...)
	if s.Len() > 100 {
		t.Errorf("Len = %d after more than the cache holds, want at most 100", s.Len())
	}
	if d, ok := s.Get(FileURI("/r/more/aa"), false); !ok || d.Badge != "A" {
		t.Errorf("an answer that is not kept is not given: %+v, %v", d, ok)
	}
	if !strings.Contains(buf.String(), "cache is full") {
		t.Errorf("the log does not say that the cache is full: %s", buf.String())
	}
}

func TestSlowProviderIsLoggedAndLeftOut(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	now := time.Unix(0, 0)
	s := New(Options{Logger: log, SlowLimit: 3, Now: func() time.Time { return now }})
	calls := 0
	reg := mustRegister(t, s, "slow-one", ProviderFunc(func(uri string) (Data, bool) {
		calls++
		now = now.Add(5 * time.Millisecond) // every answer takes 5 ms
		return Data{Letter: "S"}, true
	}))
	fast := mustRegister(t, s, "fast", ProviderFunc(func(uri string) (Data, bool) { return Data{Letter: "F"}, true }))
	_ = fast
	for i := 0; i < 50; i++ {
		s.Get(FileURI("/r/f"+string(rune('a'+i%26))+string(rune('a'+i/26))), false)
	}
	if calls != 3 {
		t.Errorf("the slow provider was asked %d times, want 3", calls)
	}
	out := buf.String()
	if strings.Count(out, "answered slowly") != 3 || !strings.Contains(out, "slow-one") || !strings.Contains(out, "not asked for new files any more") {
		t.Errorf("log = %s", out)
	}
	if d, ok := s.Get(FileURI("/r/faa"), false); !ok || d.Badge != "S, F" && d.Badge != "F, S" {
		t.Errorf("an answer it gave is kept: %+v, %v", d, ok)
	}
	if d, ok := s.Get(FileURI("/r/zz"), false); !ok || d.Badge != "F" {
		t.Errorf("a new file is answered by the others: %+v, %v", d, ok)
	}
	reg.Fire(FileURI("/r/fb"))
	if calls != 4 {
		t.Errorf("Fire did not ask the slow provider again: calls = %d", calls)
	}
}

func TestConcurrentUse(t *testing.T) {
	s := New(Options{})
	p := &table{}
	reg := mustRegister(t, s, "git", p)
	stop := s.OnChange(func(Change) {})
	defer stop()
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				uri := FileURI("/r/d" + string(rune('a'+i%10)) + "/f" + string(rune('a'+g)))
				p.set(uri, Data{Letter: "M", Bubble: true})
				reg.Fire(uri)
				s.Get(uri, false)
				s.Get(FileURI("/r"), true)
			}
		}()
	}
	wg.Wait()
	if d, ok := s.Get(FileURI("/r"), true); !ok || !d.Bubble {
		t.Errorf("root = %+v, %v", d, ok)
	}
}

func TestDeferRunsOnceTheServiceIsFree(t *testing.T) {
	s := New(Options{})
	var ran []string
	// a provider that has to do something that calls the service gets it done through Defer: here the service is free again by then
	mustRegister(t, s, "p", ProviderFunc(func(uri string) (Data, bool) {
		s.Defer(func() {
			// the lock is released: the service can be used
			s.Get("file:///other", false)
			ran = append(ran, uri)
		})
		return Data{Letter: "M"}, true
	}))
	s.Get("file:///a", false)
	// the function for the first file used the service (which asked the provider about the other file and deferred that too)
	if !slices.Contains(ran, "file:///a") || !slices.Contains(ran, "file:///other") {
		t.Errorf("ran = %v, want the functions for both files", ran)
	}
}

func TestVersionCountsTheChangesThatWereAnnounced(t *testing.T) {
	s := New(Options{})
	p := &table{data: map[string]Data{"file:///a": {Letter: "M"}}}
	v0 := s.Version()
	reg := mustRegister(t, s, "p", p)
	v1 := s.Version()
	if v1 <= v0 {
		t.Errorf("registering did not count: %d, %d", v0, v1)
	}
	s.Get("file:///a", false) // a question announces nothing for a file that does not bubble
	if s.Version() != v1 {
		t.Error("a question counted as a change")
	}
	p.remove("file:///a")
	reg.Fire("file:///a")
	if s.Version() <= v1 {
		t.Error("a change did not count")
	}
}
