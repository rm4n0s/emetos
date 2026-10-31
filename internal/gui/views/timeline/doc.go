// Package timeline is the Timeline view of the Explorer (SPEC §10.30 "The Timeline view", step 24.21): the history of the file in
// front, or of a file it is pinned to, from every provider of internal/core/timeline. Pane is the model, a port of the logic of VS
// Code's timelinePane.ts (what is asked of the providers, the pages that come back and are merged, the rows with their ages, the
// messages); it has no widget and is tested with fake providers. Body is the widget over it.
//
// Ported from VS Code: src/vs/workbench/contrib/timeline/browser/timelinePane.ts
package timeline
