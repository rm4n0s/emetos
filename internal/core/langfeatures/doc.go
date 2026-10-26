// Package langfeatures is the features model of area 3 (SPEC §9.12): the language features the editor asks for
// (completion, hover, signature help, navigation, symbols, code actions, formatting, rename, highlights), the registry of
// the providers that answer them (a language server and the providers an extension wrote in Go alike), the merging of
// their answers the way VS Code merges them, the conversion of the answers of a language server (core/lsp) to the types of
// this package, and the application of a WorkspaceEdit to the documents and files of a window.
//
// It has no user interface and imports no Glutter. Positions and ranges are Emetos's: zero-based lines and columns in
// bytes of the UTF-8 line (D6). A provider is called off the UI thread with a Document, an immutable snapshot of the text
// at one version, so that its answer can be dropped when the document has changed meanwhile; applying a WorkspaceEdit
// belongs to the goroutine that owns the documents (the UI thread).
//
// Ported from VS Code, as behavior and not as code: src/vs/editor/common/languageFeatureRegistry.ts (the score of a
// selector and the order of the providers: higher score first, the newer registration first among equals),
// src/vs/editor/contrib/suggest/browser/suggest.ts (the first group of providers that answers),
// src/vs/editor/contrib/hover/browser/getHover.ts and src/vs/editor/contrib/gotoSymbol/browser/goToSymbol.ts (hovers and
// locations of all providers) and src/vs/editor/contrib/format/browser/format.ts (the choice of one formatter).
package langfeatures
