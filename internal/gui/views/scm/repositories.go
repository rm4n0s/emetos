package scm

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Repositories is the state of the Repositories view: every repository of the model as a row, the selected rows being the
// repositories the Changes view shows. Selecting rows chooses which repositories are shown; with the single selection mode one row
// is selected.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmRepositoriesViewPane.ts
type Repositories struct {
	view    *View
	list    *kit.ListViewModel
	ctl     *kit.ListController
	stop    func()
	syncing bool
}

// NewRepositories makes the Repositories view for the model of a Changes view. The Changes view must be on the same model.
func NewRepositories(v *View) *Repositories {
	r := &Repositories{view: v, ctl: kit.NewListController(), list: kit.NewListViewModel(kit.ListViewModelOpts{MultiSelect: true})}
	r.stop = v.OnChange(r.sync)
	r.sync()
	return r
}

// Dispose stops following the view.
func (r *Repositories) Dispose() { r.stop() }

// List is the model of the list.
func (r *Repositories) List() *kit.ListViewModel { return r.list }

// Focus gives the list the keyboard focus.
func (r *Repositories) Focus() { r.ctl.Focus() }

// repoRow is the data of a row of the Repositories view.
type repoRow struct{ src *scmcore.Source }

// sync makes the rows and the selection from the model.
func (r *Repositories) sync() {
	m := r.view.model
	var items []kit.ListViewItem
	for _, s := range m.Sources() {
		items = append(items, kit.ListViewItem{Key: "r:" + s.Key(), Label: s.Name(), Data: repoRow{s}})
	}
	r.syncing = true
	_ = r.list.SetItems(items) // keys are the handles: unique
	var sel []string
	for _, s := range m.Visible() {
		sel = append(sel, "r:"+s.Key())
	}
	r.list.SetSelection(sel)
	r.syncing = false
}

// selectionChanged shows the repositories of the selected rows; the last one cannot be hidden.
func (r *Repositories) selectionChanged(keys []string, _ []int) {
	if r.syncing || len(keys) == 0 {
		return
	}
	var list []*scmcore.Source
	for _, k := range keys {
		if it, ok := r.list.Item(k); ok {
			list = append(list, it.Data.(repoRow).src)
		}
	}
	r.view.model.SetVisible(list)
}

// NewRepositoriesBody makes the body of the Repositories view.
func NewRepositoriesBody(r *Repositories) widgets.Widget {
	return &repositoriesBody{StatefulWidget: widgets.NewStatefulWidgetBase(), r: r}
}

type repositoriesBody struct {
	widgets.StatefulWidget
	r *Repositories
}

func (b *repositoriesBody) CreateState() widgets.State { return &repositoriesState{} }

type repositoriesState struct {
	widgets.StateOf[*repositoriesBody]
	stop func()
}

func (s *repositoriesState) InitState() {
	s.stop = s.Widget().r.view.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

func (s *repositoriesState) Dispose() { s.stop() }

func (s *repositoriesState) Build(widgets.BuildContext) widgets.Widget {
	r := s.Widget().r
	return kit.NewList(r.list, kit.ListOpts{
		Key: RepositoriesListKey, Controller: r.ctl, MultiSelect: true, RowPadding: foundation.Some(0.0), TypeToFind: foundation.Some(false),
		Render: r.render, OnSelectionChange: r.selectionChanged, OnContextMenu: r.contextMenu,
	})
}

func (r *Repositories) contextMenu(lr kit.ListViewRow, at ui.Offset) {
	d, ok := lr.Data.(repoRow)
	if !ok {
		return
	}
	row := &row{kind: kindRepository, src: d.src}
	if r.view.opts.ShowMenu != nil {
		r.view.opts.ShowMenu(contextMenuOf(row), rowContext(row), []any{scmcore.SourceArg(d.src)}, at)
	}
}

// render builds a row: the icon (selected when the repository is shown), the name in bold with its description, the items of its
// status bar and the count.
func (r *Repositories) render(bc widgets.BuildContext, lr kit.ListViewRow, st kit.RowState) widgets.Widget {
	d, ok := lr.Data.(repoRow)
	if !ok {
		return kit.NewHighlightedLabel(bc, lr.Label, st)
	}
	v := r.view
	icon := v.repositoryIcon(d.src)
	var right []widgets.Widget
	for _, c := range d.src.StatusBarCommands() {
		right = append(right, v.statusItem(bc, d.src, c, st))
	}
	cells := []widgets.Widget{gap(indentBase),
		widgets.NewIcon(fonts.CodiconIcon(icon), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(st.Foreground)}), gap(2),
		widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
			return widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(c.MaxWidth / 2)}),
				v.repositoryLabel(bc, d.src, st))
		}),
		widgets.NewExpanded(widgets.NewRow(right, widgets.RowOpts{MainAxisAlignment: rendering.MainAxisAlignmentEnd,
			CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))}
	if v.model.ProviderCountVisible(d.src) {
		cells = append(cells, countBadge(d.src.BadgeCount()))
	}
	cells = append(cells, gap(rightPadding))
	return line(cells...)
}
