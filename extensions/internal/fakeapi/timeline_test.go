package fakeapi

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/timelinetest"
)

// TestTimelineConformance runs the suite that the host passes too (internal/gui/exthost/timeline_test.go).
func TestTimelineConformance(t *testing.T) {
	timelinetest.Run(t, func(t *testing.T) *timelinetest.Harness {
		a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
		seen := map[*FakeTimeline]int{}
		return &timelinetest.Harness{
			API: a,
			Ask: func(source, uri string, o extension.TimelineOptions) (extension.Timeline, error, bool) {
				f := a.TimelineProvider(source)
				if f == nil {
					return extension.Timeline{}, nil, false
				}
				if !schemeMatches(f.Schemes(), uri) {
					return extension.Timeline{}, nil, false
				}
				tl, err := f.Page(context.Background(), uri, o)
				return tl, err, true
			},
			Sources: func() []string {
				var ids []string
				for _, f := range a.TimelineProviders() {
					ids = append(ids, f.ID())
				}
				return ids
			},
			Told: func() []timelinetest.Told {
				var out []timelinetest.Told
				for _, f := range a.TimelineProviders() {
					changes := f.Changes()
					for _, uris := range changes[seen[f]:] {
						out = append(out, timelinetest.Told{ID: f.ID(), URIs: uris})
					}
					seen[f] = len(changes)
				}
				return out
			},
			Pump:       func() { a.Pump() },
			Deactivate: a.Deactivate,
			Failed:     a.Failed,
		}
	})
}

func schemeMatches(schemes []string, uri string) bool {
	for _, s := range schemes {
		if s == "*" || len(uri) > len(s) && uri[:len(s)+1] == s+":" {
			return true
		}
	}
	return false
}

// TestTheFakeTimelineFailsLikeTheHost checks the routes of the fake's errors, which have the tags of the host's.
func TestTheFakeTimelineFailsLikeTheHost(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	_, err := a.Timeline().RegisterProvider(extension.TimelineProvider{})
	route(t, err, "timelineAPI.RegisterProvider.ExtensionTimelineInvalid")
	ok := extension.TimelineProvider{ID: "x", Label: "x", Schemes: []string{"file"}, Provide: func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{Items: []extension.TimelineItem{{ID: "a", Timestamp: time.Unix(1, 0)}}}, nil
	}}
	if _, err := a.Timeline().RegisterProvider(ok); err != nil {
		t.Fatal(err)
	}
	_, err = a.Timeline().RegisterProvider(ok)
	route(t, err, "timelineAPI.RegisterProvider.ExtTimelineProviderTaken")
	a.SetTimelineUnavailable(true)
	other := ok
	other.ID = "y"
	_, err = a.Timeline().RegisterProvider(other)
	route(t, err, "timelineAPI.RegisterProvider.ExtensionTimelineUnavailable")
	a.Deactivate()
	_, err = a.Timeline().RegisterProvider(ok)
	route(t, err, "timelineAPI.RegisterProvider.ExtensionDeactivated")
}
