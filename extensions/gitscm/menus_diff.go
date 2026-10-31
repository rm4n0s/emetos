package gitscm

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// item is a menu item with the group written as VS Code's package.json writes it: "inline@2" is the group inline and the order 2.
func item(menu, command, group, when string) extension.MenuItem {
	g, order, hasOrder := strings.Cut(group, "@")
	it := extension.MenuItem{Menu: menu, Command: command, Group: g, When: when}
	if hasOrder {
		it.Order, _ = strconv.ParseFloat(order, 64)
	}
	return it
}

// diffMenus are the menu items of the commands that open changes: the context menus and the inline actions of the rows of the Source
// Control view, and the actions in the title of an editor. The rows of the other commands (stage, discard...) come with their steps.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/resourceState/context, editor/title)
func diffMenus() []extension.MenuItem {
	const (
		git         = "scmProvider == git"
		enabled     = "config.git.enabled && !git.missing && gitOpenRepositoryCount != 0"
		inlineOpen  = " && config.git.showInlineOpenFileAction && config.git.openDiffOnClick"
		inlineOpenC = " && config.git.showInlineOpenFileAction && !config.git.openDiffOnClick"
	)
	res := extension.MenuSCMResourceStateContext
	var out []extension.MenuItem
	for _, g := range []string{groupMerge, groupIndex, groupWorking, groupUntracked} {
		in := git + " && scmResourceGroup == " + g
		switch g {
		case groupMerge:
			out = append(out, item(res, "git.openFile", "navigation", in))
		case groupIndex:
			out = append(out,
				item(res, "git.openChange", "navigation", in),
				item(res, "git.openFile", "navigation", in),
				item(res, "git.openHEADFile", "navigation", in))
		default:
			out = append(out,
				item(res, "git.openChange", "navigation", in),
				item(res, "git.openHEADFile", "navigation", in),
				item(res, "git.openFile", "navigation", in))
		}
		out = append(out,
			item(res, "git.revealFileInOS.linux", "2_view@1", in+" && isLinux"),
			item(res, "git.revealInExplorer", "2_view@2", in),
			item(res, "git.openFile2", "inline@1", in+inlineOpen),
			item(res, "git.openChange", "inline@1", in+inlineOpenC))
	}
	title := extension.MenuEditorTitle
	out = append(out,
		item(title, "git.openFile", "navigation", enabled+" && isInDiffEditor && (resourceScheme == git || resourceScheme == file)"),
		item(title, "git.openFile", "navigation", enabled+" && !isInDiffEditor && resourceScheme == git"),
		item(title, "git.openChange", "navigation@2", enabled+" && !isInDiffEditor && !isMergeEditor && resourceScheme == file && scmActiveResourceHasChanges"))
	return out
}
