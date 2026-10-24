package explorer

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The texts of the welcome content of the Explorer.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/explorerViewlet.ts (registerViewWelcomeContent
// of the empty view)
const (
	// WelcomeText says that no folder is open.
	WelcomeText = "You have not yet opened a folder."
	// WelcomeButton is the label of the button that opens a folder.
	WelcomeButton = "Open Folder"
	// WelcomeEditorsText is added when editors are open: opening a folder closes them.
	WelcomeEditorsText = "Opening a folder will close all currently open editors."
)

// WelcomeOpts are the options of NewWelcome.
type WelcomeOpts struct {
	// HasEditors says that editors are open, which opening a folder closes.
	HasEditors bool
	// OnOpenFolder is called when the user presses the button; without it the button is disabled.
	OnOpenFolder func()
}

// NewWelcome makes the welcome content of the Explorer in a window that has no folder: the text "You have
// not yet opened a folder." over a button that opens one, and, when editors are open, the note that
// opening a folder closes them.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/explorerViewlet.ts and
// src/vs/workbench/browser/parts/views/viewPane.ts (ViewWelcomeController)
func NewWelcome(opts WelcomeOpts) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		colors := kit.ThemeOf(bc)
		text := func(s string) widgets.Widget {
			return widgets.NewText(s, widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.Foreground).Merge(painting.NewTextStyle(painting.TextStyleOpts{Height: foundation.Some(1.5)}))})
		}
		children := []widgets.Widget{
			text(WelcomeText),
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(8, 0),
				kit.NewButton(WelcomeButton, kit.ButtonOpts{Key: OpenFolderKey, Expand: true, OnPressed: opts.OnOpenFolder})),
		}
		if opts.HasEditors {
			children = append(children, text(WelcomeEditorsText))
		}
		return widgets.NewKeyedSubtree(
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 4, 20, 0),
				widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})),
			widgets.WidgetOpts{Key: WelcomeKey})
	})
}
