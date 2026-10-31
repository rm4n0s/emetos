package exthost

import (
	"context"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/timeline"
)

// The binding of extension.Timelines to the timeline service of a scope (SPEC §10.30 "The Timeline view", step 24.21; after
// extHostTimeline.ts and mainThreadTimeline.ts). A provider of an extension is a provider of the service; the service calls it off the UI
// thread, caught at the boundary like every call into an extension (SPEC §10.15 "Containment").

// timelineAPI is the extension.Timelines of one activation.
type timelineAPI struct{ inst *instance }

var _ extension.Timelines = timelineAPI{}

// Timeline implements extension.API.
func (a *api) Timeline() extension.Timelines { return timelineAPI{a.inst} }

// RegisterProvider implements extension.Timelines. It fails with ExtensionDeactivated after the activation ended,
// ExtensionTimelineInvalid for a provider that lacks an ID, a label, a scheme or Provide, ExtensionTimelineUnavailable in a scope with
// no Timeline view and ExtTimelineProviderTaken when the ID is taken.
func (t timelineAPI) RegisterProvider(p extension.TimelineProvider) (extension.TimelineRegistration, error) {
	in := t.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if p.ID == "" || p.Label == "" || p.Provide == nil || len(p.Schemes) == 0 || containsEmpty(p.Schemes) {
		return nil, errors.New("ExtensionTimelineInvalid", "a timeline provider needs an ID, a label, at least one scheme and a function that provides the items",
			"extension", in.id, "id", p.ID, "schemes", len(p.Schemes))
	}
	svc := in.scope.opts.Timeline
	if svc == nil {
		return nil, errors.New("ExtensionTimelineUnavailable", "the scope has no Timeline view", "extension", in.id)
	}
	reg, err := svc.Register(in.id, timeline.Descriptor{ID: p.ID, Label: p.Label, Schemes: append([]string(nil), p.Schemes...)}, &timelineProvider{inst: in, p: p})
	if err != nil {
		return nil, err
	}
	h := &timelineHandle{inst: in, reg: reg}
	if !in.track(h.Dispose) {
		reg.Dispose()
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return h, nil
}

func containsEmpty(list []string) bool {
	for _, s := range list {
		if s == "" {
			return true
		}
	}
	return false
}

// timelineProvider is the timeline.Provider of a provider of an extension.
type timelineProvider struct {
	inst *instance
	p    extension.TimelineProvider
}

// Provide implements timeline.Provider: it asks the extension, which can take its time, and converts the answer. An item with no ID
// is logged and dropped. A panic fails the extension and is the error of the request.
func (t *timelineProvider) Provide(ctx context.Context, uri string, o timeline.Options) (page timeline.Page, err error) {
	in := t.inst
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionTimelinePanicked", "it panicked in the Provide of a timeline provider: %v", r).SetMetadata("extension", in.id, "scope", in.scope.opts.Info.Name, "provider", t.p.ID)
			in.scope.post(func() { in.scope.fail(in.slot, failure, "timeline provider") })
			page, err = timeline.Page{}, failure
		}
	}()
	if in.isDead() {
		return timeline.Page{}, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	tl, err := t.p.Provide(ctx, uri, extension.TimelineOptions{Cursor: o.Cursor, Limit: o.Limit})
	if err != nil {
		return timeline.Page{}, err
	}
	items := make([]timeline.Item, 0, len(tl.Items))
	for _, it := range tl.Items {
		if it.ID == "" {
			in.log.Warn("invalid timeline item: it has no ID", "provider", t.p.ID, "label", it.Label)
			continue
		}
		item := timeline.Item{
			ID: it.ID, Label: it.Label, Description: it.Description, Detail: it.Detail, Timestamp: it.Timestamp, Icon: it.Icon,
			ContextValue: it.ContextValue, Args: it.Args,
		}
		if it.Command != nil {
			item.Command = timeline.Command{ID: it.Command.ID, Title: it.Command.Title, Tooltip: it.Command.Tooltip, Args: scmCommandArgs(*it.Command)}
		}
		items = append(items, item)
	}
	return timeline.Page{Items: items, Cursor: tl.Paging.Cursor}, nil
}

// timelineHandle is the extension.TimelineRegistration of a registered provider.
type timelineHandle struct {
	inst *instance
	reg  *timeline.Registration
	once sync.Once
}

var _ extension.TimelineRegistration = (*timelineHandle)(nil)

// Changed implements extension.TimelineRegistration.
func (h *timelineHandle) Changed(uris ...string) {
	if h.inst.isDead() {
		return
	}
	h.reg.Fire(uris...)
}

// Dispose implements extension.TimelineRegistration.
func (h *timelineHandle) Dispose() { h.once.Do(h.reg.Dispose) }
