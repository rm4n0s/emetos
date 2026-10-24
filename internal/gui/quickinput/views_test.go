package quickinput

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

func viewsRig(t *testing.T, ctx contextkey.MapContext) (*Controller, *[]string) {
	t.Helper()
	var opened []string
	reg := NewRegistry()
	reg.MustRegister(NewViewsProvider(ViewsOptions{
		Views:         command.DefaultViews(),
		Context:       func() contextkey.Context { return ctx },
		OpenContainer: func(id string) { opened = append(opened, "container "+id) },
		OpenView:      func(id string) { opened = append(opened, "view "+id) },
		Channels:      func() []string { return []string{"Emetos", "Tasks"} },
		OpenChannel:   func(name string) { opened = append(opened, "channel "+name) },
	}))
	return NewController(ControllerOptions{Registry: reg}), &opened
}

func viewRows(c *Controller) []string {
	var out []string
	for _, r := range c.Picker().Rows() {
		out = append(out, r.Separator+"|"+r.Item.Label)
	}
	return out
}

func TestViewsModeListsContainersViewsAndChannels(t *testing.T) {
	c, _ := viewsRig(t, contextkey.MapContext{"workbenchState": "folder"})
	c.Show("view ", ShowOptions{})
	// the group of a container is named after the container ("Side Bar / Explorer") when the container is
	// an entry itself, and the Output channels are in the group of the Output; the views of a container
	// follow the containers
	want := []string{
		"Side Bar|Explorer",
		"Panel|Problems",
		"|Output",
		"Side Bar / Explorer|Open Editors",
		"|Folders",
		"Panel / Problems|Problems",
		"Panel / Output|Output",
		"|Emetos",
		"|Tasks",
	}
	got := viewRows(c)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows\n%v\nwant\n%v", got, want)
	}
	if c.Picker().Placeholder() != "Type the name of a view, output channel or terminal to open." {
		t.Errorf("placeholder %q", c.Picker().Placeholder())
	}
}

func TestViewsModeFiltersAndOpens(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		ctx    contextkey.MapContext
		want   []string
		open   string
	}{
		{"by label, with the group", "prob", contextkey.MapContext{}, []string{"Panel|Problems", "Panel / Problems|Problems"}, "container workbench.panel.markers"},
		{"by the container", "explorer", contextkey.MapContext{"workbenchState": "folder"}, []string{"Side Bar|Explorer", "Side Bar / Explorer|Open Editors", "|Folders"}, "container workbench.view.explorer"},
		{"the empty folder view shows without a folder", "no fold", contextkey.MapContext{"workbenchState": "empty"}, []string{"Explorer|No Folder Opened"}, "view workbench.explorer.emptyView"},
		{"a channel", "tasks", contextkey.MapContext{}, []string{"Output|Tasks"}, "channel Tasks"},
		{"no match", "zzz", contextkey.MapContext{}, []string{"|No matching views"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, opened := viewsRig(t, tc.ctx)
			c.Show("view "+tc.filter, ShowOptions{PreserveValue: true})
			if got := viewRows(c); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("rows\n%v\nwant\n%v", got, tc.want)
			}
			c.Accept(KeyMods{})
			if tc.open == "" {
				if !c.Visible() || len(*opened) != 0 {
					t.Errorf("visible %v opened %v", c.Visible(), *opened)
				}
				return
			}
			if c.Visible() || !reflect.DeepEqual(*opened, []string{tc.open}) {
				t.Errorf("visible %v opened %v, want %q", c.Visible(), *opened, tc.open)
			}
		})
	}
}
