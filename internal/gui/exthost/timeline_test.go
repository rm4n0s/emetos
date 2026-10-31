package exthost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/timelinetest"
	"github.com/rm4n0s/emetos/internal/core/timeline"
)

// timelineHarness makes the harness of the conformance suite over a tab of the host.
func timelineHarness(t *testing.T) *timelinetest.Harness {
	t.Helper()
	_, tb, api := grabAPI(t, "emetos.timelinetest", nil)
	var told []timelinetest.Told
	tb.timeline.OnChanged(func(c timeline.Change) { told = append(told, timelinetest.Told{ID: c.ID, URIs: c.URIs}) })
	return &timelinetest.Harness{
		API: api,
		Ask: func(source, uri string, o extension.TimelineOptions) (extension.Timeline, error, bool) {
			done := make(chan timeline.Result, 1)
			if tb.timeline.Request(context.Background(), source, uri, timeline.Options{Cursor: o.Cursor, Limit: o.Limit}, func(r timeline.Result) { done <- r }) == nil {
				return extension.Timeline{}, nil, false
			}
			var res timeline.Result
			select {
			case res = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the provider did not answer")
			}
			if res.Err != nil {
				return extension.Timeline{}, res.Err, true
			}
			tl := extension.Timeline{Paging: extension.TimelinePaging{Cursor: res.Page.Cursor}}
			for _, it := range res.Page.Items {
				item := extension.TimelineItem{ID: it.ID, Label: it.Label, Description: it.Description, Detail: it.Detail, Timestamp: it.Timestamp, Icon: it.Icon,
					ContextValue: it.ContextValue, Args: it.Args}
				if it.Command.ID != "" {
					item.Command = &extension.Command{ID: it.Command.ID, Title: it.Command.Title, Tooltip: it.Command.Tooltip, Args: it.Command.Args}
				}
				tl.Items = append(tl.Items, item)
			}
			return tl, nil, true
		},
		Sources: func() []string {
			var ids []string
			for _, s := range tb.timeline.Sources() {
				ids = append(ids, s.ID)
			}
			return ids
		},
		Told:       func() []timelinetest.Told { out := told; told = nil; return out },
		Pump:       tb.pump,
		Deactivate: func() { tb.disabled = []string{"emetos.timelinetest"}; tb.scope.Refresh() },
		Failed:     func() bool { return tb.scope.State("emetos.timelinetest") == StateFailed },
	}
}

// TestTimelineConformance runs the suite that the fake of the tests of the extensions passes too.
func TestTimelineConformance(t *testing.T) { timelinetest.Run(t, timelineHarness) }

func TestTimelineRoutesOfTheFailures(t *testing.T) {
	ok := func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
		return extension.Timeline{}, nil
	}
	prov := func(id string) extension.TimelineProvider {
		return extension.TimelineProvider{ID: id, Label: id, Schemes: []string{"file"}, Provide: ok}
	}
	t.Run("invalid", func(t *testing.T) {
		_, _, api := grabAPI(t, "emetos.timelinetest", nil)
		_, err := api.Timeline().RegisterProvider(extension.TimelineProvider{ID: "x"})
		mustRoute(t, err, "timelineAPI.RegisterProvider.ExtensionTimelineInvalid")
	})
	t.Run("no Timeline view", func(t *testing.T) {
		_, _, api := grabAPI(t, "emetos.timelinetest", func(r *rig) { r.noTimeline["one"] = true })
		_, err := api.Timeline().RegisterProvider(prov("x"))
		mustRoute(t, err, "timelineAPI.RegisterProvider.ExtensionTimelineUnavailable")
	})
	t.Run("taken", func(t *testing.T) {
		_, _, api := grabAPI(t, "emetos.timelinetest", nil)
		if _, err := api.Timeline().RegisterProvider(prov("x")); err != nil {
			t.Fatal(err)
		}
		_, err := api.Timeline().RegisterProvider(prov("x"))
		mustRoute(t, err, "timelineAPI.RegisterProvider->Service.Register.ExtTimelineProviderTaken")
	})
	t.Run("a panic", func(t *testing.T) {
		_, tb, api := grabAPI(t, "emetos.timelinetest", nil)
		p := prov("x")
		p.Provide = func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) { panic("boom") }
		if _, err := api.Timeline().RegisterProvider(p); err != nil {
			t.Fatal(err)
		}
		done := make(chan timeline.Result, 1)
		tb.timeline.Request(context.Background(), "x", "file:///a", timeline.Options{}, func(res timeline.Result) { done <- res })
		res := <-done
		mustRoute(t, res.Err, "timelineProvider.Provide.func1.ExtensionTimelinePanicked")
		tb.pump()
		if tb.scope.State("emetos.timelinetest") != StateFailed {
			t.Error("the extension did not fail")
		}
	})
}

func TestAnItemWithNoIDIsLoggedAndDropped(t *testing.T) {
	_, tb, api := grabAPI(t, "emetos.timelinetest", nil)
	_, err := api.Timeline().RegisterProvider(extension.TimelineProvider{ID: "x", Label: "x", Schemes: []string{"file"},
		Provide: func(context.Context, string, extension.TimelineOptions) (extension.Timeline, error) {
			return extension.Timeline{Items: []extension.TimelineItem{{Label: "no id"}, {ID: "a"}}}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan timeline.Result, 1)
	tb.timeline.Request(context.Background(), "x", "file:///a", timeline.Options{}, func(res timeline.Result) { done <- res })
	res := <-done
	if len(res.Page.Items) != 1 || res.Page.Items[0].ID != "a" {
		t.Errorf("items = %+v", res.Page.Items)
	}
	if !strings.Contains(tb.log.String(), "invalid timeline item") {
		t.Errorf("log = %s", tb.log.String())
	}
}
