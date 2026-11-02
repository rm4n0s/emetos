package testing_test

import (
	"context"
	"reflect"
	"testing"

	ct "github.com/rm4n0s/emetos/internal/core/testing"
)

func TestIDs(t *testing.T) {
	a := ct.JoinID("c", "pkg", "a")
	if ct.ControllerOf(a) != "c" || ct.ParentID(a) != ct.JoinID("c", "pkg") || ct.ParentID("c") != "" || ct.ParentID(ct.JoinID("c", "x")) != "c" {
		t.Error("ControllerOf or ParentID")
	}
	if !reflect.DeepEqual(ct.IDsToRoot(a), []string{a, ct.JoinID("c", "pkg"), "c"}) || !reflect.DeepEqual(ct.SplitID(a), []string{"c", "pkg", "a"}) {
		t.Error("IDsToRoot or SplitID")
	}
	cases := []struct {
		anc, id string
		want    bool
	}{
		{"c", a, true}, {ct.JoinID("c", "pkg"), a, true}, {a, a, false}, {a, ct.JoinID("c", "pkg"), false},
		{ct.JoinID("c", "pk"), a, false}, {"d", a, false},
	}
	for _, c := range cases {
		if got := ct.IsAncestorID(c.anc, c.id); got != c.want {
			t.Errorf("IsAncestorID(%q, %q) = %v", c.anc, c.id, got)
		}
	}
}

func TestTheTreeOfAController(t *testing.T) {
	r := newRig(t)
	if got := r.svc.Children("c"); len(got) != 2 || got[0].Label != "pkg" || got[1].Label != "other" {
		t.Errorf("top items %+v", got)
	}
	if r.svc.ItemCount("c") != 6 {
		t.Errorf("%d items", r.svc.ItemCount("c"))
	}
	// replacing keeps the place and the children
	r.put(ct.Item{ID: id(pkg), Label: "pkg (renamed)", Tags: []string{"t"}, Range: &ct.Range{End: ct.Pos{Line: 3}}})
	got := r.svc.Children("c")
	if got[0].Label != "pkg (renamed)" || len(r.svc.Children(id(pkg))) != 3 || got[0].ControllerID != "c" || got[0].ParentID != "c" || !got[0].HasTag("t") {
		t.Errorf("after the replacement %+v", got[0])
	}
	var seen []string
	r.svc.Walk("c", func(it ct.Item) bool {
		seen = append(seen, it.Label)
		return it.ID != id(pkg, sub) // do not go below sub
	})
	want := []string{"pkg (renamed)", "a", "b", "sub", "other"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("walk %q, want %q", seen, want)
	}
	r.ctl.Remove(id(pkg, sub))
	if _, ok := r.svc.Item(id(pkg, sub, "x")); ok || r.svc.ItemCount("c") != 4 || len(r.svc.Children(id(pkg))) != 2 {
		t.Error("Remove left the subtree")
	}
	evs := r.events(ct.EventTree)
	last := evs[len(evs)-1]
	if !last.Removed || !reflect.DeepEqual(last.IDs, []string{id(pkg, sub), id(pkg, sub, "x")}) {
		t.Errorf("removal event %+v", last)
	}
	r.ctl.Remove(id("not there")) // nothing happens
}

func TestControllerErrors(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.RegisterController(ct.ControllerOptions{ID: "c", Label: "again"})
	routeOf(t, err, "Service.RegisterController.TestingControllerDuplicate")
	for _, o := range []ct.ControllerOptions{{ID: "", Label: "x"}, {ID: "a" + ct.IDSeparator + "b", Label: "x"}, {ID: "ok", Label: " "}} {
		_, err = r.svc.RegisterController(o)
		routeOf(t, err, "Service.RegisterController.TestingControllerInvalid")
	}
	cases := []struct {
		name  string
		item  ct.Item
		route string
	}{
		{"another controller's id", ct.Item{ID: ct.JoinID("d", "x")}, "Controller.Upsert.TestingItemInvalid"},
		{"the controller's own id", ct.Item{ID: "c"}, "Controller.Upsert.TestingItemInvalid"},
		{"an empty last part", ct.Item{ID: "c" + ct.IDSeparator}, "Controller.Upsert.TestingItemInvalid"},
		{"a parent that is not there", ct.Item{ID: id("nope", "x")}, "Controller.Upsert.TestingItemParentUnknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { routeOf(t, r.ctl.Upsert(c.item), c.route) })
	}
	r.ctl.Dispose()
	routeOf(t, r.ctl.Upsert(ct.Item{ID: id("x")}), "Controller.Upsert.TestingControllerUnknown")
	_, err = r.ctl.AddProfile(ct.ProfileSpec{Label: "p", Group: ct.GroupRun, Run: func(context.Context, ct.ProfileRequest) error { return nil }})
	routeOf(t, err, "Controller.AddProfile.TestingControllerUnknown")
	if len(r.svc.Controllers()) != 0 {
		t.Error("the controller is still listed")
	}
	routeOf(t, r.svc.ResolveItem(context.Background(), "c"), "Service.ResolveItem.TestingControllerUnknown")
	routeOf(t, r.svc.RefreshTests(context.Background(), "c"), "Service.RefreshTests.TestingControllerUnknown")
}

func TestResolveAndRefresh(t *testing.T) {
	svc := ct.NewService(ct.Options{})
	var resolved, refreshed []string
	fail := plainError("fails")
	mk := func(id string, failRefresh bool) {
		_, err := svc.RegisterController(ct.ControllerOptions{
			ID: id, Label: id,
			Resolve: func(_ context.Context, item string) error { resolved = append(resolved, item); return nil },
			Refresh: func(context.Context) error {
				refreshed = append(refreshed, id)
				if failRefresh {
					return fail
				}
				return nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	mk("a", false)
	mk("b", true)
	if _, err := svc.RegisterController(ct.ControllerOptions{ID: "plain", Label: "plain"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := svc.ResolveItem(ctx, ct.JoinID("a", "x")); err != nil || !reflect.DeepEqual(resolved, []string{ct.JoinID("a", "x")}) {
		t.Errorf("resolve %v %q", err, resolved)
	}
	if err := svc.ResolveItem(ctx, "plain"); err != nil {
		t.Errorf("a controller with no resolve function: %v", err)
	}
	if err := svc.RefreshTests(ctx, "a"); err != nil || !reflect.DeepEqual(refreshed, []string{"a"}) {
		t.Errorf("refresh one %v %q", err, refreshed)
	}
	refreshed = nil
	if err := svc.RefreshTests(ctx, ""); err != fail || !reflect.DeepEqual(refreshed, []string{"a", "b"}) {
		t.Errorf("refresh all %v %q", err, refreshed)
	}
	if err := svc.RefreshTests(ctx, "plain"); err != nil {
		t.Errorf("refresh a controller with no refresh function: %v", err)
	}
}

func TestProfilesDefaultsAndCapabilities(t *testing.T) {
	r := newRig(t)
	noop := func(context.Context, ct.ProfileRequest) error { return nil }
	configured := 0
	b := r.profile(ct.ProfileSpec{Label: "b run", Run: noop, Configure: func() { configured++ }})
	a := r.profile(ct.ProfileSpec{Label: "a run", Run: noop})
	d := r.profile(ct.ProfileSpec{Label: "debug", Group: ct.GroupDebug, IsDefault: true, Run: noop, SupportsContinuous: true})
	order := func() []string {
		var out []string
		for _, p := range r.svc.Profiles("c") {
			out = append(out, p.Label)
		}
		return out
	}
	if got := order(); !reflect.DeepEqual(got, []string{"debug", "a run", "b run"}) {
		t.Errorf("order %q: the defaults first, then by label", got)
	}
	// with no default in the group the first profile of the group is the default
	if got := r.svc.GroupDefaults(ct.GroupRun, ""); len(got) != 1 || got[0].ID != a {
		t.Errorf("run defaults %+v", got)
	}
	if got := r.svc.GroupDefaults(ct.GroupCoverage, ""); len(got) != 0 {
		t.Errorf("coverage defaults %+v", got)
	}
	r.svc.SetGroupDefaults(ct.GroupRun, []ct.ProfileRef{{ControllerID: "c", ID: b}})
	if got := r.svc.GroupDefaults(ct.GroupRun, "c"); len(got) != 1 || got[0].ID != b || !got[0].IsDefault {
		t.Errorf("run defaults after the choice %+v", got)
	}
	// a profile of another group with the label of one that changed follows it
	label := "b run"
	r.ctl.UpdateProfile(d, &label, nil)
	r.svc.SetGroupDefaults(ct.GroupRun, []ct.ProfileRef{{ControllerID: "c", ID: b}})
	for _, p := range r.svc.Profiles("c") {
		if p.ID == d && !p.IsDefault {
			t.Error("the debug profile with the same label did not follow")
		}
	}
	c := r.svc.Capabilities("")
	if !c.Run || !c.Debug || c.Coverage || !c.HasNonDefault || !c.HasConfigurable || !c.Continuous {
		t.Errorf("capabilities %+v", c)
	}
	r.put(ct.Item{ID: id("tagged"), Label: "t", Tags: []string{"slow"}})
	r.profile(ct.ProfileSpec{Label: "slow", Group: ct.GroupCoverage, Tag: "slow", Run: noop})
	if c := r.svc.Capabilities(id(other)); c.Coverage {
		t.Error("a profile with a tag was offered for an item without it")
	}
	if c := r.svc.Capabilities(id("tagged")); !c.Coverage {
		t.Error("a profile with a tag was not offered for an item with it")
	}
	if err := r.svc.ConfigureProfile(ct.ProfileRef{ControllerID: "c", ID: b}); err != nil || configured != 1 {
		t.Errorf("configure %v %d", err, configured)
	}
	routeOf(t, r.svc.ConfigureProfile(ct.ProfileRef{ControllerID: "c", ID: 99}), "Service.ConfigureProfile.TestingProfileUnknown")
	r.ctl.RemoveProfile(b)
	if len(r.svc.Profiles("c")) != 3 {
		t.Errorf("%d profiles after the removal", len(r.svc.Profiles("c")))
	}
	if n := len(r.events(ct.EventProfiles)); n < 6 {
		t.Errorf("%d profile events", n)
	}
}

func TestProfileErrors(t *testing.T) {
	r := newRig(t)
	run := func(context.Context, ct.ProfileRequest) error { return nil }
	for _, spec := range []ct.ProfileSpec{
		{Label: "", Group: ct.GroupRun, Run: run}, {Label: "x", Group: 0, Run: run}, {Label: "x", Group: ct.GroupRun}, {Label: "x", Group: 3, Run: run},
	} {
		_, err := r.ctl.AddProfile(spec)
		routeOf(t, err, "Controller.AddProfile.TestingProfileInvalid")
	}
}

func TestSubscribersGetEventsInOrderAndCanChangeTheModel(t *testing.T) {
	svc := ct.NewService(ct.Options{})
	var kinds []ct.EventKind
	svc.Subscribe(func(e ct.Event) {
		kinds = append(kinds, e.Kind)
		if e.Kind == ct.EventControllers && e.ControllerID == "a" { // an event handler that changes the model: no deadlock
			if _, err := svc.RegisterController(ct.ControllerOptions{ID: "b", Label: "b"}); err != nil {
				t.Error(err)
			}
		}
	})
	cancel := svc.Subscribe(func(ct.Event) { t.Error("a subscriber that was cancelled got an event") })
	cancel()
	if _, err := svc.RegisterController(ct.ControllerOptions{ID: "a", Label: "a"}); err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 || len(svc.Controllers()) != 2 {
		t.Errorf("kinds %v, controllers %v", kinds, svc.Controllers())
	}
}
