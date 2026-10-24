package document

import (
	"context"
	"sort"
	"sync"

	"github.com/rm4n0s/errors"
)

// VirtualProvider generates the content of one read-only virtual document (SPEC §7.1, the `emetos-virtual:`
// scheme): the default settings JSON, for one.
type VirtualProvider struct {
	// Label is the name of the document in its tab ("Default Settings").
	Label string
	// Language is the language id of the content; plaintext when empty.
	Language string
	// Text generates the content, with "\n" lines. It runs each time the document is opened, so the content
	// reflects what the provider reads at that time. It must stop when ctx does.
	Text func(ctx context.Context) (string, error)
}

// VirtualRegistry holds the providers of the virtual documents, by the path of their URI
// (`emetos-virtual:/defaultSettings.json`). It is safe for concurrent use. There is no global one: the process
// makes its own.
type VirtualRegistry struct {
	mu        sync.Mutex
	providers map[string]VirtualProvider
}

// NewVirtualRegistry makes an empty registry.
func NewVirtualRegistry() *VirtualRegistry {
	return &VirtualRegistry{providers: map[string]VirtualProvider{}}
}

// Register adds the provider of the virtual documents whose URI has the path. It fails with the tags
// VirtualProviderInvalid (no path, or no Text) and VirtualProviderDuplicate.
func (r *VirtualRegistry) Register(path string, p VirtualProvider) error {
	if path == "" || p.Text == nil {
		return errors.New("VirtualProviderInvalid", "a virtual document provider needs a path and a function that makes the text", "path", path)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[path]; ok {
		return errors.New("VirtualProviderDuplicate", "a provider is registered for the path already", "path", path)
	}
	r.providers[path] = p
	return nil
}

// Has tells whether a URI is a virtual one that a provider makes.
func (r *VirtualRegistry) Has(uri URI) bool {
	if uri.Scheme != SchemeVirtual {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.providers[uri.Path]
	return ok
}

// Paths are the paths of the registered providers, sorted.
func (r *VirtualRegistry) Paths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.providers))
	for path := range r.providers {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// Generate asks the provider of a URI for the content. It fails with the tags VirtualURIUnknown (not an
// `emetos-virtual:` URI, or no provider has the path) and VirtualGenerateFailed (the provider failed).
func (r *VirtualRegistry) Generate(ctx context.Context, uri URI) (VirtualContent, error) {
	r.mu.Lock()
	p, ok := r.providers[uri.Path]
	r.mu.Unlock()
	if uri.Scheme != SchemeVirtual || !ok {
		return VirtualContent{}, errors.New("VirtualURIUnknown", "no provider makes this virtual document", "uri", uri.String())
	}
	text, err := p.Text(ctx)
	if err != nil {
		return VirtualContent{}, errors.NewErr("VirtualGenerateFailed", err).SetMetadata("uri", uri.String())
	}
	lang := p.Language
	if lang == "" {
		lang = DefaultLanguage
	}
	return VirtualContent{Text: normalizeNewlines(text), Label: p.Label, Language: lang}, nil
}

// VirtualContent is what a provider made.
type VirtualContent struct {
	// Text is the content, with "\n" lines.
	Text string
	// Label and Language are those of the provider.
	Label, Language string
}

// NewVirtual makes the read-only document of generated content: never dirty, never saved.
func NewVirtual(uri URI, c VirtualContent) *Document {
	opts := NewOptions()
	opts.LanguageID, opts.ReadOnly, opts.Label = c.Language, true, c.Label
	return New(uri, c.Text, opts)
}
