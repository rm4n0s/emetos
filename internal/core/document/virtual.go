package document

import (
	"context"
	"sort"
	"strings"
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

// VirtualPrefixProvider generates the content of the virtual documents whose path starts with a prefix: the rest of
// the path says which content (the file a "Compare with Saved" shows, the number of a clipboard comparison). The
// functions get the rest of the path, which is what follows the prefix.
type VirtualPrefixProvider struct {
	// Label gives the name of the document in its tab for the rest of the path; the rest of the path is the
	// name when it is nil.
	Label func(rest string) string
	// Language gives the language id of the content for the rest of the path; plaintext when nil or empty.
	Language func(rest string) string
	// Text generates the content, with "\n" lines. It runs each time the document is opened and must stop when ctx
	// does.
	Text func(ctx context.Context, rest string) (string, error)
}

// VirtualRegistry holds the providers of the virtual documents, by the path of their URI
// (`emetos-virtual:/defaultSettings.json`) or by the prefix of it (`emetos-virtual:/saved/home/me/main.go`). It is
// safe for concurrent use. There is no global one: the process makes its own.
type VirtualRegistry struct {
	mu        sync.Mutex
	providers map[string]VirtualProvider
	prefixes  map[string]VirtualPrefixProvider
}

// NewVirtualRegistry makes an empty registry.
func NewVirtualRegistry() *VirtualRegistry {
	return &VirtualRegistry{providers: map[string]VirtualProvider{}, prefixes: map[string]VirtualPrefixProvider{}}
}

// RegisterPrefix adds the provider of the virtual documents whose path starts with prefix and has something after
// it. The prefix starts and ends with "/" and must not be "/"; a path that has a provider of its own (Register)
// uses that one, and of two prefixes the longer wins. It fails with the tags VirtualPrefixInvalid (a prefix that is
// not like that, or no Text) and VirtualPrefixDuplicate.
func (r *VirtualRegistry) RegisterPrefix(prefix string, p VirtualPrefixProvider) error {
	if len(prefix) < 3 || !strings.HasPrefix(prefix, "/") || !strings.HasSuffix(prefix, "/") || p.Text == nil {
		return errors.New("VirtualPrefixInvalid", "a prefix of virtual documents starts and ends with / and has a name, and its provider makes the text", "prefix", prefix)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.prefixes[prefix]; ok {
		return errors.New("VirtualPrefixDuplicate", "a provider is registered for the prefix already", "prefix", prefix)
	}
	r.prefixes[prefix] = p
	return nil
}

// byPrefix finds the provider with the longest prefix of a path, and the rest of the path; the caller holds the lock.
func (r *VirtualRegistry) byPrefix(path string) (p VirtualPrefixProvider, rest string, ok bool) {
	best := ""
	for prefix, cand := range r.prefixes {
		if len(path) > len(prefix) && strings.HasPrefix(path, prefix) && len(prefix) > len(best) {
			best, p, ok = prefix, cand, true
		}
	}
	return p, strings.TrimPrefix(path, best), ok
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
	if _, ok := r.providers[uri.Path]; ok {
		return true
	}
	_, _, ok := r.byPrefix(uri.Path)
	return ok
}

// Paths are the paths of the registered providers of single documents, sorted (not the prefixes).
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
	pp, rest, byPrefix := r.byPrefix(uri.Path)
	r.mu.Unlock()
	if uri.Scheme != SchemeVirtual || (!ok && !byPrefix) {
		return VirtualContent{}, errors.New("VirtualURIUnknown", "no provider makes this virtual document", "uri", uri.String())
	}
	var text string
	var err error
	label, lang := p.Label, p.Language
	if ok {
		text, err = p.Text(ctx)
	} else {
		text, err = pp.Text(ctx, rest)
		label, lang = rest, ""
		if pp.Label != nil {
			label = pp.Label(rest)
		}
		if pp.Language != nil {
			lang = pp.Language(rest)
		}
	}
	if err != nil {
		return VirtualContent{}, errors.NewErr("VirtualGenerateFailed", err).SetMetadata("uri", uri.String())
	}
	if lang == "" {
		lang = DefaultLanguage
	}
	return VirtualContent{Text: normalizeNewlines(text), Label: label, Language: lang}, nil
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
