package command

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

func viewIDs(views []View) []string {
	out := make([]string, len(views))
	for i, v := range views {
		out[i] = v.ID
	}
	return out
}

func TestViewContainersOrder(t *testing.T) {
	r := NewViewRegistry()
	for _, c := range []ViewContainer{
		{ID: "scm", Title: "Source Control", Order: 2},
		{ID: "explorer", Title: "Explorer", Order: 0},
		{ID: "extra", Title: "Extra", Order: 2},
		{ID: "problems", Title: "Problems", Order: 0, Location: ViewLocationPanel},
		{ID: "search", Title: "Search", Order: 1, Location: ViewLocationSideBar},
	} {
		if err := r.RegisterContainer(c); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(loc ViewLocation) []string {
		var out []string
		for _, c := range r.Containers(loc) {
			out = append(out, c.ID)
		}
		return out
	}
	if got, want := ids(ViewLocationSideBar), []string{"explorer", "search", "scm", "extra"}; !reflect.DeepEqual(got, want) {
		t.Errorf("side bar %v, want %v", got, want)
	}
	if got, want := ids(ViewLocationPanel), []string{"problems"}; !reflect.DeepEqual(got, want) {
		t.Errorf("panel %v, want %v", got, want)
	}
	if c, ok := r.Container("scm"); !ok || c.Location != ViewLocationSideBar {
		t.Errorf("a container with no location is in the side bar: %+v %v", c, ok)
	}
	if _, ok := r.Container("nope"); ok {
		t.Error("unknown container")
	}
}

func TestViewsOfAContainer(t *testing.T) {
	r := NewViewRegistry()
	r.MustRegisterContainer(ViewContainer{ID: "explorer", Title: "Explorer"})
	r.MustRegisterContainer(ViewContainer{ID: "search", Title: "Search"})
	r.MustRegisterViews(
		View{ID: "outline", Name: "Outline", Container: "explorer", Order: 2, Collapsed: true},
		View{ID: "folders", Name: "Folders", Container: "explorer", Order: 1},
		View{ID: "open", Name: "Open Editors", Container: "explorer", Order: 0, When: contextkey.Has("openEditorsVisible")},
		View{ID: "timeline", Name: "Timeline", Container: "explorer", Order: 2},
		View{ID: "results", Name: "Results", Container: "search"},
	)
	cases := []struct {
		name      string
		container string
		ctx       contextkey.Context
		want      []string
	}{
		{"filtered by when, ordered, ties in registration order", "explorer", contextkey.MapContext{}, []string{"folders", "outline", "timeline"}},
		{"a when that holds", "explorer", contextkey.MapContext{"openEditorsVisible": true}, []string{"open", "folders", "outline", "timeline"}},
		{"no context counts every view", "explorer", nil, []string{"open", "folders", "outline", "timeline"}},
		{"another container", "search", contextkey.MapContext{}, []string{"results"}},
		{"unknown container", "nope", contextkey.MapContext{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := viewIDs(r.Views(tc.container, tc.ctx))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
	if v, ok := r.View("outline"); !ok || !v.Collapsed || v.Container != "explorer" {
		t.Errorf("View(outline) = %+v, %v", v, ok)
	}
	if _, ok := r.View("nope"); ok {
		t.Error("unknown view")
	}
}

func TestViewRegistryErrors(t *testing.T) {
	r := NewViewRegistry()
	r.MustRegisterContainer(ViewContainer{ID: "explorer", Title: "Explorer"})
	r.MustRegisterViews(View{ID: "folders", Name: "Folders", Container: "explorer"})
	cases := []struct {
		name  string
		err   error
		route string
	}{
		{"container without id", r.RegisterContainer(ViewContainer{Title: "x"}), "RegisterContainer.ViewContainerInvalid"},
		{"container without title", r.RegisterContainer(ViewContainer{ID: "x"}), "RegisterContainer.ViewContainerInvalid"},
		{"duplicate container", r.RegisterContainer(ViewContainer{ID: "explorer", Title: "Again"}), "RegisterContainer.ViewContainerDuplicate"},
		{"view without name", r.RegisterViews(View{ID: "v", Container: "explorer"}), "RegisterViews.ViewInvalid"},
		{"view in an unknown container", r.RegisterViews(View{ID: "v", Name: "V", Container: "nope"}), "RegisterViews.ViewContainerUnknown"},
		{"duplicate view", r.RegisterViews(View{ID: "folders", Name: "F", Container: "explorer"}), "RegisterViews.ViewDuplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assertRoute(t, tc.err, tc.route) })
	}
	for _, f := range []func(){
		func() { r.MustRegisterContainer(ViewContainer{ID: "explorer", Title: "Again"}) },
		func() { r.MustRegisterViews(View{ID: "folders", Name: "F", Container: "explorer"}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("the Must function must panic")
				}
			}()
			f()
		}()
	}
}

func TestDefaultViews(t *testing.T) {
	r := DefaultViews()
	containers := r.Containers(ViewLocationSideBar)
	if len(containers) != 1 || containers[0].ID != ViewContainerExplorer || containers[0].Title != "Explorer" || containers[0].Icon != 0xEAF0 {
		t.Fatalf("containers: %+v", containers)
	}
	for _, tc := range []struct {
		name  string
		state string
		want  []string
	}{
		{"a folder", "folder", []string{ViewOpenEditors, ViewExplorerFolders, ViewOutline}},
		{"no folder", "empty", []string{ViewOpenEditors, ViewExplorerEmpty, ViewOutline}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := viewIDs(r.Views(ViewContainerExplorer, contextkey.MapContext{"workbenchState": tc.state}))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("views: %v, want %v", got, tc.want)
			}
		})
	}
	if v, _ := r.View(ViewOpenEditors); !v.HideByDefault {
		t.Error("Open Editors starts hidden")
	}
}

func TestDefaultPanelViews(t *testing.T) {
	r := DefaultViews()
	containers := r.Containers(ViewLocationPanel)
	if len(containers) != 2 {
		t.Fatalf("containers: %+v", containers)
	}
	for i, want := range []struct {
		id, title string
		icon      int
		view      string
	}{
		{"workbench.panel.markers", "Problems", 0xEA6C, "workbench.panel.markers.view"},
		{"workbench.panel.output", "Output", 0xEB9D, "workbench.panel.output"},
	} {
		c := containers[i]
		if c.ID != want.id || c.Title != want.title || c.Icon != want.icon {
			t.Errorf("container %d: %+v, want %+v", i, c, want)
		}
		if got := viewIDs(r.Views(c.ID, contextkey.MapContext{})); !reflect.DeepEqual(got, []string{want.view}) {
			t.Errorf("views of %s: %v, want %v", c.ID, got, want.view)
		}
	}
	// the panel containers are not in the activity bar
	for _, c := range r.Containers(ViewLocationSideBar) {
		if c.Location != ViewLocationSideBar {
			t.Errorf("a panel container in the side bar: %+v", c)
		}
	}
}

func TestViewRegistryForkChangesAndRemoval(t *testing.T) {
	r := NewViewRegistry()
	r.MustRegisterContainer(ViewContainer{ID: "a", Title: "A"})
	r.MustRegisterViews(View{ID: "a.v", Name: "V", Container: "a"})
	fork := r.Fork()
	changes := 0
	stop := fork.OnChange(func() { changes++ })
	fork.MustRegisterContainer(ViewContainer{ID: "b", Title: "B", Location: ViewLocationPanel})
	fork.MustRegisterViews(View{ID: "b.v", Name: "BV", Container: "b"}, View{ID: "b.w", Name: "BW", Container: "b"})
	if changes != 2 {
		t.Errorf("%d changes after two registrations, want 2", changes)
	}
	if _, ok := r.Container("b"); ok {
		t.Error("what the fork added is in the original")
	}
	if _, ok := fork.Container("a"); !ok || len(fork.Views("a", nil)) != 1 {
		t.Error("the fork lost what the original had")
	}
	// a failed registration tells nothing, a partial one tells once
	before := changes
	if err := fork.RegisterViews(View{ID: "b.x", Name: "X", Container: "b"}, View{ID: "b.v", Name: "Dup", Container: "b"}); err == nil {
		t.Error("a duplicate view was accepted")
	}
	if changes != before+1 {
		t.Errorf("a registration that added one view then failed told %d times", changes-before)
	}
	fork.UnregisterView("b.x")
	fork.UnregisterView("nothing") // nothing changes
	fork.UnregisterContainer("b")
	fork.UnregisterContainer("b")
	if _, ok := fork.Container("b"); ok || len(fork.Views("b", nil)) != 0 {
		t.Error("the container or its views stayed")
	}
	if changes != before+3 {
		t.Errorf("%d changes, want %d", changes, before+3)
	}
	stop()
	fork.MustRegisterContainer(ViewContainer{ID: "c", Title: "C"})
	if changes != before+3 {
		t.Error("a listener that stopped was told")
	}
	// the original did not see any of it
	if len(r.Containers(ViewLocationPanel)) != 0 || len(r.Containers(ViewLocationSideBar)) != 1 {
		t.Errorf("the original changed: %+v", r.Containers(ViewLocationSideBar))
	}
}
