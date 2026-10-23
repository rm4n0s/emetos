// Package find holds the find and replace model (SPEC §9.8): the search of a text for a string
// or a regular expression (query.go, ported from VS Code's textModelSearch.ts), the replace
// pattern (replace.go), the state of the find widget (state.go), the decorations that mark the
// matches (decorations.go) and the model that moves to and replaces matches in an editor
// (model.go).
//
// Ported from VS Code: src/vs/editor/common/model/textModelSearch.ts,
// src/vs/editor/contrib/find/browser/{findModel,findState,findDecorations,replacePattern,replaceAllCommand}.ts
package find
