// Package outline is the Outline view of the Explorer (SPEC §9.12 "Symbols", "As built (step 12.7)"): the symbols of the active
// editor as a tree with the icons of their kinds, following the cursor, and the body widget that shows it.
//
// The model (Outline) reads the symbols from the view model of the active editor, which asks the symbol providers for them (the
// editor package owns the request and the cache), so the Outline, the breadcrumbs, sticky scroll and the Go to Symbol picker share
// one request per document version. The window makes the model, tells it which editor is active and binds the commands.
//
// Ported from VS Code: src/vs/workbench/contrib/codeEditor/browser/outline/documentSymbolsOutline.ts and
// src/vs/workbench/contrib/outline/browser/outlinePane.ts
package outline
