package search

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The metrics of the view (searchview.css): the gutter of the chevron, the space around the boxes, the rows of the results.
const (
	gutterWidth   = 20.0
	boxPadding    = 12.0
	rowGap        = 6.0
	fontSize      = 13.0
	messageSize   = 13.0
	labelSize     = 11.0
	beforeContext = 28 // the runes of the line shown before a match, with an ellipsis before them when there are more
)

// removedBackground and insertedBackground color the text a replace would take away and put in (diffEditor.removedTextBackground
// and insertedTextBackground of the dark themes).
var (
	removedBackground  = gui.NewColorFromARGB(0x33, 0xff, 0x00, 0x00)
	insertedBackground = gui.NewColorFromARGB(0x33, 0x9b, 0xb9, 0x55)
)

// build makes the widget of the view.
func (v *view) build(bc widgets.BuildContext) widgets.Widget {
	v.built = true
	colors := ui.Theme(bc)
	v.takeFocus()
	children := []widgets.Widget{
		widgets.NewKeyedSubtree(v.progress(), widgets.WidgetOpts{Key: foundation.NewKey("progress")}),
		widgets.NewKeyedSubtree(v.header(bc, colors), widgets.WidgetOpts{Key: foundation.NewKey("header")}),
		widgets.NewKeyedSubtree(v.messages(bc, colors), widgets.WidgetOpts{Key: foundation.NewKey("message")}),
		widgets.NewKeyedSubtree(widgets.NewExpanded(v.results(bc)), widgets.WidgetOpts{Key: foundation.NewKey("results")}),
	}
	column := widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return widgets.NewFocus(column, widgets.FocusOpts{
		CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "Search",
		OnFocusChange: func(has bool) {
			v.focus.view = has
			v.updateKeys()
		},
	})
}

// takeFocus gives the keyboard to the box or the list a command asked for, once the widgets are there.
func (v *view) takeFocus() {
	want := v.wantFocus
	if want == "" {
		return
	}
	v.wantFocus = ""
	focus := func() {
		switch want {
		case "search":
			v.searchBox.Focus()
			v.searchBox.SelectAll()
		case "replace":
			v.replaceBox.Focus()
			v.replaceBox.SelectAll()
		case "list":
			v.tree.ctl.Focus()
		}
	}
	v.api.Post(focus)
	time.AfterFunc(80*time.Millisecond, func() {
		v.api.Post(func() {
			if (want == "search" && !v.searchBox.HasFocus()) || (want == "replace" && !v.replaceBox.HasFocus()) {
				focus()
			}
		})
	})
}

// progress is the bar across the top of the view while a search runs.
func (v *view) progress() widgets.Widget {
	if !v.status.running {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(2.0)})
	}
	return widgets.NewSizedBox(ui.NewProgressBar(ui.ProgressBarOpts{Infinite: true}), widgets.SizedBoxOpts{Height: foundation.Some(2.0)})
}

func gap(w, h float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w), Height: foundation.Some(h)})
}

func label(bc widgets.BuildContext, s string, size float64, color gui.Color) widgets.Widget {
	return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, size, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// header is the widget of the search: the chevron of the replace box, the search box and the replace box, and the details.
func (v *view) header(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	chevron := "chevron-right"
	if v.replaceOpen {
		chevron = "chevron-down"
	}
	toggle := ui.NewIconButton(chevron, ui.IconButtonOpts{
		Tooltip: "Toggle Replace", OnPressed: func() {
			v.replaceOpen = !v.replaceOpen
			v.replacer = nil
			if v.replaceOpen {
				v.wantFocus = "replace"
			}
			v.updateKeys()
			v.refreshView()
		},
	})
	boxes := []widgets.Widget{widgets.NewKeyedSubtree(v.searchRow(), widgets.WidgetOpts{Key: foundation.NewKey("search")})}
	if v.replaceOpen {
		boxes = append(boxes, gap(0, rowGap), widgets.NewKeyedSubtree(v.replaceRow(), widgets.WidgetOpts{Key: foundation.NewKey("replace")}))
	}
	boxes = append(boxes, gap(0, 2), widgets.NewKeyedSubtree(v.detailsRow(bc, colors), widgets.WidgetOpts{Key: foundation.NewKey("details")}))
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(2, 6, boxPadding, 0), widgets.NewRow([]widgets.Widget{
		widgets.NewSizedBox(widgets.NewAlign(toggle, widgets.AlignOpts{Alignment: painting.AlignmentTopCenter}), widgets.SizedBoxOpts{Width: foundation.Some(gutterWidth - 2), Height: foundation.Some(25.0)}),
		widgets.NewExpanded(widgets.NewColumn(boxes, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}))
}

func (v *view) flagToggle(icon, tip, key string, on bool, set func(bool)) widgets.Widget {
	return ui.NewToggle(icon, ui.ToggleOpts{Key: foundation.NewKey(key), Checked: on, Tooltip: tip, NotFocusable: true, OnChanged: set})
}

func (v *view) searchRow() widgets.Widget {
	var msg *ui.InputMessage
	if v.queryInvalid && v.status.err != nil {
		msg = &ui.InputMessage{Content: describe(v.status.err), Type: ui.ValidationError}
	}
	return ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: v.searchBox, Placeholder: "Search", History: v.history, Message: msg,
		Invalid: msg != nil,
		Trailing: []widgets.Widget{
			v.flagToggle("case-sensitive", "Match Case (Alt+C)", "case", v.caseSensitive, func(b bool) { v.setFlag(&v.caseSensitive, b) }),
			v.flagToggle("whole-word", "Match Whole Word (Alt+W)", "word", v.wholeWord, func(b bool) { v.setFlag(&v.wholeWord, b) }),
			v.flagToggle("regex", "Use Regular Expression (Alt+R)", "regex", v.regex, func(b bool) { v.setFlag(&v.regex, b) }),
		},
		OnChanged: func(string) { v.queryChanged(false) },
		OnSubmit:  func(string) { v.queryChanged(true) },
		OnFocusChange: func(f bool) {
			v.focus.search = f
			v.updateKeys()
		},
	})
}

func (v *view) replaceRow() widgets.Widget {
	box := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: v.replaceBox, Placeholder: "Replace", History: v.replaceHistory,
		Trailing: []widgets.Widget{
			v.flagToggle("preserve-case", "Preserve Case (Alt+P)", "preserve", v.preserveCase, func(b bool) { v.setFlag(&v.preserveCase, b) }),
		},
		OnChanged: func(string) { v.replacer = nil; v.updateKeys(); v.refreshView() },
		OnSubmit:  func(string) { v.refreshView() },
		OnFocusChange: func(f bool) {
			v.focus.replace = f
			v.updateKeys()
		},
	})
	matches, _ := v.res.counts()
	all := ui.NewIconButton("replace-all", ui.IconButtonOpts{
		Tooltip: "Replace All (Ctrl+Alt+Enter)", Disabled: matches == 0, OnPressed: v.replaceAll,
	})
	return widgets.NewRow([]widgets.Widget{widgets.NewExpanded(box), gap(4, 0), all}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

func (v *view) detailsRow(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	dots := widgets.NewAlign(ui.NewIconButton("ellipsis", ui.IconButtonOpts{
		Tooltip: "Toggle Search Details (Ctrl+Shift+J)", Checked: v.detailsOpen, OnPressed: v.toggleDetails,
	}), widgets.AlignOpts{Alignment: painting.AlignmentCenterRight})
	rows := []widgets.Widget{dots}
	if v.detailsOpen {
		include := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
			Controller: v.includeBox, Placeholder: "e.g. *.ts, src/**/include",
			Trailing:  []widgets.Widget{v.flagToggle("book", "Search only in Open Editors", "onlyopen", v.onlyOpen, func(b bool) { v.onlyOpen = b; v.queryChanged(true); v.refreshView() })},
			OnChanged: func(string) { v.queryChanged(false) }, OnSubmit: func(string) { v.queryChanged(true) },
			OnFocusChange: func(f bool) { v.focus.include = f; v.updateKeys() },
		})
		exclude := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
			Controller: v.excludeBox, Placeholder: "e.g. *.ts, src/**/exclude",
			Trailing:  []widgets.Widget{v.flagToggle("exclude", "Use Exclude Settings and Ignore Files", "useexcludes", v.useExcludes, func(b bool) { v.useExcludes = b; v.queryChanged(true); v.refreshView() })},
			OnChanged: func(string) { v.queryChanged(false) }, OnSubmit: func(string) { v.queryChanged(true) },
			OnFocusChange: func(f bool) { v.focus.exclude = f; v.updateKeys() },
		})
		small := func(s string) widgets.Widget {
			return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 4, 0, 2), label(bc, s, labelSize, colors.DescriptionForeground))
		}
		rows = append(rows, small("files to include"), include, small("files to exclude"), exclude, gap(0, 4))
	}
	return widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
}

// ---- the messages ----

// link is text that acts when it is clicked.
func link(bc widgets.BuildContext, colors ui.Colors, text string, tip string, onTap func()) widgets.Widget {
	style := ui.TextStyle(bc, messageSize, colors.TextLinkForeground)
	text1 := widgets.NewText(text, widgets.TextOpts{Style: style})
	return widgets.NewMouseRegion(widgets.NewGestureDetector(text1, widgets.GestureDetectorOpts{OnTap: onTap, Behavior: rendering.HitTestBehaviorOpaque}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
}

// messages is the line above the results: how many results, the reason there are none, the warnings.
func (v *view) messages(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	st := v.status
	if st.replaced != "" {
		return v.messageBox(bc, colors, []widgets.Widget{plain(bc, colors, st.replaced)})
	}
	var parts []widgets.Widget
	matches, files := v.res.counts()
	switch {
	case st.noFolder:
		parts = append(parts, plain(bc, colors, "You have not opened or specified a folder."))
	case st.err != nil && !v.queryInvalid:
		parts = append(parts, widgets.NewText("Search failed: "+describe(st.err), widgets.TextOpts{Style: ui.TextStyle(bc, messageSize, colors.ErrorForeground)}))
	case st.running && matches == 0:
		parts = append(parts, plain(bc, colors, "Searching..."))
	case st.done && matches == 0 && st.cancelled:
		parts = append(parts, plain(bc, colors, "Search was canceled before any results could be found."))
	case st.done && matches == 0 && !v.queryInvalid:
		parts = append(parts, plain(bc, colors, noResultsText(st.opts)), link(bc, colors, "Open Settings", "Open the settings of the search", v.openSettings))
	case matches > 0:
		parts = append(parts, plain(bc, colors, resultCountText(matches, files)))
		if st.opts.DisregardExcludeSettings {
			parts = append(parts, plain(bc, colors, " - exclude settings and ignore files are disabled "),
				link(bc, colors, "enable", "Use Exclude Settings and Ignore Files", func() { v.useExcludes = true; v.queryChanged(true) }))
		}
		if st.opts.OnlyOpenEditors {
			parts = append(parts, plain(bc, colors, " - searching only in open files "),
				link(bc, colors, "disable", "Search in entire workspace", func() { v.onlyOpen = false; v.queryChanged(true) }))
		}
		if st.summary.LimitHit {
			parts = append(parts, widgets.NewText(" The result set only contains a subset of all matches. Be more specific in your search to narrow down the results.",
				widgets.TextOpts{Style: ui.TextStyle(bc, messageSize, colors.DescriptionForeground)}))
		}
	}
	if len(parts) == 0 {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(4.0)})
	}
	return v.messageBox(bc, colors, parts)
}

func plain(bc widgets.BuildContext, colors ui.Colors, s string) widgets.Widget {
	return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, messageSize, colors.SideBarForeground)})
}

func (v *view) messageBox(_ widgets.BuildContext, _ ui.Colors, parts []widgets.Widget) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(boxPadding+gutterWidth-2, 6, boxPadding, 6), widgets.NewWrap(parts, widgets.WrapOpts{CrossAxisAlignment: rendering.WrapCrossAlignmentCenter}))
}

// resultCountText is VS Code's "N results in M files".
func resultCountText(matches, files int) string {
	n := strconv.Itoa(matches) + " result"
	if matches != 1 {
		n += "s"
	}
	n += " in " + strconv.Itoa(files) + " file"
	if files != 1 {
		n += "s"
	}
	return n
}

// noResultsText is the message of a search that found nothing, with the include and exclude boxes in it as VS Code's does
// (searchView.ts, "No results found ...").
func noResultsText(o extension.TextSearchOptions) string {
	inc, exc := o.Include, o.Exclude
	if o.OnlyOpenEditors {
		switch {
		case inc != "" && exc != "":
			return "No results found in open editors matching '" + inc + "' excluding '" + exc + "' - "
		case inc != "":
			return "No results found in open editors matching '" + inc + "' - "
		case exc != "":
			return "No results found in open editors excluding '" + exc + "' - "
		}
		return "No results found in open editors. Review your configured exclusions and check your gitignore files - "
	}
	switch {
	case inc != "" && exc != "":
		return "No results found in '" + inc + "' excluding '" + exc + "' - "
	case inc != "":
		return "No results found in '" + inc + "' - "
	case exc != "":
		return "No results found excluding '" + exc + "' - "
	}
	return "No results found. Review your configured exclusions and check your gitignore files - "
}

// openSettings shows the settings of the search: a quick pick with the one row that opens them.
func (v *view) openSettings() {
	err := v.api.QuickPick().Show(extension.QuickPickOptions{
		Placeholder: "Search settings",
		Items:       []extension.QuickPickItem{{Label: "$(gear) Open Settings", Command: "workbench.action.openSettings", Args: "search."}},
	}, func(int) {})
	if err != nil {
		v.api.Log().Warn("the settings cannot be offered", "err", err)
	}
}

// ---- the results ----

func (v *view) results(bc widgets.BuildContext) widgets.Widget {
	if v.res.empty() {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{})
	}
	tree := ui.NewTree(v.tree.model, ui.TreeOpts{
		Controller: v.tree.ctl, Render: v.render, OnSelect: v.onSelect, OnOpen: v.onOpen,
	})
	return widgets.NewFocus(tree, widgets.FocusOpts{
		CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "SearchResults",
		OnFocusChange: func(has bool) {
			v.focus.list = has
			v.updateKeys()
		},
	})
}

// onSelect is a click on a row, or Space: a match opens in a preview tab, or, while the replace box has text, the diff of the
// file with the replacement.
func (v *view) onSelect(n ui.TreeNode) {
	d, ok := n.Data.(matchNode)
	v.updateKeys()
	if !ok {
		return
	}
	v.tree.curKey = matchKey(d.match)
	if v.replaceOpen && v.replaceBox.Value() != "" {
		v.previewReplace(d.file)
		return
	}
	v.openMatch(d, true)
}

// onOpen is Enter, or a double click: the match opens in a tab of its own.
func (v *view) onOpen(n ui.TreeNode) {
	if d, ok := n.Data.(matchNode); ok {
		v.openMatch(d, false)
	}
}

// actions are the buttons that show while the pointer is over a row.
func (v *view) actions(n ui.TreeNode) widgets.Widget {
	var buttons []widgets.Widget
	if v.replaceOpen {
		tip := "Replace"
		if _, ok := n.Data.(matchNode); !ok {
			tip = "Replace All"
		}
		buttons = append(buttons, ui.NewIconButton("replace", ui.IconButtonOpts{
			Key: foundation.NewKey("replace"), Tooltip: tip + " (Ctrl+Shift+1)", OnPressed: func() { v.replaceNode(n) },
		}))
	}
	buttons = append(buttons, ui.NewIconButton("close", ui.IconButtonOpts{
		Key: foundation.NewKey("dismiss"), Tooltip: "Dismiss (Delete)", OnPressed: func() { v.dismiss(n) },
	}))
	return widgets.NewRow(buttons, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin})
}

// render builds the contents of a row of the results.
func (v *view) render(bc widgets.BuildContext, n ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	hot := st.Hovered || st.Focused
	var content widgets.Widget
	var badge widgets.Widget
	switch d := n.Data.(type) {
	case folderNode:
		content = widgets.NewRow([]widgets.Widget{label(bc, d.label, fontSize, st.Foreground)}, widgets.RowOpts{})
		badge = v.badge(d.count)
	case fileNode:
		row := []widgets.Widget{label(bc, d.file.Name(), fontSize, st.Foreground)}
		if dir := d.file.Dir(); dir != "" && !d.inTree {
			row = append(row, gap(6, 0), widgets.NewFlexible(label(bc, dir, fontSize-1, colors.DescriptionForeground)))
		}
		content = widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		badge = v.badge(len(d.file.matches))
	case matchNode:
		return widgets.NewRow([]widgets.Widget{widgets.NewFlexible(v.matchText(bc, colors, d, st)), v.hotActions(hot, n)}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	default:
		return label(bc, "", fontSize, st.Foreground)
	}
	return widgets.NewRow([]widgets.Widget{widgets.NewFlexible(content), gap(6, 0), badge, v.hotActions(hot, n)}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

func (v *view) badge(count int) widgets.Widget {
	if !v.bool(settingDecorationBadges) {
		return gap(0, 0)
	}
	return ui.NewBadge(count)
}

func (v *view) hotActions(hot bool, n ui.TreeNode) widgets.Widget {
	if !hot {
		return gap(8, 0)
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(4, 0, 4, 0), v.actions(n))
}

// matchText is the line of a match with the match marked; while the replace box is open, the text that would go is marked in red
// and what would come in green.
func (v *view) matchText(bc widgets.BuildContext, colors ui.Colors, d matchNode, st ui.RowState) widgets.Widget {
	m := d.match
	preview := m.Preview
	ps, pe := min(max(m.PreviewStart, 0), len(preview)), min(max(m.PreviewEnd, 0), len(preview))
	if pe < ps {
		pe = ps
	}
	before, mid, after := preview[:ps], preview[ps:pe], preview[pe:]
	before = strings.TrimLeft(before, " \t")
	if utf8.RuneCountInString(before) > beforeContext {
		r := []rune(before)
		before = "…" + string(r[len(r)-beforeContext:])
	}
	base := ui.TextStyle(bc, fontSize, st.Foreground)
	hit := base.CopyWith(painting.TextStyleOpts{BackgroundColor: foundation.Some(colors.EditorFindMatchHighlightBackground)})
	var spans []painting.InlineSpan
	span := func(text string, style *painting.TextStyle) {
		if text != "" {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text), Style: style}))
		}
	}
	if v.bool(settingShowLineNumbers) {
		span(strconv.Itoa(m.Range.Start.Line+1)+": ", base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.DescriptionForeground)}))
	}
	span(before, base)
	if v.replaceOpen {
		span(mid, base.CopyWith(painting.TextStyleOpts{BackgroundColor: foundation.Some(removedBackground)}))
		span(v.replacement(m.Text), base.CopyWith(painting.TextStyleOpts{BackgroundColor: foundation.Some(insertedBackground)}))
	} else {
		span(mid, hit)
	}
	span(after, base)
	root := painting.NewTextSpan(painting.TextSpanOpts{Style: base, Children: spans})
	return widgets.NewRichText(root, widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}
