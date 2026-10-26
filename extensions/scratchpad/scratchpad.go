// Package scratchpad is the extension that adds a window tab to the strip of the window (SPEC §10.15 "As built (step
// 11.7)"). The tab "Scratchpad" has a text for notes, which the tab keeps in its state across restarts, and a feed "Recent
// edits" of the documents that change in the other window tabs. The feed is made of the events of the other tabs
// (emetos.document.changed), which reach the tab only along a link that the user allowed, so the extension exercises the
// question and the links of the event sharing from a window tab of its own.
package scratchpad

import (
	"context"
	"encoding/json"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The ids of the extension.
const (
	// ID is the id of the extension and KindID the kind of its window tab.
	ID     = "emetos.scratchpad"
	KindID = "emetos.scratchpad.tab"
)

// maxRecent is how many entries the feed keeps, and rowHeight the height of the row of an entry: the name of the document
// and, under it, where the change came from.
const (
	maxRecent = 40
	rowHeight = 40.0
)

// New is the Factory of the extension.
func New() extension.Extension { return &scratchpad{now: time.Now} }

// NewWithClock is New with the clock that stamps the entries of the feed; tests give it a fixed one.
func NewWithClock(now func() time.Time) extension.Factory {
	return func() extension.Extension { return &scratchpad{now: now} }
}

// Manifest implements extension.Extension.
func (*scratchpad) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Scratchpad", Version: "0.1.0",
		Description: "A window tab with notes that stay across restarts and a feed of the edits made in the other tabs.",
		Contributes: extension.Contributes{
			WindowTabKinds: []extension.WindowTabKindSpec{{ID: KindID, Title: "Scratchpad", Icon: "note", PlusMenuLabel: "Scratchpad"}},
		},
		// the feed hears the document events of the other tabs the user allows
		Events: extension.EventDecl{CrossTab: []string{extension.TopicDocumentChanged, extension.TopicSharingRevoked}},
	}
}

// state is what the tab keeps: the notes.
type state struct {
	Notes string `json:"notes"`
}

// entry is a line of the feed: a document of another tab that changed, how many changes came and when the last did.
type entry struct {
	workspace, uri string
	changes        int
	at             time.Time
}

// scratchpad is the extension of one scope: the one window tab. Everything in it runs on the UI thread.
type scratchpad struct {
	api   extension.API
	now   func() time.Time
	tab   extension.WindowTab
	notes *ui.InputBoxController
	feed  *ui.ListViewModel
	// recent are the entries, the newest first.
	recent []*entry
	// stopped says that the events of a tab were taken away, and which tab.
	stopped string
}

// Activate implements extension.Extension.
func (s *scratchpad) Activate(_ context.Context, api extension.API) error {
	s.api = api
	s.feed = ui.NewListViewModel(ui.ListViewModelOpts{HeightOf: func(int) float64 { return rowHeight }})
	if _, err := api.Events().Subscribe(extension.TopicDocumentChanged, s.onChanged, extension.FromOtherScopes()); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicSharingRevoked, s.onRevoked); err != nil {
		return err
	}
	return api.WindowTabs().Provide(KindID, extension.WindowTabKind{Create: s.create})
}

// create makes the content of the tab with the notes it saved.
func (s *scratchpad) create(tab extension.WindowTab, raw json.RawMessage) (extension.WindowTabContent, error) {
	var st state
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return extension.WindowTabContent{}, errors.NewErr("ScratchpadStateInvalid", err)
		}
	}
	s.tab = tab
	s.notes = ui.NewInputBoxController(st.Notes)
	return extension.WindowTabContent{
		Build:     s.build,
		SaveState: func() (any, error) { return state{Notes: s.notes.Value()}, nil },
		OnClose:   s.notes.Dispose,
	}, nil
}

// onChanged is the events of the documents of other tabs that changed: every change is one more on the entry of its document,
// and a document that changed since is moved to the top.
func (s *scratchpad) onChanged(e extension.Event) {
	changes, err := extension.EventValues[extension.DocumentChange](e)
	if err != nil {
		s.api.Log().Warn("an event of a document cannot be read", "err", err)
		return
	}
	for _, c := range changes {
		s.record(e.Scope.Name, c.URI)
	}
	s.sync()
}

// record notes a change of a document of a tab.
func (s *scratchpad) record(workspace, uri string) {
	at := s.now()
	for i, x := range s.recent {
		if x.workspace == workspace && x.uri == uri {
			x.changes++
			x.at = at
			copy(s.recent[1:i+1], s.recent[:i])
			s.recent[0] = x
			return
		}
	}
	s.recent = append([]*entry{{workspace: workspace, uri: uri, changes: 1, at: at}}, s.recent...)
	if len(s.recent) > maxRecent {
		s.recent = s.recent[:maxRecent]
	}
}

// onRevoked is the notice that the events of a tab no longer reach this one.
func (s *scratchpad) onRevoked(e extension.Event) {
	r, err := extension.EventValue[extension.SharingRevoked](e)
	if err != nil {
		return
	}
	s.stopped = r.FromName
	if s.stopped == "" {
		s.stopped = r.From
	}
	s.refresh()
}

// sync makes the list show the entries.
func (s *scratchpad) sync() {
	items := make([]ui.ListViewItem, len(s.recent))
	for i, x := range s.recent {
		items[i] = ui.ListViewItem{Key: x.workspace + "\x00" + x.uri, Data: x, Label: nameOf(x.uri)}
	}
	if err := s.feed.SetItems(items); err != nil {
		s.api.Log().Warn("the feed cannot be shown", "err", err)
	}
	s.refresh()
}

// refresh builds the tab again.
func (s *scratchpad) refresh() {
	if s.tab != nil {
		s.tab.Refresh()
	}
}

// nameOf is the base name of a document: the file of a file URI, the URI itself otherwise.
func nameOf(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Scheme == "file" && u.Path != "" {
		return path.Base(u.Path)
	}
	return uri
}

// detail is the line under the name of an entry: the tab it comes from, how many changes, and when the last came.
func (x *entry) detail() string {
	n := strconv.Itoa(x.changes) + " change"
	if x.changes != 1 {
		n += "s"
	}
	return x.workspace + " · " + n + " · " + x.at.Format("15:04:05")
}

// changed is called when the user edits the notes: the tab asks for its state to be written.
func (s *scratchpad) changed() {
	if s.tab != nil {
		s.tab.StateChanged()
	}
}

// build is the widget of the tab: the notes on the left, the feed on the right.
func (s *scratchpad) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	heading := func(text string) widgets.Widget {
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 0, 8), widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 11, colors.DescriptionForeground)}))
	}
	notes := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: s.notes, Placeholder: "Write notes here. They stay across restarts.", MultiLine: true,
		OnChanged: func(string) { s.changed() },
	})
	left := widgets.NewColumn([]widgets.Widget{heading("NOTES"), widgets.NewExpanded(notes)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	right := widgets.NewColumn([]widgets.Widget{heading("RECENT EDITS"), widgets.NewExpanded(s.buildFeed(bc, colors))},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	divider := widgets.NewSizedBox(widgets.NewColoredBox(colors.PanelBorder, widgets.NewSizedBox(nil)), widgets.SizedBoxOpts{Width: foundation.Some(1.0)})
	gap := widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(16.0)})
	return widgets.NewColoredBox(colors.EditorBackground, widgets.NewPadding(painting.NewEdgeInsetsAll(16), widgets.NewRow([]widgets.Widget{
		widgets.NewFlexible(left, widgets.FlexibleOpts{Flex: foundation.Some(3)}), gap, divider, gap, widgets.NewFlexible(right, widgets.FlexibleOpts{Flex: foundation.Some(2)}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})))
}

// buildFeed is the list of the entries, or the words that say how they come.
func (s *scratchpad) buildFeed(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	var lines []widgets.Widget
	if s.stopped != "" {
		lines = append(lines, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 0, 8),
			widgets.NewText("The edits of "+s.stopped+" no longer reach this tab.", widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.Foreground)})))
	}
	if len(s.recent) == 0 {
		lines = append(lines, widgets.NewText("No edits yet. The documents you change in the other tabs are listed here once you allow their events: the first edit asks.",
			widgets.TextOpts{Style: dim}))
		return widgets.NewColumn(lines, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	}
	list := ui.NewList(s.feed, ui.ListOpts{
		Render: func(bc widgets.BuildContext, row ui.ListViewRow, _ ui.RowState) widgets.Widget {
			x, _ := row.Data.(*entry)
			if x == nil {
				return widgets.NewSizedBox(nil)
			}
			return widgets.NewRow([]widgets.Widget{
				ui.Icon("edit", 16, colors.DescriptionForeground),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
				widgets.NewExpanded(widgets.NewColumn([]widgets.Widget{
					widgets.NewText(nameOf(x.uri), widgets.TextOpts{Overflow: painting.TextOverflowEllipsis, MaxLines: foundation.Some(1)}),
					widgets.NewText(x.detail(), widgets.TextOpts{Style: dim, Overflow: painting.TextOverflowEllipsis, MaxLines: foundation.Some(1)}),
				}, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStart})),
			}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		},
	})
	lines = append(lines, widgets.NewExpanded(list))
	return widgets.NewColumn(lines, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}
