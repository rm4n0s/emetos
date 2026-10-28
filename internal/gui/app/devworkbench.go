package app

import (
	"math"
	"path/filepath"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
)

// DevText is one run of text drawn on screen, as dev.texts reports it.
type DevText struct {
	// Text is the plain text of the paragraph.
	Text string `json:"text"`
	// X, Y, Width and Height are the place of the paragraph in window coordinates (logical pixels).
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	// Cut tells that the paragraph is narrower than its text: an ellipsis or a clip hides the end of it (step 17b.6).
	Cut bool `json:"cut"`
}

// DevTab is one editor tab of a group, as dev.workbench reports it.
type DevTab struct {
	// Name is the label of the tab, Kind the type id of its input (emetos.textEditor, ...).
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Active tells whether it is the shown editor of its group, Dirty whether it has unsaved changes, Preview whether it is
	// the group's preview editor and Pinned whether it is pinned.
	Active  bool `json:"active"`
	Dirty   bool `json:"dirty"`
	Preview bool `json:"preview"`
	Pinned  bool `json:"pinned"`
}

// DevGroup is one editor group, as dev.workbench reports it.
type DevGroup struct {
	// ID is the id of the group, Active tells whether it is the active group.
	ID     int  `json:"id"`
	Active bool `json:"active"`
	// Tabs are its editors in the order of the tabs.
	Tabs []DevTab `json:"tabs"`
}

// DevWindowTab is one tab of the strip of the window, as dev.workbench reports it.
type DevWindowTab struct {
	// Folder is the folder of the tab ("" for none), Kind the id of the kind of an extension's tab ("" for a workspace).
	Folder string `json:"folder"`
	Kind   string `json:"kind"`
	// Active tells whether it is the shown tab.
	Active bool `json:"active"`
}

// DevWorkbench is the state of the workbench of the active tab, as dev.workbench reports it.
type DevWorkbench struct {
	// WindowTabs are the tabs of the strip.
	WindowTabs []DevWindowTab `json:"windowTabs"`
	// Groups are the editor groups in the order they appear in the grid.
	Groups []DevGroup `json:"groups"`
	// SideBarVisible and ActiveView tell whether the side bar shows and which view container it holds.
	SideBarVisible bool   `json:"sideBarVisible"`
	ActiveView     string `json:"activeView"`
	// PanelVisible and ActivePanel tell whether the panel shows and which panel it holds.
	PanelVisible bool   `json:"panelVisible"`
	ActivePanel  string `json:"activePanel"`
}

// registerDevWorkbench registers dev.texts and dev.workbench (SPEC §15.2).
func registerDevWorkbench(p *Process) {
	texts := command.Meta{ID: "dev.texts", Description: "Report every run of text drawn on screen with its place, so a test can find a row or a label. Read-only; the dev server answers with the list."}
	bench := command.Meta{ID: "dev.workbench", Description: "Report the workbench of the active tab: the window tabs, the editor groups with their tabs, the side bar and the panel. Read-only; the dev server answers with it."}
	for _, q := range []struct {
		meta command.Meta
		run  func(p *Process, ctx command.Context) error
	}{{texts, devTexts}, {bench, devWorkbench}} {
		err := RegisterDev(p.Commands, q.meta, func(ctx command.Context, _ any) error { return q.run(p, ctx) })
		if err != nil {
			// the ids are fixed, new and start with dev., so a failure is a mistake here
			panic(err)
		}
	}
}

// devTexts answers dev.texts.
func devTexts(p *Process, ctx command.Context) error {
	binding := p.Workspaces.binding
	if binding == nil || binding.RootElement() == nil {
		return errors.New("DevTextsNoWindow", "the window is not up yet")
	}
	list := []DevText{}
	// an element that has no render object of its own answers with the one of its child, so a paragraph is met more than once
	seen := map[*rendering.RenderParagraph]bool{}
	var visit func(el widgets.Element)
	visit = func(el widgets.Element) {
		if r, ok := el.RenderObject().(*rendering.RenderParagraph); ok && r != nil && !seen[r] && r.Attached() && r.HasSize() {
			seen[r] = true
			if text := painting.InlineSpanToPlainText(r.Text(), painting.ToPlainTextOpts{ExcludeSemanticsLabels: true, ExcludePlaceholders: true}); text != "" {
				at := r.LocalToGlobal(zeroOffset, nil)
				size := r.Size()
				cut := size.Width+0.01 < r.ComputeMaxIntrinsicWidth(math.Inf(1))
				list = append(list, DevText{Text: text, X: at.Dx, Y: at.Dy, Width: size.Width, Height: size.Height, Cut: cut})
			}
		}
		el.DebugVisitOnstageChildren(visit)
	}
	binding.RootElement().DebugVisitOnstageChildren(visit)
	setDevAnswer(ctx.Ctx, list)
	return nil
}

// devWorkbench answers dev.workbench.
func devWorkbench(p *Process, ctx command.Context) error {
	active := p.Workspaces.Active()
	if active == nil || active.shell == nil {
		return errors.New("DevWorkbenchNoWorkspace", "no workspace tab is active")
	}
	out := DevWorkbench{WindowTabs: []DevWindowTab{}, Groups: []DevGroup{}}
	current := p.Workspaces.ActiveTab()
	for _, t := range p.Workspaces.Tabs() {
		w := DevWindowTab{Kind: t.KindID(), Active: t == current}
		if t.ws != nil && t.ws.Folder != "" {
			w.Folder = filepath.Clean(t.ws.Folder)
		}
		out.WindowTabs = append(out.WindowTabs, w)
	}
	if active.Layout != nil {
		st := active.Layout.State()
		out.SideBarVisible, out.PanelVisible = st.SideBarVisible, st.PanelVisible
	}
	if active.Views != nil {
		out.ActiveView, out.ActivePanel = active.Views.Active(), active.Views.ActivePanel()
	}
	grid := active.shell.group.grid
	if grid != nil {
		for _, g := range grid.Groups(editorgroup.GridAppearance) {
			dg := DevGroup{ID: g.ID(), Active: g == grid.Active(), Tabs: []DevTab{}}
			for _, e := range g.Model.Editors(editorgroup.Sequential, false) {
				dg.Tabs = append(dg.Tabs, DevTab{
					Name: e.Name(), Kind: e.TypeID(), Active: g.Model.IsActive(e), Dirty: e.Dirty(),
					Preview: !g.Model.IsPinned(e), Pinned: g.Model.IsPinned(e),
				})
			}
			out.Groups = append(out.Groups, dg)
		}
	}
	setDevAnswer(ctx.Ctx, out)
	return nil
}
