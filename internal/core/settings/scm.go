package settings

// categorySourceControl is the group of the settings of Source Control.
const categorySourceControl = "Source Control"

// registerSourceControl registers the scm.* settings of the Source Control view, the input and the graph (SPEC §10.30, step 24.2),
// with VS Code's keys, descriptions, scopes (all are resource settings) and defaults. Not registered, as the spec says:
// scm.workingSets.* (editor working sets per history item group) and scm.repositories.explorer (the artifacts of the
// repositories view).
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scm.contribution.ts
func registerSourceControl(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "scm.diffDecorations", Types: []Type{String}, Default: "all", Enum: []any{"all", "gutter", "overview", "minimap", "none"},
			EnumDescriptions: []string{
				"Show the diff decorations in all available locations.",
				"Show the diff decorations only in the editor gutter.",
				"Show the diff decorations only in the overview ruler.",
				"Show the diff decorations only in the minimap.",
				"Do not show the diff decorations.",
			},
			Description: "Controls diff decorations in the editor.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.diffDecorationsGutterWidth", Types: []Type{Number}, Default: 3, Enum: []any{1, 2, 3, 4, 5},
			Description: "Controls the width(px) of diff decorations in gutter (added & modified).",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.diffDecorationsGutterVisibility", Types: []Type{String}, Default: "always", Enum: []any{"always", "hover"},
			EnumDescriptions: []string{
				"Show the diff decorator in the gutter at all times.",
				"Show the diff decorator in the gutter only on hover.",
			},
			Description: "Controls the visibility of the Source Control diff decorator in the gutter.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.diffDecorationsGutterAction", Types: []Type{String}, Default: "diff", Enum: []any{"diff", "none"},
			EnumDescriptions: []string{"Show the inline diff Peek view on click.", "Do nothing."},
			Description:      "Controls the behavior of Source Control diff gutter decorations.",
			Scope:            ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.diffDecorationsGutterPattern", Types: []Type{Object}, Default: map[string]any{"added": false, "modified": true},
			Description: "Controls whether a pattern is used for the diff decorations in gutter.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.diffDecorationsIgnoreTrimWhitespace", Types: []Type{String}, Default: "false", Enum: []any{"true", "false", "inherit"},
			EnumDescriptions: []string{
				"Ignore leading and trailing whitespace.",
				"Do not ignore leading and trailing whitespace.",
				"Inherit from `diffEditor.ignoreTrimWhitespace`.",
			},
			Description: "Controls whether leading and trailing whitespace is ignored in Source Control diff gutter decorations.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.alwaysShowActions", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether inline actions are always visible in the Source Control view.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.countBadge", Types: []Type{String}, Default: "all", Enum: []any{"all", "focused", "off"},
			EnumDescriptions: []string{
				"Show the sum of all Source Control Provider count badges.",
				"Show the count badge of the focused Source Control Provider.",
				"Disable the Source Control count badge.",
			},
			Description: "Controls the count badge on the Source Control icon on the Activity Bar.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.providerCountBadge", Types: []Type{String}, Default: "hidden", Enum: []any{"hidden", "auto", "visible"},
			EnumDescriptions: []string{
				"Hide Source Control Provider count badges.",
				"Only show count badge for Source Control Provider when non-zero.",
				"Show Source Control Provider count badges.",
			},
			Description: "Controls the count badges on Source Control Provider headers. These headers appear in the Source Control view when there is more than one provider or when the `#scm.alwaysShowRepositories#` setting is enabled, and in the Source Control Repositories view.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.defaultViewMode", Types: []Type{String}, Default: "list", Enum: []any{"tree", "list"},
			EnumDescriptions: []string{"Show the repository changes as a tree.", "Show the repository changes as a list."},
			Description:      "Controls the default Source Control repository view mode.",
			Scope:            ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.defaultViewSortKey", Types: []Type{String}, Default: "path", Enum: []any{"name", "path", "status"},
			EnumDescriptions: []string{
				"Sort the repository changes by file name.",
				"Sort the repository changes by path.",
				"Sort the repository changes by Source Control status.",
			},
			Description: "Controls the default Source Control repository changes sort order when viewed as a list.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.autoReveal", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the Source Control view should automatically reveal and select files when opening them.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.inputFontFamily", Types: []Type{String}, Default: "default",
			Description: "Controls the font for the input message. Use `default` for the workbench user interface font family, `editor` for the `#editor.fontFamily#`'s value, or a custom font family.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.inputFontSize", Types: []Type{Number}, Default: 13,
			Description: "Controls the font size for the input message in pixels.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.inputMaxLineCount", Types: []Type{Number}, Default: 10, Minimum: Float(1), Maximum: Float(50),
			Description: "Controls the maximum number of lines that the input will auto-grow to.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.inputMinLineCount", Types: []Type{Number}, Default: 1, Minimum: Float(1), Maximum: Float(50),
			Description: "Controls the minimum number of lines that the input will auto-grow from.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.alwaysShowRepositories", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether repositories should always be visible in the Source Control view.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.repositories.sortOrder", Types: []Type{String}, Default: "discovery time", Enum: []any{"discovery time", "name", "path"},
			EnumDescriptions: []string{
				"Repositories in the Source Control Repositories view are sorted by discovery time. Repositories in the Source Control view are sorted in the order that they were selected.",
				"Repositories in the Source Control Repositories and Source Control views are sorted by repository name.",
				"Repositories in the Source Control Repositories and Source Control views are sorted by repository path.",
			},
			Description: "Controls the sort order of the repositories in the source control repositories view.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.repositories.visible", Types: []Type{Number}, Default: 10,
			Description: "Controls how many repositories are visible in the Source Control Repositories section. Set to 0, to be able to manually resize the view.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.repositories.selectionMode", Types: []Type{String}, Default: "multiple", Enum: []any{"multiple", "single"},
			EnumDescriptions: []string{"Multiple repositories can be selected at the same time.", "Only one repository can be selected at a time."},
			Description:      "Controls the selection mode of the repositories in the Source Control Repositories view.",
			Scope:            ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.showActionButton", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether an action button can be shown in the Source Control view.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.showInputActionButton", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether an action button can be shown in the Source Control input.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.compactFolders", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the Source Control view should render folders in a compact form. In such a form, single child folders will be compressed in a combined tree element.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.graph.pageOnScroll", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the Source Control Graph view will load the next page of items when you scroll to the end of the list.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.graph.pageSize", Types: []Type{Number}, Default: 50, Minimum: Float(1), Maximum: Float(1000),
			Description: "The number of items to show in the Source Control Graph view by default and when loading more items.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.graph.badges", Types: []Type{String}, Default: "filter", Enum: []any{"all", "filter"},
			EnumDescriptions: []string{
				"Show badges of all history item groups in the Source Control Graph view.",
				"Show only the badges of history item groups used as a filter in the Source Control Graph view.",
			},
			Description: "Controls which badges are shown in the Source Control Graph view. The badges are shown on the right side of the graph indicating the names of history item groups.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.graph.showIncomingChanges", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether to show incoming changes in the Source Control Graph view.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
		Setting{
			Key: "scm.graph.showOutgoingChanges", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether to show outgoing changes in the Source Control Graph view.",
			Scope:       ScopeResource, Category: categorySourceControl,
		},
	)
}
