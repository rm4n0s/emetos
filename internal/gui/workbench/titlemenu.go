package workbench

// Where the menu is and what it shows (`window.menuBarVisibility`, SPEC §10.1, §10.14). This file has no widgets: the
// rules are plain functions of the setting and of the state of the window, tested on their own.

// The values of window.menuBarVisibility.
const (
	menuClassic = "classic"
	menuVisible = "visible"
	menuToggle  = "toggle"
	menuHidden  = "hidden"
	menuCompact = "compact"
)

// effectiveMenuVisibility is the visibility the menu has now, from the setting: zen mode hides it, and full screen
// mode hides the classic and the compact one until the Alt key shows it (the visibility toggle, whose titles then show
// in a row of their own, since a window in full screen has no title bar). A value that is not one of the five is the
// default, compact.
//
// Ported from VS Code: src/vs/workbench/services/layout/browser/layoutService.ts (shouldShowCustomTitleBar) and
// src/vs/workbench/browser/parts/titlebar/menubarControl.ts
func effectiveMenuVisibility(setting string, zen, fullScreen bool) string {
	switch setting {
	case menuClassic, menuVisible, menuToggle, menuHidden, menuCompact:
	default:
		setting = menuCompact
	}
	switch {
	case zen:
		return menuHidden
	case fullScreen && (setting == menuClassic || setting == menuCompact):
		return menuToggle
	}
	return setting
}

// menuInTitleBar says whether the menu is in the title bar row: it is while the window has the title bar that Emetos
// draws (appBar: DecorationsApp, which the window asks for) and is not in full screen mode, where that title bar is
// not drawn. Otherwise the menu is in a row of its own over the workbench.
func menuInTitleBar(appBar, fullScreen bool) bool { return appBar && !fullScreen }

// menuRowShown says whether the row of its own that holds the menu when it is not in the title bar takes room: with the
// visibility classic, visible and compact always, with toggle after the Alt key (shown), never with hidden. The row
// has no height while the menu is in the title bar.
func menuRowShown(visibility string, inTitleBar, shown bool) bool {
	if inTitleBar {
		return false
	}
	switch visibility {
	case menuClassic, menuVisible, menuCompact:
		return true
	case menuToggle:
		return shown
	}
	return false
}

// titleBarRowShown says whether the title bar row has its menu, tabs and buttons: it has them unless zen mode is on, and
// then only the window buttons stay (the window has no other way to close or move; zen mode puts the window in full
// screen, where the whole title bar goes, unless zenMode.fullScreen is off).
func titleBarRowShown(zen bool) bool { return !zen }
