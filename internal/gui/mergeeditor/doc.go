// Package mergeeditor holds the merge editor (SPEC §9.11): the incoming and the current side of a three-way merge on top,
// the result below, the conflicts outlined and the three editors scrolled together. Session keeps the merge model and the
// result document the same; Model lines the inputs up, marks the conflicts and navigates them; Widget lays the three
// editors out. The scroll maps, the alignment of the inputs, the navigation order and the checkbox layout have no Glutter
// in them.
//
// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/view/
package mergeeditor
