package fakeapi

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The Timeline API of the fake (SPEC §10.30 "The Timeline view", step 24.21): the providers an extension registered, in memory, with the
// rules of the host (internal/gui/exthost/timeline.go): the same errors, the same disposal with the activation, an item with no ID
// dropped, a panic of the provider taking the extension off. extension/timelinetest runs one suite on this and on the host.

// timelineState is the timeline state of the fake; the API's mutex guards it.
type timelineState struct {
	// providers are the registered providers, in the order of the calls.
	providers []*FakeTimeline
	// unavailable makes RegisterProvider fail as it does in a scope with no Timeline view.
	unavailable bool
}

// FakeTimeline is a provider the extension registered: a test asks it for pages as the view would, and reads what the extension said
// changed.
type FakeTimeline struct {
	a        *API
	p        extension.TimelineProvider
	changed  [][]string
	disposed bool
}

// Timeline implements extension.API.
func (a *API) Timeline() extension.Timelines { return timelineAPI{a} }

type timelineAPI struct{ a *API }

// SetTimelineUnavailable makes RegisterProvider fail with ExtensionTimelineUnavailable, as it does in a host with no workbench.
func (a *API) SetTimelineUnavailable(unavailable bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.timeline.unavailable = unavailable
}

// RegisterProvider implements extension.Timelines with the errors of the host.
func (t timelineAPI) RegisterProvider(p extension.TimelineProvider) (extension.TimelineRegistration, error) {
	a := t.a
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.scm.dead:
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	case p.ID == "" || p.Label == "" || p.Provide == nil || len(p.Schemes) == 0 || slices.Contains(p.Schemes, ""):
		return nil, errors.New("ExtensionTimelineInvalid", "a timeline provider needs an ID, a label, at least one scheme and a function that provides the items", "id", p.ID)
	case a.timeline.unavailable:
		return nil, errors.New("ExtensionTimelineUnavailable", "the scope has no Timeline view")
	}
	for _, o := range a.timeline.providers {
		if o.p.ID == p.ID {
			return nil, errors.New("ExtTimelineProviderTaken", "a timeline provider has this ID already", "id", p.ID)
		}
	}
	f := &FakeTimeline{a: a, p: p}
	a.timeline.providers = append(a.timeline.providers, f)
	return f, nil
}

// TimelineProviders are the providers that are registered (not disposed, and not ended with the activation).
func (a *API) TimelineProviders() []*FakeTimeline {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.timeline.providers)
}

// TimelineProvider is the registered provider with the ID, or nil.
func (a *API) TimelineProvider(id string) *FakeTimeline {
	for _, f := range a.TimelineProviders() {
		if f.p.ID == id {
			return f
		}
	}
	return nil
}

// ID is the ID of the provider.
func (f *FakeTimeline) ID() string { return f.p.ID }

// Label is the label of the provider.
func (f *FakeTimeline) Label() string { return f.p.Label }

// Schemes are the schemes of the provider.
func (f *FakeTimeline) Schemes() []string { return slices.Clone(f.p.Schemes) }

// Page asks the provider for a page as the host does for the view: nothing for a scheme it has no history for, the items with no ID
// dropped and the others sorted newest first, and a panic taking the extension off.
func (f *FakeTimeline) Page(ctx context.Context, uri string, o extension.TimelineOptions) (tl extension.Timeline, err error) {
	f.a.mu.Lock()
	dead := f.disposed
	f.a.mu.Unlock()
	if dead {
		return extension.Timeline{}, errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	scheme, _, _ := strings.Cut(uri, ":")
	if !slices.ContainsFunc(f.p.Schemes, func(s string) bool { return s == "*" || s == scheme }) {
		return extension.Timeline{}, nil
	}
	defer func() {
		if r := recover(); r != nil {
			f.a.scmFail("timeline provider", r)
			tl, err = extension.Timeline{}, errors.NewErrf("ExtensionTimelinePanicked", "it panicked in the Provide of a timeline provider: %v", r)
		}
	}()
	got, err := f.p.Provide(ctx, uri, o)
	if err != nil {
		return extension.Timeline{}, err
	}
	items := slices.DeleteFunc(slices.Clone(got.Items), func(it extension.TimelineItem) bool { return it.ID == "" })
	sort.SliceStable(items, func(i, j int) bool { return items[i].Timestamp.After(items[j].Timestamp) })
	return extension.Timeline{Items: items, Paging: got.Paging}, nil
}

// Changes are the calls of Changed, each with its URIs (none for "every file").
func (f *FakeTimeline) Changes() [][]string {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	return slices.Clone(f.changed)
}

// Changed implements extension.TimelineRegistration.
func (f *FakeTimeline) Changed(uris ...string) {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	if !f.disposed {
		f.changed = append(f.changed, slices.Clone(uris))
	}
}

// Disposed says that the provider was disposed or the activation ended.
func (f *FakeTimeline) Disposed() bool {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	return f.disposed
}

// Dispose implements extension.TimelineRegistration.
func (f *FakeTimeline) Dispose() {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	f.disposed = true
	f.a.timeline.providers = slices.DeleteFunc(f.a.timeline.providers, func(o *FakeTimeline) bool { return o == f })
}
