package exthost

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/contenttest"
)

// contentHost is the ContentHost the rig gives its scopes: it keeps what the extensions ask of the window.
type contentHost struct {
	mu        sync.Mutex
	multi     map[string]*multiReg
	multiOpen []extension.MultiDiffSpec
	providers map[string]*contentReg
	diffs     []extension.DiffSpec
	docs      []extension.OpenSpec
	tabs      []extension.DocumentTab
	closed    []string
	revealed  []string
}

type contentReg struct {
	h        *contentHost
	p        extension.ContentProvider
	changed  [][]string
	disposed bool
}

func (c *contentReg) Changed(uris ...string) {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	c.changed = append(c.changed, slices.Clone(uris))
}

func (c *contentReg) Dispose() {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	c.disposed = true
	if c.h.providers[c.p.Scheme] == c {
		delete(c.h.providers, c.p.Scheme)
	}
}

func (h *contentHost) ProvideContent(owner string, p extension.ContentProvider) (extension.ContentRegistration, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.providers == nil {
		h.providers = map[string]*contentReg{}
	}
	if h.providers[p.Scheme] != nil {
		return nil, errors.New("ExtContentSchemeTaken", "the scheme is taken")
	}
	r := &contentReg{h: h, p: p}
	h.providers[p.Scheme] = r
	return r, nil
}

type multiReg struct {
	h        *contentHost
	p        extension.MultiDiffProvider
	changed  [][]string
	disposed bool
}

func (m *multiReg) Changed(keys ...string) {
	m.h.mu.Lock()
	defer m.h.mu.Unlock()
	m.changed = append(m.changed, slices.Clone(keys))
}

func (m *multiReg) Dispose() {
	m.h.mu.Lock()
	defer m.h.mu.Unlock()
	m.disposed = true
	if m.h.multi[m.p.Kind] == m {
		delete(m.h.multi, m.p.Kind)
	}
}

func (h *contentHost) ProvideMultiDiff(owner string, p extension.MultiDiffProvider) (extension.MultiDiffRegistration, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.multi == nil {
		h.multi = map[string]*multiReg{}
	}
	if h.multi[p.Kind] != nil {
		return nil, errors.New("ExtMultiDiffKindTaken", "the kind is taken")
	}
	r := &multiReg{h: h, p: p}
	h.multi[p.Kind] = r
	return r, nil
}

func (h *contentHost) OpenMultiDiff(spec extension.MultiDiffSpec) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if spec.Kind != "" && h.multi[spec.Kind] == nil {
		return errors.New("ExtMultiDiffKindUnknown", "no provider makes the sets of this kind", "kind", spec.Kind)
	}
	h.multiOpen = append(h.multiOpen, spec)
	return nil
}

func (h *contentHost) unknown(uri string) error {
	if scheme, _, ok := extension.SplitContentURI(uri); ok && h.providers[scheme] == nil {
		return errors.New("ExtContentUnknown", "no content provider makes this document", "uri", uri)
	}
	return nil
}

func (h *contentHost) OpenDiff(spec extension.DiffSpec) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.unknown(spec.Left); err != nil {
		return err
	}
	if err := h.unknown(spec.Right); err != nil {
		return err
	}
	h.diffs = append(h.diffs, spec)
	return nil
}

func (h *contentHost) OpenDocument(spec extension.OpenSpec) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.unknown(spec.URI); err != nil {
		return err
	}
	h.docs = append(h.docs, spec)
	return nil
}

func (h *contentHost) Tabs() []extension.DocumentTab {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.tabs)
}

func (h *contentHost) CloseTabs(tabs []extension.DocumentTab) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, t := range tabs {
		if i := slices.IndexFunc(h.tabs, func(x extension.DocumentTab) bool { return x.ID == t.ID }); i >= 0 {
			h.tabs = slices.Delete(h.tabs, i, i+1)
			h.closed = append(h.closed, t.ID)
			n++
		}
	}
	return n
}

func (h *contentHost) RevealFile(path string, inOS bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.revealed = append(h.revealed, path)
	if inOS {
		h.revealed[len(h.revealed)-1] += " (os)"
	}
	return nil
}

// contentHarness makes the harness of the conformance suite over a tab of the host whose documents are a contentHost.
func contentHarness(t *testing.T) (*contenttest.Harness, *contentHost, *tab) {
	t.Helper()
	_, tb, api := grabAPI(t, "emetos.contenttest", nil)
	host := tb.docs.contentHost
	provided := func(uri string) *contentReg {
		scheme, _, _ := extension.SplitContentURI(uri)
		host.mu.Lock()
		defer host.mu.Unlock()
		return host.providers[scheme]
	}
	h := &contenttest.Harness{
		API: api,
		Text: func(ctx context.Context, uri string) (string, error) {
			r := provided(uri)
			if r == nil {
				return "", errors.New("ExtContentUnknown", "no content provider makes this document", "uri", uri)
			}
			return r.p.Text(ctx, uri)
		},
		Label: func(uri string) string {
			if r := provided(uri); r != nil && r.p.Label != nil {
				return r.p.Label(uri)
			}
			return ""
		},
		Language: func(uri string) string {
			if r := provided(uri); r != nil && r.p.Language != nil {
				return r.p.Language(uri)
			}
			return ""
		},
		Diffs: func() []extension.DiffSpec { host.mu.Lock(); defer host.mu.Unlock(); return slices.Clone(host.diffs) },
		Docs:  func() []extension.OpenSpec { host.mu.Lock(); defer host.mu.Unlock(); return slices.Clone(host.docs) },
		Changed: func() [][]string {
			host.mu.Lock()
			defer host.mu.Unlock()
			if r := host.providers["tst"]; r != nil {
				return slices.Clone(r.changed)
			}
			return nil
		},
		Deactivate: func() { tb.disabled = []string{"emetos.contenttest"}; tb.scope.Refresh() },
		Failed:     func() bool { return tb.scope.State("emetos.contenttest") == StateFailed },
		Pump:       tb.pump,
		MultiFiles: func(ctx context.Context, kind, key string) ([]extension.MultiDiffResource, error) {
			host.mu.Lock()
			r := host.multi[kind]
			host.mu.Unlock()
			if r == nil {
				return nil, errors.New("ExtMultiDiffKindUnknown", "no provider makes the sets of this kind", "kind", kind)
			}
			return r.p.Resources(ctx, key)
		},
		MultiOpened: func() []extension.MultiDiffSpec {
			host.mu.Lock()
			defer host.mu.Unlock()
			return slices.Clone(host.multiOpen)
		},
		MultiChanged: func() [][]string {
			host.mu.Lock()
			defer host.mu.Unlock()
			if r := host.multi["tst.changes"]; r != nil {
				return slices.Clone(r.changed)
			}
			return nil
		},
	}
	return h, host, tb
}

// TestContentConformance runs the suite that the fake of the tests of the extensions passes too.
func TestContentConformance(t *testing.T) {
	contenttest.Run(t, func(t *testing.T) *contenttest.Harness { h, _, _ := contentHarness(t); return h })
}

func TestTheTabsAndTheRevealOfAFileReachTheWindow(t *testing.T) {
	h, host, _ := contentHarness(t)
	docs := h.API.Documents()
	host.tabs = []extension.DocumentTab{{ID: "1:a", Kind: "diff"}, {ID: "1:b", Kind: "text"}}
	if got := docs.Tabs(); len(got) != 2 {
		t.Fatalf("tabs %v", got)
	}
	if n := docs.CloseTabs(); n != 0 {
		t.Errorf("closed %d with nothing to close", n)
	}
	if n := docs.CloseTabs(extension.DocumentTab{ID: "1:a"}); n != 1 || len(docs.Tabs()) != 1 {
		t.Errorf("closed %d", n)
	}
	if err := h.API.Workspace().RevealFile("file:///work/../work/a.go", false); err != nil {
		t.Fatal(err)
	}
	if err := h.API.Workspace().RevealFile("file:///work/b.go", true); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"emetos-virtual:/x", "file://relative", ""} {
		mustRoute(t, h.API.Workspace().RevealFile(bad, false), "workspaceAPI.RevealFile.ExtensionFilePathInvalid")
	}
	if !slices.Equal(host.revealed, []string{"/work/a.go", "/work/b.go (os)"}) {
		t.Errorf("revealed %v", host.revealed)
	}
}

// bareDocs are documents that can only be read and listed: the window of a scope that shows nothing.
type bareDocs struct{ d *docSource }

func (b bareDocs) Read(ctx context.Context, uri string) (string, error) { return b.d.Read(ctx, uri) }
func (b bareDocs) Open() []string                                       { return b.d.Open() }

func TestAScopeThatCannotShowDocumentsFailsWithTheirTags(t *testing.T) {
	_, _, api := grabAPI(t, "emetos.contenttest", func(r *rig) { r.noContentHost = map[string]bool{"one": true} })
	docs := api.Documents()
	_, err := docs.ProvideContent(extension.ContentProvider{Scheme: "tst", Text: func(context.Context, string) (string, error) { return "", nil }})
	mustRoute(t, err, "documentsAPI.contentHost.ExtensionDocumentUnavailable")
	mustRoute(t, docs.OpenDiff(extension.DiffSpec{Left: "file:///a", Right: "file:///b"}), "documentsAPI.contentHost.ExtensionDocumentUnavailable")
	mustRoute(t, docs.OpenDocument(extension.OpenSpec{URI: "file:///a"}), "documentsAPI.contentHost.ExtensionDocumentUnavailable")
	_, err = docs.ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst", Resources: func(context.Context, string) ([]extension.MultiDiffResource, error) { return nil, nil }})
	mustRoute(t, err, "documentsAPI.contentHost.ExtensionDocumentUnavailable")
	mustRoute(t, docs.OpenMultiDiff(extension.MultiDiffSpec{Kind: "tst", Key: "k"}), "documentsAPI.contentHost.ExtensionDocumentUnavailable")
	if got := docs.Tabs(); len(got) != 0 {
		t.Errorf("tabs %v", got)
	}
	if n := docs.CloseTabs(extension.DocumentTab{ID: "1:a"}); n != 0 {
		t.Errorf("closed %d", n)
	}
	mustRoute(t, api.Workspace().RevealFile("file:///work/a.go", false), "workspaceAPI.RevealFile.ExtensionRevealUnavailable")
}

func TestAPanicInAProviderIsAnErrorWithItsRouteForTheCaller(t *testing.T) {
	h, _, tb := contentHarness(t)
	p := extension.ContentProvider{
		Scheme: "tst",
		Label:  func(string) string { panic("label") },
		Text:   func(context.Context, string) (string, error) { panic("text") },
	}
	if _, err := h.API.Documents().ProvideContent(p); err != nil {
		t.Fatal(err)
	}
	_, err := h.Text(context.Background(), extension.ContentURI("tst", "x"))
	mustRoute(t, err, "documentsAPI.contain.func1.ExtensionContentPanicked")
	if got := h.Label(extension.ContentURI("tst", "x")); got != "" {
		t.Errorf("label %q after a panic", got)
	}
	tb.pump()
	if !h.Failed() {
		t.Error("the extension is on after a panic")
	}
}

func TestMultiDiffRefusalsAndPanicsHaveTheirRoutes(t *testing.T) {
	h, _, tb := contentHarness(t)
	docs := h.API.Documents()
	files := func(context.Context, string) ([]extension.MultiDiffResource, error) { return nil, nil }
	_, err := docs.ProvideMultiDiff(extension.MultiDiffProvider{Kind: "9 no", Resources: files})
	mustRoute(t, err, "documentsAPI.ProvideMultiDiff.ExtensionMultiDiffInvalid")
	_, err = docs.ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst.other"})
	mustRoute(t, err, "documentsAPI.ProvideMultiDiff.ExtensionMultiDiffInvalid")
	mustRoute(t, docs.OpenMultiDiff(extension.MultiDiffSpec{}), "documentsAPI.OpenMultiDiff.ExtensionMultiDiffInvalid")
	mustRoute(t, docs.OpenMultiDiff(extension.MultiDiffSpec{Resources: []extension.MultiDiffResource{{Path: "a"}}}), "documentsAPI.OpenMultiDiff.ExtensionMultiDiffInvalid")
	_, err = docs.ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst.changes", Resources: func(context.Context, string) ([]extension.MultiDiffResource, error) {
		panic("files")
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.MultiFiles(context.Background(), "tst.changes", "k")
	mustRoute(t, err, "documentsAPI.containMultiDiff.func1.1.ExtensionMultiDiffPanicked")
	tb.pump()
	if !h.Failed() {
		t.Error("the extension is on after a panic")
	}
}
