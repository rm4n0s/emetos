package settings

// categoryTimeline is the group of the settings of the Timeline view.
const categoryTimeline = "Timeline"

// registerTimeline registers the timeline.* settings of the Timeline view (SPEC §10.30, step 24.21), with VS Code's keys, descriptions,
// scopes and defaults.
//
// Ported from VS Code: src/vs/workbench/contrib/timeline/browser/timeline.contribution.ts
func registerTimeline(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "timeline.pageSize", Types: []Type{Number, Null}, Default: 50,
			Description: "The number of items to show in the Timeline view by default and when loading more items. Setting to `null` will automatically choose a page size based on the visible area of the Timeline view.",
			Scope:       ScopeWindow, Category: categoryTimeline,
		},
		Setting{
			Key: "timeline.pageOnScroll", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the Timeline view will load the next page of items when you scroll to the end of the list.",
			Scope:       ScopeWindow, Category: categoryTimeline,
		},
	)
}
