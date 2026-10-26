package langfeatures_test

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// symbolTree is a file with a struct that has a method, a function with a closure variable and a constant.
func symbolTree() []DocumentSymbol {
	return []DocumentSymbol{
		{Name: "Server", Kind: lsp.SymbolStruct, Range: rng(2, 0, 9, 1), SelectionRange: rng(2, 5, 2, 11), Children: []DocumentSymbol{
			{Name: "Addr", Kind: lsp.SymbolField, Range: rng(3, 1, 3, 12), SelectionRange: rng(3, 1, 3, 5)},
			{Name: "Start", Kind: lsp.SymbolMethod, Range: rng(5, 0, 8, 1), SelectionRange: rng(5, 17, 5, 22), Children: []DocumentSymbol{
				{Name: "conn", Kind: lsp.SymbolVariable, Range: rng(6, 1, 6, 20), SelectionRange: rng(6, 1, 6, 5)},
			}},
		}},
		{Name: "main", Kind: lsp.SymbolFunction, Range: rng(11, 0, 14, 1), SelectionRange: rng(11, 5, 11, 9)},
		{Name: "Version", Kind: lsp.SymbolConstant, Range: rng(16, 0, 16, 20), SelectionRange: rng(16, 6, 16, 13)},
	}
}

func names(path []*DocumentSymbol) []string {
	out := make([]string, len(path))
	for i, s := range path {
		out[i] = s.Name
	}
	return out
}

func TestSymbolPathAt(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		keep func(DocumentSymbol) bool
		want []string
	}{
		{"inside a variable of a method", Position{Line: 6, Col: 3}, nil, []string{"Server", "Start", "conn"}},
		{"in a method", Position{Line: 7, Col: 2}, nil, []string{"Server", "Start"}},
		{"in a field", Position{Line: 3, Col: 2}, nil, []string{"Server", "Addr"}},
		{"in a function", Position{Line: 12, Col: 1}, nil, []string{"main"}},
		{"between symbols", Position{Line: 10, Col: 0}, nil, nil},
		{"a kind the settings leave out is skipped with its children kept", Position{Line: 6, Col: 3},
			func(s DocumentSymbol) bool { return s.Kind != lsp.SymbolMethod }, []string{"Server", "conn"}},
		{"the end of a range holds the position", Position{Line: 14, Col: 1}, nil, []string{"main"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syms := symbolTree()
			if got := names(SymbolPathAt(syms, tt.pos, tt.keep)); !slices.Equal(got, tt.want) {
				t.Errorf("path = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFlattenAndSortSymbols(t *testing.T) {
	flat := FlattenSymbols(symbolTree())
	var got []string
	for _, f := range flat {
		got = append(got, f.Container+"/"+f.Symbol.Name)
	}
	want := []string{"/Server", "Server/Addr", "Server/Start", "Server.Start/conn", "/main", "/Version"}
	if !slices.Equal(got, want) {
		t.Errorf("flat = %v, want %v", got, want)
	}
	if flat[3].Depth != 2 {
		t.Errorf("depth of conn = %d", flat[3].Depth)
	}
	for _, tt := range []struct {
		order SymbolSortOrder
		want  []string
	}{
		{SortByPosition, []string{"Server", "main", "Version"}},
		{SortByName, []string{"main", "Server", "Version"}},
		{SortByKind, []string{"main", "Version", "Server"}}, // function (12), constant (14), struct (23)
	} {
		syms := SortSymbols(symbolTree(), tt.order)
		got := make([]string, len(syms))
		for i, s := range syms {
			got[i] = s.Name
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("order %s = %v, want %v", tt.order, got, tt.want)
		}
	}
}

func TestSymbolKindNames(t *testing.T) {
	for kind, want := range map[int][2]string{
		lsp.SymbolFunction: {"Functions", "functions"}, lsp.SymbolEnumMember: {"EnumMembers", "enumeration members"},
		lsp.SymbolTypeParameter: {"TypeParameters", "type parameters"}, lsp.SymbolFile: {"Files", "files"},
	} {
		if SymbolKindSetting(kind) != want[0] || SymbolKindLabel(kind) != want[1] {
			t.Errorf("kind %d = %q/%q", kind, SymbolKindSetting(kind), SymbolKindLabel(kind))
		}
	}
	if SymbolKindSetting(0) != "" || SymbolKindSetting(99) != "" || SymbolKindLabel(99) != "properties" {
		t.Error("kinds out of range")
	}
	if SymbolKindCount != 26 {
		t.Errorf("%d kinds", SymbolKindCount)
	}
}

// slowSymbols is a symbol provider whose answer waits for the test, and which counts its requests.
type slowSymbols struct {
	mu      sync.Mutex
	calls   []int
	gate    chan struct{}
	syms    []DocumentSymbol
	err     error
	started chan int
}

func (p *slowSymbols) ProvideDocumentSymbols(ctx context.Context, doc Document) ([]DocumentSymbol, error) {
	p.mu.Lock()
	p.calls = append(p.calls, doc.Version)
	gate, syms, err := p.gate, p.syms, p.err
	p.mu.Unlock()
	if p.started != nil {
		p.started <- doc.Version
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return syms, err
}

func (p *slowSymbols) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func versioned(path string, version int) Document {
	d := docOf("go", path, "package main\n")
	d.Version = version
	return d
}

func TestSymbolCacheSharesOneRequestPerVersion(t *testing.T) {
	reg := NewRegistry()
	p := &slowSymbols{gate: make(chan struct{}), syms: symbolTree(), started: make(chan int, 4)}
	reg.RegisterSymbols("a", Selector{Languages: []string{"go"}}, p)
	cache := NewSymbolCache(t.Context(), reg)
	doc := versioned("/work/a.go", 3)
	var wg sync.WaitGroup
	results := make([][]DocumentSymbol, 3)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			syms, err := cache.Get(context.Background(), doc)
			if err != nil {
				t.Errorf("get: %v", err)
			}
			results[i] = syms
		}()
	}
	<-p.started
	if _, ok := cache.Cached(doc); ok {
		t.Error("cached while the request is pending")
	}
	close(p.gate)
	wg.Wait()
	for _, r := range results {
		if len(r) != 3 {
			t.Errorf("symbols = %d", len(r))
		}
	}
	if p.count() != 1 {
		t.Errorf("%d requests for 3 callers", p.count())
	}
	if _, ok := cache.Cached(doc); !ok {
		t.Error("the answer is not cached")
	}
	if _, err := cache.Get(context.Background(), doc); err != nil || p.count() != 1 {
		t.Errorf("a second get asked again: %d, %v", p.count(), err)
	}
	// another version asks again; an older one is answered without being kept
	if _, err := cache.Get(context.Background(), versioned("/work/a.go", 4)); err != nil || p.count() != 2 {
		t.Errorf("new version: %d requests, %v", p.count(), err)
	}
	if _, err := cache.Get(context.Background(), versioned("/work/a.go", 2)); err != nil || p.count() != 3 {
		t.Errorf("older version: %d requests, %v", p.count(), err)
	}
	if _, ok := cache.Cached(versioned("/work/a.go", 4)); !ok {
		t.Error("the older request replaced the newer entry")
	}
}

func TestSymbolCacheDropsASupersededRequest(t *testing.T) {
	reg := NewRegistry()
	p := &slowSymbols{gate: make(chan struct{}), syms: symbolTree(), started: make(chan int, 4)}
	reg.RegisterSymbols("a", Selector{}, p)
	cache := NewSymbolCache(t.Context(), reg)
	var stale atomic.Value
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := cache.Get(context.Background(), versioned("/work/a.go", 1))
		stale.Store(err)
	}()
	<-p.started
	go func() { _, _ = cache.Get(context.Background(), versioned("/work/a.go", 2)) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the superseded request is still waiting")
	}
	if err, _ := stale.Load().(error); err != context.Canceled {
		t.Errorf("stale answer = %v, want context.Canceled", err)
	}
	close(p.gate)
}

func TestSymbolCacheCallerCancelDoesNotStopTheRequest(t *testing.T) {
	reg := NewRegistry()
	p := &slowSymbols{gate: make(chan struct{}), syms: symbolTree(), started: make(chan int, 4)}
	reg.RegisterSymbols("a", Selector{}, p)
	cache := NewSymbolCache(t.Context(), reg)
	doc := versioned("/work/a.go", 1)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := cache.Get(ctx, doc); errc <- err }()
	<-p.started
	cancel()
	if err := <-errc; err != context.Canceled {
		t.Errorf("cancelled caller = %v", err)
	}
	close(p.gate)
	if syms, err := cache.Get(context.Background(), doc); err != nil || len(syms) != 3 || p.count() != 1 {
		t.Errorf("second caller: %d symbols, %v, %d requests", len(syms), err, p.count())
	}
}

func TestSymbolCacheForgetAndErrors(t *testing.T) {
	reg := NewRegistry()
	p := &slowSymbols{err: context.DeadlineExceeded}
	reg.RegisterSymbols("a", Selector{}, p)
	cache := NewSymbolCache(t.Context(), reg)
	doc := versioned("/work/a.go", 1)
	if _, err := cache.Get(context.Background(), doc); err != context.DeadlineExceeded {
		t.Errorf("error = %v, want the provider's", err)
	}
	cache.Forget(doc.URI)
	if _, ok := cache.Cached(doc); ok {
		t.Error("cached after Forget")
	}
	p.mu.Lock()
	p.err, p.syms = nil, symbolTree()
	p.mu.Unlock()
	if syms, err := cache.Get(context.Background(), doc); err != nil || len(syms) != 3 {
		t.Errorf("after Forget = %d, %v", len(syms), err)
	}
	// the cache keeps a bounded number of documents
	for i := range 80 {
		d := docOf("go", "/work/f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".go", "x")
		if _, err := cache.Get(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := cache.Cached(doc); ok {
		t.Error("the oldest entry was not evicted")
	}
}

// fakeWorkspace is a workspace symbol provider.
type fakeWorkspace struct {
	syms  []WorkspaceSymbol
	err   error
	asked []string
	mu    sync.Mutex
}

func (f *fakeWorkspace) ProvideWorkspaceSymbols(_ context.Context, q string) ([]WorkspaceSymbol, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, q)
	return f.syms, f.err
}

func TestWorkspaceSymbolsAreJoinedInProviderOrder(t *testing.T) {
	reg := NewRegistry()
	a := &fakeWorkspace{syms: []WorkspaceSymbol{{Name: "A"}}}
	b := &fakeWorkspace{syms: []WorkspaceSymbol{{Name: "B"}}}
	bad := &fakeWorkspace{err: context.DeadlineExceeded}
	reg.RegisterWorkspaceSymbols("a", a)
	reg.RegisterWorkspaceSymbols("bad", bad)
	remove := reg.RegisterWorkspaceSymbols("b", b)
	got, errs := reg.WorkspaceSymbols(context.Background(), "q")
	var ns []string
	for _, s := range got {
		ns = append(ns, s.Name+"@"+s.Owner)
	}
	if want := []string{"B@b", "A@a"}; !slices.Equal(ns, want) {
		t.Errorf("symbols = %v, want %v (the newer provider first)", ns, want)
	}
	if len(errs) != 1 || errs[0].Owner != "bad" {
		t.Errorf("errors = %v", errs)
	}
	if !slices.Equal(a.asked, []string{"q"}) {
		t.Errorf("asked = %v", a.asked)
	}
	remove()
	if got, _ := reg.WorkspaceSymbols(context.Background(), "q"); len(got) != 1 {
		t.Errorf("after remove: %d", len(got))
	}
	if !reg.Has(docOf("go", "/w/a.go", ""), FeatureWorkspaceSymbols) {
		t.Error("no workspace symbol feature")
	}
}

func TestSettingsExistForEverySymbolKind(t *testing.T) {
	reg := settings.DefaultRegistry()
	for kind := 1; kind <= SymbolKindCount; kind++ {
		for _, prefix := range []string{"outline.show", "breadcrumbs.show"} {
			key := prefix + SymbolKindSetting(kind)
			if _, ok := reg.Lookup(key); !ok {
				t.Errorf("setting %s is not registered", key)
			}
		}
	}
}
