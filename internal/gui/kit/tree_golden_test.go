package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// projectItems is a small project as the explorer would show it; "vendor" has not been read.
func projectItems() []TreeItem {
	file := func(name string) TreeItem { return TreeItem{Key: name, Data: name} }
	dir := func(name string, children ...TreeItem) TreeItem {
		return TreeItem{Key: name, Data: name[lastSlash(name)+1:], Children: children, HasChildren: true}
	}
	return []TreeItem{
		dir("cmd", dir("cmd/emetos", TreeItem{Key: "cmd/emetos/main.go", Data: "main.go"})),
		dir("internal",
			dir("internal/core", TreeItem{Key: "internal/core/buffer", Data: "buffer", HasChildren: true},
				dir("internal/core/document", TreeItem{Key: "internal/core/document/document.go", Data: "document.go"})),
			dir("internal/gui")),
		dir("docs", TreeItem{Key: "docs/SPEC.md", Data: "SPEC.md"}),
		{Key: "vendor", Data: "vendor", Lazy: true},
		{Key: "go.mod", Data: "go.mod"},
		file("README.md"),
	}
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

// fileRender shows a folder or file icon and the label, as the explorer does.
func fileRender(bc widgets.BuildContext, n TreeNode, st TreeRowState) widgets.Widget {
	icon := fonts.CodiconFileText
	switch {
	case n.Expandable && n.Expanded:
		icon = fonts.CodiconFolderOpened
	case n.Expandable:
		icon = fonts.CodiconFolder
	}
	return widgets.NewRow([]widgets.Widget{
		widgets.NewIcon(fonts.CodiconIcon(icon), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(st.Foreground)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
		widgets.NewFlexible(defaultTreeRender(bc, n, st)),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// projectRig shows the project items in a tree of a size, in the colors.
func projectRig(t *testing.T, colors Colors, size ui.Size, expanded ...string) *treeRig {
	t.Helper()
	return newTreeRig(t, treeRigOpts{
		colors: colors, size: size, items: projectItems(), expanded: expanded,
		tree: TreeOpts{Render: fileRender, IndentGuides: TreeGuidesAlways},
	})
}

var projectExpanded = []string{"cmd", "cmd/emetos", "internal", "internal/core", "internal/core/document", "docs"}

func TestGoldenTree(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := projectRig(t, th.colors(), ui.Size{Width: 260, Height: 330}, projectExpanded...)
			// document.go is selected, and the tree has the focus, on that row
			r.m.SetSelected("internal/core/document/document.go")
			r.m.SetFocus("internal/core/document/document.go")
			r.ctrl.Focus()
			r.settle()
			r.ts.Hover(ui.Offset{Dx: 150, Dy: r.y(9)}) // docs
			r.settle()
			r.noErrors()
			testutil.MatchesGolden(r.ts, "tree_"+th.name)
		})
	}
}

func TestGoldenTreeWithoutFocus(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := projectRig(t, th.colors(), ui.Size{Width: 260, Height: 330}, projectExpanded...)
			r.m.SetSelected("internal/core/document/document.go")
			r.m.SetFocus("internal/core/buffer")
			r.ctrl.Focus()
			r.settle()
			r.other.RequestFocus()
			r.settle()
			r.noErrors()
			testutil.MatchesGolden(r.ts, "tree_unfocused_"+th.name)
		})
	}
}

func TestGoldenTreeLoading(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newTreeRig(t, treeRigOpts{
				colors: th.colors(), size: ui.Size{Width: 260, Height: 154}, items: projectItems(), expanded: []string{"internal", "vendor"}, noLoader: true,
				tree: TreeOpts{Render: fileRender, IndentGuides: TreeGuidesAlways},
			})
			r.m.SetFocus("vendor")
			r.m.SetSelected("vendor")
			r.ctrl.Focus()
			r.settle()
			r.ts.Pump(100 * time.Millisecond)
			r.noErrors()
			testutil.MatchesGolden(r.ts, "tree_loading_"+th.name)
		})
	}
}

// TestProofTree makes the proof image of step 6.4 (go test ./internal/gui/kit -run TestProofTree -proof): an
// expanded tree with a selected row and indent guides.
func TestProofTree(t *testing.T) {
	if !testutil.ProofEnabled() {
		t.Skip("run with -proof")
	}
	r := projectRig(t, DarkModern(), ui.Size{Width: 300, Height: 330}, projectExpanded...)
	r.m.SetSelected("internal/core/document/document.go")
	r.m.SetFocus("internal/core/document/document.go")
	r.ctrl.Focus()
	r.settle()
	r.noErrors()
	testutil.Proof(t, r.ts, "6.4-tree.png")
}
