package fakeapi

import (
	"context"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The Quick Diff API of the fake (SPEC §10.30 "Quick diff", step 24.11), with the rules of the host (internal/gui/exthost/quickdiff.go):
// the same errors, the same disposal with the source control and the activation.

// FakeQuickDiff is a quick diff provider the extension registered on a source control.
type FakeQuickDiff struct {
	src      *FakeSource
	p        extension.QuickDiffProvider
	disposed bool
}

var _ extension.QuickDiffRegistration = (*FakeQuickDiff)(nil)

// Label is the label of the provider.
func (q *FakeQuickDiff) Label() string { return q.p.Label }

// Kind is the kind of the provider.
func (q *FakeQuickDiff) Kind() extension.QuickDiffKind { return q.p.Kind }

// Root is the root of the source control the provider is on (file:///path).
func (q *FakeQuickDiff) Root() string { return q.src.root }

// Original asks the provider for the original of the file, as the window does, contained as the host contains a panic: the extension is
// off and the caller gets an error.
func (q *FakeQuickDiff) Original(ctx context.Context, uri string) (original string, err error) {
	defer func() {
		if r := recover(); r != nil {
			q.src.a.scmFail("quick diff provider", r)
			original, err = "", errors.NewErrf("ExtensionQuickDiffPanicked", "it panicked in the Original function of a quick diff provider: %v", r)
		}
	}()
	return q.p.Original(ctx, uri)
}

// Disposed tells whether the provider was disposed, or went with its source control or the activation.
func (q *FakeQuickDiff) Disposed() bool {
	q.src.a.mu.Lock()
	defer q.src.a.mu.Unlock()
	return q.disposed
}

// Dispose implements extension.QuickDiffRegistration.
func (q *FakeQuickDiff) Dispose() {
	q.src.a.mu.Lock()
	defer q.src.a.mu.Unlock()
	q.disposed = true
}

// AddQuickDiffProvider implements extension.SourceControl with the errors of the host: ExtensionDeactivated, ExtensionQuickDiffInvalid,
// ExtensionQuickDiffUnavailable, ExtensionSCMDisposed and ExtensionQuickDiffDuplicate.
func (s *FakeSource) AddQuickDiffProvider(p extension.QuickDiffProvider) (extension.QuickDiffRegistration, error) {
	a := s.a
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.scm.dead:
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	case strings.TrimSpace(p.Label) == "" || p.Original == nil || (p.Kind != extension.QuickDiffPrimary && p.Kind != extension.QuickDiffSecondary) || s.root == "":
		return nil, errors.New("ExtensionQuickDiffInvalid", "a quick diff provider needs a label, a function that finds the original, a known kind and a source control with a root", "label", p.Label, "root", s.root)
	case a.quickDiffUnavailable:
		return nil, errors.New("ExtensionQuickDiffUnavailable", "the scope has no editors to compare")
	case s.disposed:
		return nil, errors.New("ExtensionSCMDisposed", "the source control was disposed", "source", s.id)
	}
	for _, q := range s.quickDiff {
		if !q.disposed && q.p.Kind == p.Kind {
			return nil, errors.New("ExtensionQuickDiffDuplicate", "the source control has a quick diff provider of this kind already", "source", s.id, "label", p.Label)
		}
	}
	q := &FakeQuickDiff{src: s, p: p}
	s.quickDiff = append(s.quickDiff, q)
	return q, nil
}

// RefreshQuickDiff implements extension.SourceControl: the call is counted (QuickDiffRefreshes).
func (s *FakeSource) RefreshQuickDiff() {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.live() {
		s.refreshes++
	}
}

// QuickDiffRefreshes is the number of RefreshQuickDiff calls of the source control.
func (s *FakeSource) QuickDiffRefreshes() int {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	return s.refreshes
}

// QuickDiffProviders are the providers of the source control that are not disposed, in the order they were added.
func (s *FakeSource) QuickDiffProviders() []*FakeQuickDiff {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	var out []*FakeQuickDiff
	for _, q := range s.quickDiff {
		if !q.disposed {
			out = append(out, q)
		}
	}
	return out
}

// SetQuickDiffUnavailable makes AddQuickDiffProvider and Documents().QuickDiffOriginal fail as the host does in a scope that has no
// editors (a host without a workbench), with the tag ExtensionQuickDiffUnavailable.
func (a *API) SetQuickDiffUnavailable(unavailable bool) {
	a.mu.Lock()
	a.quickDiffUnavailable = unavailable
	a.mu.Unlock()
}

// QuickDiffOriginal implements extension.Documents: the original of the first primary provider of a source control whose root holds the
// file that has one, read with the content provider of its scheme (ContentText).
func (d documents) QuickDiffOriginal(ctx context.Context, uri string) (string, bool, error) {
	a := d.a
	a.mu.Lock()
	switch {
	case a.scm.dead:
		a.mu.Unlock()
		return "", false, errors.New("ExtensionDeactivated", "the extension was deactivated")
	case a.quickDiffUnavailable:
		a.mu.Unlock()
		return "", false, errors.New("ExtensionQuickDiffUnavailable", "the scope has no editors to compare")
	}
	var candidates []*FakeQuickDiff
	for _, s := range a.scm.sources {
		if s.root == "" || (uri != s.root && !strings.HasPrefix(uri, strings.TrimSuffix(s.root, "/")+"/")) {
			continue
		}
		for _, q := range s.quickDiff {
			if !q.disposed && q.p.Kind == extension.QuickDiffPrimary {
				candidates = append(candidates, q)
			}
		}
	}
	a.mu.Unlock()
	for _, q := range slices.Clone(candidates) {
		orig, err := q.Original(ctx, uri)
		if err != nil || orig == "" {
			continue
		}
		text, err := a.ContentText(ctx, orig)
		if err != nil {
			continue
		}
		return text, true, nil
	}
	return "", false, nil
}
