package command

// The Timeline view of the Explorer (SPEC §10.30 "The Timeline view", step 24.21): its id and its commands. The items of its menus
// are those the extensions add to TimelineItemContext; the buttons of its title (Refresh, Pin Current Timeline and the filter of the
// sources) are built by the window, as their icon depends on the state of the view.
//
// Ported from VS Code: src/vs/workbench/contrib/timeline/browser/timeline.contribution.ts and timelinePane.ts (TimelinePaneCommands)

// ViewTimeline is the Timeline view of the Explorer.
const ViewTimeline = "timeline"

// registerTimelineCommands registers the commands of the Timeline view. files.openTimeline and timeline.toggleFollowActiveEditor have no
// title: VS Code lists neither in the command palette (the first is a row of the menus of the Explorer and of the tabs, the second a
// button of the title of the view).
func registerTimelineCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "timeline.focus", Title: "Focus on Timeline View", Category: "View"},
		Meta{ID: "timeline.refresh", Title: "Refresh", Category: "Timeline", Icon: "refresh"},
		Meta{ID: "timeline.toggleFollowActiveEditor"},
		Meta{ID: "files.openTimeline"},
	)
}

// registerTimelineMenus adds the Open Timeline row to the context menu of the Explorer and of a tab, for a file while a timeline
// provider is registered (the context key timelineHasProvider).
func registerTimelineMenus(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: ExplorerContext, Command: "files.openTimeline", Title: "Open Timeline", Group: "4_timeline", Order: 1,
			When: when("!explorerResourceIsFolder && resourceScheme && timelineHasProvider")},
		MenuItem{Menu: EditorTitleContext, Command: "files.openTimeline", Title: "Open Timeline", Group: "2_files", Order: 2,
			When: when("isFileSystemResource && timelineHasProvider")},
	)
}
