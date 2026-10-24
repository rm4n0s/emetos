package quickinput

import (
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

// Ported from VS Code: src/vs/workbench/contrib/quickaccess/browser/viewQuickAccess.ts

// The prefix of the views mode and the ids of the commands that open it.
const (
	// ViewsPrefix is the prefix of the mode that opens a view.
	ViewsPrefix = "view "
	// OpenViewCommand is Open View.
	OpenViewCommand = "workbench.action.openView"
	// QuickOpenViewCommand is Quick Open View, which has no key on Linux.
	QuickOpenViewCommand = "workbench.action.quickOpenView"
)

// ViewsOptions are what the provider of the views mode needs.
type ViewsOptions struct {
	// Views holds the view containers and the views in them.
	Views *command.ViewRegistry
	// Context gives the context keys the `when` of the views is evaluated in.
	Context func() contextkey.Context
	// OpenContainer shows a view container of the side bar or of the panel and gives it the focus.
	OpenContainer func(id string)
	// OpenView shows a view, and its container, and gives it the focus.
	OpenView func(id string)
	// Channels lists the names of the output channels, which the mode lists as views of the Output;
	// nil lists none.
	Channels func() []string
	// OpenChannel shows an output channel.
	OpenChannel func(name string)
}

// viewPick is an entry of the views mode before the separators are added.
type viewPick struct {
	label, container string
	accept           func()
}

// NewViewsProvider makes the provider of the `view ` mode: the view containers of the side bar and
// of the panel, then their views, then the output channels, in groups named by their container
// ("Side Bar", "Side Bar / Explorer", "Panel / Output"). Choosing one opens it.
func NewViewsProvider(o ViewsOptions) *ListProvider {
	return &ListProvider{
		ProviderInfo: ProviderInfo{
			Prefix: ViewsPrefix, Placeholder: "Type the name of a view, output channel or terminal to open.", ContextKey: "inViewsPicker",
			Help: []HelpEntry{{Description: "Open View", Command: OpenViewCommand}},
		},
		NoResults: "No matching views",
		Picks:     func(filter string) []Item { return viewPicks(o, filter) },
	}
}

func viewPicks(o ViewsOptions, filter string) []Item {
	var ctx contextkey.Context
	if o.Context != nil {
		ctx = o.Context()
	}
	var all []viewPick
	locations := []struct {
		loc   command.ViewLocation
		title string
	}{{command.ViewLocationSideBar, "Side Bar"}, {command.ViewLocationPanel, "Panel"}}
	for _, l := range locations {
		for _, c := range o.Views.Containers(l.loc) {
			id := c.ID
			all = append(all, viewPick{label: c.Title, container: l.title, accept: func() { o.OpenContainer(id) }})
		}
	}
	for _, l := range locations {
		for _, c := range o.Views.Containers(l.loc) {
			for _, v := range o.Views.Views(c.ID, ctx) {
				id := v.ID
				all = append(all, viewPick{label: v.Name, container: c.Title, accept: func() { o.OpenView(id) }})
			}
		}
	}
	if o.Channels != nil {
		for _, name := range o.Channels() {
			name := name
			all = append(all, viewPick{label: name, container: "Output", accept: func() {
				if o.OpenChannel != nil {
					o.OpenChannel(name)
				}
			}})
		}
	}

	type shown struct {
		viewPick
		match []fuzzy.Match
	}
	var picks []shown
	for _, e := range all {
		s := shown{viewPick: e}
		if filter != "" {
			m, ok := fuzzy.MatchesFuzzy(filter, e.label, true)
			if !ok && !fuzzy.FuzzyContains(e.container, filter) {
				continue
			}
			s.match = m
		}
		picks = append(picks, s)
	}
	// a group of a container that is an entry itself is named "Parent / Child"
	parent := map[string]string{}
	for _, e := range picks {
		if _, ok := parent[e.label]; !ok {
			parent[e.label] = e.container
		}
	}
	var items []Item
	last := ""
	for i, e := range picks {
		if i == 0 || e.container != last {
			last = e.container
			label := last
			if p, ok := parent[last]; ok {
				label = p + " / " + last
			}
			items = append(items, Item{Separator: true, Label: label})
		}
		accept := e.accept
		items = append(items, Item{Label: e.label, Highlights: Highlights{Label: e.match}, Accept: func(KeyMods) { accept() }})
	}
	return items
}
