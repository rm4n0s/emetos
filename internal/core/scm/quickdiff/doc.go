// Package quickdiff is the model of the quick diff (SPEC §10.30 "Quick diff", step 24.11): the changes of a document against the
// original text a source control says it is compared with, as VS Code's IChange (added, modified, deleted lines, 1-based), computed off
// the UI thread 250 ms after the last edit with core/diff, and the registry of the providers that say what a file is compared with.
// It has no UI: the editor draws the marks from a Model's changes.
//
// A Service holds the providers of the source controls of a window. A Model follows one document: it asks the service for the
// originals of the document's file, compares the text with each and keeps the changes. A Service tells its models when the providers
// or the originals changed (Refresh), and a model reads the originals again only then, not on every edit.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/quickDiffModel.ts, src/vs/workbench/contrib/scm/common/quickDiff.ts and
// quickDiffService.ts
package quickdiff
