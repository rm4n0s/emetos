// Package ui is what an extension builds the widgets of its views with (SPEC §10.15 "As built (step 11.5)"). The widget of
// a UI contribution is Glutter's widgets.Widget; an extension may use Glutter's widgets layer directly, and this package adds
// what only Emetos has: the theme colors of the window, the Codicons, and the widgets of the workbench's kit (lists, trees,
// buttons, input boxes, badges, progress bars, hovers), so that a view looks like the editor's own.
//
// The package is the one place under extension/ that imports Emetos's internal packages, and only internal/gui/kit and its
// font package (internal/archtest checks it); it re-exports a curated set as aliases and thin constructors, so an extension
// never imports internal/... itself and the kit's other widgets stay private. An extension that needs another kit widget
// asks for it to be added here.
//
// # The box
//
// The widget of a view is built inside a box that the workbench makes: the box gives it the constraints of the view, clips
// what it paints to them, keeps the keyboard focus inside while the view has it (a key the widget does not handle goes on
// to the global keys of the workbench, and Escape still works), and sets the text style and colors of the theme. A panic in
// the Build function of a view fails the extension (SPEC §10.15 "Lifetime"). A panic deeper in the tree is caught by
// Glutter, which puts an error widget in its place and logs it; to have it fail the extension too, build the parts of the
// view that can fail with NewBuilder from this package.
package ui
