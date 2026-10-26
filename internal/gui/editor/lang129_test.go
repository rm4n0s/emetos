package editor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// lang129Rig is an editor over a Go text with the providers of step 12.9 the tests register, a clock they drive and the posts of
// the answers they let through.
type lang129Rig struct {
	t     *testing.T
	vm    *ViewModel
	reg   *langfeatures.Registry
	cache *langfeatures.SemanticTokensCache
	clock *fakeTimers
	posts chan func()

	mu       sync.Mutex
	executed []langfeatures.Command
	opened   []langfeatures.Location
	logged   []string
}

func newLang129Rig(t *testing.T, text string, tweak func(*Options)) *lang129Rig {
	t.Helper()
	doc := document.New(document.FileURI("/sample.go"), text, document.NewOptions())
	doc.SetLanguageID("go")
	opts := goOptions()
	opts.CursorBlinking, opts.OccurrencesHighlight, opts.SelectionHighlight = "solid", "off", false
	opts.Lens = LensOptions{CodeLens: true, InlayHints: "on", InlayMaxLength: 43, Semantic: SemanticOn}
	opts.WordSeparators = edit.DefaultWordSeparators
	opts.FoldingStrategy = "auto"
	opts.Hover = HoverOptions{Enabled: "on", Delay: 300, Sticky: true, HidingDelay: 300, Above: true}
	if tweak != nil {
		tweak(&opts)
	}
	vm := NewViewModel(doc, opts, nil)
	r := &lang129Rig{t: t, vm: vm, reg: langfeatures.NewRegistry(), clock: &fakeTimers{}, posts: make(chan func(), 256)}
	r.cache = langfeatures.NewSemanticTokensCache(r.reg)
	vm.SetTimer(r.clock.after)
	vm.SetPoster(func(fn func()) { r.posts <- fn })
	t.Cleanup(func() {
		vm.DisposeInlayHints()
		vm.DisposeCodeLenses()
		vm.DisposeSemanticTokens()
		vm.HideHover()
	})
	return r
}

// host sets the suggest host of the editor, which starts the requests of the providers that are registered.
func (r *lang129Rig) host() {
	r.vm.SetSuggest(&SuggestHost{
		Ctx: r.t.Context(), Features: r.reg, Semantic: r.cache,
		Execute: func(_ context.Context, c langfeatures.Command) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.executed = append(r.executed, c)
			return nil
		},
		Open: func(loc langfeatures.Location, _ OpenLocation) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.opened = append(r.opened, loc)
		},
		Log: func(msg string, err error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.logged = append(r.logged, msg)
		},
	})
}

func (r *lang129Rig) selector() langfeatures.Selector {
	return langfeatures.Selector{Languages: []string{"go"}}
}

// settle runs the posts of the answers until none comes for a while.
func (r *lang129Rig) settle() {
	for {
		select {
		case fn := <-r.posts:
			fn()
		case <-time.After(40 * time.Millisecond):
			return
		}
	}
}

func (r *lang129Rig) typeAt(l, c int, text string) {
	r.t.Helper()
	if _, err := r.vm.Document().Edit([]document.Edit{{Range: rg(l, c, l, c), Text: text}}, document.EditOptions{}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *lang129Rig) executedCommands() []langfeatures.Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]langfeatures.Command(nil), r.executed...)
}

// ---- providers ----

// hintsProvider answers the inlay hints the test gives, counting the requests and holding them while its gate is closed.
type hintsProvider struct {
	mu       sync.Mutex
	hints    []langfeatures.InlayHint
	ranges   []buffer.Range
	versions []int
	gate     chan struct{}
	resolved int
	err      error
}

func (p *hintsProvider) ProvideInlayHints(ctx context.Context, doc langfeatures.Document, r langfeatures.Range) ([]langfeatures.InlayHint, error) {
	p.mu.Lock()
	p.ranges = append(p.ranges, r)
	p.versions = append(p.versions, doc.Version)
	gate, hints, err := p.gate, append([]langfeatures.InlayHint(nil), p.hints...), p.err
	p.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return hints, err
}

func (p *hintsProvider) CanResolveInlayHint(h langfeatures.InlayHint) bool {
	return len(h.Parts) > 0 && h.Parts[0].Tooltip == nil
}

func (p *hintsProvider) ResolveInlayHint(_ context.Context, _ langfeatures.Document, h langfeatures.InlayHint) (langfeatures.InlayHint, error) {
	p.mu.Lock()
	p.resolved++
	p.mu.Unlock()
	h.Parts = []langfeatures.InlayHintPart{{Value: h.Parts[0].Value, Tooltip: &langfeatures.Markup{Value: "resolved tooltip"}}}
	return h, nil
}

func (p *hintsProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.ranges)
}

func hint(line, col int, label string) langfeatures.InlayHint {
	return langfeatures.InlayHint{Position: buffer.Position{Line: line, Col: col}, Parts: []langfeatures.InlayHintPart{{Value: label}}}
}

// lensProvider answers lenses, some without a command, and resolves them.
type lensProvider struct {
	mu       sync.Mutex
	lenses   []langfeatures.CodeLens
	calls    int
	resolved int
}

func (p *lensProvider) ProvideCodeLenses(context.Context, langfeatures.Document) ([]langfeatures.CodeLens, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return append([]langfeatures.CodeLens(nil), p.lenses...), nil
}

func (p *lensProvider) CanResolveCodeLens(l langfeatures.CodeLens) bool { return !l.Resolved() }

func (p *lensProvider) ResolveCodeLens(_ context.Context, _ langfeatures.Document, l langfeatures.CodeLens) (langfeatures.CodeLens, error) {
	p.mu.Lock()
	p.resolved++
	p.mu.Unlock()
	l.Command = &langfeatures.Command{Title: "resolved", ID: "resolved.cmd"}
	return l, nil
}

func lens(line int, title string) langfeatures.CodeLens {
	l := langfeatures.CodeLens{Range: rg(line, 0, line, 4)}
	if title != "" {
		l.Command = &langfeatures.Command{Title: title, ID: "cmd." + title, Owner: "srv"}
	}
	return l
}
