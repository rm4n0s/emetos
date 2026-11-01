package quickdiff

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// rig is a model over a document with a service whose providers a test sets, and a Post that a test pumps.
type rig struct {
	t     *testing.T
	texts *fakeTexts
	svc   *Service
	doc   *document.Document
	model *Model
	posts chan func()
	asked atomic.Int32
}

const rigDelay = 5 * time.Millisecond

// newRig makes the rig with the text of the document and the text of its original ("orig:a"), which is set before the model starts:
// the model asks for it after rigDelay, and a test that set it later could lose that race on a busy machine.
func newRig(t *testing.T, text, original string, mutate func(*Config)) *rig {
	t.Helper()
	r := &rig{t: t, texts: newTexts(), posts: make(chan func(), 64)}
	r.texts.set("orig:a", original)
	r.svc = NewService(Options{Texts: r.texts.read})
	r.doc = document.New(document.FileURI("/repo/a.txt"), text, document.NewOptions())
	cfg := Config{Post: func(fn func()) { r.posts <- fn }, Delay: rigDelay}
	if mutate != nil {
		mutate(&cfg)
	}
	r.svc.Add(Provider{Label: "Working Tree", Kind: KindPrimary, RootURI: "file:///repo", Original: func(_ context.Context, uri string) (string, error) {
		r.asked.Add(1)
		return "orig:a", nil
	}})
	r.model = NewModel(r.svc, r.doc, cfg)
	t.Cleanup(r.model.Close)
	return r
}

// pump runs the posted function that delivers the next result.
func (r *rig) pump() {
	r.t.Helper()
	select {
	case fn := <-r.posts:
		fn()
	case <-time.After(5 * time.Second):
		r.t.Fatal("no result was posted")
	}
}

// settle runs posted results until the model's result is for the version of the document.
func (r *rig) settle() Result {
	r.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if res := r.model.Result(); res.Version == r.doc.Version() {
			return res
		}
		select {
		case fn := <-r.posts:
			fn()
		case <-deadline:
			r.t.Fatalf("the model did not catch up with version %d (has %d)", r.doc.Version(), r.model.Result().Version)
		}
	}
}

func (r *rig) edit(line, col int, text string) {
	r.t.Helper()
	at := buffer.Position{Line: line, Col: col}
	if _, err := r.doc.Edit([]document.Edit{{Range: buffer.Range{Start: at, End: at}, Text: text}}, document.EditOptions{}); err != nil {
		r.t.Fatal(err)
	}
}

func changesOf(res Result) []Change {
	var out []Change
	for _, c := range res.Changes {
		out = append(out, c.Change)
	}
	return out
}

func equalChanges(a, b []Change) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestModelComparesWithTheOriginal(t *testing.T) {
	r := newRig(t, "a\nB\nc\nd\n", "a\nb\nc\n", nil)
	res := r.settle()
	if got, want := changesOf(res), []Change{{2, 2, 2, 2}, {3, 0, 4, 4}}; !equalChanges(got, want) {
		t.Fatalf("changes = %+v, want %+v", got, want)
	}
	if len(res.Providers) != 1 || res.Providers[0].Label != "Working Tree" || res.Providers[0].Kind != KindPrimary || res.Providers[0].Original != "orig:a" {
		t.Errorf("providers = %+v", res.Providers)
	}
	for _, c := range res.Changes {
		if c.Kind != KindPrimary || c.ProviderID != res.Providers[0].ID {
			t.Errorf("change %+v is not the primary provider's", c)
		}
	}
}

func TestModelTellsOnlyAboutNewResults(t *testing.T) {
	r := newRig(t, "a\nB\nc\n", "a\nb\nc\n", nil)
	var told int
	r.model.OnChange(func() { told++ })
	r.settle()
	if told != 1 {
		t.Fatalf("told %d times after the first result, want 1", told)
	}
	// typing a letter and deleting it again before the pause ends: the result has the same changes, and nobody is told
	r.edit(2, 1, "x")
	if _, err := r.doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 2, Col: 1}, End: buffer.Position{Line: 2, Col: 2}}}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if res := r.settle(); res.Version != r.doc.Version() {
		t.Fatalf("version = %d, want %d", res.Version, r.doc.Version())
	}
	if told != 1 {
		t.Errorf("told %d times about an unchanged result", told)
	}
	// a real change is told once
	r.edit(0, 1, "x")
	res := r.settle()
	if told != 2 {
		t.Errorf("told %d times, want 2", told)
	}
	if got, want := changesOf(res), []Change{{1, 2, 1, 2}}; !equalChanges(got, want) {
		t.Errorf("changes = %+v, want %+v", got, want)
	}
	stop := r.model.OnChange(func() { told += 100 })
	stop()
	r.edit(0, 0, "y")
	r.settle()
	if told != 2 {
		t.Errorf("told %d times, a listener that was stopped was called or the change was the same", told)
	}
}

func TestModelAsksTheProvidersOnlyWhenTheOriginalsMayHaveChanged(t *testing.T) {
	r := newRig(t, "a\nb\n", "a\nb\n", nil)
	r.settle()
	asked, reads := r.asked.Load(), r.texts.count()
	if asked != 1 || reads != 1 {
		t.Fatalf("asked %d times and read %d, want 1 and 1", asked, reads)
	}

	// typing compares again with the original it has
	for i := range 5 {
		r.edit(0, 0, fmt.Sprint(i))
	}
	res := r.settle()
	if r.asked.Load() != asked || r.texts.count() != reads {
		t.Errorf("typing asked the providers again: %d asks and %d reads", r.asked.Load(), r.texts.count())
	}
	if got, want := changesOf(res), []Change{{1, 1, 1, 1}}; !equalChanges(got, want) {
		t.Errorf("changes = %+v, want %+v", got, want)
	}

	// the original changes and the service says so: the providers are asked again, once for any number of Refresh calls
	r.texts.set("orig:a", "43210a\nb\n")
	for range 3 {
		r.svc.Refresh("file:///repo")
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(changesOf(r.model.Result())) != 0 {
		select {
		case fn := <-r.posts:
			fn()
		case <-time.After(time.Until(deadline)):
			t.Fatalf("the refreshed original was not used: %+v", r.model.Result())
		}
	}
	if r.asked.Load() != asked+1 {
		t.Errorf("asked %d times after the refreshes, want %d", r.asked.Load(), asked+1)
	}

	// a refresh below another folder does not concern the file
	before := r.asked.Load()
	r.svc.Refresh("file:///other")
	time.Sleep(4 * rigDelay)
	if r.asked.Load() != before {
		t.Error("a refresh of another folder asked the providers")
	}
}

func TestModelWithoutAProviderHasNoChanges(t *testing.T) {
	texts := newTexts()
	svc := NewService(Options{Texts: texts.read})
	doc := document.New(document.FileURI("/repo/a.txt"), "anything\n", document.NewOptions())
	posts := make(chan func(), 8)
	m := NewModel(svc, doc, Config{Post: func(fn func()) { posts <- fn }, Delay: rigDelay})
	defer m.Close()
	select {
	case fn := <-posts:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
	}
	if res := m.Result(); len(res.Changes) != 0 || len(res.Providers) != 0 {
		t.Errorf("result = %+v, want none", res)
	}
	// a provider that arrives later is used
	texts.set("orig:a.txt", "other\n")
	svc.Add(Provider{Label: "P", RootURI: "file:///repo", Original: original})
	deadline := time.After(5 * time.Second)
	for len(m.Result().Changes) == 0 {
		select {
		case fn := <-posts:
			fn()
		case <-deadline:
			t.Fatal("the new provider was not used")
		}
	}
}

func TestModelIgnoresTrimWhitespaceWhenTold(t *testing.T) {
	var ignore atomic.Bool
	r := newRig(t, "a\nb  \n", "a\n  b\n", func(c *Config) { c.IgnoreTrimWhitespace = ignore.Load })
	if got := changesOf(r.settle()); len(got) != 1 {
		t.Fatalf("changes = %+v, want the white space change", got)
	}
	ignore.Store(true)
	r.model.Recompute()
	for len(changesOf(r.model.Result())) != 0 {
		r.pump()
	}
}

func TestModelLimits(t *testing.T) {
	big := strings.Repeat("0123456789\n", 30)
	tests := []struct {
		name     string
		text     string
		original string
		want     int
	}{
		{"a text over the size limit has no marks", big + "x\n", "a\n", 0},
		{"an original over the size limit has no marks", "a\n", big + "x\n", 0},
		{"texts under the limit are compared", big[:200], "a\n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.text, tt.original, func(c *Config) { c.MaxSize = 300 })
			res := r.settle()
			if len(res.Changes) != tt.want {
				t.Errorf("%d changes, want %d", len(res.Changes), tt.want)
			}
		})
	}
}

func TestModelClose(t *testing.T) {
	r := newRig(t, "a\nB\n", "a\nb\n", nil)
	r.settle()
	r.model.Close()
	r.model.Close() // the second does nothing
	r.edit(0, 0, "x")
	r.svc.Refresh("")
	select {
	case <-r.posts:
		t.Error("a closed model posted a result")
	case <-time.After(10 * rigDelay):
	}
}

func TestModelFollowsAFileThatIsSavedUnderAnotherName(t *testing.T) {
	r := newRig(t, "a\nB\n", "a\nb\n", nil)
	r.settle()
	r.doc.SetURI(document.FileURI("/other/a.txt"))
	deadline := time.After(5 * time.Second)
	for len(r.model.Result().Providers) != 0 {
		select {
		case fn := <-r.posts:
			fn()
		case <-deadline:
			t.Fatalf("the model still compares the file under its old name: %+v", r.model.Result())
		}
	}
}

func TestModelDropsAResultThatTheTextHasOutgrown(t *testing.T) {
	gate := make(chan struct{})
	texts := newTexts()
	texts.set("orig:a.txt", "a\nb\n")
	svc := NewService(Options{Texts: func(ctx context.Context, uri string) (string, error) {
		select {
		case <-gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return texts.read(ctx, uri)
	}})
	svc.Add(Provider{Label: "P", RootURI: "file:///repo", Original: original})
	doc := document.New(document.FileURI("/repo/a.txt"), "a\nb\n", document.NewOptions())
	posts := make(chan func(), 8)
	m := NewModel(svc, doc, Config{Post: func(fn func()) { posts <- fn }, Delay: rigDelay})
	defer m.Close()
	time.Sleep(4 * rigDelay) // the first computation waits for the original
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 0, Col: 0}, End: buffer.Position{Line: 0, Col: 0}}, Text: "x"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	close(gate)
	// only the computation of the new text reaches the UI thread
	select {
	case fn := <-posts:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
	}
	res := m.Result()
	if res.Version != doc.Version() || len(res.Changes) != 1 {
		t.Errorf("result = %+v, want one change for version %d", res, doc.Version())
	}
	select {
	case <-posts:
		t.Error("the computation of the old text was delivered too")
	case <-time.After(10 * rigDelay):
	}
}

func TestModelSecondaryProviders(t *testing.T) {
	texts := newTexts()
	texts.set("index:a", "a\nb\nc\nd\ne\n")
	texts.set("head:a", "a\nb\nc\nd\ne\nf\ng\n")
	svc := NewService(Options{Texts: texts.read})
	svc.Add(Provider{Label: "Index", Kind: KindSecondary, RootURI: "file:///repo", Original: func(context.Context, string) (string, error) { return "head:a", nil }})
	svc.Add(Provider{Label: "Working Tree", Kind: KindPrimary, RootURI: "file:///repo", Original: func(context.Context, string) (string, error) { return "index:a", nil }})
	// the work tree changes line 2 of the index version; the index differs from HEAD by a deletion at the end, which the work tree has too,
	// so only the work tree's changes and the changes of the secondary provider the primary one does not have are left
	doc := document.New(document.FileURI("/repo/a.txt"), "a\nB\nc\nd\ne\n", document.NewOptions())
	posts := make(chan func(), 8)
	m := NewModel(svc, doc, Config{Post: func(fn func()) { posts <- fn }, Delay: rigDelay})
	defer m.Close()
	for len(m.Result().Providers) != 2 {
		select {
		case fn := <-posts:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatalf("no result: %+v", m.Result())
		}
	}
	res := m.Result()
	if res.Providers[0].Label != "Working Tree" || res.Providers[1].Label != "Index" {
		t.Errorf("providers = %+v, want the primary one first", res.Providers)
	}
	type kc struct {
		kind Kind
		c    Change
	}
	var got []kc
	for _, c := range res.Changes {
		got = append(got, kc{c.Kind, c.Change})
	}
	want := []kc{{KindPrimary, Change{2, 2, 2, 2}}, {KindSecondary, Change{6, 7, 5, 0}}}
	if len(got) != len(want) {
		t.Fatalf("changes = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestModelSecondaryChangeEqualToThePrimaryOneIsLeftOut(t *testing.T) {
	texts := newTexts()
	texts.set("index:a", "a\nb\nc\n")
	texts.set("head:a", "a\nb\nc\n")
	svc := NewService(Options{Texts: texts.read})
	svc.Add(Provider{Label: "Working Tree", Kind: KindPrimary, RootURI: "file:///repo", Original: func(context.Context, string) (string, error) { return "index:a", nil }})
	svc.Add(Provider{Label: "Index", Kind: KindSecondary, RootURI: "file:///repo", Original: func(context.Context, string) (string, error) { return "head:a", nil }})
	doc := document.New(document.FileURI("/repo/a.txt"), "a\nB\nc\n", document.NewOptions())
	posts := make(chan func(), 8)
	m := NewModel(svc, doc, Config{Post: func(fn func()) { posts <- fn }, Delay: rigDelay})
	defer m.Close()
	for len(m.Result().Providers) != 2 {
		select {
		case fn := <-posts:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatalf("no result: %+v", m.Result())
		}
	}
	if got := changesOf(m.Result()); len(got) != 1 {
		t.Errorf("changes = %+v, want the primary change only", got)
	}
}

// TestModelAfterRandomEditsEqualsAFreshDiff makes seeded random edits and checks that, once the model catches up, its changes are the
// ones of a diff of the final text.
func TestModelAfterRandomEditsEqualsAFreshDiff(t *testing.T) {
	var base []string
	for i := range 40 {
		base = append(base, fmt.Sprintf("line %d", i))
	}
	original := strings.Join(base, "\n") + "\n"
	for seed := int64(1); seed <= 8; seed++ {
		t.Run(fmt.Sprint("seed ", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			r := newRig(t, original, original, nil)
			for range 30 {
				snap := r.doc.Snapshot()
				line := rng.Intn(snap.LineCount())
				text, _ := snap.LineContent(line)
				col := rng.Intn(len(text) + 1)
				var edit document.Edit
				switch rng.Intn(4) {
				case 0: // insert a line
					edit = document.Edit{Range: buffer.Range{Start: buffer.Position{Line: line, Col: col}, End: buffer.Position{Line: line, Col: col}}, Text: "\nnew " + fmt.Sprint(rng.Intn(100))}
				case 1: // delete the rest of the line and its break
					end := buffer.Position{Line: line + 1}
					if line+1 >= snap.LineCount() {
						end = buffer.Position{Line: line, Col: len(text)}
					}
					edit = document.Edit{Range: buffer.Range{Start: buffer.Position{Line: line, Col: col}, End: end}}
				case 2: // type
					edit = document.Edit{Range: buffer.Range{Start: buffer.Position{Line: line, Col: col}, End: buffer.Position{Line: line, Col: col}}, Text: "x"}
				default: // replace a line
					edit = document.Edit{Range: buffer.Range{Start: buffer.Position{Line: line}, End: buffer.Position{Line: line, Col: len(text)}}, Text: "replaced"}
				}
				if _, err := r.doc.Edit([]document.Edit{edit}, document.EditOptions{}); err != nil {
					t.Fatal(err)
				}
				if rng.Intn(5) == 0 {
					r.settle()
				}
			}
			res := r.settle()
			final := linesOf(r.doc.Text())
			want, err := Changes(context.Background(), linesOf(original), final, DiffOptions{TimeLimit: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if got := changesOf(res); !equalChanges(got, want) {
				t.Errorf("changes = %+v, want %+v", got, want)
			}
		})
	}
}

// BenchmarkRecompute10000Lines is the work of one recompute of a file of 10,000 lines with 200 changes (SPEC §14: within 20 ms of diff
// work).
func BenchmarkRecompute10000Lines(b *testing.B) {
	var orig, mod []string
	for i := range 10000 {
		orig = append(orig, fmt.Sprintf("func line%d() { return %d }", i, i))
		if i%50 == 7 {
			mod = append(mod, fmt.Sprintf("func line%d() { return %d + 1 }", i, i))
		} else {
			mod = append(mod, orig[i])
		}
	}
	b.ResetTimer()
	for range b.N {
		changes, err := Changes(context.Background(), orig, mod, DiffOptions{TimeLimit: time.Second})
		if err != nil || len(changes) != 200 {
			b.Fatalf("%d changes, %v", len(changes), err)
		}
	}
}

// BenchmarkModelCompute10000Lines is the work of the model on its goroutine for a document of 10,000 lines and an original with 200
// changes: the lines of the snapshot, those of the original and the diff (SPEC §14: the marks within 50 ms of the end of the pause).
func BenchmarkModelCompute10000Lines(b *testing.B) {
	var orig, mod strings.Builder
	for i := range 10000 {
		fmt.Fprintf(&orig, "func line%d() { return %d }\n", i, i)
		if i%50 == 7 {
			fmt.Fprintf(&mod, "func line%d() { return %d + 1 }\n", i, i)
		} else {
			fmt.Fprintf(&mod, "func line%d() { return %d }\n", i, i)
		}
	}
	doc := document.New(document.FileURI("/repo/a.txt"), mod.String(), document.NewOptions())
	m := &Model{doc: doc, cfg: Config{MaxSize: DefaultMaxSize, TimeLimit: time.Second}}
	originals := []Original{{ProviderID: "p", Label: "P", Kind: KindPrimary, Text: orig.String()}}
	b.ResetTimer()
	for range b.N {
		res, err := m.compute(context.Background(), doc.Snapshot(), doc.Version(), false, originals)
		if err != nil || len(res.Changes) != 200 {
			b.Fatalf("%d changes, %v", len(res.Changes), err)
		}
	}
}

func TestURIOfEscapesAFilePathAsTheExtensionsHaveIt(t *testing.T) {
	for _, tt := range []struct {
		uri  document.URI
		want string
	}{
		{document.FileURI("/repo/a.go"), "file:///repo/a.go"},
		{document.FileURI("/repo/a b/100%.txt"), "file:///repo/a%20b/100%25.txt"},
		{document.FileURI("/repo/#1?.txt"), "file:///repo/%231%3F.txt"},
		{document.UntitledURI(3), "untitled:3"},
	} {
		if got := URIOf(tt.uri); got != tt.want {
			t.Errorf("URIOf(%+v) = %q, want %q", tt.uri, got, tt.want)
		}
	}
	// a file with such a name is compared with the provider of the folder that holds it
	for _, name := range []string{"a b/100%.txt", "#1?.txt"} {
		doc := document.New(document.FileURI("/repo/"+name), "x\n", document.NewOptions())
		if !(Event{Root: "file:///repo"}).Covers(URIOf(doc.URI())) {
			t.Errorf("the file %s is not below file:///repo", name)
		}
	}
}
