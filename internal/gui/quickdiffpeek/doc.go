// Package quickdiffpeek is the peek of a change of the quick diff (SPEC §10.30 "The peek", step 24.12): a view zone under a changed block of
// lines of an editor, with a title bar (the provider's label, "Change 2 of 5", the actions the extensions put in the menu of the peek,
// and the arrows and the close button) over a small diff editor of the original and the modified text of the change. The Controller is
// what the editor of a file holds (editor.QuickDiffPeeker); the window gives it the model of the changes and makes the small diff editor,
// so the package knows nothing of how documents and editors are made.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/quickDiffWidget.ts
package quickdiffpeek
