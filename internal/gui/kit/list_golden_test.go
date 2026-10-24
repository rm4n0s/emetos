package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// goldenListItems are the files of a folder, as a list of results would show them.
func goldenListItems() []ListViewItem {
	names := []string{"main.go", "README.md", "go.mod", "list.go", "tree.go", "notes.txt", "listview.go", "theme.go"}
	out := make([]ListViewItem, len(names))
	for i, n := range names {
		out[i] = ListViewItem{Key: n, Label: n}
	}
	return out
}

// fileListRender shows a file icon and the label with the matches of the find.
func fileListRender(bc widgets.BuildContext, row ListViewRow, st RowState) widgets.Widget {
	return widgets.NewRow([]widgets.Widget{
		widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconFileText), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(st.Foreground)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
		widgets.NewFlexible(defaultListRender(bc, row, st)),
	})
}

func TestGoldenListMultiSelect(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newListViewRig(t, listViewRigOpts{
				colors: th.colors(), size: ui.Size{Width: 260, Height: 190}, items: goldenListItems(),
				list: ListOpts{MultiSelect: true, Render: fileListRender},
			})
			r.tapRow(1)
			r.tapWith(3, keyShift)
			r.tapWith(6, keyControl)
			r.ctrl.Focus()
			r.settle()
			r.ts.Hover(ui.Offset{Dx: 150, Dy: r.y(5)})
			r.settle()
			r.noErrors()
			testutil.MatchesGolden(r.ts, "list_multiselect_"+th.name)
		})
	}
}

func TestGoldenListFind(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newListViewRig(t, listViewRigOpts{
				colors: th.colors(), size: ui.Size{Width: 260, Height: 190}, items: goldenListItems(),
				list: ListOpts{Render: fileListRender},
			})
			r.focusList()
			r.typeText("li")
			r.noErrors()
			testutil.MatchesGolden(r.ts, "list_find_"+th.name)
		})
	}
}

func TestGoldenTreeFind(t *testing.T) {
	for _, th := range goldenThemes {
		for _, mode := range []struct {
			name string
			mode FindMode
		}{{"highlight", FindModeHighlight}, {"filter", FindModeFilter}} {
			t.Run(th.name+" "+mode.name, func(t *testing.T) {
				r := newTreeRig(t, treeRigOpts{
					colors: th.colors(), size: ui.Size{Width: 260, Height: 250}, items: projectItems(), expanded: projectExpanded,
					model: TreeModelOpts{FindMode: mode.mode},
					tree:  TreeOpts{Render: fileRender, IndentGuides: TreeGuidesAlways},
				})
				r.m.SetSelected("docs/SPEC.md")
				r.ctrl.Focus()
				r.settle()
				r.typeText("doc")
				r.noErrors()
				testutil.MatchesGolden(r.ts, "tree_find_"+mode.name+"_"+th.name)
			})
		}
	}
}

func TestGoldenTreeFindNoResults(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newTreeRig(t, treeRigOpts{
				colors: th.colors(), size: ui.Size{Width: 260, Height: 120}, items: projectItems(), expanded: projectExpanded,
				model: TreeModelOpts{FindMode: FindModeFilter},
				tree:  TreeOpts{Render: fileRender},
			})
			r.ctrl.Focus()
			r.settle()
			r.typeText("zzz")
			r.noErrors()
			testutil.MatchesGolden(r.ts, "tree_find_noresults_"+th.name)
		})
	}
}

func TestGoldenTreeDropFeedback(t *testing.T) {
	for _, th := range goldenThemes {
		for _, c := range []struct {
			name string
			row  int
			dy   float64
		}{{"inside", 9, 0}, {"between", 4, 8}} {
			t.Run(th.name+" "+c.name, func(t *testing.T) {
				r := newTreeRig(t, treeRigOpts{
					colors: th.colors(), size: ui.Size{Width: 260, Height: 330}, items: projectItems(), expanded: projectExpanded,
					tree: TreeOpts{Render: fileRender, IndentGuides: TreeGuidesAlways, MultiSelect: true, DragAndDrop: &DragAndDrop{
						CanDrop: func(string, DropPosition, []string) DropEffect { return DropEffectMove },
					}},
				})
				// the file go.mod is dragged to docs, or to between two rows of the folder cmd
				r.m.SetSelected("README.md")
				g := r.ts.StartGesture(ui.Offset{Dx: 150, Dy: r.y(13)}, mouseDown)
				g.MoveTo(ui.Offset{Dx: 150, Dy: r.y(13) + dragSlop + 2})
				g.MoveTo(ui.Offset{Dx: 150, Dy: r.y(c.row) + c.dy})
				r.settle()
				r.noErrors()
				testutil.MatchesGolden(r.ts, "tree_drop_"+c.name+"_"+th.name)
				g.Up()
				r.settle()
			})
		}
	}
}
