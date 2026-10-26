package golang

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The panel view "Go Tests" (SPEC §10.17 "As built (step 14.3)"): a tree of packages, tests and subtests with their status and time,
// and the output of the node that is selected.

// pkgItem and testItem are the data of the nodes of the tree.
type pkgItem struct{ p *pkgNode }

type testItem struct {
	p *pkgNode
	n *testNode
}

// testsPanel is the panel view. It belongs to the UI thread.
type testsPanel struct {
	g      *golang
	handle extension.ViewHandle
	job    *testJob
	model  *ui.TreeModel
	ctl    *ui.TreeController
	// selected is the key of the node whose output is shown; empty shows the output of the run.
	selected string
	scroll   ui.ScrollHandle
}

func newTestsPanel(g *golang) *testsPanel {
	return &testsPanel{g: g, model: ui.NewTreeModel(), ctl: ui.NewTreeController()}
}

func pkgKey(p *pkgNode) string { return "p:" + p.path }

func testKey(p *pkgNode, n *testNode) string { return "t:" + p.path + ":" + n.name }

// items makes the nodes of the tree from the results.
func (t *testsPanel) items(j *testJob) []ui.TreeItem {
	var build func(p *pkgNode, nodes []*testNode) []ui.TreeItem
	build = func(p *pkgNode, nodes []*testNode) []ui.TreeItem {
		out := make([]ui.TreeItem, 0, len(nodes))
		for _, n := range nodes {
			out = append(out, ui.TreeItem{Key: testKey(p, n), Label: n.short, Data: testItem{p, n}, Children: build(p, n.children)})
		}
		return out
	}
	var roots []ui.TreeItem
	for _, p := range j.results.packages {
		roots = append(roots, ui.TreeItem{Key: pkgKey(p), Label: p.path, Data: pkgItem{p}, Children: build(p, p.tests)})
	}
	return roots
}

// refresh makes the tree from the results of the job and asks the view to build again.
func (t *testsPanel) refresh() {
	j := t.job
	if j != nil {
		expanded := t.model.ExpandedKeys()
		if err := t.model.SetItems(t.items(j)); err != nil {
			t.g.api.Log().Warn("the tree of the tests cannot be made", "err", err)
		} else {
			// a package is open the first time it is seen, and so is a test with a failure below it
			for _, p := range j.results.packages {
				if !j.seen[pkgKey(p)] {
					j.seen[pkgKey(p)] = true
					expanded = append(expanded, pkgKey(p))
				}
				for _, n := range p.byName {
					if len(n.children) > 0 && n.failing() && !j.seen[testKey(p, n)] {
						j.seen[testKey(p, n)] = true
						expanded = append(expanded, testKey(p, n))
					}
				}
			}
			t.model.SetExpandedKeys(expanded)
			if t.selected != "" && t.model.Has(t.selected) {
				t.model.SetSelected(t.selected)
			}
		}
	}
	t.handle.Refresh()
}

// selectFirstFailure selects the first failed test (else the first failed package) when the user has selected nothing.
func (t *testsPanel) selectFirstFailure() {
	j := t.job
	if j == nil || t.selected != "" {
		return
	}
	for _, p := range j.results.packages {
		var first func(nodes []*testNode) *testNode
		first = func(nodes []*testNode) *testNode {
			for _, n := range nodes {
				if c := first(n.children); c != nil {
					return c
				}
				if n.status == statusFail {
					return n
				}
			}
			return nil
		}
		if n := first(p.tests); n != nil {
			t.selected = testKey(p, n)
			return
		}
	}
	for _, p := range j.results.packages {
		if p.status == statusFail {
			t.selected = pkgKey(p)
			return
		}
	}
}

// onSelect shows the output of the node.
func (t *testsPanel) onSelect(node ui.TreeNode) {
	t.selected = node.Key
	t.scroll.ScrollTo(0, 0)
	t.handle.Refresh()
}

// outputOf is the output shown for the selection: the lines of the test or the package, or the run's own.
func (t *testsPanel) outputOf() []string {
	j := t.job
	if j == nil {
		return nil
	}
	if d, ok := t.model.Data(t.selected); ok {
		switch d := d.(type) {
		case testItem:
			return d.n.output
		case pkgItem:
			return d.p.output
		}
	}
	return append([]string{"$ " + j.command}, j.results.stray...)
}

// expandTabs joins the lines for the panel with each tab replaced by spaces up to the next multiple of 8 columns, which is how a
// terminal shows the output of go test ("FAIL<tab>path<tab>0.1s"); the monospace font of the panel has no glyph for a tab.
func expandTabs(lines []string) string {
	var sb strings.Builder
	for i, l := range lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		col := 0
		for _, r := range l {
			if r == '\t' {
				n := 8 - col%8
				sb.WriteString(strings.Repeat(" ", n))
				col += n
				continue
			}
			sb.WriteRune(r)
			col++
		}
	}
	return sb.String()
}

// formatElapsed writes a time in seconds: "12 ms" below a second, "1.25 s" above.
func formatElapsed(sec float64) string {
	if sec < 1 {
		return strconv.Itoa(int(sec*1000+0.5)) + " ms"
	}
	return fmt.Sprintf("%.2f s", sec)
}

// summary is the line above the tree.
func (j *testJob) summary() string {
	c := j.results.counts()
	var parts []string
	if c.Pass > 0 {
		parts = append(parts, strconv.Itoa(c.Pass)+" passed")
	}
	if c.Fail > 0 {
		parts = append(parts, strconv.Itoa(c.Fail)+" failed")
	}
	if c.Skip > 0 {
		parts = append(parts, strconv.Itoa(c.Skip)+" skipped")
	}
	if c.BuildFailed > 0 {
		parts = append(parts, "build failed")
	} else if c.FailedPackages > 0 && c.Fail == 0 {
		parts = append(parts, strconv.Itoa(c.FailedPackages)+" package failed")
	}
	text := strings.Join(parts, ", ")
	if text == "" {
		text = "no tests"
	}
	switch j.state {
	case runRunning:
		return "Running " + j.req.what + " in " + j.where() + ": " + text
	case runStopped:
		return "Stopped " + j.req.what + " in " + j.where() + ": " + text
	case runNotStarted:
		return "Could not run " + j.req.what + ": " + j.status
	}
	return j.req.what + " in " + j.where() + ": " + text
}

// where names the folder of the run for the panel.
func (j *testJob) where() string {
	if j.req.label == "." {
		return "the workspace folder"
	}
	return j.req.label
}

// statusIcon is the icon and the color of a status; running tests spin.
func statusIcon(colors ui.Colors, status int, running bool) (string, gui.Color) {
	dark := colors.EditorBackground.ComputeLuminance() < 0.5
	pass, fail := gui.NewColorFromARGB(0xFF, 0x73, 0xC9, 0x91), colors.ErrorForeground
	if !dark {
		pass = gui.NewColorFromARGB(0xFF, 0x00, 0x71, 0x00)
	}
	switch status {
	case statusPass:
		return "pass", pass
	case statusFail:
		return "error", fail
	case statusSkip:
		return "circle-slash", colors.DescriptionForeground
	}
	if running {
		return "sync", colors.DescriptionForeground
	}
	return "circle-outline", colors.DescriptionForeground
}

// ---- the widgets ----

// build is the panel view.
func (t *testsPanel) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	plain := ui.TextStyle(bc, fontSize, colors.Foreground)
	j := t.job
	if j == nil {
		return widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewColumn([]widgets.Widget{
			widgets.NewText("Go: Test Function At Cursor, Test File and Test Package run go test and show the packages, tests and subtests here, with their output. The lenses above a test run it too.", widgets.TextOpts{Style: plain}),
			gap(8),
			widgets.NewAlign(ui.NewButton("Test Package", ui.ButtonOpts{OnPressed: func() { _ = t.g.testPackageCommand(t.g.ctx, nil) }}), widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}),
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	}
	c := j.results.counts()
	icon, color := "sync", colors.DescriptionForeground
	switch {
	case j.state == runRunning:
	case c.Fail > 0 || c.FailedPackages > 0 || j.state == runNotStarted:
		icon, color = "error", colors.ErrorForeground
	case j.state == runStopped:
		icon = "circle-slash"
	default:
		icon, color = statusIcon(colors, statusPass, false)
	}
	var action widgets.Widget
	if j.state == runRunning {
		action = ui.NewButton("Stop", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: t.g.stopTests})
	} else {
		action = ui.NewButton("Run Again", ui.ButtonOpts{Small: true, OnPressed: func() { t.g.startTests(j.req) }})
	}
	header := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 8, 12, 4), widgets.NewRow([]widgets.Widget{
		ui.Icon(icon, 14, color), hgap(8),
		widgets.NewExpanded(widgets.NewText(j.summary(), widgets.TextOpts{Style: plain, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
		hgap(8), action,
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	mono := ui.TextStyle(bc, monoSize, colors.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: monoFamily(bc)})
	output := ui.NewScrollView(widgets.NewPadding(painting.NewEdgeInsetsAll(10), widgets.NewText(expandTabs(t.outputOf()), widgets.TextOpts{Style: mono})), &t.scroll)
	var tree widgets.Widget
	if t.model.Len() > 0 {
		tree = ui.NewTree(t.model, ui.TreeOpts{Controller: t.ctl, Render: t.render, OnSelect: t.onSelect, ToggleOnClick: foundation.Some(false)})
	} else {
		tree = widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewText("Waiting for the first result...", widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, colors.DescriptionForeground)}))
	}
	return widgets.NewColumn([]widgets.Widget{
		header,
		widgets.NewExpanded(widgets.NewRow([]widgets.Widget{
			widgets.NewExpanded(tree, widgets.ExpandedOpts{Flex: foundation.Some(5)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(1.0)}),
			widgets.NewExpanded(output, widgets.ExpandedOpts{Flex: foundation.Some(6)}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// render builds the contents of a row of the tree.
func (t *testsPanel) render(bc widgets.BuildContext, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	running := t.job != nil && t.job.state == runRunning
	text := func(s string, color gui.Color) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	var status int
	var name string
	var elapsed float64
	var has bool
	switch d := node.Data.(type) {
	case pkgItem:
		status, name, elapsed, has = d.p.status, d.p.path, d.p.elapsed, d.p.hasElapsed
		if d.p.buildFailed {
			name += " (build failed)"
		}
	case testItem:
		status, name, elapsed, has = d.n.status, d.n.short, d.n.elapsed, d.n.hasElapsed
	default:
		return text("", st.Foreground)
	}
	icon, color := statusIcon(colors, status, running)
	row := []widgets.Widget{ui.Icon(icon, 14, color), hgap(6), widgets.NewFlexible(text(name, st.Foreground))}
	if has {
		row = append(row, hgap(8), text(formatElapsed(elapsed), colors.DescriptionForeground))
	}
	row = append(row, hgap(8))
	return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}
