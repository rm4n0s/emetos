package quickinput

import "context"

// Ported from VS Code: src/vs/platform/quickinput/browser/pickerQuickAccess.ts (PickerQuickAccessProvider)

// ListProvider is a provider that makes its items itself for each filter (VS Code's
// PickerQuickAccessProvider): the picker's own filtering is off, the items come back with their
// highlights, and accepting an item hides the picker and then runs the item. Make the picks with
// Picks, or, for a long job, with PicksAsync, which runs in a goroutine, is cancelled when the filter
// changes or the picker hides, and hands its result over through the session's Post.
type ListProvider struct {
	// Info describes the provider.
	ProviderInfo
	// NoResults is the label of the pick shown when there are no picks and the filter is not
	// empty ("No matching commands"); empty shows nothing.
	NoResults string
	// Picks makes the items for a filter (the value without the prefix, trimmed). It runs on the UI
	// thread.
	Picks func(filter string) []Item
	// PicksAsync, when set, is used in place of Picks. It runs in a goroutine and must stop when ctx
	// is done; its items are shown if the filter has not changed meanwhile.
	PicksAsync func(ctx context.Context, filter string) []Item
	// Start, when set, is called when the provider takes over, with the session, and its result is
	// called when the session ends.
	Start func(s *Session) (end func())
	// DefaultFilter says how the provider starts when it is opened with just its prefix.
	DefaultFilter func() DefaultFilterKind
}

// Info implements Provider.
func (p *ListProvider) Info() ProviderInfo { return p.ProviderInfo }

// DefaultFilterValue implements DefaultFilterer.
func (p *ListProvider) DefaultFilterValue() DefaultFilterKind {
	if p.DefaultFilter == nil {
		return DefaultFilterPreserve
	}
	return p.DefaultFilter()
}

// Provide implements Provider.
func (p *ListProvider) Provide(s *Session) Handler {
	pk := s.Picker()
	pk.MatchOnLabel, pk.MatchOnDescription, pk.MatchOnDetail, pk.SortByLabel = false, false, false, false
	if p.Start != nil {
		if end := p.Start(s); end != nil {
			s.OnEnd(end)
		}
	}
	return &listHandler{p: p, s: s}
}

type listHandler struct {
	p      *ListProvider
	s      *Session
	cancel context.CancelFunc
	seq    int
}

func (h *listHandler) apply(items []Item, filter string) {
	if len(items) == 0 && filter != "" && h.p.NoResults != "" {
		items = []Item{{Label: h.p.NoResults}}
	}
	h.s.Picker().SetItems(items)
}

// ValueChanged implements Handler.
func (h *listHandler) ValueChanged(filter string) {
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	if h.p.PicksAsync != nil {
		ctx, cancel := context.WithCancel(h.s.Context())
		h.cancel = cancel
		h.seq++
		seq := h.seq
		go func() {
			items := h.p.PicksAsync(ctx, filter)
			h.s.Post(func() {
				if ctx.Err() == nil && seq == h.seq {
					h.apply(items, filter)
				}
			})
		}()
		return
	}
	if h.p.Picks != nil {
		h.apply(h.p.Picks(filter), filter)
	}
}

// Accept implements Handler: the picker hides, then the item runs.
func (h *listHandler) Accept(row Row, mods KeyMods) { h.s.c.AcceptRow(row, mods) }

// Done implements Handler.
func (h *listHandler) Done(bool) {
	if h.cancel != nil {
		h.cancel()
	}
}
