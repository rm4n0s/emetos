package extension

import "context"

// The Quick Diff API (SPEC §10.30 "Quick diff", step 24.11; after QuickDiffProvider and SourceControl.quickDiffProvider of vscode.d.ts and
// extHostSCM.ts). As in VS Code the workbench draws the marks of the changes of a file in the gutter, the overview ruler and the minimap and
// moves between them, and a source control only says what a file is compared with: the provider answers, for a file, with the URI of
// the document that holds its original text (the version in the index, for Git). The document is one that the extension provides
// (Documents().ProvideContent), so the host reads it the way it reads any provided document.
//
// Everything here may be called from any goroutine, except where a comment says that Emetos calls the extension.

// QuickDiffKind tells how a provider's changes are shown (VS Code's QuickDiffProviderKind: primary, secondary).
type QuickDiffKind int

const (
	// QuickDiffPrimary is the provider whose changes are the marks of the file, and whose original the format on save of modifications
	// compares with (VS Code's SourceControl.quickDiffProvider).
	QuickDiffPrimary QuickDiffKind = iota
	// QuickDiffSecondary is a provider whose changes are drawn in the secondary colors, where the primary provider's changes do not
	// cover them (VS Code's SourceControl.secondaryQuickDiffProvider; Git's staged changes).
	QuickDiffSecondary
)

// QuickDiffProvider says what the files of a source control are compared with.
type QuickDiffProvider struct {
	// Label names the provider ("Git Local Changes (Working Tree)"); the peek of step 24.12 shows it.
	Label string
	// Kind says whether the provider is the primary one of its source control or a secondary one.
	Kind QuickDiffKind
	// Original returns the URI of the document that holds the original text of the file with the URI (file:///path), or "" when the file
	// has no original under this provider (an untracked file, a file of another repository). The host reads the document the way it reads
	// a provided one, and compares it with the file; it asks again after RefreshQuickDiff and after the provider's content changed
	// (ContentRegistration.Changed), not on every edit. It runs off the UI thread and must stop when ctx does; a failure means no
	// original for the file, and is logged by the host at debug level. Only files below the root of the source control are asked.
	Original func(ctx context.Context, uri string) (string, error)
}

// QuickDiffRegistration is a registered QuickDiffProvider.
type QuickDiffRegistration interface {
	// Dispose takes the provider away: the marks it made go. It does nothing the second time, and happens by itself when the activation
	// ends.
	Dispose()
}

// LineChange is a changed block of lines of a text against its original, VS Code's ILineChange (SPEC §10.30 "The peek"): lines are
// counted from 1, and an end of 0 says that the side has no lines in the block. An added block has OriginalEndLineNumber 0 and
// OriginalStartLineNumber the line it follows; a deleted block has ModifiedEndLineNumber 0 and ModifiedStartLineNumber the line the
// lines were deleted after (0 for the start of the text).
type LineChange struct {
	OriginalStartLineNumber, OriginalEndLineNumber int
	ModifiedStartLineNumber, ModifiedEndLineNumber int
}

// The members of a LineChange as the arguments of a command carry it (VS Code's names).
const (
	// LineChangeOriginalStart is OriginalStartLineNumber.
	LineChangeOriginalStart = "originalStartLineNumber"
	// LineChangeOriginalEnd is OriginalEndLineNumber.
	LineChangeOriginalEnd = "originalEndLineNumber"
	// LineChangeModifiedStart is ModifiedStartLineNumber.
	LineChangeModifiedStart = "modifiedStartLineNumber"
	// LineChangeModifiedEnd is ModifiedEndLineNumber.
	LineChangeModifiedEnd = "modifiedEndLineNumber"
)

// Map is the change as the argument of a command carries it: an object with the members LineChangeOriginalStart and the others.
func (c LineChange) Map() map[string]any {
	return map[string]any{
		LineChangeOriginalStart: c.OriginalStartLineNumber, LineChangeOriginalEnd: c.OriginalEndLineNumber,
		LineChangeModifiedStart: c.ModifiedStartLineNumber, LineChangeModifiedEnd: c.ModifiedEndLineNumber,
	}
}

// QuickDiffArgs makes the arguments the commands of the peek of a change (MenuSCMChangeTitle) are run with: the URI of the file, the
// changes of the provider the shown change belongs to, and the index of the shown change among them (VS Code passes the three as
// separate arguments; Emetos gives the command one list, as for SCMArgs).
func QuickDiffArgs(uri string, changes []LineChange, index int) any {
	list := make([]any, len(changes))
	for i, c := range changes {
		list[i] = c.Map()
	}
	return []any{uri, list, index}
}

// ReadQuickDiffArgs reads the arguments QuickDiffArgs made: ok is false for any other value (the command was run from the palette or by
// a key) and for an index outside the changes.
func ReadQuickDiffArgs(args any) (uri string, changes []LineChange, index int, ok bool) {
	list, isList := args.([]any)
	if !isList || len(list) != 3 {
		return "", nil, 0, false
	}
	uri, ok = list[0].(string)
	if !ok || uri == "" {
		return "", nil, 0, false
	}
	index, ok = intOf(list[2])
	if !ok {
		return "", nil, 0, false
	}
	switch v := list[1].(type) {
	case []LineChange:
		changes = v
	case []any:
		for _, e := range v {
			m, isMap := e.(map[string]any)
			if !isMap {
				return "", nil, 0, false
			}
			var c LineChange
			var all [4]bool
			c.OriginalStartLineNumber, all[0] = intOf(m[LineChangeOriginalStart])
			c.OriginalEndLineNumber, all[1] = intOf(m[LineChangeOriginalEnd])
			c.ModifiedStartLineNumber, all[2] = intOf(m[LineChangeModifiedStart])
			c.ModifiedEndLineNumber, all[3] = intOf(m[LineChangeModifiedEnd])
			if !all[0] || !all[1] || !all[2] || !all[3] {
				return "", nil, 0, false
			}
			changes = append(changes, c)
		}
	default:
		return "", nil, 0, false
	}
	if index < 0 || index >= len(changes) {
		return "", nil, 0, false
	}
	return uri, changes, index, true
}

// intOf reads a number a command argument carries: an int, or a float64 as JSON has it.
func intOf(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), n == float64(int(n))
	}
	return 0, false
}

// The members of the arguments the toolbars of the diff editor's gutter give a command (DiffEditorContext).
const (
	// DiffArgOriginalURI is the URI of the original document of the diff editor.
	DiffArgOriginalURI = "originalUri"
	// DiffArgModifiedURI is the URI of the modified document of the diff editor.
	DiffArgModifiedURI = "modifiedUri"
	// DiffArgOriginalWithModifiedChanges is the original text with the changes of the hunk, or of the selection, applied.
	DiffArgOriginalWithModifiedChanges = "originalWithModifiedChanges"
)

// DiffEditorContext is what a command of the toolbar in the gutter of the diff editor (MenuDiffEditorGutterHunk and
// MenuDiffEditorGutterSelection) is run with (VS Code's DiffEditorSelectionHunkToolbarContext). The command gets one object with the
// members DiffArgOriginalURI, DiffArgModifiedURI and DiffArgOriginalWithModifiedChanges, which ReadDiffEditorArgs reads.
type DiffEditorContext struct {
	// OriginalURI and ModifiedURI are the documents of the two sides.
	OriginalURI, ModifiedURI string
	// OriginalWithModifiedChanges is the text of the original with the changes of the hunk, or of the selected lines, applied.
	OriginalWithModifiedChanges string
}

// Args is the value the command of the toolbar is run with.
func (c DiffEditorContext) Args() any {
	return map[string]any{
		DiffArgOriginalURI: c.OriginalURI, DiffArgModifiedURI: c.ModifiedURI, DiffArgOriginalWithModifiedChanges: c.OriginalWithModifiedChanges,
	}
}

// ReadDiffEditorArgs reads the arguments DiffEditorContext.Args made; ok is false for any other value (the command was run from the
// palette or by a key).
func ReadDiffEditorArgs(args any) (DiffEditorContext, bool) {
	m, isMap := args.(map[string]any)
	if !isMap {
		return DiffEditorContext{}, false
	}
	var c DiffEditorContext
	var okOriginal, okModified, okText bool
	c.OriginalURI, okOriginal = m[DiffArgOriginalURI].(string)
	c.ModifiedURI, okModified = m[DiffArgModifiedURI].(string)
	c.OriginalWithModifiedChanges, okText = m[DiffArgOriginalWithModifiedChanges].(string)
	return c, okOriginal && okModified && okText
}
