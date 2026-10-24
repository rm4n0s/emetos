package explorer

import (
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Keys of the widgets of the Explorer, which tests find them by.
var (
	// TreeKey is the key of the tree of the folder.
	TreeKey = foundation.NewValueKey("explorer:tree")
	// ErrorKey is the key of the message that stands in place of the tree when the folder cannot be read.
	ErrorKey = foundation.NewValueKey("explorer:error")
	// ActionsKey is the key of the buttons in the header of the folder view.
	ActionsKey = foundation.NewValueKey("explorer:actions")
	// WelcomeKey is the key of the welcome content of a window with no folder.
	WelcomeKey = foundation.NewValueKey("explorer:welcome")
	// OpenFolderKey is the key of the "Open Folder" button of the welcome content.
	OpenFolderKey = foundation.NewValueKey("explorer:open-folder")
	// OpenEditorsKey is the key of the list of the Open Editors.
	OpenEditorsKey = foundation.NewValueKey("explorer:open-editors")
)

// The commands of the buttons in the header of the folder view.
const (
	// CommandRefresh refreshes the tree.
	CommandRefresh = "workbench.files.action.refreshFilesExplorer"
	// CommandCollapse folds every folder.
	CommandCollapse = "workbench.files.action.collapseExplorerFolders"
)

// BodyOpts are the options of NewBody.
type BodyOpts struct {
	// AfterFrame runs a function after the next frame; the tree is scrolled to a revealed file then. Nil
	// scrolls at once.
	AfterFrame func(func())
}

// Body is the body of the folder view: the tree of the folder, or the message that says why it cannot be
// shown.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerView.ts and
// explorerViewer.ts (FilesRenderer)
type Body struct {
	widgets.StatefulWidget
	view *View
	opts BodyOpts
}

// NewBody makes the body of the folder view for a view.
func NewBody(view *View, opts ...BodyOpts) *Body {
	b := &Body{StatefulWidget: widgets.NewStatefulWidgetBase(), view: view}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	return b
}

// CreateState implements widgets.StatefulWidget.
func (b *Body) CreateState() widgets.State { return &bodyState{} }

type bodyState struct {
	widgets.StateOf[*Body]
	stop func()
}

// InitState follows the view.
func (s *bodyState) InitState() { s.follow() }

func (s *bodyState) follow() {
	s.stop = s.Widget().view.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// DidUpdateWidget follows another view.
func (s *bodyState) DidUpdateWidget(old *Body) {
	if old.view != s.Widget().view {
		s.stop()
		s.follow()
	}
}

// Dispose stops following the view.
func (s *bodyState) Dispose() { s.stop() }

// Build implements widgets.State.
func (s *bodyState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	v := w.view
	colors := kit.ThemeOf(bc)
	if err := v.rootErr; err != nil {
		return message(bc, ErrorKey, describe(err), colors.ProblemsErrorIconForeground)
	}
	if !v.loaded {
		return widgets.NewSizedBox(nil)
	}
	tree := kit.NewTree(v.tree, kit.TreeOpts{
		Key: TreeKey, Controller: v.ctl,
		Render: func(bc widgets.BuildContext, n kit.TreeNode, st kit.TreeRowState) widgets.Widget {
			return v.render(bc, n, st)
		},
		OnSelect: func(n kit.TreeNode) { v.open(n, false) },
		OnOpen:   func(n kit.TreeNode) { v.open(n, true) },
		// Escape reduces the selection; a letter starts the find box, as VS Code's explorer does
	})
	if v.scrollTo != "" {
		after := w.opts.AfterFrame
		if after == nil {
			after = func(f func()) { f() }
		}
		after(func() {
			if s.Mounted() {
				v.flushScroll()
			}
		})
	}
	return tree
}

// describe is the text of an error for the message of a folder that cannot be read.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// message is a line of text in the body of a view.
func message(bc widgets.BuildContext, key foundation.Key, text string, color ui.Color) widgets.Widget {
	return widgets.NewKeyedSubtree(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 8, 20, 8),
			widgets.NewText(text, widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, color)})),
		widgets.WidgetOpts{Key: key})
}

// render builds the contents of a row: the icon of the file or folder, then its name.
func (v *View) render(bc widgets.BuildContext, n kit.TreeNode, st kit.TreeRowState) widgets.Widget {
	colors := kit.ThemeOf(bc)
	nd, ok := n.Data.(*nodeData)
	if !ok {
		return kit.NewHighlightedLabel(bc, n.Label, st)
	}
	var icon widgets.Widget
	if ic, ok := v.iconFor(nd, n.Expanded, colors.Type); ok {
		icon = FileIconWidget(ic, st.Foreground)
	}
	label := widgets.NewExpanded(kit.NewHighlightedLabel(bc, nd.label(), st))
	return iconRow(icon, label)
}

// ActionsOpts are the options of NewActions.
type ActionsOpts struct {
	// OnRun is called with the id of the command of the button the user pressed.
	OnRun func(command string)
}

// NewActions makes the buttons of the header of the folder view: Refresh Explorer and Collapse Folders
// in Explorer. (New File and New Folder come with the file operations of step 6.15.)
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerView.ts
func NewActions(opts ActionsOpts) widgets.Widget {
	actions := []kit.Action{
		{ID: CommandRefresh, Label: "Refresh Explorer", Icon: fonts.CodiconRefresh},
		{ID: CommandCollapse, Label: "Collapse Folders in Explorer", Icon: fonts.CodiconCollapseAll},
	}
	return widgets.NewKeyedSubtree(
		kit.NewActionBar(actions, kit.ActionBarOpts{OnRun: func(a kit.Action) {
			if opts.OnRun != nil {
				opts.OnRun(a.ID)
			}
		}}),
		widgets.WidgetOpts{Key: ActionsKey})
}

// afterFrameOf is the AfterFrame function that runs a function once the next frame is done.
func afterFrameOf(binding *widgets.WidgetsBinding) func(func()) {
	return func(f func()) {
		scheduler := binding.Scheduler()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) { f() })
	}
}

// AfterFrame returns the AfterFrame function of the body for a binding.
func AfterFrame(binding *widgets.WidgetsBinding) func(func()) { return afterFrameOf(binding) }
