package scm

import (
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// Metrics of the rows, from VS Code's scm.css and the list it draws them in.
const (
	// rowHeight is the height of every row.
	rowHeight = 22.0
	// indentBase and indentStep are the room before the first level and the room each level adds (a tree's indent).
	indentBase = 8.0
	indentStep = 8.0
	// twistieWidth is the width of the chevron of a row that folds.
	twistieWidth = 16.0
	// iconGap is the room between a file icon and its name.
	iconGap = 6.0
	// rightPadding is the room after the content of a row (`.monaco-tl-contents > div { padding-right: 12px }`).
	rightPadding = 12.0
	// fontSize is the size of the text of a row.
	fontSize = 13.0
	// letterWidth is the room of the status letter of a resource, with the margin the label gives its badge.
	letterWidth = 16.0
	// countGap is the room before the count badge (`.count { margin-left: 6px }`).
	countGap = 6.0
)

// render builds the contents of a row of the list.
func (v *View) render(bc widgets.BuildContext, lr kit.ListViewRow, st kit.RowState) widgets.Widget {
	r := rowOf(lr)
	if r == nil {
		return kit.NewHighlightedLabel(bc, lr.Label, st)
	}
	switch r.kind {
	case kindRepository:
		return v.renderRepository(bc, r, st)
	case kindGroup:
		return v.renderGroup(bc, r, st)
	case kindFolder:
		return v.renderFolder(bc, r, st)
	case kindInput:
		return v.renderInput(bc, r)
	case kindActionButton:
		return v.renderActionButton(bc, r)
	}
	return v.renderResource(bc, r, st)
}

// indent is the room before a row of a depth.
func indent(depth int) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(indentBase + float64(depth)*indentStep)})
}

// twistie is the chevron of a row that folds, or the room of one on a row that does not.
func twistie(expandable, expanded bool, fg ui.Color) widgets.Widget {
	if !expandable {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(twistieWidth)})
	}
	code := fonts.CodiconChevronRight
	if expanded {
		code = fonts.CodiconChevronDown
	}
	return widgets.NewSizedBox(
		widgets.NewCenter(widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(fg)})),
		widgets.SizedBoxOpts{Width: foundation.Some(twistieWidth), Height: foundation.Some(rowHeight)})
}

func gap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

func line(children ...widgets.Widget) widgets.Widget {
	return widgets.NewRow(children, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

func oneLine(text string, style *painting.TextStyle) widgets.Widget {
	return widgets.NewText(text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// countBadge is the number after a group or a repository.
func countBadge(n int) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(countGap, 0, 0, 0), kit.NewBadge(n))
}

// renderGroup builds a group header: the chevron, the label, the inline actions of the group (while the pointer is over the row,
// or the row has the focus, or scm.alwaysShowActions is on) and the number of resources.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (ResourceGroupRenderer)
func (v *View) renderGroup(bc widgets.BuildContext, r *row, st kit.RowState) widgets.Widget {
	cells := []widgets.Widget{indent(r.depth), twistie(true, r.expanded, st.Foreground),
		widgets.NewExpanded(oneLine(r.group.Label(), kit.TextStyleOf(bc, fontSize, st.Foreground)))}
	if a := v.inlineActions(bc, r, st); a != nil {
		cells = append(cells, a)
	}
	cells = append(cells, countBadge(r.group.Len()), gap(rightPadding))
	return line(cells...)
}

// renderRepository builds a repository header: the chevron, the icon, the name (bold) with a description that tells two
// repositories of one name apart, then, at the right end, the items of the status bar of the repository, the title actions and
// the count.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmRepositoryRenderer.ts (RepositoryRenderer)
func (v *View) renderRepository(bc widgets.BuildContext, r *row, st kit.RowState) widgets.Widget {
	icon := fonts.CodiconRepo
	if len(v.model.Sources()) > 1 && v.model.IsVisible(r.src) {
		icon = fonts.CodiconRepoSelected
	}
	var right []widgets.Widget
	for _, c := range r.src.StatusBarCommands() {
		right = append(right, v.statusItem(bc, r.src, c, st))
	}
	if a := v.inlineActions(bc, r, st); a != nil {
		right = append(right, a)
	}
	cells := []widgets.Widget{
		indent(r.depth), twistie(true, r.expanded, st.Foreground),
		widgets.NewIcon(fonts.CodiconIcon(icon), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(st.Foreground)}), gap(2),
	}
	// the name keeps its room up to half of the row; the items and the buttons at the right end give way first
	// (`.scm-provider > .monaco-icon-label { flex: 0 1 auto }`, `> .actions { flex: 1 10000 auto }`)
	cells = append(cells, widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		return widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(c.MaxWidth / 2)}),
			v.repositoryLabel(bc, r.src, st))
	}))
	cells = append(cells, widgets.NewExpanded(widgets.NewRow(right, widgets.RowOpts{MainAxisAlignment: rendering.MainAxisAlignmentEnd,
		CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})))
	if v.model.ProviderCountVisible(r.src) {
		cells = append(cells, countBadge(r.src.BadgeCount()))
	}
	cells = append(cells, gap(rightPadding))
	if !r.src.Busy() {
		return line(cells...)
	}
	// the progress bar of a command that runs, under the header
	return widgets.NewStack([]widgets.Widget{line(cells...), widgets.NewPositioned(
		widgets.NewKeyedSubtree(widgets.NewSizedBox(kit.NewProgressBar(kit.ProgressBarOpts{Infinite: true}), widgets.SizedBoxOpts{Height: foundation.Some(2.0)}),
			widgets.WidgetOpts{Key: BusyKey}),
		widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Bottom: foundation.Some(0.0)})},
		widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// repositoryLabel is the name of a repository in bold, with the description that tells two of one name apart after it.
func (v *View) repositoryLabel(bc widgets.BuildContext, src *scmcore.Source, st kit.RowState) widgets.Widget {
	bold := kit.TextStyleOf(bc, fontSize, st.Foreground).Merge(painting.NewTextStyle(painting.TextStyleOpts{FontWeight: ui.FontWeightW700}))
	label := []widgets.Widget{widgets.NewFlexible(oneLine(src.Name(), bold))}
	if d := v.repositoryDescription(src); d != "" {
		label = append(label, gap(6), widgets.NewFlexible(oneLine(d, kit.TextStyleOf(bc, fontSize*0.9, st.Foreground.WithOpacity(0.7)))))
	}
	return widgets.NewRow(label, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// repositoryDescription tells two repositories with the same name apart: the label of the provider when they have one folder,
// else the folder they are in.
func (v *View) repositoryDescription(s *scmcore.Source) string {
	if s.RootURI() == "" {
		return ""
	}
	same, sameName := 0, 0
	for _, o := range v.model.Sources() {
		if o.RootURI() == "" {
			continue
		}
		if o.RootURI() == s.RootURI() {
			same++
		}
		if o.Name() == s.Name() {
			sameName++
		}
	}
	switch {
	case same > 1:
		return s.Label()
	case sameName > 1:
		dir := strings.TrimSuffix(s.RootPath(), "/"+s.Name())
		return dir
	}
	return ""
}

// statusItem is one of the status bar commands of a repository, drawn in its header: the icons and the text of its title, which a
// click runs.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/util.ts (StatusBarActionViewItem)
func (v *View) statusItem(bc widgets.BuildContext, src *scmcore.Source, c scmcore.StatusCommand, st kit.RowState) widgets.Widget {
	style := kit.TextStyleOf(bc, fontSize, st.Foreground)
	var cells []widgets.Widget
	for _, run := range workbench.ParseStatusText(c.Text) {
		switch {
		case run.Icon != "":
			if code, ok := fonts.CodiconByName(run.Icon); ok {
				cells = append(cells, widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(st.Foreground)}))
			}
		case run.Text != "":
			cells = append(cells, widgets.NewFlexible(oneLine(strings.TrimSpace(run.Text), style)), gap(2))
		}
	}
	var w widgets.Widget = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(4, 0, 0, 0), widgets.NewRow(cells, widgets.RowOpts{
		MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	if c.Command != "" {
		cmd, args := c.Command, c.Args
		w = widgets.NewMouseRegion(widgets.NewGestureDetector(w, widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { v.run(cmd, args) }}),
			widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
	}
	if c.Tooltip != "" {
		w = kit.NewHover(w, kit.HoverOpts{Text: c.Tooltip, Compact: true})
	}
	return widgets.NewFlexible(w)
}

// renderFolder builds a folder row of the tree view mode: the chevron, the icon of the folder, its name (the names of a chain of
// folders joined by "/") and the inline actions of the folder.
func (v *View) renderFolder(bc widgets.BuildContext, r *row, st kit.RowState) widgets.Widget {
	cells := []widgets.Widget{indent(r.depth), twistie(true, r.expanded, st.Foreground)}
	if ic := v.folderIcon(bc, r.entry.Label, r.expanded); ic != nil {
		cells = append(cells, kit.NewFileIcon(*ic, st.Foreground), gap(iconGap))
	}
	cells = append(cells, widgets.NewExpanded(oneLine(r.entry.Label, kit.TextStyleOf(bc, fontSize, st.Foreground))))
	if a := v.inlineActions(bc, r, st); a != nil {
		cells = append(cells, a)
	}
	cells = append(cells, gap(rightPadding))
	return line(cells...)
}

// renderResource builds a changed file: the icon of the file, its name (struck through for a deleted file) with the folder it is
// in dimmed after it in the list view mode, the inline actions, and at the right end the letter of its decoration in the
// color of the decoration (or the icon the provider gave).
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (ResourceRenderer)
func (v *View) renderResource(bc widgets.BuildContext, r *row, st kit.RowState) widgets.Widget {
	colors := kit.ThemeOf(bc)
	res := r.res
	dec := res.Decorations
	fg := st.Foreground
	treeMode := v.model.ViewMode() == scmcore.ViewTree
	cells := []widgets.Widget{indent(r.depth)}
	if treeMode {
		cells = append(cells, twistie(false, false, fg))
	}
	if ic, ok := v.fileIcon(bc, res); ok {
		cells = append(cells, kit.NewFileIcon(ic, fg), gap(iconGap))
	}
	nameStyle, dim := kit.TextStyleOf(bc, fontSize, fg), kit.TextStyleOf(bc, fontSize*0.9, fg.WithOpacity(0.7))
	if dec.StrikeThrough {
		strike := painting.NewTextStyle(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationLineThrough)})
		nameStyle, dim = nameStyle.Merge(strike), dim.Merge(strike)
	}
	label := []widgets.Widget{widgets.NewFlexible(oneLine(res.Name(), nameStyle))}
	if !treeMode {
		if dir := v.relativeDir(res); dir != "" {
			label = append(label, gap(6), widgets.NewFlexible(oneLine(dir, dim)))
		}
	}
	cells = append(cells, widgets.NewExpanded(line(label...)))
	if a := v.inlineActions(bc, r, st); a != nil {
		cells = append(cells, a)
	}
	letter, color := "", fg
	if d, ok := v.decoration(res); ok {
		letter = d.Badge
		if c, ok := colors.Named(d.Color); ok {
			color = c
		}
	}
	switch {
	case dec.Icon != "":
		if code, ok := fonts.CodiconByName(dec.Icon); ok {
			ic := fg
			if c, ok := colors.Named(dec.Color); ok {
				ic = c
			}
			cells = append(cells, gap(5), widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(ic)}), gap(3))
		}
	case letter != "":
		style := kit.TextStyleOf(bc, fontSize*0.9, color).Merge(painting.NewTextStyle(painting.TextStyleOpts{FontWeight: ui.FontWeightW600}))
		badge := widgets.NewSizedBox(widgets.NewCenter(oneLine(letter, style)), widgets.SizedBoxOpts{Width: foundation.Some(letterWidth)})
		if d, ok := v.decoration(res); ok && d.Tooltip != "" {
			cells = append(cells, gap(5), kit.NewHover(badge, kit.HoverOpts{Text: d.Tooltip, Compact: true}), gap(3))
		} else {
			cells = append(cells, gap(5), badge, gap(3))
		}
	}
	cells = append(cells, gap(rightPadding-3))
	w := v.dragRow(r, line(cells...))
	if dec.Faded {
		w = widgets.NewOpacity(0.7, w)
	}
	return w
}

// decoration is the decoration the service gives a resource: its letter, tooltip and color.
func (v *View) decoration(res *scmcore.Resource) (Decoration, bool) {
	if v.opts.Decoration == nil {
		return v.providerLetter(res)
	}
	d, ok := v.opts.Decoration(res.URI)
	if !ok || d.Badge == "" {
		return v.providerLetter(res)
	}
	return d, true
}

// providerLetter is the letter the provider gave the resource itself (ResourceDecorations.Letter), for a file the decoration
// service says nothing about.
//
// TODO(step 24.6): the Git extension gives the letters through api.Decorations(); this goes with the field.
func (v *View) providerLetter(res *scmcore.Resource) (Decoration, bool) {
	d := res.Decorations
	if d.Letter == "" {
		return Decoration{}, false
	}
	return Decoration{Badge: d.Letter, Tooltip: d.Tooltip, Color: d.Color}, true
}

// relativeDir is the folder of a resource below the root of its repository, "" for a file at the root.
func (v *View) relativeDir(res *scmcore.Resource) string {
	dir := res.Dir()
	root := res.Group.Source().RootPath()
	if root != "" {
		if rest, ok := strings.CutPrefix(dir, root); ok {
			return strings.TrimPrefix(rest, "/")
		}
	}
	return dir
}

// fileIcon is the icon of the file icon theme for a resource.
func (v *View) fileIcon(bc widgets.BuildContext, res *scmcore.Resource) (theme.FileIcon, bool) {
	if v.opts.Icons == nil {
		return theme.FileIcon{}, false
	}
	th := v.opts.Icons()
	if th == nil {
		return theme.FileIcon{}, false
	}
	q := theme.FileIconQuery{Kind: theme.FileIconFile, Name: res.Name(), Type: kit.ThemeOf(bc).Type}
	if v.opts.Language != nil {
		q.LanguageID = v.opts.Language(res.Path)
	}
	return th.IconFor(q)
}

// folderIcon is the icon of a folder.
func (v *View) folderIcon(bc widgets.BuildContext, name string, expanded bool) *theme.FileIcon {
	if v.opts.Icons == nil {
		return nil
	}
	th := v.opts.Icons()
	if th == nil {
		return nil
	}
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	ic, ok := th.IconFor(theme.FileIconQuery{Kind: theme.FileIconFolder, Name: name, Expanded: expanded, Type: kit.ThemeOf(bc).Type})
	if !ok {
		return nil
	}
	return &ic
}

// paddingLeft is an inset of some room at the left.
func paddingLeft(w float64) painting.EdgeInsets { return painting.NewEdgeInsetsFromLTRB(w, 0, 0, 0) }
