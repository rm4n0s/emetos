package gitscm

import "github.com/rm4n0s/emetos/extension"

// The places of the commands by lines (SPEC §10.30 "The peek"): the actions in the title of the peek of a change, the toolbars in the
// gutter of the diff editor, the title and the context menu of the right side of a diff, and the keys of the commands that work on the
// selection. VS Code spells the versions of a file as git: URIs whose query holds the ref; Emetos's documents of the extension are
// emetos-virtual:/git/<repository>/<ref>/<path> (uri.go), so the conditions that read the ref read the path.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/change/title, diffEditor/gutter/hunk,
// diffEditor/gutter/selection, editor/title, editor/context; contributes.keybindings)

// The conditions on the URI of a version of a file. The ref "" is the index version, "~" the version a file changed in the work tree is
// compared with (the index version too).
const (
	// indexVersionWhen holds for a URI of the index version of a file.
	indexVersionWhen = `/^emetos-virtual:\/git\/[0-9a-f]+\/\/[^/]/`
	// workTreeVersionWhen holds for a URI of the version the work tree is compared with.
	workTreeVersionWhen = `/^emetos-virtual:\/git\/[0-9a-f]+\/~\/[^/]/`
)

// lineMenus are the menu items of the commands by lines.
func lineMenus() []extension.MenuItem {
	const (
		enabled = "config.git.enabled && !git.missing && gitOpenRepositoryCount != 0"
		diffRHS = enabled + " && isInDiffEditor && isInDiffRightEditor && !isEmbeddedDiffEditor"
	)
	change := extension.MenuSCMChangeTitle
	title, ctx := extension.MenuEditorTitle, extension.MenuEditorContext
	return []extension.MenuItem{
		item(change, "git.stageChange", "", "config.git.enabled && !git.missing && originalResource =~ "+indexVersionWhen),
		item(change, "git.revertChange", "", "config.git.enabled && !git.missing && originalResource =~ "+indexVersionWhen),
		item(change, "git.unstageChange", "", "false"),

		item(extension.MenuDiffEditorGutterHunk, "git.diff.stageHunk", "primary@10", "diffEditorOriginalUri =~ "+workTreeVersionWhen),
		item(extension.MenuDiffEditorGutterSelection, "git.diff.stageSelection", "primary@10", "diffEditorOriginalUri =~ "+workTreeVersionWhen),

		item(title, "git.stage", "2_git@1", diffRHS+" && resourceScheme == file"),
		item(title, "git.stageSelectedRanges", "2_git@2", diffRHS+" && resourceScheme == file"),
		item(title, "git.unstage", "2_git@3", diffRHS+" && resourceScheme == git"),
		item(title, "git.unstageSelectedRanges", "2_git@4", diffRHS+" && resourceScheme == git"),
		item(title, "git.revertSelectedRanges", "2_git@5", diffRHS+" && resourceScheme == file"),

		item(ctx, "git.stageSelectedRanges", "2_git@1", diffRHS+" && resourceScheme == file"),
		item(ctx, "git.unstageSelectedRanges", "2_git@2", diffRHS+" && resourceScheme == git"),
		item(ctx, "git.revertSelectedRanges", "2_git@3", diffRHS+" && resourceScheme == file"),
	}
}

// lineKeybindings are the default keys of the commands that work on the selection.
func lineKeybindings() []extension.Keybinding {
	return []extension.Keybinding{
		{Command: "git.stageSelectedRanges", Key: "ctrl+k ctrl+alt+s", When: "editorTextFocus && resourceScheme == file"},
		{Command: "git.unstageSelectedRanges", Key: "ctrl+k ctrl+n", When: "editorTextFocus && isInDiffEditor && isInDiffRightEditor && resourceScheme == git"},
		{Command: "git.revertSelectedRanges", Key: "ctrl+k ctrl+r", When: "editorTextFocus && resourceScheme == file"},
	}
}
