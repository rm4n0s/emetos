package testext

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The ids of the second test extension, which exercises the tabs of the contract (step 17e.1): an editor tab kind of which
// several are open at once, an editor title action that opens one in the group of its tab strip, a window tab kind that keeps
// its state, and the events of other window tabs. Notes and Scratchpad did this before they were removed (SPEC §10.26).
const (
	// TabsID is the id of the extension.
	TabsID = "emetos.testtabs"
	// PageKindID is the editor tab kind, NewPageCommand the command that opens a page (the editor title action runs it) and
	// PanelKindID the window tab kind.
	PageKindID     = "emetos.testtabs.page"
	NewPageCommand = "testtabs.newPage"
	PanelKindID    = "emetos.testtabs.panel"
)

// untitledPage is the title of a page with no text.
const untitledPage = "Untitled Page"

// NewTabs is the Factory of the extension. Each call makes an extension of its own, as the contract asks.
func NewTabs() extension.Extension { return &tabsExt{} }

// pageState is the state of a page tab. Text is a pointer so that a state with no text is told from an empty one.
type pageState struct {
	Text *string `json:"text"`
}

// panelState is the state of the window tab.
type panelState struct {
	Text string `json:"text"`
}

// tabsExt is the extension of one scope. Everything in it runs on the UI thread.
type tabsExt struct {
	api extension.API
	// pages are the open page tabs.
	pages []*pageTab
	// The window tab, when this scope is one.
	panel     extension.WindowTab
	panelText *ui.InputBoxController
	// changes are the documents of other tabs that changed, newest first, and revoked names the tab whose events were taken away.
	changes []string
	revoked string
}

// Manifest implements extension.Extension.
func (*tabsExt) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: TabsID, Name: "Test Tabs", Version: "0.0.1",
		Description: "Exercises the tabs of the extension contract: editor tabs, window tabs and the events of other tabs.",
		Contributes: extension.Contributes{
			Commands:       []extension.CommandSpec{{ID: NewPageCommand, Title: "New Page", Category: "Test Tabs"}},
			EditorKinds:    []extension.EditorKindSpec{{ID: PageKindID, Name: "Page", Icon: "note"}},
			EditorActions:  []extension.EditorActionSpec{{Command: NewPageCommand, Icon: "new-file", Tooltip: "New Page", Order: 10}},
			WindowTabKinds: []extension.WindowTabKindSpec{{ID: PanelKindID, Title: "Test Panel", Icon: "note", PlusMenuLabel: "Test Panel"}},
		},
		Events: extension.EventDecl{CrossTab: []string{extension.TopicDocumentChanged, extension.TopicSharingRevoked}},
	}
}

// Activate implements extension.Extension.
func (e *tabsExt) Activate(_ context.Context, api extension.API) error {
	e.api = api
	if _, err := api.Events().Subscribe(extension.TopicDocumentChanged, e.onChanged, extension.FromOtherScopes()); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicSharingRevoked, e.onRevoked); err != nil {
		return err
	}
	if err := api.Editors().Provide(PageKindID, extension.EditorKind{Create: e.createPage}); err != nil {
		return err
	}
	if err := api.WindowTabs().Provide(PanelKindID, extension.WindowTabKind{Create: e.createPanel}); err != nil {
		return err
	}
	return api.Commands().Handle(NewPageCommand, e.newPage)
}

// ---- the editor tab ----

// pageTab is an editor tab that shows one text, edited in place.
type pageTab struct {
	e    *tabsExt
	tab  extension.EditorTab
	text *ui.InputBoxController
}

// titleOf is the title of a page: the first line of its text.
func titleOf(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if line = strings.TrimSpace(line); line != "" {
		return line
	}
	return untitledPage
}

// newPage is the command that opens a page in the group named by the argument (the editor title action gives its group).
func (e *tabsExt) newPage(_ context.Context, args any) error {
	text := ""
	_, err := e.api.Editors().Open(PageKindID, pageState{Text: &text}, extension.EditorOpenOptions{Group: extension.GroupOfArgs(args)})
	return err
}

// createPage makes the content of a tab, for a new tab and for one that came back after a restart.
func (e *tabsExt) createPage(tab extension.EditorTab, state json.RawMessage) (extension.EditorTabContent, error) {
	var st pageState
	if len(state) > 0 {
		if err := json.Unmarshal(state, &st); err != nil {
			return extension.EditorTabContent{}, errors.NewErr("TestTabsPageStateInvalid", err)
		}
	}
	if st.Text == nil {
		return extension.EditorTabContent{}, errors.New("TestTabsPageStateInvalid", "the state of a page tab has no text")
	}
	p := &pageTab{e: e, tab: tab, text: ui.NewInputBoxController(*st.Text)}
	e.pages = append(e.pages, p)
	tab.SetTitle(titleOf(*st.Text))
	return extension.EditorTabContent{
		Build: p.build,
		SaveState: func() (any, error) {
			text := p.text.Value()
			return pageState{Text: &text}, nil
		},
		OnClose: p.close,
	}, nil
}

// close forgets the tab.
func (p *pageTab) close() {
	for i, x := range p.e.pages {
		if x == p {
			p.e.pages = append(p.e.pages[:i:i], p.e.pages[i+1:]...)
			break
		}
	}
	p.text.Dispose()
}

// changed is called when the user edits the text: the tab is named after it.
func (p *pageTab) changed() { p.tab.SetTitle(titleOf(p.text.Value())) }

// build is the widget of the tab.
func (p *pageTab) build(widgets.BuildContext) widgets.Widget {
	return ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: p.text, Placeholder: "Write here...", MultiLine: true, OnChanged: func(string) { p.changed() }})
}

// ---- the window tab ----

// createPanel makes the content of the window tab with the text it saved.
func (e *tabsExt) createPanel(tab extension.WindowTab, raw json.RawMessage) (extension.WindowTabContent, error) {
	var st panelState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return extension.WindowTabContent{}, errors.NewErr("TestTabsPanelStateInvalid", err)
		}
	}
	e.panel = tab
	e.panelText = ui.NewInputBoxController(st.Text)
	return extension.WindowTabContent{
		Build:     e.buildPanel,
		SaveState: func() (any, error) { return panelState{Text: e.panelText.Value()}, nil },
		OnClose:   e.panelText.Dispose,
	}, nil
}

// onChanged is the events of the documents of other tabs that changed: each is noted, the newest first.
func (e *tabsExt) onChanged(ev extension.Event) {
	changes, err := extension.EventValues[extension.DocumentChange](ev)
	if err != nil {
		e.api.Log().Warn("an event of a document cannot be read", "err", err)
		return
	}
	for _, c := range changes {
		e.changes = append([]string{ev.Scope.Name + " " + c.URI}, e.changes...)
	}
	e.refresh()
}

// onRevoked is the notice that the events of a tab no longer reach this one.
func (e *tabsExt) onRevoked(ev extension.Event) {
	r, err := extension.EventValue[extension.SharingRevoked](ev)
	if err != nil {
		return
	}
	e.revoked = r.FromName
	if e.revoked == "" {
		e.revoked = r.From
	}
	e.refresh()
}

// refresh builds the window tab again.
func (e *tabsExt) refresh() {
	if e.panel != nil {
		e.panel.Refresh()
	}
}

// buildPanel is the widget of the window tab: the text, and the line that says how many changes of other tabs came.
func (e *tabsExt) buildPanel(widgets.BuildContext) widgets.Widget {
	status := strconv.Itoa(len(e.changes)) + " changes of other tabs"
	if e.revoked != "" {
		status = "The edits of " + e.revoked + " no longer reach this tab."
	}
	return widgets.NewColumn([]widgets.Widget{
		widgets.NewText(status),
		widgets.NewExpanded(ui.NewInputBox(nil, nil, ui.InputBoxOpts{
			Controller: e.panelText, Placeholder: "Write here...", MultiLine: true,
			OnChanged: func(string) {
				if e.panel != nil {
					e.panel.StateChanged()
				}
			},
		})),
	})
}
