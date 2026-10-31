// Package timeline is the timeline service (SPEC §10.30 "The Timeline view", step 24.21): the providers of the history of a file
// that extensions register (the Git extension's "Git History"), the questions the Timeline view asks them, and the events that
// tell the view a provider came, went or has new items. The model of the view is in internal/gui/views/timeline; this package has
// no UI.
//
// It is VS Code's TimelineService (common/timelineService.ts) without the workspace timeline ("Pin Workspace Timeline" is not
// built) and the editable item. A provider is asked for a page of items for a URI; the answer of Request comes from the provider's
// goroutine and is sorted newest first, ties by the provider's ID, as VS Code sorts it.
//
// The service is safe for use from several goroutines. Events are announced from the goroutine that made the change, after the
// lock is released.
//
// Ported from VS Code: src/vs/workbench/contrib/timeline/common/timelineService.ts and common/timeline.ts
package timeline
